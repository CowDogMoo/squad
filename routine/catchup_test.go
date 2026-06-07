package routine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFindMissedFiresFireOnce(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "hourly"}
	entry, err := store.Create(ref, &Routine{
		ID:       "hourly",
		Agent:    "go",
		Schedule: "@every 1h",
		Enabled:  true,
		Catchup:  CatchupFireOnce,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Record a last run 3 hours ago.
	now := time.Now().UTC()
	last := now.Add(-3 * time.Hour)
	if err := SaveState(entry.StatePath, &State{LastRun: last, LastStatus: StatusOK}); err != nil {
		t.Fatal(err)
	}
	misses := FindMissedFires(store, now)
	if len(misses) != 1 {
		t.Fatalf("expected 1 missed, got %d", len(misses))
	}
	if misses[0].Ref.ID != "hourly" {
		t.Errorf("wrong ref: %v", misses[0].Ref)
	}
	if misses[0].Missed.After(now) || misses[0].Missed.Before(last) {
		t.Errorf("missed time %v out of expected range [%v, %v]", misses[0].Missed, last, now)
	}
}

func TestFindMissedFiresSkipPolicy(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "skipme"}
	entry, err := store.Create(ref, &Routine{
		ID:       "skipme",
		Agent:    "go",
		Schedule: "@every 1h",
		Enabled:  true,
		Catchup:  CatchupSkip,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := SaveState(entry.StatePath, &State{LastRun: now.Add(-3 * time.Hour), LastStatus: StatusOK}); err != nil {
		t.Fatal(err)
	}
	misses := FindMissedFires(store, now)
	if len(misses) != 0 {
		t.Errorf("skip policy should suppress missed fires, got %v", misses)
	}
}

func TestFindMissedFiresNoMissedFire(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "recent"}
	entry, err := store.Create(ref, &Routine{
		ID:       "recent",
		Agent:    "go",
		Schedule: "@every 1h",
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// Last run 5 minutes ago, schedule fires every 1h → no missed fires.
	if err := SaveState(entry.StatePath, &State{LastRun: now.Add(-5 * time.Minute), LastStatus: StatusOK}); err != nil {
		t.Fatal(err)
	}
	misses := FindMissedFires(store, now)
	if len(misses) != 0 {
		t.Errorf("expected no misses, got %v", misses)
	}
}

func TestFindMissedFiresDisabledRoutineIgnored(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "off"}
	entry, err := store.Create(ref, &Routine{
		ID:       "off",
		Agent:    "go",
		Schedule: "@every 1h",
		Enabled:  false,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := SaveState(entry.StatePath, &State{LastRun: now.Add(-3 * time.Hour), LastStatus: StatusOK}); err != nil {
		t.Fatal(err)
	}
	if got := FindMissedFires(store, now); len(got) != 0 {
		t.Errorf("disabled routine should not be a miss, got %v", got)
	}
}

func TestFindMissedFiresNeverRanUsesCreatedAt(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "fresh"}
	created := time.Now().UTC().Add(-3 * time.Hour)
	if _, err := store.Create(ref, &Routine{
		ID:        "fresh",
		Agent:     "go",
		Schedule:  "@every 1h",
		Enabled:   true,
		CreatedAt: created,
	}); err != nil {
		t.Fatal(err)
	}
	// No state file — never fired.
	misses := FindMissedFires(store, time.Now().UTC())
	if len(misses) != 1 {
		t.Fatalf("expected 1 missed fire derived from CreatedAt, got %d", len(misses))
	}
}

func TestQueueCatchupsFiresThroughScheduler(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "queued"}
	if _, err := store.Create(ref, &Routine{
		ID:       "queued",
		Agent:    "go",
		Schedule: "@every 1h",
		Enabled:  true,
	}); err != nil {
		t.Fatal(err)
	}

	var fired atomic.Int32
	done := make(chan struct{})
	var once sync.Once
	fire := func(_ context.Context, _ Entry) (string, error) {
		fired.Add(1)
		once.Do(func() { close(done) })
		return "sess", nil
	}
	sched, _ := NewScheduler(store, fire, SchedulerOptions{})

	QueueCatchups(context.Background(), sched, []MissedFire{{Ref: ref, Missed: time.Now()}})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("catchup fire did not happen")
	}
	if fired.Load() != 1 {
		t.Errorf("expected 1 fire, got %d", fired.Load())
	}
}
