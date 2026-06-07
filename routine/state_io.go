package routine

// This file holds the IO half of state-file persistence: atomic write via
// temp file + rename. The State type and LoadState (which has cleaner
// error contracts) live in state.go.
//
// Codecov ignores this file because every uncovered branch is an
// os.WriteFile / os.MkdirAll / os.Rename error path. Testing those would
// require injecting an os.* shim. The happy-path round-trip is exercised
// by state_test.go.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

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
