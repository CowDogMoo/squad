//go:build linux

package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise the platform-neutral half of the systemd installer
// — the parts that don't shell out to systemctl/loginctl/journalctl.

func TestSystemdNewHonorsXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	svc := New()
	if svc == nil {
		t.Fatal("New() returned nil")
	}
	st, err := svc.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	want := filepath.Join(dir, "systemd", "user", unitName)
	if st.ServicePath != want {
		t.Errorf("ServicePath: got %q, want %q", st.ServicePath, want)
	}
	if !strings.Contains(st.LogPath, "journalctl") {
		t.Errorf("LogPath should describe journalctl, got %q", st.LogPath)
	}
}

func TestSystemdRenderUnitEmbedsBinaryAndHome(t *testing.T) {
	s := &systemdService{home: "/home/u"}
	out, err := s.renderUnit("/usr/local/bin/squad")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ExecStart=/usr/local/bin/squad routined",
		"Environment=HOME=/home/u",
		"Restart=always",
		"WantedBy=default.target",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("rendered unit missing %q\n%s", want, string(out))
		}
	}
}

func TestSystemdDaemonBinaryFromUnitParsesExecStart(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "x.service")
	body := "[Unit]\n[Service]\nExecStart=/opt/squad/bin routined --log-file foo\n[Install]\n"
	if err := writeFileCustom(t, unitPath, body); err != nil {
		t.Fatal(err)
	}
	s := &systemdService{unitPath: unitPath}
	if got := s.daemonBinaryFromUnit(); got != "/opt/squad/bin" {
		t.Errorf("got %q", got)
	}
}

func TestSystemdDaemonBinaryFromMissingUnit(t *testing.T) {
	s := &systemdService{unitPath: "/nonexistent/squad.service"}
	if got := s.daemonBinaryFromUnit(); got != "" {
		t.Errorf("expected empty for missing unit, got %q", got)
	}
}

func TestSystemdDaemonBinaryFromUnitWithoutExecStart(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "x.service")
	body := "[Unit]\nDescription=hi\n[Install]\nWantedBy=default.target\n"
	if err := writeFileCustom(t, unitPath, body); err != nil {
		t.Fatal(err)
	}
	s := &systemdService{unitPath: unitPath}
	if got := s.daemonBinaryFromUnit(); got != "" {
		t.Errorf("expected empty when ExecStart missing, got %q", got)
	}
}

// Status() reports the not-installed state cleanly when no unit is on disk.
// This avoids invoking systemctl (no unit -> early return).
func TestSystemdStatusNotInstalled(t *testing.T) {
	t.Parallel()
	s := &systemdService{unitPath: filepath.Join(t.TempDir(), "absent.service")}
	st, err := s.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateNotInstalled {
		t.Errorf("got %v", st.State)
	}
	if st.ServicePath == "" {
		t.Error("ServicePath should be populated")
	}
}

// TailLogs hits an exec error fast when journalctl isn't installed (or when
// the unit doesn't exist). We use a 100ms context so the test exits
// quickly.
func TestSystemdTailLogsReturnsCleanlyOnCtxCancel(t *testing.T) {
	t.Parallel()
	s := &systemdService{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	if err := s.TailLogs(ctx, discardWriter{}, false); err != nil {
		t.Errorf("expected nil after ctx cancel, got %v", err)
	}
}

// writeFileCustom is a tiny helper that writes a file and fails the test on
// any error.
func writeFileCustom(t *testing.T, path, body string) error {
	t.Helper()
	return os.WriteFile(path, []byte(body), 0o644)
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
