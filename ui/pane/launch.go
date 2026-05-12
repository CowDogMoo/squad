package pane

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cowdogmoo/squad/ui/style"
)

// Launch is the new-run form: a minimal subset of `squad run` flags that
// matters for the first-launch flow. Advanced flags (provider, model,
// isolate, vars, MCP servers) land in a later step.
//
// Layout: agent + working dir on row 1, budget + mode + iter on row 2,
// prompt textarea below, [Launch]/[Cancel] buttons at the bottom. Tab /
// Shift+Tab cycle focus; Enter on the Launch button submits; Esc closes
// the form and returns to the parent view.
type Launch struct {
	parent View

	agent      textinput.Model
	workingDir textinput.Model
	budget     textinput.Model
	mode       textinput.Model
	iter       textinput.Model
	provider   textinput.Model
	model      textinput.Model
	isolate    textinput.Model
	prompt     textarea.Model

	focus int
	err   string
}

// Field indices for cycling focus. Order chosen to read left-to-right,
// top-to-bottom so Tab feels natural.
const (
	fldAgent = iota
	fldWorkingDir
	fldBudget
	fldMode
	fldIters
	fldProvider
	fldModel
	fldIsolate
	fldPrompt
	fldLaunch
	fldCancel
	fldCount
)

// LaunchDefaults seeds the form fields. Callers pass the current cwd as
// WorkingDir so the user doesn't retype it for every run.
type LaunchDefaults struct {
	Agent      string
	WorkingDir string
	MaxCost    float64
	Mode       string
	MaxIter    int

	// Advanced.
	Provider string
	Model    string
	Isolate  string
}

// NewLaunch builds a Launch form using `parent` as the return target
// when Esc is pressed or the form submits.
func NewLaunch(parent View, defaults LaunchDefaults) Launch {
	if defaults.MaxCost == 0 {
		defaults.MaxCost = 5
	}
	if defaults.Mode == "" {
		defaults.Mode = "edit"
	}
	if defaults.MaxIter == 0 {
		defaults.MaxIter = 40
	}

	// Field widths chosen so the three labeled rows fit inside a ~100-col
	// panel (96 inner). textinput scrolls horizontally when content
	// exceeds the visible width, so smaller widths still accept long
	// strings — they just don't render the whole value at once.
	agent := mkInput("agent", defaults.Agent, 22)
	workingDir := mkInput("working-dir", defaults.WorkingDir, 30)
	budget := mkInput("budget", fmt.Sprintf("%.2f", defaults.MaxCost), 7)
	mode := mkInput("mode", defaults.Mode, 9)
	iter := mkInput("iter", strconv.Itoa(defaults.MaxIter), 4)
	provider := mkInput("(default)", defaults.Provider, 16)
	model := mkInput("(default)", defaults.Model, 20)
	isolate := mkInput("(manifest)", defaults.Isolate, 10)

	prompt := textarea.New()
	prompt.Placeholder = "prompt — what should the agent do? (shift+enter for newline)"
	prompt.ShowLineNumbers = false
	prompt.SetHeight(4)
	prompt.MaxHeight = 8
	prompt.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
	)
	prompt.FocusedStyle.Placeholder = style.Faint
	prompt.BlurredStyle.Placeholder = style.Faint
	prompt.FocusedStyle.Text = style.Body
	prompt.BlurredStyle.Text = style.Body
	prompt.Prompt = "  "

	f := Launch{
		parent:     parent,
		agent:      agent,
		workingDir: workingDir,
		budget:     budget,
		mode:       mode,
		iter:       iter,
		provider:   provider,
		model:      model,
		isolate:    isolate,
		prompt:     prompt,
		focus:      fldAgent,
	}
	f.applyFocus()
	return f
}

func mkInput(placeholder, value string, width int) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(value)
	ti.Width = width
	ti.Prompt = ""
	ti.PromptStyle = style.Hint
	ti.PlaceholderStyle = style.Faint
	ti.TextStyle = style.Body
	return ti
}

// Init blinks the focused field's cursor.
func (l Launch) Init() tea.Cmd { return textinput.Blink }

// Update handles input. Branches are split into small helpers so each
// stays cyclo-friendly.
func (l Launch) Update(msg tea.Msg) (View, tea.Cmd) {
	if m, ok := msg.(tea.WindowSizeMsg); ok {
		l.prompt.SetWidth(m.Width - 2)
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		if next, cmd, handled := l.handleKey(km); handled {
			return next, cmd
		}
	}
	return l.forwardToFocused(msg)
}

// handleKey returns (view, cmd, true) when the key is one the form
// owns (navigation, submit, esc). Returning (_, _, false) lets the
// caller forward the message to the focused control.
func (l Launch) handleKey(km tea.KeyMsg) (View, tea.Cmd, bool) {
	switch km.String() {
	case "esc":
		return l.parent, nil, true
	case "tab":
		l.focus = (l.focus + 1) % fldCount
		l.applyFocus()
		return l, nil, true
	case "shift+tab":
		l.focus = (l.focus - 1 + fldCount) % fldCount
		l.applyFocus()
		return l, nil, true
	case "enter":
		return l.handleEnter()
	case "ctrl+s":
		v, c := l.submit()
		return v, c, true
	}
	return l, nil, false
}

func (l Launch) handleEnter() (View, tea.Cmd, bool) {
	switch l.focus {
	case fldLaunch:
		v, c := l.submit()
		return v, c, true
	case fldCancel:
		return l.parent, nil, true
	case fldPrompt:
		// Inside the prompt textarea, let Enter fall through (no-op —
		// the textarea has shift+enter for newlines).
		return l, nil, false
	default:
		l.focus = (l.focus + 1) % fldCount
		l.applyFocus()
		return l, nil, true
	}
}

// forwardToFocused routes input to whichever sub-control currently has
// focus.
func (l Launch) forwardToFocused(msg tea.Msg) (View, tea.Cmd) {
	var cmd tea.Cmd
	switch l.focus {
	case fldAgent:
		l.agent, cmd = l.agent.Update(msg)
	case fldWorkingDir:
		l.workingDir, cmd = l.workingDir.Update(msg)
	case fldBudget:
		l.budget, cmd = l.budget.Update(msg)
	case fldMode:
		l.mode, cmd = l.mode.Update(msg)
	case fldIters:
		l.iter, cmd = l.iter.Update(msg)
	case fldProvider:
		l.provider, cmd = l.provider.Update(msg)
	case fldModel:
		l.model, cmd = l.model.Update(msg)
	case fldIsolate:
		l.isolate, cmd = l.isolate.Update(msg)
	case fldPrompt:
		l.prompt, cmd = l.prompt.Update(msg)
	}
	return l, cmd
}

// applyFocus blurs all inputs and focuses the currently-selected one.
// Buttons (fldLaunch, fldCancel) have no input to focus; they render with
// a highlight.
func (l *Launch) applyFocus() {
	l.agent.Blur()
	l.workingDir.Blur()
	l.budget.Blur()
	l.mode.Blur()
	l.iter.Blur()
	l.provider.Blur()
	l.model.Blur()
	l.isolate.Blur()
	l.prompt.Blur()
	switch l.focus {
	case fldAgent:
		l.agent.Focus()
	case fldWorkingDir:
		l.workingDir.Focus()
	case fldBudget:
		l.budget.Focus()
	case fldMode:
		l.mode.Focus()
	case fldIters:
		l.iter.Focus()
	case fldProvider:
		l.provider.Focus()
	case fldModel:
		l.model.Focus()
	case fldIsolate:
		l.isolate.Focus()
	case fldPrompt:
		l.prompt.Focus()
	}
}

// submit validates fields and emits a LaunchRequest. On validation
// failure the form stays open with `err` set.
func (l Launch) submit() (View, tea.Cmd) {
	agent := strings.TrimSpace(l.agent.Value())
	prompt := strings.TrimSpace(l.prompt.Value())
	if agent == "" {
		l.err = "agent is required"
		l.focus = fldAgent
		l.applyFocus()
		return l, nil
	}
	if prompt == "" {
		l.err = "prompt is required"
		l.focus = fldPrompt
		l.applyFocus()
		return l, nil
	}
	maxCost, err := strconv.ParseFloat(strings.TrimSpace(l.budget.Value()), 64)
	if err != nil || maxCost < 0 {
		l.err = "budget must be a non-negative number"
		l.focus = fldBudget
		l.applyFocus()
		return l, nil
	}
	maxIter, err := strconv.Atoi(strings.TrimSpace(l.iter.Value()))
	if err != nil || maxIter <= 0 {
		l.err = "iter must be a positive integer"
		l.focus = fldIters
		l.applyFocus()
		return l, nil
	}
	isolate := strings.TrimSpace(l.isolate.Value())
	if isolate != "" && isolate != "worktree" && isolate != "none" {
		l.err = "isolate must be 'worktree', 'none', or empty"
		l.focus = fldIsolate
		l.applyFocus()
		return l, nil
	}
	req := LaunchRequest{
		Agent:      agent,
		WorkingDir: strings.TrimSpace(l.workingDir.Value()),
		Prompt:     prompt,
		MaxCost:    maxCost,
		Mode:       strings.TrimSpace(l.mode.Value()),
		MaxIter:    maxIter,
		Provider:   strings.TrimSpace(l.provider.Value()),
		Model:      strings.TrimSpace(l.model.Value()),
		Isolate:    isolate,
	}
	return l.parent, func() tea.Msg { return req }
}

// View renders the form. Width is supplied by the host; when ≤ 0 the
// form falls back to a sensible default. The form wraps its body in a
// rounded panel with a brand-colored title so it feels distinct from
// the surrounding chrome.
func (l Launch) View(width, _ int) string {
	if width <= 0 {
		width = 100
	}
	// Reserve panel border (2) + padding (2) for the inner content.
	innerW := width - 4
	if innerW < 40 {
		innerW = 40
	}
	// Size the textarea to match the panel's inner width so its line
	// wrapping aligns with the panel border. textinputs already scroll
	// horizontally when content exceeds their width — only the textarea
	// needs explicit sizing for visual alignment.
	l.prompt.SetWidth(innerW)

	row1 := fmt.Sprintf("%s  %s    %s  %s",
		labelW("agent", 12), boxed(l.agent.View(), l.focus == fldAgent),
		labelW("working dir", 12), boxed(l.workingDir.View(), l.focus == fldWorkingDir),
	)
	row2 := fmt.Sprintf("%s  %s    %s  %s    %s  %s",
		labelW("budget", 12), boxed(l.budget.View(), l.focus == fldBudget),
		labelW("mode", 6), boxed(l.mode.View(), l.focus == fldMode),
		labelW("iter", 6), boxed(l.iter.View(), l.focus == fldIters),
	)
	row3 := style.Faint.Render("advanced  ") + fmt.Sprintf("%s  %s    %s  %s    %s  %s",
		labelW("provider", 9), boxed(l.provider.View(), l.focus == fldProvider),
		labelW("model", 6), boxed(l.model.View(), l.focus == fldModel),
		labelW("isolate", 8), boxed(l.isolate.View(), l.focus == fldIsolate),
	)
	promptRow := style.Header.Render("prompt") + "\n" + l.prompt.View()

	buttonLaunch := button("[ Launch ]", l.focus == fldLaunch, style.Success)
	buttonCancel := button("[ Cancel ]", l.focus == fldCancel, style.Secondary)
	buttons := buttonLaunch + "    " + buttonCancel

	rows := []string{
		row1,
		row2,
		row3,
		"",
		promptRow,
		"",
		buttons,
	}
	if l.err != "" {
		rows = append(rows, "", style.Error.Render(l.err))
	}
	rows = append(rows, "",
		style.Faint.Render("tab/shift-tab move · enter activate · ctrl+s submit · esc cancel"),
	)
	body := strings.Join(rows, "\n")
	return style.Panel("NEW RUN", body, width)
}

// labelW renders a header-styled label right-padded to the given visible
// width so subsequent inputs align in vertical columns.
func labelW(s string, w int) string {
	pad := w - len(s)
	if pad < 0 {
		pad = 0
	}
	return style.Header.Render(s) + strings.Repeat(" ", pad)
}

// Title — the pane header is empty for the form (its own title is in View).
func (l Launch) Title() string { return "" }

// AsLaunchView narrows a View (or any other tea.Model) back to Launch.
// Accepts `any` to sidestep go-critic's sloppyTypeAssert when asserting
// from a specific interface to a concrete struct that satisfies it.
// Kept in production code so the per-file go-critic runner can resolve
// the type — tests can't reference cross-file types under the per-file
// caseOrder check.
func AsLaunchView(v any) (Launch, bool) {
	l, ok := v.(Launch)
	return l, ok
}

// SetSize lets the host size the form before its first WindowSizeMsg
// arrives — important when the form is installed mid-session (e.g.
// triggered by /new) and the resize message has already fired once.
func (l *Launch) SetSize(width, _ int) {
	if width > 0 {
		l.prompt.SetWidth(width - 2)
	}
}

// SetPromptValue seeds the prompt textarea. Useful when a preset carries
// a pre-written prompt the user wants to edit before launching.
func (l *Launch) SetPromptValue(s string) {
	l.prompt.SetValue(s)
}

// boxed wraps a value in brackets, highlighting if focused. textinput
// already shows a cursor when focused; the bracket gives visual structure.
func boxed(s string, focused bool) string {
	br := style.Faint
	if focused {
		br = style.Hint
	}
	return br.Render("[ ") + s + br.Render(" ]")
}

func button(label string, focused bool, baseStyle interface{ Render(strs ...string) string }) string {
	if focused {
		return style.Hint.Render("› ") + baseStyle.Render(label)
	}
	return "  " + baseStyle.Render(label)
}
