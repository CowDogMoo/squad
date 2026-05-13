package pane

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cowdogmoo/squad/metrics"
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

	agent      typeahead
	workingDir typeahead
	budget     textinput.Model
	mode       selectField
	iter       textinput.Model
	provider   typeahead
	model      typeahead
	isolate    selectField
	prompt     textarea.Model

	focus int
	err   string
}

// modeOptions, providerOptions, isolateOptions are the closed sets
// presented in the corresponding select fields. Empty strings stand for
// "use the default" — the agent manifest decides when no value is given.
var (
	modeOptions     = []string{"edit", "plan"}
	providerOptions = []string{"openai", "openai-responses", "anthropic", "gemini", "ollama"}
	isolateOptions  = []string{"", "worktree", "branch", "commit", "staged", "unstaged", "none"}
)

// modelsForProvider returns the model typeahead options for `p`. The
// list is derived from the live LiteLLM registry when available and
// falls back to an embedded curated list otherwise (see metrics
// package). The typeahead still accepts any free-text value the user
// types.
func modelsForProvider(p string) []string {
	return metrics.ModelsForProvider(p)
}

// workingDirSuggestions returns subdirectory completions for the
// user's current input. Output paths use the same prefix style as the
// input (~/foo, /abs/foo, or bare relative) so the typeahead's prefix
// filter narrows correctly as the user keeps typing. Dotfiles and
// regular files are excluded; the field is for directories.
func workingDirSuggestions(path string) []string {
	realDir, displayPrefix := completionContext(path)
	if realDir == "" {
		return nil
	}
	entries, err := os.ReadDir(realDir)
	if err != nil {
		return nil
	}
	sep := string(filepath.Separator)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, displayPrefix+e.Name()+sep)
	}
	sort.Strings(out)
	return out
}

// completionContext splits the user's input into the directory to
// read on disk (`realDir`) and the prefix to put on each suggestion
// (`displayPrefix`). Keeping the prefix in the user's style — tilde,
// absolute, or relative — is what lets the typeahead's prefix filter
// match the suggestions against the buffer.
func completionContext(path string) (realDir, displayPrefix string) {
	sep := string(filepath.Separator)
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", ""
		}
		return cwd, ""
	}
	// displayPrefix is the input up through the last separator. With
	// no separator the user typed a bare segment to be matched under
	// cwd, so the prefix is empty.
	if i := strings.LastIndex(path, sep); i >= 0 {
		displayPrefix = path[:i+1]
	}
	// realDir is displayPrefix with `~` and `$VAR` / `${VAR}` env
	// references expanded, plus a fallback to cwd when the user
	// hasn't typed a separator yet. Display prefix stays in the
	// user's original style so suggestions keep matching the buffer
	// (e.g. "$HOME/Downloads/" instead of "/Users/me/Downloads/").
	realDir = displayPrefix
	if strings.HasPrefix(realDir, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			realDir = home + strings.TrimPrefix(realDir, "~")
		}
	}
	realDir = os.ExpandEnv(realDir)
	if realDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", ""
		}
		realDir = cwd
	}
	return realDir, displayPrefix
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
// WorkingDir so the user doesn't retype it for every run. Agents is the
// list of discoverable agent names — when non-empty the agent field
// renders a filterable typeahead dropdown instead of a free-text input.
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

	Agents []string
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
	agentInput := newTypeahead("agent", defaults.Agent, 22, defaults.Agents)
	workingDir := newTypeahead("working-dir", defaults.WorkingDir, 30, workingDirSuggestions(defaults.WorkingDir))
	budget := mkInput("budget", fmt.Sprintf("%.2f", defaults.MaxCost), 7)
	mode := newSelectField(modeOptions, defaults.Mode, "edit")
	iter := mkInput("iter", strconv.Itoa(defaults.MaxIter), 4)
	provider := newTypeahead("(default)", defaults.Provider, 16, providerOptions)
	model := newTypeahead("(default)", defaults.Model, 20, modelsForProvider(defaults.Provider))
	isolate := newSelectField(isolateOptions, defaults.Isolate, "(manifest)")

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
		agent:      agentInput,
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
	case "up":
		if l.fieldOwnsVerticalKeys() {
			return l, nil, false
		}
		l.focus = (l.focus - 1 + fldCount) % fldCount
		l.applyFocus()
		return l, nil, true
	case "down":
		if l.fieldOwnsVerticalKeys() {
			return l, nil, false
		}
		l.focus = (l.focus + 1) % fldCount
		l.applyFocus()
		return l, nil, true
	case "left":
		if l.fieldOwnsHorizontalKeys() {
			return l, nil, false
		}
		l.focus = (l.focus - 1 + fldCount) % fldCount
		l.applyFocus()
		return l, nil, true
	case "right":
		if l.fieldOwnsHorizontalKeys() {
			return l, nil, false
		}
		l.focus = (l.focus + 1) % fldCount
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

// fieldOwnsVerticalKeys reports whether the currently focused field
// uses ↑/↓ for its own purpose (typeahead dropdown navigation, textarea
// line nav). For all other fields ↑/↓ moves focus to the prev/next
// field — the natural form-nav behavior on single-line inputs.
func (l Launch) fieldOwnsVerticalKeys() bool {
	switch l.focus {
	case fldAgent, fldWorkingDir, fldProvider, fldModel, fldPrompt:
		return true
	}
	return false
}

// fieldOwnsHorizontalKeys reports whether the focused field uses ←/→
// for its own purpose. Text inputs and the prompt textarea need them
// for cursor movement; selectField widgets use them to cycle values.
// Only the Launch / Cancel buttons leave them free for form-nav.
func (l Launch) fieldOwnsHorizontalKeys() bool {
	switch l.focus {
	case fldLaunch, fldCancel:
		return false
	}
	return true
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
	case fldAgent:
		// Commit the highlighted dropdown match into the input, then
		// advance focus. Lets the user pick a suggestion with Enter
		// without it acting as a form submit.
		l.agent, _ = l.agent.Update(tea.KeyMsg{Type: tea.KeyEnter})
		l.focus = (l.focus + 1) % fldCount
		l.applyFocus()
		return l, nil, true
	case fldWorkingDir:
		// Commit the highlighted directory into the input. When the
		// commit took, keep focus on the field so the user can keep
		// walking down the tree; otherwise treat Enter as "next field".
		prev := l.workingDir.Value()
		l.workingDir, _ = l.workingDir.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if l.workingDir.Value() != prev {
			l.workingDir.SetOptions(workingDirSuggestions(l.workingDir.Value()))
			return l, nil, true
		}
		l.focus = (l.focus + 1) % fldCount
		l.applyFocus()
		return l, nil, true
	case fldProvider:
		// Same dropdown-commit-then-advance behavior for the provider
		// typeahead.
		prev := l.provider.Value()
		l.provider, _ = l.provider.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if l.provider.Value() != prev {
			l.model.SetOptions(modelsForProvider(l.provider.Value()))
		}
		l.focus = (l.focus + 1) % fldCount
		l.applyFocus()
		return l, nil, true
	case fldModel:
		// Same dropdown-commit-then-advance behavior for the model
		// typeahead.
		l.model, _ = l.model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		l.focus = (l.focus + 1) % fldCount
		l.applyFocus()
		return l, nil, true
	default:
		l.focus = (l.focus + 1) % fldCount
		l.applyFocus()
		return l, nil, true
	}
}

// forwardToFocused routes input to whichever sub-control currently has
// focus.
func (l Launch) forwardToFocused(msg tea.Msg) (View, tea.Cmd) {
	if !l.acceptKey(msg) {
		return l, nil
	}
	return l.dispatchToField(msg)
}

// acceptKey gates rune input on the numeric fields so letters and
// stray punctuation never enter the buffer. Returns true for everything
// else (backspace, arrows, non-rune keys, non-numeric fields).
func (l Launch) acceptKey(msg tea.Msg) bool {
	switch l.focus {
	case fldBudget:
		return budgetAcceptKey(msg, l.budget.Value())
	case fldIters:
		return digitsOnlyAcceptKey(msg)
	}
	return true
}

// dispatchToField forwards `msg` to the focused sub-control and runs
// any cross-field side-effects (provider→model option refresh,
// workingDir filesystem rescan).
func (l Launch) dispatchToField(msg tea.Msg) (View, tea.Cmd) {
	var cmd tea.Cmd
	switch l.focus {
	case fldAgent:
		l.agent, cmd = l.agent.Update(msg)
	case fldWorkingDir:
		prev := l.workingDir.Value()
		l.workingDir, cmd = l.workingDir.Update(msg)
		if l.workingDir.Value() != prev {
			l.workingDir.SetOptions(workingDirSuggestions(l.workingDir.Value()))
		}
	case fldBudget:
		l.budget, cmd = l.budget.Update(msg)
	case fldMode:
		l.mode, cmd = l.mode.Update(msg)
	case fldIters:
		l.iter, cmd = l.iter.Update(msg)
	case fldProvider:
		prev := l.provider.Value()
		l.provider, cmd = l.provider.Update(msg)
		if l.provider.Value() != prev {
			l.model.SetOptions(modelsForProvider(l.provider.Value()))
		}
	case fldModel:
		l.model, cmd = l.model.Update(msg)
	case fldIsolate:
		l.isolate, cmd = l.isolate.Update(msg)
	case fldPrompt:
		l.prompt, cmd = l.prompt.Update(msg)
	}
	return l, cmd
}

// digitsOnlyAcceptKey returns false for rune keystrokes that aren't
// digits 0-9 (so the iter field rejects letters, `-`, `.`). Non-rune
// keys like backspace and arrows return true.
func digitsOnlyAcceptKey(msg tea.Msg) bool {
	km, ok := msg.(tea.KeyMsg)
	if !ok || km.Type != tea.KeyRunes {
		return true
	}
	for _, r := range km.Runes {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// budgetAcceptKey returns false for rune keystrokes that aren't digits
// or the first dot. `current` is the existing field value so the second
// dot can be rejected without resorting to ParseFloat on every keystroke.
func budgetAcceptKey(msg tea.Msg, current string) bool {
	km, ok := msg.(tea.KeyMsg)
	if !ok || km.Type != tea.KeyRunes {
		return true
	}
	hasDot := strings.ContainsRune(current, '.')
	for _, r := range km.Runes {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' && !hasDot:
			hasDot = true
		default:
			return false
		}
	}
	return true
}

// onAgentField reports whether focus is on the agent typeahead. Used to
// decide which keys (up/down/enter) the form swallows vs. forwards to
// the dropdown.
func (l Launch) onAgentField() bool { return l.focus == fldAgent }

// contextualHint returns a faint one-line description of the currently
// focused field — what each option means or how the value is used. The
// host always reserves a row for it so the layout doesn't jump when
// focus moves between fields.
func (l Launch) contextualHint() string {
	var hint string
	switch l.focus {
	case fldAgent:
		hint = "↑/↓ pick · enter commit · type to filter"
	case fldWorkingDir:
		hint = "directory the agent operates in (defaults to cwd)"
	case fldBudget:
		hint = "max USD spend before the agent stops"
	case fldMode:
		hint = "edit: agent may modify files · plan: analysis only (template var .Mode)"
	case fldIters:
		hint = "max LLM round trips before the agent stops"
	case fldProvider:
		hint = "(default): use config.yaml · openai/openai-responses/anthropic/gemini/ollama: override"
	case fldModel:
		hint = "↑/↓ pick · enter commit · type to filter · provider-specific; empty = manifest default"
	case fldIsolate:
		hint = "(manifest)/worktree: separate dir · branch/commit/staged: in-place w/ git snapshot · unstaged/none: in-place as-is"
	case fldPrompt:
		hint = "what should the agent do? · shift+enter for newline · ctrl+s to submit"
	}
	return style.Faint.Render("  " + hint)
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
	// isolate is a closed-set selectField — no validation needed.
	req := LaunchRequest{
		Agent:      agent,
		WorkingDir: strings.TrimSpace(l.workingDir.Value()),
		Prompt:     prompt,
		MaxCost:    maxCost,
		Mode:       strings.TrimSpace(l.mode.Value()),
		MaxIter:    maxIter,
		Provider:   strings.TrimSpace(l.provider.Value()),
		Model:      strings.TrimSpace(l.model.Value()),
		Isolate:    l.isolate.Value(),
	}
	return l.parent, func() tea.Msg { return req }
}

// View renders the form. Width is supplied by the host; when ≤ 0 the
// form falls back to a sensible default. When height > 0 the form panel
// is padded to exactly that many rows so the chrome reaches the bottom
// of the terminal (the host renders the launch form full-screen as a
// modal). The form wraps its body in a rounded panel with a brand-
// colored title so it feels distinct from the surrounding chrome.
func (l Launch) View(width, height int) string {
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
	// Render the agent dropdown directly below row1 when the agent
	// field has focus. Reserves up to maxRows of vertical space so the
	// rest of the form doesn't jump around while typing.
	var agentDropdown string
	if l.onAgentField() {
		agentDropdown = l.agent.DropdownView()
	}
	var workingDirDropdown string
	if l.focus == fldWorkingDir {
		workingDirDropdown = l.workingDir.DropdownView()
	}
	row2 := fmt.Sprintf("%s  %s    %s  %s    %s  %s",
		labelW("budget", 12), boxed(l.budget.View(), l.focus == fldBudget),
		labelW("mode", 6), l.mode.View(9),
		labelW("iter", 6), boxed(l.iter.View(), l.focus == fldIters),
	)
	row3 := style.Faint.Render("advanced  ") + fmt.Sprintf("%s  %s    %s  %s    %s  %s",
		labelW("provider", 9), boxed(l.provider.View(), l.focus == fldProvider),
		labelW("model", 6), boxed(l.model.View(), l.focus == fldModel),
		labelW("isolate", 8), l.isolate.View(10),
	)
	var providerDropdown string
	if l.focus == fldProvider {
		providerDropdown = l.provider.DropdownView()
	}
	var modelDropdown string
	if l.focus == fldModel {
		modelDropdown = l.model.DropdownView()
	}
	promptRow := style.Header.Render("prompt") + "\n" + l.prompt.View()

	buttonLaunch := button("[ Launch ]", l.focus == fldLaunch, style.Success)
	buttonCancel := button("[ Cancel ]", l.focus == fldCancel, style.Secondary)
	buttons := buttonLaunch + "    " + buttonCancel

	rows := []string{row1}
	if agentDropdown != "" {
		rows = append(rows, agentDropdown)
	}
	if workingDirDropdown != "" {
		rows = append(rows, workingDirDropdown)
	}
	rows = append(rows,
		"",
		row2,
		"",
		row3,
	)
	if providerDropdown != "" {
		rows = append(rows, providerDropdown)
	}
	if modelDropdown != "" {
		rows = append(rows, modelDropdown)
	}
	rows = append(rows,
		"",
		l.contextualHint(),
		"",
		promptRow,
		"",
		buttons,
	)
	if l.err != "" {
		rows = append(rows, "", style.Error.Bold(true).Render("  ! "+l.err))
	}
	help := style.Faint.Render("tab/shift-tab move · ↑/↓ pick · ←/→ cycle · enter select · ctrl+s submit · esc cancel")
	body := strings.Join(rows, "\n")
	if height > 0 {
		// Pad between the form content and the help footer so the
		// footer pins to the bottom of the panel chrome. Count actual
		// rendered rows (the prompt textarea is multi-line).
		bodyRows := strings.Count(body, "\n") + 1
		pad := height - 2 - bodyRows - 1 // borders + content + help
		if pad < 1 {
			pad = 1
		}
		body += strings.Repeat("\n", pad) + "\n" + help
		return style.PanelFixed("NEW RUN", body, width, height)
	}
	body += "\n\n" + help
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
// triggered by /run) and the resize message has already fired once.
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
