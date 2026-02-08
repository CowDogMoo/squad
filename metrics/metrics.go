// Package metrics provides token usage tracking and cost analysis for agent runs.
package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cowdogmoo/squad/logging"
)

const (
	// LiteLLM maintains a comprehensive pricing database updated by the community.
	liteLLMPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	// Timeout for fetching pricing data.
	fetchTimeout = 10 * time.Second
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

// liteLLMModel represents a model entry from the LiteLLM pricing JSON.
type liteLLMModel struct {
	InputCostPerToken  float64 `json:"input_cost_per_token"`
	OutputCostPerToken float64 `json:"output_cost_per_token"`
	LiteLLMProvider    string  `json:"litellm_provider"`
}

var (
	pricingCache     map[string]liteLLMModel
	pricingCacheMu   sync.RWMutex
	pricingFetched   bool
	pricingFetchErr  error
	pricingFetchOnce sync.Once
)

// fetchPricing downloads the LiteLLM pricing database.
func fetchPricing() {
	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Get(liteLLMPricingURL)
	if err != nil {
		pricingCacheMu.Lock()
		pricingFetchErr = fmt.Errorf("failed to fetch pricing data: %w", err)
		pricingCacheMu.Unlock()
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logging.Warn("failed to close pricing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		pricingCacheMu.Lock()
		pricingFetchErr = fmt.Errorf("pricing API returned status %d", resp.StatusCode)
		pricingCacheMu.Unlock()
		return
	}

	var data map[string]liteLLMModel
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		pricingCacheMu.Lock()
		pricingFetchErr = fmt.Errorf("failed to parse pricing data: %w", err)
		pricingCacheMu.Unlock()
		return
	}

	pricingCacheMu.Lock()
	pricingCache = data
	pricingFetched = true
	pricingFetchErr = nil
	pricingCacheMu.Unlock()
}

// PricingStatus returns whether pricing data was loaded and any error that occurred.
// Returns (loaded, modelCount, error).
func PricingStatus() (bool, int, error) {
	ensurePricingLoaded()

	pricingCacheMu.RLock()
	defer pricingCacheMu.RUnlock()

	return pricingFetched, len(pricingCache), pricingFetchErr
}

// ensurePricingLoaded triggers a one-time fetch of pricing data.
func ensurePricingLoaded() {
	pricingFetchOnce.Do(fetchPricing)
}

// lookupLiteLLMPricing searches the LiteLLM cache for a model.
func lookupLiteLLMPricing(provider, model string) (Pricing, bool) {
	ensurePricingLoaded()

	pricingCacheMu.RLock()
	defer pricingCacheMu.RUnlock()

	if !pricingFetched || pricingCache == nil {
		return Pricing{}, false
	}

	// Try exact match with provider prefix (e.g., "openai/gpt-4o")
	if entry, ok := pricingCache[provider+"/"+model]; ok {
		return Pricing{
			InputPerMillion:  entry.InputCostPerToken * 1_000_000,
			OutputPerMillion: entry.OutputCostPerToken * 1_000_000,
		}, true
	}

	// Try just the model name (many entries don't have provider prefix)
	if entry, ok := pricingCache[model]; ok {
		return Pricing{
			InputPerMillion:  entry.InputCostPerToken * 1_000_000,
			OutputPerMillion: entry.OutputCostPerToken * 1_000_000,
		}, true
	}

	// Try with common provider mappings
	providerMappings := map[string][]string{
		"openai":           {"openai", "azure", "azure_ai"},
		"openai-responses": {"openai", "azure"},
		"anthropic":        {"anthropic", "bedrock", "vertex_ai"},
		"gemini":           {"gemini", "vertex_ai", "vertex_ai-language-models"},
	}

	if prefixes, ok := providerMappings[provider]; ok {
		for _, prefix := range prefixes {
			if entry, found := pricingCache[prefix+"/"+model]; found {
				return Pricing{
					InputPerMillion:  entry.InputCostPerToken * 1_000_000,
					OutputPerMillion: entry.OutputCostPerToken * 1_000_000,
				}, true
			}
		}
	}

	return Pricing{}, false
}

// GetPricing returns the pricing for a given provider and model.
// Pricing is fetched from LiteLLM's community-maintained database.
// Returns zero pricing if the model is not found (displays as "pricing not available yet").
func GetPricing(provider, model string) Pricing {
	model = strings.ToLower(model)
	provider = strings.ToLower(provider)

	// Ollama is always free (local inference)
	if provider == "ollama" {
		return Pricing{InputPerMillion: 0, OutputPerMillion: 0}
	}

	// Try LiteLLM lookup
	if pricing, found := lookupLiteLLMPricing(provider, model); found {
		return pricing
	}

	// Model not found in LiteLLM database
	return Pricing{InputPerMillion: 0, OutputPerMillion: 0}
}

// String returns a human-readable summary of the metrics.
func (m *Metrics) String() string {
	cost := m.Cost()
	costStr := m.costString(cost)

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
	costLine := "  Cost:       " + m.costString(cost)

	// Check if pricing fetch failed
	var warningLine string
	loaded, _, err := PricingStatus()
	if !loaded && err != nil && strings.ToLower(m.Provider) != "ollama" {
		warningLine = fmt.Sprintf("\n  ⚠ Pricing unavailable: %v", err)
	}

	return fmt.Sprintf(`
Agent Metrics
─────────────
  Duration:   %s
  Iterations: %d
  Model:      %s (%s)
  Tokens:     %d input / %d output (%d total)
%s%s
`,
		m.Duration().Round(time.Millisecond),
		m.Iterations,
		m.Model,
		m.Provider,
		m.InputTokens,
		m.OutputTokens,
		m.TotalTokens(),
		costLine,
		warningLine,
	)
}

// costString returns a formatted cost string.
func (m *Metrics) costString(cost float64) string {
	switch {
	case cost > 0:
		return fmt.Sprintf("$%.4f", cost)
	case strings.ToLower(m.Provider) == "ollama":
		return "$0.00 (local)"
	default:
		return "N/A (pricing not available yet)"
	}
}
