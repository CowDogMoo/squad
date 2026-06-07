package routine

// This file holds the atomic-write half of routine manifest persistence.
// The struct, validation, and load logic live in manifest.go and are
// thoroughly unit-tested.
//
// Codecov ignores this file because its uncovered branches are all
// temp-file + rename error paths. Testing them would require fault
// injection on os.* (not a pattern squad uses elsewhere).

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SaveRoutine writes the routine YAML to path atomically (write-temp, rename).
// The parent directory must already exist.
func SaveRoutine(path string, r *Routine) error {
	if err := r.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal routine %s: %w", r.ID, err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".routine-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		// If rename succeeded, tmpName no longer exists and this is a no-op.
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}
	return nil
}
