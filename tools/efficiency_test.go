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
	rc.Store("/foo/bar.go", "abc123", 100, 2048, 1)
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
	rc.Store("/foo", "hash", 10, 100, 1) // should not panic
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
	rc.Store("/test", "hash1", 50, 500, 0)
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
		t.Fatalf("unexpected nudge message: %s", msg)
	}

	// Nudge only sent once
	if msg := pe.ObserveTools([]string{"Read"}); msg != "" {
		t.Fatal("nudge should only be sent once")
	}
}

func TestPhaseEnforcerEditStopsNudge(t *testing.T) {
	pe := NewPhaseEnforcer(3)
	pe.ObserveTools([]string{"Read"})
	pe.ObserveTools([]string{"Edit"}) // should mark edit seen

	// No nudge even after many more read-only iterations
	for i := 0; i < 10; i++ {
		if msg := pe.ObserveTools([]string{"Read"}); msg != "" {
			t.Fatal("no nudge expected after edit was seen")
		}
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

	summary := CompactionSummary(messages)
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
	if summary := CompactionSummary(messages); summary != "" {
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
