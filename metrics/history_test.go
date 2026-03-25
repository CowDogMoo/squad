package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogAndLoadHistory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	m := New("anthropic", "claude-opus-4-6")
	m.AddTokens(100000, 25000)
	m.IncrementIterations()
	m.IncrementIterations()
	m.Finish()

	LogRunHistory(dir, "go-review", m)

	samples, err := LoadHistory(dir, "go-review")
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(samples))
	}

	s := samples[0]
	if s.Agent != "go-review" {
		t.Fatalf("Agent = %s, want go-review", s.Agent)
	}
	if s.InputTokens != 100000 {
		t.Fatalf("InputTokens = %d, want 100000", s.InputTokens)
	}
	if s.OutputTokens != 25000 {
		t.Fatalf("OutputTokens = %d, want 25000", s.OutputTokens)
	}
	if s.Iterations != 2 {
		t.Fatalf("Iterations = %d, want 2", s.Iterations)
	}
	if s.Model != "claude-opus-4-6" {
		t.Fatalf("Model = %s, want claude-opus-4-6", s.Model)
	}
}

func TestLogHistoryAppends(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for i := 0; i < 3; i++ {
		m := New("openai", "gpt-4o")
		m.AddTokens(int64(i*1000), int64(i*500))
		m.Finish()
		LogRunHistory(dir, "test-agent", m)
	}

	samples, err := LoadHistory(dir, "test-agent")
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("samples = %d, want 3", len(samples))
	}
}

func TestLogHistoryMaxSamples(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for i := 0; i < maxSamplesPerAgent+10; i++ {
		m := New("openai", "gpt-4o")
		m.AddTokens(int64(i), 0)
		m.Finish()
		LogRunHistory(dir, "overflow-agent", m)
	}

	samples, err := LoadHistory(dir, "overflow-agent")
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(samples) != maxSamplesPerAgent {
		t.Fatalf("samples = %d, want %d", len(samples), maxSamplesPerAgent)
	}
	// Should have kept the most recent samples
	if samples[0].InputTokens != 10 {
		t.Fatalf("oldest kept sample InputTokens = %d, want 10", samples[0].InputTokens)
	}
}

func TestLoadHistoryMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := LoadHistory(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing history")
	}
}

func TestLogHistoryNilMetrics(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Should not panic or create files
	LogRunHistory(dir, "test", nil)
	LogRunHistory(dir, "", New("openai", "gpt-4o"))

	_, err := os.ReadDir(filepath.Join(dir, "cost-history"))
	if err == nil {
		entries, _ := os.ReadDir(filepath.Join(dir, "cost-history"))
		if len(entries) > 0 {
			t.Fatal("expected no history files for nil/empty inputs")
		}
	}
}
