package runlogic

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
	"github.com/cowdogmoo/squad/config"
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

// Context key types for storing config and viper instance.
type configKeyType struct{}
type viperKeyType struct{}

var (
	configKey = configKeyType{}
	viperKey  = viperKeyType{}
)

// WithConfig stores the resolved config in the context.
func WithConfig(ctx context.Context, cfg *config.Config) context.Context {
	return context.WithValue(ctx, configKey, cfg)
}

// WithViper stores the resolved Viper instance in the context.
func WithViper(ctx context.Context, v *viper.Viper) context.Context {
	return context.WithValue(ctx, viperKey, v)
}

// ConfigFromContext retrieves config from command context.
func ConfigFromContext(cmd *cobra.Command) *config.Config {
	return configFromContext(cmd)
}

// ViperFromContext retrieves Viper from command context.
func ViperFromContext(cmd *cobra.Command) *viper.Viper {
	return viperFromContext(cmd)
}

func configFromContext(cmd *cobra.Command) *config.Config {
	if cfg, ok := cmd.Context().Value(configKey).(*config.Config); ok {
		return cfg
	}
	return nil
}

func viperFromContext(cmd *cobra.Command) *viper.Viper {
	if v, ok := cmd.Context().Value(viperKey).(*viper.Viper); ok {
		return v
	}
	return viper.New()
}

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

	bundle, err := PrepareBundle(cmd, opts, prompt, workingDir)
	if err != nil {
		return err
	}
	if bundle == nil {
		return nil // dry-run
	}

	ctx := tools.InitEdits(cmd.Context())
	cmd.SetContext(ctx)
	tools.ResetEditsApplied(ctx)
	response, err := InvokeModel(cmd, opts, bundle)
	if err != nil {
		return err
	}

	return HandleResponse(cmd, opts, response, workingDir)
}

// PrepareBundle builds the agent bundle and handles bundle output. Returns nil bundle for dry-run.
func PrepareBundle(cmd *cobra.Command, opts *RunOptions, prompt, workingDir string) (*agent.Bundle, error) {
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

// InvokeModel resolves provider settings and calls the appropriate model backend.
func InvokeModel(cmd *cobra.Command, opts *RunOptions, bundle *agent.Bundle) (string, error) {
	v := viperFromContext(cmd)
	provider := NormalizeProvider(opts.Provider)
	model := opts.Model
	temperature := opts.Temperature
	maxTokens := opts.MaxTokens

	systemPrompt := bundle.System
	if opts.System != "" {
		systemPrompt += "\n\n## System Override\n\n" + strings.TrimSpace(opts.System) + "\n"
	}

	return CallModel(cmd.Context(), v, provider, model, systemPrompt, bundle, temperature, maxTokens)
}

// HandleResponse validates, applies, and writes the model response.
func HandleResponse(cmd *cobra.Command, opts *RunOptions, response, workingDir string) error {
	if opts.RequireActionable {
		if err := ValidateActionableResponse(cmd.Context(), response); err != nil {
			return err
		}
	}

	if opts.Apply {
		if err := ApplyResponseDiff(cmd.Context(), response, workingDir, opts.ApplyFallback); err != nil {
			return err
		}
	}

	return writeResponse(cmd, response, opts)
}

// CallModel dispatches the prompt to the appropriate model backend and returns the response.
func CallModel(ctx context.Context, v *viper.Viper, provider, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int) (string, error) {
	if openairesponses.UseResponsesAPI(provider, model) {
		return CallResponsesAPI(ctx, v, model, systemPrompt, bundle, temperature, maxTokens)
	}
	return CallLangChainLLM(ctx, v, provider, model, systemPrompt, bundle, temperature, maxTokens)
}

// CallResponsesAPI runs the prompt via the OpenAI Responses API.
func CallResponsesAPI(ctx context.Context, v *viper.Viper, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int) (string, error) {
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

// CallLangChainLLM runs the prompt via a LangChain-compatible LLM.
func CallLangChainLLM(ctx context.Context, v *viper.Viper, provider, model, systemPrompt string, bundle *agent.Bundle, temperature float64, maxTokens int) (string, error) {
	llm, err := BuildLLM(v, provider, model)
	if err != nil {
		return "", err
	}

	callOpts := BuildCallOpts(v, provider, temperature, maxTokens)

	logging.InfoContext(ctx, "model call started (provider=%s model=%s)", provider, model)
	modelStart := time.Now()
	response, err := tools.RunWithTools(ctx, llm, systemPrompt, bundle.User, bundle.WorkDir, callOpts...)
	if err != nil {
		return "", fmt.Errorf("model call failed: %w", err)
	}
	logging.InfoContext(ctx, "model call finished in %s (response-bytes=%d)", time.Since(modelStart).Round(time.Millisecond), len(response))
	return response, nil
}

// BuildCallOpts constructs LLM call options from provider settings.
func BuildCallOpts(v *viper.Viper, provider string, temperature float64, maxTokens int) []llms.CallOption {
	callOpts := []llms.CallOption{}
	if temperature >= 0 {
		callOpts = append(callOpts, llms.WithTemperature(temperature))
	}
	if maxTokens <= 0 {
		return callOpts
	}
	if !IsOpenAICompatProvider(provider) {
		return append(callOpts, llms.WithMaxTokens(maxTokens))
	}
	useLegacy := provider != "openai" || v.GetBool("provider.openai_compat_max_tokens")
	if useLegacy {
		return append(callOpts, llms.WithMaxTokens(maxTokens), openai.WithLegacyMaxTokensField())
	}
	return append(callOpts, openai.WithMaxCompletionTokens(maxTokens))
}

// BuildLLM constructs an LLM model instance based on the provider and configuration.
func BuildLLM(v *viper.Viper, provider, model string) (llms.Model, error) {
	provider = NormalizeProvider(provider)
	switch provider {
	case "ollama":
		return BuildNativeOllamaLLM(v, model), nil
	case "openai", "":
		return BuildOpenAICompatLLM(v, provider, model)
	case "anthropic":
		return BuildAnthropicLLM(v, model)
	default:
		return nil, fmt.Errorf("provider not implemented: %s", provider)
	}
}

func BuildOpenAICompatLLM(v *viper.Viper, provider, model string) (llms.Model, error) {
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

func BuildAnthropicLLM(v *viper.Viper, model string) (llms.Model, error) {
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

func NormalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func BuildNativeOllamaLLM(v *viper.Viper, model string) llms.Model {
	baseURL := v.GetString("provider.base_url")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	numCtx := v.GetInt("provider.num_ctx")
	if numCtx <= 0 {
		numCtx = 32768
	}
	return ollama.New(baseURL, model, numCtx)
}

func IsOpenAICompatProvider(provider string) bool {
	return provider == "" || provider == "openai"
}

// ApplyResponseDiff extracts and applies a unified diff from the model response.
func ApplyResponseDiff(ctx context.Context, response, workingDir string, fallback bool) error {
	diff, err := ExtractUnifiedDiff(response)
	if err != nil {
		if ResponseIndicatesNoChanges(response) {
			logging.InfoContext(ctx, "no changes reported; skipping apply")
			return nil
		}
		if tools.EditsApplied(ctx) {
			logging.InfoContext(ctx, "edits already applied via tools; skipping diff apply")
			return nil
		}
		return err
	}
	if tools.EditsApplied(ctx) {
		logging.InfoContext(ctx, "edits already applied via tools; skipping diff apply")
		return nil
	}
	logging.InfoContext(ctx, "applying diff (%d bytes)", len(diff))
	if err := ApplyUnifiedDiff(ctx, workingDir, diff, fallback); err != nil {
		return err
	}
	logging.InfoContext(ctx, "diff applied to %s", workingDir)
	return nil
}

func ValidateActionableResponse(ctx context.Context, response string) error {
	if tools.EditsApplied(ctx) {
		return nil
	}
	if _, err := ExtractUnifiedDiff(response); err == nil {
		return nil
	}
	lower := strings.ToLower(response)
	if strings.Contains(lower, "files touched") || strings.Contains(lower, "no changes") {
		return nil
	}
	return fmt.Errorf("response is not actionable: missing diff, files touched, or no changes section (disable with --require-actionable=false)")
}

func ResponseIndicatesNoChanges(response string) bool {
	return strings.Contains(strings.ToLower(response), "no changes")
}

func ExtractUnifiedDiff(response string) (string, error) {
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
	if !LooksLikeDiff(diff) {
		return "", fmt.Errorf("diff block does not look like a unified diff (missing diff headers)")
	}
	return diff, nil
}

func ApplyUnifiedDiff(ctx context.Context, workingDir, diff string, applyFallback bool) error {
	if strings.TrimSpace(diff) == "" {
		return fmt.Errorf("diff content is empty")
	}

	gitErr := ApplyWithGit(ctx, workingDir, diff)
	if gitErr == nil {
		return nil
	}

	if !applyFallback {
		return fmt.Errorf("failed to apply diff with git: %v (hint: retry with --apply-fallback)", gitErr)
	}

	patchErr := ApplyWithPatch(ctx, workingDir, diff)
	if patchErr == nil {
		return nil
	}

	return fmt.Errorf("failed to apply diff with git or patch: %v; %v", gitErr, patchErr)
}

func LooksLikeDiff(diff string) bool {
	if strings.Contains(diff, "diff --git ") {
		return true
	}
	if strings.Contains(diff, "--- a/") && strings.Contains(diff, "+++ b/") {
		return true
	}
	return false
}

func ApplyWithGit(ctx context.Context, workingDir, diff string) error {
	cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "--recount", "-")
	cmd.Dir = workingDir
	cmd.Stdin = strings.NewReader(diff)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git apply failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ApplyWithPatch(ctx context.Context, workingDir, diff string) error {
	cmd := exec.CommandContext(ctx, "patch", "-p1")
	cmd.Dir = workingDir
	cmd.Stdin = strings.NewReader(diff)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("patch failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
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
