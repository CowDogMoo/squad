// Package ollama provides an Ollama-backed LLM implementation.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

const (
	// requestTimeout bounds one request, from sending it to reading the
	// whole reply, for a client with thinking off.
	requestTimeout = 5 * time.Minute

	// thinkingRequestTimeout is that bound for a client with thinking on.
	// Tool-calling requests are not streamed, so the whole reply must arrive
	// inside it, and numPredict raises the reply cap of every such client's
	// request from squad's default of 1024 tokens to thinkingMinPredict.
	// The timeout is eight times longer to match, which leaves the token
	// rate a model has to sustain where it was.
	thinkingRequestTimeout = 40 * time.Minute
)

// ErrEmptyTurn reports that the model returned neither text nor tool calls,
// even after squad retried with thinking off and a nudge to continue.
var ErrEmptyTurn = errors.New("ollama model returned an empty turn")

// errCodeEmptyTurn is the code of the typed error that carries ErrEmptyTurn.
// Callers that classify errors by code see a failure that is not transient,
// whatever counters its message quotes.
const errCodeEmptyTurn llms.ErrorCode = "empty_turn"

// thinkingMinPredict is the num_predict floor while thinking is on. Thinking
// tokens count against num_predict, so squad's small leaf budget (1024 by
// default) lets a thinking model such as qwen3 spend the whole reply on
// reasoning and stop with done_reason=length before it answers or calls a tool.
const thinkingMinPredict = 8192

// emptyTurnNudge is appended to the retry request after an empty turn.
const emptyTurnNudge = "Your previous reply was empty: it had no text and no tool call. " +
	"Continue the task now: call a tool, or give your final answer."

// LLM implements llms.Model using Ollama's native /api/chat endpoint,
// which supports both tool calling and the num_ctx parameter.
type LLM struct {
	serverURL string
	model     string
	numCtx    int
	// think is sent as the request's think field.
	think bool
	// httpClient sends the requests. Its timeout is requestTimeout, or
	// thinkingRequestTimeout when think is on.
	httpClient *http.Client
	// onTrim, when set, is called each time a request is trimmed.
	onTrim func(ctx context.Context)

	// mu guards bytesPerToken, the client's current bytes-per-token
	// estimate. It starts at defaultBytesPerToken and calibrate only ever
	// lowers it.
	mu            sync.Mutex
	bytesPerToken float64
}

// Verify interface compliance.
var _ llms.Model = (*LLM)(nil)

// Option configures an LLM.
type Option func(*LLM)

// WithThink sets the value sent as Ollama's think field.
func WithThink(think bool) Option {
	return func(o *LLM) { o.think = think }
}

// WithTrimObserver registers fn to be called each time a request is trimmed
// to fit num_ctx, with the context passed to GenerateContent. After a trim
// the model no longer receives every earlier message in full: older tool
// outputs are replaced by a placeholder, turns are removed, or the latest
// outputs are cut down. A caller that tracks what the model has been shown,
// as the agent loop's read cache does, must stop counting earlier content
// as delivered.
func WithTrimObserver(fn func(ctx context.Context)) Option {
	return func(o *LLM) { o.onTrim = fn }
}

// New creates a new Ollama LLM client. Thinking is off unless [WithThink]
// says otherwise: an agent loop needs a reply or a tool call every turn, and
// a thinking model can spend minutes and its whole output budget reasoning.
func New(serverURL, model string, numCtx int, opts ...Option) *LLM {
	serverURL = strings.TrimSuffix(serverURL, "/v1")
	serverURL = strings.TrimSuffix(serverURL, "/")
	o := &LLM{
		serverURL:     serverURL,
		model:         model,
		numCtx:        numCtx,
		bytesPerToken: defaultBytesPerToken,
	}
	for _, opt := range opts {
		opt(o)
	}
	o.httpClient = &http.Client{Timeout: requestTimeout}
	if o.think {
		o.httpClient.Timeout = thinkingRequestTimeout
	}
	return o
}

// Call implements the simple string-based call interface.
func (o *LLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, o, prompt, options...)
}

// buildChatRequest constructs the Ollama /api/chat request body from call options.
func (o *LLM) buildChatRequest(chatMsgs []ollamaMessage, opts llms.CallOptions) chatRequest {
	var tools []ollamaTool
	if opts.ToolChoice != "none" {
		tools = convertTools(opts.Tools)
	}
	stream := opts.StreamingFunc != nil && len(tools) == 0
	reqBody := chatRequest{
		Model:    o.model,
		Messages: chatMsgs,
		Tools:    tools,
		Stream:   stream,
		Think:    o.think,
		Options: map[string]any{
			"num_ctx": o.numCtx,
		},
	}
	if opts.Temperature != 0 {
		reqBody.Options["temperature"] = opts.Temperature
	}
	if opts.MaxTokens > 0 {
		reqBody.Options["num_predict"] = o.numPredict(opts.MaxTokens)
	}
	return reqBody
}

// numPredict returns the output-token cap for a request, raised to
// thinkingMinPredict while thinking is on so reasoning cannot use it all.
func (o *LLM) numPredict(maxTokens int) int {
	if !o.think || maxTokens >= thinkingMinPredict {
		return maxTokens
	}
	floor := thinkingMinPredict
	if o.numCtx > 0 && floor > o.numCtx/2 {
		floor = o.numCtx / 2
	}
	return max(maxTokens, floor)
}

// readFullResponse reads a non-streaming Ollama response.
func readFullResponse(body io.Reader) (chatResponse, error) {
	respBody, err := io.ReadAll(body)
	if err != nil {
		return chatResponse{}, fmt.Errorf("failed to read ollama response: %w", err)
	}
	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return chatResponse{}, fmt.Errorf("failed to parse ollama response: %w", err)
	}
	return chatResp, nil
}

// GenerateContent implements llms.Model via Ollama's native /api/chat endpoint.
// The request is trimmed to fit num_ctx first. A turn with no text and no
// tool calls is retried once with thinking off and a nudge; a second empty
// turn fails with [ErrEmptyTurn] instead of ending the agent loop silently.
func (o *LLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	opts := llms.CallOptions{}
	for _, opt := range options {
		opt(&opts)
	}

	chatMsgs, err := convertMessages(messages)
	if err != nil {
		return nil, err
	}

	reqBody := o.buildChatRequest(chatMsgs, opts)
	reqBody.Messages = o.fitContext(ctx, reqBody)

	first, err := o.chat(ctx, reqBody, opts.StreamingFunc)
	if err != nil {
		return nil, err
	}
	if !isEmptyTurn(first) {
		return convertResponse(first), nil
	}

	logging.WarnContext(ctx, "ollama: %s returned an empty turn (%s); retrying once with thinking off and a nudge", o.model, describeTurn(first))
	retry := reqBody
	retry.Think = false
	retry.Options = maps.Clone(reqBody.Options)
	retry.Messages = append(slices.Clone(chatMsgs), ollamaMessage{Role: "user", Content: emptyTurnNudge})
	retry.Messages = o.fitContext(ctx, retry)
	second, err := o.chat(ctx, retry, opts.StreamingFunc)
	if err != nil {
		return nil, fmt.Errorf("ollama: retry after an empty turn failed: %w", err)
	}
	if !isEmptyTurn(second) {
		return convertResponse(second), nil
	}
	return nil, emptyTurnError(o.model, reqBody.Options["num_predict"], first, second)
}

// chat performs one POST to /api/chat, decodes the reply, and feeds the
// prompt token count Ollama reports back into the client's token estimate.
func (o *LLM) chat(ctx context.Context, reqBody chatRequest, streamFn func(ctx context.Context, chunk []byte) error) (resp chatResponse, retErr error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return chatResponse{}, fmt.Errorf("failed to marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.serverURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, fmt.Errorf("failed to create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := o.httpClient.Do(req)
	if err != nil {
		return chatResponse{}, fmt.Errorf("ollama request failed: %w", err)
	}
	defer func() {
		if cerr := httpResp.Body.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("failed to close response body: %w", cerr)
		}
	}()

	if httpResp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(httpResp.Body)
		return chatResponse{}, fmt.Errorf("ollama returned %d: %s", httpResp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	if reqBody.Stream {
		resp, err = o.readStream(ctx, httpResp.Body, streamFn)
	} else {
		resp, err = readFullResponse(httpResp.Body)
	}
	if err != nil {
		return chatResponse{}, err
	}
	o.calibrate(ctx, reqBody, resp.PromptEvalCount)
	return resp, nil
}

// readStream processes NDJSON streaming responses from Ollama, calling
// streamFn for each content chunk. It accumulates content, thinking, and
// tool calls, plus the token counts and done reason from the final
// (done=true) message.
func (o *LLM) readStream(ctx context.Context, body io.Reader, streamFn func(ctx context.Context, chunk []byte) error) (chatResponse, error) {
	scanner := bufio.NewScanner(body)
	var full chatResponse
	for scanner.Scan() {
		var chunk chatResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		if msg := chunk.Message; msg != nil {
			if full.Message == nil {
				full.Message = &ollamaMessage{Role: "assistant"}
			}
			if msg.Content != "" {
				if err := streamFn(ctx, []byte(msg.Content)); err != nil {
					return chatResponse{}, fmt.Errorf("streaming callback failed: %w", err)
				}
				full.Message.Content += msg.Content
			}
			full.Message.Thinking += msg.Thinking
			full.Message.ToolCalls = append(full.Message.ToolCalls, msg.ToolCalls...)
		}
		if chunk.Done {
			full.Done = true
			full.DoneReason = chunk.DoneReason
			full.PromptEvalCount = chunk.PromptEvalCount
			full.EvalCount = chunk.EvalCount
			full.Model = chunk.Model
		}
	}
	if err := scanner.Err(); err != nil {
		return chatResponse{}, fmt.Errorf("failed to read ollama stream: %w", err)
	}
	return full, nil
}

// isEmptyTurn reports whether a reply carries neither text nor tool calls.
// Thinking alone does not count: the agent loop cannot act on it.
func isEmptyTurn(resp chatResponse) bool {
	return resp.Message == nil ||
		(strings.TrimSpace(resp.Message.Content) == "" && len(resp.Message.ToolCalls) == 0)
}

// describeTurn summarizes an empty turn for logs and errors.
func describeTurn(resp chatResponse) string {
	thinking := 0
	if resp.Message != nil {
		thinking = len(resp.Message.Thinking)
	}
	reason := resp.DoneReason
	if reason == "" {
		reason = "unknown"
	}
	return fmt.Sprintf("thinking=%d bytes, done_reason=%s, eval_count=%d", thinking, reason, resp.EvalCount)
}

// emptyTurnError explains why both attempts came back empty. The error is
// typed so that callers classifying errors by code never read the counters
// in its message as an HTTP status and retry the call.
func emptyTurnError(model string, numPredict any, first, second chatResponse) error {
	hint := ""
	if first.DoneReason == "length" || second.DoneReason == "length" {
		hint = fmt.Sprintf("; the reply hit num_predict=%v before it produced an answer, so raise --max-tokens", numPredict)
	}
	msg := fmt.Sprintf("%s gave no text and no tool calls, first (%s) and again after a retry with thinking off and a nudge (%s)%s",
		model, describeTurn(first), describeTurn(second), hint)
	return llms.NewError(errCodeEmptyTurn, "ollama", msg).WithCause(ErrEmptyTurn)
}

type chatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
	// Think is always sent: omitting it lets thinking models think.
	Think   bool           `json:"think"`
	Options map[string]any `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Thinking  string           `json:"thinking,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
}

type ollamaToolCall struct {
	ID       string             `json:"id,omitempty"`
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
	DoneReason      string         `json:"done_reason,omitempty"`
	PromptEvalCount int            `json:"prompt_eval_count,omitempty"`
	EvalCount       int            `json:"eval_count,omitempty"`
}

func convertMessages(messages []llms.MessageContent) ([]ollamaMessage, error) {
	out := make([]ollamaMessage, 0, len(messages))
	for _, mc := range messages {
		msg := ollamaMessage{Role: lcRoleToOllama(mc.Role)}
		emittedToolResult := false

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
				out = append(out, ollamaMessage{Role: "tool", Content: pt.Content, ToolName: pt.Name})
				emittedToolResult = true
			}
		}
		// Once tool results were emitted, an empty remainder is not a
		// message: sending it would add a blank tool reply to every turn.
		if emittedToolResult && msg.Content == "" && len(msg.ToolCalls) == 0 {
			continue
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
		StopReason: resp.DoneReason,
		GenerationInfo: map[string]any{
			"PromptTokens":     resp.PromptEvalCount,
			"CompletionTokens": resp.EvalCount,
			"TotalTokens":      resp.PromptEvalCount + resp.EvalCount,
		},
	}

	if resp.Message != nil {
		choice.Content = resp.Message.Content
		choice.ReasoningContent = resp.Message.Thinking

		for i, tc := range resp.Message.ToolCalls {
			argsJSON, err := json.Marshal(tc.Function.Arguments)
			if err != nil {
				logging.Warn("ollama: failed to marshal tool call %q arguments: %v", tc.Function.Name, err)
				continue
			}
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("%s-%d", tc.Function.Name, i)
			}
			choice.ToolCalls = append(choice.ToolCalls, llms.ToolCall{
				ID:   id,
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
