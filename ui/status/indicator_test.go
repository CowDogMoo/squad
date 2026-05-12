package status

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update", false, "regenerate golden fixtures")

// ansiRE strips terminal escape sequences so goldens are plain text.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestRender(t *testing.T) {
	cases := []struct {
		name string
		snap Snapshot
	}{
		{
			name: "idle",
			snap: Snapshot{State: StateIdle, Width: 80},
		},
		{
			name: "connecting_frame_0",
			snap: Snapshot{State: StateConnecting, Frame: 0, Width: 80, Interrupt: true},
		},
		{
			name: "connecting_frame_2",
			snap: Snapshot{State: StateConnecting, Frame: 2, Width: 80, Interrupt: true},
		},
		{
			name: "working_minimal",
			snap: Snapshot{
				State:   StateWorking,
				Frame:   0,
				Elapsed: 8 * time.Second,
				Width:   80,
			},
		},
		{
			name: "working_with_detail_and_interrupt",
			snap: Snapshot{
				State:     StateWorking,
				Frame:     3,
				Label:     "Working",
				Detail:    "31/40 iter · 142 tools · $1.84/$5.00",
				Elapsed:   8*time.Minute + 24*time.Second,
				Width:     120,
				Interrupt: true,
			},
		},
		{
			name: "working_hours",
			snap: Snapshot{
				State:   StateWorking,
				Frame:   5,
				Elapsed: 1*time.Hour + 12*time.Minute + 4*time.Second,
				Width:   80,
			},
		},
		{
			name: "completed",
			snap: Snapshot{
				State:   StateCompleted,
				Detail:  "7 files changed · $1.94 final",
				Elapsed: 9*time.Minute + 42*time.Second,
				Width:   100,
			},
		},
		{
			name: "error",
			snap: Snapshot{
				State:   StateError,
				Detail:  "stage=fix-bugs agent=go-review",
				Elapsed: 3*time.Minute + 11*time.Second,
				Width:   100,
			},
		},
		{
			name: "paused_budget",
			snap: Snapshot{
				State:   StatePaused,
				Label:   "Budget exceeded",
				Detail:  "$5.00 cap reached",
				Elapsed: 42 * time.Second,
				Width:   100,
			},
		},
		{
			name: "no_width_compact",
			snap: Snapshot{
				State:   StateWorking,
				Frame:   1,
				Detail:  "compact",
				Elapsed: 5 * time.Second,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripANSI(Render(tc.snap))
			compareGolden(t, tc.name+".txt", got)
		})
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{-5 * time.Second, "0s"},
		{1 * time.Second, "1s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m 00s"},
		{1*time.Minute + 24*time.Second, "1m 24s"},
		{59*time.Minute + 59*time.Second, "59m 59s"},
		{1 * time.Hour, "1h 00m 00s"},
		{1*time.Hour + 12*time.Minute + 4*time.Second, "1h 12m 04s"},
	}
	for _, tc := range cases {
		got := formatElapsed(tc.in)
		if got != tc.want {
			t.Errorf("formatElapsed(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestModelLifecycle(t *testing.T) {
	m := New()
	if got := m.Snapshot().State; got != StateIdle {
		t.Fatalf("new model state = %v, want Idle", got)
	}

	// Transition to working — starts the elapsed clock.
	m.SetState(StateWorking)
	if m.started.IsZero() {
		t.Fatal("StateWorking should start the elapsed clock")
	}

	// Terminal state freezes elapsed and clears started.
	m.SetState(StateCompleted)
	if !m.started.IsZero() {
		t.Fatal("StateCompleted should freeze the elapsed clock")
	}

	// Going back to working resets.
	m.SetState(StateWorking)
	if m.started.IsZero() {
		t.Fatal("re-entering StateWorking should reset elapsed clock")
	}
}

func TestModelTickAdvancesFrame(t *testing.T) {
	m := New()
	m.SetState(StateWorking)
	startFrame := m.Snapshot().Frame
	m, _ = m.Update(TickMsg(time.Now()))
	if m.Snapshot().Frame != startFrame+1 {
		t.Errorf("tick should advance frame: got %d, want %d", m.Snapshot().Frame, startFrame+1)
	}
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		// Trailing newline keeps the file POSIX-friendly and end-of-file
		// hooks happy; tests normalize when reading.
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create)", path, err)
	}
	wantStr := strings.TrimRight(string(want), "\n")
	if got != wantStr {
		t.Errorf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", name, wantStr, got)
	}
}
