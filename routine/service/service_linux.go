//go:build linux

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"text/template"
)

// unitName is the systemd --user service name.
const unitName = "squad-routined.service"

// unitTemplate is a user-mode service unit. Restart=always covers daemon
// crashes; RestartSec backs off so a panicking binary doesn't burn CPU.
// StandardOutput/StandardError go to the systemd journal, which `routine
// logs` can tail via journalctl in a follow-up phase.
const unitTemplate = `[Unit]
Description=Squad routines daemon
After=default.target

[Service]
Type=simple
ExecStart={{.Binary}} routined
Restart=always
RestartSec=5
Environment=HOME={{.Home}}
Environment=PATH=/usr/local/bin:/usr/bin:/bin
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`

type systemdService struct {
	home     string
	username string
	unitPath string
	logPath  string
}

// New returns the Linux systemd --user service implementation.
func New() Service {
	home, _ := os.UserHomeDir()
	username := ""
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return &systemdService{
		home:     home,
		username: username,
		unitPath: filepath.Join(configHome, "systemd", "user", unitName),
		// journalctl is the real log source; the file path is informational
		// for `routine doctor` until `routine logs` learns about journald.
		logPath: "journalctl --user -u " + unitName,
	}
}

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
	st := Status{
		ServicePath:  s.unitPath,
		LogPath:      s.logPath,
		DaemonBinary: s.daemonBinaryFromUnit(),
		State:        StateNotInstalled,
	}
	if _, err := os.Stat(s.unitPath); errors.Is(err, fs.ErrNotExist) {
		return st, nil
	} else if err != nil {
		return st, err
	}
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

// renderUnit returns the rendered unit file bytes for daemonBinary.
func (s *systemdService) renderUnit(daemonBinary string) ([]byte, error) {
	tmpl, err := template.New("unit").Parse(unitTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse unit template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{
		"Binary": daemonBinary,
		"Home":   s.home,
	}); err != nil {
		return nil, fmt.Errorf("render unit: %w", err)
	}
	return buf.Bytes(), nil
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

// daemonBinaryFromUnit parses ExecStart from the installed unit, mirroring
// the macOS helper. Returns empty when the unit is missing or unparsable.
func (s *systemdService) daemonBinaryFromUnit() string {
	data, err := os.ReadFile(s.unitPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "ExecStart="))
		if len(fields) == 0 {
			return ""
		}
		return fields[0]
	}
	return ""
}
