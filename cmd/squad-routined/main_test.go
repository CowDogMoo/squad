package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// resetFlagsForTest restores the package flag.CommandLine to a fresh state so
// run() can be called multiple times without "flag redefined" panics.
func resetFlagsForTest() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
}

func TestRunExitsCleanlyWithNoRoutines(t *testing.T) {
	// Point XDG to a fresh tempdir so the daemon loads zero routines.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, ".local", "state"))
	t.Setenv("HOME", tmp)

	// We need the run() function to return promptly even though it blocks on
	// signal.NotifyContext. The trick: send ourselves a SIGINT shortly after
	// run() starts. signal.NotifyContext converts it into a ctx cancel which
	// daemon.Run respects.
	resetFlagsForTest()
	os.Args = []string{"squad-routined"}

	done := make(chan int, 1)
	go func() {
		done <- run()
	}()

	// Give the daemon a moment to start up, then signal interrupt.
	time.Sleep(200 * time.Millisecond)
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}

	select {
	case rc := <-done:
		if rc != 0 {
			t.Errorf("run() returned %d, expected 0", rc)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return within 5s after SIGINT")
	}
}

func TestRunBadConfigExitsNonZero(t *testing.T) {
	// Point XDG at a malformed config file so config.Load fails.
	if os.Getuid() == 0 {
		t.Skip("can't make /proc unreadable as root")
	}
	resetFlagsForTest()
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, ".config", "squad")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(cfgDir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte(": invalid: yaml: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, ".local", "state"))
	t.Setenv("HOME", tmp)
	os.Args = []string{"squad-routined"}
	rc := run()
	if rc != 1 {
		t.Errorf("expected rc=1 for bad config, got %d", rc)
	}
}

func TestRunDaemonErrorExitsNonZero(t *testing.T) {
	// Make daemon.Run fail by pointing XDG_STATE_HOME at a file so Watch
	// cannot create its directory.
	resetFlagsForTest()
	tmp := t.TempDir()
	stateFile := filepath.Join(tmp, "state-file")
	if err := os.WriteFile(stateFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_STATE_HOME", stateFile)
	t.Setenv("HOME", tmp)
	os.Args = []string{"squad-routined"}
	rc := run()
	if rc != 1 {
		t.Errorf("expected rc=1 when daemon.Run fails, got %d", rc)
	}
}

func TestRunRedirectStdioFailureExitsNonZero(t *testing.T) {
	// Passing an unwriteable log path makes RedirectStdio fail; run() should
	// return 1 without ever invoking daemon.Run.
	resetFlagsForTest()
	os.Args = []string{"squad-routined", "--log-file", "/proc/cannot-write-here"}
	// On macOS /proc doesn't exist either, so MkdirAll will fail.
	rc := run()
	if rc != 1 {
		t.Errorf("expected rc=1 for redirect failure, got %d", rc)
	}
	// Restore stdout/stderr if they got partially swapped.
	os.Stdout = os.NewFile(uintptr(1), "/dev/stdout")
	os.Stderr = os.NewFile(uintptr(2), "/dev/stderr")
}
