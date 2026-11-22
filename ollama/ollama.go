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
	// whole reply, for a client with thinking off. A cold prompt is
	// processed before the first byte of the reply arrives, and a slow host
	// can take minutes over it: at about 100 tokens a second, a 70k-token
	// prompt alone takes over ten minutes. [WithTimeout] overrides it.
	requestTimeout = 30 * time.Minute

	// thinkingRequestTimeout is that bound for a client with thinking on,
	// unless [WithTimeout] overrides it.
	// Tool-calling requests are not streamed, so the whole reply, reasoning
	// included, must arrive inside it. A thinking model can reason for
	// minutes before it answers, and its reply budget is twice as large (see
	// replyBudget), so it gets a much longer wait.
	thinkingRequestTimeout = 60 * time.Minute
)

// ErrEmptyTurn reports that the model returned neither text nor tool calls,
// even after squad retried with thinking off and a nudge to continue.
var ErrEmptyTurn = errors.New("ollama model returned an empty turn")

// errCodeEmptyTurn is the code of the typed error that carries ErrEmptyTurn.
// Callers that classify errors by code see a failure that is not transient,
// whatever counters its message quotes.
const errCodeEmptyTurn llms.ErrorCode = "empty_turn"

// emptyTurnNudge is appended to the retry request after an empty turn.
const emptyTurnNudge = "Your previous reply was empty: it had no text and no tool call. " +
	"Continue the task now: call a tool, or give your final answer."

// cutOffNudge is appended to the retry request after a reply the reply
// budget cut off before any tool call was complete. %s names the limit.
const cutOffNudge = "Your previous reply was cut off at %s, so any tool call in it was lost and nothing ran. " +
	"If you were calling a tool, make a shorter call: build long content (a script, a wordlist, a list of payloads) " +
	"in a file over several calls, or generate it with a short command, instead of writing it out in one reply. " +
	"If you were writing your final answer, write it again more briefly."

// cutOffAgainNote stands in for the reply when the retry after a cut-off is
// cut off too. %s names the limit.
const cutOffAgainNote = "[My reply was cut off at %s twice, before any tool call was complete, so nothing ran. " +
	"I need to make a much shorter tool call, or give my final answer.]"

// toolsOffNote answers each tool call in a reply to a tool_choice "none"
// request, so the follow-up keeps the tool definitions (and the server's
// prompt cache) and the model sees why nothing ran.
const toolsOffNote = "Not run: tools are off now. Reply with your final answer as text, without calling a tool."

// LLM implements llms.Model using Ollama's native /api/chat endpoint,
// which supports both tool calling and the num_ctx parameter.
type LLM struct {
	serverURL string
	model     string
	numCtx    int
	// think is sent as the request's think field.
	think bool
	// timeout, when positive, replaces the default request timeout.
	timeout time.Duration
	// httpClient sends the requests. Its timeout is timeout when set, else
	// requestTimeout, or thinkingRequestTimeout when think is on.
	httpClient *http.Client

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

// WithTimeout bounds each request, from sending it to reading the whole
// reply, whether thinking is on or off. A zero or negative value keeps the
// default: 30 minutes, or 60 minutes with thinking on.
func WithTimeout(timeout time.Duration) Option {
	return func(o *LLM) { o.timeout = timeout }
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
	timeout := requestTimeout
	switch {
	case o.timeout > 0:
		timeout = o.timeout
	case o.think:
		timeout = thinkingRequestTimeout
	}
	o.httpClient = &http.Client{Timeout: timeout}
	return o
}

// Call implements the simple string-based call interface.
func (o *LLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, o, prompt, options...)
}

// buildChatRequest constructs the Ollama /api/chat request body from call options.
//
// The tool definitions are sent even when opts.ToolChoice is "none". Ollama
// has no tool_choice, and chat templates such as qwen3's render the tools at
// the very start of the prompt, so leaving them out of an agent's final
// tool-less request changes the first tokens of a long conversation: the
// server's prompt cache misses and the whole conversation is processed again,
// which takes many minutes on a slow host. GenerateContent drops any tool
// calls a "none" request still produces.
func (o *LLM) buildChatRequest(chatMsgs []ollamaMessage, opts llms.CallOptions) chatRequest {
	tools := convertTools(opts.Tools)
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
	if budget := o.replyBudget(opts.MaxTokens); budget > 0 {
		reqBody.Options["num_predict"] = budget
	}
	return reqBody
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
// The conversation is sent as given: keeping it inside num_ctx is the agent
// loop's job, with the budget [LLM.ContextBudget] reports. A turn with no
// text and no tool calls is retried once with thinking off and a nudge; a
// second empty turn fails with [ErrEmptyTurn] instead of ending the agent
// loop silently. A reply the budget cut off before any tool call (see
// isCutOffTurn) is retried the same way, with a nudge to keep the call short;
// if the retry is cut off too, the agent loop gets a note saying so rather
// than an error, since the model, not the server, keeps failing.
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
	if opts.ToolChoice == "none" && len(reqBody.Tools) > 0 {
		resp, answered, err := o.chatWithoutToolCalls(ctx, reqBody, opts.StreamingFunc)
		if err != nil {
			return nil, err
		}
		if answered {
			return resp, nil
		}
		logging.WarnContext(ctx, "ollama: %s answered a request for no tool calls with no text; asking again without tool definitions", o.model)
		reqBody.Tools = nil
		reqBody.Stream = opts.StreamingFunc != nil
	}

	first, err := o.chat(ctx, reqBody, opts.StreamingFunc)
	if err != nil {
		return nil, err
	}
	cutOff := isCutOffTurn(first)
	if !isEmptyTurn(first) && !cutOff {
		return convertResponse(first), nil
	}

	nudge := emptyTurnNudge
	if cutOff {
		logging.WarnContext(ctx, "ollama: %s's reply was cut off at the reply budget with no tool call (%s); retrying once with thinking off and a nudge to keep the call short", o.model, describeTurn(first))
		nudge = o.cutOffNudge(opts.MaxTokens)
	} else {
		logging.WarnContext(ctx, "ollama: %s returned an empty turn (%s); retrying once with thinking off and a nudge", o.model, describeTurn(first))
	}
	retry := reqBody
	retry.Think = false
	retry.Options = maps.Clone(reqBody.Options)
	retry.Messages = append(slices.Clone(chatMsgs), ollamaMessage{Role: "user", Content: nudge})
	second, err := o.chat(ctx, retry, opts.StreamingFunc)
	if err != nil {
		return nil, fmt.Errorf("ollama: retry after an empty turn failed: %w", err)
	}
	if !isEmptyTurn(second) {
		return convertResponse(second), nil
	}
	if cutOff && isCutOffTurn(second) {
		// The model keeps writing a call too long to finish. That is how it
		// behaves, not a broken server, so the agent loop gets a reply saying
		// so and decides (a text nudge, or a clean end) instead of the run
		// failing with whatever it had recorded unreported.
		logging.WarnContext(ctx, "ollama: %s's reply was cut off again after the retry (%s); passing a note to the agent loop instead of failing", o.model, describeTurn(second))
		resp := convertResponse(second)
		resp.Choices[0].Content = fmt.Sprintf(cutOffAgainNote, o.replyLimit(opts.MaxTokens))
		return resp, nil
	}
	return nil, o.emptyTurnError(opts.MaxTokens, first, second)
}

// chatWithoutToolCalls sends a tool_choice "none" request with its tool
// definitions kept (see buildChatRequest) and drops any tool calls from the
// reply. A reply of tool calls alone is answered with a tool result for each
// saying tools are off, and asked again with the definitions still kept.
// answered is false when no text is left, so the caller can ask again
// without the definitions. The reply is not streamed because tools are
// present; streamFn, when set, receives the whole text once.
func (o *LLM) chatWithoutToolCalls(ctx context.Context, reqBody chatRequest, streamFn func(ctx context.Context, chunk []byte) error) (*llms.ContentResponse, bool, error) {
	resp, err := o.chat(ctx, reqBody, nil)
	if err != nil {
		return nil, false, err
	}
	if resp.Message != nil && strings.TrimSpace(resp.Message.Content) == "" && len(resp.Message.ToolCalls) > 0 {
		// Asked for no tool calls, qwen3 often makes them anyway. Answering
		// them keeps the conversation's prefix, and the server's cache, where
		// asking again without the tool definitions would reprocess it all.
		logging.InfoContext(ctx, "ollama: %s answered a request for no tool calls with %d tool call(s) only; answering them and asking again", o.model, len(resp.Message.ToolCalls))
		follow := reqBody
		follow.Messages = append(slices.Clone(reqBody.Messages), ollamaMessage{Role: "assistant", ToolCalls: resp.Message.ToolCalls})
		for _, call := range resp.Message.ToolCalls {
			follow.Messages = append(follow.Messages, ollamaMessage{Role: "tool", ToolName: call.Function.Name, Content: toolsOffNote})
		}
		if resp, err = o.chat(ctx, follow, nil); err != nil {
			return nil, false, err
		}
	}
	if resp.Message == nil || strings.TrimSpace(resp.Message.Content) == "" {
		return nil, false, nil
	}
	if n := len(resp.Message.ToolCalls); n > 0 {
		logging.InfoContext(ctx, "ollama: dropping %d tool call(s) from a reply that was asked for none", n)
		resp.Message.ToolCalls = nil
	}
	if streamFn != nil {
		if err := streamFn(ctx, []byte(resp.Message.Content)); err != nil {
			return nil, false, fmt.Errorf("streaming callback failed: %w", err)
		}
	}
	return convertResponse(resp), true, nil
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
		return chatResponse{}, &HTTPError{StatusCode: httpResp.StatusCode, Body: strings.TrimSpace(string(errBody))}
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

// isCutOffTurn reports whether the reply budget ended a reply before it made
// any tool call, with no thinking to blame. A model writing a long tool call
// (an inline wordlist, say) that runs out of budget mid-call loses the call:
// Ollama returns only the text before it, or nothing, and the agent loop
// would take that as the final answer. A reply that spent its budget
// thinking is an empty turn instead, retried with thinking off.
func isCutOffTurn(resp chatResponse) bool {
	return resp.Message != nil && resp.DoneReason == "length" &&
		len(resp.Message.ToolCalls) == 0 && strings.TrimSpace(resp.Message.Thinking) == ""
}

// cutOffNudge builds the retry nudge for a cut-off reply, naming the reply
// budget a request made with maxTokens had.
func (o *LLM) cutOffNudge(maxTokens int) string {
	return fmt.Sprintf(cutOffNudge, o.replyLimit(maxTokens))
}

// replyLimit names the reply budget a request made with maxTokens had.
func (o *LLM) replyLimit(maxTokens int) string {
	if budget := o.replyBudget(maxTokens); budget > 0 {
		return fmt.Sprintf("the %d-token reply limit", budget)
	}
	return "the reply length limit"
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

// emptyTurnError explains why both attempts of a request made with maxTokens
// came back empty. When a reply ran into its budget, the error names the
// setting that holds the budget down: --num-ctx when it is the reply's share
// of the window, --max-tokens when the caller's cap is lower than that share,
// and both when the two are equal. The error is typed so that callers
// classifying errors by code never read the counters in its message as an
// HTTP status and retry the call.
func (o *LLM) emptyTurnError(maxTokens int, first, second chatResponse) error {
	hint := ""
	budget := o.replyBudget(maxTokens)
	if budget > 0 && (first.DoneReason == "length" || second.DoneReason == "length") {
		const share = "its share of num_ctx (an eighth, or a quarter with thinking on)"
		byWindow := o.numCtx > 0 && budget == o.replyShare()
		byCap := budget == maxTokens
		bound, remedy := share, "raise --num-ctx"
		switch {
		case byWindow && byCap:
			bound = "both " + share + " and the max-tokens cap"
			remedy = fmt.Sprintf("raise --num-ctx and raise --max-tokens above %d", budget)
		case byCap:
			bound = "the max-tokens cap"
			remedy = fmt.Sprintf("raise --max-tokens above %d", budget)
		}
		hint = fmt.Sprintf("; the reply hit num_predict=%d, %s, before it produced an answer, so %s", budget, bound, remedy)
	}
	msg := fmt.Sprintf("%s gave no text and no tool calls, first (%s) and again after a retry with thinking off and a nudge (%s)%s",
		o.model, describeTurn(first), describeTurn(second), hint)
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
				if msg.hasPayload() {
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
		if emittedToolResult && !msg.hasPayload() {
			continue
		}
		if msg.Role != "" || msg.hasPayload() {
			out = append(out, msg)
		}
	}
	return out, nil
}

// hasPayload reports whether m carries text or tool calls.
func (m ollamaMessage) hasPayload() bool {
	return m.Content != "" || len(m.ToolCalls) > 0
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
