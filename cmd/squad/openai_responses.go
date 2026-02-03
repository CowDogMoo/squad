package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/logging"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/tmc/langchaingo/llms"
)

// responseFunctionCall holds the parsed details of a function_call item
// from the Responses API output.
type responseFunctionCall struct {
	ID        string // output item ID
	CallID    string // unique call ID for pairing with output
	Name      string // function name
	Arguments string // JSON arguments string
}

// responsesConfig bundles the immutable parameters for a Responses API session.
type responsesConfig struct {
	model       string
	tools       []responses.ToolUnionParam
	temperature float64
	maxTokens   int
}

func (rc *responsesConfig) applyOptionals(params *responses.ResponseNewParams) {
	if rc.temperature >= 0 && supportsResponsesTemperature(rc.model) {
		params.Temperature = openai.Float(rc.temperature)
	}
	if rc.maxTokens > 0 {
		params.MaxOutputTokens = openai.Int(int64(rc.maxTokens))
	}
}

// useResponsesAPI returns true when the Responses API path should be used
// instead of LangChainGo's Chat Completions.
func useResponsesAPI(provider, model string) bool {
	provider = normalizeProvider(provider)
	if provider == "openai-responses" {
		return true
	}
	return provider == "openai" && strings.HasPrefix(model, "gpt-5")
}

// runWithToolsResponses drives a tool-calling loop using the OpenAI
// Responses API. It reuses the same tool handlers from buildToolHandlers.
func runWithToolsResponses(ctx context.Context, apiKey, baseURL, model, systemPrompt, userPrompt, workingDir, organization string, temperature float64, maxTokens int) (string, error) {
	client := newResponsesClient(apiKey, baseURL, organization)
	handlers, toolDefs := buildToolHandlers(workingDir)
	rc := responsesConfig{
		model:       model,
		tools:       convertToolsToResponses(toolDefs),
		temperature: temperature,
		maxTokens:   maxTokens,
	}

	params := responses.ResponseNewParams{
		Model:        model,
		Instructions: openai.String(systemPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userPrompt),
		},
		Tools:      rc.tools,
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

	resp, text, err := responsesToolLoop(ctx, client, resp, handlers, &rc)
	if err != nil {
		return text, err
	}
	if text != "" {
		return text, nil
	}

	finalText, err := requestResponsesFinal(ctx, client, resp.ID, systemPrompt, &rc)
	if err == nil && finalText != "" {
		return finalText, nil
	}
	return "", fmt.Errorf("responses API tool loop ended after %d iterations with no usable response", maxToolIterations)
}

func newResponsesClient(apiKey, baseURL, organization string) openai.Client {
	clientOpts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if organization != "" {
		clientOpts = append(clientOpts, option.WithOrganization(organization))
	}
	if baseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(baseURL))
	}
	return openai.NewClient(clientOpts...)
}

// responsesToolLoop iterates the tool-calling loop, returning the final
// response, any accumulated text, and an error.
func responsesToolLoop(ctx context.Context, client openai.Client, resp *responses.Response, handlers map[string]toolHandler, rc *responsesConfig) (*responses.Response, string, error) {
	var repeat toolRepeatTracker
	for i := 0; i < maxToolIterations; i++ {
		calls := extractFunctionCalls(resp)
		if len(calls) == 0 {
			text := resp.OutputText()
			logging.InfoContext(ctx, "responses API: final response at iteration %d (%d bytes)", i, len(text))
			return resp, text, nil
		}

		logging.InfoContext(ctx, "responses API: iteration %d with %d tool call(s)", i+1, len(calls))
		if checkResponsesRepeat(&repeat, calls, ctx) {
			break
		}

		outputs := executeAndBuildOutputs(ctx, calls, handlers)
		params := responses.ResponseNewParams{
			Model:              rc.model,
			PreviousResponseID: openai.String(resp.ID),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: responses.ResponseInputParam(outputs),
			},
			Tools:      rc.tools,
			Truncation: responses.ResponseNewParamsTruncation("auto"),
		}
		rc.applyOptionals(&params)

		var err error
		resp, err = client.Responses.New(ctx, params)
		if err != nil {
			return resp, "", fmt.Errorf("responses API follow-up failed at iteration %d: %w", i+1, err)
		}
	}

	text := resp.OutputText()
	if text != "" {
		logging.InfoContext(ctx, "responses API: loop ended, returning partial text (%d bytes)", len(text))
	}
	return resp, text, nil
}

func checkResponsesRepeat(repeat *toolRepeatTracker, calls []responseFunctionCall, ctx context.Context) bool {
	fakeToolCalls := make([]llms.ToolCall, len(calls))
	for j, c := range calls {
		fakeToolCalls[j] = llms.ToolCall{
			FunctionCall: &llms.FunctionCall{Name: c.Name},
		}
	}
	repeat.update(fakeToolCalls)
	if repeat.exceeded() {
		logging.InfoContext(ctx, "responses API: %s called %d times in a row, breaking", repeat.lastName, repeat.count)
		return true
	}
	return false
}

func requestResponsesFinal(ctx context.Context, client openai.Client, previousID, systemPrompt string, rc *responsesConfig) (string, error) {
	if strings.TrimSpace(previousID) == "" {
		return "", fmt.Errorf("missing previous response id")
	}
	params := responses.ResponseNewParams{
		Model:              rc.model,
		PreviousResponseID: openai.String(previousID),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("Provide the final response. Do not call any tools."),
		},
		Instructions: openai.String(systemPrompt),
		ToolChoice: responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptionsNone),
		},
		Tools:      rc.tools,
		Truncation: responses.ResponseNewParamsTruncation("auto"),
	}
	rc.applyOptionals(&params)

	logging.InfoContext(ctx, "responses API: requesting final response with tool_choice=none")
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("responses API final call failed: %w", err)
	}
	text := resp.OutputText()
	if text == "" {
		return "", fmt.Errorf("responses API final call returned empty text")
	}
	logging.InfoContext(ctx, "responses API: final call produced response (%d bytes)", len(text))
	return text, nil
}

func supportsResponsesTemperature(model string) bool {
	return !strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-5")
}

// convertToolsToResponses converts langchaingo tool definitions to the
// Responses API FunctionToolParam format.
func convertToolsToResponses(tools []llms.Tool) []responses.ToolUnionParam {
	out := make([]responses.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
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

// extractFunctionCalls pulls function_call items from a Responses API
// response output.
func extractFunctionCalls(resp *responses.Response) []responseFunctionCall {
	if resp == nil {
		return nil
	}
	var calls []responseFunctionCall
	for _, item := range resp.Output {
		if item.Type == "function_call" {
			calls = append(calls, responseFunctionCall{
				ID:        item.ID,
				CallID:    item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
		}
	}
	return calls
}

// executeAndBuildOutputs runs each tool call through the existing handlers
// and builds the input items for the next Responses API request.
func executeAndBuildOutputs(ctx context.Context, calls []responseFunctionCall, handlers map[string]toolHandler) []responses.ResponseInputItemUnionParam {
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

		logging.DebugContext(ctx, "responses API: calling %s args=%s", call.Name, truncateString(call.Arguments, 200))
		toolStart := time.Now()
		result, err := handler.call(ctx, []byte(call.Arguments))
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

// resolveResponsesAPIKey determines the API key for the Responses API path,
// checking CLI flag, config, and environment in order.
func resolveResponsesAPIKey(cfgToken string) (string, error) {
	key := pickString(runAPIKey, cfgToken)
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	if key == "" {
		return "", fmt.Errorf("API key required for OpenAI Responses API: use --api-key, config provider.token, or OPENAI_API_KEY env var")
	}
	return key, nil
}
