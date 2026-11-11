package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/prat31/airval/internal/app"
	"github.com/prat31/airval/internal/config"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/secrets"
	"github.com/prat31/airval/internal/store/migrations"
	"github.com/prat31/airval/internal/telemetry"
)

func edition() string { return ext.Edition() }

func buildInfo() app.BuildInfo { return app.BuildInfo{Version: version, Commit: commit} }

func runMode(ctx context.Context, mode config.Mode, cfgPath string, mutate func(*config.Config)) error {
	c, err := config.Load(mode, cfgPath)
	if err != nil {
		return err
	}
	if mutate != nil {
		mutate(c)
	}
	a, err := app.New(ctx, mode, c, buildInfo())
	if err != nil {
		return err
	}
	defer a.Close()
	return a.Run(ctx)
}

func devCmd(cfg func() string) *cobra.Command {
	var (
		addr string
		open bool
	)
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "All-in-one: server + worker + embedded NATS + SQLite in ./airval-data",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMode(cmd.Context(), config.ModeDev, cfg(), func(c *config.Config) {
				if addr != "" {
					c.Server.Addr = addr
					// Keep links (webhook URLs, notifications, --open) pointing at this listener unless
					// server.base_url was configured explicitly.
					if os.Getenv("AIRVAL_SERVER_BASE_URL") == "" && c.Server.BaseURL == "http://localhost:8080" {
						c.Server.BaseURL = devBaseURL(addr)
					}
				}
				if open {
					go func() {
						time.Sleep(time.Second)
						openBrowser(c.Server.BaseURL)
					}()
				}
			})
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "listen address (default :8080)")
	cmd.Flags().StringVar(&addr, "listen", "", "alias for --addr")
	cmd.Flags().BoolVar(&open, "open", false, "open the UI in a browser")
	return cmd
}

// devBaseURL turns a listen address (":9000", "127.0.0.1:9000", "[::]:9000") into a browsable URL.
func devBaseURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://localhost:8080"
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "localhost"
	} else if ip != nil && ip.To4() == nil {
		host = "[" + host + "]"
	}
	return "http://" + host + ":" + port
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}

func serverCmd(cfg func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "API, UI, scheduler and planner (stateless; scale horizontally)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMode(cmd.Context(), config.ModeServer, cfg(), nil)
		},
	}
}

func workerCmd(cfg func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Execute run jobs from the queue (scale horizontally)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMode(cmd.Context(), config.ModeWorker, cfg(), nil)
		},
	}
}

func migrateCmd(cfg func() string) *cobra.Command {
	cmd := &cobra.Command{Use: "migrate", Short: "Database migrations"}
	open := func(cmd *cobra.Command) (*app.App, func(), error) {
		c, err := config.Load(config.ModeCLI, cfg())
		if err != nil {
			return nil, nil, err
		}
		if c.Database.URL == "" {
			c.Database.URL = "sqlite://" + config.DataDir + "/airval.db"
		}
		log := telemetry.NewLogger(c.Telemetry, cmd.ErrOrStderr())
		st, err := app.OpenStore(cmd.Context(), c, log, false)
		if err != nil {
			return nil, nil, err
		}
		return &app.App{Store: st, Config: c, Logger: log}, func() { _ = st.Close() }, nil
	}
	cmd.AddCommand(&cobra.Command{
		Use: "up", Short: "Apply all pending migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, done, err := open(cmd)
			if err != nil {
				return err
			}
			defer done()
			if err := migrations.Up(cmd.Context(), a.Store.DB(), a.Store.Dialect()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "migrations applied")
			return nil
		},
	}, &cobra.Command{
		Use: "down", Short: "Roll back the latest migration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, done, err := open(cmd)
			if err != nil {
				return err
			}
			defer done()
			if err := migrations.Down(cmd.Context(), a.Store.DB(), a.Store.Dialect()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "rolled back one migration")
			return nil
		},
	}, &cobra.Command{
		Use: "status", Short: "Show migration status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, done, err := open(cmd)
			if err != nil {
				return err
			}
			defer done()
			st, err := migrations.List(cmd.Context(), a.Store.DB(), a.Store.Dialect())
			if err != nil {
				return err
			}
			for _, s := range st {
				state := "pending"
				if s.Applied {
					state = "applied"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%05d  %-8s %s\n", s.Version, state, s.Source)
			}
			return nil
		},
	})
	return cmd
}

func secretsCmd(cfg func() string) *cobra.Command {
	cmd := &cobra.Command{Use: "secrets", Short: "Secrets maintenance"}
	cmd.AddCommand(&cobra.Command{
		Use:   "rotate",
		Short: "Re-encrypt all secrets with the current master key (keep old keys in secrets.previous_keys)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := config.Load(config.ModeCLI, cfg())
			if err != nil {
				return err
			}
			mode := config.ModeServer
			if c.Database.URL == "" {
				c.Database.URL, mode = "sqlite://"+config.DataDir+"/airval.db", config.ModeDev
			}
			log := telemetry.NewLogger(c.Telemetry, cmd.ErrOrStderr())
			kr, err := app.Keyring(mode, c, log)
			if err != nil {
				return err
			}
			st, err := app.OpenStore(cmd.Context(), c, log, false)
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := secrets.Rotate(cmd.Context(), st, kr)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "re-encrypted %d secrets with key version %d\n", n, kr.Version())
			return nil
		},
	}, &cobra.Command{
		Use:   "gen-key",
		Short: "Generate a new random master key (base64, 32 bytes)",
		Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), secrets.GenerateKey()) },
	})
	return cmd
}

func configCmd(cfg func() string) *cobra.Command {
	var mode string
	cmd := &cobra.Command{Use: "config", Short: "Configuration"}
	pr := &cobra.Command{
		Use:   "print [dev|server|worker]",
		Short: "Print the effective configuration of a mode (defaults + file + env; secrets redacted)",
		Long: "Print the configuration the given mode would run with: that mode's defaults, then the config file, then\n" +
			"AIRVAL_* env vars. The mode comes from the argument, --mode, or AIRVAL_MODE (default: server).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m := mode
			if len(args) == 1 {
				m = args[0]
			}
			if m == "" {
				m = os.Getenv("AIRVAL_MODE")
			}
			if m == "" {
				m = string(config.ModeServer)
			}
			switch config.Mode(m) {
			case config.ModeDev, config.ModeServer, config.ModeWorker:
			default:
				return fmt.Errorf("unknown mode %q (use dev, server or worker)", m)
			}
			c, err := config.LoadUnvalidated(config.Mode(m), cfg())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "# effective configuration for `airval %s`\n", m)
			out, err := yaml.Marshal(c.Redacted())
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}
	pr.Flags().StringVar(&mode, "mode", "", "mode whose defaults apply: dev, server or worker (default: $AIRVAL_MODE, else server)")
	cmd.AddCommand(pr)
	return cmd
}
