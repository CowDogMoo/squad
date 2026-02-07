// Package runner orchestrates agent execution workflows and model calls.
package runner

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/tools"
	"github.com/spf13/cobra"
)

// RunOptions holds the resolved configuration for a single run invocation.
type RunOptions struct {
	Agent             string
	AgentsDir         string
	WorkingDir        string
	APIKey            string
	BaseURL           string
	Org               string
	APIVersion        string
	APIType           string
	OpenAICompatMax   bool
	Provider          string
	Model             string
	Temperature       float64
	MaxTokens         int
	System            string
	Output            string
	Print             bool
	BundleOut         string
	PrintBundle       bool
	DryRun            bool
	RequireActionable bool
	Apply             bool
	ApplyFallback     bool
	NumCtx            int
	MaxIterations     int
	Mode              string
	ConfigAvailable   bool
}

// ExecuteRun contains the full run command logic, parameterized by RunOptions.
func ExecuteRun(cmd *cobra.Command, args []string, opts *RunOptions) error {
	prompt, err := readPrompt(cmd, args)
	if err != nil {
		return err
	}

	workingDir, err := resolveWorkingDir(opts.WorkingDir)
	if err != nil {
		return err
	}

	bundle, err := prepareBundle(cmd, opts, prompt, workingDir)
	if err != nil {
		return err
	}
	if bundle == nil {
		return nil // dry-run
	}

	// Ensure resolved paths are available for TaskConfig.
	opts.WorkingDir = workingDir

	ctx := tools.InitEdits(cmd.Context())
	cmd.SetContext(ctx)
	tools.ResetEditsApplied(ctx)
	response, m, err := invokeModel(ctx, opts, bundle)
	if err != nil {
		if m != nil {
			printMetrics(cmd, m)
		}
		return err
	}

	if err := handleResponse(cmd, opts, response, workingDir); err != nil {
		if m != nil {
			printMetrics(cmd, m)
		}
		return err
	}

	if m != nil {
		printMetrics(cmd, m)
	}
	return nil
}

// printMetrics outputs the metrics summary to stderr.
func printMetrics(cmd *cobra.Command, m *metrics.Metrics) {
	if m == nil {
		return
	}
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), m.Summary())
}

// prepareBundle builds the agent bundle and handles bundle output. Returns nil bundle for dry-run.
func prepareBundle(cmd *cobra.Command, opts *RunOptions, prompt, workingDir string) (*agent.Bundle, error) {
	agentsDir, err := resolveAgentsDir(opts.AgentsDir)
	if err != nil {
		return nil, err
	}
	opts.AgentsDir = agentsDir

	bundle, err := agent.BuildBundle(agentsDir, opts.Agent, prompt, workingDir, opts.Mode)
	if err != nil {
		return nil, err
	}

	logging.InfoContext(cmd.Context(), "agent bundle ready (agent=%s provider=%s model=%s)", opts.Agent, opts.Provider, opts.Model)

	if opts.PrintBundle {
		if _, err := io.Copy(cmd.OutOrStdout(), bytes.NewReader(bundle.Combined)); err != nil {
			return nil, err
		}
	}

	if opts.BundleOut != "" {
		if err := os.WriteFile(opts.BundleOut, bundle.Combined, 0o644); err != nil {
			return nil, fmt.Errorf("failed to write bundle: %w", err)
		}
		logging.InfoContext(cmd.Context(), "bundle written to %s", opts.BundleOut)
	}

	if opts.DryRun {
		return nil, nil
	}

	if !opts.ConfigAvailable {
		return nil, fmt.Errorf("config not available in context")
	}

	return bundle, nil
}

// handleResponse validates, applies, and writes the model response.
func handleResponse(cmd *cobra.Command, opts *RunOptions, response, workingDir string) error {
	if opts.RequireActionable {
		if err := validateActionableResponse(cmd.Context(), response); err != nil {
			return err
		}
	}

	if opts.Apply {
		if err := applyResponseDiff(cmd.Context(), response, workingDir, opts.ApplyFallback); err != nil {
			return err
		}
	}

	return writeResponse(cmd, response, opts)
}

func writeResponse(cmd *cobra.Command, response string, opts *RunOptions) error {
	if opts.Print || opts.Output == "" {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), response); err != nil {
			return err
		}
	}
	if opts.Output != "" {
		if err := os.WriteFile(opts.Output, []byte(response), 0o644); err != nil {
			return fmt.Errorf("failed to write response: %w", err)
		}
		logging.InfoContext(cmd.Context(), "response written to %s", opts.Output)
	}
	return nil
}

func readPrompt(cmd *cobra.Command, args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}

	input, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("failed to read stdin: %w", err)
	}
	prompt := strings.TrimSpace(string(input))
	if prompt == "" {
		return "", fmt.Errorf("prompt is required (pass args or pipe stdin)")
	}
	return prompt, nil
}

func resolveWorkingDir(dir string) (string, error) {
	if dir == "" {
		return os.Getwd()
	}
	return filepath.Abs(dir)
}

func resolveAgentsDir(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}

	if stat, err := os.Stat("agents"); err == nil && stat.IsDir() {
		return filepath.Abs("agents")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve agents dir: %w", err)
	}
	return filepath.Join(home, ".config", "squad", "agents"), nil
}
