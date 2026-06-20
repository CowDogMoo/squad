package session

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/config"
)

// writeLegacySession materializes a legacy in-tree session directory under
// repo/.squad/sessions/<id>/ with the given meta, mimicking sessions written
// before the move to XDG_STATE_HOME.
func writeLegacySession(t *testing.T, repo, id string, meta Meta) {
	t.Helper()
	dir := filepath.Join(repo, SessionsRoot, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir legacy session: %v", err)
	}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644); err != nil {
		t.Fatalf("write legacy meta: %v", err)
	}
}

func TestNewRecordsCanonicalAndWorktreePath(t *testing.T) {
	canonical := t.TempDir()
	worktree := t.TempDir()
	l, err := New(canonical, worktree, "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	meta := readMeta(t, l)
	if meta.CanonicalRepoPath != canonical {
		t.Errorf("CanonicalRepoPath=%q, want %q", meta.CanonicalRepoPath, canonical)
	}
	if meta.WorktreePath != worktree {
		t.Errorf("WorktreePath=%q, want %q", meta.WorktreePath, worktree)
	}
	// WorkingDir retains the canonical path for backwards compatibility.
	if meta.WorkingDir != canonical {
		t.Errorf("WorkingDir=%q, want %q", meta.WorkingDir, canonical)
	}
}

func TestSessionSurvivesWorktreeRemoval(t *testing.T) {
	canonical := t.TempDir()
	worktree := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	l, err := New(canonical, worktree, "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := l.Dir()
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The session must NOT live inside the ephemeral worktree.
	if strings.HasPrefix(dir, worktree) {
		t.Fatalf("session dir %q is inside worktree %q", dir, worktree)
	}

	// Removing the worktree (isolation cleanup) must not destroy the session.
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		t.Fatalf("session meta gone after worktree removal: %v", err)
	}
}

func TestListUnionsLegacyAndXDGDedup(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()

	// One session in the XDG store.
	l, err := New(repo, "", "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Finish(StatusCompleted, "")
	xdgID := l.SessionID()
	_ = l.Close()

	// A legacy-only session for the same repo.
	writeLegacySession(t, repo, "legacy-only", Meta{
		SessionID:         "legacy-only",
		CanonicalRepoPath: repo,
		Created:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:            StatusCompleted,
	})
	// A legacy directory that duplicates the XDG session id; the XDG entry is
	// authoritative and must win the dedup.
	writeLegacySession(t, repo, xdgID, Meta{
		SessionID:         xdgID,
		CanonicalRepoPath: repo,
		Created:           time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:            "stale",
	})

	sessions, err := List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := map[string]IndexEntry{}
	for _, s := range sessions {
		byID[s.SessionID] = s
	}
	if len(byID) != 2 {
		t.Fatalf("expected 2 unique sessions, got %d: %+v", len(byID), sessions)
	}
	if _, ok := byID["legacy-only"]; !ok {
		t.Errorf("legacy-only session missing from union")
	}
	if got := byID[xdgID].Status; got == "stale" {
		t.Errorf("dedup picked stale legacy entry; XDG entry should win, got status %q", got)
	}
}

func TestListRebuildsFromMetaWhenIndexMissing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()

	l, err := New(repo, "", "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := l.SessionID()
	_ = l.Close()

	// Delete the index entirely: List must still find the session by scanning
	// meta.json, proving the index is a rebuildable cache.
	if err := os.Remove(filepath.Join(config.StateDir(), "index.jsonl")); err != nil {
		t.Fatalf("remove index: %v", err)
	}

	sessions, err := List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != id {
		t.Fatalf("expected to rebuild session %q from meta, got %+v", id, sessions)
	}
}

func TestListReflectsLiveStatusNotIndex(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()

	l, err := New(repo, "", "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The index recorded "running" at creation; meta.json now says completed.
	l.Finish(StatusCompleted, "")
	_ = l.Close()

	sessions, err := List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Status != StatusCompleted {
		t.Errorf("List status=%q, want live %q (not frozen index value)", sessions[0].Status, StatusCompleted)
	}
}

func TestListFiltersByRepo(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repoA := t.TempDir()
	repoB := t.TempDir()

	la, err := New(repoA, "", "agent", "openai", "gpt-5", "a")
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	idA := la.SessionID()
	_ = la.Close()
	lb, err := New(repoB, "", "agent", "openai", "gpt-5", "b")
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	_ = lb.Close()

	sessions, err := List(repoA)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != idA {
		t.Fatalf("List(repoA) should return only repoA's session, got %+v", sessions)
	}
}

func TestNewWritesGitignoreToLegacyInTreeDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()
	// Pre-existing in-tree sessions dir (legacy remnant) triggers the
	// belt-and-suspenders .gitignore write.
	inTree := filepath.Join(repo, SessionsRoot)
	if err := os.MkdirAll(inTree, 0o755); err != nil {
		t.Fatalf("mkdir in-tree: %v", err)
	}

	l, err := New(repo, "", "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	data, err := os.ReadFile(filepath.Join(inTree, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(data) != "*\n" {
		t.Errorf(".gitignore contents=%q, want %q", data, "*\n")
	}
}

func TestGitignoreExcludesSessionsUnderGitAddAll(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")

	// Legacy in-tree sessions dir so New drops the self-excluding .gitignore.
	if err := os.MkdirAll(filepath.Join(repo, SessionsRoot), 0o755); err != nil {
		t.Fatalf("mkdir in-tree: %v", err)
	}
	l, err := New(repo, "", "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = l.Close()

	runGit(t, repo, "add", "-A")
	out := runGit(t, repo, "status", "--porcelain")
	if strings.Contains(out, SessionsRoot) {
		t.Errorf("git add -A staged session artifacts:\n%s", out)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
