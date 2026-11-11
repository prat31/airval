// Command airval is the single airval binary: all-in-one dev mode, API server, worker, migrations
// and the CI client.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// Set via -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = ""
)

// exitError carries a process exit code (e.g. failed gate in `airval run --gate`).
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := newRoot()
	if err := root.ExecuteContext(ctx); err != nil {
		var ee exitError
		if errors.As(err, &ee) {
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	var cfgPath string
	root := &cobra.Command{
		Use:           "airval",
		Short:         "airval — self-hostable evaluation platform for AI services",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "config file (YAML); env AIRVAL_CONFIG")
	cfg := func() string { return cfgPath }
	root.AddCommand(
		devCmd(cfg), serverCmd(cfg), workerCmd(cfg), migrateCmd(cfg), secretsCmd(cfg), configCmd(cfg),
		runCmd(), versionCmd(),
	)
	return root
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, _ []string) {
			c := commit
			if c == "" {
				c = "unknown"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "airval %s (commit %s, %s)\n", version, c, edition())
		},
	}
}
