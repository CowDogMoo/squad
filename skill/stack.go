package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
)

// Stack is the per-run set of skill directories that have been activated via
// the Skill tool. While a skill is on the stack, the runner permits Read and
// Bash to operate on files inside that directory in addition to the working
// directory.
//
// A stack rather than a single value because a skill is allowed (rarely) to
// invoke another skill mid-execution. Popping is not exposed — once activated
// a skill remains visible for the rest of the run, matching the spec
// expectation that "loaded" instructions remain in context.
//
// All methods are safe for concurrent use; the stack is touched from the
// tool-dispatch goroutine and may be observed from background task workers.
type Stack struct {
	mu   sync.RWMutex
	dirs []Entry
}

// NewStack returns an empty stack.
func NewStack() *Stack {
	return &Stack{}
}

// Push adds an entry to the stack. Idempotent: re-pushing a skill already on
// the stack is a no-op.
func (s *Stack) Push(e Entry) {
	if s == nil || e.Dir == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.dirs {
		if existing.Dir == e.Dir {
			return
		}
	}
	s.dirs = append(s.dirs, e)
}

// Top returns the most recently pushed entry. ok is false when the stack
// is empty.
func (s *Stack) Top() (Entry, bool) {
	if s == nil {
		return Entry{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.dirs) == 0 {
		return Entry{}, false
	}
	return s.dirs[len(s.dirs)-1], true
}

// Entries returns a snapshot of every entry on the stack, oldest first.
func (s *Stack) Entries() []Entry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, len(s.dirs))
	copy(out, s.dirs)
	return out
}

// Len reports the number of entries on the stack.
func (s *Stack) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.dirs)
}

// Contains reports whether abs sits at or beneath any stacked skill dir.
// abs must already be cleaned and absolute.
func (s *Stack) Contains(abs string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.dirs {
		if isWithin(abs, e.Dir) {
			return true
		}
	}
	return false
}

// Resolve interprets input the way the file tools do — absolute paths are
// taken as-is, relative paths are joined against the top-of-stack skill dir —
// and reports success only if the result is inside an active skill dir.
//
// Returns ("", false) when the stack is empty or the path escapes every
// active skill dir. Callers fall back to the working-dir anchor in that case.
func (s *Stack) Resolve(input string) (string, bool) {
	if s == nil {
		return "", false
	}
	if strings.TrimSpace(input) == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.dirs) == 0 {
		return "", false
	}

	if filepath.IsAbs(input) {
		abs, err := filepath.Abs(filepath.Clean(input))
		if err != nil {
			return "", false
		}
		for _, e := range s.dirs {
			if isWithin(abs, e.Dir) {
				return abs, true
			}
		}
		return "", false
	}

	// Relative path: try each stacked dir, most recent first, since that
	// matches "current skill scope".
	for i := len(s.dirs) - 1; i >= 0; i-- {
		base := s.dirs[i].Dir
		joined := filepath.Join(base, input)
		abs, err := filepath.Abs(joined)
		if err != nil {
			continue
		}
		if isWithin(abs, base) {
			return abs, true
		}
	}
	return "", false
}

// VerifyNoSymlinkEscape defends the relaxed read path against a skill dir that
// contains a symlink pointing outside itself. Resolve does a purely lexical
// containment check (it must, since callers resolve paths to files that may
// not exist yet); this method closes the gap for paths that DO exist by
// resolving symlinks on both the target and every active skill dir and
// re-checking containment on the real paths.
//
// A path that does not exist is allowed through unchanged — lexical
// containment already held and the caller's file operation surfaces its own
// not-found error. An existing path that resolves outside every skill dir is
// rejected.
func (s *Stack) VerifyNoSymlinkEscape(abs string) error {
	if s == nil {
		return nil
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.dirs {
		realBase, berr := filepath.EvalSymlinks(e.Dir)
		if berr != nil {
			continue
		}
		if isWithin(real, realBase) {
			return nil
		}
	}
	return fmt.Errorf("path %q resolves outside the active skill directory via symlink", abs)
}

// isWithin reports whether child sits at or beneath parent. Both must be
// cleaned and absolute. Mirrors routine.isWithin but lives here to avoid an
// import cycle (routine depends on skill semantics through PLAN.md but the
// skill package must not depend back).
func isWithin(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
