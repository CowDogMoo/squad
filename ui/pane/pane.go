// Package pane defines the polymorphic bottom-pane mount point used by the
// squad TUI. Lifted from codex's bottom_pane pattern: the host app keeps
// one active View at a time and routes input + render to it. Different
// views — composer, slash-command popup, launch form, preset picker,
// confirmation — implement the same interface, so the host doesn't need
// to know about each view's internals.
//
// A View signals lifecycle changes through its return values:
//
//	Update returns (sameView, cmd) — stay current
//	Update returns (otherView, cmd) — swap; host installs otherView
//	Update returns (nil,       cmd) — close the pane entirely
//
// Cross-view messages (the composer asking the app to launch a run, the
// confirmation modal returning a decision) flow as tea.Cmds that emit
// view-defined tea.Msg payloads. The host pattern-matches on those.
package pane

import (
	tea "github.com/charmbracelet/bubbletea"
)

// View is the polymorphic bottom-pane mount. Implementations must be
// value types or carry their own pointers — the host stores whatever
// Update returns.
type View interface {
	// Init runs once when the view is installed. It can kick off a
	// blink timer, focus a text input, etc.
	Init() tea.Cmd

	// Update processes one message and returns the next view (which
	// may be the same view, a swapped-in replacement, or nil to close
	// the pane) plus an optional command.
	Update(msg tea.Msg) (View, tea.Cmd)

	// View renders the view at the given dimensions. Implementations
	// should not emit trailing newlines.
	View(width, height int) string

	// Title is shown in the pane header. Return "" to suppress.
	Title() string
}

// Kind classifies a composer submission. Sigils on the first non-empty
// character pick the kind: `/` command, `!` shell, `@` file mention.
// Everything else is a plain prompt.
type Kind int

const (
	KindPrompt Kind = iota
	KindCommand
	KindShell
	KindFile
)

// String returns the human-readable kind name (useful in tests/logs).
func (k Kind) String() string {
	switch k {
	case KindCommand:
		return "command"
	case KindShell:
		return "shell"
	case KindFile:
		return "file"
	default:
		return "prompt"
	}
}

// Submitted is the tea.Msg the composer emits when the user submits.
// The host handles it by, e.g., dispatching to the slash-command popup,
// running a shell, or starting an agent with the prompt text.
type Submitted struct {
	Kind Kind
	Text string // sigil stripped; leading/trailing whitespace trimmed
}

// AsSubmitted unwraps a tea.Msg to a Submitted. Returns the zero value
// and false if the message is a different type. Kept in production code
// so cross-file lint passes can resolve the type — the per-file go-critic
// runner can't see Submitted from a test file in isolation.
func AsSubmitted(m tea.Msg) (Submitted, bool) {
	s, ok := m.(Submitted)
	return s, ok
}

// LaunchRequest is the tea.Msg the launch form emits on submit. The host
// (app.App) consumes it to spawn a subprocess via the registry.
type LaunchRequest struct {
	Agent      string
	WorkingDir string
	Prompt     string
	MaxCost    float64
	Mode       string
	MaxIter    int
}

// AsLaunchRequest unwraps a tea.Msg to a LaunchRequest.
func AsLaunchRequest(m tea.Msg) (LaunchRequest, bool) {
	r, ok := m.(LaunchRequest)
	return r, ok
}

// ClassifyKind inspects raw composer text and returns its kind plus the
// payload with the leading sigil (if any) stripped. Exported so the
// command-palette and shell views can re-classify pasted text.
func ClassifyKind(raw string) (Kind, string) {
	trimmed := trimLeftSpace(raw)
	if trimmed == "" {
		return KindPrompt, ""
	}
	switch trimmed[0] {
	case '/':
		return KindCommand, trimSpace(trimmed[1:])
	case '!':
		return KindShell, trimSpace(trimmed[1:])
	case '@':
		return KindFile, trimSpace(trimmed[1:])
	default:
		return KindPrompt, trimSpace(trimmed)
	}
}

// trimLeftSpace + trimSpace are tiny helpers so this package has no
// stdlib dependencies beyond bubbletea — keeps the interface header light.
func trimLeftSpace(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return s[i:]
		}
	}
	return ""
}

func trimSpace(s string) string {
	s = trimLeftSpace(s)
	for len(s) > 0 {
		c := s[len(s)-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}
