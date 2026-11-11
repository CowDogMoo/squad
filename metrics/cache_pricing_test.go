package metrics

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// Token counts from the claude-code run that prompted this accounting: 94% of
// the prompt was served from the prompt cache. Priced as if every prompt
// token were fresh input, the run came to 7.83 USD.
const (
	baselineFreshInput = 100
	baselineCacheRead  = 3_423_514
	baselineCacheWrite = 235_567
	baselineOutput     = 50_728
)

var baselineUsage = Usage{
	InputTokens:      baselineFreshInput,
	CacheReadTokens:  baselineCacheRead,
	CacheWriteTokens: baselineCacheWrite,
	OutputTokens:     baselineOutput,
}

// baselineModel is priced at 2 USD per million input tokens and 10 USD per
// million output tokens, with no cache prices, so the cache rates come from
// Anthropic's multipliers.
const baselineModel = "claude-baseline-test"

// usePricing swaps the pricing table for cache and restores it afterwards.
// Tests that call it must not run in parallel: the table is package state.
func usePricing(t *testing.T, cache map[string]liteLLMModel) {
	t.Helper()
	ensurePricingLoaded()
	oldCache, oldFetched, oldErr := getPricingState()
	setPricingState(cache, true, nil)
	t.Cleanup(func() { setPricingState(oldCache, oldFetched, oldErr) })
}

func baselinePricing() map[string]liteLLMModel {
	return map[string]liteLLMModel{
		baselineModel: {InputCostPerToken: 2e-6, OutputCostPerToken: 1e-5},
	}
}

func wantClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s = %.6f, want %.6f", name, got, want)
	}
}

// Reproduces the over-count: the run was reported at 7.83 USD because cache
// reads and writes were priced as fresh input. Priced by class it is 1.78 USD.
func TestCostPricesCachedInputSeparately(t *testing.T) {
	usePricing(t, baselinePricing())

	m := New("claude-code", baselineModel)
	m.AddUsage(baselineUsage)

	// 100 fresh at $2/M, 3,423,514 cache reads at $0.20/M, 235,567
	// five-minute cache writes at $2.50/M, 50,728 output at $10/M.
	b := m.CostBreakdown()
	wantClose(t, "input cost", b.Input, 0.0002)
	wantClose(t, "cache read cost", b.CacheRead, 0.6847028)
	wantClose(t, "cache write cost", b.CacheWrite, 0.5889175)
	wantClose(t, "output cost", b.Output, 0.50728)
	wantClose(t, "Cost", m.Cost(), 1.7811003)
	wantClose(t, "TotalCostWithChildren", m.TotalCostWithChildren(), 1.7811003)

	// The token total still counts every prompt token.
	if got := m.InputTokens(); got != 3_659_181 {
		t.Errorf("InputTokens = %d, want 3659181", got)
	}
	if got := m.TotalTokens(); got != 3_659_181+baselineOutput {
		t.Errorf("TotalTokens = %d, want %d", got, 3_659_181+baselineOutput)
	}

	// The same tokens priced as fresh input give the old, inflated figure.
	old := New("claude-code", baselineModel)
	old.AddTokens(3_659_181, baselineOutput)
	wantClose(t, "all-fresh cost", old.Cost(), 7.825642)
}

// A 5 USD cap used to stop the baseline run; at its real cost the run fits.
func TestBudgetCapUsesCachedInputRates(t *testing.T) {
	usePricing(t, baselinePricing())

	m := New("claude-code", baselineModel)
	m.SetMaxCost(5)
	m.AddUsage(baselineUsage)
	if m.BudgetExceeded() {
		t.Errorf("BudgetExceeded = true at $%.4f of a $5 cap, want false", m.TotalCostWithChildren())
	}
	wantClose(t, "RemainingBudget", m.RemainingBudget(), 5-1.7811003)
	wantClose(t, "BudgetUsedPct", m.BudgetUsedPct(), 1.7811003/5)

	// Priced as fresh input, the same tokens blow through the cap.
	old := New("claude-code", baselineModel)
	old.SetMaxCost(5)
	old.AddTokens(3_659_181, baselineOutput)
	if !old.BudgetExceeded() {
		t.Errorf("all-fresh BudgetExceeded = false at $%.4f of a $5 cap, want true", old.TotalCostWithChildren())
	}

	// A cap below the real cost still stops the run.
	tight := New("claude-code", baselineModel)
	tight.SetMaxCost(1.5)
	tight.AddUsage(baselineUsage)
	if !tight.BudgetExceeded() {
		t.Errorf("BudgetExceeded = false at $%.4f of a $1.50 cap, want true", tight.TotalCostWithChildren())
	}
}

func TestCostPricesOneHourCacheWritesHigher(t *testing.T) {
	usePricing(t, baselinePricing())

	m := New("claude-code", baselineModel)
	m.AddUsage(Usage{CacheWriteTokens: 1_000_000, CacheWrite1hTokens: 1_000_000})
	// $2/M input: 1.25x for the five-minute write, 2x for the one-hour write.
	wantClose(t, "cache write cost", m.CostBreakdown().CacheWrite, 2.5+4.0)
}

func TestCostIncludesChildCacheTokens(t *testing.T) {
	usePricing(t, baselinePricing())

	child := New("claude-code", baselineModel)
	child.AddUsage(baselineUsage)

	parent := New("claude-code", baselineModel)
	parent.SetMaxCost(5)
	parent.AddChild("reviewer", child)

	c := parent.Children[0]
	if c.InputTokens != 3_659_181 || c.CacheReadTokens != baselineCacheRead || c.CacheWriteTokens != baselineCacheWrite {
		t.Errorf("child tokens = %+v, want input 3659181 with %d cache read and %d cache write",
			c, baselineCacheRead, baselineCacheWrite)
	}
	if c.Usage() != baselineUsage {
		t.Errorf("child usage = %+v, want %+v", c.Usage(), baselineUsage)
	}
	wantClose(t, "child cost", c.Cost(), 1.7811003)
	wantClose(t, "TotalCostWithChildren", parent.TotalCostWithChildren(), 1.7811003)
	if parent.BudgetExceeded() {
		t.Errorf("BudgetExceeded = true at $%.4f of a $5 cap, want false", parent.TotalCostWithChildren())
	}
}

func TestGetPricingCacheRates(t *testing.T) {
	usePricing(t, map[string]liteLLMModel{
		// Cache prices listed for the model win over the multipliers.
		"claude-listed-test": {
			InputCostPerToken: 2e-6, OutputCostPerToken: 1e-5,
			CacheReadInputTokenCost: 1e-7, CacheCreationInputTokenCost: 3e-6, CacheCreationInputTokenCost1h: 5e-6,
		},
		baselineModel:           {InputCostPerToken: 2e-6, OutputCostPerToken: 1e-5},
		"gpt-cached-test":       {InputCostPerToken: 1e-6, OutputCostPerToken: 8e-6, CacheReadInputTokenCost: 1e-7},
		"gpt-uncached-test":     {InputCostPerToken: 1e-6, OutputCostPerToken: 8e-6},
		"bedrock/claude-hosted": {InputCostPerToken: 3e-6, OutputCostPerToken: 1.5e-5},
	})

	tests := []struct {
		name, provider, model                 string
		wantRead, wantWrite, wantWriteOneHour float64
	}{
		{"listed cache prices", "anthropic", "claude-listed-test", 0.10, 3.00, 5.00},
		{"standard multipliers", "anthropic", baselineModel, 0.20, 2.50, 4.00},
		{"claude-code uses the same rates", "claude-code", baselineModel, 0.20, 2.50, 4.00},
		{"claude on another platform", "anthropic", "claude-hosted", 0.30, 3.75, 6.00},
		{"claude model on openai-compat keeps the input price", "openai-compat", baselineModel, 2.00, 2.00, 2.00},
		{"other provider with a listed cache read price", "openai", "gpt-cached-test", 0.10, 1.00, 1.00},
		{"other provider without cache prices", "openai", "gpt-uncached-test", 1.00, 1.00, 1.00},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := GetPricing(tt.provider, tt.model)
			wantClose(t, "CacheReadPerMillion", p.CacheReadPerMillion, tt.wantRead)
			wantClose(t, "CacheWritePerMillion", p.CacheWritePerMillion, tt.wantWrite)
			wantClose(t, "CacheWrite1hPerMillion", p.CacheWrite1hPerMillion, tt.wantWriteOneHour)
		})
	}
}

func TestSummaryNamesCacheComponents(t *testing.T) {
	usePricing(t, baselinePricing())

	m := New("claude-code", baselineModel)
	m.AddUsage(baselineUsage)
	m.Finish()

	for _, want := range []string{
		"Tokens:     3659181 input / 50728 output (3709909 total)",
		"Input:      100 input / 3423514 cache read / 235567 cache write",
		"Cost:       $1.7811",
		"Cost split: input $0.0002 / cache read $0.6847 / cache write $0.5889 / output $0.5073",
	} {
		if !strings.Contains(m.Summary(), want) {
			t.Errorf("Summary missing %q:\n%s", want, m.Summary())
		}
	}
	if want := "Input: 100 input / 3423514 cache read / 235567 cache write"; !strings.Contains(m.String(), want) {
		t.Errorf("String missing %q: %s", want, m.String())
	}
	if want := "tokens: input=100 cache_read=3423514 cache_write=235567 output=50728; cost: input=$0.0002 cache_read=$0.6847 cache_write=$0.5889 output=$0.5073 total=$1.7811"; m.UsageLogLine() != want {
		t.Errorf("UsageLogLine = %q, want %q", m.UsageLogLine(), want)
	}

	// A run that never touched the prompt cache keeps the short summary.
	plain := New("claude-code", baselineModel)
	plain.AddTokens(1000, 500)
	plain.Finish()
	if s := plain.Summary(); strings.Contains(s, "cache") || strings.Contains(s, "Cost split") {
		t.Errorf("Summary of an uncached run names cache components:\n%s", s)
	}
}

func TestProgressTokensNamesCacheFieldsOnlyWhenUsed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		usage Usage
		want  string
	}{
		{"no tokens yet", Usage{}, "tokens=0/0"},
		{"no cache activity", Usage{InputTokens: 1000, OutputTokens: 500}, "tokens=1000/500"},
		{
			"cache read and write",
			Usage{InputTokens: 100, CacheReadTokens: 9000, CacheWriteTokens: 400, CacheWrite1hTokens: 300, OutputTokens: 50},
			"tokens=9800/50, cache_read=9000, cache_write=700",
		},
		{"cache read only", Usage{InputTokens: 200, CacheReadTokens: 800, OutputTokens: 50}, "tokens=1000/50, cache_read=800"},
		{"cache write only", Usage{InputTokens: 200, CacheWriteTokens: 800, OutputTokens: 50}, "tokens=1000/50, cache_write=800"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := New("anthropic", "claude-test")
			m.AddUsage(tt.usage)
			if got := m.ProgressTokens(); got != tt.want {
				t.Errorf("ProgressTokens = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLogRunHistoryRecordsCacheTokens(t *testing.T) {
	usePricing(t, baselinePricing())

	m := New("claude-code", baselineModel)
	m.AddUsage(Usage{InputTokens: 100, CacheReadTokens: 900, CacheWriteTokens: 40, CacheWrite1hTokens: 60, OutputTokens: 50})
	m.Finish()

	dir := filepath.Join(t.TempDir(), "cache")
	LogRunHistory(dir, "reviewer", m)
	samples, err := LoadHistory(dir, "reviewer")
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(samples))
	}
	s := samples[0]
	if s.InputTokens != 1100 || s.CacheReadTokens != 900 || s.CacheWriteTokens != 100 || s.OutputTokens != 50 {
		t.Errorf("sample = %+v, want input 1100 with 900 cache read, 100 cache write, and 50 output", s)
	}
}
