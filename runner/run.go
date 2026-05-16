// Package runner orchestrates agent execution workflows and model calls.
//
// The primary entry points are [ExecuteRun] for single-agent invocations and
// [InvokeModel] for direct model dispatch (used by the pipeline runner and the
// Task tool). [ResolveModelPrecedence] handles the three-layer model selection
// hierarchy (CLI flag → config default → agent manifest). [FindAgentDir]
// locates an agent directory using the configured source manager.
package runner

import (
	"bytes"
	"context"
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
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/source"
	"github.com/cowdogmoo/squad/tools"
	"github.com/spf13/cobra"
)

// RunOptions holds the resolved configuration for a single run invocation.
type RunOptions struct {
	// Agent is the name of the agent to run (must exist under AgentsDir).
	Agent string
	// AgentsDir is the directory containing agent sub-directories.
	AgentsDir string
	// WorkingDir is the directory the agent operates in.
	WorkingDir string
	// APIKey is the provider API key (overrides config/env).
	APIKey string
	// BaseURL overrides the provider's default API endpoint.
	BaseURL string
	// Org is the OpenAI organization ID.
	Org string
	// APIVersion is used by Azure OpenAI and other versioned APIs.
	APIVersion string
	// APIType selects the API variant (e.g., "azure").
	APIType string
	// OpenAICompatMax enforces max_tokens for OpenAI-compatible providers.
	OpenAICompatMax bool
	// Provider is the explicitly requested model provider (from a CLI flag
	// or routine definition). Highest precedence; empty falls through to
	// manifest then ConfigProvider.
	Provider string
	// Model is the explicitly requested model identifier (from a CLI flag
	// or routine definition). Highest precedence; empty falls through to
	// manifest then ConfigModel.
	Model string
	// ConfigProvider holds the provider default from the loaded config
	// file. Used only when neither the CLI/routine nor the agent manifest
	// specifies a provider. Kept separate from Provider so manifest
	// preferences can win over config defaults.
	ConfigProvider string
	// ConfigModel holds the model default from the loaded config file.
	// Used only when neither the CLI/routine nor the agent manifest
	// specifies a model. Kept separate from Model so manifest preferences
	// can win over config defaults.
	ConfigModel string
	// Temperature controls sampling randomness.
	Temperature float64
	// MaxTokens is the per-request output token budget.
	MaxTokens int
	// System overrides the agent's assembled system prompt.
	System string
	// Output is the file path for writing the model's final response.
	Output string
	// Print reports whether to write the response to stdout.
	Print bool
	// BundleOut is the file path for writing the assembled prompt bundle.
	BundleOut string
	// PrintBundle reports whether to print the bundle to stdout.
	PrintBundle bool
	// DryRun reports whether to estimate cost and exit without running.
	DryRun bool
	// RequireActionable reports whether the run must produce file edits.
	RequireActionable bool
	// Apply reports whether to apply a unified diff from the response.
	Apply bool
	// ApplyFallback reports whether to attempt diff parsing as a fallback.
	ApplyFallback bool
	// NumCtx is the context window size for Ollama models.
	NumCtx int
	// MaxIterations caps the number of model iterations (0 = unlimited).
	MaxIterations int
	// MaxCost is the total cost budget in USD (0 = unlimited).
	MaxCost float64
	// Mode is the run mode passed to prompt templates (e.g., "edit").
	Mode string
	// Vars holds template variables passed to prompt templates.
	Vars map[string]string
	// ConfigAvailable reports whether a config file was loaded.
	ConfigAvailable bool
	// Config is the loaded squad configuration.
	Config *config.Config
	// Findings is a shared findings store set by the pipeline runner.
	Findings *tools.FindingsStore
	// AgentName is the current agent name used for finding attribution.
	AgentName string
	// MCPServers lists MCP servers declared via --mcp-server flags.
	MCPServers []mcp.ServerConfig
	// Stream reports whether to stream response tokens to stderr.
	Stream bool
	// MaxConcurrentTasks is the maximum number of concurrent background
	// child tasks (0 = default).
	MaxConcurrentTasks int
	// ResumeID is the session ID to resume; empty starts a new session.
	ResumeID string
	// ResumeResponseID is the OpenAI response ID to chain from, set by
	// openSession when resuming an existing session.
	ResumeResponseID string
	// NoSession disables session logging (e.g., for tests).
	NoSession bool
	// Isolation overrides the agent manifest's isolation preference.
	// Empty falls through to manifest, then config, then IsolationNone.
	Isolation string
	// RoutineID, when set, is stamped onto the session's meta.json so
	// `squad routine history` can filter by exact provenance. Qualified
	// form: "global:nightly" / "repo:audit".
	RoutineID string
	// LastSessionID is an OUTPUT field populated by ExecuteRun with the
	// session id it created (or resumed). Empty when NoSession is true or
	// session creation failed. Callers like the routines daemon read this
	// after ExecuteRun returns to record which session a fire produced.
	LastSessionID string
}

// ExecuteRun runs a single agent invocation end-to-end. It reads the prompt,
// resolves working directory and isolation, builds the agent bundle, opens a
// session log, calls the model, and writes the response. Budget-exceeded runs
// apply any partial response before returning the error.
//
// The named return runErr is used so the deferred session-close always records
// the final error, including failures from post-processing steps like diff
// application.
func ExecuteRun(cmd *cobra.Command, args []string, opts *RunOptions) (runErr error) {
	prompt, err := readPrompt(cmd, args)
	if err != nil {
		return err
	}

	workingDir, err := resolveWorkingDir(opts.WorkingDir)
	if err != nil {
		return err
	}

	iso, err := setupIsolation(cmd.Context(), opts, workingDir)
	if err != nil {
		return err
	}
	defer reportIsolationTeardown(cmd, iso)
	workingDir = iso.Effective

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

	logger, err := openSession(opts, bundle, prompt)
	if err != nil {
		logging.Warn("failed to open session log: %v", err)
	}
	if logger != nil {
		opts.LastSessionID = logger.SessionID()
		if opts.RoutineID != "" {
			logger.SetRoutineID(opts.RoutineID)
		}
		ctx = session.WithLogger(ctx, logger)
		if _, fmtErr := fmt.Fprintf(cmd.ErrOrStderr(), "Session: %s\n", logger.SessionID()); fmtErr != nil {
			logging.Warn("failed to write session banner: %v", fmtErr)
		}
	}

	cmd.SetContext(ctx)
	tools.ResetEditsApplied(ctx)

	var m *metrics.Metrics
	var response string
	response, m, runErr = InvokeModel(ctx, opts, bundle)

	defer func() {
		logRunHistory(opts, m)
		if metricsErr := printMetrics(cmd, m); metricsErr != nil {
			logging.Warn("failed to print metrics: %v", metricsErr)
		}
		closeSession(logger, m, runErr)
	}()

	if runErr != nil {
		handleBudgetExceeded(cmd, opts, m, response, workingDir, runErr)
		return
	}

	runErr = handleResponse(cmd, opts, response, workingDir)
	return
}

// handleBudgetExceeded reports a budget-exceeded run and applies any partial
// response that was produced before the budget cap stopped the loop.
func handleBudgetExceeded(cmd *cobra.Command, opts *RunOptions, m *metrics.Metrics, response, workingDir string, err error) {
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		return
	}
	if _, fmtErr := fmt.Fprintf(cmd.ErrOrStderr(), "Run stopped: cost budget of $%.4f exceeded (actual: $%.4f)\n", opts.MaxCost, m.TotalCostWithChildren()); fmtErr != nil {
		logging.Warn("failed to write budget warning: %v", fmtErr)
	}
	if response == "" {
		return
	}
	if handleErr := handleResponse(cmd, opts, response, workingDir); handleErr != nil {
		logging.Warn("failed to handle partial response: %v", handleErr)
	}
}

// printMetrics outputs the metrics summary to stderr.
func printMetrics(cmd *cobra.Command, m *metrics.Metrics) error {
	if m == nil {
		return nil
	}
	_, err := fmt.Fprintln(cmd.ErrOrStderr(), m.Summary())
	return err
}

// ModelResolution holds the resolved model, provider, base URL, and any
// advisory warning produced by ResolveModelPrecedence. Callers apply these
// values back to RunOptions so that the mutation is visible at the call site.
type ModelResolution struct {
	Model    string
	Provider string
	BaseURL  string
	// Warning is non-empty when the config default is not listed in the agent
	// manifest's preferred model list but is used anyway.
	Warning string
}

// ResolveModelPrecedence resolves the model, provider, and base URL using the
// precedence: explicit (CLI flag / routine field) > config default > manifest.
// It returns a ModelResolution the caller must apply to RunOptions; the opts
// argument is read-only. Warning is non-empty when the config default is not
// listed in the manifest's preferred models but is used anyway.
func ResolveModelPrecedence(ctx context.Context, opts *RunOptions, bundle *agent.Bundle) ModelResolution {
	if opts == nil || bundle == nil {
		return ModelResolution{}
	}

	res := ModelResolution{
		Model:    opts.Model,
		Provider: opts.Provider,
		BaseURL:  opts.BaseURL,
	}

	if res.Model == "" {
		configMatch := bundle.FindModel(opts.ConfigProvider, opts.ConfigModel)

		switch {
		case configMatch != nil:
			// Config default is listed in the manifest — use it; provider is also set.
			res.Model = configMatch.Model
			res.Provider = configMatch.Provider
			if res.BaseURL == "" {
				res.BaseURL = configMatch.BaseURL
			}
			logging.InfoContext(ctx, "using config default model: %s (%s)", res.Model, res.Provider)

		case opts.ConfigModel != "" && bundle.Model != "":
			// Config default not in the manifest's preferred list — warn but still use it.
			res.Warning = fmt.Sprintf(
				"⚠  Config default model %q (%s) is not a preferred model for this agent.\n"+
					"   Running with configured default.",
				opts.ConfigModel, opts.ConfigProvider)
			logging.WarnContext(ctx, "config default model %q (%s) is not listed in agent manifest; running with configured default",
				opts.ConfigModel, opts.ConfigProvider)
			res.Model = opts.ConfigModel
			res.Provider = opts.ConfigProvider

		case bundle.Model != "":
			res.Model = bundle.Model
			logging.InfoContext(ctx, "using manifest model: %s", bundle.Model)

		case opts.ConfigModel != "":
			res.Model = opts.ConfigModel
			logging.InfoContext(ctx, "using config model: %s", opts.ConfigModel)
		}
	}

	if res.Provider == "" {
		switch {
		case bundle.Provider != "":
			res.Provider = bundle.Provider
			logging.InfoContext(ctx, "using manifest provider: %s", bundle.Provider)
		case opts.ConfigProvider != "":
			res.Provider = opts.ConfigProvider
			logging.InfoContext(ctx, "using config provider: %s", opts.ConfigProvider)
		}
	}

	if res.BaseURL == "" && bundle.BaseURL != "" {
		res.BaseURL = bundle.BaseURL
	}

	return res
}

// prepareBundle builds the agent bundle and handles bundle output. Returns nil bundle for dry-run.
func prepareBundle(cmd *cobra.Command, opts *RunOptions, prompt, workingDir string) (*agent.Bundle, error) {
	agentDir, err := FindAgentDir(opts.Agent, opts.AgentsDir, opts.Config)
	if err != nil {
		return nil, err
	}

	agentsDir := filepath.Dir(agentDir)
	opts.AgentsDir = agentsDir

	bundle, err := agent.BuildBundle(agentsDir, opts.Agent, prompt, workingDir, opts.Mode, opts.Vars)
	if err != nil {
		return nil, err
	}

	res := ResolveModelPrecedence(cmd.Context(), opts, bundle)
	opts.Model = res.Model
	opts.Provider = res.Provider
	opts.BaseURL = res.BaseURL
	if res.Warning != "" {
		if _, fmtErr := fmt.Fprintln(cmd.ErrOrStderr(), res.Warning); fmtErr != nil {
			logging.Warn("failed to write model warning: %v", fmtErr)
		}
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
			if _, fmtErr := fmt.Fprint(cmd.ErrOrStderr(), metrics.FormatEstimate(estimate, opts.Provider, opts.Model)); fmtErr != nil {
				logging.Warn("failed to write cost estimate: %v", fmtErr)
			}
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

// writeResponse prints response to stdout when Print is set or no output file
// is configured, and writes it to opts.Output when that path is non-empty.
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

// openSession creates or resumes a session log under workingDir/.squad/sessions.
// Returns nil (with no error) when session logging is disabled.
func openSession(opts *RunOptions, bundle *agent.Bundle, prompt string) (*session.Logger, error) {
	if opts.NoSession {
		return nil, nil
	}
	if opts.ResumeID != "" {
		l, err := session.Open(opts.WorkingDir, opts.ResumeID)
		if err != nil {
			return nil, err
		}
		// Pull the prior response id so the Responses API call chains
		// server-side state from where the previous run left off.
		opts.ResumeResponseID = l.LastResponseID()
		_ = l.Append(session.EventResume, map[string]any{
			"agent":            opts.Agent,
			"prompt":           prompt,
			"prev_response_id": opts.ResumeResponseID,
		})
		return l, nil
	}
	l, err := session.New(opts.WorkingDir, opts.Agent, opts.Provider, opts.Model, prompt)
	if err != nil {
		return nil, err
	}
	_ = l.Append(session.EventRunStart, map[string]any{
		"agent":        opts.Agent,
		"provider":     opts.Provider,
		"model":        opts.Model,
		"mode":         opts.Mode,
		"system_bytes": len(bundle.System),
	})
	return l, nil
}

// closeSession finalizes the session log with status and metrics.
func closeSession(l *session.Logger, m *metrics.Metrics, runErr error) {
	if l == nil {
		return
	}
	if m != nil {
		l.UpdateMetrics(m.InputTokens(), m.OutputTokens(), m.TotalCostWithChildren(), m.Iterations())
	}
	status := session.StatusCompleted
	errMsg := ""
	switch {
	case errors.Is(runErr, metrics.ErrBudgetExceeded):
		status = session.StatusBudget
	case runErr != nil:
		status = session.StatusError
		errMsg = runErr.Error()
	}
	l.Finish(status, errMsg)
	if cerr := l.Close(); cerr != nil {
		logging.Warn("failed to close session log: %v", cerr)
	}
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

// readPrompt returns the user prompt from positional arguments or, when no
// arguments are supplied, from piped stdin. It returns an empty string when
// stdin is a terminal or produces only whitespace; the agent's default
// user_prompt template will be used in that case.
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

// resolveWorkingDir returns the absolute path of dir, or the current working
// directory when dir is empty.
func resolveWorkingDir(dir string) (string, error) {
	if dir == "" {
		return os.Getwd()
	}
	return filepath.Abs(dir)
}

// setupIsolation resolves the effective IsolationMode from CLI/manifest/config
// precedence and prepares the worktree (if any). The returned Isolation must
// be torn down via reportIsolationTeardown after the run completes.
func setupIsolation(ctx context.Context, opts *RunOptions, workingDir string) (*Isolation, error) {
	manifestVal := manifestIsolation(opts)
	configVal := ""
	if opts.Config != nil {
		configVal = opts.Config.Run.Isolation
	}
	mode, err := ResolveIsolationMode(opts.Isolation, manifestVal, configVal)
	if err != nil {
		return nil, err
	}
	return PrepareIsolation(ctx, workingDir, mode, opts.Agent)
}

// manifestIsolation reads only the isolation field from the agent manifest.
// Returns empty string when the manifest cannot be located or parsed; any
// real loading error will surface again from prepareBundle.
func manifestIsolation(opts *RunOptions) string {
	if opts.Agent == "" {
		return ""
	}
	agentDir, err := FindAgentDir(opts.Agent, opts.AgentsDir, opts.Config)
	if err != nil {
		return ""
	}
	m, err := agent.LoadManifest(agentDir)
	if err != nil {
		return ""
	}
	return m.Isolation
}

// reportIsolationTeardown runs the worktree teardown and prints a notice on
// stderr when the worktree was retained for review.
func reportIsolationTeardown(cmd *cobra.Command, iso *Isolation) {
	if iso == nil {
		return
	}
	kept, path := iso.Teardown(cmd.Context())
	if kept {
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Worktree retained: %s (branch %s)\n", path, iso.Branch); err != nil {
			logging.Warn("failed to write isolation notice: %v", err)
		}
	}
}

// FindAgentDir locates an agent by name using the source manager.
// It returns the path to the agent directory (containing agent.yaml).
func FindAgentDir(agentName, explicitDir string, cfg *config.Config) (string, error) {
	if explicitDir != "" {
		absDir, err := filepath.Abs(explicitDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(absDir, agentName), nil
	}

	if cfg == nil {
		return "", fmt.Errorf("no config provided and no explicit agents directory specified")
	}

	manager, err := source.NewManager(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create agent source manager: %w", err)
	}

	agentDir, err := manager.FindAgent(agentName)
	if err != nil {
		return "", fmt.Errorf("agent %q not found: %w", agentName, err)
	}
	return agentDir, nil
}
