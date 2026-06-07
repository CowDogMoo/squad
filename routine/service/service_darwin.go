//go:build darwin

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
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// launchdLabel is the reverse-DNS identifier registered with launchd.
const launchdLabel = "dev.cowdogmoo.squad.routined"

// plistTemplate is the LaunchAgent template. RunAtLoad+KeepAlive give us
// daemon supervision: start at login, restart on crash. EnvironmentVariables
// preserves the small PATH the daemon needs for invoking `git`, `docker`, etc.
//
// WakeSystem is opt-in: when set, launchd will wake the system on RunAtLoad
// triggers. With KeepAlive=true this keeps the daemon ready post-wake, but
// does not by itself wake the system to fire a particular routine — that
// would require per-routine StartCalendarInterval entries.
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.Binary}}</string>
        <string>routined</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
{{- if .WakeSystem }}
    <key>WakeSystem</key>
    <true/>
{{- end }}
    <key>StandardOutPath</key>
    <string>{{.LogPath}}</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}</string>
    <key>WorkingDirectory</key>
    <string>{{.Home}}</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin</string>
        <key>HOME</key>
        <string>{{.Home}}</string>
    </dict>
</dict>
</plist>
`

type launchdService struct {
	home      string
	uid       int
	plistPath string
	logPath   string
}

// New returns the macOS launchd service implementation.
func New() Service {
	home, _ := os.UserHomeDir()
	return &launchdService{
		home:      home,
		uid:       os.Getuid(),
		plistPath: filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"),
		logPath:   filepath.Join(home, "Library", "Logs", "squad", "routined.log"),
	}
}

func (s *launchdService) Install(daemonBinary string, opts InstallOptions) error {
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

	// Ensure log + plist directories exist.
	if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.plistPath), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	rendered, err := s.renderPlist(abs, opts.WakeSystem)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.plistPath, rendered, 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Idempotency: if a previous instance is loaded, unload it before
	// re-bootstrapping the new content. bootout exits non-zero when the
	// service isn't loaded — that's fine, we ignore "not loaded" errors.
	_ = s.runLaunchctl("bootout", s.targetDomain())

	if out, err := s.runLaunchctlOutput("bootstrap", s.targetSystem(), s.plistPath); err != nil {
		return fmt.Errorf("bootstrap failed: %w (output: %s)", err, out)
	}
	return nil
}

func (s *launchdService) Uninstall() error {
	// bootout first; tolerate "not loaded" exit.
	_ = s.runLaunchctl("bootout", s.targetDomain())
	if err := os.Remove(s.plistPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

func (s *launchdService) Status() (Status, error) {
	binary := s.daemonBinaryFromPlist()
	st := Status{
		ServicePath:  s.plistPath,
		LogPath:      s.logPath,
		DaemonBinary: binary,
		State:        StateNotInstalled,
	}
	if _, err := os.Stat(s.plistPath); errors.Is(err, fs.ErrNotExist) {
		return st, nil
	} else if err != nil {
		return st, err
	}
	out, err := s.runLaunchctlOutput("print", s.targetDomain())
	if err != nil {
		// `launchctl print` exits non-zero when the service isn't loaded.
		st.State = StateInstalledStopped
		return st, nil
	}
	// `state = running` while the daemon is alive; `state = waiting` between
	// fires. The plist is loaded into launchd either way.
	if bytes.Contains(out, []byte("state = running")) || bytes.Contains(out, []byte("state = waiting")) {
		st.State = StateInstalledRunning
	} else {
		st.State = StateInstalledStopped
	}
	return st, nil
}

// TailLogs streams the daemon log file (launchd's StandardOutPath target).
func (s *launchdService) TailLogs(ctx context.Context, w io.Writer, follow bool) error {
	return tailFile(ctx, w, s.logPath, follow)
}

// renderPlist returns the bytes of a fully rendered LaunchAgent plist for
// daemonBinary. Extracted from Install so tests can assert on the artifact
// without shelling to launchctl.
func (s *launchdService) renderPlist(daemonBinary string, wakeSystem bool) ([]byte, error) {
	tmpl, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse plist template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]any{
		"Label":      launchdLabel,
		"Binary":     daemonBinary,
		"LogPath":    s.logPath,
		"Home":       s.home,
		"WakeSystem": wakeSystem,
	}); err != nil {
		return nil, fmt.Errorf("render plist: %w", err)
	}
	return buf.Bytes(), nil
}

// targetSystem is the user-domain target launchctl needs for `bootstrap`:
// `gui/<uid>`.
func (s *launchdService) targetSystem() string {
	return "gui/" + strconv.Itoa(s.uid)
}

// targetDomain is the per-service launchd path used by `bootout` and `print`:
// `gui/<uid>/<label>`.
func (s *launchdService) targetDomain() string {
	return "gui/" + strconv.Itoa(s.uid) + "/" + launchdLabel
}

func (s *launchdService) runLaunchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	return cmd.Run()
}

func (s *launchdService) runLaunchctlOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("launchctl", args...)
	return cmd.CombinedOutput()
}

// daemonBinaryFromPlist parses the installed plist for the ProgramArguments
// entry. Returns an empty string on any error so the caller can still report
// useful Status info. We do not depend on an XML parser since the plist
// content is fully controlled by us and a substring match is sufficient.
func (s *launchdService) daemonBinaryFromPlist() string {
	data, err := os.ReadFile(s.plistPath)
	if err != nil {
		return ""
	}
	const startTag = "<key>ProgramArguments</key>"
	idx := bytes.Index(data, []byte(startTag))
	if idx < 0 {
		return ""
	}
	tail := data[idx:]
	openTag := []byte("<string>")
	closeTag := []byte("</string>")
	start := bytes.Index(tail, openTag)
	if start < 0 {
		return ""
	}
	tail = tail[start+len(openTag):]
	end := bytes.Index(tail, closeTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(string(tail[:end]))
}
