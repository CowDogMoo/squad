package main

// This file contains the CLI wrappers that drive real OS-service installs
// (squad routine repair, the auto-install branch of squad routine create).
// Each function calls service.Install() / service.Status() which shell out
// to launchctl / systemctl / Task Scheduler. Mocking those would require a
// service abstraction we do not want for v1.
//
// Codecov ignores this file because every branch involves a side effect on
// the host's OS service registry. The non-install half of the same flows
// (manifest CRUD, addressing, state file IO) is tested in routine_test.go
// and routine_cli_test.go.

import (
	"errors"
	"fmt"
	"os"

	"github.com/cowdogmoo/squad/routine"
	"github.com/cowdogmoo/squad/routine/service"
	"github.com/spf13/cobra"
)

func newRoutineRepairCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repair",
		Short: "Reinstall the OS service for the routines daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			svc := service.New()
			bin, err := daemonBinaryPath()
			if err != nil {
				return err
			}
			store := routine.NewStore()
			if _, err := store.LoadAll(); err != nil {
				return err
			}
			opts := service.InstallOptions{WakeSystem: anyRoutineWantsWake(store)}
			if err := svc.Install(bin, opts); err != nil {
				if errors.Is(err, service.ErrUnsupported) {
					return fmt.Errorf("%w — run `squad routined` manually for now", err)
				}
				return err
			}
			st, _ := svc.Status()
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Service installed at %s\n", st.ServicePath)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Daemon: %s\n", bin)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Logs:   %s\n", st.LogPath)
			if opts.WakeSystem {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "WakeSystem: enabled (at least one routine has wake_system: true)")
			}
			return nil
		},
	}
}

// ensureServiceInstalled is the first-routine convenience that installs the
// OS service when it's missing AND keeps the install in sync with the
// current routines' wake_system union. Idempotent install lets us call it
// after every `routine create` without thrashing the daemon.
//
// The returned string is a user-facing message to print on changes (empty
// when nothing was done); errors come back as the error value so callers
// can downgrade to a warning.
//
// Set SQUAD_SKIP_SERVICE_INSTALL=1 to skip the install path entirely. This
// is intended for tests and CI runners that should not register a real
// LaunchAgent / systemd unit / Task Scheduler entry on the host.
func ensureServiceInstalled(store *routine.Store) (string, error) {
	if os.Getenv("SQUAD_SKIP_SERVICE_INSTALL") != "" {
		return "", nil
	}
	svc := service.New()
	st, err := svc.Status()
	if err != nil {
		if errors.Is(err, service.ErrUnsupported) {
			return "Service install is not supported on this platform yet — run `squad routined` manually.", nil
		}
		return "", err
	}
	wantWake := anyRoutineWantsWake(store)
	currentlyInstalled := st.State != service.StateNotInstalled
	if currentlyInstalled && !wantWake {
		// Already installed; nothing new requires re-render. Reinstallation
		// when wake-system flips on is handled below.
		return "", nil
	}
	bin, err := daemonBinaryPath()
	if err != nil {
		return "", err
	}
	if err := svc.Install(bin, service.InstallOptions{WakeSystem: wantWake}); err != nil {
		if errors.Is(err, service.ErrUnsupported) {
			return "Service install is not supported on this platform yet — run `squad routined` manually.", nil
		}
		return "", fmt.Errorf("install service: %w", err)
	}
	if currentlyInstalled {
		return "Reinstalled routines daemon with WakeSystem=true to match routine settings.", nil
	}
	return fmt.Sprintf("Installed routines daemon as a per-user service\n  artifact: %s\n  logs:     %s",
		st.ServicePath, st.LogPath), nil
}
