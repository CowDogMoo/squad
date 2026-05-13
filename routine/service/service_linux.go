//go:build linux

package service

import (
	"bytes"
	"fmt"
	"os"
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
