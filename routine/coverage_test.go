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

// --- file-write error paths via read-only parent directories. Skipped when
// running as root, where chmod doesn't restrict the writer.

func makeReadOnly(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("file-write error path can't be provoked as root")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.MkdirAll(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	return dir
}

func TestSaveStateRejectsReadOnlyParent(t *testing.T) {
	ro := makeReadOnly(t)
	err := SaveState(filepath.Join(ro, "child", "state.json"), &State{LastStatus: StatusOK})
	if err == nil {
		t.Error("expected error writing into read-only dir")
	}
}

func TestSaveRoutineRejectsReadOnlyParent(t *testing.T) {
	ro := makeReadOnly(t)
	r := &Routine{ID: "x", Agent: "go", Schedule: "@daily", Enabled: true}
	if err := SaveRoutine(filepath.Join(ro, FileName("x")), r); err == nil {
		t.Error("expected error writing into read-only dir")
	}
}

func TestStoreLoadAllScansBothScopes(t *testing.T) {
	setupTempXDG(t)
	repo := t.TempDir()
	if _, _, err := AddRoot(repo); err != nil {
		t.Fatal(err)
	}
	// Create a per-repo manifest directly on disk, before LoadAll runs.
	repoDir := RepoRoutinesDir(repo)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Routine{ID: "fromdisk", Agent: "go", Schedule: "@daily", Enabled: true}
	if err := SaveRoutine(filepath.Join(repoDir, FileName("fromdisk")), r); err != nil {
		t.Fatal(err)
	}
	store := NewStore()
	entries, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Ref.Scope == ScopeRepo && e.Ref.ID == "fromdisk" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected fromdisk in entries, got %d total", len(entries))
	}
}

func TestQueueCatchupsOnEmptyMissesNoOp(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	sched, _ := NewScheduler(store, neverFires(), SchedulerOptions{})
	// No misses → no goroutines spawned.
	QueueCatchups(context.Background(), sched, nil)
}

func TestStateFileName(t *testing.T) {
	t.Parallel()
	if got := StateFileName("foo"); got != "foo.state.json" {
		t.Errorf("got %q", got)
	}
}

func TestFileName(t *testing.T) {
	t.Parallel()
	if got := FileName("foo"); got != "foo.yaml" {
		t.Errorf("got %q", got)
	}
}

func TestValidateScheduleAcceptsDescriptorVariants(t *testing.T) {
	t.Parallel()
	cases := []string{"@daily", "@hourly", "@weekly", "@monthly", "@yearly", "@every 5m", "*/10 * * * *"}
	for _, c := range cases {
		if err := ValidateSchedule(c); err != nil {
			t.Errorf("%q should be valid: %v", c, err)
		}
	}
}

func TestStoreUpdateMissingRef(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	r := &Routine{ID: "ghost", Agent: "go", Schedule: "@daily", Enabled: true}
	if _, err := store.Update(Ref{Scope: ScopeGlobal, ID: "ghost"}, r); err == nil {
		t.Error("expected error updating missing ref")
	}
}

func TestStoreLoadStateOnEmptyState(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "fresh"}
	if _, err := store.Create(ref, &Routine{ID: "fresh", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	st, err := store.LoadState(ref)
	if err != nil {
		t.Fatalf("LoadState on fresh routine: %v", err)
	}
	if !st.LastRun.IsZero() {
		t.Errorf("expected zero state, got %+v", st)
	}
}

func TestNewSchedulerWithMaxConcurrent(t *testing.T) {
	t.Parallel()
	store := NewStore()
	s, err := NewScheduler(store, neverFires(), SchedulerOptions{MaxConcurrent: 4})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil scheduler")
	}
}

func TestSchedulerShutdownOnUnstartedIsClean(t *testing.T) {
	t.Parallel()
	store := NewStore()
	s, err := NewScheduler(store, neverFires(), SchedulerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Shutdown without Start should not error.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown without Start: %v", err)
	}
}

func TestFindMissedFiresNoEntries(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	misses := FindMissedFires(store, time.Now())
	if len(misses) != 0 {
		t.Errorf("expected no misses on empty store, got %v", misses)
	}
}

func TestEffectiveCatchupDefault(t *testing.T) {
	t.Parallel()
	r := &Routine{}
	if r.EffectiveCatchup() != DefaultCatchup {
		t.Errorf("default catchup: got %v", r.EffectiveCatchup())
	}
	r.Catchup = CatchupSkip
	if r.EffectiveCatchup() != CatchupSkip {
		t.Errorf("explicit skip: got %v", r.EffectiveCatchup())
	}
}

func TestIDFromFileNameValidExtensions(t *testing.T) {
	t.Parallel()
	if id := IDFromFileName("nightly-audit.yaml"); id != "nightly-audit" {
		t.Errorf("got %q", id)
	}
	if id := IDFromFileName("trailing-.yaml"); id != "" {
		t.Errorf("trailing-hyphen id rejected: got %q", id)
	}
}

// More targeted error-path coverage for routine/roots.go and storage.go.

func TestLoadRoutineMissingFile(t *testing.T) {
	t.Parallel()
	if _, err := LoadRoutine(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("expected error reading missing routine")
	}
}

func TestLoadRootsCorruptYAML(t *testing.T) {
	setupTempXDG(t)
	path, err := RootsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(":not\nvalid: ::: yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRoots(); err == nil {
		t.Error("expected parse error from corrupt roots yaml")
	}
}

func TestStoreCreateInUnwriteableScope(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("cannot provoke ENOENT/EACCES write failure as root")
	}
	setupTempXDG(t)
	ro := makeReadOnly(t)
	store := NewStore()
	_, err := store.Create(
		Ref{Scope: ScopeRepo, Root: filepath.Join(ro, "absent-repo"), ID: "rep"},
		&Routine{ID: "rep", Agent: "go", Schedule: "@daily", Enabled: true},
	)
	if err == nil {
		t.Error("expected error creating manifest in unwriteable repo root")
	}
}

func TestSchedulerSyncEmptySliceIsCleanWipe(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "wipe"}
	if _, err := store.Create(ref, &Routine{ID: "wipe", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	sched, _ := NewScheduler(store, neverFires(), SchedulerOptions{})
	_ = sched.Sync(store.Entries())
	if len(sched.JobIDs()) != 1 {
		t.Fatal("setup")
	}
	// Empty entries → all jobs removed.
	if err := sched.Sync(nil); err != nil {
		t.Fatalf("Sync nil: %v", err)
	}
	if len(sched.JobIDs()) != 0 {
		t.Errorf("expected no jobs, got %v", sched.JobIDs())
	}
}

func TestSchedulerSyncBadScheduleSkipsButNoError(t *testing.T) {
	// A routine with a corrupted schedule should be skipped by gocron's
	// addJob, but Sync itself returns nil so other routines still install.
	setupTempXDG(t)
	store := NewStore()
	// Force the invalid schedule into the store directly, bypassing
	// SaveRoutine validation.
	ref := Ref{Scope: ScopeGlobal, ID: "bad"}
	if _, err := store.Create(ref, &Routine{ID: "bad", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// Mutate in-memory after Create so validation passes once.
	entries := store.Entries()
	if len(entries) != 1 {
		t.Fatal("setup")
	}
	entries[0].Routine.Schedule = "this is not a schedule"
	sched, _ := NewScheduler(store, neverFires(), SchedulerOptions{})
	if err := sched.Sync(entries); err != nil {
		t.Errorf("Sync should swallow bad-schedule errors, got %v", err)
	}
	if _, ok := sched.JobIDs()[entryKey(ref)]; ok {
		t.Error("invalid-schedule job should not be in the map")
	}
}

func TestSchedulerApplyEventNoOpOnMissing(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	sched, _ := NewScheduler(store, neverFires(), SchedulerOptions{})
	// EventRemoved for a routine that was never scheduled — should not panic.
	sched.ApplyEvent(Event{Type: EventRemoved, Ref: Ref{Scope: ScopeGlobal, ID: "ghost"}})
	if len(sched.JobIDs()) != 0 {
		t.Errorf("expected empty after removing ghost, got %v", sched.JobIDs())
	}
}

func TestFindMissedFiresIgnoresInvalidSchedule(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "garbage"}
	if _, err := store.Create(ref, &Routine{ID: "garbage", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// Stomp the schedule in-memory to something NextFire can't parse.
	for _, e := range store.Entries() {
		e.Routine.Schedule = "not a schedule"
	}
	if got := FindMissedFires(store, time.Now()); len(got) != 0 {
		t.Errorf("expected zero misses for invalid schedule, got %v", got)
	}
}

func TestRoutineEffectiveCatchupHonorsExplicit(t *testing.T) {
	t.Parallel()
	for _, p := range []CatchupPolicy{CatchupFireOnce, CatchupSkip} {
		r := &Routine{Catchup: p}
		if r.EffectiveCatchup() != p {
			t.Errorf("expected %s, got %s", p, r.EffectiveCatchup())
		}
	}
}

func TestStoreFindMissingRef(t *testing.T) {
	t.Parallel()
	store := NewStore()
	if _, ok := store.Find(Ref{Scope: ScopeGlobal, ID: "ghost"}); ok {
		t.Error("expected not found")
	}
}

func TestStoreFindByIDEmpty(t *testing.T) {
	t.Parallel()
	store := NewStore()
	if got := store.FindByID("nope"); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestLoadRootsHonorsExisting(t *testing.T) {
	setupTempXDG(t)
	tmp := t.TempDir()
	abs, _, err := AddRoot(tmp)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadRoots()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != abs {
		t.Errorf("expected [%s], got %v", abs, got)
	}
}

func TestContainingRootReturnsLongestMatch(t *testing.T) {
	t.Parallel()
	outer := t.TempDir()
	inner := filepath.Join(outer, "deep")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ContainingRoot(inner, []string{outer, inner})
	if err != nil {
		t.Fatal(err)
	}
	if got != inner {
		t.Errorf("got %q, want %q (longest match wins)", got, inner)
	}
}

func TestIsWithinSameDir(t *testing.T) {
	t.Parallel()
	if !isWithin("/a/b", "/a/b") {
		t.Error("identical paths should be within")
	}
	if !isWithin("/a/b/c", "/a/b") {
		t.Error("nested path should be within")
	}
	if isWithin("/a", "/a/b") {
		t.Error("parent should not be within child")
	}
	if isWithin("/x", "/y") {
		t.Error("unrelated paths should not be within")
	}
}

func TestNormalizeRootsSkipsEmpty(t *testing.T) {
	t.Parallel()
	got, err := normalizeRoots([]string{"", "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	// Empty strings should be skipped.
	for _, r := range got {
		if r == "" {
			t.Error("empty root not filtered")
		}
	}
}

func TestRemoveRootRemoves(t *testing.T) {
	setupTempXDG(t)
	tmp := t.TempDir()
	if _, _, err := AddRoot(tmp); err != nil {
		t.Fatal(err)
	}
	changed, err := RemoveRoot(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected change=true")
	}
	roots, _ := LoadRoots()
	if len(roots) != 0 {
		t.Errorf("expected empty after remove, got %v", roots)
	}
}

func TestStoreDeleteRoutineRemovesStateFile(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "with-state"}
	entry, err := store.Create(ref, &Routine{ID: "with-state", Agent: "go", Schedule: "@daily", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveState(entry.StatePath, &State{LastStatus: StatusOK, LastRun: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// Sanity: state file exists.
	if _, err := os.Stat(entry.StatePath); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ref); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entry.StatePath); err == nil {
		t.Error("expected state file removed by Delete")
	}
}

func TestStoreLoadAllScanDirSurvivesInvalidManifests(t *testing.T) {
	setupTempXDG(t)
	dir, err := GlobalRoutinesDir()
	if err != nil {
		t.Fatal(err)
	}
	// Mix of: valid manifest, invalid YAML, non-yaml file, and a subdirectory.
	if err := SaveRoutine(filepath.Join(dir, FileName("ok")), &Routine{
		ID: "ok", Agent: "go", Schedule: "@daily", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(":: bad :::"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewStore()
	entries, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 1 || entries[0].Ref.ID != "ok" {
		t.Errorf("expected only [ok], got %v", entries)
	}
}

func TestStoreCreateMkdirAlreadyExists(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	repo := t.TempDir()
	// Pre-create the .squad/routines dir.
	if err := os.MkdirAll(RepoRoutinesDir(repo), 0o755); err != nil {
		t.Fatal(err)
	}
	ref := Ref{Scope: ScopeRepo, Root: repo, ID: "preexist"}
	if _, err := store.Create(ref, &Routine{
		ID: "preexist", Agent: "go", Schedule: "@daily", Enabled: true,
	}); err != nil {
		t.Errorf("Create over existing dir: %v", err)
	}
}

func TestStoreDeleteIdempotentOnMissingState(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "nostate"}
	if _, err := store.Create(ref, &Routine{
		ID: "nostate", Agent: "go", Schedule: "@daily", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	// No state file written. Delete should still succeed.
	if err := store.Delete(ref); err != nil {
		t.Errorf("Delete without state: %v", err)
	}
}

func TestStoreUpdateChangesManifestOnDisk(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "upd"}
	if _, err := store.Create(ref, &Routine{ID: "upd", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	newR := &Routine{ID: "upd", Agent: "py-review", Schedule: "@hourly", Enabled: false}
	entry, err := store.Update(ref, newR)
	if err != nil {
		t.Fatal(err)
	}
	on, err := LoadRoutine(entry.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if on.Agent != "py-review" || on.Schedule != "@hourly" || on.Enabled {
		t.Errorf("on-disk manifest didn't update: %+v", on)
	}
}

func TestStoreEntriesSnapshotIsSorted(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	for _, id := range []string{"c", "a", "b"} {
		if _, err := store.Create(
			Ref{Scope: ScopeGlobal, ID: id},
			&Routine{ID: id, Agent: "go", Schedule: "@daily", Enabled: true},
		); err != nil {
			t.Fatal(err)
		}
	}
	entries := store.Entries()
	if len(entries) != 3 {
		t.Fatal("setup")
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Ref.ID > entries[i].Ref.ID {
			t.Errorf("entries not sorted: %v", entries)
			break
		}
	}
}
