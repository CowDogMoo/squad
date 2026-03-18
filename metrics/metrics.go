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
	fetchTimeout      = 10 * time.Second
)

// ErrBudgetExceeded is returned when the configured cost budget is exhausted.
var ErrBudgetExceeded = fmt.Errorf("cost budget exceeded")

type ChildMetrics struct {
	Agent        string
	InputTokens  int64
	OutputTokens int64
	Model        string
	Provider     string
}

// Metrics tracks resource usage for an agent run.
// All exported methods are safe for concurrent use.
type Metrics struct {
	StartTime time.Time
	EndTime   time.Time
	Model     string
	Provider  string
	MaxCost   float64

	mu           sync.Mutex
	inputTokens  int64
	outputTokens int64
	iterations   int
	Children     []ChildMetrics
}

// New returns a Metrics instance with its start time set to now.
func New(provider, model string) *Metrics {
	return &Metrics{
		StartTime: time.Now(),
		Provider:  provider,
		Model:     model,
	}
}

// SetMaxCost sets the maximum total cost budget in USD for the run.
func (m *Metrics) SetMaxCost(maxCost float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.MaxCost = maxCost
}

// BudgetExceeded reports whether the total run cost has reached MaxCost.
func (m *Metrics) BudgetExceeded() bool {
	m.mu.Lock()
	maxCost := m.MaxCost
	m.mu.Unlock()
	return maxCost > 0 && m.TotalCostWithChildren() >= maxCost
}

// AddTokens adds input and output token counts to the run totals.
func (m *Metrics) AddTokens(input, output int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputTokens += input
	m.outputTokens += output
}

// IncrementIterations increments the recorded model iteration count.
func (m *Metrics) IncrementIterations() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.iterations++
}

// InputTokens returns the current input token count.
func (m *Metrics) InputTokens() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inputTokens
}

// OutputTokens returns the current output token count.
func (m *Metrics) OutputTokens() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputTokens
}

// Iterations returns the current iteration count.
func (m *Metrics) Iterations() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.iterations
}

// AddChild records usage from a child agent run in the parent metrics.
func (m *Metrics) AddChild(agent string, child *Metrics) {
	if child == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Children = append(m.Children, ChildMetrics{
		Agent:        agent,
		InputTokens:  child.InputTokens(),
		OutputTokens: child.OutputTokens(),
		Model:        child.Model,
		Provider:     child.Provider,
	})
}

// TotalCostWithChildren returns the estimated cost including child runs.
func (m *Metrics) TotalCostWithChildren() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := m.costLocked()
	for _, c := range m.Children {
		pricing := GetPricing(c.Provider, c.Model)
		inputCost := float64(c.InputTokens) / 1_000_000 * pricing.InputPerMillion
		outputCost := float64(c.OutputTokens) / 1_000_000 * pricing.OutputPerMillion
		total += inputCost + outputCost
	}
	return total
}

// TotalTokensWithChildren returns total tokens for the run and child runs.
func (m *Metrics) TotalTokensWithChildren() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := m.inputTokens + m.outputTokens
	for _, c := range m.Children {
		total += c.InputTokens + c.OutputTokens
	}
	return total
}

// Finish records the end time for the run.
func (m *Metrics) Finish() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EndTime = time.Now()
}

// Duration returns the elapsed time for the run.
func (m *Metrics) Duration() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.EndTime.IsZero() {
		return time.Since(m.StartTime)
	}
	return m.EndTime.Sub(m.StartTime)
}

// TotalTokens returns the total input and output tokens for the run.
func (m *Metrics) TotalTokens() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inputTokens + m.outputTokens
}

// Cost calculates the estimated cost in USD based on model pricing.
func (m *Metrics) Cost() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.costLocked()
}

// costLocked calculates cost; caller must hold m.mu.
func (m *Metrics) costLocked() float64 {
	pricing := GetPricing(m.Provider, m.Model)
	inputCost := float64(m.inputTokens) / 1_000_000 * pricing.InputPerMillion
	outputCost := float64(m.outputTokens) / 1_000_000 * pricing.OutputPerMillion
	return inputCost + outputCost
}

// Pricing holds per-million-token pricing for a model.
type Pricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

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

// PricingStatus reports whether pricing data was loaded successfully.
func PricingStatus() (bool, int, error) {
	ensurePricingLoaded()

	pricingCacheMu.RLock()
	defer pricingCacheMu.RUnlock()

	return pricingFetched, len(pricingCache), pricingFetchErr
}

func ensurePricingLoaded() {
	pricingFetchOnce.Do(fetchPricing)
}

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

// GetPricing returns pricing information for the given provider and model.
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

	return Pricing{InputPerMillion: 0, OutputPerMillion: 0}
}

// String returns a one-line summary of the recorded metrics.
func (m *Metrics) String() string {
	cost := m.Cost()
	costStr := m.costString(cost)
	in := m.InputTokens()
	out := m.OutputTokens()

	return fmt.Sprintf(
		"Duration: %s | Iterations: %d | Tokens: %d in / %d out (%d total) | Cost: %s",
		m.Duration().Round(time.Millisecond),
		m.Iterations(),
		in,
		out,
		in+out,
		costStr,
	)
}

// Summary returns a multi-line summary of the recorded metrics.
func (m *Metrics) Summary() string {
	cost := m.Cost()
	costLine := "  Cost:       " + m.costString(cost)
	in := m.InputTokens()
	out := m.OutputTokens()
	total := in + out

	var warningLine string
	loaded, _, err := PricingStatus()
	if !loaded && err != nil && strings.ToLower(m.Provider) != "ollama" {
		warningLine = fmt.Sprintf("\n  ⚠ Pricing unavailable: %v", err)
	}

	base := fmt.Sprintf(`
Agent Metrics
─────────────
  Duration:   %s
  Iterations: %d
  Model:      %s (%s)
  Tokens:     %d input / %d output (%d total)
%s%s
`,
		m.Duration().Round(time.Millisecond),
		m.Iterations(),
		m.Model,
		m.Provider,
		in,
		out,
		total,
		costLine,
		warningLine,
	)

	m.mu.Lock()
	children := make([]ChildMetrics, len(m.Children))
	copy(children, m.Children)
	m.mu.Unlock()

	if len(children) == 0 {
		return base
	}

	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("Child Agent Costs\n")
	sb.WriteString("─────────────────\n")

	var totalChildTokens int64
	var totalChildCost float64
	for _, c := range children {
		tokens := c.InputTokens + c.OutputTokens
		pricing := GetPricing(c.Provider, c.Model)
		childCost := float64(c.InputTokens)/1_000_000*pricing.InputPerMillion +
			float64(c.OutputTokens)/1_000_000*pricing.OutputPerMillion
		totalChildTokens += tokens
		totalChildCost += childCost
		costStr := "N/A"
		if childCost > 0 {
			costStr = fmt.Sprintf("$%.4f", childCost)
		}
		fmt.Fprintf(&sb, "  %-20s %10d tokens  %s\n", c.Agent, tokens, costStr)
	}

	grandTotal := cost + totalChildCost
	grandTokens := total + totalChildTokens
	fmt.Fprintf(&sb, "\n  %-20s %10d tokens  $%.4f\n", "TOTAL (all agents)", grandTokens, grandTotal)

	return sb.String()
}

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
