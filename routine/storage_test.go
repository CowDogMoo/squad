package routine

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newRoutine(id string) *Routine {
	return &Routine{
		ID:       id,
		Agent:    "go-review",
		Schedule: "@daily",
		Enabled:  true,
	}
}

func TestStoreCreateAndFind(t *testing.T) {
	setupTempXDG(t)
	s := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "nightly"}
	if _, err := s.Create(ref, newRoutine("nightly")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, ok := s.Find(ref)
	if !ok {
		t.Fatal("Find: not found after Create")
	}
	if got.Routine.ID != "nightly" {
		t.Errorf("got %q", got.Routine.ID)
	}
	// Manifest exists on disk.
	if !pathExists(got.ManifestPath) {
		t.Errorf("manifest missing: %s", got.ManifestPath)
	}
}

func TestStoreCreateRejectsDuplicate(t *testing.T) {
	setupTempXDG(t)
	s := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "nightly"}
	if _, err := s.Create(ref, newRoutine("nightly")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := s.Create(ref, newRoutine("nightly")); err == nil {
		t.Error("expected error on duplicate Create")
	}
}

func TestStoreDelete(t *testing.T) {
	setupTempXDG(t)
	s := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "nightly"}
	entry, err := s.Create(ref, newRoutine("nightly"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Also write a state file so we can verify it's removed.
	if err := SaveState(entry.StatePath, &State{LastStatus: StatusOK, LastRun: time.Now()}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if err := s.Delete(ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if pathExists(entry.ManifestPath) {
		t.Error("manifest not removed")
	}
	if pathExists(entry.StatePath) {
		t.Error("state not removed")
	}
}

func TestStoreLoadAllSplitsScopes(t *testing.T) {
	setupTempXDG(t)
	repo := t.TempDir()
	if _, _, err := AddRoot(repo); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	if err := makeDir(RepoRoutinesDir(repo)); err != nil {
		t.Fatal(err)
	}

	s := NewStore()
	if _, err := s.Create(Ref{Scope: ScopeGlobal, ID: "g-one"}, newRoutine("g-one")); err != nil {
		t.Fatalf("create global: %v", err)
	}
	if _, err := s.Create(Ref{Scope: ScopeRepo, Root: repo, ID: "r-one"}, newRoutine("r-one")); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	// Drop in-memory cache and reload from disk.
	fresh := NewStore()
	entries, err := fresh.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(entries), entries)
	}
	byScope := map[Scope]Entry{}
	for _, e := range entries {
		byScope[e.Ref.Scope] = e
	}
	if _, ok := byScope[ScopeGlobal]; !ok {
		t.Error("missing global")
	}
	if r, ok := byScope[ScopeRepo]; !ok {
		t.Error("missing repo")
	} else if r.Ref.Root != repo {
		t.Errorf("repo root: got %q want %q", r.Ref.Root, repo)
	}
}

func TestStoreFindByID(t *testing.T) {
	setupTempXDG(t)
	repo := t.TempDir()
	if _, _, err := AddRoot(repo); err != nil {
		t.Fatal(err)
	}

	s := NewStore()
	if _, err := s.Create(Ref{Scope: ScopeGlobal, ID: "audit"}, newRoutine("audit")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Ref{Scope: ScopeRepo, Root: repo, ID: "audit"}, newRoutine("audit")); err != nil {
		t.Fatal(err)
	}
	got := s.FindByID("audit")
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(got))
	}
}

func TestStoreUpdate(t *testing.T) {
	setupTempXDG(t)
	s := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "nightly"}
	if _, err := s.Create(ref, newRoutine("nightly")); err != nil {
		t.Fatal(err)
	}
	r := newRoutine("nightly")
	r.Schedule = "@hourly"
	entry, err := s.Update(ref, r)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if entry.Routine.Schedule != "@hourly" {
		t.Errorf("schedule not updated: %q", entry.Routine.Schedule)
	}
	reloaded, err := LoadRoutine(entry.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Schedule != "@hourly" {
		t.Errorf("on-disk schedule not updated: %q", reloaded.Schedule)
	}
}

func TestStoreWatchEmitsAddAndRemove(t *testing.T) {
	setupTempXDG(t)
	s := NewStore()
	if _, err := s.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := s.Watch(ctx, 8)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Create a global routine directly on disk (bypasses Store.Create) to
	// exercise the fsnotify path. The watcher should detect it and emit Added.
	dir, _ := GlobalRoutinesDir()
	path := filepath.Join(dir, FileName("watched"))
	if err := SaveRoutine(path, newRoutine("watched")); err != nil {
		t.Fatalf("SaveRoutine: %v", err)
	}
	if !waitForEvent(events, EventAdded, "watched", 3*time.Second) {
		t.Fatal("did not receive Added event in time")
	}

	// Now remove and expect a Removed event.
	if err := removeFile(path); err != nil {
		t.Fatal(err)
	}
	if !waitForEvent(events, EventRemoved, "watched", 3*time.Second) {
		t.Fatal("did not receive Removed event in time")
	}
}

func waitForEvent(ch <-chan Event, want EventType, id string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return false
			}
			if ev.Type == want && ev.Ref.ID == id {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func removeFile(path string) error {
	return removeOSFile(path)
}

func TestStoreUpdateNotFound(t *testing.T) {
	setupTempXDG(t)
	s := NewStore()
	if _, err := s.Update(Ref{Scope: ScopeGlobal, ID: "ghost"}, newRoutine("ghost")); err == nil {
		t.Error("expected error updating non-existent routine")
	}
}

func TestStoreUpdateRejectsIDMismatch(t *testing.T) {
	setupTempXDG(t)
	s := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "a"}
	if _, err := s.Create(ref, newRoutine("a")); err != nil {
		t.Fatal(err)
	}
	// Pass a routine whose ID doesn't match the ref's ID — renames forbidden.
	if _, err := s.Update(ref, newRoutine("b")); err == nil {
		t.Error("expected error on id-mismatch update")
	}
}

func TestStoreCreateRejectsIDMismatch(t *testing.T) {
	setupTempXDG(t)
	s := NewStore()
	if _, err := s.Create(Ref{Scope: ScopeGlobal, ID: "x"}, newRoutine("y")); err == nil {
		t.Error("expected error: ref id != routine id")
	}
}

func TestResolveDirsRepoNeedsRoot(t *testing.T) {
	setupTempXDG(t)
	if _, _, err := resolveDirs(Ref{Scope: ScopeRepo, ID: "x"}); err == nil {
		t.Error("expected error when repo-scoped ref has no Root")
	}
}

func TestResolveDirsInvalidScope(t *testing.T) {
	setupTempXDG(t)
	if _, _, err := resolveDirs(Ref{Scope: Scope("bogus"), ID: "x"}); err == nil {
		t.Error("expected error for invalid scope")
	}
}

func TestStoreLoadAllSkipsInvalidManifest(t *testing.T) {
	setupTempXDG(t)
	dir, err := GlobalRoutinesDir()
	if err != nil {
		t.Fatal(err)
	}
	// Drop a manifest-named file containing garbage so scanDir's LoadRoutine
	// branch errors and we exercise the warn-and-skip path.
	if err := writeFile(filepath.Join(dir, "bad.yaml"), []byte("::: not yaml :::")); err != nil {
		t.Fatal(err)
	}
	// Also a valid routine alongside so we know loading didn't stop.
	if _, err := NewStore().Create(Ref{Scope: ScopeGlobal, ID: "good"}, newRoutine("good")); err != nil {
		t.Fatal(err)
	}
	entries, err := NewStore().LoadAll()
	if err != nil {
		t.Fatalf("LoadAll should not error on invalid manifest: %v", err)
	}
	if len(entries) != 1 || entries[0].Ref.ID != "good" {
		t.Errorf("expected only the valid routine, got %v", entries)
	}
}
