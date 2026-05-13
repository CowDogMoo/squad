package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/routine"
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

// withCorruptManifest creates a routine and then corrupts its on-disk
// manifest. Returns the manifest path so tests can clean up.
func withCorruptManifest(t *testing.T, id string) string {
	t.Helper()
	dir := setupXDG(t)
	if _, err := runRoutineCmd(t,
		"create", id,
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
	); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, ".config", "squad", "routines", id+".yaml")
	if err := os.WriteFile(manifest, []byte("not\nvalid: yaml :: ::"), 0o644); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestRoutineCommandsTolerateCorruptManifest(t *testing.T) {
	// list/show/etc should gracefully skip the bad manifest and not crash.
	withCorruptManifest(t, "corrupt")

	out, err := runRoutineCmd(t, "list")
	if err != nil {
		t.Fatalf("list with corrupt manifest: %v", err)
	}
	// The bad manifest gets skipped silently — list shows zero entries.
	if !strings.Contains(out, "No routines") {
		t.Errorf("expected empty list (corrupt manifest skipped):\n%s", out)
	}

	// show on the corrupt id should return a not-found error because
	// LoadAll skipped it.
	if _, err := runRoutineCmd(t, "show", "corrupt"); err == nil {
		t.Error("expected show to fail when manifest is corrupt (skipped at load)")
	}
}

func TestRoutineEnableNonexistent(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t, "enable", "wat"); err == nil {
		t.Error("expected error enabling nonexistent routine")
	}
}

// blockManifestRead makes the routines directory unreadable so subsequent
// store.LoadAll calls return an error. Skipped as root because chmod doesn't
// restrict the superuser.
func blockManifestRead(t *testing.T, xdgDir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("chmod-based fault injection skipped as root")
	}
	dir := filepath.Join(xdgDir, ".config", "squad", "routines")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestRoutineListLoadAllErrorBubbles(t *testing.T) {
	dir := setupXDG(t)
	blockManifestRead(t, dir)
	if _, err := runRoutineCmd(t, "list"); err == nil {
		t.Error("expected error when routines dir is unreadable")
	}
}

func TestRoutineShowLoadAllErrorBubbles(t *testing.T) {
	dir := setupXDG(t)
	blockManifestRead(t, dir)
	if _, err := runRoutineCmd(t, "show", "anything"); err == nil {
		t.Error("expected error when routines dir is unreadable")
	}
}

func TestRoutineDeleteLoadAllErrorBubbles(t *testing.T) {
	dir := setupXDG(t)
	blockManifestRead(t, dir)
	if _, err := runRoutineCmd(t, "delete", "anything"); err == nil {
		t.Error("expected error when routines dir is unreadable")
	}
}

func TestRoutineEnableLoadAllErrorBubbles(t *testing.T) {
	dir := setupXDG(t)
	blockManifestRead(t, dir)
	if _, err := runRoutineCmd(t, "enable", "anything"); err == nil {
		t.Error("expected error when routines dir is unreadable")
	}
}

func TestRoutineHistoryLoadAllErrorBubbles(t *testing.T) {
	dir := setupXDG(t)
	blockManifestRead(t, dir)
	if _, err := runRoutineCmd(t, "history", "anything"); err == nil {
		t.Error("expected error when routines dir is unreadable")
	}
}

func TestRoutineRunNowLoadAllErrorBubbles(t *testing.T) {
	dir := setupXDG(t)
	t.Setenv("SQUAD_SKIP_SERVICE_INSTALL", "1")
	blockManifestRead(t, dir)
	// run-now needs config (we don't seed it), so it'll fail early anyway.
	// Use the routine cmd tree which seeds nothing in context — error should
	// surface either way.
	if _, err := runRoutineCmd(t, "run-now", "x"); err == nil {
		t.Error("expected error")
	}
}

func TestRoutineCreateBadScopeErrors(t *testing.T) {
	setupXDG(t)
	_, err := runRoutineCmd(t,
		"create", "x",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--scope", "weird",
		"--working-dir", t.TempDir(),
	)
	if err == nil {
		t.Error("expected error for invalid scope")
	}
}

func TestRoutineCreateAddRootFailsOnFile(t *testing.T) {
	setupXDG(t)
	// --repo points at a regular file. AddRoot rejects file-as-root after
	// MkdirAll auto-creates the .squad/routines parent. Wait — the test
	// setup needs a file at the exact repo path, so MkdirAll fails inside
	// Create rather than at AddRoot. Either way, the error path is exercised.
	tmp := t.TempDir()
	asFile := filepath.Join(tmp, "regular-file")
	if err := os.WriteFile(asFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := runRoutineCmd(t,
		"create", "filer",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--scope", "repo",
		"--repo", asFile,
	)
	if err == nil {
		t.Error("expected error when --repo points at a file")
	}
}

func TestRoutineUnwatchAcceptsExactPath(t *testing.T) {
	setupXDG(t)
	repo := t.TempDir()
	if _, err := runRoutineCmd(t, "watch", repo); err != nil {
		t.Fatal(err)
	}
	out, err := runRoutineCmd(t, "unwatch", repo)
	if err != nil {
		t.Fatalf("unwatch: %v", err)
	}
	if !strings.Contains(out, "Unwatched") {
		t.Errorf("expected unwatch confirmation, got:\n%s", out)
	}
}

func TestRoutineDeletePropagatesStoreError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod-based fault injection skipped as root")
	}
	dir := setupXDG(t)
	if _, err := runRoutineCmd(t,
		"create", "todelete",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
	); err != nil {
		t.Fatal(err)
	}
	// Make the manifest parent dir read-only so os.Remove fails inside Delete.
	manifestDir := filepath.Join(dir, ".config", "squad", "routines")
	if err := os.Chmod(manifestDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(manifestDir, 0o755) })

	if _, err := runRoutineCmd(t, "delete", "todelete"); err == nil {
		t.Error("expected error when manifest dir is read-only")
	}
}

func TestRoutineCreateRejectsDuplicateOnDisk(t *testing.T) {
	dir := setupXDG(t)
	wd := t.TempDir()
	if _, err := runRoutineCmd(t,
		"create", "dupedisk",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", wd,
	); err != nil {
		t.Fatal(err)
	}
	// Manifest exists; second create with same id must error.
	manifestPath := filepath.Join(dir, ".config", "squad", "routines", "dupedisk.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	_, err := runRoutineCmd(t,
		"create", "dupedisk",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", wd,
	)
	if err == nil {
		t.Error("expected error on second create with same id")
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

func writeGlobalState(t *testing.T, id string, body string) {
	t.Helper()
	stateDir, err := routine.GlobalStateDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, routine.StateFileName(id))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRoutineListRendersStateAndDisabled(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t,
		"create", "withstate",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
		"--disabled",
	); err != nil {
		t.Fatal(err)
	}
	writeGlobalState(t, "withstate", `{"last_status":"ok","last_run":"2026-05-12T02:00:00Z","last_session_id":"S1"}`)
	out, err := runRoutineCmd(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	// The disabled `no` and status `ok` columns prove both non-default
	// branches in newRoutineListCmd fired.
	if !strings.Contains(out, "withstate") || !strings.Contains(out, "no") || !strings.Contains(out, "ok") {
		t.Errorf("expected disabled=no and status=ok in list output:\n%s", out)
	}
}

func TestRoutineShowRendersStateWithError(t *testing.T) {
	setupXDG(t)
	if _, err := runRoutineCmd(t,
		"create", "errored",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
	); err != nil {
		t.Fatal(err)
	}
	writeGlobalState(t, "errored", `{"last_status":"failed","last_run":"2026-05-12T02:00:00Z","last_error":"kaboom","last_session_id":"S-9","last_duration_ms":1234}`)
	out, err := runRoutineCmd(t, "show", "errored")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Last run:", "kaboom", "Last session:  S-9", "Last duration: 1234ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestRoutineHistoryFlagsLastRecordedFire(t *testing.T) {
	setupXDG(t)
	wd := t.TempDir()
	sd := filepath.Join(wd, ".squad", "sessions")
	if err := os.MkdirAll(filepath.Join(sd, "S-LAST"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"session_id":"S-LAST","agent":"go-review","routine_id":"global:hist2","created":"2026-05-12T02:00:00Z","status":"completed","cost":0.5,"iterations":3}`
	if err := os.WriteFile(filepath.Join(sd, "S-LAST", "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoutineCmd(t,
		"create", "hist2",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", wd,
	); err != nil {
		t.Fatal(err)
	}
	writeGlobalState(t, "hist2", `{"last_status":"ok","last_run":"2026-05-12T02:00:00Z","last_session_id":"S-LAST"}`)
	out, err := runRoutineCmd(t, "history", "hist2")
	if err != nil {
		t.Fatalf("history: %v\n%s", err, out)
	}
	if !strings.Contains(out, "(last recorded fire)") {
		t.Errorf("expected last-recorded marker:\n%s", out)
	}
}

func TestRoutineHistorySkipsBadMetaAndNonDirs(t *testing.T) {
	setupXDG(t)
	wd := t.TempDir()
	sd := filepath.Join(wd, ".squad", "sessions")
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	// Non-directory entry — listSessionsForRoutine should skip it.
	if err := os.WriteFile(filepath.Join(sd, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Directory with malformed meta.json — readSessionMeta returns ok=false.
	if err := os.MkdirAll(filepath.Join(sd, "S-BAD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sd, "S-BAD", "meta.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Directory with no meta.json at all — ReadFile error branch.
	if err := os.MkdirAll(filepath.Join(sd, "S-NOMETA"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoutineCmd(t,
		"create", "skiphist",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", wd,
	); err != nil {
		t.Fatal(err)
	}
	out, err := runRoutineCmd(t, "history", "skiphist")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(out, "No sessions found") {
		t.Errorf("expected empty marker after filtering noise:\n%s", out)
	}
}

func TestRoutineHistoryRepoUsesRootWhenNoWorkingDir(t *testing.T) {
	setupXDG(t)
	repo := t.TempDir()
	t.Chdir(repo)
	// Create a repo-scoped routine without --working-dir — history must fall
	// back to ref.Root for the sessions dir lookup.
	if _, err := runRoutineCmd(t,
		"create", "rooted",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--scope", "repo",
	); err != nil {
		t.Fatal(err)
	}
	out, err := runRoutineCmd(t, "history", "rooted")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(out, repo) {
		t.Errorf("expected sessions dir under repo root:\n%s", out)
	}
}

func TestRoutineHistoryGlobalWithoutWorkingDirErrors(t *testing.T) {
	setupXDG(t)
	// Manually write a global manifest with empty working_dir; the CLI's
	// create path always sets one, so we bypass it.
	dir, err := routine.GlobalRoutinesDir()
	if err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`id: nowd
agent: go-review
schedule: '@daily'
enabled: true
`)
	if err := os.WriteFile(filepath.Join(dir, "nowd.yaml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoutineCmd(t, "history", "nowd"); err == nil {
		t.Error("expected error for routine with no working_dir")
	}
}

func TestRoutineCreateExplicitGlobalScope(t *testing.T) {
	setupXDG(t)
	// --scope=global overrides the inferred default even when cwd has .squad/.
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".squad", "routines"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	out, err := runRoutineCmd(t,
		"create", "forced",
		"--agent", "go-review",
		"--schedule", "@daily",
		"--working-dir", t.TempDir(),
		"--scope", "global",
	)
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	if !strings.Contains(out, "global:forced") {
		t.Errorf("expected global qualifier:\n%s", out)
	}
}
