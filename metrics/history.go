package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cowdogmoo/squad/logging"
)

// HistorySample records the token usage from a single agent run.
type HistorySample struct {
	Agent        string    `json:"agent"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	Iterations   int       `json:"iterations"`
	Cost         float64   `json:"cost"`
	DurationSecs float64   `json:"duration_secs"`
	Timestamp    time.Time `json:"timestamp"`
}

// HistoryFile holds the list of samples for an agent.
type HistoryFile struct {
	Samples []HistorySample `json:"samples"`
}

// maxSamplesPerAgent limits history file size.
const maxSamplesPerAgent = 50

// LogRunHistory persists token usage from a completed agent run to the
// cost history cache.  Errors are logged but not returned — this is
// best-effort telemetry, not a critical path.
func LogRunHistory(cacheDir, agentName string, m *Metrics) {
	if m == nil || agentName == "" {
		return
	}

	histDir := filepath.Join(cacheDir, "cost-history")
	if err := os.MkdirAll(histDir, 0o755); err != nil {
		logging.Warn("cost-history: mkdir failed: %v", err)
		return
	}

	histPath := filepath.Join(histDir, agentName+".json")

	var hf HistoryFile
	if data, err := os.ReadFile(histPath); err == nil {
		_ = json.Unmarshal(data, &hf) // ignore parse errors on corrupt file
	}

	sample := HistorySample{
		Agent:        agentName,
		Model:        m.Model,
		Provider:     m.Provider,
		InputTokens:  m.InputTokens(),
		OutputTokens: m.OutputTokens(),
		Iterations:   m.Iterations(),
		Cost:         m.TotalCostWithChildren(),
		DurationSecs: m.Duration().Seconds(),
		Timestamp:    time.Now().UTC(),
	}

	hf.Samples = append(hf.Samples, sample)

	// Keep only the most recent samples
	if len(hf.Samples) > maxSamplesPerAgent {
		hf.Samples = hf.Samples[len(hf.Samples)-maxSamplesPerAgent:]
	}

	data, err := json.MarshalIndent(hf, "", "  ")
	if err != nil {
		logging.Warn("cost-history: marshal failed: %v", err)
		return
	}

	if err := os.WriteFile(histPath, data, 0o644); err != nil {
		logging.Warn("cost-history: write failed: %v", err)
	}
}

// LoadHistory reads the cost history for an agent from the cache directory.
func LoadHistory(cacheDir, agentName string) ([]HistorySample, error) {
	histPath := filepath.Join(cacheDir, "cost-history", agentName+".json")
	data, err := os.ReadFile(histPath)
	if err != nil {
		return nil, fmt.Errorf("no history for %s: %w", agentName, err)
	}

	var hf HistoryFile
	if err := json.Unmarshal(data, &hf); err != nil {
		return nil, fmt.Errorf("corrupt history for %s: %w", agentName, err)
	}

	return hf.Samples, nil
}
