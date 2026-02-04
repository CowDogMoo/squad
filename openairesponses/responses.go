package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/tools"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
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
	Model       string
	Tools       []responses.ToolUnionParam
	Temperature float64
	MaxTokens   int
}

func (rc *Config) applyOptionals(params *responses.ResponseNewParams) {
	if rc.Temperature >= 0 && supportsTemperature(rc.Model) {
		params.Temperature = openai.Float(rc.Temperature)
	}
	if rc.MaxTokens > 0 {
		params.MaxOutputTokens = openai.Int(int64(rc.MaxTokens))
	}
}

// UseResponsesAPI returns true when the Responses API path should be used.
func UseResponsesAPI(provider, model string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "openai-responses" {
		return true
	}
	return provider == "openai" && strings.HasPrefix(model, "gpt-5")
}

// RunWithTools drives a tool-calling loop using the OpenAI Responses API.
func RunWithTools(ctx context.Context, apiKey, baseURL, model, systemPrompt, userPrompt, workingDir, organization string, temperature float64, maxTokens int) (string, error) {
	client := newClient(apiKey, baseURL, organization)
	handlers, toolDefs := tools.BuildHandlers(workingDir)
	rc := Config{
		Model:       model,
		Tools:       ConvertTools(toolDefs),
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	params := responses.ResponseNewParams{
		Model:        model,
		Instructions: openai.String(systemPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userPrompt),
		},
		Tools:      rc.Tools,
		Truncation: responses.ResponseNewParamsTruncation("auto"),
		ToolChoice: responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptionsRequired),
		},
	}
	rc.applyOptionals(&params)

	logging.InfoContext(ctx, "responses API: initial request (model=%s)", model)
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("responses API call failed: %w", err)
	}
	logOutputItems(ctx, resp, "initial")

	resp, text, err := toolLoop(ctx, client, resp, handlers, &rc)
	if err != nil {
		return text, err
	}
	if text != "" {
		return text, nil
	}

	finalText, finalErr := requestFinal(ctx, client, resp.ID, systemPrompt, &rc)
	if finalErr == nil && finalText != "" {
		return finalText, nil
	}
	if finalErr != nil {
		logging.DebugContext(ctx, "responses API: requestFinal error: %v", finalErr)
	}
	return "", fmt.Errorf("responses API: no usable text after tool loop (OutputText empty, requestFinal failed: %v)", finalErr)
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

func toolLoop(ctx context.Context, client openai.Client, resp *responses.Response, handlers map[string]tools.Handler, rc *Config) (*responses.Response, string, error) {
	var repeat tools.RepeatTracker
	for i := 0; i < tools.MaxToolIterations; i++ {
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
		params := responses.ResponseNewParams{
			Model:              rc.Model,
			PreviousResponseID: openai.String(resp.ID),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: responses.ResponseInputParam(outputs),
			},
			Tools:      rc.Tools,
			Truncation: responses.ResponseNewParamsTruncation("auto"),
		}
		rc.applyOptionals(&params)

		var err error
		resp, err = client.Responses.New(ctx, params)
		if err != nil {
			return resp, "", fmt.Errorf("responses API follow-up failed at iteration %d: %w", i+1, err)
		}
		logOutputItems(ctx, resp, fmt.Sprintf("follow-up-iter-%d", i+1))
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

func requestFinal(ctx context.Context, client openai.Client, previousID, systemPrompt string, rc *Config) (string, error) {
	if strings.TrimSpace(previousID) == "" {
		return "", fmt.Errorf("missing previous response id")
	}
	params := responses.ResponseNewParams{
		Model:              rc.Model,
		PreviousResponseID: openai.String(previousID),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("Provide the final response. Do not call any tools."),
		},
		Instructions: openai.String(systemPrompt),
		ToolChoice: responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptionsNone),
		},
		Tools:      rc.Tools,
		Truncation: responses.ResponseNewParamsTruncation("auto"),
	}
	rc.applyOptionals(&params)

	logging.InfoContext(ctx, "responses API: requesting final response with tool_choice=none")
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("responses API final call failed: %w", err)
	}
	logOutputItems(ctx, resp, "final-call")
	text := resp.OutputText()
	if text == "" {
		return "", fmt.Errorf("responses API final call returned empty text")
	}
	logging.InfoContext(ctx, "responses API: final call produced response (%d bytes)", len(text))
	return text, nil
}

func supportsTemperature(model string) bool {
	return !strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-5")
}

// ConvertTools converts langchaingo tool definitions to the
// Responses API FunctionToolParam format.
func ConvertTools(toolDefs []llms.Tool) []responses.ToolUnionParam {
	out := make([]responses.ToolUnionParam, 0, len(toolDefs))
	for _, t := range toolDefs {
		if t.Function == nil {
			continue
		}
		params, ok := t.Function.Parameters.(map[string]any)
		if !ok {
			params = map[string]any{}
		}
		out = append(out, responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
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
func ExtractFunctionCalls(resp *responses.Response) []FunctionCall {
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
func logOutputItems(ctx context.Context, resp *responses.Response, label string) {
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

func executeAndBuildOutputs(ctx context.Context, calls []FunctionCall, handlers map[string]tools.Handler) []responses.ResponseInputItemUnionParam {
	outputs := make([]responses.ResponseInputItemUnionParam, 0, len(calls))
	for _, call := range calls {
		handler, ok := handlers[call.Name]
		if !ok {
			logging.DebugContext(ctx, "responses API: unknown tool %s", call.Name)
			outputs = append(outputs, responses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
					CallID: call.CallID,
					Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
						OfString: openai.String(fmt.Sprintf("unknown tool: %s", call.Name)),
					},
				},
			})
			continue
		}

		logging.DebugContext(ctx, "responses API: calling %s args=%s", call.Name, tools.TruncateString(call.Arguments, 200))
		toolStart := time.Now()
		result, err := handler.Call(ctx, []byte(call.Arguments))
		toolDuration := time.Since(toolStart)

		var output string
		if err != nil {
			output = fmt.Sprintf("error: %v", err)
			logging.DebugContext(ctx, "responses API: %s failed in %s: %v", call.Name, toolDuration.Round(time.Millisecond), err)
		} else {
			output = result
			logging.DebugContext(ctx, "responses API: %s completed in %s (%d bytes)", call.Name, toolDuration.Round(time.Millisecond), len(result))
		}

		outputs = append(outputs, responses.ResponseInputItemUnionParam{
			OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
				CallID: call.CallID,
				Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
					OfString: openai.String(output),
				},
			},
		})
	}
	return outputs
}
