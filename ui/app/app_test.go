package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

func TestTabAdvancesSelection(t *testing.T) {
	a := makeApp()
	next, _ := a.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := asApp(t, next).Selected(); got != "b" {
		t.Errorf("tab: got %q, want %q", got, "b")
	}
}

func TestSelectedRunReflectsState(t *testing.T) {
	a := makeApp()
	rs, ok := a.selectedRun()
	if !ok {
		t.Fatal("selectedRun should find the first run")
	}
	if rs.run.Agent != "alpha" {
		t.Errorf("selected agent: got %q, want %q", rs.run.Agent, "alpha")
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
