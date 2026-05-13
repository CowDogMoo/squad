//go:build linux

// This file contains the side-effectful half of the systemd installer:
// Install / Uninstall / Status / TailLogs (the bits that shell out to
// systemctl, loginctl, and journalctl). The pure template + parse code
// lives in service_linux.go.
//
// Codecov ignores this file because every function here is an exec.Command
// wrapper. The contracts are exercised end-to-end via integration tests
// (`squad routine repair` on a real systemd box); they cannot be reasonably
// unit-tested without mocking the systemd D-Bus surface.

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (s *systemdService) Install(daemonBinary string, _ InstallOptions) error {
	// InstallOptions.WakeSystem is intentionally ignored on Linux: waking
	// the system from a user-mode service requires root + RTC programming
	// and is not implementable through systemd --user units.
	if daemonBinary == "" {
		return errors.New("daemon binary path required")
	}
	abs, err := filepath.Abs(daemonBinary)
	if err != nil {
		return fmt.Errorf("resolve daemon binary: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("daemon binary %s not executable: %w", abs, err)
	}
	if err := os.MkdirAll(filepath.Dir(s.unitPath), 0o755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}

	rendered, err := s.renderUnit(abs)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.unitPath, rendered, 0o644); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}

	// Reload + enable + start. enable --now is the systemd idiom for
	// "install on boot AND start right now".
	if out, err := s.runSystemctlOutput("daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w (%s)", err, out)
	}
	if out, err := s.runSystemctlOutput("enable", "--now", unitName); err != nil {
		return fmt.Errorf("systemctl enable --now: %w (%s)", err, out)
	}

	// Linger lets the service survive logout. Failures are warned but not
	// fatal — many headless boxes have linger already, and some hosting
	// environments forbid it.
	if err := s.enableLinger(); err != nil {
		fmt.Fprintf(os.Stderr,
			"warning: could not enable linger for %s (%v); the daemon will stop when you log out — fix with `loginctl enable-linger %s` (may require sudo)\n",
			s.username, err, s.username)
	}
	return nil
}

func (s *systemdService) Uninstall() error {
	_, _ = s.runSystemctlOutput("disable", "--now", unitName)
	if err := os.Remove(s.unitPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove unit: %w", err)
	}
	_, _ = s.runSystemctlOutput("daemon-reload")
	return nil
}

func (s *systemdService) Status() (Status, error) {
	have, err := s.unitFileExists()
	if err != nil {
		return s.statusNotInstalled(), err
	}
	if !have {
		return s.statusNotInstalled(), nil
	}
	st := s.statusFromExistingUnit()
	// is-active: active|inactive|failed|activating etc. Exit non-zero
	// means not active; we still want to know it's installed.
	active, _ := s.runSystemctlOutput("is-active", unitName)
	activeStr := strings.TrimSpace(string(active))
	if activeStr == "active" || activeStr == "activating" {
		st.State = StateInstalledRunning
	} else {
		st.State = StateInstalledStopped
	}
	return st, nil
}

// TailLogs streams the journal entries for the daemon unit. Uses
// `journalctl --user -u <unit>` with -f for follow, otherwise reads the
// full backlog.
func (s *systemdService) TailLogs(ctx context.Context, w io.Writer, follow bool) error {
	args := []string{"--user", "-u", unitName, "--no-pager"}
	if follow {
		args = append(args, "-f")
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		// CommandContext returns context.Canceled when ctx terminates the
		// process; that's a clean stop, not a real failure.
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}

func (s *systemdService) runSystemctlOutput(args ...string) ([]byte, error) {
	full := append([]string{"--user"}, args...)
	cmd := exec.Command("systemctl", full...)
	return cmd.CombinedOutput()
}

// enableLinger calls `loginctl enable-linger <user>` so the daemon keeps
// running after logout. Most distros let users run this without sudo for
// their own account; some do not.
func (s *systemdService) enableLinger() error {
	if s.username == "" {
		return errors.New("no username")
	}
	cmd := exec.Command("loginctl", "enable-linger", s.username)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// statusFromExistingUnit returns a Status reflecting an existing unit file
// path. The "is the daemon currently running" portion lives in the install
// sibling so it's clearly the side-effectful half.
func (s *systemdService) statusFromExistingUnit() Status {
	return Status{
		ServicePath:  s.unitPath,
		LogPath:      s.logPath,
		DaemonBinary: s.daemonBinaryFromUnit(),
		State:        StateInstalledStopped, // refined by Status() when systemctl is available
	}
}

// statusNotInstalled returns the no-install Status, populated with paths so
// `routine doctor` can show users where things would live.
func (s *systemdService) statusNotInstalled() Status {
	return Status{
		ServicePath: s.unitPath,
		LogPath:     s.logPath,
		State:       StateNotInstalled,
	}
}

// unitFileExists reports whether the systemd --user unit file is on disk.
// Returns (false, nil) for a missing file; non-nil err signals an unexpected
// stat error.
func (s *systemdService) unitFileExists() (bool, error) {
	_, err := os.Stat(s.unitPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
