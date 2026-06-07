package routine

// This file holds the file-IO half of the watched-roots registry: writing
// the YAML to disk atomically (temp file + rename). The validation and
// addressing logic (normalization, containment checks, etc.) stays in
// roots.go and is unit-tested there.
//
// Codecov ignores this file because its uncovered branches are all
// os.WriteFile / os.Rename / temp-file error paths. Testing them would
// mean injecting os.* fakes — a pattern squad doesn't use elsewhere, and
// the testable callers (LoadRoots / AddRoot / RemoveRoot) already exercise
// the happy path end-to-end.

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SaveRoots writes the given roots to the registry atomically. The slice is
// normalized (abs, clean, dedup, sort) before writing.
func SaveRoots(roots []string) error {
	normalized, err := normalizeRoots(roots)
	if err != nil {
		return err
	}
	path, err := RootsPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(rootsConfig{Roots: normalized})
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".routine-roots-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create roots temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write roots temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close roots temp: %w", err)
	}
	return os.Rename(tmpName, path)
}
