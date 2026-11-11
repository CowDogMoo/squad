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

// Usage is a token count split into the classes providers bill at different
// rates. The four input-side classes are disjoint: a prompt token is counted
// in exactly one of them.
type Usage struct {
	// InputTokens is fresh input: prompt tokens neither read from nor
	// written to a prompt cache.
	InputTokens int64
	// CacheReadTokens is prompt tokens served from a prompt cache.
	CacheReadTokens int64
	// CacheWriteTokens is prompt tokens written to a prompt cache with the
	// provider's default lifetime (five minutes on Anthropic), or with a
	// lifetime the provider did not report.
	CacheWriteTokens int64
	// CacheWrite1hTokens is prompt tokens written to a one-hour prompt cache.
	CacheWrite1hTokens int64
	// OutputTokens is generated tokens.
	OutputTokens int64
}

// TotalInputTokens returns every prompt token: fresh input plus cache reads
// and cache writes.
func (u Usage) TotalInputTokens() int64 {
	return u.InputTokens + u.CacheReadTokens + u.TotalCacheWriteTokens()
}

// TotalCacheWriteTokens returns cache writes of every lifetime.
func (u Usage) TotalCacheWriteTokens() int64 {
	return u.CacheWriteTokens + u.CacheWrite1hTokens
}

// add returns the element-wise sum of u and o.
func (u Usage) add(o Usage) Usage {
	return Usage{
		InputTokens:        u.InputTokens + o.InputTokens,
		CacheReadTokens:    u.CacheReadTokens + o.CacheReadTokens,
		CacheWriteTokens:   u.CacheWriteTokens + o.CacheWriteTokens,
		CacheWrite1hTokens: u.CacheWrite1hTokens + o.CacheWrite1hTokens,
		OutputTokens:       u.OutputTokens + o.OutputTokens,
	}
}

// CostBreakdown is a cost in USD split by the component that incurred it.
type CostBreakdown struct {
	Input      float64
	CacheRead  float64
	CacheWrite float64
	Output     float64
}

// Total returns the sum of every component.
func (c CostBreakdown) Total() float64 {
	return c.Input + c.CacheRead + c.CacheWrite + c.Output
}

// ChildMetrics holds token usage from a single child agent run,
// recorded in the parent [Metrics] by [Metrics.AddChild].
//
// InputTokens is every prompt token the child sent. The cache fields say how
// many of those were cache reads and cache writes, so they are never larger
// than InputTokens.
type ChildMetrics struct {
	Agent              string
	InputTokens        int64
	CacheReadTokens    int64
	CacheWriteTokens   int64
	CacheWrite1hTokens int64
	OutputTokens       int64
	Iterations         int
	Model              string
	Provider           string
}

// Usage returns the child's tokens split by billing class.
func (c ChildMetrics) Usage() Usage {
	fresh := c.InputTokens - c.CacheReadTokens - c.CacheWriteTokens - c.CacheWrite1hTokens
	if fresh < 0 {
		fresh = 0
	}
	return Usage{
		InputTokens:        fresh,
		CacheReadTokens:    c.CacheReadTokens,
		CacheWriteTokens:   c.CacheWriteTokens,
		CacheWrite1hTokens: c.CacheWrite1hTokens,
		OutputTokens:       c.OutputTokens,
	}
}

// Cost returns the child's estimated cost in USD at its own model's rates.
func (c ChildMetrics) Cost() float64 {
	return GetPricing(c.Provider, c.Model).Cost(c.Usage()).Total()
}

// Metrics tracks resource usage for an agent run.
// All exported methods are safe for concurrent use.
type Metrics struct {
	StartTime time.Time
	EndTime   time.Time
	Model     string
	Provider  string
	MaxCost   float64

	mu         sync.Mutex
	usage      Usage
	iterations int
	Children   []ChildMetrics
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

// AddTokens adds fresh input and output token counts to the run totals.
// Providers that report prompt-cache counts use [Metrics.AddUsage] instead so
// the cached share is priced at its own rate.
func (m *Metrics) AddTokens(input, output int64) {
	m.AddUsage(Usage{InputTokens: input, OutputTokens: output})
}

// AddUsage adds token counts split by billing class to the run totals.
func (m *Metrics) AddUsage(u Usage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage = m.usage.add(u)
}

// Usage returns the run's own token counts split by billing class.
func (m *Metrics) Usage() Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage
}

// IncrementIterations increments the recorded model iteration count.
func (m *Metrics) IncrementIterations() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.iterations++
}

// AddIterations adds n iterations to the run total. Agentic CLI providers
// report the whole run's turn count at once rather than incrementing per
// loop pass; non-positive n is ignored.
func (m *Metrics) AddIterations(n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.iterations += n
}

// InputTokens returns every prompt token sent so far: fresh input plus cache
// reads and cache writes. [Metrics.Usage] splits the total by billing class.
func (m *Metrics) InputTokens() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage.TotalInputTokens()
}

// OutputTokens returns the current output token count.
func (m *Metrics) OutputTokens() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage.OutputTokens
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
	u := child.Usage()
	m.Children = append(m.Children, ChildMetrics{
		Agent:              agent,
		InputTokens:        u.TotalInputTokens(),
		CacheReadTokens:    u.CacheReadTokens,
		CacheWriteTokens:   u.CacheWriteTokens,
		CacheWrite1hTokens: u.CacheWrite1hTokens,
		OutputTokens:       u.OutputTokens,
		Iterations:         child.Iterations(),
		Model:              child.Model,
		Provider:           child.Provider,
	})
}

// TotalCostWithChildren returns the estimated cost including child runs.
func (m *Metrics) TotalCostWithChildren() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := m.costLocked()
	for _, c := range m.Children {
		total += c.Cost()
	}
	return total
}

// TotalTokensWithChildren returns total tokens for the run and child runs.
func (m *Metrics) TotalTokensWithChildren() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := m.usage.TotalInputTokens() + m.usage.OutputTokens
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
	return m.usage.TotalInputTokens() + m.usage.OutputTokens
}

// Cost calculates the estimated cost in USD based on model pricing.
func (m *Metrics) Cost() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.costLocked()
}

// CostBreakdown returns the run's own estimated cost in USD, split into
// input, cache read, cache write, and output. Child runs are not included.
func (m *Metrics) CostBreakdown() CostBreakdown {
	m.mu.Lock()
	defer m.mu.Unlock()
	return GetPricing(m.Provider, m.Model).Cost(m.usage)
}

// costLocked calculates cost; caller must hold m.mu.
func (m *Metrics) costLocked() float64 {
	return GetPricing(m.Provider, m.Model).Cost(m.usage).Total()
}

// Pricing holds per-million-token pricing for a model.
type Pricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
	// CacheReadPerMillion prices prompt tokens served from a prompt cache.
	CacheReadPerMillion float64
	// CacheWritePerMillion prices prompt tokens written to a prompt cache
	// with the default (five-minute) lifetime.
	CacheWritePerMillion float64
	// CacheWrite1hPerMillion prices prompt tokens written to a one-hour
	// prompt cache.
	CacheWrite1hPerMillion float64
}

// Cost prices u, charging each class of token at its own rate.
func (p Pricing) Cost(u Usage) CostBreakdown {
	perToken := func(tokens int64, perMillion float64) float64 {
		return float64(tokens) / 1_000_000 * perMillion
	}
	return CostBreakdown{
		Input:     perToken(u.InputTokens, p.InputPerMillion),
		CacheRead: perToken(u.CacheReadTokens, p.CacheReadPerMillion),
		CacheWrite: perToken(u.CacheWriteTokens, p.CacheWritePerMillion) +
			perToken(u.CacheWrite1hTokens, p.CacheWrite1hPerMillion),
		Output: perToken(u.OutputTokens, p.OutputPerMillion),
	}
}

// Anthropic bills prompt-cache tokens as a multiple of the model's base input
// price. These are the multipliers from Anthropic's pricing documentation,
// https://platform.claude.com/docs/en/about-claude/pricing#prompt-caching
// (read 2026-11-11): a five-minute cache write costs 1.25x, a one-hour cache
// write costs 2x, and a cache read costs 0.1x on most models. The page prices
// cache reads lower on some newer models; those prices reach squad through
// the models' LiteLLM entries.
//
// They are the fallback for a Claude model whose LiteLLM entry carries no
// cache prices; an entry's own per-model cache prices take precedence.
const (
	anthropicCacheWrite5mMultiplier = 1.25
	anthropicCacheWrite1hMultiplier = 2.0
	anthropicCacheReadMultiplier    = 0.1
)

// isAnthropicModel reports whether provider and model (both lower-cased)
// name a Claude model, whichever platform serves it.
func isAnthropicModel(provider, model string) bool {
	return provider == "anthropic" || provider == "claude-code" || strings.Contains(model, "claude")
}

// pricingFromEntry converts a LiteLLM entry to per-million prices. Cache
// prices come from the entry when it has them. Otherwise a Claude model gets
// Anthropic's documented multiples of its input price, and any other model is
// charged the plain input price for cached tokens.
func pricingFromEntry(provider, model string, entry liteLLMModel) Pricing {
	p := Pricing{
		InputPerMillion:        entry.InputCostPerToken * 1_000_000,
		OutputPerMillion:       entry.OutputCostPerToken * 1_000_000,
		CacheReadPerMillion:    entry.CacheReadInputTokenCost * 1_000_000,
		CacheWritePerMillion:   entry.CacheCreationInputTokenCost * 1_000_000,
		CacheWrite1hPerMillion: entry.CacheCreationInputTokenCost1h * 1_000_000,
	}
	anthropic := isAnthropicModel(provider, model)
	if p.CacheReadPerMillion <= 0 {
		p.CacheReadPerMillion = p.InputPerMillion
		if anthropic {
			p.CacheReadPerMillion = p.InputPerMillion * anthropicCacheReadMultiplier
		}
	}
	if p.CacheWritePerMillion <= 0 {
		p.CacheWritePerMillion = p.InputPerMillion
		if anthropic {
			p.CacheWritePerMillion = p.InputPerMillion * anthropicCacheWrite5mMultiplier
		}
	}
	if p.CacheWrite1hPerMillion <= 0 {
		p.CacheWrite1hPerMillion = p.CacheWritePerMillion
		if anthropic {
			p.CacheWrite1hPerMillion = p.InputPerMillion * anthropicCacheWrite1hMultiplier
		}
	}
	return p
}

type liteLLMModel struct {
	InputCostPerToken             float64 `json:"input_cost_per_token"`
	OutputCostPerToken            float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost       float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost   float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCost1h float64 `json:"cache_creation_input_token_cost_above_1hr"`
	LiteLLMProvider               string  `json:"litellm_provider"`
	Mode                          string  `json:"mode"`
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
	"openai-compat",
	"claude-code",
	"antigravity",
}

// providerMappings maps a squad provider name to the LiteLLM
// `litellm_provider` keys that supply its models. The first entry is
// treated as the canonical source when building model lists.
var providerMappings = map[string][]string{
	"openai":           {"openai", "azure", "azure_ai"},
	"openai-responses": {"openai", "azure"},
	"anthropic":        {"anthropic", "bedrock", "vertex_ai"},
	"gemini":           {"gemini", "vertex_ai", "vertex_ai-language-models"},
	"openai-compat":    {"openai"},
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
//
// "openai-compat" is intentionally absent: it targets arbitrary third-party
// endpoints (DeepInfra, vLLM, NIM, etc.) that have no shared model catalog,
// so dynamic listing is not supported.
var modelListingProviders = map[string]string{
	"openai":           "openai",
	"openai-responses": "openai",
	"anthropic":        "anthropic",
	"gemini":           "gemini",
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
		return pricingFromEntry(provider, model, entry), true
	}

	// Try just the model name (many entries don't have provider prefix)
	if entry, ok := pricingCache[model]; ok {
		return pricingFromEntry(provider, model, entry), true
	}

	if prefixes, ok := providerMappings[provider]; ok {
		for _, prefix := range prefixes {
			if entry, found := pricingCache[prefix+"/"+model]; found {
				return pricingFromEntry(provider, model, entry), true
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
		return Pricing{}
	}

	// Try LiteLLM lookup
	if pricing, found := lookupLiteLLMPricing(provider, model); found {
		return pricing
	}

	return Pricing{}
}

// String returns a one-line summary of the recorded metrics.
func (m *Metrics) String() string {
	cost := m.Cost()
	costStr := m.costString(cost)
	u := m.Usage()
	in := u.TotalInputTokens()
	out := u.OutputTokens

	tokens := fmt.Sprintf("%d in / %d out (%d total)", in, out, in+out)
	if hasCacheTokens(u) {
		tokens += fmt.Sprintf(" | Input: %s", inputComponents(u))
	}
	return fmt.Sprintf(
		"Duration: %s | Iterations: %d | Tokens: %s | Cost: %s",
		m.Duration().Round(time.Millisecond),
		m.Iterations(),
		tokens,
		costStr,
	)
}

// hasCacheTokens reports whether any of u's prompt tokens went through a
// prompt cache, which is when the input and cost components are worth
// spelling out.
func hasCacheTokens(u Usage) bool {
	return u.CacheReadTokens > 0 || u.TotalCacheWriteTokens() > 0
}

// inputComponents names the classes that make up u's prompt tokens.
func inputComponents(u Usage) string {
	return fmt.Sprintf("%d input / %d cache read / %d cache write",
		u.InputTokens, u.CacheReadTokens, u.TotalCacheWriteTokens())
}

// costComponents names the classes that make up a cost.
func costComponents(b CostBreakdown) string {
	return fmt.Sprintf("input $%.4f / cache read $%.4f / cache write $%.4f / output $%.4f",
		b.Input, b.CacheRead, b.CacheWrite, b.Output)
}

// UsageLogLine returns the run's own tokens and cost with every component
// named, for a single log line.
func (m *Metrics) UsageLogLine() string {
	u := m.Usage()
	b := m.CostBreakdown()
	return fmt.Sprintf("tokens: input=%d cache_read=%d cache_write=%d output=%d; cost: input=$%.4f cache_read=$%.4f cache_write=$%.4f output=$%.4f total=$%.4f",
		u.InputTokens, u.CacheReadTokens, u.TotalCacheWriteTokens(), u.OutputTokens,
		b.Input, b.CacheRead, b.CacheWrite, b.Output, b.Total())
}

// ProgressTokens returns the run's own token counts for a per-iteration log
// line as "tokens=<input>/<output>", where input is every prompt token. The
// share of that input read from or written to a prompt cache follows only
// when it is non-zero, so a run that never used the cache logs the short form.
func (m *Metrics) ProgressTokens() string {
	u := m.Usage()
	s := fmt.Sprintf("tokens=%d/%d", u.TotalInputTokens(), u.OutputTokens)
	if u.CacheReadTokens > 0 {
		s += fmt.Sprintf(", cache_read=%d", u.CacheReadTokens)
	}
	if w := u.TotalCacheWriteTokens(); w > 0 {
		s += fmt.Sprintf(", cache_write=%d", w)
	}
	return s
}

// Summary returns a multi-line summary of the recorded metrics.
func (m *Metrics) Summary() string {
	cost := m.Cost()
	usage := m.Usage()
	breakdown := m.CostBreakdown()
	in := usage.TotalInputTokens()
	out := usage.OutputTokens
	total := in + out

	m.mu.Lock()
	children := make([]ChildMetrics, len(m.Children))
	copy(children, m.Children)
	m.mu.Unlock()

	// Header display values. When the parent ran no model calls itself but has
	// children (a pure aggregator, e.g. a sharded run where every model call
	// happens in a shard), the header would otherwise read 0 iterations / 0
	// tokens / $0. Fall back to the children sum so the top line reflects the
	// run. The per-child breakdown and "TOTAL (all agents)" line below stay
	// computed from the parent's own counters + children, so no double-count.
	dispIn, dispOut, dispIters, dispCost := in, out, m.Iterations(), cost
	dispUsage, dispBreakdown := usage, breakdown
	if total == 0 && len(children) > 0 {
		for _, c := range children {
			dispIn += c.InputTokens
			dispOut += c.OutputTokens
			dispIters += c.Iterations
			dispUsage = dispUsage.add(c.Usage())
			cb := GetPricing(c.Provider, c.Model).Cost(c.Usage())
			dispBreakdown.Input += cb.Input
			dispBreakdown.CacheRead += cb.CacheRead
			dispBreakdown.CacheWrite += cb.CacheWrite
			dispBreakdown.Output += cb.Output
		}
		dispCost = m.TotalCostWithChildren()
	}
	costLine := "  Cost:       " + m.costString(dispCost)
	// Cached prompt tokens are billed at their own rates, so when a run used
	// the prompt cache, name the components behind both totals.
	var inputLine string
	if hasCacheTokens(dispUsage) {
		inputLine = "  Input:      " + inputComponents(dispUsage) + "\n"
		if dispCost > 0 {
			costLine += "\n  Cost split: " + costComponents(dispBreakdown)
		}
	}

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
%s%s%s
`,
		m.Duration().Round(time.Millisecond),
		dispIters,
		m.Model,
		m.Provider,
		dispIn,
		dispOut,
		dispIn+dispOut,
		inputLine,
		costLine,
		warningLine,
	)

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
		childCost := c.Cost()
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
	case IsAgenticCLI(m.Provider):
		return "$0.00 (subscription)"
	default:
		return "N/A (pricing not available yet)"
	}
}
