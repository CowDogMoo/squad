// Package responses integrates OpenAI's Responses API workflows.
package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/tools"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	oairesponses "github.com/openai/openai-go/v3/responses"
	"github.com/tmc/langchaingo/llms"
)

// FunctionCall holds the parsed details of a function_call item
// from the Responses API output.
type FunctionCall struct {
	ID        string
	CallID    string
	Name      string
	Arguments string
}

// Config bundles the immutable parameters for a Responses API session.
type Config struct {
	Model             string
	Tools             []oairesponses.ToolUnionParam
	Temperature       float64
	MaxTokens         int
	Instructions      string
	ReasoningPrefixes []string
}

// DefaultMaxOutputTokens is the fallback output-token budget for reasoning
// models (gpt-5*) when the caller does not specify --max-tokens.  The API
// default (1024) is far too small — reasoning tokens compete with the
// visible response, so the model exhausts its budget on thinking alone.
const DefaultMaxOutputTokens = 16384

func (rc *Config) applyOptionals(params *oairesponses.ResponseNewParams) {
	if rc.Temperature >= 0 && !IsReasoningModel(rc.Model, rc.ReasoningPrefixes) {
		params.Temperature = openai.Float(rc.Temperature)
	}
	switch {
	case rc.MaxTokens > 0:
		params.MaxOutputTokens = openai.Int(int64(rc.MaxTokens))
	case IsReasoningModel(rc.Model, rc.ReasoningPrefixes):
		params.MaxOutputTokens = openai.Int(DefaultMaxOutputTokens)
	}
}

// IsReasoningModel reports whether a model emits reasoning tokens
// that require a larger output-token budget. It checks the model name
// against the configured reasoning prefixes (e.g. ["gpt-5"]).
func IsReasoningModel(model string, prefixes []string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, p := range prefixes {
		if strings.HasPrefix(m, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// UseResponsesAPI reports whether the Responses API path should be used.
func UseResponsesAPI(provider, model string, prefixes []string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "openai-responses" {
		return true
	}
	return provider == "openai" && IsReasoningModel(model, prefixes)
}

// RunWithTools drives a tool-calling loop using the OpenAI Responses API.
func RunWithTools(ctx context.Context, apiKey, baseURL, model, systemPrompt, userPrompt, workingDir, organization string, temperature float64, maxTokens, maxIterations int, reasoningPrefixes []string, taskCfg *tools.TaskConfig, m *metrics.Metrics, ex executor.Executor) (string, error) {
	client := newClient(apiKey, baseURL, organization)
	handlers, toolDefs := tools.BuildHandlers(workingDir, taskCfg, ex)
	if maxIterations <= 0 {
		maxIterations = tools.MaxToolIterations
	}
	rc := Config{
		Model:             model,
		Tools:             ConvertTools(toolDefs),
		Temperature:       temperature,
		MaxTokens:         maxTokens,
		Instructions:      systemPrompt,
		ReasoningPrefixes: reasoningPrefixes,
	}

	params := oairesponses.ResponseNewParams{
		Model:        model,
		Instructions: openai.String(systemPrompt),
		Input: oairesponses.ResponseNewParamsInputUnion{
			OfString: openai.String(userPrompt),
		},
		Tools:      rc.Tools,
		Truncation: oairesponses.ResponseNewParamsTruncation("auto"),
	}
	rc.applyOptionals(&params)

	logging.InfoContext(ctx, "responses API: initial request (model=%s)", model)
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		logAPIError(ctx, err, "initial request")
		return "", fmt.Errorf("responses API call failed: %w", err)
	}
	logOutputItems(ctx, resp, "initial")
	trackResponseMetrics(resp, m)

	resp, text, err := toolLoop(ctx, client, resp, handlers, &rc, maxIterations, m)
	if err != nil {
		return text, err
	}
	if text != "" {
		return text, nil
	}

	// If the loop exited with pending function calls, resolve them with
	// dummy outputs so PreviousResponseID chaining doesn't fail.
	resp, text, err = resolvePendingCalls(ctx, client, resp, &rc, m)
	if err != nil {
		return "", fmt.Errorf("responses API: resolvePendingCalls failed: %w", err)
	}
	if text != "" {
		return text, nil
	}

	finalText, finalErr := requestFinal(ctx, client, resp.ID, systemPrompt, &rc, m)
	if finalErr == nil && finalText != "" {
		return finalText, nil
	}
	if finalErr != nil {
		logging.DebugContext(ctx, "responses API: requestFinal error: %v", finalErr)
	}
	return "", fmt.Errorf("responses API: no usable text after tool loop (OutputText empty, requestFinal failed: %v)", finalErr)
}

// trackResponseMetrics extracts token usage from a response and adds it to metrics.
func trackResponseMetrics(resp *oairesponses.Response, m *metrics.Metrics) {
	if m == nil || resp == nil {
		return
	}
	m.IncrementIterations()
	if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
		m.AddTokens(resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}
}

func newClient(apiKey, baseURL, organization string) openai.Client {
	clientOpts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if organization != "" {
		clientOpts = append(clientOpts, option.WithOrganization(organization))
	}
	if baseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(baseURL))
	}
	return openai.NewClient(clientOpts...)
}

func toolLoop(ctx context.Context, client openai.Client, resp *oairesponses.Response, handlers map[string]tools.Handler, rc *Config, maxIter int, m *metrics.Metrics) (*oairesponses.Response, string, error) {
	var repeat tools.RepeatTracker
	for i := 0; i < maxIter; i++ {
		calls := ExtractFunctionCalls(resp)
		if len(calls) == 0 {
			text := resp.OutputText()
			logging.InfoContext(ctx, "responses API: final response at iteration %d (%d bytes)", i, len(text))
			if text == "" {
				logOutputItems(ctx, resp, fmt.Sprintf("empty-text-iter-%d", i))
			}
			return resp, text, nil
		}

		logging.InfoContext(ctx, "responses API: iteration %d with %d tool call(s)", i+1, len(calls))
		if checkRepeat(ctx, &repeat, calls) {
			break
		}

		outputs := executeAndBuildOutputs(ctx, calls, handlers)
		params := oairesponses.ResponseNewParams{
			Model:              rc.Model,
			PreviousResponseID: openai.String(resp.ID),
			Instructions:       openai.String(rc.Instructions),
			Input: oairesponses.ResponseNewParamsInputUnion{
				OfInputItemList: oairesponses.ResponseInputParam(outputs),
			},
			Tools:      rc.Tools,
			Truncation: oairesponses.ResponseNewParamsTruncation("auto"),
		}
		rc.applyOptionals(&params)

		var err error
		resp, err = client.Responses.New(ctx, params)
		if err != nil {
			logAPIError(ctx, err, fmt.Sprintf("follow-up iteration %d", i+1))
			return resp, "", fmt.Errorf("responses API follow-up failed at iteration %d: %w", i+1, err)
		}
		logOutputItems(ctx, resp, fmt.Sprintf("follow-up-iter-%d", i+1))
		trackResponseMetrics(resp, m)

		if m != nil && m.BudgetExceeded() {
			logging.InfoContext(ctx, "responses API: budget exceeded ($%.4f >= $%.4f max), stopping", m.TotalCostWithChildren(), m.MaxCost)
			text := resp.OutputText()
			return resp, text, metrics.ErrBudgetExceeded
		}
	}

	text := resp.OutputText()
	if text != "" {
		logging.InfoContext(ctx, "responses API: loop ended, returning partial text (%d bytes)", len(text))
	}
	return resp, text, nil
}

func checkRepeat(ctx context.Context, repeat *tools.RepeatTracker, calls []FunctionCall) bool {
	fakeToolCalls := make([]llms.ToolCall, len(calls))
	for j, c := range calls {
		fakeToolCalls[j] = llms.ToolCall{
			FunctionCall: &llms.FunctionCall{
				Name:      c.Name,
				Arguments: c.Arguments,
			},
		}
	}
	repeat.Update(fakeToolCalls)
	if repeat.Exceeded() {
		logging.InfoContext(ctx, "responses API: %s called %d times in a row, breaking", repeat.LastName, repeat.Count)
		return true
	}
	return false
}

func requestFinal(ctx context.Context, client openai.Client, previousID, systemPrompt string, rc *Config, m *metrics.Metrics) (string, error) {
	if m != nil && m.BudgetExceeded() {
		logging.InfoContext(ctx, "responses API: budget exceeded, skipping final call")
		return "", metrics.ErrBudgetExceeded
	}

	if strings.TrimSpace(previousID) == "" {
		return "", fmt.Errorf("missing previous response id")
	}
	params := oairesponses.ResponseNewParams{
		Model:              rc.Model,
		PreviousResponseID: openai.String(previousID),
		Input: oairesponses.ResponseNewParamsInputUnion{
			OfString: openai.String("Provide the final response. Do not call any tools."),
		},
		Instructions: openai.String(systemPrompt),
		ToolChoice: oairesponses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: openai.Opt(oairesponses.ToolChoiceOptionsNone),
		},
		Tools:      rc.Tools,
		Truncation: oairesponses.ResponseNewParamsTruncation("auto"),
	}
	rc.applyOptionals(&params)

	logging.InfoContext(ctx, "responses API: requesting final response with tool_choice=none")
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		logAPIError(ctx, err, "final request")
		return "", fmt.Errorf("responses API final call failed: %w", err)
	}
	logOutputItems(ctx, resp, "final-call")
	trackResponseMetrics(resp, m)
	text := resp.OutputText()
	if text == "" {
		return "", fmt.Errorf("responses API final call returned empty text")
	}
	logging.InfoContext(ctx, "responses API: final call produced response (%d bytes)", len(text))
	return text, nil
}

// resolvePendingCalls checks whether resp contains unresolved function_call
// items and, if so, submits dummy outputs with ToolChoice=none so the
// conversation is in a clean state for subsequent chaining.
func resolvePendingCalls(ctx context.Context, client openai.Client, resp *oairesponses.Response, rc *Config, m *metrics.Metrics) (*oairesponses.Response, string, error) {
	calls := ExtractFunctionCalls(resp)
	if len(calls) == 0 {
		return resp, resp.OutputText(), nil
	}

	logging.InfoContext(ctx, "responses API: resolving %d pending call(s) after iteration budget exhausted", len(calls))

	outputs := make([]oairesponses.ResponseInputItemUnionParam, 0, len(calls))
	for _, call := range calls {
		outputs = append(outputs, oairesponses.ResponseInputItemUnionParam{
			OfFunctionCallOutput: &oairesponses.ResponseInputItemFunctionCallOutputParam{
				CallID: call.CallID,
				Output: oairesponses.ResponseInputItemFunctionCallOutputOutputUnionParam{
					OfString: openai.String("Tool call skipped: iteration budget exhausted."),
				},
			},
		})
	}

	params := oairesponses.ResponseNewParams{
		Model:              rc.Model,
		PreviousResponseID: openai.String(resp.ID),
		Instructions:       openai.String(rc.Instructions),
		Input: oairesponses.ResponseNewParamsInputUnion{
			OfInputItemList: oairesponses.ResponseInputParam(outputs),
		},
		ToolChoice: oairesponses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: openai.Opt(oairesponses.ToolChoiceOptionsNone),
		},
		Tools:      rc.Tools,
		Truncation: oairesponses.ResponseNewParamsTruncation("auto"),
	}
	rc.applyOptionals(&params)

	resolved, err := client.Responses.New(ctx, params)
	if err != nil {
		return resp, "", fmt.Errorf("resolve pending calls failed: %w", err)
	}
	logOutputItems(ctx, resolved, "resolve-pending")
	trackResponseMetrics(resolved, m)
	return resolved, resolved.OutputText(), nil
}

// ConvertTools converts langchaingo tool definitions to the
// Responses API FunctionToolParam format.
func ConvertTools(toolDefs []llms.Tool) []oairesponses.ToolUnionParam {
	out := make([]oairesponses.ToolUnionParam, 0, len(toolDefs))
	for _, t := range toolDefs {
		if t.Function == nil {
			continue
		}
		params, ok := t.Function.Parameters.(map[string]any)
		if !ok {
			params = map[string]any{}
		}
		out = append(out, oairesponses.ToolUnionParam{
			OfFunction: &oairesponses.FunctionToolParam{
				Name:        t.Function.Name,
				Description: openai.String(t.Function.Description),
				Parameters:  params,
				Strict:      openai.Bool(false),
			},
		})
	}
	return out
}

// ExtractFunctionCalls pulls function_call items from a Responses API response.
func ExtractFunctionCalls(resp *oairesponses.Response) []FunctionCall {
	if resp == nil || resp.Output == nil {
		return nil
	}
	var calls []FunctionCall
	for _, item := range resp.Output {
		if item.Type == "function_call" {
			calls = append(calls, FunctionCall{
				ID:        item.ID,
				CallID:    item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
		}
	}
	return calls
}

// logOutputItems dumps the type, status, and a content preview for every
// item in a Responses API response.  This is the primary diagnostic for
// "model returned 0 bytes" issues — it shows whether the model produced
// reasoning tokens, message items, or something else entirely.
func logOutputItems(ctx context.Context, resp *oairesponses.Response, label string) {
	if resp == nil {
		logging.DebugContext(ctx, "responses API [%s]: resp is nil", label)
		return
	}
	logging.DebugContext(ctx, "responses API [%s]: response id=%s, status=%s, output items=%d",
		label, resp.ID, resp.Status, len(resp.Output))
	if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
		logging.DebugContext(ctx, "responses API [%s]: usage input_tokens=%d output_tokens=%d",
			label, resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}
	for i, item := range resp.Output {
		raw, _ := json.Marshal(item)
		preview := string(raw)
		if len(preview) > 500 {
			preview = preview[:500] + "...(truncated)"
		}
		logging.DebugContext(ctx, "responses API [%s]: output[%d] type=%s: %s",
			label, i, item.Type, preview)
	}
}

// logAPIError logs structured context about a Responses API failure.
// The OpenAI SDK already retries transient errors, so this only adds
// diagnostic information to help operators understand what happened.
func logAPIError(ctx context.Context, err error, label string) {
	if err == nil {
		return
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate limit"):
		logging.InfoContext(ctx, "responses API %s: rate limited — SDK will auto-retry", label)
	case strings.Contains(lower, "500") || strings.Contains(lower, "internal server error"):
		logging.InfoContext(ctx, "responses API %s: server error (500) — SDK will auto-retry", label)
	case strings.Contains(lower, "503") || strings.Contains(lower, "service unavailable") || strings.Contains(lower, "overloaded"):
		logging.InfoContext(ctx, "responses API %s: service unavailable (503) — SDK will auto-retry", label)
	case strings.Contains(lower, "401") || strings.Contains(lower, "authentication"):
		logging.InfoContext(ctx, "responses API %s: authentication failure — check API key", label)
	case strings.Contains(lower, "context canceled"):
		logging.InfoContext(ctx, "responses API %s: context canceled", label)
	default:
		logging.InfoContext(ctx, "responses API %s: %v", label, err)
	}
}

func executeAndBuildOutputs(ctx context.Context, calls []FunctionCall, handlers map[string]tools.Handler) []oairesponses.ResponseInputItemUnionParam {
	outputs := make([]oairesponses.ResponseInputItemUnionParam, 0, len(calls))
	for _, call := range calls {
		handler, ok := handlers[call.Name]
		if !ok {
			logging.DebugContext(ctx, "responses API: unknown tool %s", call.Name)
			outputs = append(outputs, oairesponses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &oairesponses.ResponseInputItemFunctionCallOutputParam{
					CallID: call.CallID,
					Output: oairesponses.ResponseInputItemFunctionCallOutputOutputUnionParam{
						OfString: openai.String(fmt.Sprintf("unknown tool: %s", call.Name)),
					},
				},
			})
			continue
		}

		logging.InfoContext(ctx, "responses API: calling %s args=%s", call.Name, tools.TruncateString(call.Arguments, 200))
		toolStart := time.Now()
		result, err := handler.Call(ctx, []byte(call.Arguments))
		toolDuration := time.Since(toolStart)

		var output string
		if err != nil {
			// Include both result (e.g., command output) and error message
			if result != "" {
				output = fmt.Sprintf("%s\n\nerror: %v", result, err)
				logging.InfoContext(ctx, "responses API: %s failed in %s: %v (output: %d bytes)", call.Name, toolDuration.Round(time.Millisecond), err, len(result))
			} else {
				output = fmt.Sprintf("error: %v", err)
				logging.InfoContext(ctx, "responses API: %s failed in %s: %v (no output)", call.Name, toolDuration.Round(time.Millisecond), err)
			}
		} else {
			output = result
			logging.InfoContext(ctx, "responses API: %s completed in %s (%d bytes)", call.Name, toolDuration.Round(time.Millisecond), len(result))
		}

		output = tools.TruncateToolOutputHeadTail(output, 32*1024)

		outputs = append(outputs, oairesponses.ResponseInputItemUnionParam{
			OfFunctionCallOutput: &oairesponses.ResponseInputItemFunctionCallOutputParam{
				CallID: call.CallID,
				Output: oairesponses.ResponseInputItemFunctionCallOutputOutputUnionParam{
					OfString: openai.String(output),
				},
			},
		})
	}
	return outputs
}
