package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
)

// StageStatus represents the outcome of a pipeline stage.
type StageStatus string

const (
	StatusPassed   StageStatus = "passed"
	StatusFailed   StageStatus = "failed"
	StatusReverted StageStatus = "reverted"
	StatusSkipped  StageStatus = "skipped"
)

// StageResult records the outcome of a single stage execution.
type StageResult struct {
	Name     string        `json:"name"`
	Status   StageStatus   `json:"status"`
	Agents   []AgentResult `json:"agents"`
	Duration string        `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

// AgentResult records the outcome of a single agent within a stage.
type AgentResult struct {
	Agent    string           `json:"agent"`
	Status   StageStatus      `json:"status"`
	Output   string           `json:"output,omitempty"`
	Duration string           `json:"duration"`
	Metrics  *metrics.Metrics `json:"-"`
}

// Report is the structured output of a pipeline run.
type Report struct {
	Pipeline string        `json:"pipeline"`
	Version  string        `json:"version"`
	Status   StageStatus   `json:"status"`
	Stages   []StageResult `json:"stages"`
	Duration string        `json:"duration"`
}

// RunAgentFunc is called by the runner to execute a single agent.
// It mirrors the signature needed to invoke a model run.
type RunAgentFunc func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error)

// Runner executes a pipeline configuration.
type Runner struct {
	Pipeline   *Pipeline
	WorkingDir string
	RunAgent   RunAgentFunc
	Prompt     string // base prompt passed to each agent
}

// Run executes the pipeline and returns a structured report.
func (r *Runner) Run(ctx context.Context) (*Report, error) {
	start := time.Now()
	report := &Report{
		Pipeline: r.Pipeline.Name,
		Version:  r.Pipeline.Version,
		Status:   StatusPassed,
	}

	tiers := r.Pipeline.TopologicalOrder()
	completedStages := make(map[string]*StageResult)

	for _, tier := range tiers {
		tierResults := r.runTier(ctx, tier, completedStages)
		for i := range tierResults {
			result := &tierResults[i]
			report.Stages = append(report.Stages, *result)
			completedStages[result.Name] = result

			if result.Status == StatusFailed {
				report.Status = StatusFailed
				report.Duration = time.Since(start).Round(time.Millisecond).String()
				return report, fmt.Errorf("stage %q failed: %s", result.Name, result.Error)
			}

			// Run gates after this stage.
			if err := r.runGates(ctx, result.Name, completedStages); err != nil {
				report.Status = StatusFailed
				report.Duration = time.Since(start).Round(time.Millisecond).String()
				return report, err
			}
		}
	}

	report.Duration = time.Since(start).Round(time.Millisecond).String()
	return report, nil
}

// runTier runs all stages in a tier concurrently.
func (r *Runner) runTier(ctx context.Context, stages []Stage, completed map[string]*StageResult) []StageResult {
	results := make([]StageResult, len(stages))
	var wg sync.WaitGroup

	for i, stage := range stages {
		wg.Add(1)
		go func(idx int, s Stage) {
			defer wg.Done()
			results[idx] = r.runStage(ctx, s, completed)
		}(i, stage)
	}

	wg.Wait()
	return results
}

// runStage runs a single stage, executing its agents sequentially or in parallel.
func (r *Runner) runStage(ctx context.Context, stage Stage, completed map[string]*StageResult) StageResult {
	start := time.Now()
	result := StageResult{
		Name:   stage.Name,
		Status: StatusPassed,
	}

	// Check skip condition.
	if stage.Condition != "" {
		logging.InfoContext(ctx, "pipeline: stage %q has condition %q (evaluation delegated to orchestrator)", stage.Name, stage.Condition)
	}

	agents := stage.AgentList()
	logging.InfoContext(ctx, "pipeline: running stage %q with %d agent(s)", stage.Name, len(agents))

	// Build context from completed stages for the prompt.
	promptContext := r.buildPromptContext(stage, completed)

	if len(agents) == 1 {
		// Single agent: run directly.
		ar := r.runAgent(ctx, agents[0], stage, promptContext)
		result.Agents = []AgentResult{ar}
		if ar.Status == StatusFailed {
			result.Status = StatusFailed
			result.Error = fmt.Sprintf("agent %q failed", ar.Agent)
		}
	} else {
		// Multiple agents: run in parallel.
		result.Agents = r.runAgentsParallel(ctx, agents, stage, promptContext)
		for _, ar := range result.Agents {
			if ar.Status == StatusFailed {
				result.Status = StatusFailed
				result.Error = fmt.Sprintf("agent %q failed", ar.Agent)
				break
			}
		}
	}

	result.Duration = time.Since(start).Round(time.Millisecond).String()
	return result
}

// runAgentsParallel runs multiple agents concurrently within a stage.
func (r *Runner) runAgentsParallel(ctx context.Context, agents []string, stage Stage, promptContext string) []AgentResult {
	results := make([]AgentResult, len(agents))
	var wg sync.WaitGroup

	for i, agentName := range agents {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			results[idx] = r.runAgent(ctx, name, stage, promptContext)
		}(i, agentName)
	}

	wg.Wait()
	return results
}

// runAgent executes a single agent and returns its result.
func (r *Runner) runAgent(ctx context.Context, agentName string, stage Stage, promptContext string) AgentResult {
	start := time.Now()
	result := AgentResult{
		Agent:  agentName,
		Status: StatusPassed,
	}

	prompt := r.Prompt
	if promptContext != "" {
		prompt = promptContext + "\n\n" + prompt
	}

	mode := stage.Mode
	vars := stage.Vars

	logging.InfoContext(ctx, "pipeline: running agent %q in stage %q", agentName, stage.Name)
	output, m, err := r.RunAgent(ctx, agentName, prompt, r.WorkingDir, mode, vars)
	result.Duration = time.Since(start).Round(time.Millisecond).String()
	result.Metrics = m
	result.Output = output

	if err != nil {
		result.Status = StatusFailed
		result.Output = output // preserve partial output
		logging.InfoContext(ctx, "pipeline: agent %q failed: %v", agentName, err)
	} else {
		logging.InfoContext(ctx, "pipeline: agent %q completed (%d bytes)", agentName, len(output))
	}

	return result
}

// buildPromptContext creates context from prior stage results to pass to the next agent.
func (r *Runner) buildPromptContext(stage Stage, completed map[string]*StageResult) string {
	if len(stage.DependsOn) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Prior Stage Results\n\n")
	for _, dep := range stage.DependsOn {
		sr, ok := completed[dep]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "### Stage: %s (status: %s)\n\n", sr.Name, sr.Status)
		for _, ar := range sr.Agents {
			fmt.Fprintf(&sb, "**Agent %s** (status: %s):\n", ar.Agent, ar.Status)
			// Include a summary of the output (truncated for context efficiency).
			summary := ar.Output
			if len(summary) > 4096 {
				summary = summary[:4096] + "\n...(truncated)"
			}
			sb.WriteString(summary)
			sb.WriteString("\n\n")
		}
	}
	return sb.String()
}

// runGates executes all gates configured after the named stage.
func (r *Runner) runGates(ctx context.Context, stageName string, completed map[string]*StageResult) error {
	gates := r.Pipeline.GatesAfter(stageName)
	for _, gate := range gates {
		logging.InfoContext(ctx, "pipeline: running gate after %q: %s", stageName, gate.Command)

		cmd := exec.CommandContext(ctx, "bash", "-lc", gate.Command)
		cmd.Dir = r.WorkingDir
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf

		if err := cmd.Run(); err != nil {
			output := buf.String()
			logging.InfoContext(ctx, "pipeline: gate failed after %q: %v\n%s", stageName, err, output)

			onFailure := gate.OnFailure
			if onFailure == "" {
				onFailure = "stop"
			}

			switch onFailure {
			case "revert":
				if sr, ok := completed[stageName]; ok {
					sr.Status = StatusReverted
				}
				return fmt.Errorf("gate after %q failed (reverted): %s\n%s", stageName, err, output)
			default: // "stop"
				return fmt.Errorf("gate after %q failed: %s\n%s", stageName, err, output)
			}
		}
		logging.InfoContext(ctx, "pipeline: gate after %q passed", stageName)
	}
	return nil
}

// FormatReport returns the report in the configured output format.
func (r *Runner) FormatReport(report *Report) (string, error) {
	format := "markdown"
	if r.Pipeline.Output != nil && r.Pipeline.Output.Format != "" {
		format = r.Pipeline.Output.Format
	}

	switch format {
	case "json":
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal report: %w", err)
		}
		return string(data), nil
	default:
		return formatMarkdownReport(report), nil
	}
}

// formatMarkdownReport renders a pipeline report as markdown.
func formatMarkdownReport(report *Report) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Pipeline Report: %s\n\n", report.Pipeline)
	fmt.Fprintf(&sb, "**Status:** %s | **Duration:** %s\n\n", report.Status, report.Duration)

	sb.WriteString("| Stage | Agent | Status | Duration |\n")
	sb.WriteString("|-------|-------|--------|----------|\n")
	for _, sr := range report.Stages {
		for _, ar := range sr.Agents {
			fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", sr.Name, ar.Agent, ar.Status, ar.Duration)
		}
	}
	sb.WriteString("\n")

	for _, sr := range report.Stages {
		for _, ar := range sr.Agents {
			if ar.Output == "" {
				continue
			}
			fmt.Fprintf(&sb, "## %s / %s\n\n", sr.Name, ar.Agent)
			sb.WriteString(ar.Output)
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
}
