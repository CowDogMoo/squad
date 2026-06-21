package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cowdogmoo/squad/config"
)

// IndexEntry represents a single line in the append-only index.jsonl file.
type IndexEntry struct {
	SessionID         string `json:"session_id"`
	CanonicalRepoPath string `json:"canonical_repo_path"`
	WorktreePath      string `json:"worktree_path,omitempty"`
	Timestamp         string `json:"timestamp"`
	Status            string `json:"status"`
}

// appendToIndex appends a session to the global index.jsonl.
func appendToIndex(meta Meta) error {
	stateDir := config.StateDir()
	if stateDir == "" {
		return fmt.Errorf("XDG_STATE_HOME not available")
	}

	indexFile := filepath.Join(stateDir, "index.jsonl")
	if err := os.MkdirAll(filepath.Dir(indexFile), 0o700); err != nil {
		return fmt.Errorf("mkdir state: %w", err)
	}

	entry := IndexEntry{
		SessionID:         meta.SessionID,
		CanonicalRepoPath: meta.CanonicalRepoPath,
		WorktreePath:      meta.WorktreePath,
		Timestamp:         meta.Created.Format("20060102T150405Z"),
		Status:            meta.Status,
	}

	b, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal index entry: %w", err)
	}

	f, err := os.OpenFile(indexFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open index.jsonl: %w", err)
	}

	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("write index entry: %w", err)
	}

	// Surface Close errors: on an append write they can signal a flush failure
	// that would otherwise lose the entry.
	if err := f.Close(); err != nil {
		return fmt.Errorf("close index.jsonl: %w", err)
	}
	return nil
}
