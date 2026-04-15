package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/tmc/langchaingo/llms"
)

const (
	// loopWindowSize is the number of recent steps to examine for loops.
	loopWindowSize = 10
	// loopMaxRepeats is how many times a step signature can repeat before
	// we consider the agent stuck in a loop.
	loopMaxRepeats = 3
)

// LoopDetector watches for repetitive tool-call patterns by hashing recent
// (tool-calls + tool-results) steps and detecting when the same signature
// appears too many times within a sliding window.
type LoopDetector struct {
	signatures []string
}

// stepSignature computes a SHA-256 hash of all tool calls in a step paired
// with their results. The hash covers tool name, arguments, and output so
// that identical calls producing different results are treated as distinct.
func stepSignature(calls []llms.ToolCall, results map[string]string) string {
	if len(calls) == 0 {
		return ""
	}
	h := sha256.New()
	// Sort calls by ID for deterministic ordering.
	sorted := make([]llms.ToolCall, len(calls))
	copy(sorted, calls)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})
	for _, tc := range sorted {
		if tc.FunctionCall == nil {
			continue
		}
		h.Write([]byte(tc.FunctionCall.Name))
		h.Write([]byte{0})
		h.Write([]byte(tc.FunctionCall.Arguments))
		h.Write([]byte{0})
		h.Write([]byte(results[tc.ID]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Record adds a step (tool calls + their results) to the detector's history.
func (ld *LoopDetector) Record(calls []llms.ToolCall, results map[string]string) {
	sig := stepSignature(calls, results)
	if sig == "" {
		return
	}
	ld.signatures = append(ld.signatures, sig)
}

// Stuck returns true if any step signature appears more than loopMaxRepeats
// times within the last loopWindowSize steps.
func (ld *LoopDetector) Stuck() bool {
	n := len(ld.signatures)
	start := 0
	if n > loopWindowSize {
		start = n - loopWindowSize
	}
	window := ld.signatures[start:]

	counts := make(map[string]int, len(window))
	for _, sig := range window {
		counts[sig]++
		if counts[sig] >= loopMaxRepeats {
			return true
		}
	}
	return false
}
