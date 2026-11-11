package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/airval/airval/internal/app"
	"github.com/airval/airval/internal/config"
	"github.com/airval/airval/internal/ext"
	"github.com/airval/airval/internal/secrets"
	"github.com/airval/airval/internal/store/migrations"
	"github.com/airval/airval/internal/telemetry"
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
	cmd.Flags().BoolVar(&open, "open", false, "open the UI in a browser")
	return cmd
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
		Use:   "print",
		Short: "Print the effective configuration (secrets redacted)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := config.LoadUnvalidated(config.Mode(mode), cfg())
			if err != nil {
				return err
			}
			out, err := yaml.Marshal(c.Redacted())
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}
	pr.Flags().StringVar(&mode, "mode", "dev", "mode whose defaults apply: dev|server|worker")
	cmd.AddCommand(pr)
	return cmd
}
