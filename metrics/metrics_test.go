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

func TestGetPricingOpenAI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		model  string
		input  float64
		output float64
	}{
		{"gpt-5", 15.00, 60.00},
		{"gpt-5-turbo", 15.00, 60.00},
		{"gpt-4o", 2.50, 10.00},
		{"gpt-4o-mini", 0.15, 0.60},
		{"gpt-4o-mini-2024", 0.15, 0.60},
		{"gpt-4-turbo", 10.00, 30.00},
		{"gpt-4-turbo-preview", 10.00, 30.00},
		{"gpt-4", 30.00, 60.00},
		{"gpt-4-0613", 30.00, 60.00},
		{"gpt-3.5-turbo", 0.50, 1.50},
		{"o1", 15.00, 60.00},
		{"o1-preview", 15.00, 60.00},
		{"o3", 10.00, 40.00},
		{"o3-mini", 10.00, 40.00},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := GetPricing("openai", tt.model)
			if p.InputPerMillion != tt.input {
				t.Fatalf("InputPerMillion = %v, want %v", p.InputPerMillion, tt.input)
			}
			if p.OutputPerMillion != tt.output {
				t.Fatalf("OutputPerMillion = %v, want %v", p.OutputPerMillion, tt.output)
			}
		})
	}
}

func TestGetPricingOpenAIResponses(t *testing.T) {
	t.Parallel()
	// openai-responses should use the same pricing as openai
	p := GetPricing("openai-responses", "gpt-4o")
	if p.InputPerMillion != 2.50 {
		t.Fatalf("InputPerMillion = %v, want 2.50", p.InputPerMillion)
	}
}

func TestGetPricingAnthropic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		model  string
		input  float64
		output float64
	}{
		{"claude-3-opus", 15.00, 75.00},
		{"claude-3-opus-20240229", 15.00, 75.00},
		{"claude-3-sonnet", 3.00, 15.00},
		{"claude-3-5-sonnet", 3.00, 15.00},
		{"claude-3-haiku", 0.25, 1.25},
		{"claude-3-5-haiku", 0.25, 1.25},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := GetPricing("anthropic", tt.model)
			if p.InputPerMillion != tt.input {
				t.Fatalf("InputPerMillion = %v, want %v", p.InputPerMillion, tt.input)
			}
			if p.OutputPerMillion != tt.output {
				t.Fatalf("OutputPerMillion = %v, want %v", p.OutputPerMillion, tt.output)
			}
		})
	}
}

func TestGetPricingGemini(t *testing.T) {
	t.Parallel()
	tests := []struct {
		model  string
		input  float64
		output float64
	}{
		{"gemini-pro", 1.25, 5.00},
		{"gemini-1.5-pro", 1.25, 5.00},
		{"gemini-flash", 0.075, 0.30},
		{"gemini-1.5-flash", 0.075, 0.30},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := GetPricing("gemini", tt.model)
			if p.InputPerMillion != tt.input {
				t.Fatalf("InputPerMillion = %v, want %v", p.InputPerMillion, tt.input)
			}
			if p.OutputPerMillion != tt.output {
				t.Fatalf("OutputPerMillion = %v, want %v", p.OutputPerMillion, tt.output)
			}
		})
	}
}

func TestGetPricingOllama(t *testing.T) {
	t.Parallel()
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
	p := GetPricing("unknown-provider", "unknown-model")
	if p.InputPerMillion != 0 || p.OutputPerMillion != 0 {
		t.Fatalf("unknown model should have zero pricing")
	}
}

func TestGetPricingCaseInsensitive(t *testing.T) {
	t.Parallel()
	// Provider and model should be case-insensitive
	p1 := GetPricing("OpenAI", "GPT-4o")
	p2 := GetPricing("openai", "gpt-4o")

	if p1.InputPerMillion != p2.InputPerMillion {
		t.Fatalf("pricing should be case-insensitive")
	}
}

func TestCost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		provider     string
		model        string
		inputTokens  int64
		outputTokens int64
		wantCost     float64
	}{
		{
			name:         "gpt-4o 1M tokens each",
			provider:     "openai",
			model:        "gpt-4o",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			wantCost:     12.50, // 2.50 + 10.00
		},
		{
			name:         "gpt-4o-mini 1M tokens each",
			provider:     "openai",
			model:        "gpt-4o-mini",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			wantCost:     0.75, // 0.15 + 0.60
		},
		{
			name:         "claude-3-sonnet 1M tokens each",
			provider:     "anthropic",
			model:        "claude-3-sonnet",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			wantCost:     18.00, // 3.00 + 15.00
		},
		{
			name:         "claude-3-haiku 1M tokens each",
			provider:     "anthropic",
			model:        "claude-3-haiku",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			wantCost:     1.50, // 0.25 + 1.25
		},
		{
			name:         "ollama free",
			provider:     "ollama",
			model:        "llama3",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			wantCost:     0,
		},
		{
			name:         "zero tokens",
			provider:     "openai",
			model:        "gpt-4o",
			inputTokens:  0,
			outputTokens: 0,
			wantCost:     0,
		},
		{
			name:         "small token count",
			provider:     "openai",
			model:        "gpt-4o",
			inputTokens:  1000,
			outputTokens: 500,
			wantCost:     0.0075, // (1000/1M * 2.50) + (500/1M * 10.00) = 0.0025 + 0.005
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(tt.provider, tt.model)
			m.AddTokens(tt.inputTokens, tt.outputTokens)
			cost := m.Cost()
			if cost != tt.wantCost {
				t.Fatalf("Cost() = %v, want %v", cost, tt.wantCost)
			}
		})
	}
}

func TestStringWithCost(t *testing.T) {
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
	if !strings.Contains(s, "$") {
		t.Fatalf("String() missing cost")
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
	if !strings.Contains(s, "N/A") {
		t.Fatalf("String() should show N/A for unknown pricing, got: %s", s)
	}
}

func TestSummaryWithCost(t *testing.T) {
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
	if !strings.Contains(summary, "$") {
		t.Fatalf("Summary() missing dollar sign in cost")
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
	if !strings.Contains(summary, "N/A (unknown pricing)") {
		t.Fatalf("Summary() should show N/A for unknown pricing")
	}
}

func TestMetricsIntegration(t *testing.T) {
	t.Parallel()
	// Simulate a typical agent run
	m := New("openai", "gpt-4o")

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

	// Cost: (2500/1M * 2.50) + (1000/1M * 10.00) = 0.00625 + 0.01 = 0.01625
	expectedCost := 0.01625
	if m.Cost() != expectedCost {
		t.Fatalf("Cost() = %v, want %v", m.Cost(), expectedCost)
	}
}
