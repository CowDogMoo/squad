package routine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/cowdogmoo/squad/config"
	"gopkg.in/yaml.v3"
)

// rootsFileName is the filename of the watched-roots registry.
const rootsFileName = "routine-roots.yaml"

// rootsConfig is the on-disk schema of routine-roots.yaml.
type rootsConfig struct {
	Roots []string `yaml:"roots"`
}

// RootsPath returns the absolute path of the watched-roots registry.
// It uses the XDG config home so it matches the rest of squad's config layout.
// Returns an error only when no home directory can be determined.
func RootsPath() (string, error) {
	return config.ConfigFile(rootsFileName)
}

// LoadRoots reads the registry and returns the list of watched repo roots in
// sorted, deduplicated, absolute, cleaned form. A missing registry returns
// an empty slice and no error.
func LoadRoots() ([]string, error) {
	path, err := RootsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cfg := rootsConfig{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return normalizeRoots(cfg.Roots)
}

// AddRoot adds path (resolved to absolute, cleaned) to the watched-roots
// registry. It is idempotent: adding an already-watched root is a no-op.
// Returns the cleaned absolute path that was added, plus a bool indicating
// whether the registry changed.
func AddRoot(path string) (string, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", false, fmt.Errorf("stat root %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("root %s is not a directory", abs)
	}
	current, err := LoadRoots()
	if err != nil {
		return "", false, err
	}
	for _, r := range current {
		if r == abs {
			return abs, false, nil
		}
	}
	current = append(current, abs)
	if err := SaveRoots(current); err != nil {
		return "", false, err
	}
	return abs, true, nil
}

// RemoveRoot removes path from the watched-roots registry. Returns true if
// the registry changed.
func RemoveRoot(path string) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	current, err := LoadRoots()
	if err != nil {
		return false, err
	}
	updated := make([]string, 0, len(current))
	changed := false
	for _, r := range current {
		if r == abs {
			changed = true
			continue
		}
		updated = append(updated, r)
	}
	if !changed {
		return false, nil
	}
	if err := SaveRoots(updated); err != nil {
		return false, err
	}
	return true, nil
}

// ContainingRoot returns the watched root that contains dir, or the empty
// string if none does. dir is resolved to absolute form before matching.
// When multiple watched roots contain dir, the longest (most specific) wins.
func ContainingRoot(dir string, roots []string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	best := ""
	for _, r := range roots {
		if isWithin(abs, r) && len(r) > len(best) {
			best = r
		}
	}
	return best, nil
}

// HasRepoRoutinesDir reports whether dir already contains a `.squad/routines`
// directory. Used to decide whether `routine create` defaults to repo scope.
func HasRepoRoutinesDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".squad", "routines"))
	return err == nil && info.IsDir()
}

// RepoRoutinesDir returns the manifest directory for per-repo routines under
// repoRoot. It does not create the directory.
func RepoRoutinesDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".squad", "routines")
}

// RepoStateDir returns the daemon-owned state directory under a repo root.
// Sits beside the routines/ directory and is meant to be gitignored.
func RepoStateDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".squad", "routines", ".state")
}

// GlobalRoutinesDir returns the manifest directory for global routines and
// creates it if missing.
func GlobalRoutinesDir() (string, error) {
	// config.ConfigFile creates the parent of its argument; pass a sentinel
	// child so the parent ("routines") is the directory we want to materialize.
	sentinel, err := config.ConfigFile(filepath.Join("routines", "manifests"))
	if err != nil {
		return "", err
	}
	return filepath.Dir(sentinel), nil
}

// GlobalStateDir returns the daemon-owned state directory for global routines.
// Uses XDG_STATE_HOME when set; falls back to the cache dir otherwise.
func GlobalStateDir() (string, error) {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		dir := filepath.Join(stateHome, "squad", "routines")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "state", "squad", "routines")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func normalizeRoots(in []string) ([]string, error) {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, r := range in {
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	sort.Strings(out)
	return out, nil
}

// isWithin reports whether child is at or beneath parent. Both paths must be
// absolute and cleaned.
func isWithin(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !filepath.IsAbs(rel) && rel != ".." && !startsWithDotDot(rel)
}

func startsWithDotDot(rel string) bool {
	return len(rel) >= 3 && rel[0] == '.' && rel[1] == '.' && (rel[2] == filepath.Separator)
}
