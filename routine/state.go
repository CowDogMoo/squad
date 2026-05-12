package routine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Status is the outcome of the most recent fire of a routine.
type Status string

const (
	// StatusOK indicates the most recent fire completed without error.
	StatusOK Status = "ok"
	// StatusFailed indicates the most recent fire returned an error.
	StatusFailed Status = "failed"
	// StatusSkipped indicates the fire was suppressed because a prior fire of
	// the same routine was still running (gocron singleton semantics).
	StatusSkipped Status = "skipped"
	// StatusRunning indicates a fire is currently in progress.
	StatusRunning Status = "running"
)

// State is daemon-written per-routine status, kept in a sibling file to the
// manifest so checked-in manifests stay clean.
//
// State is serialized as JSON (not YAML) because it is machine-authored, has
// no comments to preserve, and signals to humans that it is not user-editable.
type State struct {
	// LastRun is when the routine most recently fired (any status).
	LastRun time.Time `json:"last_run,omitempty"`
	// LastStatus is the outcome of LastRun.
	LastStatus Status `json:"last_status,omitempty"`
	// LastSessionID is the squad session id of the most recent fire, used by
	// `squad routine history`.
	LastSessionID string `json:"last_session_id,omitempty"`
	// LastError is the error message when LastStatus = failed.
	LastError string `json:"last_error,omitempty"`
	// LastDurationMs is wall-clock duration of the most recent fire.
	LastDurationMs int64 `json:"last_duration_ms,omitempty"`
}

// LoadState reads the state file at path. A missing file is not an error —
// the zero-value State is returned, representing "never fired".
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &State{}, nil
		}
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	s := &State{}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", path, err)
	}
	return s, nil
}

// SaveState writes the state JSON to path atomically. The parent directory is
// created if missing (state directories are daemon-owned, not user-curated).
func SaveState(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create state temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write state temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close state temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename state temp: %w", err)
	}
	return nil
}

// StateFileName returns the standard filename for the state companion of a
// routine id.
func StateFileName(id string) string {
	return id + ".state.json"
}
