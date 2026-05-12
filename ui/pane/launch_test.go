package pane

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// stub View used as the launch form's parent.
type stubView struct{ name string }

func (s stubView) Init() tea.Cmd                  { return nil }
func (s stubView) Update(tea.Msg) (View, tea.Cmd) { return s, nil }
func (s stubView) View(_, _ int) string           { return "stub:" + s.name }
func (s stubView) Title() string                  { return s.name }

func typeAll(t *testing.T, v View, s string) View {
	t.Helper()
	for _, r := range s {
		next, _ := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if next == nil {
			t.Fatalf("nil view while typing %q", s)
		}
		v = next
	}
	return v
}

// asLaunchRequest unwraps a tea.Cmd to a LaunchRequest, failing if the
// shape doesn't match. The actual assertion lives in AsLaunchRequest
// (pane.go).
func asLaunchRequest(t *testing.T, cmd tea.Cmd) LaunchRequest {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a non-nil cmd")
	}
	r, ok := AsLaunchRequest(cmd())
	if !ok {
		t.Fatalf("expected LaunchRequest, got %T", cmd())
	}
	return r
}

func TestLaunchEscReturnsParent(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{})
	next, _ := form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := next.Title(); got != "parent" {
		t.Errorf("esc should return parent (Title=parent), got %q", got)
	}
}

func TestLaunchTabCyclesFocus(t *testing.T) {
	parent := stubView{name: "parent"}
	v := View(NewLaunch(parent, LaunchDefaults{}))
	// 8 fields total — tab 8 times should land back on the first.
	for i := 0; i < fldCount; i++ {
		next, _ := v.Update(tea.KeyMsg{Type: tea.KeyTab})
		v = next
	}
	// Confirm we still have a Launch view (didn't accidentally close).
	if _, ok := AsLaunchView(v); !ok {
		t.Fatalf("expected Launch after %d tabs, got %T", fldCount, v)
	}
}

func TestLaunchSubmitMissingAgent(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{WorkingDir: "/tmp"})
	// Jump focus to Launch button via tab cycling, then press Enter.
	v := View(form)
	for v != nil {
		l, ok := AsLaunchView(v)
		if !ok {
			t.Fatal("unexpected view type")
		}
		if l.focus == fldLaunch {
			break
		}
		v, _ = v.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	v, cmd := v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil && cmd() != nil {
		// A valid LaunchRequest would not happen with empty agent.
		if _, ok := AsLaunchRequest(cmd()); ok {
			t.Error("expected validation to block submit with empty agent")
		}
	}
	// Form stays open.
	if _, ok := AsLaunchView(v); !ok {
		t.Errorf("form should remain open after validation failure, got %T", v)
	}
}

func TestLaunchViewRendersFields(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{
		Agent:      "go-review",
		WorkingDir: "/tmp/foo",
		MaxCost:    3.50,
		Mode:       "edit",
		MaxIter:    25,
	})
	out := form.View(100, 20)
	checks := []string{
		"NEW RUN",
		"agent",
		"go-review",
		"working dir",
		"/tmp/foo",
		"budget",
		"3.50",
		"edit",
		"25",
		"Launch",
		"Cancel",
		"tab/shift-tab",
	}
	for _, c := range checks {
		if !contains(out, c) {
			t.Errorf("form View missing %q", c)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLaunchSubmitEmitsRequest(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{
		WorkingDir: "/tmp",
		MaxCost:    2.5,
		Mode:       "edit",
		MaxIter:    20,
	})
	// Type into agent.
	v := typeAll(t, form, "go-review")
	// Tab to prompt (skip workingDir/budget/mode/iter/provider/model/isolate).
	for i := 0; i < fldPrompt-fldAgent; i++ {
		v, _ = v.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	// Type prompt.
	v = typeAll(t, v, "fix the bug")
	// Submit via Ctrl+S.
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	req := asLaunchRequest(t, cmd)
	if req.Agent != "go-review" || req.Prompt != "fix the bug" {
		t.Errorf("request: got %+v", req)
	}
	if req.MaxCost != 2.5 || req.Mode != "edit" || req.MaxIter != 20 {
		t.Errorf("defaults: got %+v", req)
	}
	// Submission returns the parent view.
	if got := next.Title(); got != "parent" {
		t.Errorf("after submit: got Title=%q, want parent", got)
	}
}

func TestLaunchSubmitIncludesAdvanced(t *testing.T) {
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{
		Agent:    "go-review",
		Provider: "anthropic",
		Model:    "claude-sonnet-4-6",
		Isolate:  "worktree",
		MaxIter:  20,
	})
	v := View(form)
	for {
		l, ok := AsLaunchView(v)
		if !ok {
			t.Fatal("unexpected view")
		}
		if l.focus == fldPrompt {
			break
		}
		v, _ = v.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	v = typeAll(t, v, "do the thing")
	_, cmd := v.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	req := asLaunchRequest(t, cmd)
	if req.Provider != "anthropic" || req.Model != "claude-sonnet-4-6" || req.Isolate != "worktree" {
		t.Errorf("advanced fields lost: got %+v", req)
	}
}

func TestLaunchArrowsNavigateFieldsOnSingleLineInputs(t *testing.T) {
	// On a single-line textinput (working dir is field 1), ↓ should
	// move focus forward to the next field — the form-nav behavior
	// expected on plain text inputs that don't otherwise use ↑/↓.
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "go-review"})
	v := View(form)
	// Move focus to working dir (single-line textinput).
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyTab})
	l, ok := AsLaunchView(v)
	if !ok || l.focus != fldWorkingDir {
		t.Fatalf("expected focus on workingDir, got %d", l.focus)
	}
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyDown})
	l, _ = AsLaunchView(v)
	if l.focus != fldBudget {
		t.Errorf("↓ on workingDir should advance to budget (%d), got %d", fldBudget, l.focus)
	}
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyUp})
	l, _ = AsLaunchView(v)
	if l.focus != fldWorkingDir {
		t.Errorf("↑ on budget should return to workingDir (%d), got %d", fldWorkingDir, l.focus)
	}
}

func TestLaunchArrowsForwardedToTypeaheadDropdown(t *testing.T) {
	// On the agent typeahead, ↓ must reach the dropdown — not jump to
	// the next field. fieldOwnsVerticalKeys gates this so the dropdown
	// remains usable.
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agents: []string{"alpha", "beta"}})
	v := View(form)
	// Agent field is focused by default.
	l, _ := AsLaunchView(v)
	if l.focus != fldAgent {
		t.Fatalf("expected default focus on agent, got %d", l.focus)
	}
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyDown})
	l, _ = AsLaunchView(v)
	if l.focus != fldAgent {
		t.Errorf("↓ on agent should stay on agent (dropdown nav), got focus=%d", l.focus)
	}
}

func TestLaunchModelOptionsFollowProvider(t *testing.T) {
	// The model typeahead's suggestion list must match the selected
	// provider — picking "anthropic" should never show gpt-* models.
	form := NewLaunch(stubView{name: "parent"}, LaunchDefaults{Provider: "anthropic"})
	for _, opt := range form.model.options {
		if !contains(opt, "claude") {
			t.Errorf("anthropic provider showed non-Claude model %q", opt)
		}
	}

	// Typing in the provider field should refresh model options as soon
	// as the typed value matches a known provider.
	form = NewLaunch(stubView{name: "parent"}, LaunchDefaults{})
	v := View(form)
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyTab}) // workingDir
	for {
		l, _ := AsLaunchView(v)
		if l.focus == fldProvider {
			break
		}
		v, _ = v.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	v = typeAll(t, v, "gemini")
	l, _ := AsLaunchView(v)
	if len(l.model.options) == 0 {
		t.Fatal("model options not refreshed after typing provider")
	}
	for _, opt := range l.model.options {
		if !contains(opt, "gemini") {
			t.Errorf("gemini provider showed non-Gemini model %q", opt)
		}
	}
}

func TestLaunchUnknownIsolateDefaultsToBlank(t *testing.T) {
	// Isolate is now a closed-set selectField. An unrecognized default
	// silently lands on the blank "use manifest default" option rather
	// than blocking submit (the value can no longer be invalid).
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "x", Isolate: "garbage", MaxIter: 10})
	if got := form.isolate.Value(); got != "" {
		t.Errorf("unknown isolate default: got %q, want %q (blank/manifest)", got, "")
	}
}
