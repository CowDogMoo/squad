// Package metrics provides token usage tracking and cost analysis for agent runs.
package metrics

import (
	"fmt"
	"strings"
	"time"
)

// Metrics tracks resource usage for an agent run.
type Metrics struct {
	StartTime    time.Time
	EndTime      time.Time
	InputTokens  int64
	OutputTokens int64
	Iterations   int
	Model        string
	Provider     string
}

// New creates a new Metrics instance with the start time set to now.
func New(provider, model string) *Metrics {
	return &Metrics{
		StartTime: time.Now(),
		Provider:  provider,
		Model:     model,
	}
}

// AddTokens adds token counts to the running total.
func (m *Metrics) AddTokens(input, output int64) {
	m.InputTokens += input
	m.OutputTokens += output
}

// IncrementIterations increments the iteration counter.
func (m *Metrics) IncrementIterations() {
	m.Iterations++
}

// Finish marks the end time.
func (m *Metrics) Finish() {
	m.EndTime = time.Now()
}

// Duration returns the elapsed time.
func (m *Metrics) Duration() time.Duration {
	if m.EndTime.IsZero() {
		return time.Since(m.StartTime)
	}
	return m.EndTime.Sub(m.StartTime)
}

// TotalTokens returns the sum of input and output tokens.
func (m *Metrics) TotalTokens() int64 {
	return m.InputTokens + m.OutputTokens
}

// Cost calculates the estimated cost in USD based on model pricing.
func (m *Metrics) Cost() float64 {
	pricing := GetPricing(m.Provider, m.Model)
	inputCost := float64(m.InputTokens) / 1_000_000 * pricing.InputPerMillion
	outputCost := float64(m.OutputTokens) / 1_000_000 * pricing.OutputPerMillion
	return inputCost + outputCost
}

// Pricing holds per-million-token costs for a model.
type Pricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// GetPricing returns the pricing for a given provider and model.
func GetPricing(provider, model string) Pricing {
	model = strings.ToLower(model)
	provider = strings.ToLower(provider)

	switch provider {
	case "openai", "openai-responses", "":
		return getOpenAIPricing(model)
	case "anthropic":
		return getAnthropicPricing(model)
	case "gemini":
		return getGeminiPricing(model)
	case "ollama":
		return Pricing{InputPerMillion: 0, OutputPerMillion: 0}
	default:
		return Pricing{InputPerMillion: 0, OutputPerMillion: 0}
	}
}

func getOpenAIPricing(model string) Pricing {
	switch {
	case strings.HasPrefix(model, "gpt-5"):
		return Pricing{InputPerMillion: 15.00, OutputPerMillion: 60.00}
	case strings.HasPrefix(model, "gpt-4o-mini"):
		return Pricing{InputPerMillion: 0.15, OutputPerMillion: 0.60}
	case strings.HasPrefix(model, "gpt-4o"):
		return Pricing{InputPerMillion: 2.50, OutputPerMillion: 10.00}
	case strings.HasPrefix(model, "gpt-4-turbo"):
		return Pricing{InputPerMillion: 10.00, OutputPerMillion: 30.00}
	case strings.HasPrefix(model, "gpt-4"):
		return Pricing{InputPerMillion: 30.00, OutputPerMillion: 60.00}
	case strings.HasPrefix(model, "gpt-3.5"):
		return Pricing{InputPerMillion: 0.50, OutputPerMillion: 1.50}
	case strings.HasPrefix(model, "o1"):
		return Pricing{InputPerMillion: 15.00, OutputPerMillion: 60.00}
	case strings.HasPrefix(model, "o3"):
		return Pricing{InputPerMillion: 10.00, OutputPerMillion: 40.00}
	default:
		return Pricing{InputPerMillion: 0, OutputPerMillion: 0}
	}
}

func getAnthropicPricing(model string) Pricing {
	switch {
	case strings.Contains(model, "opus"):
		return Pricing{InputPerMillion: 15.00, OutputPerMillion: 75.00}
	case strings.Contains(model, "sonnet"):
		return Pricing{InputPerMillion: 3.00, OutputPerMillion: 15.00}
	case strings.Contains(model, "haiku"):
		return Pricing{InputPerMillion: 0.25, OutputPerMillion: 1.25}
	default:
		return Pricing{InputPerMillion: 0, OutputPerMillion: 0}
	}
}

func getGeminiPricing(model string) Pricing {
	switch {
	case strings.Contains(model, "pro"):
		return Pricing{InputPerMillion: 1.25, OutputPerMillion: 5.00}
	case strings.Contains(model, "flash"):
		return Pricing{InputPerMillion: 0.075, OutputPerMillion: 0.30}
	default:
		return Pricing{InputPerMillion: 0, OutputPerMillion: 0}
	}
}

// String returns a human-readable summary of the metrics.
func (m *Metrics) String() string {
	cost := m.Cost()
	costStr := "N/A"
	if cost > 0 {
		costStr = fmt.Sprintf("$%.4f", cost)
	} else if m.Provider == "ollama" {
		costStr = "$0.00 (local)"
	}

	return fmt.Sprintf(
		"Duration: %s | Iterations: %d | Tokens: %d in / %d out (%d total) | Cost: %s",
		m.Duration().Round(time.Millisecond),
		m.Iterations,
		m.InputTokens,
		m.OutputTokens,
		m.TotalTokens(),
		costStr,
	)
}

// Summary returns a formatted multi-line summary for display.
func (m *Metrics) Summary() string {
	cost := m.Cost()
	var costLine string
	switch {
	case cost > 0:
		costLine = fmt.Sprintf("  Cost:       $%.4f", cost)
	case m.Provider == "ollama":
		costLine = "  Cost:       $0.00 (local)"
	default:
		costLine = "  Cost:       N/A (unknown pricing)"
	}

	return fmt.Sprintf(`
Agent Metrics
─────────────
  Duration:   %s
  Iterations: %d
  Model:      %s (%s)
  Tokens:     %d input / %d output (%d total)
%s
`,
		m.Duration().Round(time.Millisecond),
		m.Iterations,
		m.Model,
		m.Provider,
		m.InputTokens,
		m.OutputTokens,
		m.TotalTokens(),
		costLine,
	)
}
