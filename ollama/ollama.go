package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// LLM implements llms.Model using Ollama's native /api/chat endpoint,
// which supports both tool calling and the num_ctx parameter.
type LLM struct {
	serverURL string
	model     string
	numCtx    int
}

// Verify interface compliance.
var _ llms.Model = (*LLM)(nil)

// New creates a new Ollama LLM client.
func New(serverURL, model string, numCtx int) *LLM {
	serverURL = strings.TrimSuffix(serverURL, "/v1")
	serverURL = strings.TrimSuffix(serverURL, "/")
	return &LLM{
		serverURL: serverURL,
		model:     model,
		numCtx:    numCtx,
	}
}

// Call implements the simple string-based call interface.
func (o *LLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, o, prompt, options...)
}

// GenerateContent implements llms.Model via Ollama's native /api/chat endpoint.
func (o *LLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	opts := llms.CallOptions{}
	for _, opt := range options {
		opt(&opts)
	}

	chatMsgs, err := convertMessages(messages)
	if err != nil {
		return nil, err
	}
	var tools []ollamaTool
	if opts.ToolChoice != "none" {
		tools = convertTools(opts.Tools)
	}

	reqBody := chatRequest{
		Model:    o.model,
		Messages: chatMsgs,
		Tools:    tools,
		Stream:   false,
		Options: map[string]any{
			"num_ctx": o.numCtx,
		},
	}
	if opts.Temperature != 0 {
		reqBody.Options["temperature"] = opts.Temperature
	}
	if opts.MaxTokens > 0 {
		reqBody.Options["num_predict"] = opts.MaxTokens
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.serverURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read ollama response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to parse ollama response: %w", err)
	}

	return convertResponse(chatResp), nil
}

// --- Ollama native API types ---

type chatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function ollamaFunctionCall `json:"function"`
}

type ollamaFunctionCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type chatResponse struct {
	Model           string         `json:"model"`
	Message         *ollamaMessage `json:"message,omitempty"`
	Done            bool           `json:"done"`
	PromptEvalCount int            `json:"prompt_eval_count,omitempty"`
	EvalCount       int            `json:"eval_count,omitempty"`
}

// --- Conversion helpers ---

func convertMessages(messages []llms.MessageContent) ([]ollamaMessage, error) {
	out := make([]ollamaMessage, 0, len(messages))
	for _, mc := range messages {
		msg := ollamaMessage{Role: lcRoleToOllama(mc.Role)}

		for _, p := range mc.Parts {
			switch pt := p.(type) {
			case llms.TextContent:
				msg.Content += pt.Text
			case llms.ToolCall:
				if pt.FunctionCall != nil {
					var args map[string]any
					if err := json.Unmarshal([]byte(pt.FunctionCall.Arguments), &args); err != nil {
						return nil, fmt.Errorf("failed to unmarshal tool call %q arguments: %w", pt.FunctionCall.Name, err)
					}
					msg.ToolCalls = append(msg.ToolCalls, ollamaToolCall{
						Function: ollamaFunctionCall{
							Name:      pt.FunctionCall.Name,
							Arguments: args,
						},
					})
				}
			case llms.ToolCallResponse:
				if msg.Content != "" || len(msg.ToolCalls) > 0 {
					if msg.Role == "" {
						msg.Role = "assistant"
					}
					out = append(out, msg)
					msg = ollamaMessage{}
				}
				out = append(out, ollamaMessage{Role: "tool", Content: pt.Content})
			}
		}
		if msg.Role != "" || msg.Content != "" || len(msg.ToolCalls) > 0 {
			out = append(out, msg)
		}
	}
	return out, nil
}

func convertTools(tools []llms.Tool) []ollamaTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]ollamaTool, 0, len(tools))
	for _, t := range tools {
		if t.Function == nil {
			continue
		}
		out = append(out, ollamaTool{
			Type: "function",
			Function: ollamaToolFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}
	return out
}

func convertResponse(resp chatResponse) *llms.ContentResponse {
	choice := &llms.ContentChoice{
		GenerationInfo: map[string]any{
			"PromptTokens":     resp.PromptEvalCount,
			"CompletionTokens": resp.EvalCount,
			"TotalTokens":      resp.PromptEvalCount + resp.EvalCount,
		},
	}

	if resp.Message != nil {
		choice.Content = resp.Message.Content

		for i, tc := range resp.Message.ToolCalls {
			argsJSON, err := json.Marshal(tc.Function.Arguments)
			if err != nil {
				continue
			}
			choice.ToolCalls = append(choice.ToolCalls, llms.ToolCall{
				ID:   fmt.Sprintf("%s-%d", tc.Function.Name, i),
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	return &llms.ContentResponse{Choices: []*llms.ContentChoice{choice}}
}

func lcRoleToOllama(role llms.ChatMessageType) string {
	switch role {
	case llms.ChatMessageTypeSystem:
		return "system"
	case llms.ChatMessageTypeAI:
		return "assistant"
	case llms.ChatMessageTypeHuman, llms.ChatMessageTypeGeneric:
		return "user"
	case llms.ChatMessageTypeTool:
		return "tool"
	default:
		return "user"
	}
}
