package routine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/telemetry"
	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// FireFn invokes a single routine fire. The daemon implementation wires this
// to runner.ExecuteRun against the routine's working directory.
//
// The returned error becomes the failure status in the state file. Returning
// nil produces StatusOK. The session id is reported back via SessionID so the
// state file can record it.
type FireFn func(ctx context.Context, entry Entry) (sessionID string, err error)

// SchedulerOptions tunes how the routine scheduler runs jobs.
type SchedulerOptions struct {
	// MaxConcurrent caps the total number of in-flight routine fires across
	// the whole daemon. 0 = unlimited (gocron default).
	MaxConcurrent uint
	// FireTimeout, if non-zero, is the maximum wall-clock time a single
	// fire is allowed to run before the context is cancelled.
	FireTimeout time.Duration
}

// Scheduler glues a Store to a gocron scheduler. Routines that are Enabled
// and have a valid schedule become gocron jobs; per-routine singleton mode
// suppresses overlapping fires so the cost budget can't be blown by a
// long-running run colliding with its own next fire.
type Scheduler struct {
	mu        sync.Mutex
	gocron    gocron.Scheduler
	store     *Store
	fire      FireFn
	jobIDs    map[string]uuid.UUID           // entryKey -> gocron job UUID
	stateSink func(ref Ref, st *State) error // overridable for tests; defaults to store.SaveState
	timeout   time.Duration
}

// NewScheduler constructs a scheduler bound to a store. fire is invoked for
// every fire; the scheduler updates the routine's state file before and
// after each call.
func NewScheduler(store *Store, fire FireFn, opts SchedulerOptions) (*Scheduler, error) {
	if store == nil {
		return nil, errors.New("nil store")
	}
	if fire == nil {
		return nil, errors.New("nil fire fn")
	}

	gopts := []gocron.SchedulerOption{}
	if opts.MaxConcurrent > 0 {
		gopts = append(gopts, gocron.WithLimitConcurrentJobs(opts.MaxConcurrent, gocron.LimitModeWait))
	}
	gs, err := gocron.NewScheduler(gopts...)
	if err != nil {
		return nil, fmt.Errorf("create scheduler: %w", err)
	}

	s := &Scheduler{
		gocron:  gs,
		store:   store,
		fire:    fire,
		jobIDs:  make(map[string]uuid.UUID),
		timeout: opts.FireTimeout,
	}
	s.stateSink = s.store.SaveState
	return s, nil
}

// Start begins the underlying gocron scheduler. Sync should be called
// at least once before Start; otherwise no jobs will fire.
func (s *Scheduler) Start() {
	s.gocron.Start()
}

// Shutdown stops the scheduler and waits for in-flight fires to finish.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	return s.gocron.ShutdownWithContext(ctx)
}

// Sync reconciles the set of scheduled jobs against the entries provided.
// New entries are added, removed entries have their jobs cancelled, and
// changed entries (schedule, agent, vars, etc.) are re-scheduled.
func (s *Scheduler) Sync(entries []Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	wanted := make(map[string]Entry, len(entries))
	for _, e := range entries {
		if !e.Routine.Enabled {
			continue
		}
		wanted[entryKey(e.Ref)] = e
	}

	// Remove jobs for entries that no longer exist or are disabled.
	for key, id := range s.jobIDs {
		if _, keep := wanted[key]; !keep {
			if err := s.gocron.RemoveJob(id); err != nil && !errors.Is(err, gocron.ErrJobNotFound) {
				logging.Warn("remove job %s: %v", key, err)
			}
			delete(s.jobIDs, key)
		}
	}

	// Add/update jobs for wanted entries.
	for key, entry := range wanted {
		if existingID, have := s.jobIDs[key]; have {
			// Replace the existing job — gocron does not expose an in-place
			// reschedule API for cron strings, so remove + re-add. We accept
			// the brief gap where the job is unscheduled because Sync runs
			// off the watcher's debounced reload, not on every keystroke.
			if err := s.gocron.RemoveJob(existingID); err != nil && !errors.Is(err, gocron.ErrJobNotFound) {
				logging.Warn("replace job %s: %v", key, err)
			}
			delete(s.jobIDs, key)
		}
		id, err := s.addJob(entry)
		if err != nil {
			logging.Warn("schedule routine %s: %v", entry.Ref.Qualified(), err)
			continue
		}
		s.jobIDs[key] = id
	}
	return nil
}

// RunNow immediately fires the routine identified by ref without disturbing
// its scheduled cadence. The fire happens on the caller's goroutine so the
// returned error reflects this specific run.
func (s *Scheduler) RunNow(ctx context.Context, ref Ref) error {
	entry, ok := s.store.Find(ref)
	if !ok {
		return fmt.Errorf("routine %s not found", ref.Qualified())
	}
	return s.runFire(ctx, entry)
}

// ApplyEvent updates the scheduler in response to a store watcher event.
func (s *Scheduler) ApplyEvent(ev Event) {
	switch ev.Type {
	case EventRemoved:
		s.mu.Lock()
		key := entryKey(ev.Ref)
		if id, ok := s.jobIDs[key]; ok {
			if err := s.gocron.RemoveJob(id); err != nil && !errors.Is(err, gocron.ErrJobNotFound) {
				logging.Warn("remove job %s: %v", key, err)
			}
			delete(s.jobIDs, key)
		}
		s.mu.Unlock()
	case EventAdded, EventUpdated:
		// Reuse Sync to keep one code path for both.
		s.mu.Lock()
		entries := s.store.Entries()
		s.mu.Unlock()
		if err := s.Sync(entries); err != nil {
			logging.Warn("sync after %v: %v", ev.Type, err)
		}
	}
}

func (s *Scheduler) addJob(entry Entry) (uuid.UUID, error) {
	def := gocron.CronJob(entry.Routine.Schedule, false)
	task := gocron.NewTask(func() {
		ctx, cancel := s.fireContext(context.Background())
		defer cancel()
		if err := s.runFire(ctx, entry); err != nil {
			logging.Warn("routine %s fire failed: %v", entry.Ref.Qualified(), err)
		}
	})
	job, err := s.gocron.NewJob(def, task,
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
		gocron.WithTags(entry.Ref.Qualified()),
	)
	if err != nil {
		return uuid.UUID{}, err
	}
	return job.ID(), nil
}

func (s *Scheduler) fireContext(parent context.Context) (context.Context, context.CancelFunc) {
	if s.timeout > 0 {
		return context.WithTimeout(parent, s.timeout)
	}
	return context.WithCancel(parent)
}

// runFire invokes the fire function with proper state-file bookkeeping. It
// is exposed via RunNow and used by every scheduled fire.
func (s *Scheduler) runFire(ctx context.Context, entry Entry) error {
	ctx, span := telemetry.Tracer().Start(ctx, "routine.fire",
		trace.WithAttributes(
			attribute.String("squad.routine.id", entry.Ref.ID),
			attribute.String("squad.routine.scope", string(entry.Ref.Scope)),
			attribute.String("squad.routine.qualified", entry.Ref.Qualified()),
			attribute.String("squad.routine.agent", entry.Routine.Agent),
			attribute.String("squad.routine.schedule", entry.Routine.Schedule),
		),
	)
	defer span.End()

	logging.Info("routine %s firing (agent=%s)", entry.Ref.Qualified(), entry.Routine.Agent)
	start := time.Now()
	if err := s.stateSink(entry.Ref, &State{LastStatus: StatusRunning, LastRun: start}); err != nil {
		logging.Warn("save running state for %s: %v", entry.Ref.Qualified(), err)
	}

	sessionID, runErr := s.fire(ctx, entry)
	dur := time.Since(start)

	st := &State{
		LastRun:        start,
		LastSessionID:  sessionID,
		LastDurationMs: dur.Milliseconds(),
	}
	if runErr != nil {
		st.LastStatus = StatusFailed
		st.LastError = runErr.Error()
		span.RecordError(runErr)
		span.SetStatus(codes.Error, runErr.Error())
	} else {
		st.LastStatus = StatusOK
		span.SetStatus(codes.Ok, "")
	}
	if sessionID != "" {
		span.SetAttributes(attribute.String("squad.session.id", sessionID))
	}
	span.SetAttributes(
		attribute.String("squad.routine.status", string(st.LastStatus)),
		attribute.Int64("squad.routine.duration_ms", st.LastDurationMs),
	)
	if err := s.stateSink(entry.Ref, st); err != nil {
		logging.Warn("save state for %s: %v", entry.Ref.Qualified(), err)
	}
	return runErr
}

// JobIDs returns a copy of the (ref -> gocron job uuid) map for inspection.
// Primarily useful in tests.
func (s *Scheduler) JobIDs() map[string]uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]uuid.UUID, len(s.jobIDs))
	for k, v := range s.jobIDs {
		out[k] = v
	}
	return out
}
