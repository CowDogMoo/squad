// Package runner orchestrates agent execution workflows and model calls.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	// MaxIterationsExplicit reports whether MaxIterations came from an
	// explicit --max-iterations flag. When true the value is used verbatim;
	// when false the runner derives the effective cap from the agent's base
	// budget and the per-model iteration factor (see resolveIterationBudget).
	MaxIterationsExplicit bool
	// MaxCost is the total cost budget in USD (0 = unlimited).
	MaxCost float64
	// MaxRetries overrides the per-call LLM retry count for transient
	// errors. Zero or negative falls back to tools.DefaultMaxRetries.
	MaxRetries int
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
	// SkillOverrides applies per-run adjustments to the agent's skill
	// catalog (--allow-skill / --deny-skill / --skills-enabled flags).
	// nil means "use agent.yaml defaults".
	SkillOverrides *agent.SkillOverrides
	// AutoConfirm is the resolution policy for the Confirm tool in
	// non-TTY runs. Empty (the default) means "abort" so unattended runs
	// fail loudly when a skill needs a human checkpoint.
	AutoConfirm tools.AutoConfirmMode
	// CanonicalRepoPath is the pre-isolation git toplevel of the working dir.
	CanonicalRepoPath string
	// WorktreePath is the ephemeral worktree path (if isolation is used).
	WorktreePath string
}

// ExecuteRun contains the full run command logic, parameterized by RunOptions.
func ExecuteRun(cmd *cobra.Command, args []string, opts *RunOptions) error {
	prompt, err := readPrompt(cmd, args)
	if err != nil {
		return err
	}

	workingDir, cleanup, err := resolveRunWorkingDir(opts)
	if err != nil {
		return err
	}
	defer cleanup()

	canonicalRepoPath := ResolveGitToplevel(workingDir)
	opts.CanonicalRepoPath = canonicalRepoPath

	iso, err := setupIsolation(cmd.Context(), opts, workingDir)
	if err != nil {
		return err
	}
	defer reportIsolationTeardown(cmd, iso)

	recordWorktreePath(opts, iso, workingDir)
	workingDir = iso.Effective

	bundle, err := prepareBundle(cmd, opts, prompt, workingDir)
	if err != nil {
		return err
	}
	if bundle == nil {
		return nil // dry-run
	}

	opts.WorkingDir = workingDir

	// Remote-only agents never produce file diffs; the "must edit code"
	// guard is a code-editing-agent concern that doesn't apply here.
	if bundle.RemoteOnly && opts.RequireActionable {
		opts.RequireActionable = false
		logging.InfoContext(cmd.Context(), "remote-only agent: disabling require-actionable")
	}

	ctx := initRunContext(cmd.Context(), bundle)

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
	response, m, err := InvokeModel(ctx, opts, bundle)

	defer func() {
		logRunHistory(opts, m)
		if metricsErr := printMetrics(cmd, m); metricsErr != nil {
			logging.Warn("failed to print metrics: %v", metricsErr)
		}
		closeSession(logger, m, err)
	}()

	if err != nil {
		handleBudgetExceeded(cmd, opts, m, response, workingDir, err)
		return err
	}

	if err := handleResponse(cmd, opts, response, workingDir); err != nil {
		return err
	}

	return nil
}

// initRunContext attaches per-run tool state (edits tracker, edit-deadline
// counter, and opt-in modes from the manifest) to ctx before the agent
// invocation. Returning a single helper keeps ExecuteRun's setup linear
// regardless of how many opt-in modes get added over time.
func initRunContext(ctx context.Context, bundle *agent.Bundle) context.Context {
	ctx = tools.InitEdits(ctx)
	ctx = tools.InitEditDeadline(ctx)
	if bundle.CommentsOnly {
		ctx = tools.InitCommentsOnlyMode(ctx)
		logging.InfoContext(ctx, "comments-only mode: Edit/MultiEdit will reject non-comment changes")
	}
	return ctx
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

// ResolveModelPrecedence selects the model/provider for this run by walking,
// in order:
//
//  1. Explicit caller intent (opts.Model from a CLI flag or routine field).
//     Always wins.
//  2. Config default (opts.ConfigModel / opts.ConfigProvider) when the user
//     has credentials for that provider. The config model may sit outside the
//     agent manifest's models: list — that earns a warning, but the user's
//     explicit intent is honoured because credentials, cost ceilings, and
//     provider routing only live in the user's config.
//  3. Manifest models walked in ranked order: the first entry with
//     credentials wins. Skipping the top-ranked entry due to a missing key
//     also earns a warning so the user knows they fell off the preferred
//     path.
//  4. As a last resort, the config default is used without credentials (with
//     a strong warning that the call will likely fail). If even that is
//     unavailable, a non-nil error is returned listing the env vars that
//     would unblock the run.
//
// Returns (warning, error). Callers should display the warning on stderr when
// non-empty and abort on a non-nil error.
func ResolveModelPrecedence(ctx context.Context, opts *RunOptions, bundle *agent.Bundle) (string, error) {
	if opts == nil || bundle == nil {
		return "", nil
	}
	if opts.Model != "" {
		applyExplicitModel(opts, bundle)
		return "", nil
	}
	if warn, ok := applyConfigDefault(ctx, opts, bundle); ok {
		return warn, nil
	}
	warn, picked, skipped := walkManifestForCreds(ctx, opts, bundle)
	if picked {
		return warn, nil
	}
	if opts.ConfigModel != "" {
		return applyConfigDefaultUnauthenticated(ctx, opts, bundle), nil
	}
	if err := noCredentialsError(skipped); err != nil {
		return "", err
	}
	applyLegacyBundleFallback(opts, bundle)
	return "", nil
}

// applyExplicitModel handles rule 1: opts.Model was set explicitly. Fill in
// Provider and BaseURL from the manifest match (preferred), bundle primary,
// or config, without overriding values the caller already set.
func applyExplicitModel(opts *RunOptions, bundle *agent.Bundle) {
	if opts.Provider == "" {
		for i := range bundle.Models {
			if bundle.Models[i].Model != opts.Model {
				continue
			}
			opts.Provider = bundle.Models[i].Provider
			if opts.BaseURL == "" {
				opts.BaseURL = bundle.Models[i].BaseURL
			}
			break
		}
	}
	if opts.Provider == "" {
		switch {
		case bundle.Provider != "":
			opts.Provider = bundle.Provider
		case opts.ConfigProvider != "":
			opts.Provider = opts.ConfigProvider
		}
	}
	if opts.BaseURL == "" && bundle.BaseURL != "" {
		opts.BaseURL = bundle.BaseURL
	}
}

// applyConfigDefault handles rule 2: use the config default when its provider
// has detected credentials. Returns (warn, true) when the config default was
// applied; (",", false) when caller should fall through to manifest walk.
func applyConfigDefault(ctx context.Context, opts *RunOptions, bundle *agent.Bundle) (string, bool) {
	if opts.ConfigModel == "" {
		return "", false
	}
	if metrics.KeyStatus(opts.ConfigProvider, opts.APIKey).State == metrics.APIKeyMissing {
		return "", false
	}
	opts.Model = opts.ConfigModel
	if opts.Provider == "" {
		opts.Provider = opts.ConfigProvider
	}
	match := bundle.FindModel(opts.Provider, opts.Model)
	fillBaseURL(opts, match, bundle)
	if match == nil && len(bundle.Models) > 0 {
		logging.WarnContext(ctx, "config default model %q (%s) is not listed in agent manifest; proceeding with config default", opts.Model, opts.Provider)
		warn := fmt.Sprintf(
			"⚠  Config default model %q (%s) is not listed in the agent manifest's models: list.\n"+
				"   Proceeding with the config default; add it to the manifest's models: list to silence this warning.",
			opts.Model, opts.Provider)
		return warn, true
	}
	logging.InfoContext(ctx, "using config default model: %s (%s)", opts.Model, opts.Provider)
	return "", true
}

// walkManifestForCreds handles rule 3: scan manifest models in rank order and
// pick the first one whose provider has detected credentials. Returns
// (warning, picked, skipped). When picked is false, skipped contains every
// manifest entry that lacked credentials so the caller can build an error.
func walkManifestForCreds(ctx context.Context, opts *RunOptions, bundle *agent.Bundle) (string, bool, []agent.ModelPreference) {
	var skipped []agent.ModelPreference
	for i := range bundle.Models {
		candidate := bundle.Models[i]
		if metrics.KeyStatus(candidate.Provider, opts.APIKey).State == metrics.APIKeyMissing {
			skipped = append(skipped, candidate)
			continue
		}
		opts.Model = candidate.Model
		if opts.Provider == "" {
			opts.Provider = candidate.Provider
		}
		if opts.BaseURL == "" {
			opts.BaseURL = candidate.BaseURL
		}
		if opts.BaseURL == "" && bundle.BaseURL != "" {
			opts.BaseURL = bundle.BaseURL
		}
		if len(skipped) == 0 {
			logging.InfoContext(ctx, "using manifest model: %s (%s)", opts.Model, opts.Provider)
			return "", true, nil
		}
		top := skipped[0]
		topEnv := metrics.KeyStatus(top.Provider, "").EnvVar
		logging.WarnContext(ctx, "manifest top model %q (%s) lacks credentials; using %q (%s) instead", top.Model, top.Provider, opts.Model, opts.Provider)
		warn := fmt.Sprintf(
			"⚠  Manifest's preferred model %q (%s) has no detected credentials; using %q (%s) instead.\n"+
				"   Set %s to use the agent's preferred model.",
			top.Model, top.Provider, opts.Model, opts.Provider, topEnv)
		return warn, true, nil
	}
	return "", false, skipped
}

// applyConfigDefaultUnauthenticated handles rule 4a: nothing in the manifest
// had credentials, but a config default exists — proceed with it so the user
// sees a clear "auth will fail" warning rather than a silent override.
func applyConfigDefaultUnauthenticated(ctx context.Context, opts *RunOptions, bundle *agent.Bundle) string {
	opts.Model = opts.ConfigModel
	if opts.Provider == "" {
		opts.Provider = opts.ConfigProvider
	}
	match := bundle.FindModel(opts.Provider, opts.Model)
	fillBaseURL(opts, match, bundle)
	logging.WarnContext(ctx, "no provider has detected credentials; proceeding with config default %q (%s)", opts.Model, opts.Provider)
	return fmt.Sprintf(
		"⚠  No provider has detected credentials. Proceeding with config default %q (%s);\n"+
			"   the model call will likely fail authentication. Set the relevant API key to fix.",
		opts.Model, opts.Provider)
}

// noCredentialsError handles rule 4b: manifest had entries but none had
// credentials, and there is no config default. Returns an error listing the
// env vars that would unblock the run, or nil if there is nothing to report.
func noCredentialsError(skipped []agent.ModelPreference) error {
	if len(skipped) == 0 {
		return nil
	}
	seen := map[string]bool{}
	envs := make([]string, 0, len(skipped))
	for _, m := range skipped {
		env := metrics.KeyStatus(m.Provider, "").EnvVar
		if env == "" || seen[env] {
			continue
		}
		seen[env] = true
		envs = append(envs, env)
	}
	if len(envs) == 0 {
		return nil
	}
	return fmt.Errorf(
		"no provider in agent manifest has credentials configured; set one of: %s",
		strings.Join(envs, ", "))
}

// applyLegacyBundleFallback preserves the pre-credential-aware behaviour for
// the edge case of an empty manifest where the bundle still has Model /
// Provider / BaseURL populated directly. Production bundles built by
// BuildBundle always co-populate Models, so this only triggers on synthetic
// bundles used by early-exit / dry-run callers and tests.
func applyLegacyBundleFallback(opts *RunOptions, bundle *agent.Bundle) {
	if opts.Model == "" && bundle.Model != "" {
		opts.Model = bundle.Model
	}
	if opts.Provider == "" && bundle.Provider != "" {
		opts.Provider = bundle.Provider
	}
	if opts.BaseURL == "" && bundle.BaseURL != "" {
		opts.BaseURL = bundle.BaseURL
	}
}

// fillBaseURL sets opts.BaseURL from the manifest match (preferred) or the
// bundle's primary BaseURL, leaving an explicit caller-set BaseURL alone.
func fillBaseURL(opts *RunOptions, match *agent.ModelPreference, bundle *agent.Bundle) {
	if opts.BaseURL != "" {
		return
	}
	if match != nil && match.BaseURL != "" {
		opts.BaseURL = match.BaseURL
		return
	}
	if bundle.BaseURL != "" {
		opts.BaseURL = bundle.BaseURL
	}
}

// prepareBundle builds the agent bundle and handles bundle output. Returns nil bundle for dry-run.
// resolveSkillCatalogPaths returns the catalog-scope directories — local
// paths and cached git repos — declared in cfg.Skills. Returns nil when
// cfg is absent or has no skills sources, which is the common case for
// agents that don't opt into the catalog mechanism.
func resolveSkillCatalogPaths(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	if len(cfg.Skills.Repositories) == 0 && len(cfg.Skills.LocalPaths) == 0 {
		return nil
	}
	mgr, err := source.NewSkillsManager(cfg)
	if err != nil {
		logging.Warn("failed to build skills manager: %v", err)
		return nil
	}
	return mgr.CatalogPaths()
}

func prepareBundle(cmd *cobra.Command, opts *RunOptions, prompt, workingDir string) (*agent.Bundle, error) {
	agentDir, err := FindAgentDir(opts.Agent, opts.AgentsDir, opts.Config)
	if err != nil {
		return nil, err
	}

	agentsDir := filepath.Dir(agentDir)
	opts.AgentsDir = agentsDir

	bundle, err := agent.BuildBundleWithOptions(agentsDir, opts.Agent, prompt, workingDir, opts.Mode, opts.Vars, &agent.BundleOptions{
		SkillOverrides: opts.SkillOverrides,
		CatalogPaths:   resolveSkillCatalogPaths(opts.Config),
	})
	if err != nil {
		return nil, err
	}

	warn, resolveErr := ResolveModelPrecedence(cmd.Context(), opts, bundle)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if warn != "" {
		if _, fmtErr := fmt.Fprintln(cmd.ErrOrStderr(), warn); fmtErr != nil {
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

	if err := bundle.Requires.Preflight(); err != nil {
		return nil, err
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

// openSession creates or resumes a session log under workingDir/.squad/sessions.
// Returns nil (with no error) when session logging is disabled.
func openSession(opts *RunOptions, bundle *agent.Bundle, prompt string) (*session.Logger, error) {
	if opts.NoSession {
		return nil, nil
	}
	if opts.ResumeID != "" {
		if opts.ResumeID == "latest" {
			sessions, err := session.List(opts.CanonicalRepoPath)
			if err != nil || len(sessions) == 0 {
				return nil, fmt.Errorf("no sessions found to resume for %s", opts.CanonicalRepoPath)
			}
			opts.ResumeID = sessions[0].SessionID
		}
		l, err := session.Open(opts.CanonicalRepoPath, opts.ResumeID)
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
	l, err := session.New(opts.CanonicalRepoPath, opts.WorktreePath, opts.Agent, opts.Provider, opts.Model, prompt)
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

// resolveRunWorkingDir picks the working directory for a run and returns a
// cleanup func. Remote-only agents (working_dir: none in the manifest) get a
// fresh temp dir that is removed on exit; everyone else gets the resolved
// opts.WorkingDir (or cwd) and a no-op cleanup.
func resolveRunWorkingDir(opts *RunOptions) (string, func(), error) {
	if agentIsRemoteOnly(opts) {
		dir, err := os.MkdirTemp("", "squad-remote-")
		if err != nil {
			return "", func() {}, fmt.Errorf("failed to create temp working dir: %w", err)
		}
		cleanup := func() {
			if rmErr := os.RemoveAll(dir); rmErr != nil {
				logging.Warn("failed to clean temp working dir %s: %v", dir, rmErr)
			}
		}
		return dir, cleanup, nil
	}
	dir, err := resolveWorkingDir(opts.WorkingDir)
	if err != nil {
		return "", func() {}, err
	}
	return dir, func() {}, nil
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

// agentIsRemoteOnly reads only the working_dir field from the agent manifest
// to decide if a temp dir should be allocated before prepareBundle. Returns
// false on any load failure; the real error surfaces from prepareBundle.
func agentIsRemoteOnly(opts *RunOptions) bool {
	if opts.Agent == "" {
		return false
	}
	agentDir, err := FindAgentDir(opts.Agent, opts.AgentsDir, opts.Config)
	if err != nil {
		return false
	}
	m, err := agent.LoadManifest(agentDir)
	if err != nil {
		return false
	}
	return m.IsRemoteOnly()
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

// recordWorktreePath notes the ephemeral worktree on opts when isolation moved
// the working directory away from workingDir. Sessions persist this so resume
// can map a worktree back to its canonical repo. iso is always non-nil here.
func recordWorktreePath(opts *RunOptions, iso *Isolation, workingDir string) {
	if iso.Effective != workingDir {
		opts.WorktreePath = iso.Effective
	}
}

// ResolveGitToplevel returns the canonical repository root for dir by asking
// git for its toplevel. It falls back to dir unchanged when dir is not inside a
// git repository, so callers always get a usable canonical path. This must be
// called before worktree isolation rewrites the working directory so sessions
// are keyed to the real repo, not an ephemeral worktree.
func ResolveGitToplevel(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return dir // fallback to the original dir if not in a git repo
	}
	return strings.TrimSpace(string(out))
}
