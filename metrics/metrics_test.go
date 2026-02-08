package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")

	if m.Provider != "openai" {
		t.Fatalf("Provider = %q, want openai", m.Provider)
	}
	if m.Model != "gpt-4o" {
		t.Fatalf("Model = %q, want gpt-4o", m.Model)
	}
	if m.StartTime.IsZero() {
		t.Fatalf("StartTime should be set")
	}
	if m.InputTokens != 0 {
		t.Fatalf("InputTokens = %d, want 0", m.InputTokens)
	}
	if m.OutputTokens != 0 {
		t.Fatalf("OutputTokens = %d, want 0", m.OutputTokens)
	}
	if m.Iterations != 0 {
		t.Fatalf("Iterations = %d, want 0", m.Iterations)
	}
}

func TestAddTokens(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")

	m.AddTokens(100, 50)
	if m.InputTokens != 100 {
		t.Fatalf("InputTokens = %d, want 100", m.InputTokens)
	}
	if m.OutputTokens != 50 {
		t.Fatalf("OutputTokens = %d, want 50", m.OutputTokens)
	}

	// Accumulate more tokens
	m.AddTokens(200, 100)
	if m.InputTokens != 300 {
		t.Fatalf("InputTokens = %d, want 300", m.InputTokens)
	}
	if m.OutputTokens != 150 {
		t.Fatalf("OutputTokens = %d, want 150", m.OutputTokens)
	}
}

func TestAddTokensZero(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	m.AddTokens(100, 50)

	// Adding zero should not change totals
	m.AddTokens(0, 0)
	if m.InputTokens != 100 {
		t.Fatalf("InputTokens = %d, want 100", m.InputTokens)
	}
	if m.OutputTokens != 50 {
		t.Fatalf("OutputTokens = %d, want 50", m.OutputTokens)
	}
}

func TestIncrementIterations(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")

	m.IncrementIterations()
	if m.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1", m.Iterations)
	}

	m.IncrementIterations()
	m.IncrementIterations()
	if m.Iterations != 3 {
		t.Fatalf("Iterations = %d, want 3", m.Iterations)
	}
}

func TestFinish(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")

	if !m.EndTime.IsZero() {
		t.Fatalf("EndTime should be zero before Finish()")
	}

	time.Sleep(5 * time.Millisecond)
	m.Finish()

	if m.EndTime.IsZero() {
		t.Fatalf("EndTime should be set after Finish()")
	}
	if m.EndTime.Before(m.StartTime) {
		t.Fatalf("EndTime should be after StartTime")
	}
}

func TestDurationBeforeFinish(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	time.Sleep(10 * time.Millisecond)

	d := m.Duration()
	if d < 10*time.Millisecond {
		t.Fatalf("Duration() should be at least 10ms, got %v", d)
	}
}

func TestDurationAfterFinish(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	time.Sleep(10 * time.Millisecond)
	m.Finish()

	d1 := m.Duration()
	time.Sleep(10 * time.Millisecond)
	d2 := m.Duration()

	// After Finish(), duration should be fixed
	if d1 != d2 {
		t.Fatalf("Duration() should be fixed after Finish(), got %v and %v", d1, d2)
	}
}

func TestTotalTokens(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")

	if m.TotalTokens() != 0 {
		t.Fatalf("TotalTokens() = %d, want 0", m.TotalTokens())
	}

	m.AddTokens(1000, 500)
	if m.TotalTokens() != 1500 {
		t.Fatalf("TotalTokens() = %d, want 1500", m.TotalTokens())
	}
}

func TestGetPricingOllama(t *testing.T) {
	t.Parallel()
	// Ollama is always free (local inference)
	models := []string{"llama3", "mistral", "codellama", "phi3"}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			p := GetPricing("ollama", model)
			if p.InputPerMillion != 0 {
				t.Fatalf("InputPerMillion = %v, want 0", p.InputPerMillion)
			}
			if p.OutputPerMillion != 0 {
				t.Fatalf("OutputPerMillion = %v, want 0", p.OutputPerMillion)
			}
		})
	}
}

func TestGetPricingUnknown(t *testing.T) {
	t.Parallel()
	// Unknown models should return zero pricing
	p := GetPricing("unknown-provider", "unknown-model-xyz-123")
	if p.InputPerMillion != 0 || p.OutputPerMillion != 0 {
		t.Fatalf("unknown model should have zero pricing, got input=%v output=%v",
			p.InputPerMillion, p.OutputPerMillion)
	}
}

func TestGetPricingCaseInsensitive(t *testing.T) {
	t.Parallel()
	// Provider and model should be case-insensitive
	p1 := GetPricing("OpenAI", "GPT-4o")
	p2 := GetPricing("openai", "gpt-4o")

	if p1.InputPerMillion != p2.InputPerMillion {
		t.Fatalf("pricing should be case-insensitive, got %v and %v",
			p1.InputPerMillion, p2.InputPerMillion)
	}
}

func TestGetPricingLiteLLMFetch(t *testing.T) {
	// This test verifies that LiteLLM pricing is fetched for known models.
	// It requires network access and may be skipped in CI.
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	// gpt-4o is a well-known model that should be in LiteLLM's database
	p := GetPricing("openai", "gpt-4o")

	// We don't check exact values since LiteLLM prices may change,
	// but gpt-4o should have non-zero pricing
	if p.InputPerMillion == 0 || p.OutputPerMillion == 0 {
		t.Logf("Warning: gpt-4o returned zero pricing, LiteLLM fetch may have failed")
	}
}

func TestCostCalculation(t *testing.T) {
	t.Parallel()

	// Test with Ollama (free)
	m := New("ollama", "llama3")
	m.AddTokens(1_000_000, 1_000_000)
	if m.Cost() != 0 {
		t.Fatalf("Ollama cost should be 0, got %v", m.Cost())
	}

	// Test with zero tokens
	m2 := New("openai", "gpt-4o")
	if m2.Cost() != 0 {
		t.Fatalf("Zero tokens should have zero cost, got %v", m2.Cost())
	}
}

func TestStringOutput(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	m.AddTokens(100, 50)
	m.IncrementIterations()
	m.Finish()

	s := m.String()
	if !strings.Contains(s, "Duration:") {
		t.Fatalf("String() missing Duration")
	}
	if !strings.Contains(s, "Iterations: 1") {
		t.Fatalf("String() missing Iterations")
	}
	if !strings.Contains(s, "100 in / 50 out") {
		t.Fatalf("String() missing token counts")
	}
	if !strings.Contains(s, "150 total") {
		t.Fatalf("String() missing total tokens")
	}
}

func TestStringOllama(t *testing.T) {
	t.Parallel()
	m := New("ollama", "llama3")
	m.AddTokens(1000, 500)
	m.Finish()

	s := m.String()
	if !strings.Contains(s, "$0.00 (local)") {
		t.Fatalf("String() should show $0.00 (local) for ollama, got: %s", s)
	}
}

func TestStringUnknownPricing(t *testing.T) {
	t.Parallel()
	m := New("unknown", "unknown-model")
	m.AddTokens(1000, 500)
	m.Finish()

	s := m.String()
	if !strings.Contains(s, "N/A (pricing not available yet)") {
		t.Fatalf("String() should show pricing not available, got: %s", s)
	}
}

func TestSummaryOutput(t *testing.T) {
	t.Parallel()
	m := New("anthropic", "claude-3-opus")
	m.AddTokens(1000, 500)
	m.IncrementIterations()
	m.Finish()

	summary := m.Summary()
	if !strings.Contains(summary, "Agent Metrics") {
		t.Fatalf("Summary() missing header")
	}
	if !strings.Contains(summary, "claude-3-opus") {
		t.Fatalf("Summary() missing model name")
	}
	if !strings.Contains(summary, "anthropic") {
		t.Fatalf("Summary() missing provider")
	}
	if !strings.Contains(summary, "Duration:") {
		t.Fatalf("Summary() missing Duration")
	}
	if !strings.Contains(summary, "Iterations:") {
		t.Fatalf("Summary() missing Iterations")
	}
	if !strings.Contains(summary, "Tokens:") {
		t.Fatalf("Summary() missing Tokens")
	}
	if !strings.Contains(summary, "Cost:") {
		t.Fatalf("Summary() missing Cost")
	}
}

func TestSummaryOllama(t *testing.T) {
	t.Parallel()
	m := New("ollama", "llama3")
	m.AddTokens(1000, 500)
	m.Finish()

	summary := m.Summary()
	if !strings.Contains(summary, "$0.00 (local)") {
		t.Fatalf("Summary() should show $0.00 (local) for ollama")
	}
}

func TestSummaryUnknownPricing(t *testing.T) {
	t.Parallel()
	m := New("unknown", "model")
	m.AddTokens(1000, 500)
	m.Finish()

	summary := m.Summary()
	if !strings.Contains(summary, "N/A (pricing not available yet)") {
		t.Fatalf("Summary() should show pricing not available")
	}
}

func TestMetricsIntegration(t *testing.T) {
	t.Parallel()
	// Simulate a typical agent run with Ollama (guaranteed zero cost)
	m := New("ollama", "llama3")

	// Simulate multiple iterations with token accumulation
	for i := 0; i < 5; i++ {
		m.IncrementIterations()
		m.AddTokens(500, 200) // 500 input, 200 output per iteration
	}

	m.Finish()

	if m.Iterations != 5 {
		t.Fatalf("Iterations = %d, want 5", m.Iterations)
	}
	if m.InputTokens != 2500 {
		t.Fatalf("InputTokens = %d, want 2500", m.InputTokens)
	}
	if m.OutputTokens != 1000 {
		t.Fatalf("OutputTokens = %d, want 1000", m.OutputTokens)
	}
	if m.TotalTokens() != 3500 {
		t.Fatalf("TotalTokens() = %d, want 3500", m.TotalTokens())
	}
	if m.Cost() != 0 {
		t.Fatalf("Ollama cost should be 0, got %v", m.Cost())
	}
}

func TestCostString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		cost     float64
		want     string
	}{
		{"positive cost", "openai", 1.2345, "$1.2345"},
		{"zero cost ollama", "ollama", 0, "$0.00 (local)"},
		{"zero cost unknown", "unknown", 0, "N/A (pricing not available yet)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Metrics{Provider: tt.provider}
			got := m.costString(tt.cost)
			if got != tt.want {
				t.Fatalf("costString(%v) = %q, want %q", tt.cost, got, tt.want)
			}
		})
	}
}

func TestPricingStatus(t *testing.T) {
	// This test verifies PricingStatus returns sensible values.
	// It requires network access for a successful fetch.
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	loaded, count, err := PricingStatus()

	// If fetch succeeded, we should have models loaded
	if loaded {
		if count == 0 {
			t.Fatal("PricingStatus reports loaded but count is 0")
		}
		if err != nil {
			t.Fatalf("PricingStatus reports loaded but has error: %v", err)
		}
		t.Logf("Pricing loaded successfully: %d models", count)
	} else {
		// If not loaded, there should be an error explaining why
		if err == nil {
			t.Log("Warning: PricingStatus not loaded but no error reported")
		} else {
			t.Logf("Pricing not loaded: %v", err)
		}
	}
}
