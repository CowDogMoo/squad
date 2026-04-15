// Package runner orchestrates agent execution workflows and model calls.
package runner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/mcp"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/source"
	"github.com/cowdogmoo/squad/tools"
	"github.com/spf13/cobra"
)

// RunOptions holds the resolved configuration for a single run invocation.
type RunOptions struct {
	Agent              string
	AgentsDir          string
	WorkingDir         string
	APIKey             string
	BaseURL            string
	Org                string
	APIVersion         string
	APIType            string
	OpenAICompatMax    bool
	Provider           string
	Model              string
	Temperature        float64
	MaxTokens          int
	System             string
	Output             string
	Print              bool
	BundleOut          string
	PrintBundle        bool
	DryRun             bool
	RequireActionable  bool
	Apply              bool
	ApplyFallback      bool
	NumCtx             int
	MaxIterations      int
	MaxCost            float64
	Mode               string
	Vars               map[string]string // Template variables (e.g., COVERAGE_TARGET=85)
	ConfigAvailable    bool
	Config             *config.Config
	Findings           *tools.FindingsStore // shared findings store (set by pipeline runner)
	AgentName          string               // current agent name for finding attribution
	MCPServers         []mcp.ServerConfig   // MCP servers from CLI --mcp-server flags
	Stream             bool                 // stream model output tokens to stderr as they arrive
	MaxConcurrentTasks int                  // max concurrent background child tasks (0 = default)
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

	opts.WorkingDir = workingDir

	ctx := tools.InitEdits(cmd.Context())
	ctx = tools.InitEditDeadline(ctx)
	cmd.SetContext(ctx)
	tools.ResetEditsApplied(ctx)
	response, m, err := InvokeModel(ctx, opts, bundle)

	defer func() {
		logRunHistory(opts, m)
		if metricsErr := printMetrics(cmd, m); metricsErr != nil {
			logging.Warn("failed to print metrics: %v", metricsErr)
		}
	}()

	if err != nil {
		if errors.Is(err, metrics.ErrBudgetExceeded) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Run stopped: cost budget of $%.4f exceeded (actual: $%.4f)\n", opts.MaxCost, m.TotalCostWithChildren())
			if response != "" {
				if handleErr := handleResponse(cmd, opts, response, workingDir); handleErr != nil {
					logging.Warn("failed to handle partial response: %v", handleErr)
				}
			}
		}
		return err
	}

	if err := handleResponse(cmd, opts, response, workingDir); err != nil {
		return err
	}

	return nil
}

// printMetrics outputs the metrics summary to stderr.
func printMetrics(cmd *cobra.Command, m *metrics.Metrics) error {
	if m == nil {
		return nil
	}
	_, err := fmt.Fprintln(cmd.ErrOrStderr(), m.Summary())
	return err
}

// prepareBundle builds the agent bundle and handles bundle output. Returns nil bundle for dry-run.
func prepareBundle(cmd *cobra.Command, opts *RunOptions, prompt, workingDir string) (*agent.Bundle, error) {
	agentDir, err := findAgentDir(opts.Agent, opts.AgentsDir, opts.Config)
	if err != nil {
		return nil, err
	}

	agentsDir := filepath.Dir(agentDir)
	opts.AgentsDir = agentsDir

	bundle, err := agent.BuildBundle(agentsDir, opts.Agent, prompt, workingDir, opts.Mode, opts.Vars)
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
		estimate, err := metrics.EstimateCost(agentsDir, opts.Agent, opts.Provider, opts.Model)
		if err != nil {
			logging.Warn("cost estimation failed: %v", err)
		} else {
			_, _ = fmt.Fprint(cmd.ErrOrStderr(), metrics.FormatEstimate(estimate, opts.Provider, opts.Model))
		}
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

// logRunHistory persists token usage to the cost history cache.
func logRunHistory(opts *RunOptions, m *metrics.Metrics) {
	if m == nil || opts.Agent == "" {
		return
	}
	cacheDir := config.CacheDir()
	if cacheDir == "" {
		return
	}
	metrics.LogRunHistory(cacheDir, opts.Agent, m)
}

func readPrompt(cmd *cobra.Command, args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}

	if f, ok := cmd.InOrStdin().(*os.File); ok {
		fi, err := f.Stat()
		if err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
			input, err := io.ReadAll(f)
			if err != nil {
				return "", fmt.Errorf("failed to read stdin: %w", err)
			}
			prompt := strings.TrimSpace(string(input))
			if prompt != "" {
				return prompt, nil
			}
		}
	}

	return "", nil
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

	// Use XDG config directories
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
	return "", fmt.Errorf("failed to resolve agents dir: no config directories available")
}

// findAgentDir locates an agent by name using the source manager.
// Falls back to the legacy single-directory resolution if config is unavailable.
func findAgentDir(agentName, explicitDir string, cfg *config.Config) (string, error) {
	// If explicit directory is provided, use it directly
	if explicitDir != "" {
		absDir, err := filepath.Abs(explicitDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(absDir, agentName), nil
	}

	// Use agent source manager if config is available
	if cfg != nil {
		manager, err := source.NewManager(cfg)
		if err != nil {
			// Fall through to legacy resolution
			logging.Warn("failed to create agent source manager: %v", err)
		} else {
			agentDir, err := manager.FindAgent(agentName)
			if err == nil {
				return agentDir, nil
			}
			// Log but don't fail - try legacy resolution
			logging.Debug("agent not found via source manager: %v", err)
		}
	}

	// Legacy resolution: ./agents or ~/.config/squad/agents
	agentsDir, err := resolveAgentsDir("")
	if err != nil {
		return "", err
	}
	return filepath.Join(agentsDir, agentName), nil
}
