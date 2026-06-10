//go:build darwin

package service

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// New tests below mutate PATH to inject a fake launchctl. Subtests share PATH — do not add t.Parallel().

func TestLaunchdInstallAndUninstall_HappyPath(t *testing.T) {
	// Tests mutate PATH — do not add t.Parallel().
	s := newTestService(t)
	binary := mockBinary(t)

	// Fake launchctl: bootstrap succeeds, bootout fails (ignored), print unused.
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  bootstrap) echo bootstrapped; exit 0;;\n" +
		"  bootout) echo not-loaded; exit 1;;\n" +
		"  print) echo state = running; exit 0;;\n" +
		"  *) exit 0;;\n" +
		"esac\n"
	lc := filepath.Join(dir, "launchctl")
	if err := os.WriteFile(lc, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Prepend fake to PATH so exec.Command finds it first.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := s.Install(binary, InstallOptions{WakeSystem: true}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// Plist should exist after install.
	if _, err := os.Stat(s.plistPath); err != nil {
		t.Fatalf("plist not written: %v", err)
	}

	// Now uninstall; plist should be removed even if bootout fails.
	if err := s.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(s.plistPath); !os.IsNotExist(err) {
		t.Fatalf("expected plist to be removed, stat err=%v", err)
	}
}

func TestLaunchdStatus_StatesFromLaunchctl(t *testing.T) {
	// Tests mutate PATH — do not add t.Parallel().
	cases := []struct {
		name string
		out  string
		want State
	}{
		{"running", "state = running", StateInstalledRunning},
		{"waiting", "state = waiting", StateInstalledRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestService(t)
			binary := mockBinary(t)
			if err := writePlist(s.plistPath, binary, s.logPath, s.home, false); err != nil {
				t.Fatal(err)
			}

			dir := t.TempDir()
			script := "#!/bin/sh\n" +
				"case \"$1\" in\n" +
				"  print) echo " + tc.out + "; exit 0;;\n" +
				"  *) exit 0;;\n" +
				"esac\n"
			lc := filepath.Join(dir, "launchctl")
			if err := os.WriteFile(lc, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

			st, err := s.Status()
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if st.State != tc.want {
				t.Fatalf("state = %v, want %v", st.State, tc.want)
			}
		})
	}
}

// TestTailFileNotFound verifies TailLogs returns an error when the log file
// does not exist.
func TestTailFileNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var buf bytes.Buffer
	svc := newTestService(t)
	svc.logPath = filepath.Join(t.TempDir(), "nonexistent.log")
	err := svc.TailLogs(ctx, &buf, false)
	if err == nil {
		t.Fatal("expected error for missing log file, got nil")
	}
	if !strings.Contains(err.Error(), "daemon log not found") {
		t.Errorf("error %q does not contain 'daemon log not found'", err.Error())
	}
}

func TestTailFileNoFollow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(logPath, []byte("hello log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t)
	svc.logPath = logPath
	var buf bytes.Buffer
	ctx := context.Background()
	if err := svc.TailLogs(ctx, &buf, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "hello log") {
		t.Errorf("output %q does not contain 'hello log'", buf.String())
	}
}

func TestUninstallIdempotent(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	// plistPath does not exist — Uninstall should succeed (ErrNotExist tolerated).
	if err := svc.Uninstall(); err != nil {
		t.Fatalf("Uninstall on missing plist returned error: %v", err)
	}
}

func TestUninstallRemovesPlist(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	// Write a plist so Remove has something to delete.
	if err := os.MkdirAll(filepath.Dir(svc.plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(svc.plistPath, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.Uninstall(); err != nil {
		t.Fatalf("Uninstall returned error: %v", err)
	}
	if _, err := os.Stat(svc.plistPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("plist still exists after Uninstall")
	}
}

func TestStatusNotInstalled(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	// plistPath does not exist → StateNotInstalled.
	st, err := svc.Status()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.State != StateNotInstalled {
		t.Errorf("State = %v, want StateNotInstalled", st.State)
	}
}

func TestInstallRejectsNonExistentBinary(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	err := svc.Install(filepath.Join(t.TempDir(), "no-such-binary"), InstallOptions{})
	if err == nil {
		t.Fatal("expected error for non-existent binary, got nil")
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Errorf("error %q does not mention 'not executable'", err.Error())
	}
}

func TestTailFileFollow(t *testing.T) {
	// Exercises the follow=true branch: writes initial content, then cancels
	// the context to trigger the ctx.Done() return path.
	dir := t.TempDir()
	logPath := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(logPath, []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t)
	svc.logPath = logPath
	var buf strings.Builder
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay so the follow loop exits.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if err := svc.TailLogs(ctx, &buf, true); err != nil {
		t.Fatalf("TailLogs(follow=true): %v", err)
	}
	if !strings.Contains(buf.String(), "line1") {
		t.Errorf("output %q missing 'line1'", buf.String())
	}
}

func TestRenderPlistEmptyBinary(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	// renderPlist with an empty binary should still succeed (template
	// substitution does not validate the binary path).
	out, err := svc.renderPlist("", false)
	if err != nil {
		t.Fatalf("renderPlist: %v", err)
	}
	if len(out) == 0 {
		t.Error("renderPlist returned empty bytes")
	}
}

func TestDaemonBinaryFromPlistMissingFile(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	// plistPath does not exist — should return empty string, not panic.
	got := svc.daemonBinaryFromPlist()
	if got != "" {
		t.Errorf("daemonBinaryFromPlist() = %q, want empty string", got)
	}
}

func TestDaemonBinaryFromPlistMalformed(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	if err := os.MkdirAll(filepath.Dir(svc.plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a plist without ProgramArguments — should return "".
	if err := os.WriteFile(svc.plistPath, []byte("<plist><dict></dict></plist>"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := svc.daemonBinaryFromPlist()
	if got != "" {
		t.Errorf("daemonBinaryFromPlist() = %q, want empty string", got)
	}
}

func TestInstallWritesPlistAndBootstraps(t *testing.T) {
	// Uses a fake launchctl that always succeeds.
	svc := newTestService(t)
	binary := mockBinary(t)
	dir := t.TempDir()
	script := "#!/bin/sh\nexit 0\n"
	lc := filepath.Join(dir, "launchctl")
	if err := os.WriteFile(lc, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := svc.Install(binary, InstallOptions{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := os.Stat(svc.plistPath); err != nil {
		t.Fatalf("plist not written: %v", err)
	}
}

func TestInstallBootstrapFails(t *testing.T) {
	// Uses a fake launchctl whose bootstrap subcommand fails.
	svc := newTestService(t)
	binary := mockBinary(t)
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n  bootstrap) echo 'bootstrap error' >&2; exit 1;;\n  *) exit 0;;\nesac\n"
	lc := filepath.Join(dir, "launchctl")
	if err := os.WriteFile(lc, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	err := svc.Install(binary, InstallOptions{})
	if err == nil {
		t.Fatal("expected error when bootstrap fails, got nil")
	}
	if !strings.Contains(err.Error(), "bootstrap failed") {
		t.Errorf("error %q does not contain 'bootstrap failed'", err.Error())
	}
}

func TestRenderPlistContainsAllFields(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	binary := mockBinary(t)
	out, err := svc.renderPlist(binary, false)
	if err != nil {
		t.Fatalf("renderPlist: %v", err)
	}
	content := string(out)
	for _, want := range []string{
		launchdLabel,
		binary,
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		"routined",
		svc.logPath,
		svc.home,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("plist missing %q", want)
		}
	}
}

func TestUninstallBootoutAndRemove(t *testing.T) {
	// Fake launchctl that always succeeds for bootout.
	svc := newTestService(t)
	dir := t.TempDir()
	script := "#!/bin/sh\nexit 0\n"
	lc := filepath.Join(dir, "launchctl")
	if err := os.WriteFile(lc, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Write a plist so Remove has something to delete.
	if err := os.MkdirAll(filepath.Dir(svc.plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(svc.plistPath, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(svc.plistPath); !errors.Is(err, fs.ErrNotExist) {
		t.Error("plist should be removed after Uninstall")
	}
}

func TestStatusStatError(t *testing.T) {
	// Make plistPath a non-ErrNotExist error by pointing it at a path
	// inside a non-existent parent that is itself a file.
	svc := newTestService(t)
	// Create a file where the plist directory should be.
	plistParent := filepath.Dir(svc.plistPath)
	if err := os.MkdirAll(filepath.Dir(plistParent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plistParent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Now plistPath = <file>/something.plist — Stat returns a non-ErrNotExist error.
	_, err := svc.Status()
	if err == nil {
		t.Fatal("expected error when plist parent is a file, got nil")
	}
}

func TestInstallEmptyBinaryReturnsError(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	err := svc.Install("", InstallOptions{})
	if err == nil {
		t.Fatal("expected error for empty binary, got nil")
	}
	if !strings.Contains(err.Error(), "daemon binary path required") {
		t.Errorf("error %q does not contain 'daemon binary path required'", err.Error())
	}
}

func TestRenderPlistWakeSystemTrue(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	out, err := svc.renderPlist("/usr/local/bin/squad", true)
	if err != nil {
		t.Fatalf("renderPlist: %v", err)
	}
	if !strings.Contains(string(out), "<key>WakeSystem</key>") {
		t.Errorf("plist missing WakeSystem key:\n%s", out)
	}
}

func TestTailFileFollowNewBytes(t *testing.T) {
	// Exercises the follow=true branch where new bytes arrive after the
	// initial read.
	dir := t.TempDir()
	logPath := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(logPath, []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t)
	svc.logPath = logPath
	var buf strings.Builder
	ctx, cancel := context.WithCancel(context.Background())
	// Append a new line after a short delay, then cancel.
	go func() {
		time.Sleep(30 * time.Millisecond)
		f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
		if f != nil {
			_, _ = f.WriteString("appended\n")
			_ = f.Close()
		}
		time.Sleep(250 * time.Millisecond)
		cancel()
	}()
	if err := svc.TailLogs(ctx, &buf, true); err != nil {
		t.Fatalf("TailLogs: %v", err)
	}
	if !strings.Contains(buf.String(), "initial") {
		t.Errorf("output missing 'initial': %q", buf.String())
	}
}

func TestInstallWritesPlistFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod ineffective as root")
	}
	svc := newTestService(t)
	binary := mockBinary(t)
	// Create plist dir as read-only so WriteFile fails.
	plistDir := filepath.Dir(svc.plistPath)
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(plistDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(plistDir, 0o755) })
	// Also create log dir so we don't fail there first.
	if err := os.MkdirAll(filepath.Dir(svc.logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	err := svc.Install(binary, InstallOptions{})
	if err == nil {
		t.Fatal("expected error when plist write fails, got nil")
	}
}

func TestRenderPlistContainsBinaryAndHome(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	binary := mockBinary(t)
	data, err := s.renderPlist(binary, false)
	if err != nil {
		t.Fatalf("renderPlist: %v", err)
	}
	rendered := string(data)
	if !strings.Contains(rendered, binary) {
		t.Errorf("plist missing binary %q:\n%s", binary, rendered)
	}
	if !strings.Contains(rendered, s.home) {
		t.Errorf("plist missing home %q:\n%s", s.home, rendered)
	}
	if !strings.Contains(rendered, launchdLabel) {
		t.Errorf("plist missing label %q:\n%s", launchdLabel, rendered)
	}
}

func TestUninstallWhenPlistMissing(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	// plist never created — Uninstall should succeed (ErrNotExist is tolerated).
	if err := s.Uninstall(); err != nil {
		t.Fatalf("Uninstall with no plist: %v", err)
	}
}

func TestStatusPlistExistsButLaunchctlFails(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	binary := mockBinary(t)
	// Write a plist so os.Stat succeeds.
	if err := os.MkdirAll(filepath.Dir(s.plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	rendered, err := s.renderPlist(binary, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.plistPath, rendered, 0o644); err != nil {
		t.Fatal(err)
	}
	// launchctl print will fail (not a real launchd env) → StateInstalledStopped.
	st, err := s.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateInstalledStopped {
		t.Errorf("expected StateInstalledStopped, got %v", st.State)
	}
	if st.DaemonBinary != binary {
		t.Errorf("DaemonBinary = %q, want %q", st.DaemonBinary, binary)
	}
}

func TestDaemonBinaryFromPlistNoStartTag(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	// Write a plist without ProgramArguments key.
	if err := os.MkdirAll(filepath.Dir(s.plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.plistPath, []byte("<plist><dict></dict></plist>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.daemonBinaryFromPlist(); got != "" {
		t.Errorf("expected empty string for plist without ProgramArguments, got %q", got)
	}
}

func TestDaemonBinaryFromPlistMissingCloseTag(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	if err := os.MkdirAll(filepath.Dir(s.plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Has ProgramArguments key and opening <string> but no closing </string>.
	content := "<key>ProgramArguments</key><array><string>/usr/bin/squad"
	if err := os.WriteFile(s.plistPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.daemonBinaryFromPlist(); got != "" {
		t.Errorf("expected empty string for truncated plist, got %q", got)
	}
}

func TestInstallRejectsEmptyBinaryExplicit(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	err := s.Install("", InstallOptions{})
	if err == nil {
		t.Fatal("expected error for empty binary")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("error %q should mention 'binary'", err.Error())
	}
}

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
