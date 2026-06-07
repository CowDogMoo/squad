package app

import (
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cowdogmoo/squad/ui/pane"
	"github.com/cowdogmoo/squad/ui/presets"
	"github.com/cowdogmoo/squad/ui/sidebar"
)

func makeApp() App {
	return New([]sidebar.Run{
		{ID: "a", Agent: "alpha", State: sidebar.StateWorking, Alive: true, Elapsed: time.Minute},
		{ID: "b", Agent: "beta", State: sidebar.StateCompleted, Alive: false, Elapsed: 2 * time.Minute},
		{ID: "c", Agent: "gamma", State: sidebar.StateFailed, Alive: false, Elapsed: 3 * time.Minute},
	})
}

// asApp narrows a tea.Model back to App or fails the test. The actual
// assertion lives in AsApp (app.go) because the per-file go-critic
// runner can't resolve cross-file types in test sources.
func asApp(t *testing.T, m tea.Model) App {
	t.Helper()
	a, ok := AsApp(m)
	if !ok {
		t.Fatalf("expected App, got %T", m)
	}
	return a
}

func TestNewSelectsFirstRun(t *testing.T) {
	if got := makeApp().Selected(); got != "a" {
		t.Errorf("selected: got %q, want %q", got, "a")
	}
}

func TestNewWithNoRunsHasEmptySelection(t *testing.T) {
	if got := New(nil).Selected(); got != "" {
		t.Errorf("expected empty selection with no runs, got %q", got)
	}
}

func TestCycleSelectionForward(t *testing.T) {
	a := makeApp()
	a.cycleSelection(+1)
	if a.Selected() != "b" {
		t.Errorf("after +1: got %q, want %q", a.Selected(), "b")
	}
	a.cycleSelection(+1)
	a.cycleSelection(+1) // wraps
	if a.Selected() != "a" {
		t.Errorf("after wrap: got %q, want %q", a.Selected(), "a")
	}
}

func TestCycleSelectionBackward(t *testing.T) {
	a := makeApp()
	a.cycleSelection(-1)
	if a.Selected() != "c" {
		t.Errorf("after -1: got %q, want %q", a.Selected(), "c")
	}
}

func TestQuitOnCtrlC(t *testing.T) {
	a := makeApp()
	next, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !asApp(t, next).Quitting() {
		t.Error("ctrl+c should set quitting=true")
	}
	if cmd == nil {
		t.Error("ctrl+c should emit tea.Quit cmd")
	}
}

func TestWindowSizeRoutes(t *testing.T) {
	a := makeApp()
	next, _ := a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	w, h := asApp(t, next).Size()
	if w != 100 || h != 30 {
		t.Errorf("dimensions: got %dx%d, want 100x30", w, h)
	}
}

func TestCtrlNAdvancesSelection(t *testing.T) {
	// Tab now cycles focus regions, not sidebar selection. ctrl+n is the
	// "always advance sidebar" shortcut that works from any region (so
	// users can flip through runs without leaving the composer).
	a := makeApp()
	next, _ := a.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if got := asApp(t, next).Selected(); got != "b" {
		t.Errorf("ctrl+n: got %q, want %q", got, "b")
	}
}

func TestTabCyclesRegions(t *testing.T) {
	a := makeApp()
	if got := a.focusedRegion; got != regionComposer {
		t.Fatalf("default region: got %v, want regionComposer", got)
	}
	next, _ := a.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := asApp(t, next).focusedRegion; got != regionSidebar {
		t.Errorf("after tab: got %v, want regionSidebar", got)
	}
	// In sidebar region, ↓ cycles runs.
	after, _ := next.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := asApp(t, after).Selected(); got != "b" {
		t.Errorf("sidebar+↓: got %q, want %q", got, "b")
	}
}

func TestSelectedRunReflectsState(t *testing.T) {
	a := makeApp()
	r, ok := a.selectedSidebarRun()
	if !ok {
		t.Fatal("selectedSidebarRun should find the first run")
	}
	if r.Agent != "alpha" {
		t.Errorf("selected agent: got %q, want %q", r.Agent, "alpha")
	}
}

func TestHandleSubmitBareRunOpensLaunchForm(t *testing.T) {
	a := makeApp()
	// Before: pane is a Composer (the default).
	if _, ok := pane.AsLaunchView(a.pane); ok {
		t.Fatal("precondition failed: pane should not be a Launch form yet")
	}
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "run"})
	if _, ok := pane.AsLaunchView(a.pane); !ok {
		t.Errorf("bare /run should open the launch form, got %T", a.pane)
	}
}

func TestHandleSubmitQuit(t *testing.T) {
	a := makeApp()
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "quit"})
	if !a.Quitting() {
		t.Error("/quit should set quitting=true")
	}
}

func TestHandleSubmitUnknownCommandSetsToast(t *testing.T) {
	a := makeApp()
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "bogus arg"})
	if a.currentToast() == "" {
		t.Error("unknown command should set a toast")
	}
}

func TestViewRendersLaunchFormAfterOpen(t *testing.T) {
	a := makeApp()
	// Size the app so the form has room to lay out.
	next, _ := a.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	sized, ok := AsApp(next)
	if !ok {
		t.Fatal("expected App after resize")
	}
	sized.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "run"})
	out := sized.View()
	if !contains(out, "NEW RUN") {
		t.Errorf("after /run, View() should contain NEW RUN heading; got:\n%s", out)
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

func TestTruncate(t *testing.T) {
	cases := []struct {
		in, want string
		width    int
	}{
		{"short", "short", 20},
		{"abcdef", "abcd…", 5},
		{"x", "x", 1}, // fits exactly — no ellipsis needed
		{"xy", "…", 1},
		{"abc", "", 0},
	}
	for _, tc := range cases {
		if got := truncate(tc.in, tc.width); got != tc.want {
			t.Errorf("truncate(%q,%d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}

func TestRenderProgressBar(t *testing.T) {
	// Render with ANSI stripped via the existing helper from sidebar tests
	// — but we need our own minimal stripper here.
	cases := []struct {
		spent, budget float64
		width         int
		wantFilled    int
	}{
		{0, 5, 10, 0},
		{2.5, 5, 10, 5},
		{5, 5, 10, 10},
		{10, 5, 10, 10}, // capped
		{-1, 5, 10, 0},  // clamped at 0
	}
	for _, tc := range cases {
		out := renderProgressBar(tc.spent, tc.budget, tc.width)
		// Count filled segments (▰).
		filled := 0
		for _, r := range out {
			if r == '▰' {
				filled++
			}
		}
		if filled != tc.wantFilled {
			t.Errorf("renderProgressBar(%v,%v,%v): filled=%d, want %d (out=%q)",
				tc.spent, tc.budget, tc.width, filled, tc.wantFilled, out)
		}
	}
}

func TestBudgetForExternalRunIsZero(t *testing.T) {
	a := makeApp()
	if got := a.budgetFor("nonexistent-session"); got != 0 {
		t.Errorf("budgetFor unknown should be 0, got %v", got)
	}
}

func TestBudgetForPairedLaunch(t *testing.T) {
	a := makeApp()
	a.launchPairs["session-x"] = "L0001"
	a.launchBudgets["L0001"] = 7.5
	if got := a.budgetFor("session-x"); got != 7.5 {
		t.Errorf("budgetFor paired: got %v, want 7.5", got)
	}
}

func TestHelpToastsCommandList(t *testing.T) {
	a := makeApp()
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "help"})
	toast := a.currentToast()
	if toast == "" || !contains(toast, "/run") {
		t.Errorf("help toast should list commands, got %q", toast)
	}
}

func TestWrapPrefix(t *testing.T) {
	cases := []struct {
		in, prefix string
		width      int
		want       string
	}{
		{"short", "  ", 20, "short"},
		{"abc def ghi", "  ", 6, "abc\n  def\n  ghi"},
	}
	for _, tc := range cases {
		got := wrapPrefix(tc.in, tc.prefix, tc.width)
		if got != tc.want {
			t.Errorf("wrapPrefix(%q,%q,%d) = %q, want %q", tc.in, tc.prefix, tc.width, got, tc.want)
		}
	}
}

func TestPresetSaveRequiresLastLaunch(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "preset save mypreset"})
	if a.currentToast() == "" {
		t.Error("save without prior launch should toast")
	}
	if _, ok := store.Get("mypreset"); ok {
		t.Error("nothing should have been saved")
	}
}

func TestPresetSaveAndLoadRoundTrip(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	// Simulate a prior launch having occurred.
	a.lastLaunch = &pane.LaunchRequest{
		Agent:      "go-review",
		WorkingDir: "/tmp",
		MaxCost:    4.5,
		Mode:       "edit",
		MaxIter:    50,
		Prompt:     "fix the bug",
	}
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "preset save dev"})
	p, ok := store.Get("dev")
	if !ok {
		t.Fatal("preset 'dev' should exist after save")
	}
	if p.Agent != "go-review" || p.Prompt != "fix the bug" {
		t.Errorf("preset persisted as %+v", p)
	}

	// Load should open the launch form.
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "preset load dev"})
	if _, ok := pane.AsLaunchView(a.pane); !ok {
		t.Errorf("after /preset load the pane should be a Launch form, got %T", a.pane)
	}
}

func TestPresetListEmpty(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "preset list"})
	if a.currentToast() == "" {
		t.Error("preset list should toast even when empty")
	}
}

func TestPresetDeleteUnknown(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "preset delete nope"})
	if a.currentToast() == "" {
		t.Error("delete unknown should toast")
	}
}

func TestPresetWithoutStoreToasts(t *testing.T) {
	a := makeApp() // no WithPresets
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "preset save x"})
	if a.currentToast() == "" {
		t.Error("preset commands without a store should toast 'disabled'")
	}
}

func TestCancelFocusedNoSelection(t *testing.T) {
	a := New(nil)
	a.cancelFocused()
	if a.currentToast() == "" {
		t.Error("cancel with no selection should toast")
	}
}

func TestCancelFocusedUnpairedSession(t *testing.T) {
	a := makeApp()
	// makeApp's runs aren't paired with any launch — should toast.
	a.cancelFocused()
	if a.currentToast() == "" {
		t.Error("cancel of unpaired session should toast about external runs")
	}
}

func TestCancelFocusedUnknownLaunchID(t *testing.T) {
	a := makeApp()
	a.launchPairs[a.Selected()] = "L9999" // pair the selected to a nonexistent launch
	a.cancelFocused()
	if a.currentToast() == "" {
		t.Error("cancel of unknown launch should toast an error")
	}
}

func TestToastExpires(t *testing.T) {
	a := makeApp()
	a.setToast("hello")
	if a.currentToast() == "" {
		t.Fatal("toast should be visible immediately after set")
	}
	a.toastUntil = time.Now().Add(-time.Second)
	if a.currentToast() != "" {
		t.Error("toast should be hidden after toastUntil passes")
	}
}

func TestIndicatorForAllStates(t *testing.T) {
	cases := []struct {
		state sidebar.State
		label string
	}{
		{sidebar.StateWorking, "Working"},
		{sidebar.StateConnecting, "Connecting"},
		{sidebar.StateCompleted, "Completed"},
		{sidebar.StateFailed, "Failed"},
		{sidebar.StateNeedsInput, "Needs input"},
		{sidebar.StateBudget, "Budget exceeded"},
	}
	for _, tc := range cases {
		_, label := indicatorFor(sidebar.Run{State: tc.state})
		if label != tc.label {
			t.Errorf("state %v: got %q, want %q", tc.state, label, tc.label)
		}
	}
}
