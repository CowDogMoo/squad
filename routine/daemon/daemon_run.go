package daemon

// This file holds the daemon's blocking lifecycle: signal handling, watch
// goroutine spawn, scheduler start, and the ctx.Done-driven shutdown.
//
// Codecov ignores this file because its semantics are wall-clock and
// signal-driven — unit tests can only assert that we don't deadlock, which
// is already covered by the higher-level Run tests in daemon_test.go.
// Real confidence comes from `squad routined` integration tests.

import (
	"context"
	"fmt"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/routine"
)

// runLifecycle is the goroutine + signal-aware half of Run. It assumes opts
// has already been validated and normalized by Run.
func runLifecycle(ctx context.Context, cfg *config.Config, opts Options) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, sched, err := newStoreAndScheduler(cfg, opts)
	if err != nil {
		return err
	}
	logging.Info("routined: %d routine(s) loaded", len(store.Entries()))

	// Catch up on fires missed while the daemon was not running. Each missed
	// routine fires exactly once (CatchupFireOnce); catchup respects
	// per-routine skip policy.
	if misses := routine.FindMissedFires(store, time.Now()); len(misses) > 0 {
		logging.Info("routined: catching up %d missed fire(s)", len(misses))
		routine.QueueCatchups(ctx, sched, misses)
	}

	// Watch for manifest changes and feed them to the scheduler.
	events, err := store.Watch(ctx, 32)
	if err != nil {
		return fmt.Errorf("watch: %w", err)
	}
	var watcherDone sync.WaitGroup
	watcherDone.Add(1)
	go func() {
		defer watcherDone.Done()
		for ev := range events {
			logging.Info("routined: %s %s", eventTypeName(ev.Type), ev.Ref.Qualified())
			sched.ApplyEvent(ev)
		}
	}()

	sched.Start()
	logging.Info("routined: scheduler started")
	<-ctx.Done()
	logging.Info("routined: shutting down")
	// Wait for the event-watcher goroutine to drain before shutting down the
	// scheduler, so no ApplyEvent call races against a stopped gocron instance.
	watcherDone.Wait()
	shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return sched.Shutdown(shutCtx)
}
