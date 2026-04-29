// Package responses integrates OpenAI's Responses API workflows.
package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/telemetry"
	"github.com/cowdogmoo/squad/tools"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	oairesponses "github.com/openai/openai-go/v3/responses"
	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
// resumeResponseID, if non-empty, chains the initial request via
// PreviousResponseID so a prior session can be continued server-side.
func RunWithTools(ctx context.Context, apiKey, baseURL, model, systemPrompt, userPrompt, workingDir, organization, resumeResponseID string, temperature float64, maxTokens, maxIterations, editDeadline int, reasoningPrefixes []string, taskCfg *tools.TaskConfig, m *metrics.Metrics, ex executor.Executor) (string, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "responses.tool_loop",
		trace.WithAttributes(
			attribute.String("gen_ai.request.model", model),
			attribute.Int("squad.tool_loop.max_iterations", maxIterations),
		),
	)
	defer span.End()

	client := newClient(apiKey, baseURL, organization)
	handlers, toolDefs := tools.BuildHandlers(workingDir, taskCfg, ex)
	registerLargeResultTool(ctx, handlers, &toolDefs)
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

	logger := session.FromContext(ctx)
	logEvent(logger, session.EventPrompt, map[string]any{
		"role":          "user",
		"bytes":         len(userPrompt),
		"resumed_chain": resumeResponseID != "",
	})

	params := oairesponses.ResponseNewParams{
		Model:        model,
		Instructions: openai.String(systemPrompt),
		Input: oairesponses.ResponseNewParamsInputUnion{
			OfString: openai.String(userPrompt),
		},
		Tools:      rc.Tools,
		Truncation: oairesponses.ResponseNewParamsTruncation("auto"),
	}
	if resumeResponseID != "" {
		params.PreviousResponseID = openai.String(resumeResponseID)
	}
	rc.applyOptionals(&params)

	logging.InfoContext(ctx, "responses API: initial request (model=%s, resumed=%v)", model, resumeResponseID != "")
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		logAPIError(ctx, err, "initial request")
		logEvent(logger, session.EventError, map[string]any{"phase": "initial", "error": err.Error()})
		return "", fmt.Errorf("responses API call failed: %w", err)
	}
	logOutputItems(ctx, resp, "initial")
	trackResponseMetrics(resp, m)
	recordResponse(logger, resp, "initial")

	resp, text, err := toolLoop(ctx, client, resp, handlers, &rc, maxIterations, editDeadline, m)
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

func toolLoop(ctx context.Context, client openai.Client, resp *oairesponses.Response, handlers map[string]tools.Handler, rc *Config, maxIter, editDeadline int, m *metrics.Metrics) (*oairesponses.Response, string, error) {
	var repeat tools.RepeatTracker
	editEnforcer := tools.NewEditEnforcer(editDeadline)
	logger := session.FromContext(ctx)
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

		logEvent(logger, session.EventIteration, map[string]any{"index": i + 1, "tool_calls": len(calls)})
		logging.InfoContext(ctx, "responses API: iteration %d with %d tool call(s)", i+1, len(calls))
		if checkRepeat(ctx, &repeat, calls) {
			break
		}

		// Check edit deadline enforcement.
		if editEnforcer != nil {
			var toolNames []string
			for _, c := range calls {
				toolNames = append(toolNames, c.Name)
			}
			if editEnforcer.CheckNames(toolNames) {
				logging.InfoContext(ctx, "responses API: edit deadline reached: %d iterations with no Edit calls, stopping", editEnforcer.Deadline)
				tools.MarkEditDeadlineReached(ctx)
				text := resp.OutputText()
				return resp, text, nil
			}
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
			logEvent(logger, session.EventError, map[string]any{"phase": "follow-up", "iteration": i + 1, "error": err.Error()})
			return resp, "", fmt.Errorf("responses API follow-up failed at iteration %d: %w", i+1, err)
		}
		logOutputItems(ctx, resp, fmt.Sprintf("follow-up-iter-%d", i+1))
		trackResponseMetrics(resp, m)
		recordResponse(logger, resp, fmt.Sprintf("iter-%d", i+1))

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

	var apiErr *oairesponses.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 429:
			logging.InfoContext(ctx, "responses API %s: rate limited — SDK will auto-retry", label)
		case 500:
			logging.InfoContext(ctx, "responses API %s: server error (500) — SDK will auto-retry", label)
		case 503:
			logging.InfoContext(ctx, "responses API %s: service unavailable (503) — SDK will auto-retry", label)
		case 401:
			logging.InfoContext(ctx, "responses API %s: authentication failure — check API key", label)
		default:
			logging.InfoContext(ctx, "responses API %s: HTTP %d — %s", label, apiErr.StatusCode, apiErr.Message)
		}
		return
	}

	if ctx.Err() != nil {
		logging.InfoContext(ctx, "responses API %s: context canceled", label)
		return
	}

	logging.InfoContext(ctx, "responses API %s: %v", label, err)
}

func executeAndBuildOutputs(ctx context.Context, calls []FunctionCall, handlers map[string]tools.Handler) []oairesponses.ResponseInputItemUnionParam {
	outputs := make([]oairesponses.ResponseInputItemUnionParam, 0, len(calls))
	logger := session.FromContext(ctx)
	for _, call := range calls {
		logEvent(logger, session.EventToolCall, map[string]any{
			"call_id": call.CallID,
			"name":    call.Name,
			"args":    tools.TruncateString(call.Arguments, 4096),
		})
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

		callCtx, toolSpan := telemetry.Tracer().Start(ctx, "tool."+call.Name,
			trace.WithAttributes(
				attribute.String("squad.tool.name", call.Name),
			),
		)

		logging.InfoContext(callCtx, "responses API: calling %s args=%s", call.Name, tools.TruncateString(call.Arguments, 200))
		toolStart := time.Now()
		result, err := handler.Call(callCtx, []byte(call.Arguments))
		toolDuration := time.Since(toolStart)

		var output string
		if err != nil {
			toolSpan.RecordError(err)
			toolSpan.SetStatus(codes.Error, err.Error())
			// Include both result (e.g., command output) and error message
			if result != "" {
				output = fmt.Sprintf("%s\n\nerror: %v", result, err)
				logging.InfoContext(callCtx, "responses API: %s failed in %s: %v (output: %d bytes)", call.Name, toolDuration.Round(time.Millisecond), err, len(result))
			} else {
				output = fmt.Sprintf("error: %v", err)
				logging.InfoContext(callCtx, "responses API: %s failed in %s: %v (no output)", call.Name, toolDuration.Round(time.Millisecond), err)
			}
		} else {
			output = result
			logging.InfoContext(callCtx, "responses API: %s completed in %s (%d bytes)", call.Name, toolDuration.Round(time.Millisecond), len(result))
		}
		toolSpan.SetAttributes(attribute.Int("squad.tool.output_bytes", len(output)))
		toolSpan.End()

		fullBytes := len(output)
		// get_tool_result must always pass through verbatim — its whole job is
		// to deliver content the model already knows is large.
		if logger != nil && call.Name != toolGetToolResult && fullBytes > session.LargeResultThreshold {
			if id, storeErr := logger.StoreLargeResult(output); storeErr == nil {
				logEvent(logger, session.EventLargeResult, map[string]any{
					"call_id":   call.CallID,
					"name":      call.Name,
					"result_id": id,
					"bytes":     fullBytes,
				})
				output = formatLargeResultPlaceholder(id, call.Name, fullBytes)
			} else {
				logging.Warn("session: failed to spill large result: %v", storeErr)
			}
		}

		output = tools.TruncateToolOutputHeadTail(output, 32*1024)

		logEvent(logger, session.EventToolResult, map[string]any{
			"call_id":     call.CallID,
			"name":        call.Name,
			"bytes":       fullBytes,
			"sent_bytes":  len(output),
			"duration_ms": toolDuration.Milliseconds(),
		})

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

// logEvent is a nil-safe helper for appending to the session log.
func logEvent(l *session.Logger, eventType string, payload any) {
	if l == nil {
		return
	}
	if err := l.Append(eventType, payload); err != nil {
		logging.Warn("session: append %s failed: %v", eventType, err)
	}
}

// recordResponse persists the response id and writes a session event with
// status + token usage.
func recordResponse(l *session.Logger, resp *oairesponses.Response, label string) {
	if l == nil || resp == nil {
		return
	}
	l.SetLastResponseID(resp.ID)
	logEvent(l, session.EventResponse, map[string]any{
		"label":         label,
		"id":            resp.ID,
		"status":        string(resp.Status),
		"output_items":  len(resp.Output),
		"input_tokens":  resp.Usage.InputTokens,
		"output_tokens": resp.Usage.OutputTokens,
	})
}

// formatLargeResultPlaceholder is the inline summary returned to the model
// when a tool result has been spilled to disk. The wording tells the model
// how to fetch the full bytes via get_tool_result.
func formatLargeResultPlaceholder(resultID, toolName string, totalBytes int) string {
	return fmt.Sprintf(
		"[result:%s — %d bytes from %s elided. Call get_tool_result(result_id=%q) to read the full content; pass offset/limit for paging.]",
		resultID, totalBytes, toolName, resultID,
	)
}
