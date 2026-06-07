package pipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestShouldSummarizeAuto(t *testing.T) {
	t.Parallel()
	stage := Stage{Summarize: "auto"}

	if ShouldSummarize(stage, 100) {
		t.Fatal("auto should not trigger for small output")
	}
	if !ShouldSummarize(stage, summarizeThreshold+1) {
		t.Fatal("auto should trigger above threshold")
	}
}

func TestShouldSummarizeAlways(t *testing.T) {
	t.Parallel()
	stage := Stage{Summarize: "always"}

	if !ShouldSummarize(stage, 100) {
		t.Fatal("always should trigger for small output")
	}
	if !ShouldSummarize(stage, summarizeThreshold+1) {
		t.Fatal("always should trigger for large output")
	}
}

func TestShouldSummarizeNever(t *testing.T) {
	t.Parallel()
	stage := Stage{Summarize: "never"}
	if ShouldSummarize(stage, summarizeThreshold+1) {
		t.Fatal("never should not trigger")
	}
}

func TestShouldSummarizeEmpty(t *testing.T) {
	t.Parallel()
	stage := Stage{}
	if ShouldSummarize(stage, summarizeThreshold+1) {
		t.Fatal("empty summarize should default to never")
	}
}

func TestSummarizeOutputNilFunc(t *testing.T) {
	t.Parallel()
	stage := Stage{Summarize: "always"}

	// Short output — returned as-is
	result := SummarizeOutput(context.Background(), nil, stage, "short")
	if result != "short" {
		t.Fatalf("expected short output unchanged, got: %s", result)
	}

	// Long output — truncated
	long := strings.Repeat("x", 5000)
	result = SummarizeOutput(context.Background(), nil, stage, long)
	if !strings.Contains(result, "...(truncated)") {
		t.Fatal("expected truncation fallback for nil func")
	}
	if len(result) > 4200 {
		t.Fatalf("expected truncated output around 4096 bytes, got %d", len(result))
	}
}

func TestSummarizeOutputSuccess(t *testing.T) {
	t.Parallel()
	stage := Stage{Summarize: "always"}
	fn := func(ctx context.Context, sysPrompt, text string) (string, error) {
		return "compressed summary", nil
	}

	long := strings.Repeat("x", 10000)
	result := SummarizeOutput(context.Background(), fn, stage, long)
	if !strings.Contains(result, "Summarized from 10000 bytes") {
		t.Fatalf("expected summary header, got: %s", result)
	}
	if !strings.Contains(result, "compressed summary") {
		t.Fatal("expected summary content")
	}
}

func TestSummarizeOutputCustomPrompt(t *testing.T) {
	t.Parallel()
	stage := Stage{
		Summarize:       "always",
		SummarizePrompt: "Extract vulnerabilities only.",
	}
	var capturedPrompt string
	fn := func(ctx context.Context, sysPrompt, text string) (string, error) {
		capturedPrompt = sysPrompt
		return "vuln summary", nil
	}

	SummarizeOutput(context.Background(), fn, stage, "output")
	if capturedPrompt != "Extract vulnerabilities only." {
		t.Fatalf("expected custom prompt, got: %s", capturedPrompt)
	}
}

func TestSummarizeOutputError(t *testing.T) {
	t.Parallel()
	stage := Stage{Summarize: "always"}
	fn := func(ctx context.Context, sysPrompt, text string) (string, error) {
		return "", fmt.Errorf("LLM unavailable")
	}

	long := strings.Repeat("x", 5000)
	result := SummarizeOutput(context.Background(), fn, stage, long)
	if !strings.Contains(result, "...(truncated)") {
		t.Fatal("expected truncation fallback on error")
	}
}

func TestSummaryCacheGetSet(t *testing.T) {
	t.Parallel()
	c := newSummaryCache()

	_, ok := c.get("key1")
	if ok {
		t.Fatal("expected miss on empty cache")
	}

	c.set("key1", "value1")
	v, ok := c.get("key1")
	if !ok || v != "value1" {
		t.Fatalf("expected hit, got ok=%v v=%q", ok, v)
	}
}

func TestBuildPromptContextWithSummarize(t *testing.T) {
	t.Parallel()

	longOutput := strings.Repeat("x", 10000)
	var summarizeCalled bool
	fn := func(ctx context.Context, sysPrompt, text string) (string, error) {
		summarizeCalled = true
		return "LLM summary of review output", nil
	}

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review", Summarize: "auto"},
			{Name: "fix", Agent: "fixer", DependsOn: []string{"review"}},
		},
	}

	runner := &Runner{
		Pipeline:  p,
		Summarize: fn,
	}

	completed := map[string]*StageResult{
		"review": {
			Name:   "review",
			Status: StatusPassed,
			Agents: []AgentResult{
				{Agent: "go-review", Status: StatusPassed, Output: longOutput},
			},
		},
	}

	ctx := runner.buildPromptContext(context.Background(), p.Stages[1], completed)

	if !summarizeCalled {
		t.Fatal("expected summarize function to be called for large output with auto mode")
	}
	if !strings.Contains(ctx, "LLM summary of review output") {
		t.Fatal("expected summarized output in context")
	}
	if !strings.Contains(ctx, "Summarized from") {
		t.Fatal("expected summary header")
	}
	// Full output should NOT be present
	if strings.Contains(ctx, longOutput) {
		t.Fatal("expected summarized output, not full output")
	}
}

func TestBuildPromptContextSummarizeCache(t *testing.T) {
	t.Parallel()

	callCount := 0
	fn := func(ctx context.Context, sysPrompt, text string) (string, error) {
		callCount++
		return "cached summary", nil
	}

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review", Summarize: "always"},
			{Name: "fix1", Agent: "fixer1", DependsOn: []string{"review"}},
			{Name: "fix2", Agent: "fixer2", DependsOn: []string{"review"}},
		},
	}

	runner := &Runner{
		Pipeline:  p,
		Summarize: fn,
	}

	completed := map[string]*StageResult{
		"review": {
			Name:   "review",
			Status: StatusPassed,
			Agents: []AgentResult{
				{Agent: "go-review", Status: StatusPassed, Output: "output"},
			},
		},
	}

	// Call twice for different downstream stages
	runner.buildPromptContext(context.Background(), p.Stages[1], completed)
	runner.buildPromptContext(context.Background(), p.Stages[2], completed)

	if callCount != 1 {
		t.Fatalf("expected summarize called once (cached), got %d", callCount)
	}
}
