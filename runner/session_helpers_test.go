package runner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/session"
)

func TestOpenSessionNoSessionReturnsNil(t *testing.T) {
	opts := &RunOptions{NoSession: true, WorkingDir: t.TempDir()}
	l, err := openSession(opts, &agent.Bundle{}, "go")
	if err != nil {
		t.Fatalf("openSession: %v", err)
	}
	if l != nil {
		t.Fatalf("expected nil logger when NoSession=true")
	}
}

func TestOpenSessionFreshWritesRunStart(t *testing.T) {
	wd := t.TempDir()
	opts := &RunOptions{
		WorkingDir: wd,
		Agent:      "agent",
		Provider:   "openai",
		Model:      "gpt-5",
	}
	l, err := openSession(opts, &agent.Bundle{System: "sys"}, "do the thing")
	if err != nil {
		t.Fatalf("openSession: %v", err)
	}
	if l == nil {
		t.Fatalf("expected logger")
	}
	t.Cleanup(func() { _ = l.Close() })

	// events.jsonl should contain run_start with system_bytes encoded.
	events, err := os.ReadFile(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if !strings.Contains(string(events), `"type":"run_start"`) {
		t.Fatalf("expected run_start event, got %s", events)
	}
}

func TestOpenSessionResumeRehydratesResponseID(t *testing.T) {
	wd := t.TempDir()
	first, err := session.New(wd, "agent", "openai", "gpt-5", "first prompt")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	first.SetLastResponseID("resp_prior")
	id := first.SessionID()
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	opts := &RunOptions{WorkingDir: wd, Agent: "agent", ResumeID: id}
	l, err := openSession(opts, &agent.Bundle{}, "second prompt")
	if err != nil {
		t.Fatalf("openSession resume: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	if opts.ResumeResponseID != "resp_prior" {
		t.Fatalf("ResumeResponseID = %q, want resp_prior", opts.ResumeResponseID)
	}
	events, err := os.ReadFile(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if !strings.Contains(string(events), `"type":"resume"`) {
		t.Fatalf("expected resume event, got %s", events)
	}
}

func TestOpenSessionResumeMissingErrors(t *testing.T) {
	opts := &RunOptions{WorkingDir: t.TempDir(), ResumeID: "nope"}
	if _, err := openSession(opts, &agent.Bundle{}, "x"); err == nil {
		t.Fatalf("expected error resuming missing session")
	}
}

func TestCloseSessionNilLoggerNoop(t *testing.T) {
	closeSession(nil, nil, nil) // must not panic
}

func TestOpenSessionFreshFailsWhenSessionsRootBlocked(t *testing.T) {
	wd := t.TempDir()
	// Make .squad a file so session.New's MkdirAll fails.
	if err := os.WriteFile(filepath.Join(wd, ".squad"), []byte("blocked"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	opts := &RunOptions{WorkingDir: wd, Agent: "a", Provider: "p", Model: "m"}
	if _, err := openSession(opts, &agent.Bundle{}, "go"); err == nil {
		t.Fatalf("expected error when session dir cannot be created")
	}
}

func TestCloseSessionStatusMapping(t *testing.T) {
	cases := []struct {
		name   string
		runErr error
		want   string
	}{
		{"completed", nil, session.StatusCompleted},
		{"budget", metrics.ErrBudgetExceeded, session.StatusBudget},
		{"error", errors.New("boom"), session.StatusError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wd := t.TempDir()
			l, err := session.New(wd, "agent", "openai", "gpt-5", "p")
			if err != nil {
				t.Fatalf("session.New: %v", err)
			}
			m := metrics.New("openai", "gpt-5")
			m.AddTokens(10, 20)
			m.IncrementIterations()
			closeSession(l, m, tc.runErr)

			data, err := os.ReadFile(filepath.Join(l.Dir(), "meta.json"))
			if err != nil {
				t.Fatalf("read meta: %v", err)
			}
			if !strings.Contains(string(data), `"status": "`+tc.want+`"`) {
				t.Fatalf("meta.json missing status %q: %s", tc.want, data)
			}
		})
	}
}
