package pane

import (
	"os"
	"path/filepath"
	"strings"
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
	// On a single-line input that doesn't own ↑/↓ (budget, iter), ↓
	// should move focus forward to the next field — the form-nav
	// behavior expected on inputs without dropdowns.
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "go-review"})
	v := View(form)
	// Move focus to budget (single-line textinput, no dropdown).
	for {
		l, ok := AsLaunchView(v)
		if !ok {
			t.Fatal("unexpected view")
		}
		if l.focus == fldBudget {
			break
		}
		v, _ = v.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyDown})
	l, _ := AsLaunchView(v)
	if l.focus != fldMode {
		t.Errorf("↓ on budget should advance to mode (%d), got %d", fldMode, l.focus)
	}
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyUp})
	l, _ = AsLaunchView(v)
	if l.focus != fldBudget {
		t.Errorf("↑ on mode should return to budget (%d), got %d", fldBudget, l.focus)
	}
}

// suggestionsFixture builds <tmp>/{alpha, beta, .hidden, file.txt} and
// returns the root. The visible directories are alpha and beta; the
// dotfile and regular file must never appear in suggestions.
func suggestionsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestWorkingDirSuggestionsTrailingSeparator(t *testing.T) {
	root := suggestionsFixture(t)
	sep := string(filepath.Separator)
	got := workingDirSuggestions(root + sep)
	want := []string{root + sep + "alpha" + sep, root + sep + "beta" + sep}
	if len(got) != len(want) {
		t.Fatalf("got %d suggestions, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d]: got %q, want %q", i, got[i], w)
		}
	}
}

func TestWorkingDirSuggestionsFiltersDotfilesAndFiles(t *testing.T) {
	root := suggestionsFixture(t)
	sep := string(filepath.Separator)
	for _, s := range workingDirSuggestions(root + sep) {
		base := filepath.Base(strings.TrimRight(s, sep))
		if base == "file.txt" || strings.HasPrefix(base, ".") {
			t.Errorf("filtered entry appeared: %q", s)
		}
	}
}

func TestWorkingDirSuggestionsPartialSegment(t *testing.T) {
	root := suggestionsFixture(t)
	sep := string(filepath.Separator)
	// Partial segments come back unnarrowed — the typeahead's filtered()
	// is responsible for the prefix match against the buffer.
	got := workingDirSuggestions(root + sep + "a")
	if len(got) != 2 {
		t.Errorf("partial segment: got %d suggestions, want 2: %v", len(got), got)
	}
}

func TestWorkingDirSuggestionsTildePrefix(t *testing.T) {
	sep := string(filepath.Separator)
	if _, err := os.UserHomeDir(); err != nil {
		t.Skip("no home dir")
	}
	for _, s := range workingDirSuggestions("~" + sep) {
		if !strings.HasPrefix(s, "~"+sep) {
			t.Errorf("tilde suggestion lost prefix: %q", s)
		}
	}
}

func TestWorkingDirSuggestionsEnvVarPrefix(t *testing.T) {
	root := suggestionsFixture(t)
	sep := string(filepath.Separator)
	t.Setenv("SQUAD_TEST_ROOT", root)
	cases := []struct {
		input, prefix string
	}{
		{"$SQUAD_TEST_ROOT" + sep, "$SQUAD_TEST_ROOT" + sep},
		{"${SQUAD_TEST_ROOT}" + sep, "${SQUAD_TEST_ROOT}" + sep},
	}
	for _, tc := range cases {
		got := workingDirSuggestions(tc.input)
		if len(got) != 2 {
			t.Errorf("%s: got %d suggestions, want 2: %v", tc.input, len(got), got)
		}
		for _, s := range got {
			if !strings.HasPrefix(s, tc.prefix) {
				t.Errorf("%s: suggestion lost prefix: %q", tc.input, s)
			}
		}
	}
}

func TestLaunchWorkingDirDropdownOwnsVerticalKeys(t *testing.T) {
	// workingDir now exposes a filesystem typeahead — ↓ must stay on the
	// field to navigate the dropdown, not advance focus to budget.
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "go-review"})
	v := View(form)
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyTab})
	l, ok := AsLaunchView(v)
	if !ok || l.focus != fldWorkingDir {
		t.Fatalf("expected focus on workingDir, got %d", l.focus)
	}
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyDown})
	l, _ = AsLaunchView(v)
	if l.focus != fldWorkingDir {
		t.Errorf("↓ on workingDir should stay on field (dropdown nav), got focus=%d", l.focus)
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

func TestLaunchArrowsNavigateOnButtons(t *testing.T) {
	// ←/→ are claimed by text inputs (cursor) and selectFields (cycle),
	// but Launch/Cancel buttons have nothing to do with them. On those,
	// ←/→ should move focus like Tab/Shift+Tab so the user can flip
	// between the two without reaching for Tab.
	parent := stubView{name: "parent"}
	form := NewLaunch(parent, LaunchDefaults{Agent: "x"})
	v := View(form)
	for {
		l, ok := AsLaunchView(v)
		if !ok {
			t.Fatal("unexpected view")
		}
		if l.focus == fldLaunch {
			break
		}
		v, _ = v.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyRight})
	l, _ := AsLaunchView(v)
	if l.focus != fldCancel {
		t.Errorf("→ on Launch should advance to Cancel (%d), got %d", fldCancel, l.focus)
	}
	v, _ = v.Update(tea.KeyMsg{Type: tea.KeyLeft})
	l, _ = AsLaunchView(v)
	if l.focus != fldLaunch {
		t.Errorf("← on Cancel should return to Launch (%d), got %d", fldLaunch, l.focus)
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
