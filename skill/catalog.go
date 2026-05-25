package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/cowdogmoo/squad/config"
)

// Scope identifies where a skill manifest lives. Scope ordering is meaningful:
// when two skills share a name, the lower-numbered scope wins.
type Scope int

const (
	// ScopeRepo is a skill checked into the project at <repo>/.squad/skills/<name>/SKILL.md.
	ScopeRepo Scope = iota
	// ScopeGlobal is a user-level skill at $XDG_CONFIG_HOME/squad/skills/<name>/SKILL.md.
	ScopeGlobal
	// ScopeCatalog is a skill from a shared catalog — a git repo cloned under
	// the skills cache dir or a directory the user has registered via
	// skills.local_paths. Lowest precedence so local edits always win.
	ScopeCatalog
)

// String returns the lowercase scope name used in CLI output and config.
func (s Scope) String() string {
	switch s {
	case ScopeRepo:
		return "repo"
	case ScopeGlobal:
		return "global"
	case ScopeCatalog:
		return "catalog"
	default:
		return fmt.Sprintf("scope(%d)", int(s))
	}
}

// ParseScope converts a CLI-supplied string to a Scope value.
func ParseScope(s string) (Scope, error) {
	switch s {
	case "repo":
		return ScopeRepo, nil
	case "global":
		return ScopeGlobal, nil
	case "catalog":
		return ScopeCatalog, nil
	default:
		return 0, fmt.Errorf("invalid skill scope %q (must be repo, global, or catalog)", s)
	}
}

// Entry is a fully resolved skill in the catalog. ManifestPath points at the
// SKILL.md; Dir is the parent directory containing references/, scripts/, etc.
type Entry struct {
	Manifest     *Manifest
	Scope        Scope
	Dir          string
	ManifestPath string
	// Origin is a human-readable label for `squad skill list` ("global" or the
	// repo root path).
	Origin string
	// Shadowed is true when another entry with the same name lives in a
	// higher-precedence scope. Shadowed entries are kept in the catalog so the
	// CLI can surface them, but they are excluded from Visible().
	Shadowed bool
}

// Name returns the skill name from the manifest. Convenience accessor for
// callers that don't want to dereference twice.
func (e Entry) Name() string {
	if e.Manifest == nil {
		return ""
	}
	return e.Manifest.Name
}

// Catalog is the set of skills discovered across one or more scopes. The
// zero value is unusable; construct via Discover.
type Catalog struct {
	entries []Entry
	// loadErrors records non-fatal parse/validation failures, one per skill
	// directory. Callers surface these as warnings; a broken skill never
	// blocks discovery of the rest.
	loadErrors []LoadError
}

// LoadError describes a single skill that could not be added to the catalog.
type LoadError struct {
	Path string
	Err  error
}

// Error implements the error interface.
func (e LoadError) Error() string {
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

// Unwrap allows errors.Is / errors.As to reach the underlying parse error.
func (e LoadError) Unwrap() error {
	return e.Err
}

// Discover scans the configured scopes for skill manifests. repoRoot, when
// non-empty, enables repo-scope discovery under <repoRoot>/.squad/skills/.
// catalogDirs are additional roots whose immediate subdirectories are scanned
// as catalog-scope skills (lowest precedence). A missing directory at any
// scope is not an error.
//
// Returns the catalog and (separately) the slice of non-fatal load errors.
// The catalog's All() method also exposes these via LoadErrors().
func Discover(repoRoot string, catalogDirs ...string) (*Catalog, error) {
	c := &Catalog{}

	if repoRoot != "" {
		if err := c.scanDir(RepoSkillsDir(repoRoot), ScopeRepo, repoRoot); err != nil {
			return nil, fmt.Errorf("scan repo skills: %w", err)
		}
	}

	globalDir, err := GlobalSkillsDir()
	if err != nil {
		return nil, fmt.Errorf("resolve global skills dir: %w", err)
	}
	if err := c.scanDir(globalDir, ScopeGlobal, "global"); err != nil {
		return nil, fmt.Errorf("scan global skills: %w", err)
	}

	for _, dir := range catalogDirs {
		if dir == "" {
			continue
		}
		if err := c.scanDir(dir, ScopeCatalog, dir); err != nil {
			return nil, fmt.Errorf("scan catalog skills at %s: %w", dir, err)
		}
	}

	c.resolveCollisions()
	c.sortStable()
	return c, nil
}

// scanDir loads skills from dir. The Agent Skills spec defines a skill as a
// directory containing SKILL.md, so dir itself is a skill when SKILL.md sits
// at its root (the layout used by single-skill catalog repos like
// blader/humanizer). Either way, immediate subdirectories are also scanned so
// monorepo-style catalogs continue to work. Parse failures become LoadErrors
// and are surfaced separately from successfully-loaded entries.
func (c *Catalog) scanDir(dir string, scope Scope, origin string) error {
	rootManifest := filepath.Join(dir, FileName)
	if info, err := os.Stat(rootManifest); err == nil && !info.IsDir() {
		if m, err := LoadManifest(rootManifest); err != nil {
			c.loadErrors = append(c.loadErrors, LoadError{Path: rootManifest, Err: err})
		} else {
			c.entries = append(c.entries, Entry{
				Manifest:     m,
				Scope:        scope,
				Dir:          dir,
				ManifestPath: rootManifest,
				Origin:       origin,
			})
		}
	}

	items, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, item := range items {
		if !item.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, item.Name())
		manifestPath := filepath.Join(skillDir, FileName)
		info, err := os.Stat(manifestPath)
		if err != nil || info.IsDir() {
			continue
		}
		m, err := LoadManifest(manifestPath)
		if err != nil {
			c.loadErrors = append(c.loadErrors, LoadError{Path: manifestPath, Err: err})
			continue
		}
		if m.Name != item.Name() {
			c.loadErrors = append(c.loadErrors, LoadError{
				Path: manifestPath,
				Err:  fmt.Errorf("manifest name %q does not match directory name %q", m.Name, item.Name()),
			})
			continue
		}
		c.entries = append(c.entries, Entry{
			Manifest:     m,
			Scope:        scope,
			Dir:          skillDir,
			ManifestPath: manifestPath,
			Origin:       origin,
		})
	}
	return nil
}

// resolveCollisions walks entries and marks every duplicate name (by lower
// precedence) as Shadowed. Entries are not removed so callers can list them.
func (c *Catalog) resolveCollisions() {
	winners := make(map[string]int) // name → index of the current winner
	for i := range c.entries {
		name := c.entries[i].Name()
		prev, ok := winners[name]
		if !ok {
			winners[name] = i
			continue
		}
		// Lower Scope value wins (ScopeRepo = 0 < ScopeGlobal = 1).
		if c.entries[i].Scope < c.entries[prev].Scope {
			c.entries[prev].Shadowed = true
			winners[name] = i
			continue
		}
		c.entries[i].Shadowed = true
	}
}

// sortStable orders entries by name then scope so list output is deterministic.
func (c *Catalog) sortStable() {
	sort.SliceStable(c.entries, func(i, j int) bool {
		a, b := c.entries[i], c.entries[j]
		if a.Name() != b.Name() {
			return a.Name() < b.Name()
		}
		return a.Scope < b.Scope
	})
}

// Visible returns the unshadowed entries — the set an agent should be told
// about via the system prompt.
func (c *Catalog) Visible() []Entry {
	out := make([]Entry, 0, len(c.entries))
	for _, e := range c.entries {
		if !e.Shadowed {
			out = append(out, e)
		}
	}
	return out
}

// All returns every discovered entry including shadowed ones. Used by
// `squad skill list` to surface conflicts.
func (c *Catalog) All() []Entry {
	out := make([]Entry, len(c.entries))
	copy(out, c.entries)
	return out
}

// LoadErrors returns the list of skills that could not be loaded.
func (c *Catalog) LoadErrors() []LoadError {
	out := make([]LoadError, len(c.loadErrors))
	copy(out, c.loadErrors)
	return out
}

// Find returns the visible entry with the given name. Shadowed entries are
// not returned — by definition the lookup should yield the active skill.
func (c *Catalog) Find(name string) (Entry, bool) {
	for _, e := range c.entries {
		if e.Shadowed {
			continue
		}
		if e.Name() == name {
			return e, true
		}
	}
	return Entry{}, false
}

// FilterOptions controls which visible entries an agent actually sees. Empty
// fields are treated as "no constraint".
type FilterOptions struct {
	// Scopes restricts to this set of scopes (empty = all).
	Scopes []Scope
	// Allow, when non-empty, restricts to skills whose name is listed.
	Allow []string
	// Deny removes skills whose name is listed, after Allow is applied.
	Deny []string
}

// Filter applies FilterOptions to the visible entries and returns the
// resulting list. Visible-only is intentional: filtering should not bring
// shadowed skills back to life.
//
// Precedence (per PLAN.md): allow > deny > scopes. When Allow is non-empty
// it is an exclusive allowlist — Deny and Scopes are ignored and only the
// listed names appear. When Allow is empty, the result is the entries
// matching Scopes minus the entries listed in Deny.
func (c *Catalog) Filter(opts FilterOptions) []Entry {
	allowSet := stringSet(opts.Allow)
	denySet := stringSet(opts.Deny)
	scopeSet := scopeSet(opts.Scopes)

	out := make([]Entry, 0, len(c.entries))
	for _, e := range c.entries {
		if e.Shadowed {
			continue
		}
		if len(allowSet) > 0 {
			if _, ok := allowSet[e.Name()]; !ok {
				continue
			}
			out = append(out, e)
			continue
		}
		if len(scopeSet) > 0 {
			if _, ok := scopeSet[e.Scope]; !ok {
				continue
			}
		}
		if _, denied := denySet[e.Name()]; denied {
			continue
		}
		out = append(out, e)
	}
	return out
}

// RepoSkillsDir returns the per-repo skills directory under repoRoot. It does
// not create the directory.
func RepoSkillsDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".squad", "skills")
}

// GlobalSkillsDir returns the user-level skills directory. The parent
// (`$XDG_CONFIG_HOME/squad/`) is created on demand by the config package; we
// do not create the `skills/` directory itself so that an absent directory
// remains a clean "no global skills" signal during discovery.
func GlobalSkillsDir() (string, error) {
	// config.ConfigFile creates the parent of its argument. We pass a
	// sentinel child path so the parent ("skills") becomes the directory we
	// return — same trick routine.GlobalRoutinesDir uses.
	sentinel, err := config.ConfigFile(filepath.Join("skills", "_sentinel"))
	if err != nil {
		return "", err
	}
	return filepath.Dir(sentinel), nil
}

func stringSet(in []string) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}

func scopeSet(in []Scope) map[Scope]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[Scope]struct{}, len(in))
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}
