package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

func TestReadCache(t *testing.T) {
	rc := NewReadCache()
	if rc.Len() != 0 {
		t.Fatalf("new cache should be empty, got %d", rc.Len())
	}

	// Store and check
	rc.Store("/foo/bar.go", "abc123", 100, 2048, 1, "")
	if rc.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", rc.Len())
	}

	entry, hit := rc.Check("/foo/bar.go", "abc123")
	if !hit {
		t.Fatal("expected cache hit")
	}
	if entry.Lines != 100 || entry.Bytes != 2048 || entry.Iteration != 1 {
		t.Fatalf("unexpected entry: %+v", entry)
	}

	// Different hash = miss
	_, hit = rc.Check("/foo/bar.go", "different_hash")
	if hit {
		t.Fatal("expected cache miss for different hash")
	}

	// Different path = miss
	_, hit = rc.Check("/foo/baz.go", "abc123")
	if hit {
		t.Fatal("expected cache miss for different path")
	}
}

func TestReadCacheNil(t *testing.T) {
	var rc *ReadCache
	_, hit := rc.Check("/foo", "hash")
	if hit {
		t.Fatal("nil cache should never hit")
	}
	rc.Store("/foo", "hash", 10, 100, 1, "") // should not panic
	if rc.Len() != 0 {
		t.Fatal("nil cache len should be 0")
	}
}

func TestReadCacheContext(t *testing.T) {
	ctx := context.Background()

	// Before init, should return nil
	if rc := GetReadCache(ctx); rc != nil {
		t.Fatal("expected nil cache before init")
	}

	ctx = InitReadCache(ctx)
	rc := GetReadCache(ctx)
	if rc == nil {
		t.Fatal("expected non-nil cache after init")
	}
	rc.Store("/test", "hash1", 50, 500, 0, "")
	if rc.Len() != 1 {
		t.Fatal("expected 1 entry")
	}
}

func TestIterationCounter(t *testing.T) {
	ctx := context.Background()

	// Before init
	if i := GetIteration(ctx); i != 0 {
		t.Fatalf("expected 0, got %d", i)
	}

	ctx = InitIterationCounter(ctx)
	SetIteration(ctx, 5)
	if i := GetIteration(ctx); i != 5 {
		t.Fatalf("expected 5, got %d", i)
	}
	SetIteration(ctx, 10)
	if i := GetIteration(ctx); i != 10 {
		t.Fatalf("expected 10, got %d", i)
	}
}

func TestHashContent(t *testing.T) {
	h1 := HashContent([]byte("hello"))
	h2 := HashContent([]byte("hello"))
	h3 := HashContent([]byte("world"))

	if h1 != h2 {
		t.Fatal("same content should produce same hash")
	}
	if h1 == h3 {
		t.Fatal("different content should produce different hash")
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hex hash, got %d", len(h1))
	}
}

func TestPhaseEnforcer(t *testing.T) {
	// nil enforcer should be safe
	var pe *PhaseEnforcer
	if msg := pe.ObserveTools([]string{"Read"}); msg != "" {
		t.Fatal("nil enforcer should return empty")
	}

	// Basic nudge after 3 read-only iterations
	pe = NewPhaseEnforcer(3)
	if msg := pe.ObserveTools([]string{"Read", "Glob"}); msg != "" {
		t.Fatal("too early for nudge")
	}
	if msg := pe.ObserveTools([]string{"Grep"}); msg != "" {
		t.Fatal("too early for nudge")
	}
	msg := pe.ObserveTools([]string{"Read"})
	if msg == "" {
		t.Fatal("expected nudge after 3 read-only iterations")
	}
	if !strings.Contains(msg, "PROGRESS CHECK") {
		t.Fatalf("expected first nudge to be PROGRESS CHECK, got: %s", msg)
	}

	// After first nudge, ShouldBlockReads is still false (only 1 nudge)
	if pe.ShouldBlockReads() {
		t.Fatal("should not block reads after first nudge")
	}

	// Second nudge fires 3 iterations later (escalating)
	pe.ObserveTools([]string{"Read"})
	pe.ObserveTools([]string{"Read"})
	msg = pe.ObserveTools([]string{"Read"}) // iteration 6
	if msg == "" {
		t.Fatal("expected second nudge at iteration 6")
	}
	if !strings.Contains(msg, "URGENT") {
		t.Fatalf("expected second nudge to be URGENT, got: %s", msg)
	}

	// After second nudge, ShouldBlockReads is true
	if !pe.ShouldBlockReads() {
		t.Fatal("should block reads after 2 ignored nudges")
	}

	// Third nudge fires 3 more iterations later (final warning)
	pe.ObserveTools([]string{"Read"})
	pe.ObserveTools([]string{"Read"})
	msg = pe.ObserveTools([]string{"Read"}) // iteration 9
	if msg == "" {
		t.Fatal("expected third nudge at iteration 9")
	}
	if !strings.Contains(msg, "FINAL WARNING") {
		t.Fatalf("expected third nudge to be FINAL WARNING, got: %s", msg)
	}
}

func TestPhaseEnforcerEditStopsNudge(t *testing.T) {
	pe := NewPhaseEnforcer(3)
	pe.ObserveTools([]string{"Read"})
	pe.ObserveTools([]string{"Edit"}) // Edit attempted but not yet confirmed
	// Confirm the edit succeeded
	pe.ConfirmEdit([]llms.ToolCall{{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Edit"}}}, map[string]string{"1": "ok"})

	// No nudge even after many more read-only iterations
	for i := 0; i < 10; i++ {
		if msg := pe.ObserveTools([]string{"Read"}); msg != "" {
			t.Fatal("no nudge expected after edit was confirmed")
		}
	}
}

func TestPhaseEnforcerFailedEditDoesNotDisarm(t *testing.T) {
	pe := NewPhaseEnforcer(3)
	pe.ObserveTools([]string{"Edit"})
	// Edit failed
	pe.ConfirmEdit([]llms.ToolCall{{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Edit"}}}, map[string]string{"1": "text not found in foo.go"})

	// Should still nudge since edit failed
	pe.ObserveTools([]string{"Read"})
	pe.ObserveTools([]string{"Read"})
	msg := pe.ObserveTools([]string{"Read"})
	if msg == "" {
		t.Fatal("expected nudge after failed Edit + 3 read-only iterations")
	}
}

func TestPhaseEnforcerDisabled(t *testing.T) {
	pe := NewPhaseEnforcer(0)
	if pe != nil {
		t.Fatal("expected nil for nudgeAfter=0")
	}
	pe = NewPhaseEnforcer(-1)
	if pe != nil {
		t.Fatal("expected nil for negative nudgeAfter")
	}
}

func TestCompactionSummary(t *testing.T) {
	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{
					FunctionCall: &llms.FunctionCall{
						Name:      "Read",
						Arguments: `{"path": "foo.go"}`,
					},
				},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{
					FunctionCall: &llms.FunctionCall{
						Name:      "Grep",
						Arguments: `{"pattern": "func main"}`,
					},
				},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{
					FunctionCall: &llms.FunctionCall{
						Name:      "Edit",
						Arguments: `{"path": "foo.go", "old": "a", "new": "b"}`,
					},
				},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{
					FunctionCall: &llms.FunctionCall{
						Name:      "Bash",
						Arguments: `{"command": "go test ./..."}`,
					},
				},
			},
		},
	}

	summary := CompactionSummary(messages, nil)
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if !strings.Contains(summary, "SESSION STATE") {
		t.Fatal("missing SESSION STATE header")
	}
	if !strings.Contains(summary, "foo.go") {
		t.Fatal("missing file read")
	}
	if !strings.Contains(summary, "func main") {
		t.Fatal("missing grep pattern")
	}
	if !strings.Contains(summary, "Edits applied") {
		t.Fatal("missing edit info")
	}
	if !strings.Contains(summary, "go test") {
		t.Fatal("missing bash command")
	}
	if !strings.Contains(summary, "Do NOT re-read") {
		t.Fatal("missing re-read warning")
	}
}

func TestCompactionSummaryEmpty(t *testing.T) {
	messages := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: "hello"}},
		},
	}
	if summary := CompactionSummary(messages, nil); summary != "" {
		t.Fatalf("expected empty summary for no tool calls, got: %s", summary)
	}
}

func TestClassifyFileSize(t *testing.T) {
	// Small file
	r := ClassifyFileSize(50, 1000)
	if r.Action != "full" {
		t.Fatalf("expected full, got %s", r.Action)
	}

	// Medium file
	r = ClassifyFileSize(300, 6000)
	if r.Action != "warn" {
		t.Fatalf("expected warn, got %s", r.Action)
	}
	if r.Warning == "" {
		t.Fatal("expected warning message")
	}

	// Large file
	r = ClassifyFileSize(600, 12000)
	if r.Action != "truncate" {
		t.Fatalf("expected truncate, got %s", r.Action)
	}
	if !strings.Contains(r.Warning, "Large file") {
		t.Fatalf("expected Large file warning, got: %s", r.Warning)
	}
}

func TestTruncateToLines(t *testing.T) {
	// Build a 20-line file
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n")

	// No truncation needed
	result := TruncateToLines(content, 15, 5)
	if result != content {
		t.Fatal("should not truncate when total lines <= head+tail")
	}

	// Truncation
	result = TruncateToLines(content, 5, 3)
	if !strings.Contains(result, "line 1") {
		t.Fatal("should contain first line")
	}
	if !strings.Contains(result, "line 20") {
		t.Fatal("should contain last line")
	}
	if !strings.Contains(result, "12 lines omitted") {
		t.Fatal("should contain omitted count")
	}
}

func TestTokenCalibration(t *testing.T) {
	tc := NewTokenCalibration()

	// No samples — factor should be 1.0
	if f := tc.CorrectionFactor(); f != 1.0 {
		t.Fatalf("expected 1.0, got %f", f)
	}

	// Actual tokens are 20% more than estimated
	tc.Record(1000, 1200)
	tc.Record(2000, 2400)
	f := tc.CorrectionFactor()
	if f < 1.19 || f > 1.21 {
		t.Fatalf("expected ~1.2, got %f", f)
	}

	// Calibrated estimate
	est := tc.CalibratedEstimate(500)
	if est < 590 || est > 610 {
		t.Fatalf("expected ~600, got %d", est)
	}

	if tc.Samples() != 2 {
		t.Fatalf("expected 2 samples, got %d", tc.Samples())
	}
}

func TestTokenCalibrationNil(t *testing.T) {
	var tc *TokenCalibration
	tc.Record(100, 120) // should not panic
	if f := tc.CorrectionFactor(); f != 1.0 {
		t.Fatalf("nil calibration factor should be 1.0, got %f", f)
	}
	if tc.Samples() != 0 {
		t.Fatalf("nil calibration samples should be 0")
	}
}

func TestExtractJSONField(t *testing.T) {
	tests := []struct {
		json  string
		field string
		want  string
	}{
		{`{"path": "foo.go"}`, "path", "foo.go"},
		{`{"pattern":"hello world"}`, "pattern", "hello world"},
		{`{"command": "go test ./..."}`, "command", "go test ./..."},
		{`{"path": 123}`, "path", ""},    // non-string value
		{`{"other": "val"}`, "path", ""}, // field not present
		{`not json`, "path", ""},         // invalid json
	}
	for _, tt := range tests {
		got := extractJSONField(tt.json, tt.field)
		if got != tt.want {
			t.Errorf("extractJSONField(%q, %q) = %q, want %q", tt.json, tt.field, got, tt.want)
		}
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]bool{"c": true, "a": true, "b": true}
	keys := sortedKeys(m)
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Fatalf("expected [a b c], got %v", keys)
	}
}

// --- Adaptive Compaction Threshold Tests ---

type mockBudget struct {
	pct     float64
	maxCost float64
}

func (m *mockBudget) BudgetUsedPct() float64 { return m.pct }
func (m *mockBudget) MaxCostValue() float64  { return m.maxCost }

func TestAdaptiveCompactionThresholdNilMetrics(t *testing.T) {
	t.Parallel()
	if got := AdaptiveCompactionThreshold(nil); got != contextTokenThreshold {
		t.Fatalf("nil metrics: got %d, want %d", got, contextTokenThreshold)
	}
}

func TestAdaptiveCompactionThresholdUnlimited(t *testing.T) {
	t.Parallel()
	m := &mockBudget{pct: 0, maxCost: 0}
	if got := AdaptiveCompactionThreshold(m); got != contextTokenThreshold {
		t.Fatalf("unlimited: got %d, want %d", got, contextTokenThreshold)
	}
}

func TestAdaptiveCompactionThresholdLow(t *testing.T) {
	t.Parallel()
	m := &mockBudget{pct: 0.20, maxCost: 5.0}
	if got := AdaptiveCompactionThreshold(m); got != contextTokenThreshold {
		t.Fatalf("20%% used: got %d, want %d", got, contextTokenThreshold)
	}
}

func TestAdaptiveCompactionThresholdMid(t *testing.T) {
	t.Parallel()
	m := &mockBudget{pct: 0.55, maxCost: 5.0}
	if got := AdaptiveCompactionThreshold(m); got != 40_000 {
		t.Fatalf("55%% used: got %d, want 40000", got)
	}
}

func TestAdaptiveCompactionThresholdHigh(t *testing.T) {
	t.Parallel()
	m := &mockBudget{pct: 0.80, maxCost: 5.0}
	if got := AdaptiveCompactionThreshold(m); got != 30_000 {
		t.Fatalf("80%% used: got %d, want 30000", got)
	}
}

// --- Semantic Message Scoring Tests ---

func TestScoreMessageEditTool(t *testing.T) {
	t.Parallel()
	msg := llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Edit", Arguments: `{"path":"foo.go"}`}},
		},
	}
	scores := ScoreMessages([]llms.MessageContent{msg}, map[string]bool{"foo.go": true}, nil)
	// Edit tool (80) + edited file (60) = 140
	if scores[0].Score < 100 {
		t.Fatalf("edit tool on edited file should score high, got %d", scores[0].Score)
	}
}

func TestScoreMessageEditedFile(t *testing.T) {
	t.Parallel()
	// A Read call on a file that was previously edited should score high
	// due to the "edited file" bonus (60).
	msg := llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"edited.go"}`}},
		},
	}
	editedFiles := map[string]bool{"edited.go": true}
	scores := ScoreMessages([]llms.MessageContent{msg}, editedFiles, nil)
	// Read (no edit tool bonus) + edited file (60) = 60+
	if scores[0].Score < 50 {
		t.Fatalf("read of edited file should score high, got %d", scores[0].Score)
	}
}

func TestScoreMessageRecentFile(t *testing.T) {
	t.Parallel()
	msg := llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"bar.go"}`}},
		},
	}
	scores := ScoreMessages([]llms.MessageContent{msg}, nil, map[string]bool{"bar.go": true})
	// recent file (40) + path exists (10 from else branch — actually 40 wins)
	if scores[0].Score < 30 {
		t.Fatalf("read of recent file should score medium-high, got %d", scores[0].Score)
	}
}

func TestScoreMessageUnrelatedGrep(t *testing.T) {
	t.Parallel()
	msg := llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Grep", Arguments: `{"pattern":"TODO","path":"src/"}`}},
		},
	}
	scores := ScoreMessages([]llms.MessageContent{msg}, nil, nil)
	if scores[0].Score > 20 {
		t.Fatalf("unrelated grep should score low, got %d", scores[0].Score)
	}
}

func TestScoreMessageEditResult(t *testing.T) {
	t.Parallel()
	msg := llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{Content: "updated foo.go (1 replacement)"},
		},
	}
	scores := ScoreMessages([]llms.MessageContent{msg}, nil, nil)
	if scores[0].Score < 40 {
		t.Fatalf("edit result should score medium-high, got %d", scores[0].Score)
	}
}

func TestExtractRecentFiles(t *testing.T) {
	t.Parallel()
	msgs := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"a.go"}`}},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Edit", Arguments: `{"path":"b.go"}`}},
			},
		},
	}
	files := ExtractRecentFiles(msgs)
	if !files["a.go"] || !files["b.go"] {
		t.Fatalf("expected a.go and b.go, got %v", files)
	}
	if files["c.go"] {
		t.Fatal("unexpected file c.go")
	}
}

func TestCollectEditedFiles(t *testing.T) {
	t.Parallel()
	msgs := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"a.go"}`}},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Edit", Arguments: `{"path":"b.go"}`}},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Write", Arguments: `{"path":"c.go"}`}},
			},
		},
	}
	edited := CollectEditedFiles(msgs)
	if edited["a.go"] {
		t.Fatal("Read should not count as edit")
	}
	if !edited["b.go"] {
		t.Fatal("Edit should be tracked")
	}
	if !edited["c.go"] {
		t.Fatal("Write should be tracked")
	}
}

func TestTokenCalibration_ZeroActual(t *testing.T) {
	tc := NewTokenCalibration()
	tc.Record(100, 0)  // actual <= 0, should be ignored
	tc.Record(100, -5) // negative, should be ignored
	if tc.Samples() != 0 {
		t.Fatalf("expected 0 samples for zero/negative actual, got %d", tc.Samples())
	}
	if f := tc.CorrectionFactor(); f != 1.0 {
		t.Fatalf("expected 1.0 with no valid samples, got %f", f)
	}
}

func TestReadCacheStreak(t *testing.T) {
	rc := NewReadCache()

	// Streak starts at 0
	if s := rc.IncrementStreak(); s != 1 {
		t.Fatalf("expected streak 1, got %d", s)
	}
	if s := rc.IncrementStreak(); s != 2 {
		t.Fatalf("expected streak 2, got %d", s)
	}
	if s := rc.IncrementStreak(); s != 3 {
		t.Fatalf("expected streak 3, got %d", s)
	}

	// Store resets streak (new file read)
	rc.Store("/new.go", "hash", 10, 100, 0, "")
	if s := rc.IncrementStreak(); s != 1 {
		t.Fatalf("expected streak reset to 1 after Store, got %d", s)
	}

	// Explicit reset
	rc.IncrementStreak()
	rc.IncrementStreak()
	rc.ResetStreak()
	if s := rc.IncrementStreak(); s != 1 {
		t.Fatalf("expected streak 1 after ResetStreak, got %d", s)
	}
}

func TestReadCacheStreakNil(t *testing.T) {
	var rc *ReadCache
	if s := rc.IncrementStreak(); s != 0 {
		t.Fatalf("nil cache IncrementStreak should return 0, got %d", s)
	}
	rc.ResetStreak() // should not panic
}

func TestReadCacheSummaries(t *testing.T) {
	rc := NewReadCache()
	rc.Store("/a.go", "h1", 100, 2000, 1, "declares: main, handleError")
	rc.Store("/b.go", "h2", 50, 1000, 2, "")
	rc.Store("/c.py", "h3", 30, 600, 3, "declares: Pipeline, run")

	summaries := rc.Summaries()
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}
	if !strings.Contains(summaries["/a.go"], "declares: main") {
		t.Fatalf("expected summary to include declarations, got: %s", summaries["/a.go"])
	}
	if !strings.Contains(summaries["/a.go"], "100 lines") {
		t.Fatalf("expected summary to include line count, got: %s", summaries["/a.go"])
	}
	if strings.Contains(summaries["/b.go"], "declares") {
		t.Fatalf("expected no declarations for /b.go, got: %s", summaries["/b.go"])
	}
}

func TestReadCacheSummariesNil(t *testing.T) {
	var rc *ReadCache
	if s := rc.Summaries(); s != nil {
		t.Fatalf("nil cache Summaries should return nil, got %v", s)
	}
}

func TestGenerateFileSummary(t *testing.T) {
	// Go file with funcs and types
	goContent := `package main

import "fmt"

type Config struct {
	Name string
}

func main() {
	fmt.Println("hello")
}

func (c *Config) String() string {
	return c.Name
}

func handleError(err error) {
	panic(err)
}
`
	summary := GenerateFileSummary(goContent)
	if !strings.Contains(summary, "Config") {
		t.Fatalf("expected Config in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "main") {
		t.Fatalf("expected main in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "String") {
		t.Fatalf("expected String (method) in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "handleError") {
		t.Fatalf("expected handleError in summary, got: %s", summary)
	}

	// Python file
	pyContent := `class Pipeline:
    def run(self):
        pass

    def process(self, data):
        pass

def validate(config):
    return True
`
	summary = GenerateFileSummary(pyContent)
	if !strings.Contains(summary, "Pipeline") {
		t.Fatalf("expected Pipeline in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "run") {
		t.Fatalf("expected run in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "validate") {
		t.Fatalf("expected validate in summary, got: %s", summary)
	}

	// Rust file
	rsContent := `pub fn extract_hashes(data: &[u8]) -> Vec<Hash> {
    todo!()
}

fn parse_ticket(bytes: &[u8]) -> Ticket {
    todo!()
}
`
	summary = GenerateFileSummary(rsContent)
	if !strings.Contains(summary, "extract_hashes") {
		t.Fatalf("expected extract_hashes in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "parse_ticket") {
		t.Fatalf("expected parse_ticket in summary, got: %s", summary)
	}

	// Empty file
	if s := GenerateFileSummary(""); s != "" {
		t.Fatalf("expected empty summary for empty file, got: %s", s)
	}

	// File with no declarations
	if s := GenerateFileSummary("just some text\nanother line\n"); s != "" {
		t.Fatalf("expected empty summary for text-only file, got: %s", s)
	}
}

func TestReadCacheCompactionEpoch(t *testing.T) {
	rc := NewReadCache()

	// Store a file at epoch 0.
	rc.Store("/foo.go", "hash1", 100, 2000, 1, "declares: main")
	entry, hit := rc.Check("/foo.go", "hash1")
	if !hit {
		t.Fatal("expected cache hit")
	}

	// Before compaction, entry should NOT be stale.
	if rc.IsStaleAfterCompaction(entry) {
		t.Fatal("entry should not be stale before compaction")
	}

	// Bump compaction epoch to simulate context compaction.
	rc.BumpCompactionEpoch()
	if rc.CompactionEpoch() != 1 {
		t.Fatalf("expected epoch 1, got %d", rc.CompactionEpoch())
	}

	// Same entry should now be stale.
	if !rc.IsStaleAfterCompaction(entry) {
		t.Fatal("entry should be stale after compaction")
	}

	// Re-store the file (simulating re-read after compaction).
	rc.Store("/foo.go", "hash1", 100, 2000, 5, "declares: main")
	entry2, hit := rc.Check("/foo.go", "hash1")
	if !hit {
		t.Fatal("expected cache hit after re-store")
	}

	// After re-store at current epoch, entry should NOT be stale.
	if rc.IsStaleAfterCompaction(entry2) {
		t.Fatal("re-stored entry should not be stale")
	}

	// Another compaction makes it stale again.
	rc.BumpCompactionEpoch()
	if !rc.IsStaleAfterCompaction(entry2) {
		t.Fatal("entry should be stale after second compaction")
	}
}

func TestReadCacheCompactionEpochNil(t *testing.T) {
	var rc *ReadCache
	rc.BumpCompactionEpoch() // should not panic
	if rc.CompactionEpoch() != 0 {
		t.Fatal("nil cache epoch should be 0")
	}
	if rc.IsStaleAfterCompaction(ReadCacheEntry{}) {
		t.Fatal("nil cache should never report stale")
	}
}

func TestCompactionSummaryWithCache(t *testing.T) {
	rc := NewReadCache()
	rc.Store("/src/main.go", "h1", 100, 2000, 1, "declares: main, handleError")
	rc.Store("/src/config.go", "h2", 50, 1000, 2, "declares: Config, Load")

	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{
					FunctionCall: &llms.FunctionCall{
						Name:      "Read",
						Arguments: `{"path": "/src/main.go"}`,
					},
				},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{
					FunctionCall: &llms.FunctionCall{
						Name:      "Read",
						Arguments: `{"path": "/src/config.go"}`,
					},
				},
			},
		},
	}

	summary := CompactionSummary(messages, rc)
	if !strings.Contains(summary, "previously read") {
		t.Fatalf("expected 'previously read' header with cache, got: %s", summary)
	}
	if !strings.Contains(summary, "declares: main") {
		t.Fatalf("expected file summary in compaction, got: %s", summary)
	}
	if !strings.Contains(summary, "declares: Config") {
		t.Fatalf("expected config summary in compaction, got: %s", summary)
	}
	if !strings.Contains(summary, "do NOT re-read") {
		t.Fatalf("expected re-read warning, got: %s", summary)
	}
}
