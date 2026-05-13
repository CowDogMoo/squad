package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/routine"
)

// setupXDG points all XDG/HOME env vars at a fresh tempdir so tests never
// touch the user's real config.
func setupXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, ".local", "state"))
	t.Setenv("HOME", dir)
	return dir
}

func TestParseVarsRoutine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []string
		want map[string]string
	}{
		{"empty", nil, nil},
		{"single", []string{"k=v"}, map[string]string{"k": "v"}},
		{"multi with equals in value", []string{"k=v=w", "x=y"}, map[string]string{"k": "v=w", "x": "y"}},
		{"missing equals skipped", []string{"bare"}, map[string]string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseVars(c.in)
			if c.want == nil && got != nil && len(got) != 0 {
				t.Errorf("expected nil, got %v", got)
			}
			if c.want == nil {
				return
			}
			if len(got) != len(c.want) {
				t.Errorf("len mismatch: got=%v want=%v", got, c.want)
				return
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("key %q: got %q want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestNonEmptyOr(t *testing.T) {
	t.Parallel()
	if got := nonEmptyOr("a", "fallback"); got != "a" {
		t.Errorf("got %q", got)
	}
	if got := nonEmptyOr("", "fallback"); got != "fallback" {
		t.Errorf("got %q", got)
	}
}

func TestAbsolutePathEmptyResolvesCwd(t *testing.T) {
	t.Parallel()
	got, err := absolutePath("")
	if err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if got != cwd {
		t.Errorf("got %q, want cwd %q", got, cwd)
	}
}

func TestAbsolutePathResolvesRelative(t *testing.T) {
	t.Parallel()
	got, err := absolutePath(".")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("expected absolute, got %q", got)
	}
}

func TestHasSquadDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if hasSquadDir(dir) {
		t.Error("expected false on fresh dir")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".squad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasSquadDir(dir) {
		t.Error("expected true after .squad created")
	}
}

func TestWorkingDirForDisplay(t *testing.T) {
	t.Parallel()
	// Global with explicit working_dir
	entry := routine.Entry{
		Ref:     routine.Ref{Scope: routine.ScopeGlobal, ID: "x"},
		Routine: &routine.Routine{WorkingDir: "/explicit"},
	}
	if got := workingDirForDisplay(entry); got != "/explicit" {
		t.Errorf("got %q", got)
	}
	// Global without working_dir
	entry.Routine = &routine.Routine{}
	if got := workingDirForDisplay(entry); got != "(unset)" {
		t.Errorf("got %q", got)
	}
	// Repo without explicit working_dir
	entry.Ref = routine.Ref{Scope: routine.ScopeRepo, Root: "/repo"}
	if got := workingDirForDisplay(entry); got != "/repo (repo root)" {
		t.Errorf("got %q", got)
	}
}

func TestAnyRoutineWantsWake(t *testing.T) {
	setupXDG(t)
	store := routine.NewStore()
	if got := anyRoutineWantsWake(store); got {
		t.Error("empty store should return false")
	}
	r := &routine.Routine{ID: "wake", Agent: "go", Schedule: "@daily", Enabled: true, WakeSystem: true}
	if _, err := store.Create(routine.Ref{Scope: routine.ScopeGlobal, ID: "wake"}, r); err != nil {
		t.Fatal(err)
	}
	if !anyRoutineWantsWake(store) {
		t.Error("enabled wake routine should return true")
	}
	// Disabled routine with wake_system should NOT trigger wake.
	r2 := &routine.Routine{ID: "off", Agent: "go", Schedule: "@daily", Enabled: false, WakeSystem: true}
	if _, err := store.Create(routine.Ref{Scope: routine.ScopeGlobal, ID: "off"}, r2); err != nil {
		t.Fatal(err)
	}
	// Disable the first to confirm.
	if err := store.Delete(routine.Ref{Scope: routine.ScopeGlobal, ID: "wake"}); err != nil {
		t.Fatal(err)
	}
	if anyRoutineWantsWake(store) {
		t.Error("disabled wake routine should not trigger")
	}
}

func TestResolveCreateRefGlobalDefault(t *testing.T) {
	setupXDG(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	ref, err := resolveCreateRef("nightly", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Scope != routine.ScopeGlobal {
		t.Errorf("expected global, got %s", ref.Scope)
	}
	if ref.ID != "nightly" {
		t.Errorf("id %q", ref.ID)
	}
}

func TestResolveCreateRefRepoWhenSquadDirPresent(t *testing.T) {
	setupXDG(t)
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".squad"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	ref, err := resolveCreateRef("audit", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Scope != routine.ScopeRepo {
		t.Errorf("expected repo, got %s", ref.Scope)
	}
	if ref.Root == "" {
		t.Error("repo ref must have Root set")
	}
}

func TestResolveCreateRefExplicitOverride(t *testing.T) {
	setupXDG(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	// Force repo scope with --repo override even when cwd has no .squad.
	repo := t.TempDir()
	ref, err := resolveCreateRef("audit", "repo", repo)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Scope != routine.ScopeRepo || ref.Root == "" {
		t.Errorf("expected repo with root, got %+v", ref)
	}
}

func TestResolveCreateRefBadScope(t *testing.T) {
	setupXDG(t)
	if _, err := resolveCreateRef("x", "weird", ""); err == nil {
		t.Error("expected error for invalid scope")
	}
}

func TestResolveExistingRefQualified(t *testing.T) {
	setupXDG(t)
	store := routine.NewStore()
	if _, err := store.Create(routine.Ref{Scope: routine.ScopeGlobal, ID: "n"}, &routine.Routine{
		ID: "n", Agent: "go", Schedule: "@daily", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	entry, err := resolveExistingRef(store, "global:n")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Ref.ID != "n" {
		t.Errorf("got %q", entry.Ref.ID)
	}
}

func TestResolveExistingRefBareUnique(t *testing.T) {
	setupXDG(t)
	store := routine.NewStore()
	if _, err := store.Create(routine.Ref{Scope: routine.ScopeGlobal, ID: "uniq"}, &routine.Routine{
		ID: "uniq", Agent: "go", Schedule: "@daily", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	entry, err := resolveExistingRef(store, "uniq")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Ref.Scope != routine.ScopeGlobal {
		t.Errorf("scope %s", entry.Ref.Scope)
	}
}

func TestResolveExistingRefMissing(t *testing.T) {
	setupXDG(t)
	store := routine.NewStore()
	if _, err := resolveExistingRef(store, "nope"); err == nil {
		t.Error("expected error for missing routine")
	}
}

func TestResolveExistingRefInvalidID(t *testing.T) {
	setupXDG(t)
	store := routine.NewStore()
	if _, err := resolveExistingRef(store, "BadCaps"); err == nil {
		t.Error("expected validation error")
	}
}

// --- session-meta listing helpers ---

func TestReadSessionMetaParsesRoutineID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "meta.json")
	payload := map[string]any{
		"session_id": "S1",
		"agent":      "go-review",
		"routine_id": "global:nightly",
		"created":    "2026-05-12T02:00:00Z",
		"status":     "completed",
		"cost":       0.05,
		"iterations": 3,
	}
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := readSessionMeta(path)
	if !ok {
		t.Fatal("read failed")
	}
	if got.id != "S1" || got.routineID != "global:nightly" || got.agent != "go-review" {
		t.Errorf("got %+v", got)
	}
}

func TestReadSessionMetaBadJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "meta.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readSessionMeta(path); ok {
		t.Error("expected ok=false for malformed json")
	}
}

func TestListSessionsForRoutineFiltersByRoutineID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkSession := func(id, agent, routineID string) {
		t.Helper()
		sd := filepath.Join(dir, id)
		if err := os.MkdirAll(sd, 0o755); err != nil {
			t.Fatal(err)
		}
		payload := map[string]any{
			"session_id": id,
			"agent":      agent,
			"routine_id": routineID,
			"created":    time.Now().UTC().Format(time.RFC3339Nano),
			"status":     "completed",
		}
		data, _ := json.Marshal(payload)
		if err := os.WriteFile(filepath.Join(sd, "meta.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkSession("s1", "go-review", "global:nightly")
	mkSession("s2", "go-review", "global:other")
	mkSession("s3", "py-review", "")

	// Sessions tagged with the routine id win on the exact match.
	got, err := listSessionsForRoutine(dir, "global:nightly", "go-review")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "s1" {
		t.Errorf("expected only s1, got %v", got)
	}

	// Sessions without a routine_id (s3) fall back to agent-name match.
	got2, err := listSessionsForRoutine(dir, "global:legacy", "py-review")
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 1 || got2[0].ID != "s3" {
		t.Errorf("expected only s3 (agent-fallback), got %v", got2)
	}
}

func TestListSessionsForRoutineMissingDir(t *testing.T) {
	t.Parallel()
	got, err := listSessionsForRoutine(filepath.Join(t.TempDir(), "nope"), "x", "y")
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// --- show-cmd printer fragments ---

func TestPrintRoutineSummaryFields(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	entry := routine.Entry{
		Ref:     routine.Ref{Scope: routine.ScopeRepo, Root: "/code/api", ID: "audit"},
		Routine: &routine.Routine{ID: "audit", Agent: "go-review", Schedule: "@daily", Enabled: true},
	}
	printRoutineSummary(buf, entry)
	for _, want := range []string{
		"ID:          audit",
		"Scope:       repo:api",
		"Agent:       go-review",
		"Schedule:    @daily",
		"Enabled:     true",
	} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("summary missing %q\n--- output ---\n%s", want, buf.String())
		}
	}
}

func TestPrintRoutineOptionalSkipsEmpty(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	printRoutineOptional(buf, &routine.Routine{})
	if buf.Len() != 0 {
		t.Errorf("empty routine should produce no output, got: %s", buf.String())
	}
}

func TestPrintRoutineOptionalEmitsSetFields(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	printRoutineOptional(buf, &routine.Routine{
		Prompt: "hello",
		Vars:   map[string]string{"a": "1", "b": "2"},
	})
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("Prompt:      hello")) {
		t.Errorf("missing prompt line:\n%s", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte("a=1")) || !bytes.Contains(buf.Bytes(), []byte("b=2")) {
		t.Errorf("vars not rendered:\n%s", out)
	}
}

func TestPrintRoutineFooterWakeSystem(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	entry := routine.Entry{
		Ref:          routine.Ref{Scope: routine.ScopeGlobal, ID: "x"},
		Routine:      &routine.Routine{WakeSystem: true},
		ManifestPath: "/a",
		StatePath:    "/b",
	}
	printRoutineFooter(buf, entry)
	if !bytes.Contains(buf.Bytes(), []byte("Wake system: yes")) {
		t.Errorf("missing wake line:\n%s", buf.String())
	}
}

func TestPrintRoutineStateSkipsZero(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	printRoutineState(buf, &routine.State{})
	if buf.Len() != 0 {
		t.Errorf("zero state should produce no output, got: %s", buf.String())
	}
}

func TestPrintRoutineStateRendersLastRun(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	printRoutineState(buf, &routine.State{
		LastRun:        time.Date(2026, 5, 12, 2, 0, 0, 0, time.UTC),
		LastStatus:     routine.StatusOK,
		LastSessionID:  "S1",
		LastDurationMs: 4321,
	})
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("Last run:")) || !bytes.Contains(buf.Bytes(), []byte("ok")) {
		t.Errorf("missing last run:\n%s", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Last session:  S1")) {
		t.Errorf("missing session:\n%s", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Last duration: 4321ms")) {
		t.Errorf("missing duration:\n%s", out)
	}
}

func TestPrintRoutineStateRendersLastError(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	printRoutineState(buf, &routine.State{
		LastRun:    time.Now(),
		LastStatus: routine.StatusFailed,
		LastError:  "model timeout after 3 retries",
	})
	if !bytes.Contains(buf.Bytes(), []byte("Last error:")) {
		t.Errorf("missing last error line:\n%s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("model timeout")) {
		t.Errorf("missing error detail:\n%s", buf.String())
	}
}

func TestResolveExistingRefQualifiedNotFound(t *testing.T) {
	setupXDG(t)
	store := routine.NewStore()
	if _, err := resolveExistingRef(store, "global:ghost"); err == nil {
		t.Error("expected error for qualified id not in store")
	}
	if _, err := resolveExistingRef(store, "repo:ghost"); err == nil {
		t.Error("expected error for qualified id not in store")
	}
}

func TestReadSessionMetaMissingFile(t *testing.T) {
	t.Parallel()
	if _, ok := readSessionMeta(filepath.Join(t.TempDir(), "absent.json")); ok {
		t.Error("expected ok=false for missing file")
	}
}

func TestPrintNextFireSkipsInvalidSchedule(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	printNextFire(buf, "totally not a schedule")
	if buf.Len() != 0 {
		t.Errorf("invalid schedule should produce no output, got: %s", buf.String())
	}
}

func TestPrintNextFireValidSchedule(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	printNextFire(buf, "@daily")
	if !bytes.Contains(buf.Bytes(), []byte("Next fire:")) {
		t.Errorf("missing next fire:\n%s", buf.String())
	}
}

func TestPreferDaemonBinary(t *testing.T) {
	t.Parallel()
	// preferDaemonBinary is platform-specific; on non-Windows it returns exe unchanged.
	got := preferDaemonBinary("/path/to/squad")
	if got == "" {
		t.Error("expected non-empty path")
	}
}
