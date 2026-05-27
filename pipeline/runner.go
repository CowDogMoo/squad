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

	"github.com/cowdogmoo/squad/csync"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/telemetry"
	"github.com/cowdogmoo/squad/tools"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// StageStatus represents the outcome of a pipeline stage.
type StageStatus string

const (
	StatusPassed   StageStatus = "passed"
	StatusFailed   StageStatus = "failed"
	StatusReverted StageStatus = "reverted"
	StatusSkipped  StageStatus = "skipped"

	// PipelineMaxCostVar is a reserved stage var key used to propagate
	// the effective per-agent cost cap from the pipeline runner.
	PipelineMaxCostVar = "__pipeline_max_cost"

	// PipelineStageNameVar is a reserved stage var key used to propagate
	// the current stage name to the RunAgent callback so it can look up
	// stage-scoped overrides (e.g. mcp_servers) on the Pipeline.
	PipelineStageNameVar = "__pipeline_stage_name"
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
	Pipeline string          `json:"pipeline"`
	Version  string          `json:"version"`
	Status   StageStatus     `json:"status"`
	Stages   []StageResult   `json:"stages"`
	Findings []tools.Finding `json:"findings,omitempty"`
	Duration string          `json:"duration"`
}

// RunAgentFunc is called by the runner to execute a single agent.
// It mirrors the signature needed to invoke a model run.
type RunAgentFunc func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error)

// Runner executes a pipeline configuration.
type Runner struct {
	Pipeline     *Pipeline
	WorkingDir   string
	RunAgent     RunAgentFunc
	Prompt       string  // base prompt passed to each agent
	MaxCost      float64 // total cost budget for the pipeline (0 = unlimited)
	Findings     *tools.FindingsStore
	Summarize    SummarizeFunc            // optional LLM summarization for stage handoffs
	InlineAgents map[string]*InlineConfig // inline agent configs keyed by stage name
	ComposedDir  string                   // directory of the composed agent (for resolving inline prompt files)
	spent        *csync.Value[float64]
	sumCache     *summaryCache
}

// Run executes the pipeline and returns a structured report.
func (r *Runner) Run(ctx context.Context) (*Report, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "pipeline.run",
		trace.WithAttributes(
			attribute.String("squad.pipeline.name", r.Pipeline.Name),
		),
	)
	defer span.End()

	start := time.Now()

	// Create a shared findings store if not already set.
	if r.Findings == nil {
		r.Findings = tools.NewFindingsStore()
	}

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
				r.attachFindings(report)
				return report, fmt.Errorf("stage %q failed: %s", result.Name, result.Error)
			}

			// Run gates after this stage.
			if err := r.runGates(ctx, result.Name, completedStages); err != nil {
				report.Status = StatusFailed
				report.Duration = time.Since(start).Round(time.Millisecond).String()
				r.attachFindings(report)
				return report, err
			}
		}
	}

	// Attach findings to the report.
	if r.Findings != nil && r.Findings.Count() > 0 {
		report.Findings = r.Findings.All()
		logging.InfoContext(ctx, "pipeline: %d findings collected", len(report.Findings))
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

// runPreGates executes pre-gate commands and returns their combined output
// for injection into agent prompts.
func (r *Runner) runPreGates(ctx context.Context, stage Stage) (string, error) {
	if len(stage.PreGates) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("## Static Analysis Output\n\n")

	for _, pg := range stage.PreGates {
		label := pg.Label
		if label == "" {
			label = pg.Command
		}
		logging.InfoContext(ctx, "pipeline: running pre-gate %q for stage %q", label, stage.Name)

		cmd := exec.CommandContext(ctx, "bash", "-lc", pg.Command)
		cmd.Dir = r.WorkingDir
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf

		err := cmd.Run()
		output := buf.String()

		if err != nil {
			onError := pg.OnError
			if onError == "" {
				onError = "continue"
			}
			switch onError {
			case "skip":
				logging.InfoContext(ctx, "pipeline: pre-gate %q failed (skipping stage): %v", label, err)
				return "", fmt.Errorf("pre-gate %q failed: %w", label, err)
			case "stop":
				return "", fmt.Errorf("pre-gate %q failed (stopping pipeline): %w", label, err)
			default: // "continue"
				logging.InfoContext(ctx, "pipeline: pre-gate %q failed (continuing): %v", label, err)
			}
		}

		fmt.Fprintf(&sb, "### %s\n\n", label)
		if output == "" {
			sb.WriteString("(no output — all checks passed)\n\n")
		} else {
			// Cap pre-gate output to avoid blowing up context.
			if len(output) > 8192 {
				output = output[:8192] + "\n...(truncated)"
			}
			sb.WriteString("```\n")
			sb.WriteString(output)
			sb.WriteString("```\n\n")
		}
	}

	return sb.String(), nil
}

// runStage runs a single stage, executing its agents sequentially or in parallel.
func (r *Runner) runStage(ctx context.Context, stage Stage, completed map[string]*StageResult) StageResult {
	start := time.Now()
	result := StageResult{
		Name:   stage.Name,
		Status: StatusPassed,
	}

	if stage.Condition != "" {
		logging.InfoContext(ctx, "pipeline: stage %q has condition %q (evaluation delegated to orchestrator)", stage.Name, stage.Condition)
	}

	// Run pre-gates (static analysis tools) before agents.
	preGateOutput, preGateErr := r.runPreGates(ctx, stage)
	if preGateErr != nil {
		result.Status = StatusSkipped
		result.Error = preGateErr.Error()
		result.Duration = time.Since(start).Round(time.Millisecond).String()
		return result
	}

	promptContext := r.buildPromptContext(ctx, stage, completed)

	// Prepend pre-gate output to the prompt context so agents get static analysis results.
	if preGateOutput != "" {
		promptContext = preGateOutput + "\n" + promptContext
	}

	// Handle partitioned stages: expand files into groups and run one
	// agent instance per partition in parallel.
	if stage.Partition != nil {
		partitions, partErr := ExpandPartition(r.WorkingDir, stage.Partition)
		if partErr != nil {
			result.Status = StatusFailed
			result.Error = fmt.Sprintf("partition expansion failed: %v", partErr)
			result.Duration = time.Since(start).Round(time.Millisecond).String()
			return result
		}
		if len(partitions) == 0 {
			logging.InfoContext(ctx, "pipeline: stage %q partition matched no files, skipping", stage.Name)
			result.Status = StatusSkipped
			result.Error = "partition matched no files"
			result.Duration = time.Since(start).Round(time.Millisecond).String()
			return result
		}
		logging.InfoContext(ctx, "pipeline: stage %q partitioned into %d groups (%d total files)",
			stage.Name, len(partitions), countFiles(partitions))
		result.Agents = r.runPartitions(ctx, stage, partitions, promptContext)
		for _, ar := range result.Agents {
			if ar.Status == StatusFailed {
				result.Status = StatusFailed
				result.Error = fmt.Sprintf("agent %q failed", ar.Agent)
				break
			}
		}
		result.Duration = time.Since(start).Round(time.Millisecond).String()
		return result
	}

	agents := stage.AgentList()
	logging.InfoContext(ctx, "pipeline: running stage %q with %d agent(s)", stage.Name, len(agents))

	if len(agents) == 1 {
		ar := r.runAgent(ctx, agents[0], stage, promptContext)
		result.Agents = []AgentResult{ar}
		if ar.Status == StatusFailed {
			result.Status = StatusFailed
			result.Error = fmt.Sprintf("agent %q failed", ar.Agent)
		}
	} else {
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

// runPartitions spawns one agent instance per file partition in parallel.
func (r *Runner) runPartitions(ctx context.Context, stage Stage, partitions [][]string, promptContext string) []AgentResult {
	results := make([]AgentResult, len(partitions))
	var wg sync.WaitGroup

	for i, files := range partitions {
		wg.Add(1)
		go func(idx int, fileList []string) {
			defer wg.Done()
			partPrompt := FormatPartitionPrompt(fileList, idx+1, len(partitions))
			fullContext := partPrompt + "\n\n" + promptContext
			results[idx] = r.runAgent(ctx, stage.Agent, stage, fullContext)
			// Tag the agent result with the partition index for clarity.
			results[idx].Agent = fmt.Sprintf("%s[%d/%d]", stage.Agent, idx+1, len(partitions))
		}(i, files)
	}

	wg.Wait()
	return results
}

// countFiles returns the total number of files across all partitions.
func countFiles(partitions [][]string) int {
	total := 0
	for _, p := range partitions {
		total += len(p)
	}
	return total
}

// addSpent records cost from a completed agent and returns the new total.
func (r *Runner) addSpent(cost float64) float64 {
	return r.getSpent().Update(func(v float64) float64 { return v + cost })
}

// RemainingBudget returns the remaining cost budget, or 0 if unlimited.
func (r *Runner) RemainingBudget() float64 {
	if r.MaxCost <= 0 {
		return 0
	}
	remaining := r.MaxCost - r.getSpent().Get()
	if remaining < 0 {
		return 0
	}
	return remaining
}

// getSpent lazily initializes and returns the spent value.
func (r *Runner) getSpent() *csync.Value[float64] {
	if r.spent == nil {
		r.spent = csync.NewValue(0.0)
	}
	return r.spent
}

// runAgent executes a single agent and returns its result.
func (r *Runner) runAgent(ctx context.Context, agentName string, stage Stage, promptContext string) AgentResult {
	ctx, agentSpan := telemetry.Tracer().Start(ctx, "pipeline.agent",
		trace.WithAttributes(
			attribute.String("squad.agent", agentName),
			attribute.String("squad.pipeline.stage", stage.Name),
		),
	)
	defer agentSpan.End()

	start := time.Now()
	result := AgentResult{
		Agent:  agentName,
		Status: StatusPassed,
	}

	if r.MaxCost > 0 {
		remaining := r.RemainingBudget()
		if remaining <= 0 {
			result.Status = StatusFailed
			result.Duration = time.Since(start).Round(time.Millisecond).String()
			result.Output = "pipeline cost budget exceeded"
			logging.InfoContext(ctx, "pipeline: skipping agent %q — budget exhausted ($%.4f spent of $%.4f)", agentName, r.getSpent().Get(), r.MaxCost)
			return result
		}
		logging.InfoContext(ctx, "pipeline: agent %q budget: $%.4f remaining of $%.4f total", agentName, remaining, r.MaxCost)
	}

	prompt := r.Prompt
	if promptContext != "" {
		prompt = promptContext + "\n\n" + prompt
	}

	mode := stage.Mode
	// Copy stage vars so we can inject reserved keys without mutating the
	// shared Stage.Vars map (stages may run multiple agents).
	vars := make(map[string]string, len(stage.Vars)+2)
	for k, v := range stage.Vars {
		vars[k] = v
	}
	// Always inject the stage name so the RunAgent callback can look up
	// stage-scoped overrides (e.g. mcp_servers) on the Pipeline.
	vars[PipelineStageNameVar] = stage.Name

	// Inject effective budget cap for the agent via reserved var.
	// This is min(remaining pipeline budget, stage max_cost).
	if r.MaxCost > 0 || stage.MaxCost > 0 {
		effectiveCap := r.RemainingBudget()
		if stage.MaxCost > 0 && (effectiveCap <= 0 || stage.MaxCost < effectiveCap) {
			effectiveCap = stage.MaxCost
		}
		if effectiveCap > 0 {
			vars[PipelineMaxCostVar] = fmt.Sprintf("%.4f", effectiveCap)
		}
	}

	logging.InfoContext(ctx, "pipeline: running agent %q in stage %q", agentName, stage.Name)
	output, m, err := r.RunAgent(ctx, agentName, prompt, r.WorkingDir, mode, vars)
	result.Duration = time.Since(start).Round(time.Millisecond).String()
	result.Metrics = m
	result.Output = output

	// Track cost across the pipeline.
	if m != nil {
		totalSpent := r.addSpent(m.TotalCostWithChildren())
		logging.InfoContext(ctx, "pipeline: agent %q cost=$%.4f, pipeline total=$%.4f", agentName, m.TotalCostWithChildren(), totalSpent)
	}

	if err != nil {
		result.Status = StatusFailed
		result.Output = output // preserve partial output
		agentSpan.RecordError(err)
		agentSpan.SetStatus(codes.Error, err.Error())
		logging.InfoContext(ctx, "pipeline: agent %q failed: %v", agentName, err)
	} else {
		logging.InfoContext(ctx, "pipeline: agent %q completed (%d bytes)", agentName, len(output))
	}

	return result
}

// buildPromptContext creates context from prior stage results to pass to the next agent.
// When a shared FindingsStore is available, structured findings are included instead of
// raw output, providing compressed handoffs that reduce downstream token waste.
// When summarization is configured on a dependency stage, LLM-powered summarization
// replaces the hard truncation fallback.
func (r *Runner) buildPromptContext(ctx context.Context, stage Stage, completed map[string]*StageResult) string {
	if len(stage.DependsOn) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Prior Stage Results\n\n")

	// Include structured findings if available (compressed handoff).
	if r.Findings != nil && r.Findings.Count() > 0 {
		sb.WriteString("### Structured Findings from Prior Stages\n\n")
		findingsJSON, err := r.Findings.FormatJSON()
		if err == nil && len(findingsJSON) > 0 {
			sb.WriteString("The following findings were reported by prior agents. Focus on these\n")
			sb.WriteString("rather than re-reading files to discover the same issues.\n\n")
			sb.WriteString("```json\n")
			sb.WriteString(findingsJSON)
			sb.WriteString("\n```\n\n")
		}
	}

	// Build a stage lookup so we can check summarization config on deps.
	stageMap := make(map[string]Stage, len(r.Pipeline.Stages))
	for _, s := range r.Pipeline.Stages {
		stageMap[s.Name] = s
	}

	for _, dep := range stage.DependsOn {
		sr, ok := completed[dep]
		if !ok {
			continue
		}
		depStage := stageMap[dep]

		fmt.Fprintf(&sb, "### Stage: %s (status: %s)\n\n", sr.Name, sr.Status)
		for _, ar := range sr.Agents {
			fmt.Fprintf(&sb, "**Agent %s** (status: %s):\n", ar.Agent, ar.Status)
			summary := r.summarizeAgentOutput(ctx, depStage, sr.Name, ar)
			sb.WriteString(summary)
			sb.WriteString("\n\n")
		}
	}
	return sb.String()
}

// summarizeAgentOutput returns a compressed version of an agent's output.
// It checks the summary cache first, then tries LLM summarization if
// configured, and falls back to truncation.
func (r *Runner) summarizeAgentOutput(ctx context.Context, depStage Stage, stageName string, ar AgentResult) string {
	output := ar.Output
	cacheKey := stageName + "/" + ar.Agent

	// Check cache first.
	if r.sumCache != nil {
		if cached, ok := r.sumCache.get(cacheKey); ok {
			return cached
		}
	}

	var result string
	switch {
	case ShouldSummarize(depStage, len(output)):
		result = SummarizeOutput(ctx, r.Summarize, depStage, output)
	case len(output) > 4096:
		result = output[:4096] + "\n...(truncated)"
	default:
		result = output
	}

	// Cache the result.
	if r.sumCache == nil {
		r.sumCache = newSummaryCache()
	}
	r.sumCache.set(cacheKey, result)

	return result
}

// hasUncommittedChanges checks whether the working directory has any
// uncommitted file changes. Returns false if not a git repo or on error.
func (r *Runner) hasUncommittedChanges(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "git", "diff", "--stat", "HEAD")
	cmd.Dir = r.WorkingDir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		// Not a git repo or other error — assume changes exist to be safe.
		return true
	}
	return strings.TrimSpace(buf.String()) != ""
}

// runGates executes all gates configured after the named stage.
// Gates are skipped when the stage produced no file changes (nothing to regress).
func (r *Runner) runGates(ctx context.Context, stageName string, completed map[string]*StageResult) error {
	gates := r.Pipeline.GatesAfter(stageName)
	if len(gates) == 0 {
		return nil
	}

	// Skip gates if no files were changed — nothing to regress.
	if !r.hasUncommittedChanges(ctx) {
		logging.InfoContext(ctx, "pipeline: skipping gates after %q — no file changes detected", stageName)
		return nil
	}

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

// attachFindings copies findings from the store into the report.
func (r *Runner) attachFindings(report *Report) {
	if r.Findings != nil && r.Findings.Count() > 0 {
		report.Findings = r.Findings.All()
	}
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

	// Include aggregated findings if present.
	if len(report.Findings) > 0 {
		store := tools.NewFindingsStore()
		for _, f := range report.Findings {
			store.Add(f)
		}
		sb.WriteString(store.FormatMarkdown())
		sb.WriteString("\n")
	}

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
