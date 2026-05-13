package routine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/cowdogmoo/squad/logging"
	"github.com/fsnotify/fsnotify"
)

// Entry is a fully resolved routine, suitable for handing to the scheduler.
// It carries everything the daemon needs to load state, save state, and locate
// the manifest on disk for delete operations.
type Entry struct {
	Ref          Ref
	Routine      *Routine
	ManifestPath string
	StatePath    string
}

// EventType describes how an Entry changed in the store.
type EventType int

const (
	// EventAdded means a new routine appeared.
	EventAdded EventType = iota
	// EventUpdated means an existing routine's manifest changed.
	EventUpdated
	// EventRemoved means a routine's manifest was deleted.
	EventRemoved
)

// Event is emitted by Store.Watch for each detected manifest change.
type Event struct {
	Type  EventType
	Ref   Ref
	Entry Entry // For Added/Updated; zero for Removed.
}

// Store manages routine manifests across global and per-repo scopes. It is
// safe for concurrent use; the daemon shares a single Store between the
// scheduler goroutine and the watcher goroutine.
type Store struct {
	mu       sync.RWMutex
	entries  map[string]Entry // key = Ref.Qualified() + "\x00" + Ref.Root
	roots    []string         // current watched repo roots snapshot
	watcher  *fsnotify.Watcher
	watching map[string]struct{} // directories currently being watched
}

// NewStore returns an unwatched Store. Call LoadAll to populate entries and
// Watch to begin reacting to filesystem changes.
func NewStore() *Store {
	return &Store{
		entries:  make(map[string]Entry),
		watching: make(map[string]struct{}),
	}
}

// LoadAll scans the global routines dir and every watched repo's
// `.squad/routines` dir and replaces the in-memory entry set with what it
// finds. Returns the snapshot of entries after loading.
func (s *Store) LoadAll() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	roots, err := LoadRoots()
	if err != nil {
		return nil, err
	}
	s.roots = roots

	entries := make(map[string]Entry)

	globalDir, err := GlobalRoutinesDir()
	if err != nil {
		return nil, err
	}
	globalState, err := GlobalStateDir()
	if err != nil {
		return nil, err
	}
	if err := s.scanDir(entries, globalDir, ScopeGlobal, "", globalState); err != nil {
		return nil, err
	}

	for _, root := range roots {
		repoDir := RepoRoutinesDir(root)
		stateDir := RepoStateDir(root)
		if err := s.scanDir(entries, repoDir, ScopeRepo, root, stateDir); err != nil {
			return nil, err
		}
	}

	s.entries = entries
	return s.snapshotLocked(), nil
}

// scanDir reads dir for routine manifests and populates entries. Manifests
// that fail to parse are logged and skipped — one bad file should not break
// the rest of the daemon.
func (s *Store) scanDir(entries map[string]Entry, dir string, scope Scope, root, stateDir string) error {
	items, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, item := range items {
		if item.IsDir() || !isManifestFile(item.Name()) {
			continue
		}
		path := filepath.Join(dir, item.Name())
		r, err := LoadRoutine(path)
		if err != nil {
			logging.Warn("skipping invalid routine %s: %v", path, err)
			continue
		}
		ref := Ref{Scope: scope, Root: root, ID: r.ID}
		entry := Entry{
			Ref:          ref,
			Routine:      r,
			ManifestPath: path,
			StatePath:    filepath.Join(stateDir, StateFileName(r.ID)),
		}
		entries[entryKey(ref)] = entry
	}
	return nil
}

// Entries returns a snapshot of all known routines.
func (s *Store) Entries() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() []Entry {
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ref.Scope != out[j].Ref.Scope {
			return out[i].Ref.Scope < out[j].Ref.Scope
		}
		if out[i].Ref.Root != out[j].Ref.Root {
			return out[i].Ref.Root < out[j].Ref.Root
		}
		return out[i].Ref.ID < out[j].Ref.ID
	})
	return out
}

// Find returns the entry matching ref, or ok=false if not found.
func (s *Store) Find(ref Ref) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[entryKey(ref)]
	return e, ok
}

// FindByID returns every entry whose ID matches id, across all scopes/roots.
func (s *Store) FindByID(id string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Entry
	for _, e := range s.entries {
		if e.Ref.ID == id {
			out = append(out, e)
		}
	}
	return out
}

// Create writes the routine manifest for ref and adds it to the in-memory
// snapshot. ref.Root must be set for repo-scoped creates.
func (s *Store) Create(ref Ref, r *Routine) (Entry, error) {
	if r.ID != ref.ID {
		return Entry{}, fmt.Errorf("ref id %q does not match routine id %q", ref.ID, r.ID)
	}
	dir, stateDir, err := resolveDirs(ref)
	if err != nil {
		return Entry{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Entry{}, fmt.Errorf("create routines dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, FileName(r.ID))
	if _, err := os.Stat(path); err == nil {
		return Entry{}, fmt.Errorf("routine %s already exists at %s", ref.Qualified(), path)
	}
	if err := SaveRoutine(path, r); err != nil {
		return Entry{}, err
	}
	entry := Entry{
		Ref:          ref,
		Routine:      r,
		ManifestPath: path,
		StatePath:    filepath.Join(stateDir, StateFileName(r.ID)),
	}
	s.mu.Lock()
	s.entries[entryKey(ref)] = entry
	s.mu.Unlock()
	return entry, nil
}

// Delete removes the routine manifest and its state file. Missing files are
// not treated as errors so the operation is idempotent.
func (s *Store) Delete(ref Ref) error {
	s.mu.Lock()
	entry, ok := s.entries[entryKey(ref)]
	if ok {
		delete(s.entries, entryKey(ref))
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("routine %s not found", ref.Qualified())
	}
	if err := os.Remove(entry.ManifestPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Remove(entry.StatePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Update overwrites the manifest for an existing routine. The routine ID
// must match; renames are not supported (delete + create instead).
func (s *Store) Update(ref Ref, r *Routine) (Entry, error) {
	s.mu.RLock()
	existing, ok := s.entries[entryKey(ref)]
	s.mu.RUnlock()
	if !ok {
		return Entry{}, fmt.Errorf("routine %s not found", ref.Qualified())
	}
	if r.ID != ref.ID {
		return Entry{}, fmt.Errorf("cannot rename routine in update (got id %q)", r.ID)
	}
	if err := SaveRoutine(existing.ManifestPath, r); err != nil {
		return Entry{}, err
	}
	updated := existing
	updated.Routine = r
	s.mu.Lock()
	s.entries[entryKey(ref)] = updated
	s.mu.Unlock()
	return updated, nil
}

// LoadState reads the on-disk state for ref.
func (s *Store) LoadState(ref Ref) (*State, error) {
	entry, ok := s.Find(ref)
	if !ok {
		return nil, fmt.Errorf("routine %s not found", ref.Qualified())
	}
	return LoadState(entry.StatePath)
}

// SaveState writes the on-disk state for ref.
func (s *Store) SaveState(ref Ref, st *State) error {
	entry, ok := s.Find(ref)
	if !ok {
		return fmt.Errorf("routine %s not found", ref.Qualified())
	}
	return SaveState(entry.StatePath, st)
}

// routineChanged reports whether any scheduler-relevant field differs.
// Created-at and similar metadata are excluded.
func routineChanged(a, b *Routine) bool {
	if a == nil || b == nil {
		return a != b
	}
	if a.Schedule != b.Schedule || a.Agent != b.Agent || a.Enabled != b.Enabled {
		return true
	}
	if a.WorkingDir != b.WorkingDir || a.Provider != b.Provider || a.Model != b.Model {
		return true
	}
	if a.Prompt != b.Prompt || a.MaxCost != b.MaxCost || a.MaxIterations != b.MaxIterations {
		return true
	}
	if a.EffectiveCatchup() != b.EffectiveCatchup() {
		return true
	}
	if !stringMapEqual(a.Vars, b.Vars) {
		return true
	}
	return false
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// resolveDirs returns the manifest dir and state dir for a Ref, creating them
// as needed.
func resolveDirs(ref Ref) (manifestDir, stateDir string, err error) {
	switch ref.Scope {
	case ScopeGlobal:
		manifestDir, err = GlobalRoutinesDir()
		if err != nil {
			return "", "", err
		}
		stateDir, err = GlobalStateDir()
		if err != nil {
			return "", "", err
		}
	case ScopeRepo:
		if ref.Root == "" {
			return "", "", errors.New("repo-scoped routine requires Root")
		}
		manifestDir = RepoRoutinesDir(ref.Root)
		stateDir = RepoStateDir(ref.Root)
	default:
		return "", "", fmt.Errorf("invalid scope %q", ref.Scope)
	}
	return manifestDir, stateDir, nil
}

func entryKey(ref Ref) string {
	return string(ref.Scope) + "\x00" + ref.Root + "\x00" + ref.ID
}

func isManifestFile(name string) bool {
	return IDFromFileName(name) != ""
}
