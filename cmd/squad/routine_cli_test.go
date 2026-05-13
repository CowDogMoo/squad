package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runRoutineCmd builds the routine subtree and executes it with args. It
// returns combined stdout output and any error. Skips real OS-service
// installation via SQUAD_SKIP_SERVICE_INSTALL.
func runRoutineCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("SQUAD_SKIP_SERVICE_INSTALL", "1")
	cmd := newRoutineCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return buf.String(), err
}

func TestRoutineListEmpty(t *testing.T) {
	setupXDG(t)
	out, err := runRoutineCmd(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "No routines") {
		t.Errorf("expected empty marker, got:\n%s", out)
	}
}

func TestRoutineCreateListShowDelete(t *testing.T) {
	setupXDG(t)
	if out, err := runRoutineCmd(t,
		"create", "nightly",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
	); err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	} else if !strings.Contains(out, "Created global:nightly") {
		t.Errorf("create banner missing:\n%s", out)
	}

	listOut, err := runRoutineCmd(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut, "nightly") || !strings.Contains(listOut, "@daily") {
		t.Errorf("list missing routine:\n%s", listOut)
	}

	showOut, err := runRoutineCmd(t, "show", "nightly")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(showOut, "Schedule:    @daily") {
		t.Errorf("show output mismatch:\n%s", showOut)
	}

	if _, err := runRoutineCmd(t, "delete", "nightly"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	finalList, _ := runRoutineCmd(t, "list")
	if strings.Contains(finalList, "nightly") {
		t.Errorf("routine still listed after delete:\n%s", finalList)
	}
}

func TestRoutineEnableDisable(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t,
		"create", "toggle",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoutineCmd(t, "disable", "toggle"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	out, _ := runRoutineCmd(t, "show", "toggle")
	if !strings.Contains(out, "Enabled:     false") {
		t.Errorf("expected disabled, got:\n%s", out)
	}
	if _, err := runRoutineCmd(t, "enable", "toggle"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	out, _ = runRoutineCmd(t, "show", "toggle")
	if !strings.Contains(out, "Enabled:     true") {
		t.Errorf("expected enabled, got:\n%s", out)
	}
}

func TestRoutineWatchUnwatchRoots(t *testing.T) {
	setupXDG(t)
	repo := t.TempDir()
	if out, err := runRoutineCmd(t, "watch", repo); err != nil {
		t.Fatalf("watch: %v\n%s", err, out)
	}
	rootsOut, err := runRoutineCmd(t, "roots")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootsOut, repo) {
		t.Errorf("watched root not listed:\n%s", rootsOut)
	}
	// Idempotent re-watch.
	if out, err := runRoutineCmd(t, "watch", repo); err != nil {
		t.Fatalf("rewatch: %v", err)
	} else if !strings.Contains(out, "Already watching") {
		t.Errorf("expected idempotency message, got:\n%s", out)
	}
	// Unwatch.
	if _, err := runRoutineCmd(t, "unwatch", repo); err != nil {
		t.Fatalf("unwatch: %v", err)
	}
	finalRoots, _ := runRoutineCmd(t, "roots")
	if strings.Contains(finalRoots, repo) {
		t.Errorf("root still listed after unwatch:\n%s", finalRoots)
	}
}

func TestRoutineHistoryEmpty(t *testing.T) {
	setupXDG(t)
	workDir := t.TempDir()
	if _, err := runRoutineCmd(t,
		"create", "hist",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", workDir,
	); err != nil {
		t.Fatal(err)
	}
	out, err := runRoutineCmd(t, "history", "hist")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(out, "No sessions found") {
		t.Errorf("expected empty marker:\n%s", out)
	}
}

func TestRoutineCreateRejectsBadID(t *testing.T) {
	setupXDG(t)
	_, err := runRoutineCmd(t,
		"create", "BadCase",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
	)
	if err == nil {
		t.Error("expected validation error for invalid slug")
	}
}

func TestRoutineCreateRejectsBadSchedule(t *testing.T) {
	setupXDG(t)
	_, err := runRoutineCmd(t,
		"create", "x",
		"--agent", "go-review",
		"--schedule", "not a cron",
		"--working-dir", t.TempDir(),
	)
	if err == nil {
		t.Error("expected validation error for invalid schedule")
	}
}

func TestRoutineCreatePerRepoAutoWatches(t *testing.T) {
	setupXDG(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".squad"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	out, err := runRoutineCmd(t,
		"create", "audit",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--scope", "repo",
	)
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if !strings.Contains(out, "repo:audit") {
		t.Errorf("expected repo qualifier:\n%s", out)
	}
	// Roots list should now contain this repo.
	rootsOut, _ := runRoutineCmd(t, "roots")
	if !strings.Contains(rootsOut, repo) {
		t.Errorf("repo not auto-watched:\n%s", rootsOut)
	}
}

func TestRoutineDoctorReportsNotInstalledInTestMode(t *testing.T) {
	setupXDG(t)
	// SQUAD_SKIP_SERVICE_INSTALL prevents the test from registering anything
	// with launchd/systemd/Task Scheduler; doctor only reads state.
	out, err := runRoutineCmd(t, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	// We don't assert on exact state ("not installed" / "installed (stopped)")
	// because the host running this test may already have a real install. We
	// do assert the section headings come through.
	for _, want := range []string{"Service:", "Manifest:", "Daemon:", "Logs:", "Routines:"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q\n--- output ---\n%s", want, out)
			break
		}
	}
}

func TestDaemonBinaryPathReturnsExecutable(t *testing.T) {
	t.Parallel()
	got, err := daemonBinaryPath()
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Error("expected non-empty path")
	}
}

func TestRoutineRunNowMissingRoutine(t *testing.T) {
	setupXDG(t)
	// run-now without an existing routine should error from resolveExistingRef.
	_, err := runRoutineCmd(t, "run-now", "ghost")
	if err == nil {
		t.Error("expected error for missing routine")
	}
}

// helper guard so unused-import warnings don't trip in environments where
// cobra isn't transitively needed.
var _ = (*cobra.Command)(nil)
