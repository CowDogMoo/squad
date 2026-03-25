/*
Copyright © 2026 Jayson Grace <jayson.e.grace@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
	pl "github.com/cowdogmoo/squad/pipeline"
	"github.com/cowdogmoo/squad/runner"
	"github.com/cowdogmoo/squad/source"
	"github.com/spf13/cobra"
)

func newPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Run multi-agent pipelines",
		Long:  "Execute declarative multi-agent pipelines defined in YAML.",
	}

	cmd.AddCommand(newPipelineRunCmd())
	return cmd
}

func newPipelineRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <pipeline.yaml> [prompt]",
		Short: "Execute a pipeline",
		Long: `Execute a multi-agent pipeline defined in a YAML file.

The pipeline file declares stages with agents, dependencies, gates, and
output format. Agents within a stage run in parallel; stages execute in
dependency order.

Examples:
  # Run a pipeline
  squad pipeline run recon.yaml "Assess the target system"

  # Run with cost limit and output file
  squad pipeline run security-audit.yaml --max-cost 5.00 --out report.md

  # Dry run to validate the pipeline
  squad pipeline run recon.yaml --dry-run`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return runPipeline(cmd, args)
		},
	}

	cmd.Flags().String("agents-dir", "", "Agents directory (default: ./agents, then ~/.config/squad/agents)")
	cmd.Flags().String("working-dir", "", "Working directory (default: current working directory)")
	cmd.Flags().String("api-key", "", "API key (overrides env/config)")
	cmd.Flags().String("base-url", "", "Base URL override for provider")
	cmd.Flags().String("organization", "", "Organization ID")
	cmd.Flags().String("api-version", "", "API version")
	cmd.Flags().String("api-type", "", "API type (openai or azure)")
	cmd.Flags().Bool("openai-compat-max-tokens", false, "Use max_tokens for OpenAI-compatible endpoints")
	cmd.Flags().String("provider", "", "Model provider")
	cmd.Flags().String("model", "", "Model name")
	cmd.Flags().Float64("temperature", -1, "Sampling temperature")
	cmd.Flags().Int("max-tokens", -1, "Max output tokens")
	cmd.Flags().Int("max-iterations", 100, "Max tool-calling iterations per agent (10-1000)")
	cmd.Flags().Float64("max-cost", 10, "Max total cost budget in USD for entire pipeline (0 = unlimited)")
	cmd.Flags().String("out", "", "Write report to file")
	cmd.Flags().Bool("print", true, "Print report to stdout")
	cmd.Flags().Bool("dry-run", false, "Validate pipeline and exit without running")
	cmd.Flags().Bool("json", false, "Force JSON output format")
	cmd.Flags().Int("num-ctx", 32768, "Context window size for Ollama models")
	cmd.Flags().StringArray("var", nil, "Template variable in KEY=VALUE format (can be repeated)")

	return cmd
}

func runPipeline(cmd *cobra.Command, args []string) error {
	pipelinePath := args[0]
	prompt := ""
	if len(args) > 1 {
		prompt = strings.Join(args[1:], " ")
	}

	// Load and validate the pipeline.
	p, err := pl.Load(pipelinePath)
	if err != nil {
		return fmt.Errorf("failed to load pipeline: %w", err)
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Pipeline %q (%s) validated: %d stages\n", p.Name, p.Version, len(p.Stages))
		return err
	}

	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available")
	}

	workingDir, err := resolveWorkingDir(cmd)
	if err != nil {
		return err
	}

	maxCost, _ := cmd.Flags().GetFloat64("max-cost")
	if maxCost < 0 {
		maxCost = 0
	}

	opts := buildPipelineRunOpts(cmd, cfg)

	agentsDir, err := resolveAgentsDir(cmd, cfg)
	if err != nil {
		return err
	}

	varStrings, _ := cmd.Flags().GetStringArray("var")
	vars := parseVars(varStrings)

	logging.InfoContext(cmd.Context(), "pipeline: starting %q with %d stages (max_cost=$%.2f)", p.Name, len(p.Stages), maxCost)

	pipelineRunner := &pl.Runner{
		Pipeline:   p,
		WorkingDir: workingDir,
		Prompt:     prompt,
		MaxCost:    maxCost,
	}
	pipelineRunner.RunAgent = buildRunAgentFunc(opts, agentsDir, cfg, vars, pipelineRunner)

	report, err := pipelineRunner.Run(cmd.Context())

	// Always try to output the report, even on error.
	if report != nil {
		outputReport(cmd, p, pipelineRunner, report)
	}

	return err
}

func resolveWorkingDir(cmd *cobra.Command) (string, error) {
	workingDir, _ := cmd.Flags().GetString("working-dir")
	if workingDir == "" {
		return os.Getwd()
	}
	return filepath.Abs(workingDir)
}

func resolveAgentsDir(cmd *cobra.Command, cfg *config.Config) (string, error) {
	agentsDir, _ := cmd.Flags().GetString("agents-dir")
	if agentsDir == "" {
		return resolveAgentsDirForPipeline(cfg)
	}
	return filepath.Abs(agentsDir)
}

func buildRunAgentFunc(opts *runner.RunOptions, agentsDir string, cfg *config.Config, vars map[string]string, pipelineRunner *pl.Runner) func(ctx context.Context, agentName, agentPrompt, wd, mode string, stageVars map[string]string) (string, *metrics.Metrics, error) {
	return func(ctx context.Context, agentName, agentPrompt, wd, mode string, stageVars map[string]string) (string, *metrics.Metrics, error) {
		mergedVars := mergeVars(vars, stageVars)

		resolvedAgentsDir, agentErr := findAgentDirForPipeline(agentName, agentsDir, cfg)
		if agentErr != nil {
			return "", nil, agentErr
		}

		bundle, agentErr := agent.BuildBundle(resolvedAgentsDir, agentName, agentPrompt, wd, mode, mergedVars)
		if agentErr != nil {
			return "", nil, fmt.Errorf("failed to build agent %s: %w", agentName, agentErr)
		}

		agentOpts := *opts
		agentOpts.Agent = agentName
		agentOpts.AgentsDir = resolvedAgentsDir
		agentOpts.WorkingDir = wd
		agentOpts.Mode = mode
		agentOpts.Vars = mergedVars
		agentOpts.Findings = pipelineRunner.Findings
		agentOpts.AgentName = agentName

		return runner.InvokeModel(ctx, &agentOpts, bundle)
	}
}

func outputReport(cmd *cobra.Command, p *pl.Pipeline, pipelineRunner *pl.Runner, report *pl.Report) {
	forceJSON, _ := cmd.Flags().GetBool("json")
	if forceJSON && (p.Output == nil || p.Output.Format != "json") {
		if p.Output == nil {
			p.Output = &pl.Output{}
		}
		p.Output.Format = "json"
	}

	formatted, fmtErr := pipelineRunner.FormatReport(report)
	if fmtErr != nil {
		logging.Warn("failed to format report: %v", fmtErr)
		return
	}

	printOut, _ := cmd.Flags().GetBool("print")
	outFile, _ := cmd.Flags().GetString("out")

	if printOut || outFile == "" {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), formatted)
	}
	if outFile != "" {
		if writeErr := os.WriteFile(outFile, []byte(formatted), 0o644); writeErr != nil {
			logging.Warn("failed to write report: %v", writeErr)
		} else {
			logging.InfoContext(cmd.Context(), "pipeline report written to %s", outFile)
		}
	}
}

func buildPipelineRunOpts(cmd *cobra.Command, cfg *config.Config) *runner.RunOptions {
	v := viperFromContext(cmd)

	provider := flagOrViper(cmd, "provider", v, "provider.default")
	model := flagOrViper(cmd, "model", v, "model.default")
	apiKey := flagOrViper(cmd, "api-key", v, "provider.token")
	baseURL := flagOrViper(cmd, "base-url", v, "provider.base_url")
	org := flagOrViper(cmd, "organization", v, "provider.organization")
	apiVersion := flagOrViper(cmd, "api-version", v, "provider.api_version")
	apiType := flagOrViper(cmd, "api-type", v, "provider.api_type")

	openAICompatMax, _ := cmd.Flags().GetBool("openai-compat-max-tokens")
	temp, _ := cmd.Flags().GetFloat64("temperature")
	maxTokens, _ := cmd.Flags().GetInt("max-tokens")
	numCtx, _ := cmd.Flags().GetInt("num-ctx")

	maxIter, _ := cmd.Flags().GetInt("max-iterations")
	if maxIter < 10 {
		maxIter = 10
	} else if maxIter > 1000 {
		maxIter = 1000
	}

	maxCost, _ := cmd.Flags().GetFloat64("max-cost")
	if maxCost < 0 {
		maxCost = 0
	}

	return &runner.RunOptions{
		APIKey:          apiKey,
		BaseURL:         baseURL,
		Org:             org,
		APIVersion:      apiVersion,
		APIType:         apiType,
		OpenAICompatMax: openAICompatMax,
		Provider:        provider,
		Model:           model,
		Temperature:     temp,
		MaxTokens:       maxTokens,
		NumCtx:          numCtx,
		MaxIterations:   maxIter,
		MaxCost:         maxCost,
		ConfigAvailable: cfg != nil,
		Config:          cfg,
	}
}

// flagOrViper returns the flag value if explicitly set, otherwise the Viper value.
func flagOrViper(cmd *cobra.Command, flagName string, v interface{ GetString(string) string }, viperKey string) string {
	if cmd.Flags().Changed(flagName) {
		val, _ := cmd.Flags().GetString(flagName)
		return val
	}
	if v != nil {
		return v.GetString(viperKey)
	}
	return ""
}

func mergeVars(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]string)
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
	return merged
}

// resolveAgentsDirForPipeline finds the agents directory using the same logic as the run command.
func resolveAgentsDirForPipeline(cfg *config.Config) (string, error) {
	// Check local ./agents directory first.
	if stat, err := os.Stat("agents"); err == nil && stat.IsDir() {
		return filepath.Abs("agents")
	}

	// Use XDG config directories.
	for _, configDir := range config.GetConfigDirs() {
		agentsDir := filepath.Join(configDir, "agents")
		if stat, err := os.Stat(agentsDir); err == nil && stat.IsDir() {
			return agentsDir, nil
		}
	}

	dirs := config.GetConfigDirs()
	if len(dirs) > 0 {
		return filepath.Join(dirs[0], "agents"), nil
	}
	return "", fmt.Errorf("failed to resolve agents dir")
}

// findAgentDirForPipeline locates an agent's parent directory.
func findAgentDirForPipeline(agentName, defaultDir string, cfg *config.Config) (string, error) {
	// Try source manager first.
	if cfg != nil {
		manager, err := source.NewManager(cfg)
		if err == nil {
			agentDir, err := manager.FindAgent(agentName)
			if err == nil {
				return filepath.Dir(agentDir), nil
			}
		}
	}

	// Fall back to default directory.
	return defaultDir, nil
}
