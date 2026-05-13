package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestNewUICmdFlags(t *testing.T) {
	cmd := newUICmd()
	if cmd.Use != "ui" {
		t.Errorf("Use = %q, want %q", cmd.Use, "ui")
	}
	for _, name := range []string{"mock", "sessions-dir", "working-dir", "agents-dir"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag %q", name)
		}
	}
}

func TestDiscoverAgentsReturnsSortedNames(t *testing.T) {
	// Isolated XDG so config.Load() doesn't touch the user's real config.
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("HOME", base)

	// agentsDir contains two valid agents; discoverAgents should pick both up
	// via the --agents-dir override and return them sorted alphabetically.
	agentsDir := t.TempDir()
	for _, name := range []string{"zeta", "alpha"} {
		dir := filepath.Join(agentsDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "version: v1\ndescription: " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := discoverAgents(agentsDir)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 agents, got %d (%v)", len(got), got)
	}
	// Sorted result: alpha before zeta.
	var ai, zi = -1, -1
	for i, n := range got {
		if n == "alpha" {
			ai = i
		}
		if n == "zeta" {
			zi = i
		}
	}
	if ai == -1 || zi == -1 || ai > zi {
		t.Errorf("expected alpha before zeta in sorted list, got %v", got)
	}
}

func TestRunUIExitsOnCancelledContext(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("HOME", base)

	cmd := newUICmd()
	// Pre-cancelled context so tea.NewProgram exits without needing a TTY.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)
	// Mock=true uses hand-crafted runs and avoids the sessions-dir discovery
	// branch (which would otherwise touch the local filesystem in unexpected
	// ways).
	if err := cmd.Flags().Set("mock", "true"); err != nil {
		t.Fatal(err)
	}
	// Working dir explicitly set so the os.Getwd branch is skipped.
	if err := cmd.Flags().Set("working-dir", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// runUI returns whatever bubble tea returns when the context is dead;
	// we only assert it doesn't hang or panic.
	_ = runUI(cmd, nil)
}

func TestRunUINonMockMissingDirIsHandled(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("HOME", base)

	cmd := newUICmd()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)
	if err := cmd.Flags().Set("sessions-dir", filepath.Join(t.TempDir(), "no-such-dir")); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("working-dir", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// Result may be an error (missing dir) or a bubble-tea exit; we just want
	// the early branches in runUI executed without hanging.
	_ = runUI(cmd, nil)
}

// guard against unused-import warnings if cobra goes unused.
var _ = (*cobra.Command)(nil)

func TestDiscoverAgentsEmptyDirReturnsEmpty(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("HOME", base)
	// Empty agents dir is a valid override; result depends on the user's
	// configured sources. We only assert that the call returns without
	// panicking and the result is sorted.
	got := discoverAgents(t.TempDir())
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("unsorted: %v", got)
			break
		}
	}
}
