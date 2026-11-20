package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		Provider: config.ProviderConfig{Default: "anthropic", NumCtx: 32768, OllamaTimeout: 45 * time.Minute},
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
	if opts.Model != "" {
		t.Errorf("unset routine model should not populate opts.Model: %q", opts.Model)
	}
	if opts.ConfigModel != "claude-sonnet-4-6" {
		t.Errorf("config model not threaded into ConfigModel fallback: %q", opts.ConfigModel)
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
	if opts.OllamaTimeout != 45*time.Minute {
		t.Errorf("ollama timeout not threaded through: %s", opts.OllamaTimeout)
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
	if opts.Provider != "" {
		t.Errorf("unset routine provider should not populate opts.Provider: %q", opts.Provider)
	}
	if opts.ConfigProvider != "ollama" {
		t.Errorf("config provider not threaded into ConfigProvider fallback: %q", opts.ConfigProvider)
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

func TestBuildFireFnInvocationReachesRunnerWithMissingAgent(t *testing.T) {
	// Invoke the returned FireFn. It will fail inside runner.ExecuteRun when
	// the agent can't be found — that's fine; we're exercising the fire
	// wiring (working dir resolution, RunOptions construction), not the
	// full agent execution path.
	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Agents.LocalPaths = []string{tmp} // no real agents -> guaranteed lookup failure
	fire := BuildFireFn(cfg)

	entry := routine.Entry{
		Ref: routine.Ref{Scope: routine.ScopeGlobal, ID: "x"},
		Routine: &routine.Routine{
			ID:         "x",
			Agent:      "definitely-missing-agent",
			Schedule:   "@daily",
			WorkingDir: tmp,
			Enabled:    true,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := fire(ctx, entry)
	if err == nil {
		t.Error("expected error when agent doesn't exist")
	}
}

// fireTestAgent is the agent the fire tests run. The name is unlikely to
// match an agent in any cached repository on the machine running the tests.
const fireTestAgent = "squad-daemon-fire-test"

// writeFireTestAgent creates a minimal edit-capable agent and returns the
// directory to list under agents.local_paths.
func writeFireTestAgent(t *testing.T) string {
	t.Helper()
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, fireTestAgent)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	files := map[string]string{
		"agent.yaml": "name: " + fireTestAgent + "\nversion: 0.0.0\nentrypoint: system.md\nwrapper: wrapper.md\n",
		"system.md":  "System prompt.",
		"wrapper.md": "Wrapper prompt.",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(agentDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	return agentsDir
}

// fakeOllama answers the first probes requests with a read-only Bash call
// and every later one with a final answer. It records each request body.
type fakeOllama struct {
	mu     sync.Mutex
	bodies []string
}

func newFakeOllama(t *testing.T, probes int) (*fakeOllama, *httptest.Server) {
	t.Helper()
	f := &fakeOllama{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		n := len(f.bodies)
		f.bodies = append(f.bodies, string(body))
		f.mu.Unlock()
		if n < probes {
			_, _ = fmt.Fprintf(w, `{"model":"mistral","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"Bash","arguments":{"command":"ls nomatch%d"}}}]},"done":true}`, n)
			return
		}
		_, _ = io.WriteString(w, `{"model":"mistral","message":{"role":"assistant","content":"done"},"done":true}`)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeOllama) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.bodies...)
}

// fireOllamaRoutine fires one routine that runs fireTestAgent against the
// Ollama server at baseURL, the way the daemon does.
func fireOllamaRoutine(t *testing.T, cfg *config.Config, baseURL string) {
	t.Helper()
	entry := routine.Entry{
		Ref: routine.Ref{Scope: routine.ScopeGlobal, ID: "x"},
		Routine: &routine.Routine{
			ID:            "x",
			Agent:         fireTestAgent,
			Schedule:      "@daily",
			Prompt:        "map the target",
			WorkingDir:    t.TempDir(),
			Provider:      "ollama",
			Model:         "mistral",
			BaseURL:       baseURL,
			MaxIterations: 30,
			Enabled:       true,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := BuildFireFn(cfg)(ctx, entry); err != nil {
		t.Fatalf("fire: %v", err)
	}
}

// TestFireKeepsEditGuards checks that a routine fire still pushes an
// edit-capable agent to edit. The daemon never sets RequireActionable, and
// leaving it unset must not switch off the phase nudges and read blocking.
func TestFireKeepsEditGuards(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Agents.LocalPaths = []string{writeFireTestAgent(t)}
	fake, srv := newFakeOllama(t, 12)

	fireOllamaRoutine(t, cfg, srv.URL)

	sent := strings.Join(fake.requests(), "\n")
	for _, marker := range []string{"[PROGRESS CHECK]", "READ BLOCKED"} {
		if !strings.Contains(sent, marker) {
			t.Errorf("a routine fire must keep the edit guards, but the agent never saw %q", marker)
		}
	}
}

// TestFireReadsOllamaThinkFromYAML is the regression test for an unquoted
// YAML boolean in provider.ollama_think failing every Ollama routine fire:
// the daemon reads the setting from the decoded config struct, and the value
// must reach Ollama's think option.
func TestFireReadsOllamaThinkFromYAML(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     bool
	}{
		{"unquoted true", "provider:\n  ollama_think: true\n", true},
		{"unquoted false", "provider:\n  ollama_think: false\n", false},
		{"quoted true", "provider:\n  ollama_think: \"true\"\n", true},
		{"unset", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			path := filepath.Join(t.TempDir(), "config.yaml")
			yaml := tt.provider + "agents:\n  local_paths:\n    - " + writeFireTestAgent(t) + "\n"
			if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			cfg, _, err := config.LoadFromPath(path)
			if err != nil {
				t.Fatalf("LoadFromPath: %v", err)
			}
			fake, srv := newFakeOllama(t, 0)

			fireOllamaRoutine(t, cfg, srv.URL)

			reqs := fake.requests()
			if len(reqs) != 1 {
				t.Fatalf("requests = %d, want 1", len(reqs))
			}
			var sent struct {
				Think *bool `json:"think"`
			}
			if err := json.Unmarshal([]byte(reqs[0]), &sent); err != nil {
				t.Fatalf("request is not JSON: %v", err)
			}
			if sent.Think == nil || *sent.Think != tt.want {
				t.Fatalf("request think = %v, want %v", sent.Think, tt.want)
			}
		})
	}
}

func TestNewStoreAndSchedulerErrorBubbles(t *testing.T) {
	// Point XDG at an unwriteable area so LoadAll fails — exercises the
	// error path in newStoreAndScheduler.
	if os.Geteuid() == 0 {
		t.Skip("can't make /proc-style unwriteable as root")
	}
	t.Setenv("XDG_CONFIG_HOME", "/proc/cannot")
	t.Setenv("XDG_STATE_HOME", "/proc/cannot")
	t.Setenv("HOME", "/proc/cannot")
	if _, _, err := newStoreAndScheduler(config.Defaults(), Options{MaxConcurrent: 1}); err == nil {
		t.Error("expected error when XDG dirs are unwriteable")
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

func TestRedirectStdioMkdirFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("mkdir under read-only dir skipped as root")
	}
	// Parent of `path` is a regular file, so MkdirAll inside RedirectStdio
	// fails with "not a directory".
	base := t.TempDir()
	asFile := filepath.Join(base, "blocker")
	if err := os.WriteFile(asFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RedirectStdio(filepath.Join(asFile, "child", "log.txt")); err == nil {
		t.Error("expected error when parent path is a file")
	}
}

func TestRedirectStdioOpenFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-only chmod ineffective as root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := RedirectStdio(filepath.Join(dir, "log.txt")); err == nil {
		t.Error("expected open error in read-only dir")
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

func TestRunLifecycleWatchError(t *testing.T) {
	// runLifecycle should propagate the error from store.Watch when the
	// watch directory cannot be created (XDG_STATE_HOME is a file, not a dir).
	tmp := t.TempDir()
	// Make XDG_STATE_HOME a file so Watch's MkdirAll fails.
	stateFile := filepath.Join(tmp, "state-file")
	if err := os.WriteFile(stateFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_STATE_HOME", stateFile)
	t.Setenv("HOME", tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := Run(ctx, config.Defaults(), Options{MaxConcurrent: 1})
	if err == nil {
		t.Error("expected error when watch dir cannot be created, got nil")
	}
}

func TestRunLifecycleCatchupFires(t *testing.T) {
	// Verify runLifecycle runs without error when there are no missed fires
	// (the common case) — exercises the FindMissedFires branch with empty result.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, ".local", "state"))
	t.Setenv("HOME", tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := Run(ctx, config.Defaults(), Options{MaxConcurrent: 1}); err != nil {
		t.Errorf("Run: %v", err)
	}
}

func TestNewStoreAndSchedulerSyncError(t *testing.T) {
	// Make LoadAll fail by pointing XDG_STATE_HOME at a file so the
	// store's state directory cannot be created.
	tmp := t.TempDir()
	stateFile := filepath.Join(tmp, "state-file")
	if err := os.WriteFile(stateFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_STATE_HOME", stateFile)
	t.Setenv("HOME", tmp)
	_, _, err := newStoreAndScheduler(config.Defaults(), Options{MaxConcurrent: 1})
	if err == nil {
		t.Error("expected error when state dir is a file, got nil")
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

func TestRunLifecycleCatchupWithMissedFire(t *testing.T) {
	// Create a routine that has a missed fire so QueueCatchups is exercised.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, ".local", "state"))
	t.Setenv("HOME", tmp)

	// Create a routine with a past fire time.
	store := routine.NewStore()
	ref := routine.Ref{Scope: routine.ScopeGlobal, ID: "missed-job"}
	r := &routine.Routine{
		ID:       "missed-job",
		Agent:    "test-agent",
		Schedule: "@hourly",
		Enabled:  true,
	}
	if _, err := store.Create(ref, r); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	// Run should complete cleanly even with a missed fire (fire fn will fail
	// but that's expected in unit tests).
	_ = Run(ctx, config.Defaults(), Options{MaxConcurrent: 1})
}

// TestFireFailsWhenAgentReportsBlocked checks a fire whose agent ends with a
// blocked status line returns an error, so the scheduler records it failed
// instead of ok, and that the agent was told how to report its status.
func TestFireFailsWhenAgentReportsBlocked(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Agents.LocalPaths = []string{writeFireTestAgent(t)}
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		_, _ = io.WriteString(w, `{"model":"mistral","message":{"role":"assistant","content":"The calendar tools are missing.\nSTATUS: blocked: calendar MCP server missing"},"done":true}`)
	}))
	t.Cleanup(srv.Close)

	entry := routine.Entry{
		Ref: routine.Ref{Scope: routine.ScopeGlobal, ID: "planner"},
		Routine: &routine.Routine{
			ID: "planner", Agent: fireTestAgent, Schedule: "@daily", Prompt: "plan the week",
			WorkingDir: t.TempDir(), Provider: "ollama", Model: "mistral", BaseURL: srv.URL,
			MaxIterations: 30, Enabled: true,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sessionID, err := BuildFireFn(cfg)(ctx, entry)
	if err == nil || !strings.Contains(err.Error(), "the agent reported blocked: calendar MCP server missing") {
		t.Fatalf("fire err = %v, want the blocked report", err)
	}
	if sessionID == "" {
		t.Error("fire returned no session id")
	}
	mu.Lock()
	sent := strings.Join(bodies, "\n")
	mu.Unlock()
	if !strings.Contains(sent, "STATUS: blocked") {
		t.Error("the agent was never told to end with a status line")
	}
}
