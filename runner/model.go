package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/mcp"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/ollama"
	"github.com/cowdogmoo/squad/responses"
	"github.com/cowdogmoo/squad/tools"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/openai"
)

// InvokeModel resolves provider settings and calls the appropriate model backend.
// It is exported so the pipeline runner can call it directly.
func InvokeModel(ctx context.Context, opts *RunOptions, bundle *agent.Bundle) (string, *metrics.Metrics, error) {
	provider := normalizeProvider(opts.Provider)
	model := opts.Model
	temperature := opts.Temperature
	maxTokens := opts.MaxTokens

	systemPrompt := bundle.System
	if opts.System != "" {
		systemPrompt += "\n\n## System Override\n\n" + strings.TrimSpace(opts.System) + "\n"
	}

	ex, err := executor.New(bundle.Environment, bundle.WorkDir)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create executor: %w", err)
	}
	defer func() { _ = ex.Close() }()

	logging.InfoContext(ctx, "executor created (type=%s)", ex.Type())
	if ex.Type() != "local" {
		systemPrompt += "\n\n## Execution Environment\n\n" + ex.EnvironmentDescription() + "\n"
	}

	taskCfg := buildTaskConfig(opts)

	// Connect MCP servers declared in the agent manifest and/or CLI flags.
	mcpServers := bundle.MCPServers
	mcpServers = append(mcpServers, opts.MCPServers...)
	if len(mcpServers) > 0 {
		clients, mcpErr := connectMCPServers(ctx, mcpServers)
		defer closeMCPClients(clients)
		if mcpErr != nil {
			return "", nil, mcpErr
		}
		taskCfg.ExtraTools = mcp.BuildHandlers(clients)
		logging.InfoContext(ctx, "MCP tools loaded: %d tools from %d server(s)", len(taskCfg.ExtraTools), len(clients))
	}

	return callModel(ctx, opts, provider, model, systemPrompt, bundle, temperature, maxTokens, taskCfg, ex)
}

// callModel dispatches the prompt to the appropriate model backend and returns the response.
func callModel(ctx context.Context, opts *RunOptions, provider, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int, taskCfg *tools.TaskConfig, ex executor.Executor) (string, *metrics.Metrics, error) {
	if responses.UseResponsesAPI(provider, model, reasoningPrefixes(opts)) {
		return callResponsesAPI(ctx, opts, model, systemPrompt, bundle, temperature, maxTokens, taskCfg, ex)
	}
	return callLangChainLLM(ctx, opts, provider, model, systemPrompt, bundle, temperature, maxTokens, taskCfg, ex)
}

// callResponsesAPI runs the prompt via the OpenAI Responses API.
func callResponsesAPI(ctx context.Context, opts *RunOptions, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int, taskCfg *tools.TaskConfig, ex executor.Executor) (string, *metrics.Metrics, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return "", nil, fmt.Errorf("API key required for OpenAI Responses API: use --api-key, config provider.token, or OPENAI_API_KEY env var")
	}

	// Reasoning models (gpt-5*) consume output tokens on internal reasoning
	// before producing visible text.  The config default of 1024 is far too
	// small — the model burns the entire budget on thinking and returns no
	// message.  Apply a higher floor unless the user explicitly set --max-tokens.
	if responses.IsReasoningModel(model, reasoningPrefixes(opts)) && maxTokens < responses.DefaultMaxOutputTokens {
		logging.InfoContext(ctx, "raising max_output_tokens %d → %d for reasoning model %s", maxTokens, responses.DefaultMaxOutputTokens, model)
		maxTokens = responses.DefaultMaxOutputTokens
	}

	provider := "openai"
	if responses.UseResponsesAPI(opts.Provider, model, reasoningPrefixes(opts)) && opts.Provider == "openai-responses" {
		provider = "openai-responses"
	}

	m := metrics.New(provider, model)
	m.SetMaxCost(opts.MaxCost)
	if taskCfg != nil {
		taskCfg.ParentMetrics = m
	}
	logging.InfoContext(ctx, "model call started via Responses API (model=%s)", model)
	response, err := responses.RunWithTools(ctx, apiKey, opts.BaseURL, model, systemPrompt, bundle.User, bundle.WorkDir, opts.Org, temperature, maxTokens, opts.MaxIterations, reasoningPrefixes(opts), taskCfg, m, ex)
	m.Finish()
	if err != nil {
		if errors.Is(err, metrics.ErrBudgetExceeded) {
			return response, m, metrics.ErrBudgetExceeded
		}
		return "", m, fmt.Errorf("model call failed: %w", err)
	}
	logging.InfoContext(ctx, "model call finished in %s (response-bytes=%d)", m.Duration().Round(time.Millisecond), len(response))
	return response, m, nil
}

// DefaultMaxTokensWithTask is the output-token floor for agents that have
// access to the Task tool.  Task prompts embed file lists, prior-agent
// context, and hard constraints, easily requiring 4K+ tokens in a single
// tool-call argument.  langchaingo's default of 2048 silently truncates
// these arguments, causing the child dispatch to fail with "prompt is
// required".
const DefaultMaxTokensWithTask = 16384

// DefaultMaxTokensLeaf is the output-token floor for leaf agents (no Task
// tool).  Leaf agents mostly emit Edit/Write/Bash tool calls whose
// arguments are modest, but 4096 gives comfortable headroom for long
// file-write operations.
const DefaultMaxTokensLeaf = 4096

// inferMaxTokens applies a sensible output-token floor when the caller did
// not explicitly set --max-tokens.  Orchestrator agents (with Task tool)
// need a much larger budget than leaf agents because the Task tool's prompt
// argument can be thousands of tokens.
func inferMaxTokens(maxTokens int, hasTaskTool bool) int {
	if maxTokens > 0 {
		return maxTokens // explicit user override — respect it
	}
	if hasTaskTool {
		return DefaultMaxTokensWithTask
	}
	return DefaultMaxTokensLeaf
}

// callLangChainLLM runs the prompt via a LangChain-compatible LLM.
func callLangChainLLM(ctx context.Context, opts *RunOptions, provider, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int, taskCfg *tools.TaskConfig, ex executor.Executor) (string, *metrics.Metrics, error) {
	llm, err := buildLLM(opts, provider, model)
	if err != nil {
		return "", nil, err
	}

	maxTokens = inferMaxTokens(maxTokens, taskCfg != nil)
	logging.DebugContext(ctx, "max_tokens=%d (hasTaskTool=%v)", maxTokens, taskCfg != nil)
	callOpts := buildCallOpts(opts, provider, temperature, maxTokens)

	m := metrics.New(provider, model)
	m.SetMaxCost(opts.MaxCost)
	if taskCfg != nil {
		taskCfg.ParentMetrics = m
	}
	logging.InfoContext(ctx, "model call started (provider=%s model=%s)", provider, model)
	response, err := tools.RunWithTools(ctx, llm, systemPrompt, bundle.User, bundle.WorkDir, opts.MaxIterations, taskCfg, m, ex, callOpts...)
	m.Finish()
	if err != nil {
		if errors.Is(err, metrics.ErrBudgetExceeded) {
			return response, m, metrics.ErrBudgetExceeded
		}
		return "", m, fmt.Errorf("model call failed: %w", err)
	}
	logging.InfoContext(ctx, "model call finished in %s (response-bytes=%d)", m.Duration().Round(time.Millisecond), len(response))
	return response, m, nil
}

// buildCallOpts constructs LLM call options from provider settings.
func buildCallOpts(opts *RunOptions, provider string, temperature float64, maxTokens int) []llms.CallOption {
	callOpts := []llms.CallOption{}
	if temperature >= 0 {
		callOpts = append(callOpts, llms.WithTemperature(temperature))
	}
	// Enable prompt caching for Anthropic models.  The system prompt is
	// sent on every tool-loop iteration and can be 18K+ tokens; caching
	// gives a 90% discount on repeated input tokens.
	if provider == "anthropic" {
		callOpts = append(callOpts, anthropic.WithPromptCaching())
	}
	if maxTokens <= 0 {
		return callOpts
	}
	if !isOpenAICompatProvider(provider) {
		return append(callOpts, llms.WithMaxTokens(maxTokens))
	}
	useLegacy := provider != "openai" || opts.OpenAICompatMax
	if useLegacy {
		return append(callOpts, llms.WithMaxTokens(maxTokens), openai.WithLegacyMaxTokensField())
	}
	return append(callOpts, openai.WithMaxCompletionTokens(maxTokens))
}

// buildLLM constructs an LLM model instance based on the provider and configuration.
func buildLLM(opts *RunOptions, provider, model string) (llms.Model, error) {
	switch provider {
	case "ollama":
		return buildNativeOllamaLLM(opts, model), nil
	case "openai", "":
		return buildOpenAICompatLLM(opts, provider, model)
	case "anthropic":
		return buildAnthropicLLM(opts, model)
	default:
		return nil, fmt.Errorf("provider not implemented: %s", provider)
	}
}

func buildOpenAICompatLLM(opts *RunOptions, provider, model string) (llms.Model, error) {
	oaiOpts := []openai.Option{}
	if model != "" {
		oaiOpts = append(oaiOpts, openai.WithModel(model))
	}

	if opts.BaseURL != "" {
		oaiOpts = append(oaiOpts, openai.WithBaseURL(opts.BaseURL))
	}

	if opts.APIKey != "" {
		oaiOpts = append(oaiOpts, openai.WithToken(opts.APIKey))
	}

	if provider == "openai" || provider == "" {
		if opts.Org != "" {
			oaiOpts = append(oaiOpts, openai.WithOrganization(opts.Org))
		}

		if opts.APIVersion != "" {
			oaiOpts = append(oaiOpts, openai.WithAPIVersion(opts.APIVersion))
		}

		if apiType := strings.ToLower(opts.APIType); apiType == "azure" {
			oaiOpts = append(oaiOpts, openai.WithAPIType(openai.APITypeAzure))
		}
	}

	return openai.New(oaiOpts...)
}

func buildAnthropicLLM(opts *RunOptions, model string) (llms.Model, error) {
	aOpts := []anthropic.Option{}
	if model != "" {
		aOpts = append(aOpts, anthropic.WithModel(model))
	}

	if opts.APIKey != "" {
		aOpts = append(aOpts, anthropic.WithToken(opts.APIKey))
	}

	if opts.BaseURL != "" {
		aOpts = append(aOpts, anthropic.WithBaseURL(opts.BaseURL))
	}

	return anthropic.New(aOpts...)
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func buildNativeOllamaLLM(opts *RunOptions, model string) llms.Model {
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	numCtx := opts.NumCtx
	if numCtx <= 0 {
		numCtx = 32768
	}
	return ollama.New(baseURL, model, numCtx)
}

func isOpenAICompatProvider(provider string) bool {
	return provider == "" || provider == "openai"
}

// reasoningPrefixes returns the configured reasoning model prefixes,
// falling back to the default if config is unavailable.
func reasoningPrefixes(opts *RunOptions) []string {
	if opts.Config != nil && len(opts.Config.Model.ReasoningPrefixes) > 0 {
		return opts.Config.Model.ReasoningPrefixes
	}
	return config.Defaults().Model.ReasoningPrefixes
}

// buildTaskConfig creates a TaskConfig for the Task tool from RunOptions.
func buildTaskConfig(opts *RunOptions) *tools.TaskConfig {
	cfg := &tools.TaskConfig{
		AgentsDir:     opts.AgentsDir,
		WorkingDir:    opts.WorkingDir,
		MaxIterations: opts.MaxIterations,
		MaxCost:       opts.MaxCost,
		Registry:      tools.NewBackgroundTaskRegistry(),
		Findings:      opts.Findings,
		AgentName:     opts.AgentName,
	}
	cfg.CallModel = func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
		// Child agents inherit parent's template variables
		childBundle, err := agent.BuildBundle(agentsDir, agentName, prompt, workingDir, mode, opts.Vars)
		if err != nil {
			return "", nil, fmt.Errorf("failed to build child agent bundle: %w", err)
		}

		childOpts := *opts
		childOpts.Agent = agentName
		childOpts.AgentsDir = agentsDir
		childOpts.WorkingDir = workingDir
		childOpts.Mode = mode
		childOpts.System = ""
		childOpts.AgentName = agentName

		// Propagate remaining budget from parent metrics so child agents
		// cannot each spend the full original budget independently.
		if cfg.ParentMetrics != nil && opts.MaxCost > 0 {
			remaining := cfg.ParentMetrics.RemainingBudget()
			if remaining <= 0 {
				return "", nil, metrics.ErrBudgetExceeded
			}
			childOpts.MaxCost = remaining
			logging.InfoContext(ctx, "child agent %s budget: $%.4f remaining of $%.4f total", agentName, remaining, opts.MaxCost)
		}

		return InvokeModel(ctx, &childOpts, childBundle)
	}
	return cfg
}

// connectMCPServers starts all configured MCP server subprocesses and
// performs the protocol handshake. Returns connected clients and any error.
// On error, already-connected clients are still returned for cleanup.
func connectMCPServers(ctx context.Context, servers []mcp.ServerConfig) ([]*mcp.Client, error) {
	var clients []*mcp.Client
	for _, cfg := range servers {
		logging.InfoContext(ctx, "connecting MCP server %q (%s %v)", cfg.Name, cfg.Command, cfg.Args)
		c, err := mcp.Connect(ctx, cfg)
		if err != nil {
			return clients, fmt.Errorf("MCP server %q failed: %w", cfg.Name, err)
		}
		logging.InfoContext(ctx, "MCP server %q connected (%d tools)", cfg.Name, len(c.Tools()))
		clients = append(clients, c)
	}
	return clients, nil
}

// closeMCPClients shuts down all MCP server subprocesses.
func closeMCPClients(clients []*mcp.Client) {
	for _, c := range clients {
		_ = c.Close()
	}
}
