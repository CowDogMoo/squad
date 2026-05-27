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

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
	pl "github.com/cowdogmoo/squad/pipeline"
	"github.com/cowdogmoo/squad/runner"
	"github.com/cowdogmoo/squad/source"
	"github.com/spf13/cobra"
)

// buildRunAgentFunc creates the callback used by the pipeline runner to execute
// individual agents. It handles agent resolution, bundle building, budget
// propagation, and model invocation.
func buildRunAgentFunc(opts *runner.RunOptions, agentsDir string, composedAgentDir string, cfg *config.Config, vars map[string]string, pipelineRunner *pl.Runner) func(ctx context.Context, agentName, agentPrompt, wd, mode string, stageVars map[string]string) (string, *metrics.Metrics, error) {
	return func(ctx context.Context, agentName, agentPrompt, wd, mode string, stageVars map[string]string) (string, *metrics.Metrics, error) {
		mergedVars := mergeVars(vars, stageVars)

		bundle, err := buildAgentBundle(agentName, agentPrompt, wd, mode, mergedVars, agentsDir, composedAgentDir, cfg, pipelineRunner)
		if err != nil {
			return "", nil, err
		}

		if err := applyStageMCPOverride(bundle, mergedVars, mode, composedAgentDir, pipelineRunner); err != nil {
			return "", nil, err
		}

		agentOpts := *opts
		agentOpts.Agent = agentName
		agentOpts.WorkingDir = wd
		agentOpts.Mode = mode
		agentOpts.Vars = mergedVars
		agentOpts.Findings = pipelineRunner.Findings
		agentOpts.AgentName = agentName

		warn, resolveErr := runner.ResolveModelPrecedence(ctx, &agentOpts, bundle)
		if resolveErr != nil {
			return "", nil, resolveErr
		}
		if warn != "" {
			fmt.Fprintln(os.Stderr, warn)
		}

		if err := applyPipelineBudgetCap(&agentOpts, mergedVars); err != nil {
			return "", nil, err
		}

		return runner.InvokeModel(ctx, &agentOpts, bundle)
	}
}

// buildAgentBundle builds the agent bundle, dispatching to inline or
// external loading based on whether the named agent is registered as
// an inline agent on the pipeline runner.
func buildAgentBundle(agentName, prompt, wd, mode string, mergedVars map[string]string, agentsDir, composedAgentDir string, cfg *config.Config, pipelineRunner *pl.Runner) (*agent.Bundle, error) {
	if inlineCfg, ok := pipelineRunner.InlineAgents[agentName]; ok && inlineCfg != nil {
		inlineAgent := &agent.InlineAgentConfig{
			Name:       agentName,
			EntryPoint: inlineCfg.EntryPoint,
			Wrapper:    inlineCfg.Wrapper,
			Task:       inlineCfg.Task,
			References: inlineCfg.References,
		}
		for _, m := range inlineCfg.Models {
			inlineAgent.Models = append(inlineAgent.Models, agent.ModelPreference{
				Model:    m.Model,
				Provider: m.Provider,
			})
		}
		bundle, err := agent.BuildBundleInline(pipelineRunner.ComposedDir, inlineAgent, prompt, wd, mode, mergedVars)
		if err != nil {
			return nil, fmt.Errorf("failed to build inline agent %s: %w", agentName, err)
		}
		return bundle, nil
	}

	resolvedAgentsDir, err := findAgentDirForComposed(agentName, agentsDir, composedAgentDir, cfg)
	if err != nil {
		return nil, err
	}
	bundle, err := agent.BuildBundle(resolvedAgentsDir, agentName, prompt, wd, mode, mergedVars)
	if err != nil {
		return nil, fmt.Errorf("failed to build agent %s: %w", agentName, err)
	}
	return bundle, nil
}

// applyStageMCPOverride replaces the bundle's MCP server list with the
// running stage's override, if one is declared. Non-empty list overrides;
// explicit empty list disables MCP for the stage; nil inherits from the
// agent's manifest. Consumes the reserved stage-name var.
func applyStageMCPOverride(bundle *agent.Bundle, mergedVars map[string]string, mode, composedAgentDir string, pipelineRunner *pl.Runner) error {
	stageName, ok := mergedVars[pl.PipelineStageNameVar]
	if !ok {
		return nil
	}
	delete(mergedVars, pl.PipelineStageNameVar)
	stage := pipelineRunner.Pipeline.StageByName(stageName)
	if stage == nil || stage.MCPServers == nil {
		return nil
	}
	if err := agent.ApplyMCPOverride(bundle, stage.MCPServers, mode, composedAgentDir, mergedVars); err != nil {
		return fmt.Errorf("stage %q: mcp override: %w", stageName, err)
	}
	return nil
}

// applyPipelineBudgetCap reads the reserved per-agent cost cap injected
// by the pipeline runner and applies it to the run options. Consumes
// the reserved var. An unparsable or zero cap signals an exhausted
// pipeline budget.
func applyPipelineBudgetCap(agentOpts *runner.RunOptions, mergedVars map[string]string) error {
	capStr, ok := mergedVars[pl.PipelineMaxCostVar]
	if !ok {
		return nil
	}
	delete(mergedVars, pl.PipelineMaxCostVar)
	var capVal float64
	if _, err := fmt.Sscanf(capStr, "%f", &capVal); err != nil || capVal <= 0 {
		return fmt.Errorf("pipeline budget exhausted")
	}
	agentOpts.MaxCost = capVal
	return nil
}

// outputReport formats and writes the pipeline report to stdout and/or a file.
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
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatted); err != nil {
			logging.Warn("failed to print report: %v", err)
		}
	}
	if outFile != "" {
		if writeErr := os.WriteFile(outFile, []byte(formatted), 0o644); writeErr != nil {
			logging.Warn("failed to write report: %v", writeErr)
		} else {
			logging.InfoContext(cmd.Context(), "report written to %s", outFile)
		}
	}
}

// buildComposedRunOpts creates RunOptions for composed agent execution from
// CLI flags and Viper config.
func buildComposedRunOpts(cmd *cobra.Command, cfg *config.Config) *runner.RunOptions {
	v := viperFromContext(cmd)

	// Split provider/model into explicit (CLI) vs config-default buckets
	// so manifest preferences can win over config defaults.
	var providerVal, modelVal string
	if v != nil {
		providerVal = v.GetString("provider.default")
		modelVal = v.GetString("model.default")
	}
	var explicitProvider, configProvider, explicitModel, configModel string
	if cmd.Flags().Changed("provider") {
		explicitProvider, _ = cmd.Flags().GetString("provider")
	} else {
		configProvider = providerVal
	}
	if cmd.Flags().Changed("model") {
		explicitModel, _ = cmd.Flags().GetString("model")
	} else {
		configModel = modelVal
	}
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
		Provider:        explicitProvider,
		Model:           explicitModel,
		ConfigProvider:  configProvider,
		ConfigModel:     configModel,
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

// mergeVars combines base and override variable maps, with override taking precedence.
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

// resolveAgentsDirFromConfig resolves the agents directory from config sources.
func resolveAgentsDirFromConfig(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("no config provided and no explicit agents directory specified")
	}
	manager, err := source.NewManager(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create agent source manager: %w", err)
	}
	paths, err := manager.GetSearchPaths()
	if err != nil {
		return "", fmt.Errorf("failed to get agent search paths: %w", err)
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no agent source directories configured")
	}
	return paths[0], nil
}

// findAgentDirForComposed locates an agent's parent directory for composed agent execution.
// It checks for nested sub-agents inside composedDir/agents/ first, then falls back to
// global search paths and the default directory.
func findAgentDirForComposed(agentName, defaultDir, composedDir string, cfg *config.Config) (string, error) {
	// Check for nested sub-agents inside the composed agent's directory.
	if composedDir != "" {
		nestedDir := filepath.Join(composedDir, "agents")
		manifestPath := filepath.Join(nestedDir, agentName, "agent.yaml")
		if _, err := os.Stat(manifestPath); err == nil {
			return nestedDir, nil
		}
	}

	if cfg != nil {
		manager, err := source.NewManager(cfg)
		if err == nil {
			agentDir, err := manager.FindAgent(agentName)
			if err == nil {
				return filepath.Dir(agentDir), nil
			}
		}
	}
	return defaultDir, nil
}
