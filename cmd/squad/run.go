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
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/mcp"
	pl "github.com/cowdogmoo/squad/pipeline"
	"github.com/cowdogmoo/squad/runner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// parseVars parses KEY=VALUE strings into a map.
func parseVars(vars []string) map[string]string {
	if len(vars) == 0 {
		return nil
	}
	result := make(map[string]string)
	for _, v := range vars {
		if idx := strings.Index(v, "="); idx > 0 {
			result[v[:idx]] = v[idx+1:]
		}
	}
	return result
}

// newRunOptions creates a RunOptions by reading resolved values from flags and Viper.
func newRunOptions(cmd *cobra.Command) *runner.RunOptions {
	v := viperFromContext(cmd)
	if v == nil {
		return nil
	}
	agent := v.GetString("run.agent")
	agentsDir := v.GetString("run.agents_dir")
	workingDir := v.GetString("run.working_dir")
	system := v.GetString("run.system")
	output := v.GetString("run.out")
	printOut := v.GetBool("run.print")
	bundleOut := v.GetString("run.bundle_out")
	printBundle := v.GetBool("run.print_bundle")
	dryRun := v.GetBool("run.dry_run")
	requireActionable := v.GetBool("run.require_actionable")
	apply := v.GetBool("run.apply")
	applyFallback := v.GetBool("run.apply_fallback")
	mode := v.GetString("run.mode")

	varStrings, _ := cmd.Flags().GetStringArray("var")
	vars := parseVars(varStrings)

	mcpStrings, _ := cmd.Flags().GetStringArray("mcp-server")
	mcpServers := parseMCPServers(mcpStrings)

	maxIter := v.GetInt("run.max_iterations")
	if maxIter < 10 {
		maxIter = 10
	} else if maxIter > 1000 {
		maxIter = 1000
	}

	maxCost := v.GetFloat64("run.max_cost")
	if maxCost < 0 {
		maxCost = 0
	}

	cfg := configFromContext(cmd)
	return &runner.RunOptions{
		Agent:              agent,
		AgentsDir:          agentsDir,
		WorkingDir:         workingDir,
		APIKey:             v.GetString("provider.token"),
		BaseURL:            v.GetString("provider.base_url"),
		Org:                v.GetString("provider.organization"),
		APIVersion:         v.GetString("provider.api_version"),
		APIType:            v.GetString("provider.api_type"),
		OpenAICompatMax:    v.GetBool("provider.openai_compat_max_tokens"),
		Provider:           v.GetString("provider.default"),
		Model:              v.GetString("model.default"),
		Temperature:        v.GetFloat64("model.temperature"),
		MaxTokens:          v.GetInt("model.max_tokens"),
		System:             system,
		Output:             output,
		Print:              printOut,
		BundleOut:          bundleOut,
		PrintBundle:        printBundle,
		DryRun:             dryRun,
		RequireActionable:  requireActionable,
		Apply:              apply,
		ApplyFallback:      applyFallback,
		NumCtx:             v.GetInt("provider.num_ctx"),
		MaxIterations:      maxIter,
		MaxCost:            maxCost,
		Mode:               mode,
		Vars:               vars,
		ConfigAvailable:    cfg != nil,
		Config:             cfg,
		MCPServers:         mcpServers,
		Stream:             v.GetBool("run.stream"),
		MaxConcurrentTasks: v.GetInt("run.max_concurrent_tasks"),
	}
}

// bindRunFlags binds the run command's flags to Viper keys so that env vars
// and config file values participate in precedence resolution.
func bindRunFlags(cmd *cobra.Command, v *viper.Viper) error {
	flags := cmd.Flags()
	bind := func(key string, f string) error {
		if err := v.BindPFlag(key, flags.Lookup(f)); err != nil {
			return fmt.Errorf("failed to bind flag %q to %q: %w", f, key, err)
		}
		return nil
	}
	if err := bind("provider.default", "provider"); err != nil {
		return err
	}
	if err := bind("model.default", "model"); err != nil {
		return err
	}
	if err := bind("model.temperature", "temperature"); err != nil {
		return err
	}
	if err := bind("model.max_tokens", "max-tokens"); err != nil {
		return err
	}
	if err := bind("provider.base_url", "base-url"); err != nil {
		return err
	}
	if err := bind("provider.organization", "organization"); err != nil {
		return err
	}
	if err := bind("provider.api_version", "api-version"); err != nil {
		return err
	}
	if err := bind("provider.api_type", "api-type"); err != nil {
		return err
	}
	if err := bind("provider.openai_compat_max_tokens", "openai-compat-max-tokens"); err != nil {
		return err
	}
	if err := bind("provider.token", "api-key"); err != nil {
		return err
	}
	if err := bind("provider.num_ctx", "num-ctx"); err != nil {
		return err
	}
	for _, pair := range [][2]string{
		{"run.agent", "agent"},
		{"run.agents_dir", "agents-dir"},
		{"run.working_dir", "working-dir"},
		{"run.system", "system"},
		{"run.out", "out"},
		{"run.print", "print"},
		{"run.bundle_out", "bundle-out"},
		{"run.print_bundle", "print-bundle"},
		{"run.dry_run", "dry-run"},
		{"run.require_actionable", "require-actionable"},
		{"run.apply", "apply"},
		{"run.apply_fallback", "apply-fallback"},
		{"run.mode", "mode"},
		{"run.max_iterations", "max-iterations"},
		{"run.max_cost", "max-cost"},
		{"run.stream", "stream"},
		{"run.max_concurrent_tasks", "max-concurrent-tasks"},
	} {
		if err := bind(pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "run [prompt]",
		Aliases: []string{"r"},
		Short:   "Run an agent workflow",
		Long: `Run an agent workflow with an optional prompt.

If no prompt is provided via arguments or stdin, the agent's default
user_prompt will be used (if configured in the agent's manifest).`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return nil
			}
			if hasPipedInput(cmd.InOrStdin()) {
				return nil
			}
			// Allow no prompt - the agent bundle will use default user_prompt if available.
			// If the agent has no default, BuildBundle will return an error.
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Silence usage for runtime errors (API key missing, etc).
			// Args validation errors still show usage.
			cmd.SilenceUsage = true
			opts := newRunOptions(cmd)
			if opts == nil {
				return fmt.Errorf("run configuration not initialized")
			}

			// Check if the agent is a composed agent (has stages).
			// If so, fork to the composed agent execution path.
			if opts.Agent != "" {
				agentDir, err := runner.FindAgentDir(opts.Agent, opts.AgentsDir, opts.Config)
				if err == nil {
					manifest, mErr := agent.LoadManifest(agentDir)
					if mErr == nil && manifest.IsComposed() {
						return runComposedAgent(cmd, args, opts, manifest, agentDir)
					}
				}
			}

			return runner.ExecuteRun(cmd, args, opts)
		},
	}

	cmd.Flags().String("agent", "", "Agent name (e.g. go-cobra)")
	cmd.Flags().String("agents-dir", "", "Agents directory (default: ./agents, then ~/.config/squad/agents)")
	cmd.Flags().String("working-dir", "", "Working directory (default: current working directory)")
	cmd.Flags().String("api-key", "", "API key (overrides env/config)")
	cmd.Flags().String("base-url", "", "Base URL override for provider")
	cmd.Flags().String("organization", "", "Organization ID (OpenAI-compatible)")
	cmd.Flags().String("api-version", "", "API version (Azure/OpenAI-compatible)")
	cmd.Flags().String("api-type", "", "API type (openai or azure)")
	cmd.Flags().Bool("openai-compat-max-tokens", false, "Use max_tokens for OpenAI-compatible endpoints")
	cmd.Flags().String("provider", "", "Model provider (openai, anthropic, gemini, ollama, etc)")
	cmd.Flags().String("model", "", "Model name")
	cmd.Flags().Float64("temperature", -1, "Sampling temperature (default from config)")
	cmd.Flags().Int("max-tokens", -1, "Max output tokens (default from config)")
	cmd.Flags().String("system", "", "System prompt override")
	cmd.Flags().String("out", "", "Write response to a file")
	cmd.Flags().Bool("print", true, "Print response to stdout")
	cmd.Flags().String("bundle-out", "", "Write agent bundle to a file")
	cmd.Flags().Bool("print-bundle", false, "Print agent bundle to stdout")
	cmd.Flags().Bool("dry-run", false, "Build bundle and exit without calling the model")
	cmd.Flags().Bool("require-actionable", true, "Require actionable output (diff/files/no changes)")
	cmd.Flags().Bool("apply", false, "Apply unified diff from the response to the working directory")
	cmd.Flags().Bool("apply-fallback", false, "Fallback to patch(1) if git apply fails (may create .rej/.orig)")
	cmd.Flags().Int("num-ctx", 32768, "Context window size for Ollama models")
	cmd.Flags().String("mode", "", "Agent mode override (e.g. readonly)")
	cmd.Flags().Int("max-iterations", 100, "Maximum tool-calling iterations (range: 10-1000)")
	cmd.Flags().Float64("max-cost", 5, "Maximum cost budget in USD (0 = unlimited)")
	cmd.Flags().StringArray("var", nil, "Template variable in KEY=VALUE format (can be repeated)")
	cmd.Flags().StringArray("mcp-server", nil, "MCP server: stdio NAME:COMMAND[:ARG1,ARG2,...] or SSE NAME:sse:URL (can be repeated)")
	cmd.Flags().Bool("stream", false, "Stream model output tokens to stderr as they arrive")
	cmd.Flags().Int("max-concurrent-tasks", 0, "Max concurrent background child tasks (default: 4)")
	cmd.Flags().Bool("json", false, "Force JSON output format (composed agents only)")

	cmd.MarkFlagsMutuallyExclusive("dry-run", "apply")

	// Dynamic completions for --agent (scan agents directories).
	_ = cmd.RegisterFlagCompletionFunc("agent", completeAgentNames)
	// Static completions for --provider.
	_ = cmd.RegisterFlagCompletionFunc("provider", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"openai", "openai-responses", "anthropic", "ollama", "gemini"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("api-type", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"openai", "azure"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("model", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

func hasPipedInput(r io.Reader) bool {
	if f, ok := r.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil {
			return false
		}
		return (fi.Mode() & os.ModeCharDevice) == 0
	}
	if b, ok := r.(*bytes.Buffer); ok {
		return b.Len() > 0
	}
	return false
}

func completeAgentNames(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	dirs := []string{"agents"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", "squad", "agents"))
	}
	var names []string
	seen := make(map[string]bool)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !seen[e.Name()] && strings.HasPrefix(e.Name(), toComplete) {
				seen[e.Name()] = true
				names = append(names, e.Name())
			}
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// runComposedAgent executes a composed agent (one with stages) by converting
// its manifest to a pipeline and running it through the pipeline runner.
func runComposedAgent(cmd *cobra.Command, args []string, opts *runner.RunOptions, manifest *agent.Manifest, agentDir string) error {
	if err := validateComposedFlags(cmd); err != nil {
		return err
	}

	p, err := runner.ManifestToPipeline(manifest)
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		return composedDryRun(cmd, manifest, p)
	}

	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available")
	}

	prompt := ""
	if len(args) > 0 {
		prompt = strings.Join(args, " ")
	} else if hasPipedInput(cmd.InOrStdin()) {
		input, readErr := io.ReadAll(cmd.InOrStdin())
		if readErr != nil {
			return fmt.Errorf("failed to read stdin: %w", readErr)
		}
		prompt = strings.TrimSpace(string(input))
	}

	workingDir := opts.WorkingDir
	if workingDir == "" {
		workingDir, err = os.Getwd()
		if err != nil {
			return err
		}
	} else {
		workingDir, err = filepath.Abs(workingDir)
		if err != nil {
			return err
		}
	}

	agentsDir := filepath.Dir(agentDir)
	varStrings, _ := cmd.Flags().GetStringArray("var")
	vars := parseVars(varStrings)

	pipelineOpts := buildComposedRunOpts(cmd, cfg)

	logging.InfoContext(cmd.Context(), "composed agent: starting %q with %d stages (max_cost=$%.2f)",
		manifest.Name, len(p.Stages), opts.MaxCost)

	pipelineRunner := &pl.Runner{
		Pipeline:   p,
		WorkingDir: workingDir,
		Prompt:     prompt,
		MaxCost:    opts.MaxCost,
	}
	pipelineRunner.RunAgent = buildRunAgentFunc(pipelineOpts, agentsDir, cfg, vars, pipelineRunner)

	report, runErr := pipelineRunner.Run(cmd.Context())

	if report != nil {
		outputReport(cmd, p, pipelineRunner, report)
	}

	return runErr
}

// validateComposedFlags checks that no incompatible flags are set for composed agents.
func validateComposedFlags(cmd *cobra.Command) error {
	for _, flag := range runner.ComposedFlags {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("--%s is not applicable to composed agents (sub-agents declare their own configuration)", flag)
		}
	}
	return nil
}

// composedDryRun validates and prints the composed agent's pipeline structure.
func composedDryRun(cmd *cobra.Command, manifest *agent.Manifest, p *pl.Pipeline) error {
	w := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(w, "Composed agent %q (%s) validated: %d stages\n\n", manifest.Name, manifest.Version, len(p.Stages)); err != nil {
		return fmt.Errorf("failed to write dry-run output: %w", err)
	}

	tiers := p.TopologicalOrder()
	for i, tier := range tiers {
		if _, err := fmt.Fprintf(w, "Tier %d:\n", i+1); err != nil {
			return fmt.Errorf("failed to write dry-run output: %w", err)
		}
		for _, stage := range tier {
			agents := stage.AgentList()
			mode := stage.Mode
			if mode == "" {
				mode = "edit"
			}
			if _, err := fmt.Fprintf(w, "  Stage %q [mode=%s]: %s\n", stage.Name, mode, strings.Join(agents, ", ")); err != nil {
				return fmt.Errorf("failed to write dry-run output: %w", err)
			}
			if len(stage.DependsOn) > 0 {
				if _, err := fmt.Fprintf(w, "    depends_on: %s\n", strings.Join(stage.DependsOn, ", ")); err != nil {
					return fmt.Errorf("failed to write dry-run output: %w", err)
				}
			}
		}
	}

	if len(p.Gates) > 0 {
		if _, err := fmt.Fprintf(w, "\nGates:\n"); err != nil {
			return fmt.Errorf("failed to write dry-run output: %w", err)
		}
		for _, g := range p.Gates {
			onFailure := g.OnFailure
			if onFailure == "" {
				onFailure = "stop"
			}
			if _, err := fmt.Fprintf(w, "  after %q: %s (on_failure=%s)\n", g.After, g.Command, onFailure); err != nil {
				return fmt.Errorf("failed to write dry-run output: %w", err)
			}
		}
	}

	return nil
}

// parseMCPServers parses MCP server specs into ServerConfig.
// Supported formats:
//
//	Stdio: NAME:COMMAND[:ARG1,ARG2,...]
//	SSE:   NAME:sse:URL
func parseMCPServers(specs []string) []mcp.ServerConfig {
	if len(specs) == 0 {
		return nil
	}
	var configs []mcp.ServerConfig
	for _, spec := range specs {
		parts := strings.SplitN(spec, ":", 3)
		if len(parts) < 2 {
			continue
		}

		// Detect SSE transport: NAME:sse:URL
		if strings.EqualFold(parts[1], "sse") {
			if len(parts) < 3 || parts[2] == "" {
				continue
			}
			configs = append(configs, mcp.ServerConfig{
				Name:      parts[0],
				Transport: "sse",
				URL:       parts[2],
			})
			continue
		}

		// Default: stdio transport NAME:COMMAND[:ARG1,ARG2,...]
		cfg := mcp.ServerConfig{
			Name:    parts[0],
			Command: parts[1],
		}
		if len(parts) == 3 && parts[2] != "" {
			cfg.Args = strings.Split(parts[2], ",")
		}
		configs = append(configs, cfg)
	}
	return configs
}
