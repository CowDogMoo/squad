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
//
// History: ↑/↓ recall previously submitted entries. Index -1 means "not
// browsing — buffer is the live draft." When the user starts walking
// back, the live draft is saved in `draft` so ↓ past the newest entry
// restores it. Bounded by historyMax to keep memory predictable.
type Composer struct {
	ta      textarea.Model
	history []string
	histIdx int    // -1 = not navigating
	draft   string // live buffer saved when history nav begins
}

// historyMax caps how many past submissions the composer remembers.
// Big enough that the limit is rarely the user's bottleneck; small
// enough that the slice never balloons.
const historyMax = 100

// NewComposer returns a Composer with sensible defaults: 1-line visible
// height when empty (grows to MaxHeight as the user types), no line
// numbers, placeholder hint, prompt arrow.
func NewComposer() Composer {
	ta := textarea.New()
	ta.Placeholder = "type a prompt, /command, !shell, or @file …"
	ta.Prompt = "> "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(1)
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
	return Composer{ta: ta, histIdx: -1}
}

// Init starts the textarea cursor blink.
func (c Composer) Init() tea.Cmd { return textarea.Blink }

// Update handles input. Enter on a non-empty buffer triggers a submit
// (Submitted msg) and resets the input. WindowSizeMsg adjusts width.
// Ctrl+↑/Ctrl+↓ (with alt+↑/↓ aliases for terminals that don't pass
// ctrl-modified arrows) walk submission history. Plain ↑/↓ is owned by
// the host App for sidebar cycling.
func (c Composer) Update(msg tea.Msg) (View, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		// Reserve 2 cols for the prompt gutter; leave a generous margin.
		c.ta.SetWidth(m.Width - 2)
	case tea.KeyMsg:
		switch m.String() {
		case "enter":
			text := c.ta.Value()
			if strings.TrimSpace(text) == "" {
				// Ignore empty submits — let the user keep typing.
				return c, nil
			}
			kind, payload := ClassifyKind(text)
			c.pushHistory(text)
			c.histIdx = -1
			c.draft = ""
			c.ta.Reset()
			return c, func() tea.Msg { return Submitted{Kind: kind, Text: payload} }
		case "ctrl+up", "alt+up":
			if c.historyPrev() {
				return c, nil
			}
		case "ctrl+down", "alt+down":
			if c.historyNext() {
				return c, nil
			}
		}
	}
	var cmd tea.Cmd
	c.ta, cmd = c.ta.Update(msg)
	c.fitHeight()
	return c, cmd
}

// pushHistory appends a submission, deduping back-to-back repeats and
// capping at historyMax (oldest evicted first).
func (c *Composer) pushHistory(text string) {
	if n := len(c.history); n > 0 && c.history[n-1] == text {
		return
	}
	c.history = append(c.history, text)
	if len(c.history) > historyMax {
		c.history = c.history[len(c.history)-historyMax:]
	}
}

// historyPrev replaces the buffer with the previous submission. Returns
// false when there's no history (caller should let ↑ flow through to
// the textarea's normal cursor handling).
func (c *Composer) historyPrev() bool {
	if len(c.history) == 0 {
		return false
	}
	switch {
	case c.histIdx == -1:
		c.draft = c.ta.Value()
		c.histIdx = len(c.history) - 1
	case c.histIdx > 0:
		c.histIdx--
	default:
		// Already at the oldest entry — swallow the key so it doesn't
		// move the textarea cursor unexpectedly.
		return true
	}
	c.ta.SetValue(c.history[c.histIdx])
	c.ta.CursorEnd()
	c.fitHeight()
	return true
}

// historyNext walks toward newer submissions. Past the newest, the
// saved draft is restored. Returns false when not currently navigating.
func (c *Composer) historyNext() bool {
	if c.histIdx == -1 {
		return false
	}
	c.histIdx++
	if c.histIdx >= len(c.history) {
		c.histIdx = -1
		c.ta.SetValue(c.draft)
		c.draft = ""
	} else {
		c.ta.SetValue(c.history[c.histIdx])
	}
	c.ta.CursorEnd()
	c.fitHeight()
	return true
}

// fitHeight resizes the textarea to match the buffer's line count, capped
// at MaxHeight. Keeps the composer flat (1 row) when empty so the bottom
// edge doesn't display stacks of empty `>` rows.
func (c *Composer) fitHeight() {
	lines := strings.Count(c.ta.Value(), "\n") + 1
	if lines < 1 {
		lines = 1
	}
	if lines > c.ta.MaxHeight {
		lines = c.ta.MaxHeight
	}
	c.ta.SetHeight(lines)
}

// View renders the composer. Width/height come from the host; if width is
// 0 we trust the textarea's existing size (set via WindowSizeMsg).
func (c Composer) View(width, _ int) string {
	if width > 0 {
		c.ta.SetWidth(width - 2)
	}
	return c.ta.View()
}

// Height returns the composer's current rendered row count. The host uses
// it to size the panel above so the chrome fills the terminal.
func (c Composer) Height() int { return c.ta.Height() }

// Title — the composer is the always-mounted default, so it has no header.
func (c Composer) Title() string { return "" }

// Value returns the current buffer (untrimmed). Test helper; the host
// shouldn't normally need it.
func (c Composer) Value() string { return c.ta.Value() }

// SetValue replaces the buffer. Useful for hydrating a draft from
// history or restoring after a failed submit.
func (c *Composer) SetValue(s string) { c.ta.SetValue(s) }
