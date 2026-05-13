package pane

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestComposerInit(t *testing.T) {
	c := NewComposer()
	if c.Init() == nil {
		t.Error("Init should return a non-nil cmd (textarea.Blink)")
	}
}

func TestComposerSetValue(t *testing.T) {
	c := NewComposer()
	c.SetValue("hello")
	if c.Value() != "hello" {
		t.Errorf("after SetValue: got %q, want hello", c.Value())
	}
}

func TestComposerView(t *testing.T) {
	c := NewComposer()
	out := c.View(80, 0)
	if out == "" {
		t.Error("View should produce output")
	}
}

func TestComposerHeight(t *testing.T) {
	c := NewComposer()
	if c.Height() < 1 {
		t.Errorf("Height should be >= 1, got %d", c.Height())
	}
}

func TestComposerPushHistoryDedupesAndCaps(t *testing.T) {
	c := NewComposer()
	// Dedupe back-to-back repeats.
	c.pushHistory("same")
	c.pushHistory("same")
	if len(c.history) != 1 {
		t.Errorf("dedupe: got %d entries, want 1", len(c.history))
	}
	// Cap.
	for i := 0; i < historyMax+10; i++ {
		c.pushHistory(string(rune('a' + (i % 26))))
	}
	if len(c.history) > historyMax {
		t.Errorf("cap: got %d entries, want ≤ %d", len(c.history), historyMax)
	}
}

func TestComposerHistoryAtOldestStaysPut(t *testing.T) {
	up := tea.KeyMsg{Type: tea.KeyUp, Alt: true}
	v := View(NewComposer())
	v = typeString(t, v, "only")
	v, _ = v.Update(enterKey())
	// First ↑ pulls "only" from history.
	v, _ = v.Update(up)
	// Second ↑ should stay put (already at the oldest entry).
	v, _ = v.Update(up)
	if got := composerValue(t, v); got != "only" {
		t.Errorf("at oldest, ↑ should be no-op, got %q", got)
	}
}

func TestComposerHistoryNextWithoutNavIsNoOp(t *testing.T) {
	c := NewComposer()
	down := tea.KeyMsg{Type: tea.KeyDown, Alt: true}
	v, _ := c.Update(down)
	if v == nil {
		t.Fatal("history next on fresh composer should not close the pane")
	}
}

func TestLaunchInit(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{})
	if form.Init() == nil {
		t.Error("Init should return a non-nil cmd")
	}
}

func TestLaunchTitleEmpty(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{})
	if form.Title() != "" {
		t.Errorf("Title() = %q, want empty", form.Title())
	}
}

func TestLaunchSetSize(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{})
	form.SetSize(120, 30)
	// Just ensure no panic / state is reasonable; the textarea width
	// changed but we can't introspect it from outside the package.
}

func TestLaunchSetPromptValue(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{})
	form.SetPromptValue("seed prompt")
	if v := strings.TrimSpace(form.prompt.Value()); v != "seed prompt" {
		t.Errorf("prompt value: got %q, want seed prompt", v)
	}
}

func TestLaunchWindowSizePropagates(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{})
	v, _ := form.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if v == nil {
		t.Fatal("WindowSizeMsg should not close the form")
	}
}

func TestLaunchValidatesBudgetInvalid(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{Agent: "x", MaxIter: 10})
	// Set a non-numeric budget by mutating the textinput directly.
	form.budget.SetValue("not-a-number")
	form.prompt.SetValue("do thing")
	v, cmd := form.submit()
	if cmd != nil && cmd() != nil {
		if _, ok := AsLaunchRequest(cmd()); ok {
			t.Error("invalid budget should not produce a LaunchRequest")
		}
	}
	if _, ok := AsLaunchView(v); !ok {
		t.Error("form should stay open after validation failure")
	}
	l, _ := AsLaunchView(v)
	if l.err == "" {
		t.Error("err should be populated")
	}
	if l.focus != fldBudget {
		t.Errorf("focus should land on budget (%d), got %d", fldBudget, l.focus)
	}
}

func TestLaunchValidatesIterInvalid(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{Agent: "x"})
	form.iter.SetValue("0")
	form.prompt.SetValue("do thing")
	v, _ := form.submit()
	l, ok := AsLaunchView(v)
	if !ok {
		t.Fatal("form should stay open on iter validation failure")
	}
	if l.focus != fldIters {
		t.Errorf("focus should land on iter (%d), got %d", fldIters, l.focus)
	}
}

func TestLaunchValidatesPromptRequired(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{Agent: "x"})
	// No prompt set; budget/iter defaults are valid.
	v, _ := form.submit()
	l, ok := AsLaunchView(v)
	if !ok {
		t.Fatal("form should stay open on prompt validation failure")
	}
	if l.focus != fldPrompt {
		t.Errorf("focus should land on prompt (%d), got %d", fldPrompt, l.focus)
	}
}

func TestLaunchValidatesNegativeBudget(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{Agent: "x"})
	form.budget.SetValue("-1")
	form.prompt.SetValue("do thing")
	v, _ := form.submit()
	l, _ := AsLaunchView(v)
	if l.focus != fldBudget {
		t.Errorf("negative budget should focus budget field, got %d", l.focus)
	}
}

func TestLaunchSubmitErrorRenderedInView(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{})
	// Submit with empty agent — triggers validation, sets err.
	v, _ := form.submit()
	l, _ := AsLaunchView(v)
	if l.err == "" {
		t.Fatal("err should be set after validation failure")
	}
	out := l.View(100, 0)
	if !strings.Contains(out, l.err) {
		t.Errorf("View should render error message %q, got: %s", l.err, out)
	}
}

func TestDigitsOnlyAcceptKey(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.Msg
		want bool
	}{
		{"digit", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}}, true},
		{"letter", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, false},
		{"non-rune key (backspace)", tea.KeyMsg{Type: tea.KeyBackspace}, true},
		{"non-key msg", tea.WindowSizeMsg{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := digitsOnlyAcceptKey(tc.msg); got != tc.want {
				t.Errorf("digitsOnlyAcceptKey: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBudgetAcceptKey(t *testing.T) {
	cases := []struct {
		name    string
		msg     tea.Msg
		current string
		want    bool
	}{
		{"digit", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}}, "", true},
		{"first dot", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}}, "1", true},
		{"second dot rejected", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}}, "1.5", false},
		{"letter rejected", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, "", false},
		{"backspace passes", tea.KeyMsg{Type: tea.KeyBackspace}, "1.5", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := budgetAcceptKey(tc.msg, tc.current); got != tc.want {
				t.Errorf("budgetAcceptKey(%q): got %v, want %v", tc.current, got, tc.want)
			}
		})
	}
}

func TestContextualHintCoversAllFields(t *testing.T) {
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{Agent: "x"})
	for i := 0; i < fldCount; i++ {
		form.focus = i
		hint := form.contextualHint()
		if hint == "" {
			t.Errorf("focus=%d: hint should never be empty", i)
		}
	}
}

func TestTypeaheadUpDownNavigatesDropdown(t *testing.T) {
	ta := newTypeahead("", "", 20, []string{"a", "b", "c"})
	ta.Focus()
	ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyDown})
	if ta.cursor != 1 {
		t.Errorf("↓ should move cursor to 1, got %d", ta.cursor)
	}
	ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyDown})
	ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyDown})
	// Should clamp at end (3 options, max cursor = 2).
	if ta.cursor != 2 {
		t.Errorf("cursor should clamp at len-1=2, got %d", ta.cursor)
	}
	ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyUp})
	if ta.cursor != 1 {
		t.Errorf("↑ should move cursor back to 1, got %d", ta.cursor)
	}
}

func TestTypeaheadEnterCommitsHighlight(t *testing.T) {
	ta := newTypeahead("", "", 20, []string{"foo", "bar"})
	ta.Focus()
	ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyDown}) // cursor = 1
	ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ta.Value() != "bar" {
		t.Errorf("Enter should commit bar, got %q", ta.Value())
	}
}

func TestTypeaheadSetValue(t *testing.T) {
	ta := newTypeahead("", "", 20, []string{"x"})
	ta.SetValue("hello")
	if ta.Value() != "hello" {
		t.Errorf("SetValue: got %q, want hello", ta.Value())
	}
}

func TestTypeaheadDropdownView(t *testing.T) {
	ta := newTypeahead("", "", 20, []string{"alpha", "beta", "gamma"})
	if got := ta.DropdownView(); got != "" {
		t.Errorf("blurred dropdown should be empty, got %q", got)
	}
	ta.Focus()
	out := ta.DropdownView()
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("DropdownView should list options, got: %s", out)
	}
}

func TestTypeaheadDropdownNoMatches(t *testing.T) {
	ta := newTypeahead("", "zzz", 20, []string{"alpha", "beta"})
	ta.Focus()
	out := ta.DropdownView()
	if !strings.Contains(out, "no matches") {
		t.Errorf("DropdownView with no matches should say so, got: %s", out)
	}
}

func TestTypeaheadDropdownScrollsToCursor(t *testing.T) {
	ta := newTypeahead("", "", 20, []string{"a", "b", "c", "d", "e", "f", "g", "h"})
	ta.maxRows = 3
	ta.Focus()
	// Move cursor below visible window.
	for i := 0; i < 5; i++ {
		ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	out := ta.DropdownView()
	// The cursor row (index 5 = "f") should appear in view.
	if !strings.Contains(out, "f") {
		t.Errorf("DropdownView should scroll cursor into view, got: %s", out)
	}
}

func TestTypeaheadDropdownHeight(t *testing.T) {
	ta := newTypeahead("", "", 20, []string{"a", "b", "c"})
	if h := ta.DropdownHeight(); h != 0 {
		t.Errorf("blurred DropdownHeight should be 0, got %d", h)
	}
	ta.Focus()
	if h := ta.DropdownHeight(); h != 3 {
		t.Errorf("focused DropdownHeight with 3 opts should be 3, got %d", h)
	}
	ta.SetValue("zzz")
	if h := ta.DropdownHeight(); h != 1 {
		t.Errorf("focused DropdownHeight with no matches should be 1, got %d", h)
	}
}

func TestTypeaheadUpdateRunInputResetsCursor(t *testing.T) {
	ta := newTypeahead("", "", 20, []string{"alpha", "beta"})
	ta.Focus()
	ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyDown}) // cursor = 1
	ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if ta.cursor != 0 {
		t.Errorf("typing should reset cursor to 0, got %d", ta.cursor)
	}
}

func TestSelectFieldCyclesWithArrowsAndHL(t *testing.T) {
	sf := newSelectField([]string{"a", "b", "c"}, "a", "")
	sf.Focus()
	sf, _ = sf.Update(tea.KeyMsg{Type: tea.KeyRight})
	if sf.Value() != "b" {
		t.Errorf("right → b, got %q", sf.Value())
	}
	sf, _ = sf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if sf.Value() != "c" {
		t.Errorf("l → c, got %q", sf.Value())
	}
	sf, _ = sf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	// Should clamp at end.
	if sf.Value() != "c" {
		t.Errorf("clamp at end: got %q, want c", sf.Value())
	}
	sf, _ = sf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if sf.Value() != "b" {
		t.Errorf("h → b, got %q", sf.Value())
	}
	sf, _ = sf.Update(tea.KeyMsg{Type: tea.KeyLeft})
	sf, _ = sf.Update(tea.KeyMsg{Type: tea.KeyLeft})
	// Should clamp at start.
	if sf.Value() != "a" {
		t.Errorf("clamp at start: got %q, want a", sf.Value())
	}
}

func TestSelectFieldSpaceCycles(t *testing.T) {
	sf := newSelectField([]string{"a", "b"}, "a", "")
	sf, _ = sf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if sf.Value() != "b" {
		t.Errorf("space should cycle forward, got %q", sf.Value())
	}
}

func TestSelectFieldIgnoresNonKey(t *testing.T) {
	sf := newSelectField([]string{"a", "b"}, "a", "")
	sf, _ = sf.Update(tea.WindowSizeMsg{})
	if sf.Value() != "a" {
		t.Errorf("non-key msg should leave cursor alone, got %q", sf.Value())
	}
}

func TestSelectFieldFocusBlur(t *testing.T) {
	sf := newSelectField([]string{"a", "b"}, "a", "")
	sf.Focus()
	if !sf.focused {
		t.Error("Focus should set focused=true")
	}
	sf.Blur()
	if sf.focused {
		t.Error("Blur should set focused=false")
	}
	// View runs in both states without panic.
	_ = sf.View(10)
	sf.Focus()
	_ = sf.View(10)
}

func TestSelectFieldViewEmptyLabel(t *testing.T) {
	sf := newSelectField([]string{"", "x", "y"}, "", "(default)")
	out := sf.View(20)
	if !strings.Contains(out, "(default)") {
		t.Errorf("empty value should render emptyLabel, got: %s", out)
	}
}

func TestSelectFieldValueOutOfRange(t *testing.T) {
	sf := selectField{options: []string{"a"}, cursor: 99}
	if got := sf.Value(); got != "" {
		t.Errorf("out-of-range cursor should yield empty, got %q", got)
	}
}

func TestCompletionContextEmptyInput(t *testing.T) {
	realDir, prefix := completionContext("")
	if realDir == "" {
		t.Error("empty input should fall back to cwd")
	}
	if prefix != "" {
		t.Errorf("empty input prefix should be empty, got %q", prefix)
	}
}

// focusField cycles a Launch form's focus to the requested field.
func focusField(t *testing.T, v View, target int) View {
	t.Helper()
	for i := 0; i < fldCount*2; i++ {
		l, ok := AsLaunchView(v)
		if !ok {
			t.Fatal("focusField: not a Launch view")
		}
		if l.focus == target {
			return v
		}
		v, _ = v.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	t.Fatalf("focusField: could not reach %d", target)
	return v
}

func TestLaunchEnterOnCancelReturnsParent(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "x"})
	v := focusField(t, View(form), fldCancel)
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if v.Title() != "parent" {
		t.Errorf("Enter on Cancel should return parent, got Title=%q", v.Title())
	}
}

func TestLaunchEnterOnLaunchSubmits(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "go-review"})
	form.prompt.SetValue("do the thing")
	v := focusField(t, View(form), fldLaunch)
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	req := asLaunchRequest(t, cmd)
	if req.Agent != "go-review" || req.Prompt != "do the thing" {
		t.Errorf("Enter on Launch should submit, got %+v", req)
	}
	if next.Title() != "parent" {
		t.Errorf("after submit: got Title=%q, want parent", next.Title())
	}
}

func TestLaunchEnterOnAgentAdvancesFocus(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agents: []string{"alpha", "beta"}})
	// Default focus is fldAgent.
	v, _ := form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	l, _ := AsLaunchView(v)
	if l.focus != fldWorkingDir {
		t.Errorf("Enter on agent should advance to workingDir (%d), got %d", fldWorkingDir, l.focus)
	}
}

func TestLaunchEnterOnWorkingDirNoChangeAdvancesFocus(t *testing.T) {
	parent := stubView{name: "parent"}
	// No options → Enter can't commit a new value → falls through to advance.
	form := NewLaunch(parent, LaunchDefaults{Agent: "x", WorkingDir: "/nowhere"})
	form.workingDir.SetOptions(nil)
	v := focusField(t, View(form), fldWorkingDir)
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	l, _ := AsLaunchView(v)
	if l.focus != fldBudget {
		t.Errorf("Enter on workingDir with no commit should advance to budget (%d), got %d", fldBudget, l.focus)
	}
}

func TestLaunchEnterOnWorkingDirCommitsAndStays(t *testing.T) {
	root := t.TempDir()
	sep := string(filepath.Separator)
	if err := os.Mkdir(filepath.Join(root, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "x", WorkingDir: root + sep})
	form.workingDir.SetOptions(workingDirSuggestions(root + sep))
	v := focusField(t, View(form), fldWorkingDir)
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	l, _ := AsLaunchView(v)
	// Commit took → focus stays on workingDir.
	if l.focus != fldWorkingDir {
		t.Errorf("Enter that commits should stay on workingDir, got focus=%d", l.focus)
	}
	if !strings.HasSuffix(l.workingDir.Value(), "alpha"+sep) {
		t.Errorf("Enter should commit highlighted dir, got %q", l.workingDir.Value())
	}
}

func TestLaunchEnterOnProviderRefreshesModelOptions(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "x"})
	v := focusField(t, View(form), fldProvider)
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	l, _ := AsLaunchView(v)
	if l.focus != fldModel {
		t.Errorf("Enter on provider should advance to model (%d), got %d", fldModel, l.focus)
	}
}

func TestLaunchEnterOnModelAdvancesFocus(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "x"})
	v := focusField(t, View(form), fldModel)
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	l, _ := AsLaunchView(v)
	if l.focus != fldIsolate {
		t.Errorf("Enter on model should advance to isolate (%d), got %d", fldIsolate, l.focus)
	}
}

func TestLaunchEnterOnPromptFallsThrough(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "x"})
	v := focusField(t, View(form), fldPrompt)
	// Enter on prompt is not "handled" by the form — it forwards to the
	// textarea, which inserts no newline (the textarea reserves Enter for
	// composer submit). The form stays open and focus stays put.
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	l, ok := AsLaunchView(v)
	if !ok {
		t.Fatal("Enter on prompt should keep form open")
	}
	if l.focus != fldPrompt {
		t.Errorf("focus should remain on prompt, got %d", l.focus)
	}
}

func TestLaunchEnterOnIsolateAdvancesFocus(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "x"})
	v := focusField(t, View(form), fldIsolate)
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	l, _ := AsLaunchView(v)
	// fldIsolate uses the default branch in handleEnter.
	if l.focus != fldPrompt {
		t.Errorf("Enter on isolate should advance to prompt (%d), got %d", fldPrompt, l.focus)
	}
}

func TestLaunchAcceptKeyOnIterRejectsLetters(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "x", MaxIter: 10})
	v := focusField(t, View(form), fldIters)
	before, _ := AsLaunchView(v)
	prev := before.iter.Value()
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	l, _ := AsLaunchView(v)
	if l.iter.Value() != prev {
		t.Errorf("letter should be rejected on iter field, got %q", l.iter.Value())
	}
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	l, _ = AsLaunchView(v)
	if !strings.Contains(l.iter.Value(), "5") {
		t.Errorf("digit should pass through, got %q", l.iter.Value())
	}
}

func TestLaunchAcceptKeyOnBudgetRejectsSecondDot(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "x", MaxCost: 1.5})
	v := focusField(t, View(form), fldBudget)
	before, _ := AsLaunchView(v)
	prev := before.budget.Value()
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	l, _ := AsLaunchView(v)
	if l.budget.Value() != prev {
		t.Errorf("second dot should be rejected, got %q", l.budget.Value())
	}
}

func TestButtonHelperBranches(t *testing.T) {
	// button() takes a Render-able style; reuse package style.Header which
	// both branches share. The focused branch prepends a "›" marker, the
	// unfocused branch indents with two spaces — assert both contain the
	// label and only the focused output carries the marker.
	focused := button("OK", true, headerStyle{})
	unfocused := button("OK", false, headerStyle{})
	if !strings.Contains(focused, "OK") || !strings.Contains(unfocused, "OK") {
		t.Errorf("button output missing label; focused=%q unfocused=%q", focused, unfocused)
	}
	if !strings.Contains(focused, "›") {
		t.Errorf("focused button missing marker: %q", focused)
	}
	if strings.Contains(unfocused, "›") {
		t.Errorf("unfocused button should not have marker: %q", unfocused)
	}
}

func TestFieldOwnsHorizontalKeys(t *testing.T) {
	parent := stubView{name: "parent"}
	cases := []struct {
		focus int
		want  bool
	}{
		{fldAgent, true},
		{fldPrompt, true},
		{fldLaunch, false},
		{fldCancel, false},
	}
	for _, c := range cases {
		l := NewLaunch(parent, LaunchDefaults{})
		l.focus = c.focus
		if got := l.fieldOwnsHorizontalKeys(); got != c.want {
			t.Errorf("focus=%v: got %v, want %v", c.focus, got, c.want)
		}
	}
}

func TestLabelWPadsToWidth(t *testing.T) {
	// labelW pads with trailing spaces up to the requested width. When the
	// label is wider than w, no padding is added (pad becomes 0).
	short := labelW("ab", 6)
	if !strings.HasSuffix(short, "    ") {
		t.Errorf("expected 4-space pad after 2-char label, got %q", short)
	}
	long := labelW("verylonglabel", 3)
	if strings.HasSuffix(long, " ") {
		t.Errorf("label wider than w should not pad, got %q", long)
	}
}

// headerStyle is a minimal stand-in for lipgloss styles that satisfies the
// `Render(strs ...string) string` interface button() takes.
type headerStyle struct{}

func (headerStyle) Render(strs ...string) string { return strings.Join(strs, "") }
