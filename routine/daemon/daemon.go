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
	"path/filepath"
	"time"

	"github.com/cowdogmoo/squad/config"
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

// Run owns the daemon's lifecycle: validate inputs, build store + scheduler,
// catch up on missed fires, then hand off to runLoop (which spawns the
// watch goroutine and blocks on ctx). Splitting the entrypoint keeps the
// testable validation/setup in this file; the lifecycle wrapper lives in
// daemon_run.go and is excluded from coverage because its assertion
// surface is goroutines + signals.
func Run(ctx context.Context, cfg *config.Config, opts Options) error {
	if cfg == nil {
		return fmt.Errorf("daemon: nil config")
	}
	applyOptions(&opts)
	return runLifecycle(ctx, cfg, opts)
}

// applyOptions normalizes the Options struct in place, filling in defaults.
// Exposed so Run's validation path is unit-testable without spawning the
// lifecycle goroutines.
func applyOptions(opts *Options) {
	if opts.MaxConcurrent == 0 {
		opts.MaxConcurrent = 2
	}
}

// newStoreAndScheduler constructs the Store + Scheduler the daemon will own
// for its lifetime. Returned objects are owned by the caller; failures here
// are setup-time and don't require goroutine teardown.
func newStoreAndScheduler(cfg *config.Config, opts Options) (*routine.Store, *routine.Scheduler, error) {
	store := routine.NewStore()
	if _, err := store.LoadAll(); err != nil {
		return nil, nil, fmt.Errorf("load routines: %w", err)
	}
	fire := BuildFireFn(cfg)
	sched, err := routine.NewScheduler(store, fire, routine.SchedulerOptions{
		MaxConcurrent: opts.MaxConcurrent,
		FireTimeout:   opts.FireTimeout,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("scheduler: %w", err)
	}
	if err := sched.Sync(store.Entries()); err != nil {
		return nil, nil, fmt.Errorf("initial sync: %w", err)
	}
	return store, sched, nil
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
		Provider:        r.Provider,
		Model:           r.Model,
		BaseURL:         r.BaseURL,
		ConfigProvider:  cfg.Provider.Default,
		ConfigModel:     cfg.Model.Default,
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
