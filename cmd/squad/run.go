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

	"github.com/cowdogmoo/squad/runner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// newRunOptions creates a RunOptions by reading resolved values from flags and Viper.
func newRunOptions(cmd *cobra.Command) *runner.RunOptions {
	v := viperFromContext(cmd)
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

	return &runner.RunOptions{
		Agent:             agent,
		AgentsDir:         agentsDir,
		WorkingDir:        workingDir,
		APIKey:            v.GetString("provider.token"),
		BaseURL:           v.GetString("provider.base_url"),
		Org:               v.GetString("provider.organization"),
		APIVersion:        v.GetString("provider.api_version"),
		APIType:           v.GetString("provider.api_type"),
		OpenAICompatMax:   v.GetBool("provider.openai_compat_max_tokens"),
		Provider:          v.GetString("provider.default"),
		Model:             v.GetString("model.default"),
		Temperature:       v.GetFloat64("model.temperature"),
		MaxTokens:         v.GetInt("model.max_tokens"),
		System:            system,
		Output:            output,
		Print:             printOut,
		BundleOut:         bundleOut,
		PrintBundle:       printBundle,
		DryRun:            dryRun,
		RequireActionable: requireActionable,
		Apply:             apply,
		ApplyFallback:     applyFallback,
		NumCtx:            v.GetInt("provider.num_ctx"),
		Mode:              mode,
		ConfigAvailable:   configFromContext(cmd) != nil,
	}
}

// bindRunFlags binds the run command's flags to Viper keys so that env vars
// and config file values participate in precedence resolution.
func bindRunFlags(cmd *cobra.Command, v *viper.Viper) error {
	flags := cmd.Flags()
	bind := func(key string, f string) error {
		return v.BindPFlag(key, flags.Lookup(f))
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
	// run-scoped flags
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
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return nil
			}
			if hasPipedInput(cmd.InOrStdin()) {
				return nil
			}
			return fmt.Errorf("prompt is required (pass args or pipe stdin)")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := newRunOptions(cmd)
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

	cmd.MarkFlagsMutuallyExclusive("dry-run", "apply")

	// Dynamic completions for --agent (scan agents directories).
	_ = cmd.RegisterFlagCompletionFunc("agent", completeAgentNames)
	// Static completions for --provider.
	_ = cmd.RegisterFlagCompletionFunc("provider", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"openai", "openai-responses", "anthropic", "ollama", "gemini"}, cobra.ShellCompDirectiveNoFileComp
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
