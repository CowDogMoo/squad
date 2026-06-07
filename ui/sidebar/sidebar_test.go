package sidebar

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

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestRender(t *testing.T) {
	mix := []Run{
		{ID: "1", Agent: "go-review", State: StateWorking, Alive: true, Elapsed: 8*time.Minute + 24*time.Second},
		{ID: "2", Agent: "python-review", State: StateWorking, Alive: true, Elapsed: 3*time.Minute + 11*time.Second},
		{ID: "3", Agent: "bug-hunt:fix-bug", State: StateWorking, Alive: true, Elapsed: 14*time.Minute + 42*time.Second},
		{ID: "4", Agent: "infra-up: confirm $$", State: StateNeedsInput, Alive: true, Elapsed: 42 * time.Second},
		{ID: "5", Agent: "go-review-prev", State: StateCompleted, Alive: false, Elapsed: 9*time.Minute + 42*time.Second},
		{ID: "6", Agent: "azure-prov-test", State: StateFailed, Alive: false, Elapsed: 3*time.Minute + 11*time.Second},
		{ID: "7", Agent: "infra-up", State: StateBudget, Alive: false, Elapsed: 42 * time.Second},
		{ID: "8", Agent: "mcp-docs", State: StateCompleted, Alive: false, Elapsed: 1*time.Minute + 8*time.Second},
		{ID: "9", Agent: "agents-doc", State: StateCompleted, Alive: false, Elapsed: 35 * time.Second},
	}

	cases := []struct {
		name string
		snap Snapshot
	}{
		{
			name: "empty",
			snap: Snapshot{Width: 40},
		},
		{
			name: "all_states",
			snap: Snapshot{Runs: mix, Width: 40},
		},
		{
			name: "selected_working",
			snap: Snapshot{Runs: mix, Selected: "1", Width: 40},
		},
		{
			name: "selected_completed",
			snap: Snapshot{Runs: mix, Selected: "5", Width: 40},
		},
		{
			name: "truncate_recent",
			snap: Snapshot{Runs: mix, MaxPerGroup: 2, Width: 40},
		},
		{
			name: "narrow_width_agent_truncates",
			snap: Snapshot{Runs: mix, Width: 24},
		},
		{
			name: "custom_groups_override",
			snap: Snapshot{
				Groups: []Group{
					{Title: "PINNED", Runs: []Run{mix[0]}},
					{Title: "OTHER", Runs: []Run{mix[5]}},
				},
				Width: 40,
			},
		},
		{
			name: "hours_elapsed",
			snap: Snapshot{
				Runs: []Run{
					{ID: "a", Agent: "long-run", State: StateWorking, Alive: true, Elapsed: 1*time.Hour + 12*time.Minute + 4*time.Second},
				},
				Width: 40,
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

func TestBucket(t *testing.T) {
	runs := []Run{
		{ID: "w", State: StateWorking, Alive: true},
		{ID: "c", State: StateConnecting, Alive: true},
		{ID: "ni", State: StateNeedsInput, Alive: true},
		{ID: "bp", State: StateBudget, Alive: true},
		{ID: "done", State: StateCompleted, Alive: false},
		{ID: "fail", State: StateFailed, Alive: false},
		{ID: "bdgt", State: StateBudget, Alive: false},
	}
	groups := Bucket(runs, 0)
	if len(groups) != 3 {
		t.Fatalf("want 3 groups, got %d", len(groups))
	}
	if groups[0].Title != "WORKING" || len(groups[0].Runs) != 2 {
		t.Errorf("WORKING: want 2 runs, got %d", len(groups[0].Runs))
	}
	if groups[1].Title != "NEEDS INPUT" || len(groups[1].Runs) != 2 {
		t.Errorf("NEEDS INPUT: want 2 runs (NeedsInput + alive Budget), got %d", len(groups[1].Runs))
	}
	if groups[2].Title != "RECENT" || len(groups[2].Runs) != 3 {
		t.Errorf("RECENT: want 3 runs, got %d", len(groups[2].Runs))
	}
}

func TestBucketDropsEmptyGroups(t *testing.T) {
	groups := Bucket([]Run{{ID: "a", State: StateCompleted, Alive: false}}, 0)
	if len(groups) != 1 || groups[0].Title != "RECENT" {
		t.Errorf("want one RECENT group, got %v", groups)
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{-time.Second, "0s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m00s"},
		{1*time.Minute + 24*time.Second, "1m24s"},
		{1 * time.Hour, "1h00m"},
		{1*time.Hour + 12*time.Minute + 4*time.Second, "1h12m"},
	}
	for _, tc := range cases {
		if got := formatElapsed(tc.in); got != tc.want {
			t.Errorf("formatElapsed(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
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
