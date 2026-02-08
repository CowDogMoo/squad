package runner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/ollama"
	"github.com/cowdogmoo/squad/responses"
	"github.com/cowdogmoo/squad/tools"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/openai"
)

// invokeModel resolves provider settings and calls the appropriate model backend.
func invokeModel(ctx context.Context, opts *RunOptions, bundle *agent.Bundle) (string, *metrics.Metrics, error) {
	provider := normalizeProvider(opts.Provider)
	model := opts.Model
	temperature := opts.Temperature
	maxTokens := opts.MaxTokens

	systemPrompt := bundle.System
	if opts.System != "" {
		systemPrompt += "\n\n## System Override\n\n" + strings.TrimSpace(opts.System) + "\n"
	}

	taskCfg := buildTaskConfig(opts)
	return callModel(ctx, opts, provider, model, systemPrompt, bundle, temperature, maxTokens, taskCfg)
}

// callModel dispatches the prompt to the appropriate model backend and returns the response.
func callModel(ctx context.Context, opts *RunOptions, provider, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int, taskCfg *tools.TaskConfig) (string, *metrics.Metrics, error) {
	if responses.UseResponsesAPI(provider, model) {
		return callResponsesAPI(ctx, opts, model, systemPrompt, bundle, temperature, maxTokens, taskCfg)
	}
	return callLangChainLLM(ctx, opts, provider, model, systemPrompt, bundle, temperature, maxTokens, taskCfg)
}

// callResponsesAPI runs the prompt via the OpenAI Responses API.
func callResponsesAPI(ctx context.Context, opts *RunOptions, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int, taskCfg *tools.TaskConfig) (string, *metrics.Metrics, error) {
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
	if responses.IsReasoningModel(model) && maxTokens < responses.DefaultMaxOutputTokens {
		logging.InfoContext(ctx, "raising max_output_tokens %d → %d for reasoning model %s", maxTokens, responses.DefaultMaxOutputTokens, model)
		maxTokens = responses.DefaultMaxOutputTokens
	}

	provider := "openai"
	if responses.UseResponsesAPI(opts.Provider, model) && opts.Provider == "openai-responses" {
		provider = "openai-responses"
	}

	m := metrics.New(provider, model)
	logging.InfoContext(ctx, "model call started via Responses API (model=%s)", model)
	response, err := responses.RunWithTools(ctx, apiKey, opts.BaseURL, model, systemPrompt, bundle.User, bundle.WorkDir, opts.Org, temperature, maxTokens, opts.MaxIterations, taskCfg, m)
	m.Finish()
	if err != nil {
		return "", m, fmt.Errorf("model call failed: %w", err)
	}
	logging.InfoContext(ctx, "model call finished in %s (response-bytes=%d)", m.Duration().Round(time.Millisecond), len(response))
	return response, m, nil
}

// callLangChainLLM runs the prompt via a LangChain-compatible LLM.
func callLangChainLLM(ctx context.Context, opts *RunOptions, provider, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int, taskCfg *tools.TaskConfig) (string, *metrics.Metrics, error) {
	llm, err := buildLLM(opts, provider, model)
	if err != nil {
		return "", nil, err
	}

	callOpts := buildCallOpts(opts, provider, temperature, maxTokens)

	m := metrics.New(provider, model)
	logging.InfoContext(ctx, "model call started (provider=%s model=%s)", provider, model)
	response, err := tools.RunWithTools(ctx, llm, systemPrompt, bundle.User, bundle.WorkDir, opts.MaxIterations, taskCfg, m, callOpts...)
	m.Finish()
	if err != nil {
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

// buildTaskConfig creates a TaskConfig for the Task tool from RunOptions.
func buildTaskConfig(opts *RunOptions) *tools.TaskConfig {
	return &tools.TaskConfig{
		AgentsDir:     opts.AgentsDir,
		WorkingDir:    opts.WorkingDir,
		MaxIterations: opts.MaxIterations,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			childBundle, err := agent.BuildBundle(agentsDir, agentName, prompt, workingDir, mode)
			if err != nil {
				return "", nil, fmt.Errorf("failed to build child agent bundle: %w", err)
			}

			childOpts := *opts
			childOpts.Agent = agentName
			childOpts.AgentsDir = agentsDir
			childOpts.WorkingDir = workingDir
			childOpts.Mode = mode
			childOpts.System = ""

			return invokeModel(ctx, &childOpts, childBundle)
		},
	}
}
