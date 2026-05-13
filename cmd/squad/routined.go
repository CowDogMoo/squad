package main

import (
	"fmt"

	"github.com/cowdogmoo/squad/routine/daemon"
	"github.com/spf13/cobra"
)

// newRoutinedCmd builds the hidden daemon command. It is not shown in `--help`
// because users interact with routines through `squad routine` — the OS
// service wrapper invokes `squad routined` directly.
func newRoutinedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "routined",
		Short:  "Run the squad routines daemon (used by the OS service)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			cfg := configFromContext(cmd)
			if cfg == nil {
				return fmt.Errorf("config not available")
			}
			logFile, _ := cmd.Flags().GetString("log-file")
			if err := daemon.RedirectStdio(logFile); err != nil {
				return err
			}
			maxConcurrent, _ := cmd.Flags().GetUint("max-concurrent")
			fireTimeout, _ := cmd.Flags().GetDuration("fire-timeout")
			return daemon.Run(cmd.Context(), cfg, daemon.Options{
				MaxConcurrent: maxConcurrent,
				FireTimeout:   fireTimeout,
			})
		},
	}
	cmd.Flags().Uint("max-concurrent", 2, "Maximum concurrent routine fires across the daemon")
	cmd.Flags().Duration("fire-timeout", 0, "Optional per-fire wall-clock timeout (0 = unlimited)")
	cmd.Flags().String("log-file", "", "Append daemon stdout/stderr to this file (Windows install uses this; launchd/systemd handle redirection natively)")
	return cmd
}
