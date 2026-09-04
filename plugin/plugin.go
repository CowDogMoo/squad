// Package plugin parses the subset of the Claude Code plugin format that
// Squad consumes. A plugin is a directory whose optional manifest at
// .claude-plugin/plugin.json declares the components it ships; Squad reads
// the manifest as a content-declaration layer on top of its filesystem
// discovery. Unknown manifest fields are ignored so newer plugins keep
// parsing, mirroring the tolerance skill.Manifest applies to SKILL.md
// frontmatter.
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// ManifestDir is the directory that holds a plugin's manifest.
	ManifestDir = ".claude-plugin"
	// ManifestFile is the manifest filename inside ManifestDir.
	ManifestFile = "plugin.json"
	// DefaultSkillsDir is the component directory the plugin format scans
	// for skills even when the manifest declares no explicit roots.
	DefaultSkillsDir = "skills"
)

// Manifest is the subset of .claude-plugin/plugin.json that Squad reads.
type Manifest struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	// Skills lists additional skill roots relative to the plugin root. Per
	// the plugin format these extend (not replace) the default skills/
	// directory.
	Skills PathList `json:"skills,omitempty"`
}

// PathList unmarshals the plugin format's string-or-array shorthand for
// path-valued fields.
type PathList []string

// UnmarshalJSON accepts either a single JSON string or an array of strings.
func (p *PathList) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*p = PathList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("expected string or array of strings: %w", err)
	}
	*p = PathList(many)
	return nil
}

// ManifestPath returns the manifest location for a plugin rooted at root.
func ManifestPath(root string) string {
	return filepath.Join(root, ManifestDir, ManifestFile)
}

// Load reads and parses the manifest for the plugin rooted at root. A
// missing manifest returns an error satisfying errors.Is(err, fs.ErrNotExist)
// so callers can treat manifest-less directories as ordinary directories.
func Load(root string) (*Manifest, error) {
	data, err := os.ReadFile(ManifestPath(root))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ManifestPath(root), err)
	}
	return &m, nil
}

// SkillRoots resolves the manifest's declared skill roots against root and
// returns them as absolute paths. The default skills/ directory is included
// first when it exists, matching the format's extend-not-replace semantics.
// Entries that are absolute or escape root are rejected; valid entries are
// still returned alongside the joined error so one bad path never hides the
// rest.
func (m *Manifest) SkillRoots(root string) ([]string, error) {
	var roots []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == root || seen[p] {
			return
		}
		seen[p] = true
		roots = append(roots, p)
	}

	if info, err := os.Stat(filepath.Join(root, DefaultSkillsDir)); err == nil && info.IsDir() {
		add(filepath.Join(root, DefaultSkillsDir))
	}

	var errs []error
	for _, entry := range m.Skills {
		if entry == "" {
			continue
		}
		if filepath.IsAbs(entry) {
			errs = append(errs, fmt.Errorf("skill root %q: absolute paths are not allowed", entry))
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(entry))
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			errs = append(errs, fmt.Errorf("skill root %q: path escapes plugin root", entry))
			continue
		}
		if clean == "." {
			continue // the plugin root itself is already scanned by callers
		}
		add(filepath.Join(root, clean))
	}
	return roots, errors.Join(errs...)
}
