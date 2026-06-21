package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/skill"
	"github.com/cowdogmoo/squad/telemetry"
	"github.com/cowdogmoo/squad/tools"
	"github.com/mattn/go-isatty"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/openai"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// buildConfirmRuntime wires os.Stdin / os.Stderr / the resolved --auto-confirm
// policy into a ConfirmRuntime for the Confirm tool. Always returns a non-nil
// runtime so the tool is registered for every run; the runtime's AutoConfirm
// + isStdinTTY decide whether each call prompts a human or resolves against
// the policy.
func buildConfirmRuntime(opts *RunOptions) *tools.ConfirmRuntime {
	mode := opts.AutoConfirm
	if mode != "" && !mode.IsValid() {
		logging.Warn("ignoring invalid --auto-confirm value %q", mode)
		mode = tools.AutoConfirmUnset
	}
	return &tools.ConfirmRuntime{
		In:          os.Stdin,
		Out:         os.Stderr,
		IsTTY:       isStdinTTY,
		AutoConfirm: mode,
	}
}

// isStdinTTY reports whether stdin is connected to a terminal. The mattn
// isatty pull is already an indirect dep, so this stays a single file.
func isStdinTTY() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

// buildSkillRuntime constructs the per-run Skill tool runtime from the
// bundle's filtered entries. Returns nil when no skills are visible so
// RunWithToolsConfig treats this as "no Skill tool".
//
// The returned stack is empty for a fresh run; when ctx carries a session
// logger that is replaying a prior run (via --resume), any `skill_loaded`
// events already logged are pushed back onto the stack so loaded skills
// remain reachable to Read/Bash after the resume.
func buildSkillRuntime(ctx context.Context, bundle *agent.Bundle, _ *RunOptions) *tools.SkillRuntime {
	if bundle == nil || len(bundle.SkillEntries) == 0 {
		return nil
	}
	rt := &tools.SkillRuntime{
		Entries: bundle.SkillEntries,
		Stack:   skill.NewStack(),
	}
	rehydrateSkillStack(rt, bundle.WorkDir, session.FromContext(ctx))
	logger := session.FromContext(ctx)
	rt.OnLoad = func(e skill.Entry) {
		logging.InfoContext(ctx, "skill loaded: name=%s scope=%s dir=%s",
			e.Name(), e.Scope.String(), e.Dir)
		_ = logger.Append(session.EventSkillLoaded, map[string]any{
			"name":  e.Name(),
			"scope": e.Scope.String(),
			"dir":   e.Dir,
		})
	}
	return rt
}

// rehydrateSkillStack replays the events.jsonl of the active session and
// re-pushes any skill that was previously loaded onto the fresh stack. It
// is a no-op when there is no session logger, when the log can't be read,
// or when the agent's filtered Entries no longer include a previously
// loaded skill (the agent.yaml may have changed between runs).
func rehydrateSkillStack(rt *tools.SkillRuntime, workingDir string, logger *session.Logger) {
	if rt == nil || logger == nil {
		return
	}
	events, err := os.ReadFile(filepath.Join(logger.Dir(), "events.jsonl"))
	if err != nil {
		return
	}
	byName := make(map[string]skill.Entry, len(rt.Entries))
	for _, e := range rt.Entries {
		byName[e.Name()] = e
	}
	for _, line := range strings.Split(string(events), "\n") {
		if line == "" {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Name string `json:"name"`
			} `json:"payload"`
		}
		if jsonErr := json.Unmarshal([]byte(line), &ev); jsonErr != nil {
			continue
		}
		if ev.Type != session.EventSkillLoaded {
			continue
		}
		if entry, ok := byName[ev.Payload.Name]; ok {
			rt.Stack.Push(entry)
		}
	}
	_ = workingDir // reserved for future use if absolute paths need rebasing
}

// applyReadOnlyMode enables the readonly tool gate on ctx when mode is
// "readonly", and returns ctx unchanged otherwise.
func applyReadOnlyMode(ctx context.Context, mode string) context.Context {
	if mode != "readonly" {
		return ctx
	}
	logging.InfoContext(ctx, "readonly mode: Write/Edit/MultiEdit will be rejected")
	return tools.InitReadOnlyMode(ctx)
}

// InvokeModel resolves provider settings and calls the appropriate model backend.
// It is exported so the pipeline runner can call it directly.
//
// The returned *metrics.Metrics is always non-nil so callers can report
// partial cost even when the run is interrupted (e.g. ctrl+c during MCP
// server initialization).
func InvokeModel(ctx context.Context, opts *RunOptions, bundle *agent.Bundle) (string, *metrics.Metrics, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "agent.invoke",
		trace.WithAttributes(
			attribute.String("gen_ai.system", normalizeProvider(opts.Provider)),
			attribute.String("gen_ai.request.model", opts.Model),
			attribute.String("squad.agent", opts.Agent),
		),
	)
	defer span.End()

	provider := normalizeProvider(opts.Provider)
	model := opts.Model
	temperature := opts.Temperature
	maxTokens := opts.MaxTokens

	// InvokeModel is the single chokepoint for both leaf and composed
	// sub-agent runs, so readonly enforcement applies uniformly here.
	ctx = applyReadOnlyMode(ctx, opts.Mode)

	// Create metrics early so partial cost is always available, even if
	// we fail during executor or MCP setup.
	m := metrics.New(provider, model)
	m.SetMaxCost(opts.MaxCost)

	systemPrompt := bundle.System
	if opts.System != "" {
		systemPrompt += "\n\n## System Override\n\n" + strings.TrimSpace(opts.System) + "\n"
	}

	ex, err := executor.New(bundle.Environment, bundle.WorkDir)
	if err != nil {
		m.Finish()
		return "", m, fmt.Errorf("failed to create executor: %w", err)
	}
	defer func() {
		if cerr := ex.Close(); cerr != nil {
			logging.Warn("failed to close executor: %v", cerr)
		}
	}()

	logging.InfoContext(ctx, "executor created (type=%s)", ex.Type())
	if ex.Type() != "local" {
		systemPrompt += "\n\n## Execution Environment\n\n" + ex.EnvironmentDescription() + "\n"
	}

	taskCfg := buildTaskConfig(opts)
	applyBundleBudget(taskCfg, bundle)
	if bundle.DisableTask {
		logging.InfoContext(ctx, "Task tool disabled for this agent")
		taskCfg.CallModel = nil
		taskCfg.Registry = nil
	}

	// Connect MCP servers declared in the agent manifest and/or CLI flags.
	mcpServers := bundle.MCPServers
	mcpServers = append(mcpServers, opts.MCPServers...)
	var extraMCPTools []tools.Handler
	if len(mcpServers) > 0 {
		clients, mcpErr := connectMCPServers(ctx, mcpServers)
		defer closeMCPClients(clients)
		if mcpErr != nil {
			m.Finish()
			return "", m, mcpErr
		}
		mcpHandlers := mcp.BuildHandlers(clients)
		extraMCPTools = convertMCPHandlers(mcpHandlers)
		if taskCfg != nil {
			taskCfg.ExtraTools = extraMCPTools
		}
		logging.InfoContext(ctx, "MCP tools loaded: %d tools from %d server(s)", len(extraMCPTools), len(clients))
	}

	return callModel(ctx, opts, provider, model, systemPrompt, bundle, temperature, maxTokens, taskCfg, ex, m)
}

// callModel dispatches the prompt to the appropriate model backend and returns the response.
// The caller-provided metrics m is passed through to the backend so token
// counts accumulate on the same object that was created in InvokeModel.
func callModel(ctx context.Context, opts *RunOptions, provider, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int, taskCfg *tools.TaskConfig, ex executor.Executor, m *metrics.Metrics) (string, *metrics.Metrics, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "llm.call",
		trace.WithAttributes(
			attribute.String("gen_ai.system", provider),
			attribute.String("gen_ai.request.model", model),
			attribute.Float64("gen_ai.request.temperature", temperature),
			attribute.Int("gen_ai.request.max_tokens", maxTokens),
		),
	)
	defer span.End()

	// Effective iteration cap: explicit --max-iterations verbatim, else the
	// agent's base budget scaled by the per-model factor. Computed here (not
	// written back to opts) so child agents inherit the unscaled base and
	// scale once against their own model rather than compounding the factor.
	maxIter := resolveIterationBudget(opts, bundle, model)

	var response string
	var err error
	if responses.UseResponsesAPI(provider, model, reasoningPrefixes(opts)) {
		response, err = callResponsesAPI(ctx, opts, model, systemPrompt, bundle, temperature, maxTokens, maxIter, taskCfg, ex, m)
	} else {
		response, err = callLangChainLLM(ctx, opts, provider, model, systemPrompt, bundle, temperature, maxTokens, maxIter, taskCfg, ex, m)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.SetAttributes(
		attribute.Int64("gen_ai.usage.input_tokens", m.InputTokens()),
		attribute.Int64("gen_ai.usage.output_tokens", m.OutputTokens()),
		attribute.Float64("gen_ai.usage.cost_usd", m.Cost()),
		attribute.Int("gen_ai.usage.iterations", m.Iterations()),
	)
	return response, m, err
}

// callResponsesAPI runs the prompt via the OpenAI Responses API.
func callResponsesAPI(ctx context.Context, opts *RunOptions, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens, maxIter int, taskCfg *tools.TaskConfig, ex executor.Executor, m *metrics.Metrics) (string, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return "", fmt.Errorf("API key required for OpenAI Responses API: use --api-key, config provider.token, or OPENAI_API_KEY env var")
	}

	// Reasoning models (gpt-5*) consume output tokens on internal reasoning
	// before producing visible text.  The config default of 1024 is far too
	// small — the model burns the entire budget on thinking and returns no
	// message.  Apply a higher floor unless the user explicitly set --max-tokens.
	if responses.IsReasoningModel(model, reasoningPrefixes(opts)) && maxTokens < responses.DefaultMaxOutputTokens {
		logging.InfoContext(ctx, "raising max_output_tokens %d → %d for reasoning model %s", maxTokens, responses.DefaultMaxOutputTokens, model)
		maxTokens = responses.DefaultMaxOutputTokens
	}

	if taskCfg != nil {
		taskCfg.ParentMetrics = m
	}
	logging.InfoContext(ctx, "model call started via Responses API (model=%s)", model)
	skillRuntime := buildSkillRuntime(ctx, bundle, opts)
	confirmRuntime := buildConfirmRuntime(opts)
	response, err := responses.RunWithTools(ctx, apiKey, opts.BaseURL, model, systemPrompt, bundle.User, bundle.WorkDir, opts.Org, opts.ResumeResponseID, temperature, maxTokens, maxIter, bundle.EditDeadline, reasoningPrefixes(opts), taskCfg, m, ex, skillRuntime, confirmRuntime)
	m.Finish()
	if err != nil {
		if errors.Is(err, metrics.ErrBudgetExceeded) {
			return response, metrics.ErrBudgetExceeded
		}
		return "", fmt.Errorf("model call failed: %w", err)
	}
	logging.InfoContext(ctx, "model call finished in %s (response-bytes=%d)", m.Duration().Round(time.Millisecond), len(response))
	return response, nil
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

// inferMaxTokens applies a sensible output-token floor.  Orchestrator agents
// (with Task tool) need a much larger budget than leaf agents because the
// Task tool's prompt argument can be thousands of tokens.  The floor is
// enforced even when a config default is present, because the config default
// of 1024 is too small for orchestrators and would silently truncate the
// Task prompt argument to empty.
func inferMaxTokens(maxTokens int, hasTaskTool bool) int {
	if hasTaskTool && maxTokens < DefaultMaxTokensWithTask {
		return DefaultMaxTokensWithTask
	}
	if maxTokens > 0 {
		return maxTokens
	}
	return DefaultMaxTokensLeaf
}

// callLangChainLLM runs the prompt via a LangChain-compatible LLM.
func callLangChainLLM(ctx context.Context, opts *RunOptions, provider, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens, maxIter int, taskCfg *tools.TaskConfig, ex executor.Executor, m *metrics.Metrics) (string, error) {
	llm, err := buildLLM(ctx, opts, provider, model)
	if err != nil {
		return "", err
	}
	if closer, ok := llm.(io.Closer); ok {
		defer func() {
			if cerr := closer.Close(); cerr != nil {
				logging.Warn("failed to close LLM client: %v", cerr)
			}
		}()
	}

	maxTokens = inferMaxTokens(maxTokens, taskCfg != nil)
	logging.DebugContext(ctx, "max_tokens=%d (hasTaskTool=%v)", maxTokens, taskCfg != nil)
	if modelRequiresTemperatureOne(model) {
		temperature = 1.0
	}
	callOpts := buildCallOpts(opts, provider, temperature, maxTokens)

	if taskCfg != nil {
		taskCfg.ParentMetrics = m
	}
	// Only the LangChain Anthropic provider handles CachedContent; all other
	// LangChain providers (openai, openai-compat, gemini, ollama) silently drop it.
	useCacheControl := provider == "anthropic"
	// openai-compat endpoints default to tool_choice:"auto", allowing the model
	// to respond without calling any tools. Force the first iteration to use
	// tool_choice:"required" so the model must explore before answering.
	// Regular openai users rely on the auto behavior; only restrict compat endpoints.
	forceFirstTool := provider == "openai-compat"
	// langchaingo's OpenAI chat-completions integration rejects a single
	// role:"tool" message with multiple parts ("expected exactly one part
	// for role tool, got N"). Emit one message per tool result for those
	// providers; Anthropic, Responses API, gemini, ollama keep the
	// single-message form.
	splitToolResults := provider == "openai" || provider == "openai-compat"
	priorMessages := loadResumeTranscript(ctx, opts)
	logging.InfoContext(ctx, "model call started (provider=%s model=%s)", provider, model)
	response, err := tools.RunWithTools(ctx, llm, systemPrompt, bundle.User, bundle.WorkDir, tools.RunWithToolsConfig{
		MaxIterations:      maxIter,
		EditDeadline:       bundle.EditDeadline,
		TaskCfg:            taskCfg,
		Metrics:            m,
		Executor:           ex,
		UseCacheControl:    useCacheControl,
		ForceFirstToolCall: forceFirstTool,
		SplitToolResults:   splitToolResults,
		Skill:              buildSkillRuntime(ctx, bundle, opts),
		Confirm:            buildConfirmRuntime(opts),
		MaxRetries:         opts.MaxRetries,
		PriorMessages:      priorMessages,
	}, callOpts...)
	m.Finish()
	if err != nil {
		if errors.Is(err, metrics.ErrBudgetExceeded) {
			return response, metrics.ErrBudgetExceeded
		}
		return "", fmt.Errorf("model call failed: %w", err)
	}
	logging.InfoContext(ctx, "model call finished in %s (response-bytes=%d)", m.Duration().Round(time.Millisecond), len(response))
	return response, nil
}

// loadResumeTranscript returns the prior conversation to replay when this run
// is a --resume on the stateless (LangChain) path. It returns nil for a fresh
// run, and warns — rather than silently proceeding context-free — when a
// resume finds no replayable transcript (e.g. the prior run used the OpenAI
// Responses path, which persists no transcript).
func loadResumeTranscript(ctx context.Context, opts *RunOptions) []llms.MessageContent {
	if opts.ResumeID == "" {
		return nil
	}
	logger := session.FromContext(ctx)
	if logger == nil {
		return nil
	}
	prior, err := tools.LoadTranscript(logger.Dir())
	if err != nil {
		logging.Warn("resume: failed to load transcript for session %s: %v", opts.ResumeID, err)
		return nil
	}
	if len(prior) == 0 {
		logging.Warn("resume: no transcript for session %s — continuing without prior context", opts.ResumeID)
		return nil
	}
	logging.InfoContext(ctx, "resume: loaded %d prior message(s) for session %s", len(prior), opts.ResumeID)
	return prior
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
	if opts.Stream {
		callOpts = append(callOpts, llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
			_, err := os.Stderr.Write(chunk)
			return err
		}))
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
func buildLLM(ctx context.Context, opts *RunOptions, provider, model string) (llms.Model, error) {
	switch provider {
	case "ollama":
		return buildNativeOllamaLLM(opts, model), nil
	case "openai", "":
		return buildOpenAICompatLLM(opts, provider, model)
	case "anthropic":
		return buildAnthropicLLM(opts, model)
	case "gemini":
		return buildGeminiLLM(ctx, opts, model)
	case "openai-compat":
		return buildOpenAICompatLLM(opts, "openai-compat", model)
	default:
		return nil, fmt.Errorf("provider not implemented: %s", provider)
	}
}

// buildOpenAICompatLLM constructs an OpenAI-compatible LangChain LLM for any
// provider that speaks the OpenAI chat-completions API (openai, openai-compat,
// and the legacy Azure path). When provider is "openai" or empty, organization
// and API-version headers are applied; otherwise only base URL and token are
// set, making it suitable for third-party endpoints like DeepInfra or vLLM.
func buildOpenAICompatLLM(opts *RunOptions, provider, model string) (llms.Model, error) {
	if provider == "openai-compat" && opts.BaseURL == "" {
		return nil, fmt.Errorf("openai-compat provider requires --base-url (e.g. https://api.deepinfra.com/v1/openai)")
	}

	oaiOpts := []openai.Option{}
	if model != "" {
		oaiOpts = append(oaiOpts, openai.WithModel(model))
	}

	if opts.BaseURL != "" {
		oaiOpts = append(oaiOpts, openai.WithBaseURL(opts.BaseURL))
	}

	apiKey := opts.APIKey
	if apiKey == "" && provider == "openai-compat" {
		if v := os.Getenv("OPENAI_COMPAT_API_KEY"); v != "" {
			apiKey = v
		} else {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
	}
	if apiKey != "" {
		oaiOpts = append(oaiOpts, openai.WithToken(apiKey))
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

func buildGeminiLLM(ctx context.Context, opts *RunOptions, model string) (llms.Model, error) {
	gOpts := []googleai.Option{}
	if model != "" {
		gOpts = append(gOpts, googleai.WithDefaultModel(model))
	}
	if opts.APIKey != "" {
		gOpts = append(gOpts, googleai.WithAPIKey(opts.APIKey))
	}
	return googleai.New(ctx, gOpts...)
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
	return provider == "" || provider == "openai" || provider == "openai-compat"
}

// modelRequiresTemperatureOne reports whether the model requires
// `temperature=1` (Anthropic's API default) because it has extended
// thinking enabled. Claude opus-4-7 and opus-4-8 return HTTP 400 for
// any other value. Skipping the WithTemperature call would NOT work
// here: langchaingo's anthropic Temperature field has no `omitempty`,
// so omitting it sends `temperature: 0`, which still violates the
// thinking-mode constraint. The caller must explicitly set 1.0.
func modelRequiresTemperatureOne(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, prefix := range []string{"claude-opus-4-7", "claude-opus-4.7", "claude-opus-4-8", "claude-opus-4.8"} {
		if strings.HasPrefix(m, prefix) {
			return true
		}
	}
	return false
}

// reasoningPrefixes returns the configured reasoning model prefixes,
// falling back to the default if config is unavailable.
func reasoningPrefixes(opts *RunOptions) []string {
	if opts.Config != nil && len(opts.Config.Model.ReasoningPrefixes) > 0 {
		return opts.Config.Model.ReasoningPrefixes
	}
	return config.Defaults().Model.ReasoningPrefixes
}

// Iteration budgets clamp to the same range the --max-iterations flag enforces.
const (
	minIterationBudget = 10
	maxIterationBudget = 1000
)

// resolveIterationBudget computes the effective iteration cap for a run. An
// explicit --max-iterations is honored verbatim. Otherwise the agent's base
// budget — the manifest's max_iterations, or the configured/default cap when
// the manifest leaves it unset — is scaled by the per-model iteration factor
// and clamped to [10, 1000].
func resolveIterationBudget(opts *RunOptions, bundle *agent.Bundle, model string) int {
	if opts.MaxIterationsExplicit {
		return opts.MaxIterations
	}
	base := opts.MaxIterations
	if bundle != nil && bundle.MaxIterations > 0 {
		base = bundle.MaxIterations
	}
	scaled := int(float64(base)*modelIterationFactor(opts.Config, model) + 0.5)
	if scaled < minIterationBudget {
		return minIterationBudget
	}
	if scaled > maxIterationBudget {
		return maxIterationBudget
	}
	return scaled
}

// modelIterationFactor returns the per-model iteration multiplier from config,
// preferring an exact model entry, then a "default" entry, then 1.0.
// Non-positive factors are ignored so a stray 0 can't zero out the budget.
func modelIterationFactor(cfg *config.Config, model string) float64 {
	if cfg == nil || len(cfg.Model.IterationFactor) == 0 {
		return 1.0
	}
	if f, ok := cfg.Model.IterationFactor[model]; ok && f > 0 {
		return f
	}
	if f, ok := cfg.Model.IterationFactor["default"]; ok && f > 0 {
		return f
	}
	return 1.0
}

// applyChildIterationCap sets the child agent's iteration cap based on
// per-child budget config and manifest settings.
func applyChildIterationCap(ctx context.Context, childOpts *RunOptions, cfg *tools.TaskConfig, childBundle *agent.Bundle, agentName string) {
	if cfg.ChildMaxIter != nil {
		if childIter := cfg.ChildMaxIter(agentName); childIter > 0 {
			childOpts.MaxIterations = childIter
			// A deliberate budget cap must not be re-scaled by the per-model
			// factor downstream; mark it explicit so InvokeModel honors it.
			childOpts.MaxIterationsExplicit = true
			logging.InfoContext(ctx, "child agent %s iteration cap: %d (from parent budget)", agentName, childIter)
		}
	}
	if childBundle.MaxIterations > 0 && (childOpts.MaxIterations <= 0 || childBundle.MaxIterations < childOpts.MaxIterations) {
		childOpts.MaxIterations = childBundle.MaxIterations
		childOpts.MaxIterationsExplicit = true
		logging.InfoContext(ctx, "child agent %s iteration cap: %d (from manifest)", agentName, childBundle.MaxIterations)
	}
}

// applyChildModelOverrides applies model, provider, and base URL overrides
// from the child agent's manifest to the child options.
//
// BaseURL must be propagated here because child agents are dispatched via
// InvokeModel directly (not ExecuteRun), so ResolveModelPrecedence is never
// called on the child path — without this, openai-compat child agents would
// always fail with "openai-compat provider requires --base-url".
func applyChildModelOverrides(ctx context.Context, childOpts *RunOptions, childBundle *agent.Bundle, agentName string) {
	if childBundle.Model != "" {
		childOpts.Model = childBundle.Model
		logging.InfoContext(ctx, "child agent %s using manifest model override: %s", agentName, childBundle.Model)
	}
	if childBundle.Provider != "" {
		childOpts.Provider = childBundle.Provider
		logging.InfoContext(ctx, "child agent %s using manifest provider override: %s", agentName, childBundle.Provider)
	}
	if childBundle.BaseURL != "" && childOpts.BaseURL == "" {
		childOpts.BaseURL = childBundle.BaseURL
		logging.InfoContext(ctx, "child agent %s using manifest base URL override", agentName)
	}
	if len(childBundle.Models) > 1 {
		altModels := make([]string, 0, len(childBundle.Models)-1)
		for _, m := range childBundle.Models[1:] {
			altModels = append(altModels, m.Provider+"/"+m.Model)
		}
		logging.InfoContext(ctx, "child agent %s has %d alternate model(s): %v", agentName, len(altModels), altModels)
	}
}

// applyChildBudget propagates budget constraints to the child agent,
// using per-child dedicated caps when available.
func applyChildBudget(ctx context.Context, childOpts *RunOptions, cfg *tools.TaskConfig, opts *RunOptions, agentName string) error {
	if cfg.ParentMetrics == nil || opts.MaxCost <= 0 {
		return nil
	}
	remaining := cfg.ParentMetrics.RemainingBudget()
	if remaining <= 0 {
		return metrics.ErrBudgetExceeded
	}

	var childBudget float64
	if cfg.ChildMaxCost != nil {
		childBudget = cfg.ChildMaxCost(agentName)
	}
	if childBudget > 0 {
		if childBudget > remaining {
			childBudget = remaining
		}
		childOpts.MaxCost = childBudget
		logging.InfoContext(ctx, "child agent %s budget: $%.4f dedicated (pipeline remaining: $%.4f)", agentName, childBudget, remaining)
	} else {
		childOpts.MaxCost = remaining
		logging.InfoContext(ctx, "child agent %s budget: $%.4f remaining of $%.4f total", agentName, remaining, opts.MaxCost)
	}
	return nil
}

// buildTaskConfig creates a TaskConfig for the Task tool from RunOptions.
func buildTaskConfig(opts *RunOptions) *tools.TaskConfig {
	cfg := &tools.TaskConfig{
		AgentsDir:     opts.AgentsDir,
		WorkingDir:    opts.WorkingDir,
		MaxIterations: opts.MaxIterations,
		MaxCost:       opts.MaxCost,
		Registry:      tools.NewBackgroundTaskRegistry(opts.MaxConcurrentTasks),
		Findings:      opts.Findings,
		AgentName:     opts.AgentName,
	}
	cfg.CallModel = func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
		childBundle, err := agent.BuildBundleWithOptions(agentsDir, agentName, prompt, workingDir, mode, opts.Vars, &agent.BundleOptions{
			SkillOverrides: opts.SkillOverrides,
		})
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

		applyChildIterationCap(ctx, &childOpts, cfg, childBundle, agentName)
		applyChildModelOverrides(ctx, &childOpts, childBundle, agentName)
		if err := applyChildBudget(ctx, &childOpts, cfg, opts, agentName); err != nil {
			return "", nil, err
		}

		return InvokeModel(ctx, &childOpts, childBundle)
	}
	return cfg
}

// applyBundleBudget wires up per-child budget lookups from the parent bundle's
// budget config into the TaskConfig.
func applyBundleBudget(cfg *tools.TaskConfig, bundle *agent.Bundle) {
	if bundle.Budget != nil && len(bundle.Budget.Children) > 0 {
		cfg.ChildMaxCost = bundle.Budget.ChildMaxCost
		cfg.ChildMaxIter = bundle.Budget.ChildMaxIterations
	}
	cfg.RemoteOnly = bundle.RemoteOnly
}

// convertMCPHandlers converts MCP ToolHandlers to tools.Handler.
// This bridge avoids an import cycle (agent → mcp → tools → metrics → agent).
func convertMCPHandlers(handlers []mcp.ToolHandler) []tools.Handler {
	result := make([]tools.Handler, len(handlers))
	for i, h := range handlers {
		result[i] = tools.Handler{Def: h.Def, Call: h.Call}
	}
	return result
}

// connectMCPServers starts all configured MCP server subprocesses and
// performs the protocol handshake. Returns connected clients and any error.
// On error, already-connected clients are still returned for cleanup.
func connectMCPServers(ctx context.Context, servers []mcp.ServerConfig) ([]*mcp.Client, error) {
	var clients []*mcp.Client
	for _, cfg := range servers {
		mcp.PreflightServer(ctx, cfg)
		logging.InfoContext(ctx, "connecting MCP server %q (%s)", cfg.Name, cfg.ConnectString())
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
		if err := c.Close(); err != nil {
			logging.Warn("MCP server %q shutdown: %v", c.Name(), err)
		}
	}
}
