// Package metrics provides token usage tracking and cost analysis for agent runs.
package metrics

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/cowdogmoo/squad/logging"
)

const (
	// LiteLLM maintains a comprehensive pricing database updated by the community.
	liteLLMPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	fetchTimeout      = 10 * time.Second
)

// ErrBudgetExceeded is returned when the configured cost budget is exhausted.
var ErrBudgetExceeded = fmt.Errorf("cost budget exceeded")

// ChildMetrics holds token usage from a single child agent run,
// recorded in the parent [Metrics] by [Metrics.AddChild].
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

// BudgetUsedPct returns the fraction of the cost budget consumed (0.0–1.0).
// Returns 0 when no budget is set.
func (m *Metrics) BudgetUsedPct() float64 {
	m.mu.Lock()
	maxCost := m.MaxCost
	m.mu.Unlock()
	if maxCost <= 0 {
		return 0
	}
	pct := m.TotalCostWithChildren() / maxCost
	if pct > 1 {
		return 1
	}
	return pct
}

// RemainingBudget returns the remaining cost budget in USD.
func (m *Metrics) RemainingBudget() float64 {
	m.mu.Lock()
	maxCost := m.MaxCost
	m.mu.Unlock()
	if maxCost <= 0 {
		return 0
	}
	remaining := maxCost - m.TotalCostWithChildren()
	if remaining < 0 {
		return 0
	}
	return remaining
}

// MaxCostValue returns the configured maximum cost budget.
func (m *Metrics) MaxCostValue() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.MaxCost
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
	Mode               string  `json:"mode"`
}

var (
	pricingCache     map[string]liteLLMModel
	pricingCacheMu   sync.RWMutex
	pricingFetched   bool
	pricingFetchErr  error
	pricingFetchOnce sync.Once
)

// SupportedProviders is the canonical, ordered list of provider names
// squad recognizes for the --provider flag and the TUI launch form.
// Keep this in sync with the provider dispatch in runner/model.go.
var SupportedProviders = []string{
	"openai",
	"openai-responses",
	"anthropic",
	"gemini",
	"ollama",
	"nvidia",
	"databricks",
}

// providerMappings maps a squad provider name to the LiteLLM
// `litellm_provider` keys that supply its models. The first entry is
// treated as the canonical source when building model lists.
var providerMappings = map[string][]string{
	"openai":           {"openai", "azure", "azure_ai"},
	"openai-responses": {"openai", "azure"},
	"anthropic":        {"anthropic", "bedrock", "vertex_ai"},
	"gemini":           {"gemini", "vertex_ai", "vertex_ai-language-models"},
	"nvidia":           {"nvidia"},
	"databricks":       {"databricks"},
}

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

// WarmPricing kicks off the LiteLLM pricing fetch in the background.
// Subsequent calls are no-ops (sync.Once ensures the fetch runs at most
// once). Call at app startup so the model typeahead has live data by
// the time a user opens the launch form.
func WarmPricing() {
	go ensurePricingLoaded()
}

//go:embed fallback_models.yaml
var fallbackModelsYAML []byte

var (
	fallbackModels   map[string][]string
	fallbackOnce     sync.Once
	fallbackParseErr error
)

func loadFallbackModels() {
	if err := yaml.Unmarshal(fallbackModelsYAML, &fallbackModels); err != nil {
		fallbackParseErr = err
		logging.Warn("failed to parse fallback model list: %v", err)
	}
}

// ModelsForProvider returns a sorted, deduped list of known model names
// for `provider`. When the live LiteLLM cache is loaded the list is
// derived from it (chat-mode entries only). Otherwise the embedded
// fallback list is returned. An empty `provider` yields the union
// across all known providers.
func ModelsForProvider(provider string) []string {
	provider = strings.ToLower(strings.TrimSpace(provider))

	pricingCacheMu.RLock()
	loaded := pricingFetched
	cache := pricingCache
	pricingCacheMu.RUnlock()

	if loaded && cache != nil {
		if live := liveModelsForProvider(cache, provider); len(live) > 0 {
			return live
		}
	}

	fallbackOnce.Do(loadFallbackModels)
	if provider == "" {
		var all []string
		seen := make(map[string]struct{})
		for _, models := range fallbackModels {
			for _, m := range models {
				if _, dup := seen[m]; dup {
					continue
				}
				seen[m] = struct{}{}
				all = append(all, m)
			}
		}
		sort.Strings(all)
		return all
	}
	return append([]string(nil), fallbackModels[provider]...)
}

// modelListingProviders names the canonical LiteLLM provider key for
// each squad provider when populating dropdowns. Narrower than
// providerMappings (which also includes proxy backends like bedrock /
// vertex_ai used during pricing lookup) — using only the canonical key
// here avoids surfacing cross-vendor models like bedrock-hosted cohere
// under "anthropic". Ollama maps to LiteLLM's "ollama_chat" key; users
// still need the model pulled locally, but at least the typeahead
// hints at popular options instead of going blank.
var modelListingProviders = map[string]string{
	"openai":           "openai",
	"openai-responses": "openai",
	"anthropic":        "anthropic",
	"gemini":           "gemini",
	"nvidia":           "nvidia",
	"databricks":       "databricks",
	"ollama":           "ollama_chat",
}

// liveModelsForProvider filters the LiteLLM cache to chat-completion
// models whose `litellm_provider` matches `provider`'s canonical key.
// Caller must not hold pricingCacheMu.
func liveModelsForProvider(cache map[string]liteLLMModel, provider string) []string {
	wanted := map[string]bool{}
	if provider == "" {
		for _, key := range modelListingProviders {
			wanted[key] = true
		}
	} else if key, ok := modelListingProviders[provider]; ok {
		wanted[key] = true
	} else {
		return nil
	}

	seen := make(map[string]struct{})
	for key, entry := range cache {
		if !isChatMode(entry.Mode) {
			continue
		}
		if !wanted[entry.LiteLLMProvider] {
			continue
		}
		name := key
		if i := strings.IndexByte(key, '/'); i >= 0 {
			name = key[i+1:]
		}
		if name == "" {
			continue
		}
		seen[name] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// isChatMode reports whether a LiteLLM entry is usable as a chat
// completion model. Empty mode is accepted because older entries omit
// the field and are still chat models in practice.
func isChatMode(mode string) bool {
	switch mode {
	case "", "chat", "responses":
		return true
	}
	return false
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
