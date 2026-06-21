package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/session"
	"github.com/spf13/cobra"
)

// runSessionsSubcmd invokes a sessions subcommand's RunE and returns its
// captured output.
func runSessionsSubcmd(t *testing.T, cmd *cobra.Command, args []string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	err := cmd.RunE(cmd, args)
	return buf.String(), err
}

func TestSessionsCmdListsAndOpensWithoutMutating(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()
	t.Chdir(repo)

	// Not a git repo, so the canonical path is the working dir itself.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	l, err := session.New(wd, "", "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	l.Finish(session.StatusCompleted, "")
	id := l.SessionID()
	dir := l.Dir()
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// `sessions` lists the session for this repo.
	out, err := runSessionsSubcmd(t, sessionsCmd, nil)
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("session list missing id %q:\n%s", id, out)
	}

	// `sessions open` (no arg) prints the latest session's directory.
	out, err = runSessionsSubcmd(t, openSessionCmd, nil)
	if err != nil {
		t.Fatalf("sessions open: %v", err)
	}
	if got := strings.TrimSpace(out); got != dir {
		t.Fatalf("open printed %q, want %q", got, dir)
	}

	// Critically, `open` must NOT reopen the session and flip its status back
	// to running.
	metaBytes, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var meta struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if meta.Status != session.StatusCompleted {
		t.Errorf("open mutated session status to %q, want %q", meta.Status, session.StatusCompleted)
	}
}

func TestSessionsCmdEmpty(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	out, err := runSessionsSubcmd(t, sessionsCmd, nil)
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if !strings.Contains(out, "No sessions found") {
		t.Fatalf("expected empty message, got:\n%s", out)
	}

	// `open` with no sessions is an error.
	if _, err := runSessionsSubcmd(t, openSessionCmd, nil); err == nil {
		t.Fatal("expected error opening latest with no sessions")
	}
}

func TestSessionsCmdOpenByIDAndNotFound(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()
	t.Chdir(repo)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	l, err := session.New(wd, "", "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	id := l.SessionID()
	dir := l.Dir()
	_ = l.Close()

	// Open by explicit id prints the directory.
	out, err := runSessionsSubcmd(t, openSessionCmd, []string{id})
	if err != nil {
		t.Fatalf("open %s: %v", id, err)
	}
	if got := strings.TrimSpace(out); got != dir {
		t.Fatalf("open %s printed %q, want %q", id, got, dir)
	}

	// Unknown id is an error.
	if _, err := runSessionsSubcmd(t, openSessionCmd, []string{"no-such-id"}); err == nil {
		t.Fatal("expected error opening unknown session id")
	}
}

func TestSessionsCmdRendersWorktreeColumn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()
	t.Chdir(repo)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	// A session whose worktree lives under the repo: the listing shows the
	// path relative to the repo.
	worktree := filepath.Join(wd, "wt-1")
	l, err := session.New(wd, worktree, "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	_ = l.Close()

	out, err := runSessionsSubcmd(t, sessionsCmd, nil)
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if !strings.Contains(out, "wt-1") {
		t.Fatalf("expected relative worktree path in listing:\n%s", out)
	}
}
