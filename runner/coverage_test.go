package runner

import (
	"context"
	"os"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/mcp"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/tools"
	"github.com/spf13/cobra"
)

// TestReadPromptPipedStdin verifies readPrompt reads content from a real
// os.Pipe, exercising the *os.File branch that strings.Reader cannot reach.
func TestReadPromptPipedStdin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"non-empty content", "hello from pipe\n", "hello from pipe"},
		{"whitespace-only returns empty", "   \n\t  \n", ""},
		{"multi-line content", "line one\nline two\n", "line one\nline two"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("os.Pipe: %v", err)
			}
			if _, err := w.WriteString(tt.input); err != nil {
				t.Fatalf("WriteString: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("w.Close: %v", err)
			}

			cmd := &cobra.Command{}
			cmd.SetIn(r)
			got, err := readPrompt(cmd, nil)
			if err != nil {
				t.Fatalf("readPrompt() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("readPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSetupIsolationWithConfig verifies setupIsolation respects the isolation
// mode from the loaded config when CLI flag and manifest are both empty.
func TestSetupIsolationWithConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		cliFlag     string
		configValue string
		wantMode    IsolationMode
	}{
		{
			name:        "config none applied when cli and manifest empty",
			cliFlag:     "",
			configValue: "none",
			wantMode:    IsolationNone,
		},
		{
			name:        "cli flag beats config isolation",
			cliFlag:     "none",
			configValue: "worktree",
			wantMode:    IsolationNone,
		},
		{
			name:        "nil config still resolves to none",
			cliFlag:     "",
			configValue: "",
			wantMode:    IsolationNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			opts := &RunOptions{
				Isolation: tt.cliFlag,
				Config: &config.Config{
					Run: config.RunConfig{Isolation: tt.configValue},
				},
			}
			iso, err := setupIsolation(context.Background(), opts, dir)
			if err != nil {
				t.Fatalf("setupIsolation() error = %v", err)
			}
			if iso.Mode != tt.wantMode {
				t.Fatalf("mode = %q, want %q", iso.Mode, tt.wantMode)
			}
		})
	}
}

// TestSetupIsolationInvalidFlagErrors verifies setupIsolation propagates
// an error from ResolveIsolationMode for invalid isolation values.
func TestSetupIsolationInvalidFlagErrors(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{Isolation: "garbage"}
	_, err := setupIsolation(context.Background(), opts, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid isolation mode")
	}
}

// TestApplyChildBudgetCapsToRemaining verifies that when a dedicated child
// budget exceeds the remaining parent budget, it is capped to remaining.
func TestApplyChildBudgetCapsToRemaining(t *testing.T) {
	t.Parallel()
	// No tokens consumed → remaining equals maxCost (1.0).
	parentMetrics := metrics.New("openai", "gpt-4o")
	parentMetrics.SetMaxCost(1.0)

	remaining := parentMetrics.RemainingBudget()
	if remaining <= 0 {
		t.Fatalf("expected remaining > 0 with no tokens consumed, got %f", remaining)
	}

	opts := &RunOptions{MaxCost: 1.0}
	childOpts := &RunOptions{}
	cfg := &tools.TaskConfig{
		ParentMetrics: parentMetrics,
		// Dedicated budget (5.0) far exceeds remaining (1.0) — must be capped.
		ChildMaxCost: func(string) float64 { return 5.0 },
	}

	if err := applyChildBudget(context.Background(), childOpts, cfg, opts, "child"); err != nil {
		t.Fatalf("applyChildBudget() error = %v", err)
	}
	if childOpts.MaxCost > remaining+1e-9 {
		t.Fatalf(
			"child budget %f exceeds remaining %f — dedicated cap not applied",
			childOpts.MaxCost, remaining,
		)
	}
	if childOpts.MaxCost != remaining {
		t.Fatalf("child budget = %f, want %f (remaining)", childOpts.MaxCost, remaining)
	}
}

// TestApplyChildBudgetNilMetricsNoop verifies applyChildBudget is a no-op
// when ParentMetrics is nil or MaxCost is zero.
func TestApplyChildBudgetNilMetricsNoop(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     *tools.TaskConfig
		maxCost float64
	}{
		{"nil metrics", &tools.TaskConfig{}, 1.0},
		{"zero max cost", &tools.TaskConfig{ParentMetrics: metrics.New("openai", "gpt-4o")}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			childOpts := &RunOptions{}
			opts := &RunOptions{MaxCost: tt.maxCost}
			if err := applyChildBudget(context.Background(), childOpts, tt.cfg, opts, "child"); err != nil {
				t.Fatalf("applyChildBudget() error = %v", err)
			}
			if childOpts.MaxCost != 0 {
				t.Fatalf("expected no budget set, got %f", childOpts.MaxCost)
			}
		})
	}
}

// TestInvokeModelMCPServerFailure verifies InvokeModel returns an error when
// an MCP server fails to connect, and still returns non-nil metrics.
func TestInvokeModelMCPServerFailure(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{
		Provider:      "ollama",
		Model:         "mistral",
		MaxIterations: 1,
		MCPServers: []mcp.ServerConfig{
			{Name: "bad-server", Command: "/nonexistent/binary/xyz"},
		},
	}
	bundle := &agent.Bundle{
		System:  "system",
		User:    "user",
		WorkDir: t.TempDir(),
	}
	_, m, err := InvokeModel(context.Background(), opts, bundle)
	if err == nil {
		t.Fatal("expected error for bad MCP server")
	}
	if m == nil {
		t.Fatal("metrics should be non-nil even on MCP connection failure")
	}
}

// TestCloseMCPClientsWithTestClients verifies closeMCPClients handles test
// clients without panicking.
func TestCloseMCPClientsWithTestClients(t *testing.T) {
	t.Parallel()
	clients := []*mcp.Client{
		mcp.NewTestClient("a", nil),
		mcp.NewTestClient("b", nil),
	}
	// Should not panic.
	closeMCPClients(clients)
}
