package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/session"
	"github.com/tmc/langchaingo/llms"
)

// TranscriptFile is the per-session file holding the full, provider-faithful
// conversation for the LangChain (stateless-provider) path. Unlike
// events.jsonl — which is a lossy audit log of metadata — this file stores the
// actual message content so a --resume can replay the prior turns. The OpenAI
// Responses API does not need it: it chains server-side via PreviousResponseID.
const TranscriptFile = "transcript.json"

// transcriptMessage is the on-disk form of one llms.MessageContent. The role is
// stored as its raw string (llms.ChatMessageType is itself a string) so no enum
// mapping is required on the way back.
type transcriptMessage struct {
	Role  string           `json:"role"`
	Parts []transcriptPart `json:"parts"`
}

// transcriptPart is the on-disk form of a single content part. Only the part
// kinds the tool loop actually produces are represented; anything else is
// dropped on save (and would simply be absent on replay).
type transcriptPart struct {
	Type string `json:"type"`
	// Text is set for type "text".
	Text string `json:"text,omitempty"`
	// Tool-call fields, set for type "tool_call".
	ID       string `json:"id,omitempty"`
	ToolType string `json:"tool_type,omitempty"`
	Name     string `json:"name,omitempty"`
	Args     string `json:"args,omitempty"`
	// Tool-response fields, set for type "tool_response".
	ToolCallID string `json:"tool_call_id,omitempty"`
	Content    string `json:"content,omitempty"`
}

const (
	partKindText         = "text"
	partKindToolCall     = "tool_call"
	partKindToolResponse = "tool_response"
)

// SaveTranscript persists the conversational turns of messages to
// <dir>/transcript.json, atomically. The leading system message (and any other
// system-role message) is omitted: a resume re-prepends a freshly built system
// prompt, which may legitimately differ between runs. A nil/empty slice writes
// nothing. Best-effort errors are returned to the caller, which logs them.
func SaveTranscript(dir string, messages []llms.MessageContent) error {
	if dir == "" {
		return errors.New("transcript: empty session dir")
	}
	out := make([]transcriptMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == llms.ChatMessageTypeSystem {
			continue
		}
		parts := encodeParts(msg.Parts)
		if len(parts) == 0 {
			continue
		}
		out = append(out, transcriptMessage{Role: string(msg.Role), Parts: parts})
	}
	if len(out) == 0 {
		return nil
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("transcript: marshal: %w", err)
	}
	final := filepath.Join(dir, TranscriptFile)
	tmpFile, err := os.CreateTemp(dir, ".transcript-*.json.tmp")
	if err != nil {
		return fmt.Errorf("transcript: create temp: %w", err)
	}
	tmp := tmpFile.Name()
	if _, werr := tmpFile.Write(data); werr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("transcript: write: %w", werr)
	}
	if cerr := tmpFile.Close(); cerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("transcript: close: %w", cerr)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("transcript: chmod: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("transcript: rename: %w", err)
	}
	return nil
}

// LoadTranscript reads <dir>/transcript.json and rebuilds the message slice.
// A missing file is not an error — it returns (nil, nil), which the caller
// treats as "no prior context to replay".
func LoadTranscript(dir string) ([]llms.MessageContent, error) {
	data, err := os.ReadFile(filepath.Join(dir, TranscriptFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("transcript: read: %w", err)
	}
	var stored []transcriptMessage
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("transcript: parse: %w", err)
	}
	messages := make([]llms.MessageContent, 0, len(stored))
	for _, sm := range stored {
		messages = append(messages, llms.MessageContent{
			Role:  llms.ChatMessageType(sm.Role),
			Parts: decodeParts(sm.Parts),
		})
	}
	return messages, nil
}

// encodeParts normalizes content parts into their on-disk form, unwrapping
// cache-control hints (an optimization that need not survive a replay).
func encodeParts(parts []llms.ContentPart) []transcriptPart {
	out := make([]transcriptPart, 0, len(parts))
	for _, part := range parts {
		if cached, ok := part.(llms.CachedContent); ok {
			part = cached.ContentPart
		}
		switch p := part.(type) {
		case llms.TextContent:
			out = append(out, transcriptPart{Type: partKindText, Text: p.Text})
		case llms.ToolCall:
			tp := transcriptPart{Type: partKindToolCall, ID: p.ID, ToolType: p.Type}
			if p.FunctionCall != nil {
				tp.Name = p.FunctionCall.Name
				tp.Args = p.FunctionCall.Arguments
			}
			out = append(out, tp)
		case llms.ToolCallResponse:
			out = append(out, transcriptPart{
				Type:       partKindToolResponse,
				ToolCallID: p.ToolCallID,
				Name:       p.Name,
				Content:    p.Content,
			})
		default:
			// Image/binary parts are not produced by the tool loop; skip them
			// rather than guess at a lossy encoding.
		}
	}
	return out
}

// decodeParts rebuilds content parts from their on-disk form.
func decodeParts(parts []transcriptPart) []llms.ContentPart {
	out := make([]llms.ContentPart, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case partKindText:
			out = append(out, llms.TextContent{Text: p.Text})
		case partKindToolCall:
			out = append(out, llms.ToolCall{
				ID:           p.ID,
				Type:         p.ToolType,
				FunctionCall: &llms.FunctionCall{Name: p.Name, Arguments: p.Args},
			})
		case partKindToolResponse:
			out = append(out, llms.ToolCallResponse{
				ToolCallID: p.ToolCallID,
				Name:       p.Name,
				Content:    p.Content,
			})
		}
	}
	return out
}

// splicePriorMessages inserts replayed prior turns between the freshly built
// system message and the new user prompt, yielding
// [system?, ...prior, newUser] — the order stateless provider APIs expect.
func splicePriorMessages(initial, prior []llms.MessageContent) []llms.MessageContent {
	if len(prior) == 0 {
		return initial
	}
	if len(initial) == 0 {
		return prior
	}
	last := initial[len(initial)-1]
	head := initial[:len(initial)-1]
	out := make([]llms.MessageContent, 0, len(initial)+len(prior))
	out = append(out, head...)
	out = append(out, prior...)
	out = append(out, last)
	return out
}

// persistTranscript saves the conversation for the active session, if any. It
// is best-effort: a failure is logged but never fails the run, since the
// transcript only affects a future --resume.
func persistTranscript(ctx context.Context, messages []llms.MessageContent) {
	logger := session.FromContext(ctx)
	if logger == nil {
		return
	}
	if err := SaveTranscript(logger.Dir(), messages); err != nil {
		logging.Warn("failed to persist resume transcript: %v", err)
	}
}
