package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleMeta() Meta {
	return Meta{
		SessionID:         "test-id",
		CanonicalRepoPath: "/repo",
		Status:            StatusRunning,
	}
}

func TestAppendToIndexErrorsWithoutStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	if err := appendToIndex(sampleMeta()); err == nil {
		t.Fatal("expected error when no state home is available")
	}
}

func TestAppendToIndexErrorsWhenStateDirIsAFile(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// StateDir() becomes <blocked>/sub/squad; MkdirAll fails because blocked is
	// a regular file.
	t.Setenv("XDG_STATE_HOME", filepath.Join(blocked, "sub"))
	if err := appendToIndex(sampleMeta()); err == nil {
		t.Fatal("expected mkdir error when state dir cannot be created")
	}
}

func TestAppendToIndexErrorsWhenIndexPathIsADir(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	// Pre-create index.jsonl as a directory so the append OpenFile fails.
	if err := os.MkdirAll(filepath.Join(state, "squad", "index.jsonl"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := appendToIndex(sampleMeta()); err == nil {
		t.Fatal("expected open error when index.jsonl is a directory")
	}
}

func TestAppendToIndexAppendsEntries(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	// Two appends must both land as separate JSONL lines (append-only).
	if err := appendToIndex(sampleMeta()); err != nil {
		t.Fatalf("appendToIndex: %v", err)
	}
	if err := appendToIndex(sampleMeta()); err != nil {
		t.Fatalf("appendToIndex second: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(state, "squad", "index.jsonl"))
	if err != nil {
		t.Fatalf("read index.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 index lines, got %d: %q", len(lines), data)
	}
	if !strings.Contains(lines[0], `"session_id":"test-id"`) {
		t.Fatalf("index entry missing session id: %s", lines[0])
	}
	if !strings.Contains(lines[0], `"canonical_repo_path":"/repo"`) {
		t.Fatalf("index entry missing canonical repo path: %s", lines[0])
	}
}
