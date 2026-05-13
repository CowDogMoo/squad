//go:build darwin

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchdInstallRejectsMissingBinary(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	if err := s.Install("/nonexistent/squad", InstallOptions{}); err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestPlistWakeSystemToggle(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	binary := mockBinary(t)
	off, err := s.renderPlist(binary, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(off), "<key>WakeSystem</key>") {
		t.Errorf("wake_system=false should omit WakeSystem key, got:\n%s", off)
	}
	on, err := s.renderPlist(binary, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(on), "<key>WakeSystem</key>") {
		t.Errorf("wake_system=true should include WakeSystem key, got:\n%s", on)
	}
}

func TestStateString(t *testing.T) {
	t.Parallel()
	cases := map[State]string{
		StateNotInstalled:     "not installed",
		StateInstalledStopped: "installed (stopped)",
		StateInstalledRunning: "installed (running)",
		State(99):             "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("State(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestNewReturnsServiceWithPopulatedStatusPaths(t *testing.T) {
	t.Parallel()
	svc := New()
	if svc == nil {
		t.Fatal("New() returned nil")
	}
	st, err := svc.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.ServicePath == "" || st.LogPath == "" {
		t.Errorf("paths not populated: %+v", st)
	}
}

func TestTargetDomainEmbedsUIDAndLabel(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	if got := s.targetSystem(); got == "" {
		t.Error("targetSystem empty")
	}
	got := s.targetDomain()
	if !strings.Contains(got, launchdLabel) {
		t.Errorf("targetDomain missing label: %q", got)
	}
}

func TestLaunchdTailLogsReadsExistingFile(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.logPath, []byte("first-line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf := &strings.Builder{}
	if err := s.TailLogs(t.Context(), buf, false); err != nil {
		t.Fatalf("TailLogs: %v", err)
	}
	if !strings.Contains(buf.String(), "first-line") {
		t.Errorf("got %q", buf.String())
	}
}

func TestLaunchdStatusReturnsStoppedForInstalledPlist(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	binary := mockBinary(t)
	// Pre-render the plist on disk without invoking launchctl.
	if err := writePlist(s.plistPath, binary, s.logPath, s.home, false); err != nil {
		t.Fatal(err)
	}
	st, err := s.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	// The plist file exists; launchctl print will likely not find a loaded
	// service for our test label, so we expect StateInstalledStopped.
	if st.State == StateNotInstalled {
		t.Errorf("expected stopped/installed, got %v", st.State)
	}
	if st.DaemonBinary != binary {
		t.Errorf("daemon binary parsed: got %q want %q", st.DaemonBinary, binary)
	}
}

func TestLaunchdStatusReflectsMissingPlist(t *testing.T) {
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

func TestDaemonBinaryFromPlist(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	binary := mockBinary(t)

	if err := writePlist(s.plistPath, binary, s.logPath, s.home, false); err != nil {
		t.Fatal(err)
	}
	got := s.daemonBinaryFromPlist()
	if got != binary {
		t.Errorf("got %q, want %q", got, binary)
	}
}

func TestPlistContainsExpectedFields(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	binary := mockBinary(t)
	if err := writePlist(s.plistPath, binary, s.logPath, s.home, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.plistPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		launchdLabel,
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"<key>ProgramArguments</key>",
		"routined",
		binary,
		s.logPath,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("plist missing %q\nfull content:\n%s", want, content)
		}
	}
}

// newTestService builds a launchdService rooted at a temp dir so tests never
// touch the user's real ~/Library/LaunchAgents.
func newTestService(t *testing.T) *launchdService {
	t.Helper()
	tmp := t.TempDir()
	return &launchdService{
		home:      tmp,
		uid:       os.Getuid(),
		plistPath: filepath.Join(tmp, "Library", "LaunchAgents", launchdLabel+".plist"),
		logPath:   filepath.Join(tmp, "Library", "Logs", "squad", "routined.log"),
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

// writePlist exercises the Install template path without invoking launchctl,
// so we can assert on the rendered file in unit tests.
func writePlist(plistPath, binary, logPath, home string, wakeSystem bool) error {
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	// Render via the same template Install uses by reusing the implementation
	// internals — call Install would shell to launchctl, so we replicate the
	// file-creation half here.
	s := &launchdService{home: home, plistPath: plistPath, logPath: logPath}
	tmpl, err := s.renderPlist(binary, wakeSystem)
	if err != nil {
		return err
	}
	return os.WriteFile(plistPath, tmpl, 0o644)
}
