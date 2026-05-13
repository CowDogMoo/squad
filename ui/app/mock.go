package app

import (
	"time"

	"github.com/cowdogmoo/squad/ui/sidebar"
)

// MockRuns returns a hand-crafted list of runs covering all sidebar states
// so the TUI has interesting content before the real subprocess registry
// lands. Treat as a fixture, not an example.
func MockRuns() []sidebar.Run {
	return []sidebar.Run{
		{
			ID:        "20260511T144812Z-a4f2",
			Agent:     "go-review",
			State:     sidebar.StateWorking,
			Alive:     true,
			Elapsed:   8*time.Minute + 24*time.Second,
			LastEvent: "Edit  runner/run.go",
		},
		{
			ID:      "20260511T145501Z-b803",
			Agent:   "python-review",
			State:   sidebar.StateWorking,
			Alive:   true,
			Elapsed: 3*time.Minute + 11*time.Second,
		},
		{
			ID:      "20260511T143230Z-c19f",
			Agent:   "bug-hunt:fix-bug",
			State:   sidebar.StateWorking,
			Alive:   true,
			Elapsed: 14*time.Minute + 42*time.Second,
		},
		{
			ID:        "20260511T145820Z-d447",
			Agent:     "infra-up: confirm $$",
			State:     sidebar.StateNeedsInput,
			Alive:     true,
			Elapsed:   42 * time.Second,
			LastEvent: "awaiting cost confirmation",
		},
		{
			ID:      "20260511T140530Z-e98a",
			Agent:   "go-review-prev",
			State:   sidebar.StateCompleted,
			Alive:   false,
			Elapsed: 9*time.Minute + 42*time.Second,
		},
		{
			ID:      "20260511T135112Z-f221",
			Agent:   "azure-prov-test",
			State:   sidebar.StateFailed,
			Alive:   false,
			Elapsed: 3*time.Minute + 11*time.Second,
		},
		{
			ID:      "20260511T134422Z-005c",
			Agent:   "infra-up",
			State:   sidebar.StateBudget,
			Alive:   false,
			Elapsed: 42 * time.Second,
		},
		{
			ID:      "20260511T133844Z-118e",
			Agent:   "mcp-docs",
			State:   sidebar.StateCompleted,
			Alive:   false,
			Elapsed: 1*time.Minute + 8*time.Second,
		},
		{
			ID:      "20260511T132230Z-2240",
			Agent:   "agents-doc",
			State:   sidebar.StateCompleted,
			Alive:   false,
			Elapsed: 35 * time.Second,
		},
	}
}
