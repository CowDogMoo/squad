// Package presets stores reusable launch configurations on disk so users
// can replay a frequent run with one command (/preset load NAME) instead
// of re-typing the agent, working dir, budget, and prompt every time.
//
// Storage lives at $XDG_CONFIG_HOME/squad/presets.yaml (default
// ~/.config/squad/presets.yaml). The format is a tiny YAML document:
//
//	presets:
//	  - name: go-review-self
//	    agent: go-review
//	    working_dir: .
//	    max_cost: 5.0
//	    mode: edit
//	    max_iter: 40
//	    prompt: |
//	      review go files
//
// The file is rewritten atomically on every change.
package presets

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// Preset is a saved launch configuration. Fields mirror pane.LaunchRequest
// so the TUI can map back and forth without conversion code.
type Preset struct {
	Name       string    `yaml:"name"`
	Agent      string    `yaml:"agent"`
	WorkingDir string    `yaml:"working_dir,omitempty"`
	MaxCost    float64   `yaml:"max_cost,omitempty"`
	Mode       string    `yaml:"mode,omitempty"`
	MaxIter    int       `yaml:"max_iter,omitempty"`
	Prompt     string    `yaml:"prompt,omitempty"`
	UpdatedAt  time.Time `yaml:"updated_at,omitempty"`
}

// Store wraps a slice of presets with disk persistence. Methods are not
// safe for concurrent use; the TUI calls them from the bubble-tea
// Update goroutine.
type Store struct {
	path    string
	presets []Preset
}

// file is the on-disk schema. Kept separate from Store so we can extend
// the public type without breaking the wire format.
type file struct {
	Presets []Preset `yaml:"presets"`
}

// DefaultPath returns the conventional location for the presets file.
// Honors $XDG_CONFIG_HOME; falls back to $HOME/.config/squad/presets.yaml.
func DefaultPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "squad", "presets.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	return filepath.Join(home, ".config", "squad", "presets.yaml"), nil
}

// Load reads presets from path. A missing file returns an empty store
// (not an error) so first-run users see a clean slate.
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read presets %s: %w", path, err)
	}
	var f file
	if err := yaml.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("parse presets %s: %w", path, err)
	}
	s.presets = f.Presets
	return s, nil
}

// Path returns the file the store reads from / writes to.
func (s *Store) Path() string { return s.path }

// All returns presets sorted by name (case-insensitive).
func (s *Store) All() []Preset {
	out := make([]Preset, len(s.presets))
	copy(out, s.presets)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// Names returns just the sorted names (handy for fuzzy filters / tab
// completion).
func (s *Store) Names() []string {
	names := make([]string, 0, len(s.presets))
	for _, p := range s.presets {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}

// Get returns the preset by name (case-sensitive — keep names simple).
func (s *Store) Get(name string) (Preset, bool) {
	for _, p := range s.presets {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

// Set upserts a preset (matched by Name) and persists. UpdatedAt is
// stamped automatically.
func (s *Store) Set(p Preset) error {
	if p.Name == "" {
		return fmt.Errorf("preset name is required")
	}
	p.UpdatedAt = time.Now().UTC()
	for i, existing := range s.presets {
		if existing.Name == p.Name {
			s.presets[i] = p
			return s.save()
		}
	}
	s.presets = append(s.presets, p)
	return s.save()
}

// Remove deletes a preset by name. Returns false (no error) if the
// preset didn't exist.
func (s *Store) Remove(name string) (bool, error) {
	for i, p := range s.presets {
		if p.Name == name {
			s.presets = append(s.presets[:i], s.presets[i+1:]...)
			return true, s.save()
		}
	}
	return false, nil
}

// save atomically writes the on-disk file.
func (s *Store) save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	body, err := yaml.Marshal(file{Presets: s.presets})
	if err != nil {
		return fmt.Errorf("marshal presets: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp, s.path, err)
	}
	return nil
}
