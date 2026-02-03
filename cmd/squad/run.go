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

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/spf13/cobra"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/openai"
	"gopkg.in/yaml.v3"
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

var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "Run an agent workflow",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runAgent == "" {
			return fmt.Errorf("--agent is required")
		}

		prompt, err := readPrompt(cmd, args)
		if err != nil {
			return err
		}

		workingDir, err := resolveWorkingDir(runWorkingDir)
		if err != nil {
			return err
		}

		agentsDir, err := resolveAgentsDir(runAgentsDir)
		if err != nil {
			return err
		}

		bundle, err := buildAgentBundle(agentsDir, runAgent, prompt, workingDir)
		if err != nil {
			return err
		}

		logging.InfoContext(cmd.Context(), "agent bundle ready (agent=%s provider=%s model=%s)", runAgent, runProvider, runModel)

		if runPrintBundle {
			if _, err := io.Copy(cmd.OutOrStdout(), bytes.NewReader(bundle.Combined)); err != nil {
				return err
			}
		}

		if runBundleOut != "" {
			if err := os.WriteFile(runBundleOut, bundle.Combined, 0o644); err != nil {
				return fmt.Errorf("failed to write bundle: %w", err)
			}
			logging.InfoContext(cmd.Context(), "bundle written to %s", runBundleOut)
		}

		if runDryRun {
			return nil
		}

		cfg := configFromContext(cmd)
		if cfg == nil {
			return fmt.Errorf("config not available in context")
		}

		provider := normalizeProvider(pickString(runProvider, cfg.Provider.Default))
		model := pickString(runModel, cfg.Model.Default)
		temperature := pickFloat(runTemperature, cfg.Model.Temperature)
		maxTokens := pickInt(runMaxTokens, cfg.Model.MaxTokens)

		llm, err := buildLLM(provider, model, cfg)
		if err != nil {
			return err
		}

		callOpts := []llms.CallOption{
			llms.WithTemperature(temperature),
		}

		if maxTokens > 0 {
			if isOpenAICompatProvider(provider) {
				useLegacy := provider != "openai" || runOpenAICompatMax || cfg.Provider.OpenAICompatMaxTokens
				if useLegacy {
					callOpts = append(callOpts, llms.WithMaxTokens(maxTokens), openai.WithLegacyMaxTokensField())
				} else {
					callOpts = append(callOpts, openai.WithMaxCompletionTokens(maxTokens))
				}
			} else {
				callOpts = append(callOpts, llms.WithMaxTokens(maxTokens))
			}
		}

		systemPrompt := bundle.System
		if runSystem != "" {
			systemPrompt += "\n\n## System Override\n\n" + strings.TrimSpace(runSystem) + "\n"
		}

		logging.InfoContext(cmd.Context(), "model call started (provider=%s model=%s)", provider, model)
		modelStart := time.Now()
		response, err := runWithTools(cmd.Context(), llm, systemPrompt, bundle.User, bundle.WorkDir, callOpts...)
		if err != nil {
			return fmt.Errorf("model call failed: %w", err)
		}
		logging.InfoContext(cmd.Context(), "model call finished in %s (response-bytes=%d)", time.Since(modelStart).Round(time.Millisecond), len(response))

		if runRequireActionable {
			if err := validateActionableResponse(response); err != nil {
				return err
			}
		}

		if runApply {
			diff, err := extractUnifiedDiff(response)
			if err != nil {
				return err
			}
			logging.InfoContext(cmd.Context(), "applying diff (%d bytes)", len(diff))
			if err := applyUnifiedDiff(cmd.Context(), workingDir, diff); err != nil {
				return err
			}
			logging.InfoContext(cmd.Context(), "diff applied to %s", workingDir)
		}

		if runPrint || runOutput == "" {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), response); err != nil {
				return err
			}
		}

		if runOutput != "" {
			if err := os.WriteFile(runOutput, []byte(response), 0o644); err != nil {
				return fmt.Errorf("failed to write response: %w", err)
			}
			logging.InfoContext(cmd.Context(), "response written to %s", runOutput)
		}
		return nil
	},
}

func init() {
	runCmd.Flags().StringVar(&runAgent, "agent", "", "Agent name (e.g. go-cobra)")
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
}

type agentModeOverride struct {
	EntryPoint string   `yaml:"entrypoint,omitempty"`
	Wrapper    string   `yaml:"wrapper,omitempty"`
	References []string `yaml:"references,omitempty"`
}

type agentManifest struct {
	Name       string                       `yaml:"name"`
	Version    string                       `yaml:"version"`
	EntryPoint string                       `yaml:"entrypoint"`
	Wrapper    string                       `yaml:"wrapper"`
	References []string                     `yaml:"references"`
	Modes      map[string]agentModeOverride `yaml:"modes,omitempty"`
}

type agentBundle struct {
	System   string // wrapper + system prompt + references
	User     string // user request only
	Combined []byte // concatenated for --print-bundle/--bundle-out
	WorkDir  string
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

func buildAgentBundle(agentsDir, agentName, prompt, workingDir string) (*agentBundle, error) {
	agentPath := filepath.Join(agentsDir, agentName)
	manifestPath := filepath.Join(agentPath, "agent.yaml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent manifest: %w", err)
	}

	var manifest agentManifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse agent manifest: %w", err)
	}

	if runMode != "" {
		override, ok := manifest.Modes[runMode]
		if !ok {
			return nil, fmt.Errorf("agent %q has no mode %q", agentName, runMode)
		}
		if override.EntryPoint != "" {
			manifest.EntryPoint = override.EntryPoint
		}
		if override.Wrapper != "" {
			manifest.Wrapper = override.Wrapper
		}
		if len(override.References) > 0 {
			manifest.References = override.References
		}
	}

	entryPath := filepath.Join(agentPath, manifest.EntryPoint)
	systemData, err := os.ReadFile(entryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read system prompt: %w", err)
	}

	wrapperPath := filepath.Join(agentPath, manifest.Wrapper)
	wrapperData, err := os.ReadFile(wrapperPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent wrapper: %w", err)
	}

	var refs []string
	for _, ref := range manifest.References {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		refPath := filepath.Join(agentPath, ref)
		refData, err := os.ReadFile(refPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read reference %s: %w", ref, err)
		}
		refs = append(refs, fmt.Sprintf("## Reference: %s\n\n%s\n", ref, strings.TrimSpace(string(refData))))
	}

	// Build the system message content (wrapper + system prompt + references).
	var sys bytes.Buffer
	sys.WriteString("# Squad Agent Bundle\n\n")
	sys.WriteString(fmt.Sprintf("Agent: %s (%s)\n", manifest.Name, manifest.Version))
	sys.WriteString(fmt.Sprintf("Working Directory: %s\n\n", workingDir))
	sys.WriteString("## Agent Wrapper\n\n")
	sys.Write(wrapperData)
	sys.WriteString("\n\n## System Prompt\n\n")
	sys.Write(systemData)

	if len(refs) > 0 {
		sys.WriteString("\n\n## References\n\n")
		for _, ref := range refs {
			sys.WriteString(ref)
			sys.WriteString("\n")
		}
	}

	// Build the combined output for --print-bundle/--bundle-out.
	var combined bytes.Buffer
	combined.Write(sys.Bytes())
	combined.WriteString("\n\n## User Request\n\n")
	combined.WriteString(prompt)
	combined.WriteString("\n")

	return &agentBundle{
		System:   sys.String(),
		User:     prompt,
		Combined: combined.Bytes(),
		WorkDir:  workingDir,
	}, nil
}

func buildLLM(provider, model string, cfg *config.Config) (llms.Model, error) {
	provider = normalizeProvider(provider)
	switch provider {
	case "ollama":
		return buildNativeOllamaLLM(model, cfg), nil
	case "openai", "":
		return buildOpenAICompatLLM(provider, model, cfg)
	case "anthropic":
		return buildAnthropicLLM(model, cfg)
	default:
		return nil, fmt.Errorf("provider not implemented: %s", provider)
	}
}

func buildOpenAICompatLLM(provider, model string, cfg *config.Config) (llms.Model, error) {
	opts := []openai.Option{}
	if model != "" {
		opts = append(opts, openai.WithModel(model))
	}

	baseURL := pickString(runBaseURL, cfg.Provider.BaseURL)
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}

	token := pickString(runAPIKey, cfg.Provider.Token)
	if token != "" {
		opts = append(opts, openai.WithToken(token))
	}

	if provider == "openai" || provider == "" {
		org := pickString(runOrg, cfg.Provider.Organization)
		if org != "" {
			opts = append(opts, openai.WithOrganization(org))
		}

		apiVersion := pickString(runAPIVersion, cfg.Provider.APIVersion)
		if apiVersion != "" {
			opts = append(opts, openai.WithAPIVersion(apiVersion))
		}

		apiType := strings.ToLower(pickString(runAPIType, cfg.Provider.APIType))
		if apiType == "azure" {
			opts = append(opts, openai.WithAPIType(openai.APITypeAzure))
		}
	}

	return openai.New(opts...)
}

func buildAnthropicLLM(model string, cfg *config.Config) (llms.Model, error) {
	opts := []anthropic.Option{}
	if model != "" {
		opts = append(opts, anthropic.WithModel(model))
	}

	token := pickString(runAPIKey, cfg.Provider.Token)
	if token != "" {
		opts = append(opts, anthropic.WithToken(token))
	}

	baseURL := pickString(runBaseURL, cfg.Provider.BaseURL)
	if baseURL != "" {
		opts = append(opts, anthropic.WithBaseURL(baseURL))
	}

	return anthropic.New(opts...)
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func buildNativeOllamaLLM(model string, cfg *config.Config) llms.Model {
	baseURL := pickString(runBaseURL, cfg.Provider.BaseURL)
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	numCtx := pickInt(runNumCtx, 32768)
	return newOllamaLLM(baseURL, model, numCtx)
}

func isOpenAICompatProvider(provider string) bool {
	return provider == "" || provider == "openai"
}

func pickString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func pickFloat(value, fallback float64) float64 {
	if value >= 0 {
		return value
	}
	return fallback
}

func pickInt(value, fallback int) int {
	if value >= 0 {
		return value
	}
	return fallback
}

func validateActionableResponse(response string) error {
	lower := strings.ToLower(response)
	if strings.Contains(response, "```diff") {
		return nil
	}
	if strings.Contains(lower, "files touched") {
		return nil
	}
	if strings.Contains(lower, "no changes") {
		return nil
	}
	return fmt.Errorf("response is not actionable: missing diff, files touched, or no changes section (disable with --require-actionable=false)")
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

func applyUnifiedDiff(ctx context.Context, workingDir, diff string) error {
	if strings.TrimSpace(diff) == "" {
		return fmt.Errorf("diff content is empty")
	}

	gitErr := applyWithGit(ctx, workingDir, diff)
	if gitErr == nil {
		return nil
	}

	if !runApplyFallback {
		return fmt.Errorf("failed to apply diff with git: %v", gitErr)
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
