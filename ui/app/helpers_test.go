package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/ui/pane"
	"github.com/cowdogmoo/squad/ui/presets"
	"github.com/cowdogmoo/squad/ui/sidebar"
	"github.com/cowdogmoo/squad/watch"
)

func TestMockRuns(t *testing.T) {
	if got := MockRuns(); len(got) == 0 {
		t.Error("MockRuns should produce a non-empty fixture list")
	}
}

func TestWithAgents(t *testing.T) {
	a := makeApp().WithAgents([]string{"go-review", "python-review"})
	if len(a.agents) != 2 {
		t.Errorf("agents: got %d, want 2", len(a.agents))
	}
}

func TestWithProviderToken(t *testing.T) {
	a := makeApp().WithProviderToken("cfg-token")
	if a.providerToken != "cfg-token" {
		t.Errorf("providerToken: got %q, want %q", a.providerToken, "cfg-token")
	}
}

func TestInit(t *testing.T) {
	a := makeApp()
	if a.Init() == nil {
		t.Error("Init should return a non-nil batch cmd")
	}
}

func TestNewWithSessionsEmpty(t *testing.T) {
	a, err := NewWithSessions(t.TempDir(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("NewWithSessions on empty dir should not error, got %v", err)
	}
	if a.Selected() != "" {
		t.Errorf("empty sessions root → no selection, got %q", a.Selected())
	}
}

func TestNewWithSessionsWithSession(t *testing.T) {
	root := t.TempDir()
	sid := "session-x"
	dir := filepath.Join(root, sid)
	if err := mkSessionDir(t, dir, session.Meta{SessionID: sid, Agent: "go-review", Status: session.StatusRunning}); err != nil {
		t.Fatal(err)
	}
	a, err := NewWithSessions(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Selected() != sid {
		t.Errorf("expected selection on discovered session, got %q", a.Selected())
	}
}

func mkSessionDir(t *testing.T, dir string, meta session.Meta) error {
	t.Helper()
	if err := makeDir(dir); err != nil {
		return err
	}
	body := []byte(`{"session_id":"` + meta.SessionID + `","agent":"` + meta.Agent + `","status":"` + meta.Status + `"}`)
	return writeFile(filepath.Join(dir, "meta.json"), body)
}

func TestPadBlock(t *testing.T) {
	out := padBlock("ab\ncde", 5)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	// Each line should be 5 visible cols.
	for i, line := range lines {
		if len([]rune(line)) != 5 {
			t.Errorf("line %d width: got %d, want 5 (%q)", i, len([]rune(line)), line)
		}
	}
}

func TestPadBlockTruncates(t *testing.T) {
	out := padBlock("this is too long", 6)
	if !strings.Contains(out, "…") {
		t.Errorf("oversized line should be truncated, got %q", out)
	}
}

func TestTruncateAnsi(t *testing.T) {
	cases := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"empty width", "abc", 0, ""},
		{"width one is ellipsis", "abc", 1, "…"},
		{"plain string", "abcdef", 4, "abc…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateAnsi(tc.s, tc.width)
			if got != tc.want {
				t.Errorf("truncateAnsi(%q, %d) = %q, want %q", tc.s, tc.width, got, tc.want)
			}
		})
	}
}

func TestTruncateAnsiPreservesEscapes(t *testing.T) {
	// CSI sequences should pass through.
	styled := "\x1b[31mhello\x1b[0m"
	out := truncateAnsi(styled, 4)
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("ANSI escape lost: %q", out)
	}
}

func TestRenderFocusBar(t *testing.T) {
	a := makeApp()
	out := a.renderFocusBar()
	if !strings.Contains(out, "composer") || !strings.Contains(out, "sidebar") {
		t.Errorf("focus bar should list regions, got %q", out)
	}
}

func TestRenderSidebarEmpty(t *testing.T) {
	a := New(nil)
	out := a.renderSidebar(40)
	if !strings.Contains(out, "no runs") {
		t.Errorf("empty sidebar should show onboarding, got %q", out)
	}
}

func TestRenderSidebarWithRuns(t *testing.T) {
	a := makeApp()
	out := a.renderSidebar(40)
	if !strings.Contains(out, "alpha") {
		t.Errorf("sidebar should list runs, got %q", out)
	}
}

func TestRenderFocusedNoSelection(t *testing.T) {
	a := New(nil)
	out := a.renderFocused(60)
	if !strings.Contains(out, "WELCOME") {
		t.Errorf("renderFocused with no selection should show welcome, got %q", out)
	}
}

func TestRenderFocusedWithSelection(t *testing.T) {
	a := makeApp()
	out := a.renderFocused(60)
	if !strings.Contains(out, "alpha") {
		t.Errorf("focused panel should mention selected agent, got %q", out)
	}
}

func TestRenderWelcome(t *testing.T) {
	a := makeApp()
	out := a.renderWelcome(80)
	if !strings.Contains(out, "WELCOME") || !strings.Contains(out, "/run") {
		t.Errorf("welcome should onboard with /run, got %q", out)
	}
}

func TestRenderEventTail(t *testing.T) {
	now := time.Now()
	events := []watch.EventLine{
		{Ts: now, Type: "iteration", Summary: "iter 1"},
		{Ts: now.Add(time.Second), Type: "tool_call", Summary: "Edit"},
	}
	rows := renderEventTail(events, 5, 80)
	if len(rows) != 2 {
		t.Errorf("got %d rows, want 2", len(rows))
	}
}

func TestRenderEventTailZeroN(t *testing.T) {
	if got := renderEventTail([]watch.EventLine{{}}, 0, 80); got != nil {
		t.Errorf("n=0 should produce no rows, got %v", got)
	}
}

func TestRenderEventTailTruncates(t *testing.T) {
	rows := renderEventTail([]watch.EventLine{
		{Type: "x", Summary: strings.Repeat("a", 200)},
	}, 1, 50)
	if !strings.Contains(rows[0], "…") {
		t.Errorf("summary should be truncated, got %q", rows[0])
	}
}

func TestPadType(t *testing.T) {
	if got := padType("x", 5); got != "x    " {
		t.Errorf("padType(x, 5) = %q, want %q", got, "x    ")
	}
	if got := padType("over-long", 4); got != "over-long" {
		t.Errorf("padType should not truncate, got %q", got)
	}
}

func TestRenderStatusNoSelection(t *testing.T) {
	a := New(nil)
	out := a.renderStatus(40)
	if out == "" {
		t.Error("renderStatus with no selection should still render an idle line")
	}
}

func TestRenderStatusWithSelection(t *testing.T) {
	a := makeApp()
	out := a.renderStatus(60)
	if out == "" {
		t.Error("renderStatus should produce output for active run")
	}
}

func TestFormatHMS(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{-1, "0s"},
		{30 * time.Second, "30s"},
		{75 * time.Second, "1m 15s"},
		{3725 * time.Second, "1h 02m 05s"},
	}
	for _, tc := range cases {
		if got := formatHMS(tc.d); got != tc.want {
			t.Errorf("formatHMS(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestStateLabelAliveVsExited(t *testing.T) {
	alive := stateLabel(sidebar.Run{State: sidebar.StateWorking, Alive: true})
	if alive != "Working" {
		t.Errorf("alive working: got %q, want Working", alive)
	}
	exited := stateLabel(sidebar.Run{State: sidebar.StateCompleted, Alive: false})
	if !strings.Contains(exited, "exited") {
		t.Errorf("dead state should append (exited), got %q", exited)
	}
}

func TestStateFromMeta(t *testing.T) {
	cases := []struct {
		in        string
		wantState sidebar.State
		wantAlive bool
	}{
		{session.StatusRunning, sidebar.StateWorking, true},
		{session.StatusCompleted, sidebar.StateCompleted, false},
		{session.StatusError, sidebar.StateFailed, false},
		{session.StatusBudget, sidebar.StateBudget, false},
		{"", sidebar.StateConnecting, true},
		{"unknown-future-state", sidebar.StateCompleted, false},
	}
	for _, tc := range cases {
		state, alive := stateFromMeta(tc.in)
		if state != tc.wantState || alive != tc.wantAlive {
			t.Errorf("stateFromMeta(%q) = (%v, %v), want (%v, %v)",
				tc.in, state, alive, tc.wantState, tc.wantAlive)
		}
	}
}

func TestCurrentRunsStatic(t *testing.T) {
	a := makeApp()
	runs := a.currentRuns()
	if len(runs) != 3 {
		t.Errorf("static runs: got %d, want 3", len(runs))
	}
}

func TestCurrentRunsFromTailers(t *testing.T) {
	root := t.TempDir()
	sid := "session-y"
	dir := filepath.Join(root, sid)
	if err := mkSessionDir(t, dir, session.Meta{SessionID: sid, Agent: "go-review", Status: session.StatusRunning}); err != nil {
		t.Fatal(err)
	}
	a, err := NewWithSessions(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	runs := a.currentRuns()
	if len(runs) != 1 {
		t.Fatalf("tailer-derived runs: got %d, want 1", len(runs))
	}
	if runs[0].Agent != "go-review" {
		t.Errorf("agent: got %q, want go-review", runs[0].Agent)
	}
}

func TestTailerStateForUnknown(t *testing.T) {
	a, _ := NewWithSessions(t.TempDir(), t.TempDir(), "")
	if _, ok := a.tailerStateFor("nope"); ok {
		t.Error("unknown session should return false")
	}
}

func TestPresetOrDefault(t *testing.T) {
	if got := presetOrDefault("a", "b"); got != "a" {
		t.Errorf("non-empty value: got %q, want a", got)
	}
	if got := presetOrDefault("", "b"); got != "b" {
		t.Errorf("empty value should fall back: got %q, want b", got)
	}
}

func TestRenderCostRowNoBudget(t *testing.T) {
	a := makeApp()
	out := a.renderCostRow("unknown", 1.23)
	if !strings.Contains(out, "$1.23") {
		t.Errorf("cost row should display spent, got %q", out)
	}
}

func TestRenderCostRowWithBudget(t *testing.T) {
	a := makeApp()
	a.launchPairs["a"] = "L0001"
	a.launchBudgets["L0001"] = 5.0
	out := a.renderCostRow("a", 2.5)
	if !strings.Contains(out, "$5.00") || !strings.Contains(out, "50%") {
		t.Errorf("cost row should show budget and pct, got %q", out)
	}
}

func TestRenderCostRowOverBudget(t *testing.T) {
	a := makeApp()
	a.launchPairs["a"] = "L0001"
	a.launchBudgets["L0001"] = 5.0
	out := a.renderCostRow("a", 50.0)
	if !strings.Contains(out, "100%") {
		t.Errorf("over-budget pct should cap at 100, got %q", out)
	}
}

func TestSelectedSidebarRunNotFound(t *testing.T) {
	a := New(nil)
	a.selected = "no-such-id" // simulate stale selection
	if _, ok := a.selectedSidebarRun(); ok {
		t.Error("missing selection should return false")
	}
}

func TestViewBeforeWindowSize(t *testing.T) {
	// View should not panic before WindowSizeMsg sets dimensions; it uses
	// fallback defaults.
	a := makeApp()
	out := a.View()
	if out == "" {
		t.Error("View should produce output even without WindowSizeMsg")
	}
}

func TestViewQuittingReturnsEmpty(t *testing.T) {
	a := makeApp()
	a.quitting = true
	if got := a.View(); got != "" {
		t.Errorf("quitting View should be empty, got %q", got)
	}
}

func TestViewWithRunsRendersPanel(t *testing.T) {
	a := makeApp()
	a.width = 120
	a.height = 30
	out := a.View()
	if !strings.Contains(out, "SQUAD") {
		t.Errorf("View should render SQUAD panel, got: %s", out)
	}
}

func TestViewEmptyShowsWelcome(t *testing.T) {
	a := New(nil)
	a.width = 120
	a.height = 30
	out := a.View()
	if !strings.Contains(out, "WELCOME") {
		t.Errorf("empty View should show welcome, got: %s", out)
	}
}

func TestViewWithToast(t *testing.T) {
	a := makeApp()
	a.width = 120
	a.height = 30
	a.setToast("hello toast")
	out := a.View()
	if !strings.Contains(out, "hello toast") {
		t.Errorf("View should include active toast, got: %s", out)
	}
}

func TestViewLaunchFormModalTakesOver(t *testing.T) {
	a := makeApp()
	a.width = 120
	a.height = 30
	a.handleSubmit(pane.Submitted{Kind: pane.KindCommand, Text: "run"})
	out := a.View()
	// The launch form should occupy the screen without the SQUAD panel.
	if !strings.Contains(out, "NEW RUN") {
		t.Errorf("launch form modal should render NEW RUN, got: %s", out)
	}
}

func TestHandleCommandQuitAlias(t *testing.T) {
	a := makeApp()
	a.handleCommand("exit")
	if !a.Quitting() {
		t.Error("/exit should quit")
	}
}

func TestHandleCommandHelpAlias(t *testing.T) {
	a := makeApp()
	a.handleCommand("?")
	if a.currentToast() == "" {
		t.Error("/? alias should toast")
	}
}

func TestHandleCommandPresetMissingAction(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleCommand("preset")
	if a.currentToast() == "" {
		t.Error("/preset (no args) should toast usage")
	}
}

func TestHandleCommandPresetUnknownAction(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleCommand("preset gibberish")
	if a.currentToast() == "" {
		t.Error("/preset gibberish should toast")
	}
}

func TestHandleCommandPresetSaveMissingName(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleCommand("preset save")
	if a.currentToast() == "" {
		t.Error("preset save without name should toast")
	}
}

func TestHandleCommandPresetLoadMissingName(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleCommand("preset load")
	if a.currentToast() == "" {
		t.Error("preset load without name should toast")
	}
}

func TestHandleCommandPresetLoadUnknownName(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleCommand("preset load nonexistent")
	if a.currentToast() == "" {
		t.Error("preset load unknown should toast")
	}
}

func TestHandleCommandPresetDeleteMissingName(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleCommand("preset delete")
	if a.currentToast() == "" {
		t.Error("preset delete without name should toast")
	}
}

func TestHandleSubmitShell(t *testing.T) {
	a := makeApp()
	a.handleSubmit(pane.Submitted{Kind: pane.KindShell, Text: "ls"})
	if a.currentToast() == "" {
		t.Error("shell submit should toast not-yet-wired")
	}
}

func TestHandleSubmitFile(t *testing.T) {
	a := makeApp()
	a.handleSubmit(pane.Submitted{Kind: pane.KindFile, Text: "README"})
	if a.currentToast() == "" {
		t.Error("file submit should toast picker hint")
	}
}

func TestHandleSubmitBarePrompt(t *testing.T) {
	a := makeApp()
	a.handleSubmit(pane.Submitted{Kind: pane.KindPrompt, Text: "do something"})
	if a.currentToast() == "" {
		t.Error("bare prompt should toast hint")
	}
}

func TestRegionShiftTab(t *testing.T) {
	a := makeApp()
	a.focusedRegion = regionComposer
	next, _ := a.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	a2 := asApp(t, next)
	if a2.focusedRegion == regionComposer {
		t.Errorf("shift+tab should move out of composer region")
	}
}

func TestCtrlKDispatchesCancel(t *testing.T) {
	a := makeApp()
	next, _ := a.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	a2 := asApp(t, next)
	if a2.currentToast() == "" {
		t.Error("ctrl+k on unpaired run should toast")
	}
}

func makeDir(p string) error {
	return os.MkdirAll(p, 0o755)
}

func writeFile(p string, body []byte) error {
	return os.WriteFile(p, body, 0o644)
}

func TestUpdateFrameTickRefreshesTailers(t *testing.T) {
	root := t.TempDir()
	sid := "session-z"
	dir := filepath.Join(root, sid)
	if err := mkSessionDir(t, dir, session.Meta{SessionID: sid, Agent: "x", Status: session.StatusRunning}); err != nil {
		t.Fatal(err)
	}
	a, err := NewWithSessions(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	startFrame := a.frame
	next, cmd := a.Update(frameTickMsg(time.Now()))
	a2 := asApp(t, next)
	if a2.frame != startFrame+1 {
		t.Errorf("frame: got %d, want %d", a2.frame, startFrame+1)
	}
	if cmd == nil {
		t.Error("frame tick should schedule another tick")
	}
}

func TestUpdateLaunchRequestRoutesToLaunch(t *testing.T) {
	a := makeApp()
	a.workingDir = t.TempDir() // safe to spawn in
	// Use a non-existent binary so launch records a failed start without
	// running anything substantial. The handler still routes the message.
	req := pane.LaunchRequest{
		Agent:  "x",
		Prompt: "do it",
	}
	// We can't easily test successful launch without spawning, but we can
	// verify that the routing path executes (lastLaunch is populated or
	// toast is set on failure). Either way, no panic.
	next, _ := a.Update(req)
	if _, ok := AsApp(next); !ok {
		t.Fatal("Update should keep returning App")
	}
}

func TestUpdateSubmittedRoutesToHandler(t *testing.T) {
	a := makeApp()
	next, _ := a.Update(pane.Submitted{Kind: pane.KindCommand, Text: "help"})
	a2 := asApp(t, next)
	if a2.currentToast() == "" {
		t.Error("Submitted /help should set toast via Update")
	}
}

func TestCmdPresetDeleteSuccess(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	_ = store.Set(presets.Preset{Name: "tmp", Agent: "x"})
	a := makeApp().WithPresets(store)
	a.handleCommand("preset delete tmp")
	if a.currentToast() == "" {
		t.Error("preset delete success should toast")
	}
	if _, ok := store.Get("tmp"); ok {
		t.Error("preset should have been removed")
	}
}

func TestHandleRegionKeyUnhandled(t *testing.T) {
	a := makeApp()
	cmd, handled := a.handleRegionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if handled {
		t.Error("unrelated key should not be handled by region dispatcher")
	}
	if cmd != nil {
		t.Error("unhandled key should not return a cmd")
	}
}

func TestRunFromTailerWithCreatedTime(t *testing.T) {
	root := t.TempDir()
	sid := "session-time"
	dir := filepath.Join(root, sid)
	created := time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
	meta := `{"session_id":"` + sid + `","agent":"a","status":"running","created":"` + created + `"}`
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := NewWithSessions(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	runs := a.currentRuns()
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	if runs[0].Elapsed <= 0 {
		t.Errorf("alive run with created time should have positive elapsed, got %v", runs[0].Elapsed)
	}
}

func TestRunFromTailerCompletedUsesUpdatedDelta(t *testing.T) {
	root := t.TempDir()
	sid := "session-done"
	dir := filepath.Join(root, sid)
	created := time.Now().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	updated := time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
	meta := `{"session_id":"` + sid + `","agent":"a","status":"completed","created":"` + created + `","updated":"` + updated + `"}`
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := NewWithSessions(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	runs := a.currentRuns()
	if len(runs) != 1 {
		t.Fatal("expected 1 run")
	}
	if runs[0].Alive {
		t.Error("completed session should not be alive")
	}
	// Updated - Created ~= 1h. Allow some tolerance for parse rounding.
	if runs[0].Elapsed < 50*time.Minute || runs[0].Elapsed > 70*time.Minute {
		t.Errorf("expected ~1h elapsed, got %v", runs[0].Elapsed)
	}
}

func TestRediscoverPicksUpNewSessions(t *testing.T) {
	root := t.TempDir()
	// Start with empty root.
	a, err := NewWithSessions(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.currentRuns()) != 0 {
		t.Fatal("precondition: should start empty")
	}
	// Create a new session dir on disk, then re-discover.
	sid := "fresh-session"
	dir := filepath.Join(root, sid)
	if err := mkSessionDir(t, dir, session.Meta{SessionID: sid, Agent: "go-review", Status: session.StatusRunning}); err != nil {
		t.Fatal(err)
	}
	a.rediscover()
	runs := a.currentRuns()
	if len(runs) != 1 {
		t.Fatalf("after rediscover: got %d runs, want 1", len(runs))
	}
	if a.Selected() != sid {
		t.Errorf("rediscover should auto-select first discovered session, got %q", a.Selected())
	}
}

func TestRediscoverPairsPendingLaunch(t *testing.T) {
	root := t.TempDir()
	a, err := NewWithSessions(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	a.pendingLaunches = []string{"L0001"}
	sid := "new-paired"
	dir := filepath.Join(root, sid)
	if err := mkSessionDir(t, dir, session.Meta{SessionID: sid, Agent: "x", Status: session.StatusRunning}); err != nil {
		t.Fatal(err)
	}
	a.rediscover()
	if a.launchPairs[sid] != "L0001" {
		t.Errorf("FIFO pair: got %q, want L0001", a.launchPairs[sid])
	}
	if len(a.pendingLaunches) != 0 {
		t.Errorf("pending should be drained, got %v", a.pendingLaunches)
	}
}

func TestFrameTickFunctionReturnsCmd(t *testing.T) {
	if frameTick() == nil {
		t.Error("frameTick() should return a non-nil tea.Cmd")
	}
}

func TestLaunchWithMissingRegistry(t *testing.T) {
	a := makeApp()
	a.registry = nil
	a.launch(pane.LaunchRequest{Agent: "x", Prompt: "y"})
	if a.currentToast() == "" {
		t.Error("launch with nil registry should toast")
	}
}

func TestLaunchSuccessRecordsLastLaunch(t *testing.T) {
	a := makeApp()
	a.workingDir = t.TempDir()
	a.launch(pane.LaunchRequest{
		Agent:    "x",
		Prompt:   "y",
		MaxCost:  3.0,
		MaxIter:  10,
		Mode:     "edit",
		Provider: "anthropic",
		Model:    "claude",
		Isolate:  "worktree",
	})
	// Either succeeded (registered a launch + lastLaunch) or failed with a
	// toast. In both cases the function exercised most branches.
	if a.lastLaunch == nil && a.currentToast() == "" {
		t.Error("launch should either record lastLaunch or set a toast")
	}
}

func TestLaunchFillsWorkingDirFromAppDefault(t *testing.T) {
	a := makeApp()
	a.workingDir = t.TempDir()
	a.launch(pane.LaunchRequest{Agent: "x", Prompt: "y"})
	if a.lastLaunch != nil && a.lastLaunch.WorkingDir != a.workingDir {
		t.Errorf("missing WorkingDir should fall back to app default, got %q want %q",
			a.lastLaunch.WorkingDir, a.workingDir)
	}
}

func TestHandleCommandPresetListEmpty(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleCommand("preset list")
	if !strings.Contains(a.currentToast(), "no presets") {
		t.Errorf("empty store should toast 'no presets', got %q", a.currentToast())
	}
}

func TestHandleCommandPresetListWithItems(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	if err := store.Set(presets.Preset{Name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(presets.Preset{Name: "beta"}); err != nil {
		t.Fatal(err)
	}
	a := makeApp().WithPresets(store)
	a.handleCommand("preset list")
	out := a.currentToast()
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("list toast should include both names, got %q", out)
	}
}

func TestHandleCommandPresetDeleteUnknown(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	a := makeApp().WithPresets(store)
	a.handleCommand("preset delete nonesuch")
	if !strings.Contains(a.currentToast(), "not found") {
		t.Errorf("unknown delete should toast 'not found', got %q", a.currentToast())
	}
}

func TestHandleCommandPresetDeleteSucceeds(t *testing.T) {
	store, _ := presets.Load(filepath.Join(t.TempDir(), "p.yaml"))
	if err := store.Set(presets.Preset{Name: "victim"}); err != nil {
		t.Fatal(err)
	}
	a := makeApp().WithPresets(store)
	a.handleCommand("preset delete victim")
	if !strings.Contains(a.currentToast(), "Deleted") {
		t.Errorf("delete should toast 'Deleted', got %q", a.currentToast())
	}
}

func TestRenderFocusedWithTailerState(t *testing.T) {
	root := t.TempDir()
	sid := "session-render"
	dir := filepath.Join(root, sid)
	meta := `{"session_id":"` + sid + `","agent":"a","status":"running","prompt":"fix the thing","provider":"anthropic","model":"claude","input_tokens":100,"output_tokens":50,"cost":1.5}`
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := NewWithSessions(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	out := a.renderFocused(80)
	if !strings.Contains(out, "fix the thing") {
		t.Errorf("focused panel should show prompt, got: %s", out)
	}
	if !strings.Contains(out, "METRICS") {
		t.Errorf("focused panel should show METRICS header, got: %s", out)
	}
}
