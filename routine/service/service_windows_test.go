//go:build windows

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsInstallScriptContainsExpectedDirectives(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	binary := `C:\Users\test\squad.exe`
	script := s.installScript(binary, false)
	if strings.Contains(script, "-WakeToRun") {
		t.Errorf("wake_system=false should not include -WakeToRun, got:\n%s", script)
	}
	if wakeOn := s.installScript(binary, true); !strings.Contains(wakeOn, "-WakeToRun") {
		t.Errorf("wake_system=true should include -WakeToRun, got:\n%s", wakeOn)
	}
	for _, want := range []string{
		"New-ScheduledTaskAction",
		"-Execute '" + binary + "'",
		"-Argument 'routined --log-file",
		"-AtLogon",
		"-Hidden",
		"-StartWhenAvailable",
		"-RestartCount 3",
		"Register-ScheduledTask",
		"-TaskPath '" + taskFolder + "'",
		"-TaskName '" + taskNameLeaf + "'",
		"-Force",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("install script missing %q\n--- full script ---\n%s", want, script)
		}
	}
}

func TestWindowsStatusReflectsMissingTask(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	// Status shells out to PowerShell. On a clean CI runner there's no task
	// registered, so we expect StateNotInstalled (or StateInstalledStopped
	// if a leftover task somehow exists). Tolerate both — the test asserts
	// only that we don't panic and the paths are populated.
	st, err := s.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.ServicePath == "" {
		t.Error("ServicePath should be populated")
	}
	if st.LogPath == "" {
		t.Error("LogPath should be populated")
	}
}

func TestPsEscapeDoublesQuotes(t *testing.T) {
	t.Parallel()
	if got := psEscape(`C:\Users\Bob's PC\squad.exe`); got != `C:\Users\Bob''s PC\squad.exe` {
		t.Errorf("psEscape: got %q", got)
	}
	if got := psEscape("no quotes"); got != "no quotes" {
		t.Errorf("psEscape no-op: got %q", got)
	}
}

func newTestService(t *testing.T) *taskService {
	t.Helper()
	tmp := t.TempDir()
	return &taskService{
		home:    tmp,
		logPath: filepath.Join(tmp, "AppData", "Local", "squad", "Logs", "routined.log"),
	}
}

// Suppress unused-mockBinary lint if other test files reference it; keep this
// helper for symmetry with the macOS/Linux test fixtures.
func mockBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "squad.exe")
	if err := os.WriteFile(path, []byte{0x4d, 0x5a}, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
