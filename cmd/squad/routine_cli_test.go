package main

import (
	"bytes"
	"context"
	"fmt"
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

func TestRoutineHistoryWithSessions(t *testing.T) {
	setupXDG(t)
	workDir := t.TempDir()
	sd := filepath.Join(workDir, ".squad", "sessions")
	if err := os.MkdirAll(filepath.Join(sd, "S1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sd, "S2"), 0o755); err != nil {
		t.Fatal(err)
	}
	mkMeta := func(id, agent, routineID string, cost float64) {
		t.Helper()
		body := `{"session_id":"` + id + `","agent":"` + agent + `","routine_id":"` + routineID + `","created":"2026-05-12T02:00:00Z","status":"completed","cost":` + fmtFloat(cost) + `,"iterations":2}`
		if err := os.WriteFile(filepath.Join(sd, id, "meta.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkMeta("S1", "go-review", "global:hist", 0.123)
	mkMeta("S2", "go-review", "global:other", 0.456)

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
	if !strings.Contains(out, "S1") {
		t.Errorf("missing S1 in history (matched on routine_id):\n%s", out)
	}
	if strings.Contains(out, "S2") {
		t.Errorf("S2 should be excluded (different routine_id):\n%s", out)
	}
}

func TestRoutineRunNowNeedsConfig(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t,
		"create", "noconfig",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
	); err != nil {
		t.Fatal(err)
	}
	// runRoutineCmd doesn't seed config, so run-now should fail at the
	// configFromContext guard before any service call.
	out, err := runRoutineCmd(t, "run-now", "noconfig")
	if err == nil {
		t.Errorf("expected error from missing config; got: %s", out)
	}
}

func TestRoutineWatchNonexistentPathErrors(t *testing.T) {
	setupXDG(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := runRoutineCmd(t, "watch", missing); err == nil {
		t.Error("expected error watching a non-existent path")
	}
}

func TestRoutineUnwatchUnknownPathNoOp(t *testing.T) {
	setupXDG(t)
	out, err := runRoutineCmd(t, "unwatch", filepath.Join(t.TempDir(), "never-watched"))
	if err != nil {
		t.Errorf("unwatching an unknown path should be a no-op, got: %v", err)
	}
	if !strings.Contains(out, "was not in the registry") {
		t.Errorf("expected no-op message, got:\n%s", out)
	}
}

func TestRoutineRootsEmptyMessage(t *testing.T) {
	setupXDG(t)
	out, err := runRoutineCmd(t, "roots")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(none)") {
		t.Errorf("expected (none) for empty roots, got:\n%s", out)
	}
}

func TestRoutineDeleteUnknownErrors(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t, "delete", "ghost"); err == nil {
		t.Error("expected error deleting non-existent routine")
	}
}

func TestRoutineEnableUnknownErrors(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t, "enable", "ghost"); err == nil {
		t.Error("expected error enabling non-existent routine")
	}
}

func TestRoutineShowMissingErrors(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t, "show", "ghost"); err == nil {
		t.Error("expected error showing non-existent routine")
	}
}

// fmtFloat formats a float without trailing zeros, suitable for embedding
// in test JSON literals.
func fmtFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(formatFloat(v), "0"), ".")
}

func formatFloat(v float64) string {
	// strconv would be more correct but adds an import only this helper uses.
	// fmt.Sprintf is fine here.
	return fmt.Sprintf("%f", v)
}

func TestRoutineCreateDuplicateErrors(t *testing.T) {
	setupXDG(t)
	wd := t.TempDir()
	if _, err := runRoutineCmd(t,
		"create", "dup",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", wd,
	); err != nil {
		t.Fatal(err)
	}
	// Second create with same id must fail.
	if _, err := runRoutineCmd(t,
		"create", "dup",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", wd,
	); err == nil {
		t.Error("expected error on duplicate create")
	}
}

func TestRoutineDisableUnknownErrors(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t, "disable", "ghost"); err == nil {
		t.Error("expected error disabling non-existent routine")
	}
}

func TestRoutineHistoryWorkingDirMissing(t *testing.T) {
	setupXDG(t)
	// Create a global routine but with a working_dir that has no sessions
	// subdir. History should produce the "no sessions" path, not an error.
	if _, err := runRoutineCmd(t,
		"create", "empty",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
	); err != nil {
		t.Fatal(err)
	}
	out, err := runRoutineCmd(t, "history", "empty")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(out, "No sessions found") {
		t.Errorf("expected empty marker:\n%s", out)
	}
}

func TestRoutineCreateScopeRepoWithoutCwdInSquadDir(t *testing.T) {
	setupXDG(t)
	repo := t.TempDir()
	// --scope=repo with explicit --repo path even when cwd has no .squad.
	out, err := runRoutineCmd(t,
		"create", "explicit",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--scope", "repo",
		"--repo", repo,
	)
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if !strings.Contains(out, "repo:explicit") {
		t.Errorf("expected repo qualifier:\n%s", out)
	}
}

func TestRoutineShowDisplaysAllOptionalFields(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t,
		"create", "full",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
		"--prompt", "audit",
		"--provider", "openai",
		"--model", "gpt-5",
		"--max-cost", "3.50",
		"--max-iterations", "30",
		"--var", "k=v",
		"--wake-system",
	); err != nil {
		t.Fatal(err)
	}
	out, err := runRoutineCmd(t, "show", "full")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Prompt:", "Provider:    openai", "Model:       gpt-5",
		"Max cost:    $3.50", "Max iter:    30", "Vars:", "k=v", "Wake system: yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q\n%s", want, out)
		}
	}
}

func TestRoutineWatchNoArgsUsesCwd(t *testing.T) {
	setupXDG(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	out, err := runRoutineCmd(t, "watch")
	if err != nil {
		t.Fatalf("watch (no args): %v\n%s", err, out)
	}
	if !strings.Contains(out, cwd) {
		t.Errorf("expected cwd in output, got:\n%s", out)
	}
}

func TestRoutineShowQualifiedNotFound(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t, "show", "global:ghost"); err == nil {
		t.Error("expected error for qualified-but-not-found")
	}
}

func TestRoutineHistoryQualified(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t,
		"create", "qual",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
	); err != nil {
		t.Fatal(err)
	}
	// History via qualified id should resolve the same routine.
	out, err := runRoutineCmd(t, "history", "global:qual")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(out, "No sessions found") {
		t.Errorf("expected empty history marker:\n%s", out)
	}
}

func TestRoutineCreateAutoCreatesRepoRoot(t *testing.T) {
	setupXDG(t)
	// When --repo points at a path that didn't exist, Create's MkdirAll
	// materializes the .squad/routines/ tree under it. The subsequent
	// AddRoot then succeeds because the directory is now on disk.
	target := filepath.Join(t.TempDir(), "fresh-repo")
	out, err := runRoutineCmd(t,
		"create", "freshrepo",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--scope", "repo",
		"--repo", target,
	)
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if !strings.Contains(out, "repo:freshrepo") {
		t.Errorf("expected repo qualifier:\n%s", out)
	}
	// Path should now exist with the manifest dir created.
	if _, err := os.Stat(filepath.Join(target, ".squad", "routines")); err != nil {
		t.Errorf("expected .squad/routines created: %v", err)
	}
}

// helper guard so unused-import warnings don't trip in environments where
// cobra isn't transitively needed.
var _ = (*cobra.Command)(nil)
