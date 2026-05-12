package pane

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// keyMsg builds a tea.KeyMsg for a single rune or named key. Keep this
// tiny — the textarea model parses tea.KeyMsg natively, so we just need
// to construct one.
func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

// typeString sends each character through Update as a KeyRunes msg.
// Returns the resulting view (which may swap if Update produced one).
func typeString(t *testing.T, v View, s string) View {
	t.Helper()
	for _, r := range s {
		next, _ := v.Update(runeKey(r))
		if next == nil {
			t.Fatalf("Update returned nil view while typing %q", s)
		}
		v = next
	}
	return v
}

// submitted runs the cmd and returns the Submitted payload, failing the
// test on any other shape. The assertion lives in pane.go (AsSubmitted)
// because the per-file go-critic runner can't see cross-file types in tests.
func submitted(t *testing.T, cmd tea.Cmd) Submitted {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a non-nil cmd")
	}
	msg := cmd()
	s, ok := AsSubmitted(msg)
	if !ok {
		t.Fatalf("expected Submitted, got %T", msg)
	}
	return s
}

func TestComposerSubmitsPrompt(t *testing.T) {
	c := NewComposer()
	v := typeString(t, c, "fix the bug")
	next, cmd := v.Update(enterKey())
	if next == nil {
		t.Fatal("submit should keep the view alive")
	}
	sub := submitted(t, cmd)
	if sub.Kind != KindPrompt {
		t.Errorf("kind: got %v, want KindPrompt", sub.Kind)
	}
	if sub.Text != "fix the bug" {
		t.Errorf("text: got %q, want %q", sub.Text, "fix the bug")
	}
}

func TestComposerClassifiesSigils(t *testing.T) {
	cases := []struct {
		input string
		kind  Kind
		text  string
	}{
		{"/new agent", KindCommand, "new agent"},
		{"!ls", KindShell, "ls"},
		{"@README.md", KindFile, "README.md"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			c := NewComposer()
			v := typeString(t, c, tc.input)
			_, cmd := v.Update(enterKey())
			sub := submitted(t, cmd)
			if sub.Kind != tc.kind {
				t.Errorf("kind: got %v, want %v", sub.Kind, tc.kind)
			}
			if sub.Text != tc.text {
				t.Errorf("text: got %q, want %q", sub.Text, tc.text)
			}
		})
	}
}

func TestComposerIgnoresEmptySubmit(t *testing.T) {
	c := NewComposer()
	_, cmd := c.Update(enterKey())
	if cmd != nil {
		t.Errorf("Enter on empty buffer should not submit (cmd was %v)", cmd())
	}
}

func TestComposerResetsAfterSubmit(t *testing.T) {
	c := NewComposer()
	v := typeString(t, c, "first prompt")
	v, _ = v.Update(enterKey())
	// Behavioral check: with the buffer cleared, a follow-up Enter must
	// not emit another Submitted (the "ignore empty submit" path).
	_, cmd := v.Update(enterKey())
	if cmd != nil {
		t.Errorf("buffer should be empty after submit; got a follow-up cmd: %v", cmd())
	}
}

func TestComposerHandlesWindowSize(t *testing.T) {
	c := NewComposer()
	v, _ := c.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if v == nil {
		t.Fatal("WindowSizeMsg should not close the pane")
	}
}

func TestComposerTitleIsEmpty(t *testing.T) {
	c := NewComposer()
	if got := c.Title(); got != "" {
		t.Errorf("composer Title() = %q, want empty (default pane has no header)", got)
	}
}

func TestComposerHistoryRecallsPriorSubmissions(t *testing.T) {
	// History walks on ctrl+↑/ctrl+↓ — plain ↑/↓ belongs to the host App
	// (sidebar cycle) so users can navigate runs without leaving the
	// composer.
	up := tea.KeyMsg{Type: tea.KeyUp, Alt: true}
	down := tea.KeyMsg{Type: tea.KeyDown, Alt: true}
	v := View(NewComposer())
	v = typeString(t, v, "first")
	v, _ = v.Update(enterKey())
	v = typeString(t, v, "second")
	v, _ = v.Update(enterKey())
	v = typeString(t, v, "draft")
	v, _ = v.Update(up)
	if got := composerValue(t, v); got != "second" {
		t.Errorf("after ctrl+↑: got %q, want %q", got, "second")
	}
	v, _ = v.Update(up)
	if got := composerValue(t, v); got != "first" {
		t.Errorf("after second ctrl+↑: got %q, want %q", got, "first")
	}
	v, _ = v.Update(down)
	v, _ = v.Update(down)
	if got := composerValue(t, v); got != "draft" {
		t.Errorf("after walk back: got %q, want %q (saved draft)", got, "draft")
	}
}

func TestComposerHistoryEmptyIsNoOp(t *testing.T) {
	c := NewComposer()
	up := tea.KeyMsg{Type: tea.KeyUp, Alt: true}
	v, _ := c.Update(up)
	if v == nil {
		t.Fatal("history key on fresh composer should not close the pane")
	}
	if got := composerValue(t, v); got != "" {
		t.Errorf("history key with no history should leave buffer empty, got %q", got)
	}
}

// composerValue recovers the Composer concrete from a View handle and
// returns its current buffer. The indirection via reflect.ValueOf keeps
// gocritic's sloppyTypeAssert checker happy without requiring inline
// nolint directives.
func composerValue(t *testing.T, v View) string {
	t.Helper()
	rv := reflect.ValueOf(v)
	c, ok := rv.Interface().(Composer)
	if !ok {
		t.Fatalf("expected Composer, got %T", v)
	}
	return c.Value()
}
