package pane

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cowdogmoo/squad/ui/style"
)

// typeahead is a small filterable list-of-options control: a textinput
// at the top, a substring-filtered list of suggestions below. Up/down
// move the highlight; Enter commits the highlighted value into the
// input. The host decides when to render the dropdown — typically only
// while the field has focus.
type typeahead struct {
	input   textinput.Model
	options []string
	cursor  int // index into filtered() for the highlighted row
	maxRows int // dropdown max rows; 0 → 6
}

func newTypeahead(placeholder, value string, width int, options []string) typeahead {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(value)
	ti.Width = width
	ti.Prompt = ""
	ti.PromptStyle = style.Hint
	ti.PlaceholderStyle = style.Faint
	ti.TextStyle = style.Body
	return typeahead{
		input:   ti,
		options: append([]string(nil), options...),
		maxRows: 6,
	}
}

func (t *typeahead) Focus() { t.input.Focus() }
func (t *typeahead) Blur()  { t.input.Blur() }
func (t typeahead) Value() string {
	return strings.TrimSpace(t.input.Value())
}
func (t *typeahead) SetValue(s string) { t.input.SetValue(s) }

// SetOptions replaces the suggestion pool and resets the dropdown
// cursor. Use when the option set depends on another field's value
// (e.g. model suggestions following the selected provider).
func (t *typeahead) SetOptions(options []string) {
	t.options = append([]string(nil), options...)
	t.cursor = 0
}

// CompletePrefix replaces the buffer with the longest common prefix
// of currently-filtered options whenever that prefix is longer than
// the buffer. Returns true when the buffer was actually extended —
// shell-style Tab completion. Case-insensitive matches are tolerated
// (e.g. `/use` → `/Users/`) by aligning to the option's casing.
func (t *typeahead) CompletePrefix() bool {
	matches := t.filtered()
	if len(matches) == 0 {
		return false
	}
	lcp := matches[0]
	for _, m := range matches[1:] {
		lcp = commonPrefix(lcp, m)
		if lcp == "" {
			break
		}
	}
	current := t.input.Value()
	if len(lcp) <= len(current) {
		return false
	}
	t.input.SetValue(lcp)
	t.input.CursorEnd()
	t.cursor = 0
	return true
}

func commonPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// filtered returns options whose name contains the current input as a
// substring (case-insensitive). Exact-prefix matches are ranked first.
func (t typeahead) filtered() []string {
	q := strings.ToLower(strings.TrimSpace(t.input.Value()))
	if q == "" {
		return t.options
	}
	var prefix, contains []string
	for _, o := range t.options {
		lo := strings.ToLower(o)
		switch {
		case strings.HasPrefix(lo, q):
			prefix = append(prefix, o)
		case strings.Contains(lo, q):
			contains = append(contains, o)
		}
	}
	return append(prefix, contains...)
}

// Update routes a tea.Msg through the typeahead. The host should call
// this only while the field is focused. Returns the (possibly mutated)
// receiver and an optional tea.Cmd to propagate.
func (t typeahead) Update(msg tea.Msg) (typeahead, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "up":
			if t.cursor > 0 {
				t.cursor--
			}
			return t, nil
		case "down":
			if t.cursor < len(t.filtered())-1 {
				t.cursor++
			}
			return t, nil
		case "enter":
			// Commit the highlighted suggestion (if any) into the input.
			matches := t.filtered()
			if t.cursor < len(matches) {
				t.input.SetValue(matches[t.cursor])
				t.input.CursorEnd()
			}
			return t, nil
		}
	}
	prev := t.input.Value()
	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	// Only reset the highlight when the query actually changed — moving
	// the text cursor or pressing a no-op key should leave the picker
	// alone. Without this guard, the highlight jumps back to row 0
	// after a Down-then-type sequence.
	if t.input.Value() != prev {
		t.cursor = 0
	}
	return t, cmd
}

// View renders the input box. The dropdown is rendered separately by the
// host (so the host can lay it out below the row containing this input).
func (t typeahead) View() string { return t.input.View() }

// DropdownView renders up to maxRows filtered options under a focused
// input. Highlighted row is styled with the brand cyan; others are body
// text. Returns "" when the list is empty or the control is blurred.
func (t typeahead) DropdownView() string {
	if !t.input.Focused() {
		return ""
	}
	matches := t.filtered()
	if len(matches) == 0 {
		return style.Faint.Render("  (no matches)")
	}
	limit := t.maxRows
	if limit <= 0 || limit > len(matches) {
		limit = len(matches)
	}
	// Scroll the window so the cursor row is always visible.
	start := 0
	if t.cursor >= limit {
		start = t.cursor - limit + 1
	}
	end := start + limit
	if end > len(matches) {
		end = len(matches)
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		row := matches[i]
		if i == t.cursor {
			b.WriteString(style.Hint.Render("  ▸ " + row))
		} else {
			b.WriteString(style.Secondary.Render("    " + row))
		}
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// DropdownHeight returns the number of rendered lines DropdownView
// produces, so the host can reserve space for it.
func (t typeahead) DropdownHeight() int {
	if !t.input.Focused() {
		return 0
	}
	matches := t.filtered()
	if len(matches) == 0 {
		return 1
	}
	limit := t.maxRows
	if limit <= 0 || limit > len(matches) {
		limit = len(matches)
	}
	return limit
}

// selectField is a "[ < value > ]" cycle picker for closed sets of short
// strings (mode, provider, isolate). Left/right and h/l cycle; space
// cycles forward. emptyLabel is shown when the current option is the
// empty string (which represents "use the default") so the user sees a
// meaningful word like "(default)" rather than blank space.
type selectField struct {
	options    []string
	emptyLabel string
	cursor     int
	focused    bool
}

func newSelectField(options []string, value, emptyLabel string) selectField {
	sf := selectField{options: append([]string(nil), options...), emptyLabel: emptyLabel}
	for i, o := range sf.options {
		if o == value {
			sf.cursor = i
			break
		}
	}
	return sf
}

func (s *selectField) Focus() { s.focused = true }
func (s *selectField) Blur()  { s.focused = false }
func (s selectField) Value() string {
	if s.cursor >= len(s.options) {
		return ""
	}
	return s.options[s.cursor]
}

// Update advances or rewinds the cursor on the relevant keys.
func (s selectField) Update(msg tea.Msg) (selectField, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch km.String() {
	case "left", "h":
		if s.cursor > 0 {
			s.cursor--
		}
	case "right", "l", " ":
		if s.cursor < len(s.options)-1 {
			s.cursor++
		}
	}
	return s, nil
}

// View renders the cycle widget. Width pads the displayed value so
// adjacent selects line up vertically. Empty values use emptyLabel
// rendered with the placeholder style.
func (s selectField) View(width int) string {
	raw := s.Value()
	display := raw
	textStyle := style.Body
	if raw == "" && s.emptyLabel != "" {
		display = s.emptyLabel
		textStyle = style.Faint
	}
	pad := width - len(display)
	if pad < 0 {
		pad = 0
	}
	bracket := style.Faint
	chev := style.Faint
	if s.focused {
		bracket = style.Hint
		chev = style.Hint
	}
	return bracket.Render("[ ") + chev.Render("‹ ") +
		textStyle.Render(display) + strings.Repeat(" ", pad) +
		chev.Render(" ›") + bracket.Render(" ]")
}
