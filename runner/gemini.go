package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"
)

// geminiLLM keeps googleai's content/tool conversion, but replaces its usage
// with the final wire response. The SDK merges the first chunk's usage and
// drops thinking tokens from CompletionTokens.
type geminiLLM struct {
	*googleai.GoogleAI
}

type geminiUsageKey struct{}

type geminiUsage struct {
	Prompt     int64 `json:"promptTokenCount"`
	Candidates int64 `json:"candidatesTokenCount"`
	Total      int64 `json:"totalTokenCount"`
	Cached     int64 `json:"cachedContentTokenCount"`
}

type geminiCapture struct {
	usage *geminiUsage
	body  *geminiUsageBody
}

func (g *geminiLLM) Call(ctx context.Context, prompt string, opts ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, g, prompt, opts...)
}

func (g *geminiLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, opts ...llms.CallOption) (*llms.ContentResponse, error) {
	// Capture is per call, so retries and later turns cannot reuse stale usage.
	capture := &geminiCapture{}
	ctx, cancel := context.WithCancel(context.WithValue(ctx, geminiUsageKey{}, capture))
	defer cancel()
	defer func() {
		if capture.body != nil {
			_ = capture.body.Close()
		}
	}()
	resp, err := g.GoogleAI.GenerateContent(ctx, messages, opts...)
	if err != nil {
		return resp, err
	}
	// googleai can stop at a candidate with no content. Consume any trailing
	// usage-only chunks too, without sending them to the text callback.
	if capture.body != nil && !capture.body.eof {
		if _, err := io.Copy(io.Discard, capture.body); err != nil {
			return nil, fmt.Errorf("read Gemini final usage: %w", err)
		}
	}
	if u := capture.usage; u != nil && resp != nil {
		// Total includes both visible output and billed thinking tokens;
		// Candidates alone excludes thinking. Keep the candidate count as a
		// fallback when an endpoint omits the total count.
		output := max(u.Candidates, u.Total-u.Prompt)
		for _, ch := range resp.Choices {
			if ch.GenerationInfo == nil {
				ch.GenerationInfo = make(map[string]any)
			}
			gi := ch.GenerationInfo
			gi["PromptTokens"] = u.Prompt
			gi["CompletionTokens"] = output
			gi["CachedTokens"] = u.Cached
		}
	}
	return resp, nil
}

type geminiUsageTransport struct {
	base http.RoundTripper
}

func (t geminiUsageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	capture, _ := req.Context().Value(geminiUsageKey{}).(*geminiCapture)
	if capture != nil && resp.StatusCode == http.StatusOK &&
		(strings.HasSuffix(req.URL.Path, ":generateContent") || strings.HasSuffix(req.URL.Path, ":streamGenerateContent")) {
		capture.usage = nil
		capture.body = &geminiUsageBody{ReadCloser: resp.Body, capture: capture}
		resp.Body = capture.body
	}
	return resp, nil
}

// geminiUsageBody observes complete top-level JSON objects in the REST
// response (a single object or a streamed array). It passes bytes through
// unchanged, buffering only the current chunk. Quotes and escapes can span
// reads; braces inside model text must never delimit a usage object.
type geminiUsageBody struct {
	io.ReadCloser
	capture *geminiCapture
	chunk   []byte
	depth   int
	quoted  bool
	escaped bool
	eof     bool
}

func (b *geminiUsageBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	for _, c := range p[:n] {
		if b.depth == 0 && c != '{' {
			continue
		}
		b.chunk = append(b.chunk, c)
		if b.quoted {
			switch {
			case b.escaped:
				b.escaped = false
			case c == '\\':
				b.escaped = true
			case c == '"':
				b.quoted = false
			}
			continue
		}
		switch c {
		case '"':
			b.quoted = true
		case '{':
			b.depth++
		case '}':
			b.depth--
			if b.depth == 0 {
				var chunk struct {
					Usage *geminiUsage `json:"usageMetadata"`
				}
				if json.Unmarshal(b.chunk, &chunk) == nil && chunk.Usage != nil {
					b.capture.usage = chunk.Usage
				}
				b.chunk = b.chunk[:0]
			}
		}
	}
	if err == io.EOF {
		b.eof = true
	}
	return n, err
}
