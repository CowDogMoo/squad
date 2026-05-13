package routine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerSyncAddsEnabled(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	if _, err := store.Create(Ref{Scope: ScopeGlobal, ID: "a"}, &Routine{ID: "a", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(Ref{Scope: ScopeGlobal, ID: "b"}, &Routine{ID: "b", Agent: "go", Schedule: "@daily", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	sched, err := NewScheduler(store, neverFires(), SchedulerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.Sync(store.Entries()); err != nil {
		t.Fatal(err)
	}
	jobs := sched.JobIDs()
	if len(jobs) != 1 {
		t.Errorf("expected 1 scheduled job (a only), got %d", len(jobs))
	}
}

func TestSchedulerSyncRemovesAndUpdates(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "a"}
	if _, err := store.Create(ref, &Routine{ID: "a", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	sched, err := NewScheduler(store, neverFires(), SchedulerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.Sync(store.Entries()); err != nil {
		t.Fatal(err)
	}
	firstJobID := sched.JobIDs()[entryKey(ref)]

	// Update schedule — should reschedule with a new UUID.
	if _, err := store.Update(ref, &Routine{ID: "a", Agent: "go", Schedule: "@hourly", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := sched.Sync(store.Entries()); err != nil {
		t.Fatal(err)
	}
	secondJobID := sched.JobIDs()[entryKey(ref)]
	if firstJobID == secondJobID {
		t.Error("expected new job UUID after schedule change")
	}

	// Disable — should remove the job.
	if _, err := store.Update(ref, &Routine{ID: "a", Agent: "go", Schedule: "@hourly", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := sched.Sync(store.Entries()); err != nil {
		t.Fatal(err)
	}
	if _, has := sched.JobIDs()[entryKey(ref)]; has {
		t.Error("expected no job for disabled routine")
	}
}

func TestSchedulerRunNowInvokesFireAndPersistsState(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "now"}
	entry, err := store.Create(ref, &Routine{ID: "now", Agent: "go", Schedule: "@daily", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	var fired atomic.Int32
	fire := func(_ context.Context, e Entry) (string, error) {
		fired.Add(1)
		if e.Ref.ID != "now" {
			t.Errorf("got fire for %s", e.Ref.ID)
		}
		return "sess-xyz", nil
	}
	sched, err := NewScheduler(store, fire, SchedulerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.RunNow(context.Background(), ref); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if fired.Load() != 1 {
		t.Errorf("expected 1 fire, got %d", fired.Load())
	}

	st, err := LoadState(entry.StatePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st.LastStatus != StatusOK {
		t.Errorf("status: got %q want %q", st.LastStatus, StatusOK)
	}
	if st.LastSessionID != "sess-xyz" {
		t.Errorf("session id: got %q", st.LastSessionID)
	}
	if st.LastRun.IsZero() {
		t.Error("last_run not recorded")
	}
}

func TestSchedulerRunNowRecordsFailure(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "fail"}
	entry, err := store.Create(ref, &Routine{ID: "fail", Agent: "go", Schedule: "@daily", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	fire := func(_ context.Context, _ Entry) (string, error) {
		return "sess-fail", errors.New("kaboom")
	}
	sched, err := NewScheduler(store, fire, SchedulerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.RunNow(context.Background(), ref); err == nil {
		t.Error("expected error from RunNow")
	}
	st, err := LoadState(entry.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastStatus != StatusFailed {
		t.Errorf("status: got %q want %q", st.LastStatus, StatusFailed)
	}
	if st.LastError == "" {
		t.Error("error message not recorded")
	}
}

func TestSchedulerFiresOnInterval(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "fast"}
	if _, err := store.Create(ref, &Routine{ID: "fast", Agent: "go", Schedule: "@every 200ms", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		count int
	)
	done := make(chan struct{})
	fire := func(_ context.Context, _ Entry) (string, error) {
		mu.Lock()
		count++
		if count == 2 {
			close(done)
		}
		mu.Unlock()
		return "s", nil
	}
	sched, err := NewScheduler(store, fire, SchedulerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.Sync(store.Entries()); err != nil {
		t.Fatal(err)
	}
	sched.Start()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sched.Shutdown(shutCtx)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		mu.Lock()
		got := count
		mu.Unlock()
		t.Fatalf("expected 2 fires within 2s, got %d", got)
	}
}

func TestSchedulerApplyEventRemoves(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "drop"}
	if _, err := store.Create(ref, &Routine{ID: "drop", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	sched, _ := NewScheduler(store, neverFires(), SchedulerOptions{})
	_ = sched.Sync(store.Entries())
	if len(sched.JobIDs()) != 1 {
		t.Fatal("setup")
	}
	if err := store.Delete(ref); err != nil {
		t.Fatal(err)
	}
	sched.ApplyEvent(Event{Type: EventRemoved, Ref: ref})
	if len(sched.JobIDs()) != 0 {
		t.Errorf("expected job removed, jobs=%v", sched.JobIDs())
	}
}

func neverFires() FireFn {
	return func(_ context.Context, _ Entry) (string, error) {
		return "", errors.New("should not fire in this test")
	}
}

func TestNewSchedulerRejectsNilStore(t *testing.T) {
	t.Parallel()
	if _, err := NewScheduler(nil, neverFires(), SchedulerOptions{}); err == nil {
		t.Error("expected error for nil store")
	}
}

func TestNewSchedulerRejectsNilFire(t *testing.T) {
	setupTempXDG(t)
	if _, err := NewScheduler(NewStore(), nil, SchedulerOptions{}); err == nil {
		t.Error("expected error for nil fire fn")
	}
}

func TestSchedulerApplyEventAddedTriggersSync(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	sched, err := NewScheduler(store, neverFires(), SchedulerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// No jobs scheduled yet.
	if len(sched.JobIDs()) != 0 {
		t.Fatal("expected no initial jobs")
	}
	ref := Ref{Scope: ScopeGlobal, ID: "addsync"}
	if _, err := store.Create(ref, &Routine{ID: "addsync", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	sched.ApplyEvent(Event{Type: EventAdded, Ref: ref})
	if _, has := sched.JobIDs()[entryKey(ref)]; !has {
		t.Error("EventAdded should trigger Sync and schedule the new job")
	}
}

func TestSchedulerStateSinkErrorsDoNotMaskFire(t *testing.T) {
	setupTempXDG(t)
	store := NewStore()
	ref := Ref{Scope: ScopeGlobal, ID: "sinkfail"}
	if _, err := store.Create(ref, &Routine{ID: "sinkfail", Agent: "go", Schedule: "@daily", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	fireCount := 0
	fire := func(_ context.Context, _ Entry) (string, error) {
		fireCount++
		return "s1", nil
	}
	sched, err := NewScheduler(store, fire, SchedulerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Swap in a stateSink that always errors so both Warn branches in runFire
	// execute (the pre-fire "running" save and the post-fire final save).
	sched.stateSink = func(_ Ref, _ *State) error {
		return errors.New("disk full")
	}
	if err := sched.RunNow(context.Background(), ref); err != nil {
		t.Fatalf("RunNow returned error despite sink-only failure: %v", err)
	}
	if fireCount != 1 {
		t.Errorf("expected fire to run once, got %d", fireCount)
	}
}
