package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/config"
)

func TestNewRoutinedCmdHasExpectedFlags(t *testing.T) {
	t.Parallel()
	cmd := newRoutinedCmd()
	if cmd.Use != "routined" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if !cmd.Hidden {
		t.Error("routined should be hidden")
	}
	for _, f := range []string{"max-concurrent", "fire-timeout", "log-file"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("missing flag --%s", f)
		}
	}
}

func TestRoutinedRequiresConfig(t *testing.T) {
	setupXDG(t)
	t.Setenv("SQUAD_SKIP_SERVICE_INSTALL", "1")
	cmd := newRoutinedCmd()
	cmd.SetContext(context.Background())
	// No config value bound on the context -> RunE should refuse.
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Error("expected error when config not in context")
	} else if !strings.Contains(err.Error(), "config not available") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRoutinedRunsWithConfigAndShortCtx(t *testing.T) {
	setupXDG(t)
	t.Setenv("SQUAD_SKIP_SERVICE_INSTALL", "1")
	cmd := newRoutinedCmd()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd.SetContext(withConfig(ctx, config.Defaults()))
	// Should exit cleanly when the context times out (no routines configured,
	// daemon spins up + shuts down).
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("RunE: %v", err)
	}
}
