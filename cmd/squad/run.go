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
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/ollama"
	"github.com/cowdogmoo/squad/openairesponses"
	"github.com/cowdogmoo/squad/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/openai"
)

var (
	runAgent             string
	runAgentsDir         string
	runWorkingDir        string
	runAPIKey            string
	runBaseURL           string
	runOrg               string
	runAPIVersion        string
	runAPIType           string
	runOpenAICompatMax   bool
	runProvider          string
	runModel             string
	runTemperature       float64
	runMaxTokens         int
	runSystem            string
	runOutput            string
	runPrint             bool
	runBundleOut         string
	runPrintBundle       bool
	runDryRun            bool
	runRequireActionable bool
	runApply             bool
	runApplyFallback     bool
	runNumCtx            int
	runMode              string
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
	Mode              string
}

// newRunOptions creates a RunOptions by copying from the current CLI flag globals.
func newRunOptions() *RunOptions {
	v := appViper
	apiKey := v.GetString("provider.token")
	if runAPIKey != "" {
		apiKey = runAPIKey
	}
	provider := v.GetString("provider.default")
	if runProvider != "" {
		provider = runProvider
	}
	model := v.GetString("model.default")
	if runModel != "" {
		model = runModel
	}
	temp := v.GetFloat64("model.temperature")
	if runTemperature >= 0 {
		temp = runTemperature
	}
	maxT := v.GetInt("model.max_tokens")
	if runMaxTokens >= 0 {
		maxT = runMaxTokens
	}
	return &RunOptions{
		Agent:             runAgent,
		AgentsDir:         runAgentsDir,
		WorkingDir:        runWorkingDir,
		APIKey:            apiKey,
		BaseURL:           v.GetString("provider.base_url"),
		Org:               v.GetString("provider.organization"),
		APIVersion:        v.GetString("provider.api_version"),
		APIType:           v.GetString("provider.api_type"),
		OpenAICompatMax:   v.GetBool("provider.openai_compat_max_tokens"),
		Provider:          provider,
		Model:             model,
		Temperature:       temp,
		MaxTokens:         maxT,
		System:            runSystem,
		Output:            runOutput,
		Print:             runPrint,
		BundleOut:         runBundleOut,
		PrintBundle:       runPrintBundle,
		DryRun:            runDryRun,
		RequireActionable: runRequireActionable,
		Apply:             runApply,
		ApplyFallback:     runApplyFallback,
		NumCtx:            v.GetInt("provider.num_ctx"),
		Mode:              runMode,
	}
}

// bindRunFlags binds the run command's flags to Viper keys so that env vars
// and config file values participate in precedence resolution.
func bindRunFlags(v *viper.Viper) error {
	flags := runCmd.Flags()
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
	return nil
}

var runCmd = &cobra.Command{
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
		opts := newRunOptions()
		return executeRun(cmd, args, opts)
	},
}

func init() {
	runCmd.Flags().StringVar(&runAgent, "agent", "", "Agent name (e.g. go-cobra)")
	if err := runCmd.MarkFlagRequired("agent"); err != nil {
		panic(err)
	}
	runCmd.Flags().StringVar(&runAgentsDir, "agents-dir", "", "Agents directory (default: ./agents, then ~/.config/squad/agents)")
	runCmd.Flags().StringVar(&runWorkingDir, "working-dir", "", "Working directory (default: current working directory)")
	runCmd.Flags().StringVar(&runAPIKey, "api-key", "", "API key (overrides env/config)")
	runCmd.Flags().StringVar(&runBaseURL, "base-url", "", "Base URL override for provider")
	runCmd.Flags().StringVar(&runOrg, "organization", "", "Organization ID (OpenAI-compatible)")
	runCmd.Flags().StringVar(&runAPIVersion, "api-version", "", "API version (Azure/OpenAI-compatible)")
	runCmd.Flags().StringVar(&runAPIType, "api-type", "", "API type (openai or azure)")
	runCmd.Flags().BoolVar(&runOpenAICompatMax, "openai-compat-max-tokens", false, "Use max_tokens for OpenAI-compatible endpoints")
	runCmd.Flags().StringVar(&runProvider, "provider", "", "Model provider (openai, anthropic, gemini, ollama, etc)")
	runCmd.Flags().StringVar(&runModel, "model", "", "Model name")
	runCmd.Flags().Float64Var(&runTemperature, "temperature", -1, "Sampling temperature (default from config)")
	runCmd.Flags().IntVar(&runMaxTokens, "max-tokens", -1, "Max output tokens (default from config)")
	runCmd.Flags().StringVar(&runSystem, "system", "", "System prompt override")
	runCmd.Flags().StringVar(&runOutput, "out", "", "Write response to a file")
	runCmd.Flags().BoolVar(&runPrint, "print", true, "Print response to stdout")
	runCmd.Flags().StringVar(&runBundleOut, "bundle-out", "", "Write agent bundle to a file")
	runCmd.Flags().BoolVar(&runPrintBundle, "print-bundle", false, "Print agent bundle to stdout")
	runCmd.Flags().BoolVar(&runDryRun, "dry-run", false, "Build bundle and exit without calling the model")
	runCmd.Flags().BoolVar(&runRequireActionable, "require-actionable", true, "Require actionable output (diff/files/no changes)")
	runCmd.Flags().BoolVar(&runApply, "apply", false, "Apply unified diff from the response to the working directory")
	runCmd.Flags().BoolVar(&runApplyFallback, "apply-fallback", false, "Fallback to patch(1) if git apply fails (may create .rej/.orig)")
	runCmd.Flags().IntVar(&runNumCtx, "num-ctx", 32768, "Context window size for Ollama models")
	runCmd.Flags().StringVar(&runMode, "mode", "", "Agent mode override (e.g. readonly)")

	runCmd.MarkFlagsMutuallyExclusive("dry-run", "apply")

	// Dynamic completions for --agent (scan agents directories).
	_ = runCmd.RegisterFlagCompletionFunc("agent", completeAgentNames)
	// Static completions for --provider.
	_ = runCmd.RegisterFlagCompletionFunc("provider", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"openai", "openai-responses", "anthropic", "ollama", "gemini"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = runCmd.RegisterFlagCompletionFunc("model", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	})
}

// executeRun contains the full run command logic, parameterized by RunOptions.
func executeRun(cmd *cobra.Command, args []string, opts *RunOptions) error {
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

	tools.ResetEditsApplied()
	response, err := invokeModel(cmd, opts, bundle)
	if err != nil {
		return err
	}

	return handleResponse(cmd, opts, response, workingDir)
}

// prepareBundle builds the agent bundle and handles bundle output. Returns nil bundle for dry-run.
func prepareBundle(cmd *cobra.Command, opts *RunOptions, prompt, workingDir string) (*agent.Bundle, error) {
	agentsDir, err := resolveAgentsDir(opts.AgentsDir)
	if err != nil {
		return nil, err
	}

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

	if configFromContext(cmd) == nil {
		return nil, fmt.Errorf("config not available in context")
	}

	return bundle, nil
}

// invokeModel resolves provider settings and calls the appropriate model backend.
func invokeModel(cmd *cobra.Command, opts *RunOptions, bundle *agent.Bundle) (string, error) {
	v := appViper
	provider := normalizeProvider(opts.Provider)
	model := opts.Model
	temperature := opts.Temperature
	maxTokens := opts.MaxTokens

	systemPrompt := bundle.System
	if opts.System != "" {
		systemPrompt += "\n\n## System Override\n\n" + strings.TrimSpace(opts.System) + "\n"
	}

	return callModel(cmd.Context(), v, provider, model, systemPrompt, bundle, temperature, maxTokens)
}

// handleResponse validates, applies, and writes the model response.
func handleResponse(cmd *cobra.Command, opts *RunOptions, response, workingDir string) error {
	if opts.RequireActionable {
		if err := validateActionableResponse(response); err != nil {
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

// callModel dispatches the prompt to the appropriate model backend and returns the response.
func callModel(ctx context.Context, v *viper.Viper, provider, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int) (string, error) {
	if openairesponses.UseResponsesAPI(provider, model) {
		return callResponsesAPI(ctx, v, model, systemPrompt, bundle, temperature, maxTokens)
	}
	return callLangChainLLM(ctx, v, provider, model, systemPrompt, bundle, temperature, maxTokens)
}

// callResponsesAPI runs the prompt via the OpenAI Responses API.
func callResponsesAPI(ctx context.Context, v *viper.Viper, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int) (string, error) {
	apiKey := v.GetString("provider.token")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return "", fmt.Errorf("API key required for OpenAI Responses API: use --api-key, config provider.token, or OPENAI_API_KEY env var")
	}

	logging.InfoContext(ctx, "model call started via Responses API (model=%s)", model)
	modelStart := time.Now()
	response, err := openairesponses.RunWithTools(ctx, apiKey, v.GetString("provider.base_url"), model, systemPrompt, bundle.User, bundle.WorkDir, v.GetString("provider.organization"), temperature, maxTokens)
	if err != nil {
		return "", fmt.Errorf("model call failed: %w", err)
	}
	logging.InfoContext(ctx, "model call finished in %s (response-bytes=%d)", time.Since(modelStart).Round(time.Millisecond), len(response))
	return response, nil
}

// callLangChainLLM runs the prompt via a LangChain-compatible LLM.
func callLangChainLLM(ctx context.Context, v *viper.Viper, provider, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int) (string, error) {
	llm, err := buildLLM(provider, model)
	if err != nil {
		return "", err
	}

	callOpts := buildCallOpts(v, provider, temperature, maxTokens)

	logging.InfoContext(ctx, "model call started (provider=%s model=%s)", provider, model)
	modelStart := time.Now()
	response, err := tools.RunWithTools(ctx, llm, systemPrompt, bundle.User, bundle.WorkDir, callOpts...)
	if err != nil {
		return "", fmt.Errorf("model call failed: %w", err)
	}
	logging.InfoContext(ctx, "model call finished in %s (response-bytes=%d)", time.Since(modelStart).Round(time.Millisecond), len(response))
	return response, nil
}

// buildCallOpts constructs LLM call options from provider settings.
func buildCallOpts(v *viper.Viper, provider string, temperature float64, maxTokens int) []llms.CallOption {
	callOpts := []llms.CallOption{
		llms.WithTemperature(temperature),
	}
	if maxTokens <= 0 {
		return callOpts
	}
	if !isOpenAICompatProvider(provider) {
		return append(callOpts, llms.WithMaxTokens(maxTokens))
	}
	useLegacy := provider != "openai" || v.GetBool("provider.openai_compat_max_tokens")
	if useLegacy {
		return append(callOpts, llms.WithMaxTokens(maxTokens), openai.WithLegacyMaxTokensField())
	}
	return append(callOpts, openai.WithMaxCompletionTokens(maxTokens))
}

// applyResponseDiff extracts and applies a unified diff from the model response.
func applyResponseDiff(ctx context.Context, response, workingDir string, fallback bool) error {
	diff, err := extractUnifiedDiff(response)
	if err != nil {
		if responseIndicatesNoChanges(response) {
			logging.InfoContext(ctx, "no changes reported; skipping apply")
			return nil
		}
		if tools.EditsApplied() {
			logging.InfoContext(ctx, "edits already applied via tools; skipping diff apply")
			return nil
		}
		return err
	}
	if tools.EditsApplied() {
		logging.InfoContext(ctx, "edits already applied via tools; skipping diff apply")
		return nil
	}
	logging.InfoContext(ctx, "applying diff (%d bytes)", len(diff))
	if err := applyUnifiedDiff(ctx, workingDir, diff, fallback); err != nil {
		return err
	}
	logging.InfoContext(ctx, "diff applied to %s", workingDir)
	return nil
}

// writeResponse outputs the model response to stdout and/or a file.
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

// buildLLM constructs an LLM model instance based on the provider and configuration.
func buildLLM(provider, model string) (llms.Model, error) {
	provider = normalizeProvider(provider)
	switch provider {
	case "ollama":
		return buildNativeOllamaLLM(model), nil
	case "openai", "":
		return buildOpenAICompatLLM(provider, model)
	case "anthropic":
		return buildAnthropicLLM(model)
	default:
		return nil, fmt.Errorf("provider not implemented: %s", provider)
	}
}

func buildOpenAICompatLLM(provider, model string) (llms.Model, error) {
	v := appViper
	oaiOpts := []openai.Option{}
	if model != "" {
		oaiOpts = append(oaiOpts, openai.WithModel(model))
	}

	if baseURL := v.GetString("provider.base_url"); baseURL != "" {
		oaiOpts = append(oaiOpts, openai.WithBaseURL(baseURL))
	}

	if token := v.GetString("provider.token"); token != "" {
		oaiOpts = append(oaiOpts, openai.WithToken(token))
	}

	if provider == "openai" || provider == "" {
		if org := v.GetString("provider.organization"); org != "" {
			oaiOpts = append(oaiOpts, openai.WithOrganization(org))
		}

		if apiVersion := v.GetString("provider.api_version"); apiVersion != "" {
			oaiOpts = append(oaiOpts, openai.WithAPIVersion(apiVersion))
		}

		if apiType := strings.ToLower(v.GetString("provider.api_type")); apiType == "azure" {
			oaiOpts = append(oaiOpts, openai.WithAPIType(openai.APITypeAzure))
		}
	}

	return openai.New(oaiOpts...)
}

func buildAnthropicLLM(model string) (llms.Model, error) {
	v := appViper
	aOpts := []anthropic.Option{}
	if model != "" {
		aOpts = append(aOpts, anthropic.WithModel(model))
	}

	if token := v.GetString("provider.token"); token != "" {
		aOpts = append(aOpts, anthropic.WithToken(token))
	}

	if baseURL := v.GetString("provider.base_url"); baseURL != "" {
		aOpts = append(aOpts, anthropic.WithBaseURL(baseURL))
	}

	return anthropic.New(aOpts...)
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func buildNativeOllamaLLM(model string) llms.Model {
	v := appViper
	baseURL := v.GetString("provider.base_url")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	numCtx := appViper.GetInt("provider.num_ctx")
	if numCtx <= 0 {
		numCtx = 32768
	}
	return ollama.New(baseURL, model, numCtx)
}

func isOpenAICompatProvider(provider string) bool {
	return provider == "" || provider == "openai"
}

func validateActionableResponse(response string) error {
	if tools.EditsApplied() {
		return nil
	}
	if _, err := extractUnifiedDiff(response); err == nil {
		return nil
	}
	lower := strings.ToLower(response)
	if strings.Contains(lower, "files touched") || strings.Contains(lower, "no changes") {
		return nil
	}
	return fmt.Errorf("response is not actionable: missing diff, files touched, or no changes section (disable with --require-actionable=false)")
}

func responseIndicatesNoChanges(response string) bool {
	return strings.Contains(strings.ToLower(response), "no changes")
}

func extractUnifiedDiff(response string) (string, error) {
	const fence = "```diff"
	const altFence = "```patch"
	const endFence = "```"
	var blocks []string
	for {
		start := strings.Index(response, fence)
		useFence := fence
		if start == -1 {
			start = strings.Index(response, altFence)
			useFence = altFence
		}
		if start == -1 {
			break
		}
		response = response[start+len(useFence):]
		end := strings.Index(response, endFence)
		if end == -1 {
			block := strings.TrimSpace(response)
			if block != "" {
				blocks = append(blocks, block)
			}
			break
		}
		block := strings.TrimSpace(response[:end])
		if block != "" {
			blocks = append(blocks, block)
		}
		response = response[end+len(endFence):]
	}
	if len(blocks) == 0 {
		return "", fmt.Errorf("apply requires a unified diff block (```diff ... ```)")
	}
	diff := strings.Join(blocks, "\n")
	if !looksLikeDiff(diff) {
		return "", fmt.Errorf("diff block does not look like a unified diff (missing diff headers)")
	}
	return diff, nil
}

func applyUnifiedDiff(ctx context.Context, workingDir, diff string, applyFallback bool) error {
	if strings.TrimSpace(diff) == "" {
		return fmt.Errorf("diff content is empty")
	}

	gitErr := applyWithGit(ctx, workingDir, diff)
	if gitErr == nil {
		return nil
	}

	if !applyFallback {
		return fmt.Errorf("failed to apply diff with git: %v (hint: retry with --apply-fallback)", gitErr)
	}

	patchErr := applyWithPatch(ctx, workingDir, diff)
	if patchErr == nil {
		return nil
	}

	return fmt.Errorf("failed to apply diff with git or patch: %v; %v", gitErr, patchErr)
}

func looksLikeDiff(diff string) bool {
	if strings.Contains(diff, "diff --git ") {
		return true
	}
	if strings.Contains(diff, "--- a/") && strings.Contains(diff, "+++ b/") {
		return true
	}
	return false
}

func applyWithGit(ctx context.Context, workingDir, diff string) error {
	cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "--recount", "-")
	cmd.Dir = workingDir
	cmd.Stdin = strings.NewReader(diff)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git apply failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
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

func applyWithPatch(ctx context.Context, workingDir, diff string) error {
	cmd := exec.CommandContext(ctx, "patch", "-p1")
	cmd.Dir = workingDir
	cmd.Stdin = strings.NewReader(diff)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("patch failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
