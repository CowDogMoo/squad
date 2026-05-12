// Package daemon implements the squad routines daemon's runtime loop —
// loading routines, wiring the scheduler, catching up on missed fires,
// watching for manifest changes, and firing agents.
//
// The package is consumed by two callers:
//
//   - `cmd/squad` (subcommand `routined`) for "just run the squad binary as
//     the daemon" — what launchd / systemd use by default.
//   - `cmd/squad-routined` (Windows GUI-subsystem binary) so Task Scheduler
//     doesn't flash a console window on every restart.
//
// Both invocations end up calling Run with the same loaded config.
package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/routine"
	"github.com/cowdogmoo/squad/runner"
	"github.com/spf13/cobra"
)

// Options tune the daemon's runtime behaviour. The zero value runs with
// sensible defaults.
type Options struct {
	// MaxConcurrent caps simultaneous in-flight fires across the daemon.
	// Defaults to 2.
	MaxConcurrent uint
	// FireTimeout, if non-zero, applies a wall-clock deadline to each fire.
	FireTimeout time.Duration
}

// Run owns the daemon's lifecycle: build store + scheduler, catch up on
// missed fires, then run until ctx is cancelled or SIGINT/SIGTERM arrive.
func Run(ctx context.Context, cfg *config.Config, opts Options) error {
	if cfg == nil {
		return fmt.Errorf("daemon: nil config")
	}
	if opts.MaxConcurrent == 0 {
		opts.MaxConcurrent = 2
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store := routine.NewStore()
	if _, err := store.LoadAll(); err != nil {
		return fmt.Errorf("load routines: %w", err)
	}
	entries := store.Entries()
	logging.Info("routined: %d routine(s) loaded", len(entries))

	fire := BuildFireFn(cfg)
	sched, err := routine.NewScheduler(store, fire, routine.SchedulerOptions{
		MaxConcurrent: opts.MaxConcurrent,
		FireTimeout:   opts.FireTimeout,
	})
	if err != nil {
		return fmt.Errorf("scheduler: %w", err)
	}
	if err := sched.Sync(entries); err != nil {
		return fmt.Errorf("initial sync: %w", err)
	}

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
	go func() {
		for ev := range events {
			logging.Info("routined: %s %s", eventTypeName(ev.Type), ev.Ref.Qualified())
			sched.ApplyEvent(ev)
		}
	}()

	sched.Start()
	logging.Info("routined: scheduler started")
	<-ctx.Done()
	logging.Info("routined: shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return sched.Shutdown(shutCtx)
}

// RedirectStdio appends-opens path and points os.Stdout/os.Stderr at it.
// Used on Windows where Task Scheduler has no native stdio redirection like
// launchd's StandardOutPath or systemd's StandardOutput=journal.
func RedirectStdio(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	os.Stdout = f
	os.Stderr = f
	return nil
}

// BuildFireFn returns a routine.FireFn that invokes runner.ExecuteRun against
// the squad agent system for one routine fire. Exposed so tests and
// alternative daemon entrypoints can construct the same fire path.
func BuildFireFn(cfg *config.Config) routine.FireFn {
	return func(ctx context.Context, entry routine.Entry) (string, error) {
		workingDir := resolveWorkingDir(entry)
		opts := newRunOptions(entry, cfg, workingDir)

		// Synthesize a cobra.Command so we can hand runner.ExecuteRun the same
		// surface it expects from the CLI path. stdin is a closed buffer (no
		// piped input); stdout/stderr go to discard so daemon-side fires
		// don't spam the terminal — sessions remain the durable record.
		synth := &cobra.Command{Use: "routine-fire"}
		synth.SetContext(ctx)
		synth.SetIn(bytes.NewReader(nil))
		synth.SetOut(io.Discard)
		synth.SetErr(io.Discard)

		args := []string{}
		if entry.Routine.Prompt != "" {
			args = []string{entry.Routine.Prompt}
		}
		err := runner.ExecuteRun(synth, args, opts)
		return opts.LastSessionID, err
	}
}

func newRunOptions(entry routine.Entry, cfg *config.Config, workingDir string) *runner.RunOptions {
	r := entry.Routine
	return &runner.RunOptions{
		Agent:           r.Agent,
		WorkingDir:      workingDir,
		Provider:        firstNonEmpty(r.Provider, cfg.Provider.Default),
		Model:           firstNonEmpty(r.Model, cfg.Model.Default),
		Temperature:     cfg.Model.Temperature,
		MaxTokens:       cfg.Model.MaxTokens,
		NumCtx:          cfg.Provider.NumCtx,
		Vars:            r.Vars,
		ConfigAvailable: true,
		Config:          cfg,
		Print:           false,
		MaxIterations:   firstNonZeroInt(r.MaxIterations, 100),
		MaxCost:         firstNonZeroFloat(r.MaxCost, 5.0),
		RoutineID:       entry.Ref.Qualified(),
	}
}

func resolveWorkingDir(entry routine.Entry) string {
	if entry.Routine.WorkingDir != "" {
		return entry.Routine.WorkingDir
	}
	if entry.Ref.Scope == routine.ScopeRepo {
		return entry.Ref.Root
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZeroInt(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func firstNonZeroFloat(values ...float64) float64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func eventTypeName(t routine.EventType) string {
	switch t {
	case routine.EventAdded:
		return "added"
	case routine.EventUpdated:
		return "updated"
	case routine.EventRemoved:
		return "removed"
	default:
		return "unknown"
	}
}
