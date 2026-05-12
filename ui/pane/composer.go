package pane

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cowdogmoo/squad/ui/style"
)

// Composer is the default bottom-pane view: a multi-line text input that
// accepts free-form prompts, slash commands (/foo), shell pass-through
// (!foo), and file mentions (@foo). Enter submits; shift+enter or ctrl+j
// inserts a newline (universal multi-line affordance — terminals vary on
// what shift+enter sends, ctrl+j always works).
//
// On submit the composer emits a Submitted message (with sigil-classified
// Kind) and clears its buffer. The host app pattern-matches on Submitted
// to route the action.
type Composer struct {
	ta textarea.Model
}

// NewComposer returns a Composer with sensible defaults: 3-line visible
// height, no line numbers, placeholder hint, prompt arrow.
func NewComposer() Composer {
	ta := textarea.New()
	ta.Placeholder = "type a prompt, /command, !shell, or @file …"
	ta.Prompt = "  "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(3)
	ta.MaxHeight = 8

	// Enter is reserved for submit; map newline-insert to shift+enter and
	// ctrl+j (universally available, regardless of terminal).
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "newline"),
	)

	ta.FocusedStyle.Prompt = style.Hint
	ta.FocusedStyle.Placeholder = style.Faint
	ta.FocusedStyle.Text = style.Body
	ta.BlurredStyle.Placeholder = style.Faint
	ta.BlurredStyle.Text = style.Body

	ta.Focus()
	return Composer{ta: ta}
}

// Init starts the textarea cursor blink.
func (c Composer) Init() tea.Cmd { return textarea.Blink }

// Update handles input. Enter on a non-empty buffer triggers a submit
// (Submitted msg) and resets the input. WindowSizeMsg adjusts width.
func (c Composer) Update(msg tea.Msg) (View, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		// Reserve 2 cols for the prompt gutter; leave a generous margin.
		c.ta.SetWidth(m.Width - 2)
	case tea.KeyMsg:
		if m.String() == "enter" {
			text := c.ta.Value()
			if strings.TrimSpace(text) == "" {
				// Ignore empty submits — let the user keep typing.
				return c, nil
			}
			kind, payload := ClassifyKind(text)
			c.ta.Reset()
			return c, func() tea.Msg { return Submitted{Kind: kind, Text: payload} }
		}
	}
	var cmd tea.Cmd
	c.ta, cmd = c.ta.Update(msg)
	return c, cmd
}

// View renders the composer. Width/height come from the host; if width is
// 0 we trust the textarea's existing size (set via WindowSizeMsg).
func (c Composer) View(width, _ int) string {
	if width > 0 {
		c.ta.SetWidth(width - 2)
	}
	return c.ta.View()
}

// Title — the composer is the always-mounted default, so it has no header.
func (c Composer) Title() string { return "" }

// Value returns the current buffer (untrimmed). Test helper; the host
// shouldn't normally need it.
func (c Composer) Value() string { return c.ta.Value() }

// SetValue replaces the buffer. Useful for hydrating a draft from
// history or restoring after a failed submit.
func (c *Composer) SetValue(s string) { c.ta.SetValue(s) }
