package routine

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Scope identifies where a routine lives.
type Scope string

const (
	// ScopeGlobal is a user-level routine stored under XDG config.
	ScopeGlobal Scope = "global"
	// ScopeRepo is a per-repo routine stored under <repo>/.squad/routines/.
	ScopeRepo Scope = "repo"
)

// IsValid reports whether s is one of the recognised scope values.
func (s Scope) IsValid() bool {
	return s == ScopeGlobal || s == ScopeRepo
}

// Ref uniquely identifies a routine across both scopes. Root is the repo root
// for repo-scoped routines and is empty for global routines.
type Ref struct {
	Scope Scope
	Root  string // empty for global
	ID    string
}

// Qualified is a string form suitable for log lines and the session
// "routine_id" tag: "global:<id>" or "repo:<id>" (with repo display name
// elided so the same routine across machines has the same identity).
func (r Ref) Qualified() string {
	return fmt.Sprintf("%s:%s", r.Scope, r.ID)
}

// Display is a human-facing rendering for `squad routine list`:
//   - global:        "global"
//   - repo (foo):    "repo:foo"
func (r Ref) Display() string {
	if r.Scope == ScopeRepo && r.Root != "" {
		return fmt.Sprintf("repo:%s", filepath.Base(r.Root))
	}
	return string(r.Scope)
}

// ParseQualified parses "<scope>:<id>" into a partial Ref. The Root is left
// blank — callers that need it resolve it from the watched-roots registry.
// Returns ok=false when s is not in qualified form.
func ParseQualified(s string) (Ref, bool) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return Ref{}, false
	}
	scope := Scope(s[:idx])
	if !scope.IsValid() {
		return Ref{}, false
	}
	id := s[idx+1:]
	if ValidateID(id) != nil {
		return Ref{}, false
	}
	return Ref{Scope: scope, ID: id}, true
}

// Resolve picks a single Ref from a set of candidates that all share the same
// bare ID. inRepoRoot, when non-empty, biases toward a repo-scoped match
// whose Root equals that path (the user is running the command inside a
// watched repo).
//
// Returns an error describing the ambiguity when no unique match can be
// determined, with the qualified options listed for the user.
func Resolve(candidates []Ref, inRepoRoot string) (Ref, error) {
	switch len(candidates) {
	case 0:
		return Ref{}, fmt.Errorf("no routine matched")
	case 1:
		return candidates[0], nil
	}
	if inRepoRoot != "" {
		for _, c := range candidates {
			if c.Scope == ScopeRepo && c.Root == inRepoRoot {
				return c, nil
			}
		}
	}
	return Ref{}, ambiguityError(candidates)
}

func ambiguityError(candidates []Ref) error {
	opts := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if c.Scope == ScopeRepo && c.Root != "" {
			opts = append(opts, fmt.Sprintf("%s (%s)", c.Qualified(), c.Root))
		} else {
			opts = append(opts, c.Qualified())
		}
	}
	return fmt.Errorf("routine id is ambiguous; specify scope: %s", strings.Join(opts, ", "))
}
