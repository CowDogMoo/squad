package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/routine"
)

func TestFirstNonEmpty(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"first wins", []string{"a", "b"}, "a"},
		{"skip empties", []string{"", "", "c"}, "c"},
		{"all empty", []string{"", ""}, ""},
		{"no values", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := firstNonEmpty(c.in...); got != c.want {
				t.Errorf("firstNonEmpty(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestFirstNonZeroInt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []int
		want int
	}{
		{[]int{0, 0, 5}, 5},
		{[]int{1, 2, 3}, 1},
		{[]int{0, 0}, 0},
		{nil, 0},
	}
	for _, c := range cases {
		if got := firstNonZeroInt(c.in...); got != c.want {
			t.Errorf("firstNonZeroInt(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFirstNonZeroFloat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []float64
		want float64
	}{
		{[]float64{0, 0, 2.5}, 2.5},
		{[]float64{1.1, 2.2}, 1.1},
		{[]float64{0}, 0},
	}
	for _, c := range cases {
		if got := firstNonZeroFloat(c.in...); got != c.want {
			t.Errorf("firstNonZeroFloat(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestResolveWorkingDirExplicit(t *testing.T) {
	t.Parallel()
	entry := routine.Entry{
		Ref:     routine.Ref{Scope: routine.ScopeGlobal, ID: "x"},
		Routine: &routine.Routine{WorkingDir: "/path/to/repo"},
	}
	if got := resolveWorkingDir(entry); got != "/path/to/repo" {
		t.Errorf("got %q, want explicit path", got)
	}
}

func TestResolveWorkingDirRepoDefault(t *testing.T) {
	t.Parallel()
	entry := routine.Entry{
		Ref:     routine.Ref{Scope: routine.ScopeRepo, Root: "/repo/root", ID: "x"},
		Routine: &routine.Routine{},
	}
	if got := resolveWorkingDir(entry); got != "/repo/root" {
		t.Errorf("got %q, want repo root", got)
	}
}

func TestResolveWorkingDirGlobalUnset(t *testing.T) {
	t.Parallel()
	entry := routine.Entry{
		Ref:     routine.Ref{Scope: routine.ScopeGlobal, ID: "x"},
		Routine: &routine.Routine{},
	}
	if got := resolveWorkingDir(entry); got != "" {
		t.Errorf("global with no working_dir should yield empty, got %q", got)
	}
}

func TestEventTypeName(t *testing.T) {
	t.Parallel()
	cases := map[routine.EventType]string{
		routine.EventAdded:    "added",
		routine.EventUpdated:  "updated",
		routine.EventRemoved:  "removed",
		routine.EventType(99): "unknown",
	}
	for in, want := range cases {
		if got := eventTypeName(in); got != want {
			t.Errorf("eventTypeName(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestNewRunOptionsAppliesRoutineFields(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Provider: config.ProviderConfig{Default: "anthropic", NumCtx: 32768},
		Model:    config.ModelConfig{Default: "claude-sonnet-4-6", Temperature: 0.3, MaxTokens: 4000},
	}
	entry := routine.Entry{
		Ref: routine.Ref{Scope: routine.ScopeRepo, Root: "/repo", ID: "nightly"},
		Routine: &routine.Routine{
			Agent:         "go-review",
			Provider:      "openai", // overrides config default
			MaxCost:       3.0,
			MaxIterations: 50,
			Vars:          map[string]string{"k": "v"},
		},
	}
	opts := newRunOptions(entry, cfg, "/repo")
	if opts.Agent != "go-review" {
		t.Errorf("agent: %q", opts.Agent)
	}
	if opts.Provider != "openai" {
		t.Errorf("provider override not applied: %q", opts.Provider)
	}
	if opts.Model != "claude-sonnet-4-6" {
		t.Errorf("model from config not inherited: %q", opts.Model)
	}
	if opts.MaxCost != 3.0 {
		t.Errorf("max_cost: %v", opts.MaxCost)
	}
	if opts.MaxIterations != 50 {
		t.Errorf("max_iter: %d", opts.MaxIterations)
	}
	if opts.RoutineID != "repo:nightly" {
		t.Errorf("routine_id tag: %q", opts.RoutineID)
	}
	if opts.Vars["k"] != "v" {
		t.Errorf("vars not threaded through: %v", opts.Vars)
	}
}

func TestNewRunOptionsAppliesDefaultsForUnsetFields(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Provider: config.ProviderConfig{Default: "ollama"},
	}
	entry := routine.Entry{
		Ref:     routine.Ref{Scope: routine.ScopeGlobal, ID: "x"},
		Routine: &routine.Routine{Agent: "any"},
	}
	opts := newRunOptions(entry, cfg, "/tmp")
	if opts.Provider != "ollama" {
		t.Errorf("provider default not applied: %q", opts.Provider)
	}
	if opts.MaxIterations != 100 {
		t.Errorf("max_iter default: %d", opts.MaxIterations)
	}
	if opts.MaxCost != 5.0 {
		t.Errorf("max_cost default: %v", opts.MaxCost)
	}
	if opts.RoutineID != "global:x" {
		t.Errorf("routine_id: %q", opts.RoutineID)
	}
}

func TestBuildFireFnNotNil(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	fire := BuildFireFn(cfg)
	if fire == nil {
		t.Fatal("BuildFireFn returned nil")
	}
}

func TestRedirectStdioCreatesLogFile(t *testing.T) {
	// Not parallel: we mutate os.Stdout/os.Stderr.
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "routined.log")
	origStdout := os.Stdout
	origStderr := os.Stderr
	t.Cleanup(func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	})

	if err := RedirectStdio(path); err != nil {
		t.Fatalf("RedirectStdio: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	// Writes via os.Stdout should land in the file.
	_, _ = fmt.Fprintln(os.Stdout, "hello-from-redirect")
	// Close + flush.
	_ = os.Stdout.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello-from-redirect") {
		t.Errorf("log file does not contain redirected line: %q", string(data))
	}
}

func TestRedirectStdioEmptyPathIsNoOp(t *testing.T) {
	t.Parallel()
	if err := RedirectStdio(""); err != nil {
		t.Errorf("empty path should be a no-op, got %v", err)
	}
}

func TestRunReturnsCleanlyWithNoRoutines(t *testing.T) {
	// The daemon should load zero routines from a clean XDG home, start the
	// scheduler, then shut down cleanly when its context is cancelled.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, ".local", "state"))
	t.Setenv("HOME", tmp)

	cfg := config.Defaults()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, cfg, Options{MaxConcurrent: 1})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s after ctx cancel")
	}
}

func TestRunNilConfigErrors(t *testing.T) {
	t.Parallel()
	err := Run(context.Background(), nil, Options{})
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestApplyOptionsDefaults(t *testing.T) {
	t.Parallel()
	opts := &Options{}
	applyOptions(opts)
	if opts.MaxConcurrent != 2 {
		t.Errorf("MaxConcurrent default = %d, want 2", opts.MaxConcurrent)
	}
	// Explicit value preserved.
	opts2 := &Options{MaxConcurrent: 7}
	applyOptions(opts2)
	if opts2.MaxConcurrent != 7 {
		t.Errorf("MaxConcurrent override = %d, want 7", opts2.MaxConcurrent)
	}
}

func TestNewStoreAndSchedulerHappyPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, ".local", "state"))
	t.Setenv("HOME", tmp)

	store, sched, err := newStoreAndScheduler(config.Defaults(), Options{MaxConcurrent: 1})
	if err != nil {
		t.Fatalf("newStoreAndScheduler: %v", err)
	}
	if store == nil || sched == nil {
		t.Fatal("nil store or scheduler")
	}
}

func TestRunDefaultsMaxConcurrent(t *testing.T) {
	// MaxConcurrent=0 should default to 2 — exercised by running a Run with
	// no explicit concurrency and confirming clean shutdown.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, ".local", "state"))
	t.Setenv("HOME", tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := Run(ctx, config.Defaults(), Options{}); err != nil {
		t.Errorf("Run with defaults: %v", err)
	}
}
