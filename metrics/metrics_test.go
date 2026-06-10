package metrics

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc struct {
	roundTrip func(*http.Request) (*http.Response, error)
}

func (rt roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt.roundTrip(req)
}

func getPricingState() (map[string]liteLLMModel, bool, error) {
	pricingCacheMu.RLock()
	defer pricingCacheMu.RUnlock()

	return pricingCache, pricingFetched, pricingFetchErr
}

func setPricingState(cache map[string]liteLLMModel, fetched bool, fetchErr error) {
	pricingCacheMu.Lock()
	pricingCache = cache
	pricingFetched = fetched
	pricingFetchErr = fetchErr
	pricingCacheMu.Unlock()
}

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
	if m.InputTokens() != 0 {
		t.Fatalf("InputTokens = %d, want 0", m.InputTokens())
	}
	if m.OutputTokens() != 0 {
		t.Fatalf("OutputTokens = %d, want 0", m.OutputTokens())
	}
	if m.Iterations() != 0 {
		t.Fatalf("Iterations = %d, want 0", m.Iterations())
	}
}

func TestAddTokens(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")

	m.AddTokens(100, 50)
	if m.InputTokens() != 100 {
		t.Fatalf("InputTokens = %d, want 100", m.InputTokens())
	}
	if m.OutputTokens() != 50 {
		t.Fatalf("OutputTokens = %d, want 50", m.OutputTokens())
	}

	// Accumulate more tokens
	m.AddTokens(200, 100)
	if m.InputTokens() != 300 {
		t.Fatalf("InputTokens = %d, want 300", m.InputTokens())
	}
	if m.OutputTokens() != 150 {
		t.Fatalf("OutputTokens = %d, want 150", m.OutputTokens())
	}
}

func TestAddTokensZero(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	m.AddTokens(100, 50)

	// Adding zero should not change totals
	m.AddTokens(0, 0)
	if m.InputTokens() != 100 {
		t.Fatalf("InputTokens = %d, want 100", m.InputTokens())
	}
	if m.OutputTokens() != 50 {
		t.Fatalf("OutputTokens = %d, want 50", m.OutputTokens())
	}
}

func TestIncrementIterations(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")

	m.IncrementIterations()
	if m.Iterations() != 1 {
		t.Fatalf("Iterations = %d, want 1", m.Iterations())
	}

	m.IncrementIterations()
	m.IncrementIterations()
	if m.Iterations() != 3 {
		t.Fatalf("Iterations = %d, want 3", m.Iterations())
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
	// Requires network access.
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

	if m.Iterations() != 5 {
		t.Fatalf("Iterations = %d, want 5", m.Iterations())
	}
	if m.InputTokens() != 2500 {
		t.Fatalf("InputTokens = %d, want 2500", m.InputTokens())
	}
	if m.OutputTokens() != 1000 {
		t.Fatalf("OutputTokens = %d, want 1000", m.OutputTokens())
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
	// Requires network access.
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

func TestSetMaxCost(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	if m.MaxCost != 0 {
		t.Fatalf("MaxCost = %v, want 0", m.MaxCost)
	}
	m.SetMaxCost(1.50)
	if m.MaxCost != 1.50 {
		t.Fatalf("MaxCost = %v, want 1.50", m.MaxCost)
	}
}

func TestMaxCostValue(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	if got := m.MaxCostValue(); got != 0 {
		t.Errorf("MaxCostValue = %v, want 0", got)
	}
	m.SetMaxCost(2.75)
	if got := m.MaxCostValue(); got != 2.75 {
		t.Errorf("MaxCostValue = %v, want 2.75", got)
	}
}

func TestWarmPricingNoOp(t *testing.T) {
	// WarmPricing kicks off a background goroutine. Calling it twice should
	// be safe — sync.Once gates the fetch. We don't assert on the outcome
	// (it depends on network), only that the call is non-blocking.
	WarmPricing()
	WarmPricing()
}

func TestModelsForProviderFallback(t *testing.T) {
	// With no pricing data loaded yet (or fetch failed), ModelsForProvider
	// falls through to the embedded fallback list. Known providers should
	// return at least one model name.
	for _, provider := range []string{"openai", "anthropic", "gemini"} {
		got := ModelsForProvider(provider)
		if len(got) == 0 {
			t.Errorf("ModelsForProvider(%q) returned empty; expected fallback entries", provider)
		}
	}
}

func TestModelsForProviderEmptyReturnsUnion(t *testing.T) {
	// Empty provider yields the union across all fallback providers.
	got := ModelsForProvider("")
	if len(got) == 0 {
		t.Error("ModelsForProvider(\"\") should aggregate across all providers")
	}
	// Result is sorted; spot-check monotonic ordering.
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("union not sorted: %v", got)
			break
		}
	}
}

func TestModelsForProviderUnknownReturnsEmpty(t *testing.T) {
	got := ModelsForProvider("not-a-provider")
	if len(got) != 0 {
		t.Errorf("unknown provider should yield empty list, got %v", got)
	}
}

func TestBudgetExceeded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		maxCost float64
		want    bool
	}{
		{"zero max cost (unlimited)", 0, false},
		{"below budget", 100.0, false},
		{"above budget with ollama (free)", 0.01, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := New("ollama", "llama3")
			m.SetMaxCost(tt.maxCost)
			m.AddTokens(1_000_000, 1_000_000)
			if got := m.BudgetExceeded(); got != tt.want {
				t.Fatalf("BudgetExceeded() = %v, want %v (cost=%v)", got, tt.want, m.TotalCostWithChildren())
			}
		})
	}
}

func TestBudgetExceededWithPricing(t *testing.T) {
	m := New("ollama", "llama3")
	m.SetMaxCost(0.001)
	m.AddTokens(10_000_000, 10_000_000)
	if m.BudgetExceeded() {
		t.Fatalf("ollama should never exceed budget (cost is $0)")
	}

	m2 := New("openai", "gpt-4o")
	m2.SetMaxCost(0)
	m2.AddTokens(10_000_000, 10_000_000)
	if m2.BudgetExceeded() {
		t.Fatalf("zero MaxCost should mean unlimited")
	}

	if testing.Short() {
		return
	}
	m3 := New("openai", "gpt-4o")
	m3.SetMaxCost(0.0001)
	m3.AddTokens(1_000_000, 1_000_000)
	cost := m3.TotalCostWithChildren()
	if cost > 0 && !m3.BudgetExceeded() {
		t.Fatalf("should exceed $0.0001 budget with 1M tokens (cost=$%.4f)", cost)
	}
}

func TestErrBudgetExceeded(t *testing.T) {
	t.Parallel()
	if ErrBudgetExceeded == nil {
		t.Fatal("ErrBudgetExceeded should not be nil")
	}
	if !strings.Contains(ErrBudgetExceeded.Error(), "budget exceeded") {
		t.Fatalf("ErrBudgetExceeded = %q, want containing 'budget exceeded'", ErrBudgetExceeded.Error())
	}
}

func TestAddChildNil(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	m.AddChild("child1", nil)
	if len(m.Children) != 0 {
		t.Fatalf("AddChild(nil) should not add entry, got %d children", len(m.Children))
	}
}

func TestAddChildValid(t *testing.T) {
	t.Parallel()
	parent := New("openai", "gpt-4o")
	child := New("ollama", "llama3")
	child.AddTokens(500, 200)

	parent.AddChild("worker-1", child)

	if len(parent.Children) != 1 {
		t.Fatalf("Children = %d, want 1", len(parent.Children))
	}
	c := parent.Children[0]
	if c.Agent != "worker-1" {
		t.Fatalf("Agent = %q, want worker-1", c.Agent)
	}
	if c.InputTokens != 500 || c.OutputTokens != 200 {
		t.Fatalf("tokens = %d/%d, want 500/200", c.InputTokens, c.OutputTokens)
	}
	if c.Model != "llama3" || c.Provider != "ollama" {
		t.Fatalf("model/provider = %q/%q, want llama3/ollama", c.Model, c.Provider)
	}
}

func TestAddChildMultiple(t *testing.T) {
	t.Parallel()
	parent := New("openai", "gpt-4o")
	for i := 0; i < 3; i++ {
		child := New("ollama", "llama3")
		child.AddTokens(int64(100*(i+1)), int64(50*(i+1)))
		parent.AddChild(fmt.Sprintf("child-%d", i), child)
	}
	if len(parent.Children) != 3 {
		t.Fatalf("Children = %d, want 3", len(parent.Children))
	}
}

func TestTotalCostWithChildrenNoChildren(t *testing.T) {
	t.Parallel()
	m := New("ollama", "llama3")
	m.AddTokens(1000, 500)
	total := m.TotalCostWithChildren()
	if total != 0 {
		t.Fatalf("TotalCostWithChildren() = %v, want 0 for ollama", total)
	}
}

func TestTotalCostWithChildrenOllamaChildren(t *testing.T) {
	t.Parallel()
	parent := New("ollama", "llama3")
	parent.AddTokens(1000, 500)
	child := New("ollama", "llama3")
	child.AddTokens(2000, 1000)
	parent.AddChild("worker", child)

	total := parent.TotalCostWithChildren()
	if total != 0 {
		t.Fatalf("TotalCostWithChildren() = %v, want 0 for all-ollama", total)
	}
}

func TestTotalTokensWithChildrenNoChildren(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	m.AddTokens(100, 50)
	total := m.TotalTokensWithChildren()
	if total != 150 {
		t.Fatalf("TotalTokensWithChildren() = %d, want 150", total)
	}
}

func TestTotalTokensWithChildren(t *testing.T) {
	t.Parallel()
	parent := New("openai", "gpt-4o")
	parent.AddTokens(100, 50)

	child1 := New("ollama", "llama3")
	child1.AddTokens(200, 100)
	parent.AddChild("c1", child1)

	child2 := New("openai", "gpt-4o")
	child2.AddTokens(300, 150)
	parent.AddChild("c2", child2)

	total := parent.TotalTokensWithChildren()
	// parent: 150, child1: 300, child2: 450 = 900
	if total != 900 {
		t.Fatalf("TotalTokensWithChildren() = %d, want 900", total)
	}
}

func TestSummaryWithChildren(t *testing.T) {
	t.Parallel()
	parent := New("ollama", "llama3")
	parent.AddTokens(1000, 500)
	parent.IncrementIterations()
	parent.Finish()

	child := New("ollama", "llama3")
	child.AddTokens(2000, 1000)
	parent.AddChild("sub-agent", child)

	summary := parent.Summary()
	if !strings.Contains(summary, "Child Agent Costs") {
		t.Fatalf("Summary() missing Child Agent Costs section")
	}
	if !strings.Contains(summary, "sub-agent") {
		t.Fatalf("Summary() missing child agent name")
	}
	if !strings.Contains(summary, "TOTAL (all agents)") {
		t.Fatalf("Summary() missing TOTAL line")
	}
}

func TestBudgetExceededWithChildren(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pricing test in short mode")
	}
	parent := New("openai", "gpt-4o")
	parent.SetMaxCost(0.0001)
	// Parent has no tokens, but child has lots
	child := New("openai", "gpt-4o")
	child.AddTokens(1_000_000, 1_000_000)
	parent.AddChild("expensive-child", child)

	childCost := parent.TotalCostWithChildren()
	if childCost > 0 && !parent.BudgetExceeded() {
		t.Fatalf("BudgetExceeded() should be true when child cost ($%.4f) exceeds budget", childCost)
	}
}

func TestChildMetricsStruct(t *testing.T) {
	t.Parallel()
	cm := ChildMetrics{
		Agent:        "test-agent",
		InputTokens:  100,
		OutputTokens: 50,
		Model:        "gpt-4o",
		Provider:     "openai",
	}
	if cm.Agent != "test-agent" {
		t.Fatalf("Agent = %q, want test-agent", cm.Agent)
	}
	if cm.InputTokens+cm.OutputTokens != 150 {
		t.Fatalf("total tokens = %d, want 150", cm.InputTokens+cm.OutputTokens)
	}
}

func TestRemainingBudgetUnlimited(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	m.AddTokens(1000, 500)
	if m.RemainingBudget() != 0 {
		t.Fatalf("RemainingBudget() = %v, want 0 for unlimited", m.RemainingBudget())
	}
}

func TestRemainingBudgetWithCost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pricing test in short mode")
	}
	m := New("openai", "gpt-4o")
	m.SetMaxCost(1.00)
	m.AddTokens(100_000, 50_000) // should cost something

	remaining := m.RemainingBudget()
	cost := m.TotalCostWithChildren()
	if cost > 0 {
		if remaining >= 1.00 {
			t.Fatalf("RemainingBudget() = %v, should be less than MaxCost after spending", remaining)
		}
		if remaining+cost < 0.999 || remaining+cost > 1.001 {
			t.Fatalf("remaining (%v) + cost (%v) should equal MaxCost (1.00)", remaining, cost)
		}
	}
}

func TestRemainingBudgetExhausted(t *testing.T) {
	t.Parallel()
	m := New("ollama", "llama3")
	m.SetMaxCost(0.0001)
	// Ollama is free, so budget is never actually spent
	if m.RemainingBudget() != 0.0001 {
		t.Fatalf("RemainingBudget() = %v, want 0.0001", m.RemainingBudget())
	}
}

func TestRemainingBudgetWithChildren(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pricing test in short mode")
	}
	parent := New("openai", "gpt-4o")
	parent.SetMaxCost(10.00)

	child := New("openai", "gpt-4o")
	child.AddTokens(1_000_000, 500_000)
	parent.AddChild("expensive-child", child)

	remaining := parent.RemainingBudget()
	totalCost := parent.TotalCostWithChildren()
	if totalCost > 0 && remaining >= 10.00 {
		t.Fatalf("RemainingBudget() = %v should be less than MaxCost after child spending", remaining)
	}
}

func TestSummaryWithPricingWarning(t *testing.T) {
	// This test manipulates pricing globals -- do not add t.Parallel().
	// Trigger the sync.Once fetch first so it doesn't override our state.
	ensurePricingLoaded()

	originalCache, originalFetched, originalErr := getPricingState()
	t.Cleanup(func() {
		setPricingState(originalCache, originalFetched, originalErr)
	})

	// Simulate a failed pricing fetch for a non-ollama provider.
	setPricingState(nil, false, fmt.Errorf("network down"))

	m := New("openai", "gpt-4o")
	m.AddTokens(100, 50)
	m.IncrementIterations()
	m.Finish()

	summary := m.Summary()
	if !strings.Contains(summary, "Pricing unavailable") {
		t.Fatalf("Summary() should contain pricing warning when fetch failed, got:\n%s", summary)
	}
	if !strings.Contains(summary, "network down") {
		t.Fatalf("Summary() should include fetch error message, got:\n%s", summary)
	}
}

func TestSummaryOllamaNoPricingWarning(t *testing.T) {
	// Ollama should NOT show pricing warning even if fetch failed.
	// Trigger the sync.Once fetch first so it doesn't override our state.
	ensurePricingLoaded()

	originalCache, originalFetched, originalErr := getPricingState()
	t.Cleanup(func() {
		setPricingState(originalCache, originalFetched, originalErr)
	})

	setPricingState(nil, false, fmt.Errorf("network down"))

	m := New("ollama", "llama3")
	m.AddTokens(100, 50)
	m.Finish()

	summary := m.Summary()
	if strings.Contains(summary, "Pricing unavailable") {
		t.Fatalf("Summary() should NOT show pricing warning for ollama, got:\n%s", summary)
	}
}

func TestCostStringEdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider string
		cost     float64
		want     string
	}{
		{"tiny positive cost", "openai", 0.0001, "$0.0001"},
		{"Ollama with uppercase", "Ollama", 0, "$0.00 (local)"},
		{"OLLAMA all caps", "OLLAMA", 0, "$0.00 (local)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &Metrics{Provider: tt.provider}
			got := m.costString(tt.cost)
			if got != tt.want {
				t.Fatalf("costString(%v) = %q, want %q", tt.cost, got, tt.want)
			}
		})
	}
}

func TestFetchPricingVariants(t *testing.T) {
	// Subtests share pricing globals — do not add t.Parallel().
	originalTransport := http.DefaultTransport
	originalCache, originalFetched, originalErr := getPricingState()
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
		setPricingState(originalCache, originalFetched, originalErr)
	})

	successBody := `{"azure/gpt-4o":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`

	tests := []struct {
		name        string
		roundTrip   func(*http.Request) (*http.Response, error)
		wantErr     string
		wantFetched bool
		checkLookup bool
	}{
		{
			name: "http error",
			roundTrip: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network down")
			},
			wantErr:     "failed to fetch pricing data",
			wantFetched: false,
		},
		{
			name: "status error",
			roundTrip: func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("oops")),
					Header:     make(http.Header),
				}, nil
			},
			wantErr:     "pricing API returned status 500",
			wantFetched: false,
		},
		{
			name: "decode error",
			roundTrip: func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("{")),
					Header:     make(http.Header),
				}, nil
			},
			wantErr:     "failed to parse pricing data",
			wantFetched: false,
		},
		{
			name: "success",
			roundTrip: func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(successBody)),
					Header:     make(http.Header),
				}, nil
			},
			wantFetched: true,
			checkLookup: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setPricingState(nil, false, nil)
			http.DefaultTransport = roundTripperFunc{roundTrip: tt.roundTrip}

			fetchPricing()

			cache, fetched, fetchErr := getPricingState()
			if tt.wantErr != "" {
				if fetchErr == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(fetchErr.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want %q", fetchErr.Error(), tt.wantErr)
				}
			} else if fetchErr != nil {
				t.Fatalf("unexpected error: %v", fetchErr)
			}

			if fetched != tt.wantFetched {
				t.Fatalf("fetched = %v, want %v", fetched, tt.wantFetched)
			}

			if tt.checkLookup {
				if cache == nil {
					t.Fatal("expected cache to be populated")
				}
				pricing, found := lookupLiteLLMPricing("openai", "gpt-4o")
				if !found {
					t.Fatal("expected provider mapping to find pricing")
				}
				if pricing.InputPerMillion == 0 || pricing.OutputPerMillion == 0 {
					t.Fatalf("unexpected pricing: %+v", pricing)
				}
			}
		})
	}
}

func TestBudgetUsedPctNoMaxCost(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	if pct := m.BudgetUsedPct(); pct != 0 {
		t.Fatalf("BudgetUsedPct() = %v, want 0 when no MaxCost set", pct)
	}
}

func TestBudgetUsedPctPartial(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pricing test in short mode")
	}
	m := New("openai", "gpt-4o")
	m.SetMaxCost(10.00)
	// Add some tokens to create a partial cost.
	m.AddTokens(100000, 10000)

	pct := m.BudgetUsedPct()
	if pct <= 0 || pct >= 1.0 {
		t.Fatalf("BudgetUsedPct() = %v, want between 0 and 1", pct)
	}
}

func TestBudgetUsedPctExceeded(t *testing.T) {
	t.Parallel()
	m := New("openai", "gpt-4o")
	m.SetMaxCost(0.0001)
	m.AddTokens(1_000_000, 1_000_000)

	pct := m.BudgetUsedPct()
	if pct != 1.0 {
		t.Fatalf("BudgetUsedPct() = %v, want 1.0 when budget exceeded", pct)
	}
}

func TestLiveModelsForProviderFiltersByMode(t *testing.T) {
	t.Parallel()
	cache := map[string]liteLLMModel{
		"openai/gpt-4o":           {LiteLLMProvider: "openai", Mode: "chat"},
		"openai/gpt-4o-mini":      {LiteLLMProvider: "openai", Mode: "chat"},
		"openai/text-embedding-3": {LiteLLMProvider: "openai", Mode: "embedding"},
		"openai/dall-e-3":         {LiteLLMProvider: "openai", Mode: "image_generation"},
		"claude-3-5-sonnet":       {LiteLLMProvider: "anthropic", Mode: "chat"},
		"gpt-legacy":              {LiteLLMProvider: "openai"}, // empty mode counts as chat
	}

	got := liveModelsForProvider(cache, "openai")
	want := []string{"gpt-4o", "gpt-4o-mini", "gpt-legacy"}
	if !equalStringSlices(got, want) {
		t.Errorf("openai models: got %v, want %v", got, want)
	}

	got = liveModelsForProvider(cache, "anthropic")
	if !equalStringSlices(got, []string{"claude-3-5-sonnet"}) {
		t.Errorf("anthropic models: got %v, want [claude-3-5-sonnet]", got)
	}
}

func TestLiveModelsForProviderUnknownProviderReturnsEmpty(t *testing.T) {
	t.Parallel()
	cache := map[string]liteLLMModel{
		"openai/gpt-4o": {LiteLLMProvider: "openai", Mode: "chat"},
	}
	got := liveModelsForProvider(cache, "no-such-provider")
	if len(got) != 0 {
		t.Errorf("unknown provider should return empty list, got %v", got)
	}
}

func TestModelsForProviderFallbackParses(t *testing.T) {
	t.Parallel()
	// The embedded fallback must parse and surface a few canonical
	// entries — these are the suggestions users see on cold start
	// before the LiteLLM fetch completes.
	fallbackOnce.Do(loadFallbackModels)
	if fallbackParseErr != nil {
		t.Fatalf("fallback YAML parse error: %v", fallbackParseErr)
	}

	checks := map[string]string{
		"openai":    "gpt-4o",
		"anthropic": "claude-sonnet-4-6",
		"gemini":    "gemini-2.5-pro",
	}
	for provider, want := range checks {
		models := fallbackModels[provider]
		if !containsString(models, want) {
			t.Errorf("fallback %s: expected to contain %q, got %v", provider, want, models)
		}
	}
}

func TestModelsForProvider_FallbackUnknownProvider(t *testing.T) {
	t.Parallel()
	got := ModelsForProvider("totally-unknown-xyz")
	if len(got) != 0 {
		t.Errorf("expected empty for unknown provider, got %v", got)
	}
}

func TestModelsForProvider_FallbackEmptyIsSorted(t *testing.T) {
	t.Parallel()
	got := ModelsForProvider("")
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("result not sorted at index %d: %q > %q", i, got[i-1], got[i])
		}
	}
}

func TestGetPricing_OllamaAlwaysFree(t *testing.T) {
	t.Parallel()
	p := GetPricing("ollama", "llama3")
	if p.InputPerMillion != 0 || p.OutputPerMillion != 0 {
		t.Errorf("ollama pricing should be zero, got %+v", p)
	}
}

func TestGetPricing_UnknownModelZero(t *testing.T) {
	t.Parallel()
	p := GetPricing("openai", "totally-nonexistent-model-xyz-999")
	if p.InputPerMillion != 0 || p.OutputPerMillion != 0 {
		t.Errorf("unknown model pricing should be zero, got %+v", p)
	}
}

func TestIsChatMode_Variants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode string
		want bool
	}{
		{"", true},
		{"chat", true},
		{"responses", true},
		{"embedding", false},
		{"image_generation", false},
	}
	for _, tt := range tests {
		got := isChatMode(tt.mode)
		if got != tt.want {
			t.Errorf("isChatMode(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
