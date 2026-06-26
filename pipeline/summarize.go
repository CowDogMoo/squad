package pipeline

import (
	"context"
	"fmt"
	"sync"

	"github.com/cowdogmoo/squad/logging"
)

const (
	// summarizeThreshold is the byte size above which "auto" mode triggers.
	summarizeThreshold = 8192

	// defaultSummarizePrompt instructs the LLM on how to compress stage output.
	defaultSummarizePrompt = "Summarize the following agent output for the next pipeline stage. " +
		"Preserve: all findings with severity, affected files, and actionable recommendations. " +
		"Remove: verbose explanations, repeated file contents, and tool call logs. " +
		"Keep the summary under 2000 characters."
)

// SummarizeFunc calls an LLM to summarize text. The system prompt provides
// summarization instructions; text is the content to summarize.
type SummarizeFunc func(ctx context.Context, systemPrompt, text string) (string, error)

// ShouldSummarize reports whether the stage output should be summarized
// based on the stage's Summarize setting and the output size.
func ShouldSummarize(stage Stage, outputLen int) bool {
	switch stage.Summarize {
	case "always":
		return true
	case "auto":
		return outputLen > summarizeThreshold
	default: // "never" or empty
		return false
	}
}

// SummarizeOutput calls the LLM to produce a compressed summary of the
// stage output. Falls back to truncation if the summarize function is nil
// or returns an error.
func SummarizeOutput(ctx context.Context, fn SummarizeFunc, stage Stage, output string) string {
	if fn == nil {
		return truncateFallback(output)
	}

	prompt := stage.SummarizePrompt
	if prompt == "" {
		prompt = defaultSummarizePrompt
	}

	summary, err := fn(ctx, prompt, output)
	if err != nil {
		logging.InfoContext(ctx, "summarization failed, falling back to truncation: %v", err)
		return truncateFallback(output)
	}

	return fmt.Sprintf("[Summarized from %d bytes]\n%s", len(output), summary)
}

// truncateFallback applies the existing hard truncation as a fallback.
func truncateFallback(output string) string {
	if len(output) > 4096 {
		return output[:4096] + "\n...(truncated)"
	}
	return output
}

// summaryCache caches LLM-generated summaries to avoid re-summarizing
// the same output for multiple downstream stages.
type summaryCache struct {
	mu      sync.RWMutex
	entries map[string]string // key: "stageName/agentName" → summary
}

func newSummaryCache() *summaryCache {
	return &summaryCache{entries: make(map[string]string)}
}

func (c *summaryCache) get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *summaryCache) set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = value
}
