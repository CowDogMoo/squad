//go:build linux

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdRenderUnitContainsExpectedFields(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	binary := mockBinary(t)
	rendered, err := s.renderUnit(binary)
	if err != nil {
		t.Fatal(err)
	}
	content := string(rendered)
	for _, want := range []string{
		"Description=Squad routines daemon",
		"ExecStart=" + binary + " routined",
		"Restart=always",
		"WantedBy=default.target",
		"Environment=HOME=" + s.home,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("unit missing %q\n--- full content ---\n%s", want, content)
		}
	}
}

func TestSystemdStatusReflectsMissingUnit(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	st, err := s.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateNotInstalled {
		t.Errorf("expected not installed, got %v", st.State)
	}
	if st.ServicePath == "" {
		t.Error("ServicePath should be populated even when not installed")
	}
}

func TestSystemdInstallRejectsMissingBinary(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	if err := s.Install("/nonexistent/squad", InstallOptions{}); err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestDaemonBinaryFromUnit(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	binary := mockBinary(t)
	rendered, err := s.renderUnit(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(s.unitPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.unitPath, rendered, 0o644); err != nil {
		t.Fatal(err)
	}
	got := s.daemonBinaryFromUnit()
	if got != binary {
		t.Errorf("got %q, want %q", got, binary)
	}
}

// newTestService builds a systemdService rooted at a temp dir so tests never
// touch the user's real ~/.config/systemd/user/ tree.
func newTestService(t *testing.T) *systemdService {
	t.Helper()
	tmp := t.TempDir()
	return &systemdService{
		home:     tmp,
		username: "testuser",
		unitPath: filepath.Join(tmp, ".config", "systemd", "user", unitName),
		logPath:  "journalctl --user -u " + unitName,
	}
}

func mockBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "squad")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
