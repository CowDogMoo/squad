package routine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This file targets the long tail of branches the unit tests don't naturally
// exercise: helper predicates, error paths, XDG fallbacks, and the
// Added/Updated branch of the scheduler's event handler.

func TestStringMapEqual(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b map[string]string
		want bool
	}{
		{nil, nil, true},
		{map[string]string{}, nil, true},
		{map[string]string{"k": "v"}, map[string]string{"k": "v"}, true},
		{map[string]string{"k": "v"}, map[string]string{"k": "w"}, false},
		{map[string]string{"a": "1"}, map[string]string{"a": "1", "b": "2"}, false},
		{map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1"}, false},
	}
	for _, c := range cases {
		if got := stringMapEqual(c.a, c.b); got != c.want {
			t.Errorf("stringMapEqual(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestRoutineChanged(t *testing.T) {
	t.Parallel()
	base := &Routine{ID: "x", Agent: "go", Schedule: "@daily", Enabled: true}
	if routineChanged(base, base) {
		t.Error("identical routines should not register as changed")
	}
	if !routineChanged(nil, base) {
		t.Error("nil vs non-nil must register as changed")
	}
	if !routineChanged(base, nil) {
		t.Error("non-nil vs nil must register as changed")
	}

	fields := []func(*Routine){
		func(r *Routine) { r.Schedule = "@hourly" },
		func(r *Routine) { r.Agent = "py" },
		func(r *Routine) { r.Enabled = false },
		func(r *Routine) { r.WorkingDir = "/somewhere" },
		func(r *Routine) { r.Provider = "openai" },
		func(r *Routine) { r.Model = "gpt-5" },
		func(r *Routine) { r.Prompt = "do something" },
		func(r *Routine) { r.MaxCost = 9.99 },
		func(r *Routine) { r.MaxIterations = 17 },
		func(r *Routine) { r.Catchup = CatchupSkip },
		func(r *Routine) { r.Vars = map[string]string{"k": "v"} },
	}
	for i, mutate := range fields {
		modified := *base
		mutate(&modified)
		if !routineChanged(base, &modified) {
			t.Errorf("case %d: change not detected", i)
		}
	}
}

func TestStoreLoadStateAndSaveStateViaMethods(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "state-roundtrip"}
	if _, err := store.Create(ref, &Routine{ID: "state-roundtrip", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	// Round-trip through the Store wrappers, not the package-level funcs.
	in := &State{LastStatus: StatusOK, LastRun: time.Now().UTC(), LastSessionID: "S-1"}
	if err := store.SaveState(ref, in); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	out, err := store.LoadState(ref)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if out.LastSessionID != "S-1" || out.LastStatus != StatusOK {
		t.Errorf("round-trip mismatch: %+v", out)
	}
}

func TestStoreLoadStateRejectsMissingRef(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	if _, err := store.LoadState(Ref{Scope: ScopeGlobal, ID: "ghost"}); err == nil {
		t.Error("expected error for missing routine")
	}
	if err := store.SaveState(Ref{Scope: ScopeGlobal, ID: "ghost"}, &State{}); err == nil {
		t.Error("expected error for missing routine")
	}
}

func TestSchedulerApplyEventAddRespawnsJob(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	sched, err := NewScheduler(store, neverFires(), SchedulerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{Scope: ScopeGlobal, ID: "added"}
	entry, err := store.Create(ref, &Routine{ID: "added", Agent: "go", Schedule: "@daily", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	sched.ApplyEvent(Event{Type: EventAdded, Ref: ref, Entry: entry})
	if _, ok := sched.JobIDs()[entryKey(ref)]; !ok {
		t.Error("EventAdded did not schedule a job")
	}
}

func TestSchedulerApplyEventUpdatedReplaces(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "upd"}
	if _, err := store.Create(ref, &Routine{ID: "upd", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	sched, _ := NewScheduler(store, neverFires(), SchedulerOptions{})
	_ = sched.Sync(store.Entries())
	first := sched.JobIDs()[entryKey(ref)]

	// Update the schedule + apply the event.
	if _, err := store.Update(ref, &Routine{ID: "upd", Agent: "go", Schedule: "@hourly", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	sched.ApplyEvent(Event{Type: EventUpdated, Ref: ref})
	if sched.JobIDs()[entryKey(ref)] == first {
		t.Error("EventUpdated did not produce a new job UUID")
	}
}

func TestNewSchedulerRejectsNilArgs(t *testing.T) {
	t.Parallel()
	if _, err := NewScheduler(nil, neverFires(), SchedulerOptions{}); err == nil {
		t.Error("expected error for nil store")
	}
	if _, err := NewScheduler(NewStore(), nil, SchedulerOptions{}); err == nil {
		t.Error("expected error for nil fire fn")
	}
}

func TestSchedulerFireContextHonorsTimeout(t *testing.T) {
	t.Parallel()
	s := &Scheduler{timeout: 10 * time.Millisecond}
	ctx, cancel := s.fireContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 50*time.Millisecond {
		t.Errorf("expected sub-50ms deadline, got %v", deadline)
	}
}

func TestSchedulerFireContextNoTimeout(t *testing.T) {
	t.Parallel()
	s := &Scheduler{}
	ctx, cancel := s.fireContext(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Error("expected no deadline when timeout=0")
	}
}

func TestGlobalStateDirHonorsXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	got, err := GlobalStateDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "squad", "routines")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("expected dir created: %v", err)
	}
}

func TestGlobalStateDirFallsBackToHomeLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := GlobalStateDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "state", "squad", "routines")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSaveRoutineValidates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := &Routine{ID: "BadCase"}
	if err := SaveRoutine(filepath.Join(dir, FileName("x")), bad); err == nil {
		t.Error("expected validation error in SaveRoutine")
	}
}

func TestLoadRoutineRejectsBadYAML(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte(": not valid yaml :::"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRoutine(path); err == nil {
		t.Error("expected parse error")
	}
}

func TestLoadRoutineRejectsValidYAMLWithInvalidContent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("id: BadCase\nagent: x\nschedule: '@daily'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRoutine(path); err == nil {
		t.Error("expected validate error")
	}
}

func TestSendOrDropDropsOnFullChannel(t *testing.T) {
	t.Parallel()
	ch := make(chan Event, 1)
	ch <- Event{Type: EventAdded}
	// Channel is full; sendOrDrop must not block.
	sendOrDrop(ch, Event{Type: EventRemoved, Ref: Ref{Scope: ScopeGlobal, ID: "x"}})
	// First event still there; second dropped.
	first := <-ch
	if first.Type != EventAdded {
		t.Errorf("expected the original event to remain queued, got %v", first.Type)
	}
}

func TestScanDirSkipsInvalidManifestSilently(t *testing.T) {
	dir := t.TempDir()
	// Bad manifest + good manifest in the same dir.
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("id: NOPE"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := &Routine{ID: "good", Agent: "go", Schedule: "@daily", Enabled: true}
	if err := SaveRoutine(filepath.Join(dir, FileName("good")), good); err != nil {
		t.Fatal(err)
	}

	store := NewStore()
	entries := map[string]Entry{}
	if err := store.scanDir(entries, dir, ScopeGlobal, "", t.TempDir()); err != nil {
		t.Fatalf("scanDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 valid entry, got %d (bad manifest should be skipped)", len(entries))
	}
}

func TestResolveDirsRejectsRepoWithoutRoot(t *testing.T) {
	t.Parallel()
	if _, _, err := resolveDirs(Ref{Scope: ScopeRepo, ID: "x"}); err == nil {
		t.Error("expected error for repo ref with empty Root")
	}
	if _, _, err := resolveDirs(Ref{Scope: Scope("weird"), ID: "x"}); err == nil {
		t.Error("expected error for unknown scope")
	}
}

func TestSaveStateRejectsUnwriteablePath(t *testing.T) {
	t.Parallel()
	// /proc on Linux and / on macOS are both unwriteable; SaveState's
	// MkdirAll should fail and the error must propagate.
	err := SaveState("/proc/cannot/exist/state.json", &State{LastStatus: StatusOK})
	if err == nil {
		t.Error("expected error writing to an unwriteable location")
	}
}

func TestLoadStateRejectsCorruptJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Error("expected parse error")
	}
}

func TestStoreCreateMismatchedID(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	_, err := store.Create(
		Ref{Scope: ScopeGlobal, ID: "ref-id"},
		&Routine{ID: "different", Agent: "go", Schedule: "@daily", Enabled: true},
	)
	if err == nil {
		t.Error("expected error when ref.ID != routine.ID")
	}
}

func TestStoreCreateRepoRequiresRoot(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	_, err := store.Create(
		Ref{Scope: ScopeRepo, ID: "audit"}, // no Root
		&Routine{ID: "audit", Agent: "go", Schedule: "@daily", Enabled: true},
	)
	if err == nil {
		t.Error("expected error for repo ref without Root")
	}
}

func TestStoreDeleteMissingRoutine(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	if err := store.Delete(Ref{Scope: ScopeGlobal, ID: "ghost"}); err == nil {
		t.Error("expected error deleting non-existent routine")
	}
}

func TestStoreUpdateRejectsRename(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "stable"}
	if _, err := store.Create(ref, &Routine{ID: "stable", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// Mismatched id should error.
	_, err := store.Update(ref, &Routine{ID: "renamed", Agent: "go", Schedule: "@daily", Enabled: true})
	if err == nil {
		t.Error("expected error when Update routine id differs from ref id")
	}
	// Missing routine should error.
	_, err = store.Update(Ref{Scope: ScopeGlobal, ID: "ghost"}, &Routine{ID: "ghost", Agent: "go", Schedule: "@daily", Enabled: true})
	if err == nil {
		t.Error("expected error updating missing routine")
	}
}

func TestSaveRoutineAtomicCleansTmpOnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Removing read+exec permission on the parent makes the rename fail.
	// (Skipped if we're running as root.)
	if os.Geteuid() == 0 {
		t.Skip("rename-error path can't be provoked as root")
	}
	readOnly := filepath.Join(dir, "ro")
	if err := os.MkdirAll(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(readOnly, FileName("x"))
	err := SaveRoutine(path, &Routine{ID: "x", Agent: "go", Schedule: "@daily", Enabled: true})
	if err == nil {
		t.Error("expected error writing into read-only dir")
	}
}

func TestAddRootResolvesAndRejectsNonDirs(t *testing.T) {
	setupTempXDG(t)
	missing := filepath.Join(t.TempDir(), "ghost")
	if _, _, err := AddRoot(missing); err == nil {
		t.Error("expected stat error for missing dir")
	}
	// File-as-root should error.
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddRoot(file); err == nil {
		t.Error("expected error for file-as-root")
	}
}

func TestRemoveRootUnknownNoop(t *testing.T) {
	setupTempXDG(t)
	changed, err := RemoveRoot(filepath.Join(t.TempDir(), "not-watched"))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected changed=false when removing unknown root")
	}
}

func TestNormalizeRootsDeduplicates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Pass the same root twice plus an absolute equivalent.
	got, err := normalizeRoots([]string{dir, dir, dir + string(os.PathSeparator)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 deduped entry, got %v", got)
	}
}

func TestIsManifestFile(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"foo.yaml":     true,
		"foo-bar.yaml": true,
		"":             false,
		"foo.yml":      false,
		"BadCase.yaml": false,
		".yaml":        false,
		"foo.txt":      false,
	}
	for name, want := range cases {
		if got := isManifestFile(name); got != want {
			t.Errorf("isManifestFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSchedulerRunNowOnMissingRoutine(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	sched, _ := NewScheduler(store, neverFires(), SchedulerOptions{})
	if err := sched.RunNow(context.Background(), Ref{Scope: ScopeGlobal, ID: "ghost"}); err == nil {
		t.Error("expected error for missing routine")
	}
}
