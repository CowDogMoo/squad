package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/tools"
	"github.com/spf13/cobra"
)

func TestInitRunContext_CommentsOnly(t *testing.T) {
	t.Parallel()
	bundle := &agent.Bundle{CommentsOnly: true}
	ctx := initRunContext(context.Background(), bundle)
	if !tools.IsCommentsOnlyMode(ctx) {
		t.Error("expected comments-only mode to be active")
	}
}

func TestInitRunContext_Normal(t *testing.T) {
	t.Parallel()
	bundle := &agent.Bundle{CommentsOnly: false}
	ctx := initRunContext(context.Background(), bundle)
	if tools.IsCommentsOnlyMode(ctx) {
		t.Error("expected comments-only mode to be inactive")
	}
}

func TestApplyConfigDefaultUnauthenticated(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{
		ConfigModel:    "gpt-4o",
		ConfigProvider: "openai",
	}
	bundle := &agent.Bundle{}
	ctx := context.Background()
	msg := applyConfigDefaultUnauthenticated(ctx, opts, bundle)
	if opts.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", opts.Model)
	}
	if opts.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", opts.Provider)
	}
	if !strings.Contains(msg, "No provider has detected credentials") {
		t.Errorf("message missing expected text, got: %q", msg)
	}
}

func TestApplyConfigDefaultUnauthenticated_ProviderAlreadySet(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{
		Provider:       "anthropic",
		ConfigModel:    "claude-3-5-sonnet",
		ConfigProvider: "openai",
	}
	bundle := &agent.Bundle{}
	ctx := context.Background()
	applyConfigDefaultUnauthenticated(ctx, opts, bundle)
	// Provider was already set, should not be overwritten.
	if opts.Provider != "anthropic" {
		t.Errorf("Provider = %q, want anthropic (should not be overwritten)", opts.Provider)
	}
}

func TestManifestIsolation_NoAgent(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{}
	got := manifestIsolation(opts)
	if got != "" {
		t.Errorf("manifestIsolation() = %q, want empty", got)
	}
}

func TestManifestIsolation_AgentNotFound(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{
		Agent:     "nonexistent-agent",
		AgentsDir: t.TempDir(),
	}
	got := manifestIsolation(opts)
	if got != "" {
		t.Errorf("manifestIsolation() = %q, want empty for missing agent", got)
	}
}

func TestManifestIsolation_WithIsolation(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, "my-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := "name: my-agent\nversion: 0.0.0\nentrypoint: system.md\nwrapper: wrapper.md\nisolation: worktree\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "wrapper.md"), []byte("wrapper"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	opts := &RunOptions{Agent: "my-agent", AgentsDir: agentsDir}
	got := manifestIsolation(opts)
	if got != "worktree" {
		t.Errorf("manifestIsolation() = %q, want worktree", got)
	}
}

func TestReportIsolationTeardown_Nil(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	// Should not panic.
	reportIsolationTeardown(cmd, nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for nil iso, got %q", buf.String())
	}
}

func TestSetupIsolation_NoneMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := &RunOptions{Isolation: "none"}
	iso, err := setupIsolation(context.Background(), opts, dir)
	if err != nil {
		t.Fatalf("setupIsolation: %v", err)
	}
	if iso.Mode != IsolationNone {
		t.Errorf("Mode = %v, want IsolationNone", iso.Mode)
	}
}

func TestSetupIsolation_InvalidMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := &RunOptions{Isolation: "bogus-mode"}
	_, err := setupIsolation(context.Background(), opts, dir)
	if err == nil {
		t.Fatal("expected error for invalid isolation mode")
	}
}

func TestHandleBudgetExceeded_WithPartialResponse(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	var stderr, stdout bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&stdout)
	opts := &RunOptions{MaxCost: 0.5, Print: true}
	m := &metrics.Metrics{}
	// Wrap ErrBudgetExceeded so errors.Is must unwrap to find it.
	err := fmt.Errorf("loop aborted: %w", metrics.ErrBudgetExceeded)
	handleBudgetExceeded(cmd, opts, m, "partial answer", "", err)
	if !strings.Contains(stderr.String(), "cost budget") {
		t.Errorf("expected budget message on stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "partial answer") {
		t.Errorf("expected partial response on stdout, got %q", stdout.String())
	}
}

func TestSetupIsolation_ConfigIsolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := &RunOptions{
		Config: &config.Config{
			Run: config.RunConfig{Isolation: "none"},
		},
	}
	iso, err := setupIsolation(context.Background(), opts, dir)
	if err != nil {
		t.Fatalf("setupIsolation: %v", err)
	}
	if iso.Mode != IsolationNone {
		t.Errorf("Mode = %v, want IsolationNone", iso.Mode)
	}
}
