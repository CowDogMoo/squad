// Package browser manages named Chromium user-data directories used by
// browser-driving MCPs (chrome-devtools-mcp, primarily). Each profile is a
// long-lived directory the user signs into once; agents reference it by
// name via the {{.BrowserProfile "name"}} template helper in agent.yaml.
//
// Profiles live under $XDG_DATA_HOME/squad/browser-profiles/<name>
// (default: ~/.local/share/squad/browser-profiles/<name>). Names are
// validated to be filesystem-safe identifiers.
package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// validName matches profile names: lowercase letters, digits, hyphens, and
// underscores. No dots, no slashes, no leading/trailing hyphen. Mirrors
// skill-name conventions for consistency.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*[a-z0-9]$|^[a-z0-9]$`)

// ErrInvalidName is returned by validators when a name does not satisfy
// validName.
var ErrInvalidName = errors.New("browser profile name must be lowercase alphanumerics with optional `-`/`_`, no leading/trailing punctuation")

// ErrProfileNotFound is returned by Delete when a name has no on-disk dir.
var ErrProfileNotFound = errors.New("browser profile not found")

// ValidateName reports whether name is a legal profile identifier.
func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}

// Root returns the absolute directory containing all squad-managed browser
// profiles. It is created lazily by ProfileDir; Root itself does not touch
// the filesystem.
func Root() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "squad", "browser-profiles")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Last-resort fallback. Picking cwd over panicking keeps tests
		// runnable even in extreme environments where the user has no home.
		return filepath.Join(".", ".squad", "browser-profiles")
	}
	return filepath.Join(home, ".local", "share", "squad", "browser-profiles")
}

// ProfileDir returns the absolute path to the profile named name, creating
// the directory if it does not yet exist. Returns ErrInvalidName for
// invalid names; otherwise the path is guaranteed to exist on success.
func ProfileDir(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	dir := filepath.Join(Root(), name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create profile dir %s: %w", dir, err)
	}
	return dir, nil
}

// Exists reports whether the profile named name has an on-disk dir. It
// does NOT validate name (use ValidateName separately if needed); a
// malformed name simply returns false.
func Exists(name string) bool {
	if err := ValidateName(name); err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(Root(), name))
	return err == nil && info.IsDir()
}

// Profile is a discovered profile entry. ModTime is the directory's mtime
// (a rough "last used" signal; Chrome touches the dir on session writes).
type Profile struct {
	Name    string
	Dir     string
	ModTime time.Time
}

// List returns all profiles under Root(), sorted by name. A missing root
// directory is not an error and returns an empty slice.
func List() ([]Profile, error) {
	root := Root()
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles root %s: %w", root, err)
	}
	out := make([]Profile, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip entries that don't satisfy name validation — those weren't
		// created by squad and may belong to another tool.
		if err := ValidateName(e.Name()); err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Profile{
			Name:    e.Name(),
			Dir:     filepath.Join(root, e.Name()),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Delete removes the profile dir for name. Returns ErrProfileNotFound when
// the profile does not exist; other I/O failures are wrapped.
func Delete(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	dir := filepath.Join(Root(), name)
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrProfileNotFound
		}
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("expected directory at %s, found a non-directory", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove profile dir %s: %w", dir, err)
	}
	return nil
}
