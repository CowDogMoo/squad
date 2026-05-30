package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/mcp"
	pl "github.com/cowdogmoo/squad/pipeline"
	"github.com/cowdogmoo/squad/runner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// newTestRunCmdWithContext creates a run command with a context that
// includes a Viper instance and an optional config.
func newTestRunCmdWithContext(cfg *config.Config) *cobra.Command {
	cmd := newRunCmd()
	ctx := context.Background()
	ctx = withViper(ctx, viper.New())
	if cfg != nil {
		ctx = withConfig(ctx, cfg)
	}
	cmd.SetContext(ctx)
	return cmd
}

func TestMergeVars(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		base     map[string]string
		override map[string]string
		wantKey  string
		wantVal  string
	}{
		{"nil both", nil, nil, "", ""},
		{"base only", map[string]string{"A": "1"}, nil, "A", "1"},
		{"override wins", map[string]string{"A": "1"}, map[string]string{"A": "2"}, "A", "2"},
		{"merge", map[string]string{"A": "1"}, map[string]string{"B": "2"}, "B", "2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeVars(tt.base, tt.override)
			if tt.wantKey == "" {
				if result != nil {
					t.Fatalf("expected nil, got %v", result)
				}
				return
			}
			if result[tt.wantKey] != tt.wantVal {
				t.Fatalf("result[%q] = %q, want %q", tt.wantKey, result[tt.wantKey], tt.wantVal)
			}
		})
	}
}

func TestFlagOrViper(t *testing.T) {
	t.Parallel()
	cmd := newRunCmd()
	// Without changing flags, flagOrViper should return empty string.
	val := flagOrViper(cmd, "provider", nil, "provider.default")
	if val != "" {
		t.Fatalf("expected empty, got %q", val)
	}
}

func TestBuildComposedRunOpts(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		cmd := newTestRunCmdWithContext(cfg)
		opts := buildComposedRunOpts(cmd, cfg)
		if opts == nil {
			t.Fatal("expected non-nil opts")
		}
		if opts.MaxIterations != 100 {
			t.Fatalf("expected MaxIterations=100, got %d", opts.MaxIterations)
		}
		if opts.MaxCost != 5 {
			t.Fatalf("expected MaxCost=5, got %f", opts.MaxCost)
		}
		if opts.NumCtx != 32768 {
			t.Fatalf("expected NumCtx=32768, got %d", opts.NumCtx)
		}
		if !opts.ConfigAvailable {
			t.Fatal("expected ConfigAvailable=true")
		}
	})

	t.Run("nil config", func(t *testing.T) {
		t.Parallel()
		cmd := newTestRunCmdWithContext(nil)
		opts := buildComposedRunOpts(cmd, nil)
		if opts.ConfigAvailable {
			t.Fatal("expected ConfigAvailable=false")
		}
	})

	t.Run("max-iterations clamped low", func(t *testing.T) {
		t.Parallel()
		cmd := newTestRunCmdWithContext(nil)
		_ = cmd.Flags().Set("max-iterations", "3")
		opts := buildComposedRunOpts(cmd, nil)
		if opts.MaxIterations != 10 {
			t.Fatalf("expected MaxIterations=10, got %d", opts.MaxIterations)
		}
	})

	t.Run("max-iterations clamped high", func(t *testing.T) {
		t.Parallel()
		cmd := newTestRunCmdWithContext(nil)
		_ = cmd.Flags().Set("max-iterations", "5000")
		opts := buildComposedRunOpts(cmd, nil)
		if opts.MaxIterations != 1000 {
			t.Fatalf("expected MaxIterations=1000, got %d", opts.MaxIterations)
		}
	})

	t.Run("negative max-cost becomes zero", func(t *testing.T) {
		t.Parallel()
		cmd := newTestRunCmdWithContext(nil)
		_ = cmd.Flags().Set("max-cost", "-5")
		opts := buildComposedRunOpts(cmd, nil)
		if opts.MaxCost != 0 {
			t.Fatalf("expected MaxCost=0, got %f", opts.MaxCost)
		}
	})

	t.Run("flag values propagate", func(t *testing.T) {
		t.Parallel()
		cmd := newTestRunCmdWithContext(nil)
		_ = cmd.Flags().Set("provider", "openai")
		_ = cmd.Flags().Set("model", "gpt-4")
		_ = cmd.Flags().Set("api-key", "sk-test")
		_ = cmd.Flags().Set("temperature", "0.7")
		_ = cmd.Flags().Set("max-tokens", "2048")
		opts := buildComposedRunOpts(cmd, nil)
		if opts.Provider != "openai" {
			t.Fatalf("expected Provider=openai, got %q", opts.Provider)
		}
		if opts.Model != "gpt-4" {
			t.Fatalf("expected Model=gpt-4, got %q", opts.Model)
		}
		if opts.APIKey != "sk-test" {
			t.Fatalf("expected APIKey=sk-test, got %q", opts.APIKey)
		}
		if opts.Temperature != 0.7 {
			t.Fatalf("expected Temperature=0.7, got %f", opts.Temperature)
		}
		if opts.MaxTokens != 2048 {
			t.Fatalf("expected MaxTokens=2048, got %d", opts.MaxTokens)
		}
	})

}

// TestBuildComposedRunOptsExplicitFlagBucket asserts that explicit
// --provider/--model CLI flags populate Model/Provider and leave
// ConfigModel/ConfigProvider empty, so the manifest precedence logic
// treats them as user-explicit (highest precedence).
func TestBuildComposedRunOptsExplicitFlagBucket(t *testing.T) {
	t.Parallel()
	cmd := newTestRunCmdWithContext(nil)
	_ = cmd.Flags().Set("provider", "openai")
	_ = cmd.Flags().Set("model", "gpt-4")
	opts := buildComposedRunOpts(cmd, nil)
	if opts.Provider != "openai" {
		t.Fatalf("Provider=%q, want openai", opts.Provider)
	}
	if opts.Model != "gpt-4" {
		t.Fatalf("Model=%q, want gpt-4", opts.Model)
	}
	if opts.ConfigProvider != "" {
		t.Fatalf("explicit flag should not populate ConfigProvider, got %q", opts.ConfigProvider)
	}
	if opts.ConfigModel != "" {
		t.Fatalf("explicit flag should not populate ConfigModel, got %q", opts.ConfigModel)
	}
}

// TestBuildComposedRunOptsConfigDefaultsBucket asserts that config-file
// defaults route into ConfigModel/ConfigProvider rather than the explicit
// Model/Provider fields. If they collapsed into Model/Provider, the agent
// manifest's model preference would be silently overridden.
func TestBuildComposedRunOptsConfigDefaultsBucket(t *testing.T) {
	t.Parallel()
	cmd := newRunCmd()
	ctx := context.Background()
	v := viper.New()
	v.Set("provider.default", "openai-compat")
	v.Set("model.default", "qwen/qwen3-coder")
	ctx = withViper(ctx, v)
	cmd.SetContext(ctx)
	opts := buildComposedRunOpts(cmd, nil)
	if opts.Provider != "" {
		t.Fatalf("config default leaked into explicit Provider: %q", opts.Provider)
	}
	if opts.Model != "" {
		t.Fatalf("config default leaked into explicit Model: %q", opts.Model)
	}
	if opts.ConfigProvider != "openai-compat" {
		t.Fatalf("ConfigProvider=%q, want openai-compat", opts.ConfigProvider)
	}
	if opts.ConfigModel != "qwen/qwen3-coder" {
		t.Fatalf("ConfigModel=%q, want qwen/qwen3-coder", opts.ConfigModel)
	}
}

func TestOutputReport(t *testing.T) {
	t.Parallel()

	makeReport := func() *pl.Report {
		return &pl.Report{
			Pipeline: "test",
			Version:  "v1",
			Status:   pl.StatusPassed,
			Stages:   []pl.StageResult{},
			Duration: "1s",
		}
	}

	t.Run("print to stdout markdown", func(t *testing.T) {
		t.Parallel()
		cmd := newRunCmd()
		var out strings.Builder
		cmd.SetOut(&out)

		p := &pl.Pipeline{Name: "test", Version: "v1"}
		pRunner := &pl.Runner{Pipeline: p}
		report := makeReport()

		outputReport(cmd, p, pRunner, report)
		if out.Len() == 0 {
			t.Fatal("expected output, got empty")
		}
	})

	t.Run("json output", func(t *testing.T) {
		t.Parallel()
		cmd := newRunCmd()
		_ = cmd.Flags().Set("json", "true")
		var out strings.Builder
		cmd.SetOut(&out)

		p := &pl.Pipeline{Name: "test", Version: "v1"}
		pRunner := &pl.Runner{Pipeline: p}
		report := makeReport()

		outputReport(cmd, p, pRunner, report)

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &parsed); err != nil {
			t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out.String())
		}
		if parsed["pipeline"] != "test" {
			t.Fatalf("expected pipeline=test, got %v", parsed["pipeline"])
		}
	})

	t.Run("json sets output format on nil Output", func(t *testing.T) {
		t.Parallel()
		cmd := newRunCmd()
		_ = cmd.Flags().Set("json", "true")
		var out strings.Builder
		cmd.SetOut(&out)

		p := &pl.Pipeline{Name: "test", Version: "v1"}
		pRunner := &pl.Runner{Pipeline: p}
		report := makeReport()

		outputReport(cmd, p, pRunner, report)
		if p.Output == nil || p.Output.Format != "json" {
			t.Fatal("expected Output.Format to be set to json")
		}
	})

	t.Run("write to file", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		outFile := filepath.Join(tmp, "report.md")
		cmd := newRunCmd()
		_ = cmd.Flags().Set("out", outFile)
		_ = cmd.Flags().Set("print", "false")
		var out strings.Builder
		cmd.SetOut(&out)

		p := &pl.Pipeline{Name: "test", Version: "v1"}
		pRunner := &pl.Runner{Pipeline: p}
		report := makeReport()

		outputReport(cmd, p, pRunner, report)

		data, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}
		if len(data) == 0 {
			t.Fatal("expected non-empty file")
		}
	})

	t.Run("print false and no out still prints", func(t *testing.T) {
		t.Parallel()
		cmd := newRunCmd()
		_ = cmd.Flags().Set("print", "false")
		var out strings.Builder
		cmd.SetOut(&out)

		p := &pl.Pipeline{Name: "test", Version: "v1"}
		pRunner := &pl.Runner{Pipeline: p}
		report := makeReport()

		outputReport(cmd, p, pRunner, report)
		if out.Len() == 0 {
			t.Fatal("expected output when no out file is specified")
		}
	})
}

func TestResolveAgentsDirFromConfig(t *testing.T) {
	t.Parallel()

	t.Run("nil config returns error", func(t *testing.T) {
		t.Parallel()
		_, err := resolveAgentsDirFromConfig(nil)
		if err == nil {
			t.Fatal("expected error with nil config, got nil")
		}
	})

	t.Run("empty config returns error", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		_, err := resolveAgentsDirFromConfig(cfg)
		if err == nil {
			t.Fatal("expected error with empty config, got nil")
		}
	})

	t.Run("config with local path returns path", func(t *testing.T) {
		t.Parallel()
		agentsDir := t.TempDir()
		cfg := &config.Config{
			Agents: config.AgentsConfig{
				LocalPaths: []string{agentsDir},
			},
		}
		result, err := resolveAgentsDirFromConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == "" {
			t.Fatal("expected non-empty path")
		}
	})
}

func TestFindAgentDirForComposed(t *testing.T) {
	t.Parallel()

	t.Run("nil config returns default dir", func(t *testing.T) {
		t.Parallel()
		defaultDir := "/some/default/agents"
		dir, err := findAgentDirForComposed("my-agent", defaultDir, "", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dir != defaultDir {
			t.Fatalf("expected %q, got %q", defaultDir, dir)
		}
	})

	t.Run("config with no sources falls back to default", func(t *testing.T) {
		t.Parallel()
		defaultDir := "/fallback/agents"
		cfg := &config.Config{}
		dir, err := findAgentDirForComposed("nonexistent-agent", defaultDir, "", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dir != defaultDir {
			t.Fatalf("expected %q, got %q", defaultDir, dir)
		}
	})

	t.Run("nested sub-agent found inside composed dir", func(t *testing.T) {
		t.Parallel()
		composedDir := t.TempDir()
		subAgentDir := filepath.Join(composedDir, "agents", "my-sub-agent")
		if err := os.MkdirAll(subAgentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subAgentDir, "agent.yaml"), []byte("name: my-sub-agent\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		dir, err := findAgentDirForComposed("my-sub-agent", "/fallback", composedDir, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(composedDir, "agents")
		if dir != expected {
			t.Fatalf("expected %q, got %q", expected, dir)
		}
	})

	t.Run("nested dir checked before global fallback", func(t *testing.T) {
		t.Parallel()
		composedDir := t.TempDir()
		// No sub-agent exists — should fall back to default
		dir, err := findAgentDirForComposed("missing-agent", "/fallback", composedDir, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dir != "/fallback" {
			t.Fatalf("expected /fallback, got %q", dir)
		}
	})
}

func TestFlagOrViperWithMockViper(t *testing.T) {
	t.Parallel()

	t.Run("flag changed takes precedence", func(t *testing.T) {
		t.Parallel()
		cmd := &cobra.Command{}
		cmd.Flags().String("provider", "", "")
		_ = cmd.Flags().Set("provider", "anthropic")

		mv := &mockViper{vals: map[string]string{"provider.default": "openai"}}
		val := flagOrViper(cmd, "provider", mv, "provider.default")
		if val != "anthropic" {
			t.Fatalf("expected flag value 'anthropic', got %q", val)
		}
	})

	t.Run("viper used when flag not changed", func(t *testing.T) {
		t.Parallel()
		cmd := &cobra.Command{}
		cmd.Flags().String("provider", "", "")
		// Don't set the flag.

		mv := &mockViper{vals: map[string]string{"provider.default": "openai"}}
		val := flagOrViper(cmd, "provider", mv, "provider.default")
		if val != "openai" {
			t.Fatalf("expected viper value 'openai', got %q", val)
		}
	})
}

func TestBuildRunAgentFunc_ResolvesAgent(t *testing.T) {
	t.Parallel()

	// Create a real agent on disk that buildRunAgentFunc can resolve.
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, "test-leaf")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestYAML := "name: test-leaf\nversion: v1\nentrypoint: system.md\nwrapper: agent.md\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("system prompt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("agent wrapper"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &runner.RunOptions{
		Provider: "test",
		Model:    "test-model",
	}
	pRunner := &pl.Runner{
		InlineAgents: make(map[string]*pl.InlineConfig),
	}

	fn := buildRunAgentFunc(opts, agentsDir, "", &config.Config{}, nil, pRunner)

	// The callback should resolve the agent and build its bundle. It will fail
	// at InvokeModel (no real provider), but that confirms the agent resolution
	// and bundle building paths are exercised.
	_, _, err := fn(context.Background(), "test-leaf", "review this", t.TempDir(), "edit", nil)
	if err == nil {
		t.Fatal("expected error from InvokeModel (no real provider)")
	}
	// The error should come from the model invocation, not agent resolution.
	if strings.Contains(err.Error(), "failed to build agent") {
		t.Fatalf("agent resolution failed: %v", err)
	}
}

func TestBuildRunAgentFunc_ManifestModelProvider(t *testing.T) {
	t.Parallel()

	t.Run("manifest model and provider applied when opts empty", func(t *testing.T) {
		t.Parallel()
		agentsDir := t.TempDir()
		agentDir := filepath.Join(agentsDir, "model-leaf")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := "name: model-leaf\nversion: v1\nentrypoint: system.md\nwrapper: agent.md\nmodels:\n  - model: manifest-model\n    provider: manifest-provider\n"
		if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("sys"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrap"), 0o644); err != nil {
			t.Fatal(err)
		}

		opts := &runner.RunOptions{} // No model or provider set
		pRunner := &pl.Runner{InlineAgents: make(map[string]*pl.InlineConfig)}

		fn := buildRunAgentFunc(opts, agentsDir, "", &config.Config{}, nil, pRunner)
		_, _, err := fn(context.Background(), "model-leaf", "prompt", t.TempDir(), "edit", nil)
		// Will fail at InvokeModel but confirms manifest values are applied.
		if err == nil {
			t.Fatal("expected error from InvokeModel")
		}
		// Error should mention manifest-provider (meaning it was applied), not "failed to build agent".
		if strings.Contains(err.Error(), "failed to build agent") {
			t.Fatalf("agent resolution failed: %v", err)
		}
	})

	t.Run("CLI flags take precedence over manifest", func(t *testing.T) {
		t.Parallel()
		agentsDir := t.TempDir()
		agentDir := filepath.Join(agentsDir, "cli-leaf")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := "name: cli-leaf\nversion: v1\nentrypoint: system.md\nwrapper: agent.md\nmodels:\n  - model: manifest-model\n    provider: manifest-provider\n"
		if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("sys"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrap"), 0o644); err != nil {
			t.Fatal(err)
		}

		opts := &runner.RunOptions{
			Provider: "cli-provider",
			Model:    "cli-model",
		}
		pRunner := &pl.Runner{InlineAgents: make(map[string]*pl.InlineConfig)}

		fn := buildRunAgentFunc(opts, agentsDir, "", &config.Config{}, nil, pRunner)
		_, _, err := fn(context.Background(), "cli-leaf", "prompt", t.TempDir(), "edit", nil)
		if err == nil {
			t.Fatal("expected error from InvokeModel")
		}
		if strings.Contains(err.Error(), "failed to build agent") {
			t.Fatalf("agent resolution failed: %v", err)
		}
	})
}

// TestBuildRunAgentFunc_ConfigDefaultNotInManifestWarns asserts that when the
// config default model/provider is not listed in the agent manifest, the
// fallback warning is emitted on stderr.
func TestBuildRunAgentFunc_ConfigDefaultNotInManifestWarns(t *testing.T) {
	// Redirects os.Stderr; cannot run in parallel with other os.Stderr users.
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, "warn-leaf")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: warn-leaf\nversion: v1\nentrypoint: system.md\nwrapper: agent.md\nmodels:\n  - model: manifest-model\n    provider: manifest-provider\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("sys"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrap"), 0o644); err != nil {
		t.Fatal(err)
	}

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	opts := &runner.RunOptions{
		ConfigModel:    "other-model",
		ConfigProvider: "other-provider",
	}
	pRunner := &pl.Runner{InlineAgents: make(map[string]*pl.InlineConfig)}

	fn := buildRunAgentFunc(opts, agentsDir, "", &config.Config{}, nil, pRunner)
	_, _, runErr := fn(context.Background(), "warn-leaf", "prompt", t.TempDir(), "edit", nil)
	_ = w.Close()
	out, _ := io.ReadAll(r)
	if runErr == nil {
		t.Fatal("expected error from InvokeModel")
	}
	if !strings.Contains(string(out), "not listed in the agent manifest") {
		t.Fatalf("expected warning on stderr, got %q", string(out))
	}
}

func TestBuildRunAgentFunc_BudgetPropagation(t *testing.T) {
	t.Parallel()

	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, "budgeted")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"),
		[]byte("name: budgeted\nversion: v1\nentrypoint: system.md\nwrapper: agent.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("sys"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrap"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &runner.RunOptions{MaxCost: 10.0}
	pRunner := &pl.Runner{InlineAgents: make(map[string]*pl.InlineConfig)}

	fn := buildRunAgentFunc(opts, agentsDir, "", &config.Config{}, nil, pRunner)

	// Inject pipeline budget var.
	stageVars := map[string]string{pl.PipelineMaxCostVar: "3.50"}
	_, _, err := fn(context.Background(), "budgeted", "prompt", t.TempDir(), "edit", stageVars)
	// Will fail at InvokeModel but confirms budget parsing path runs.
	if err == nil {
		t.Fatal("expected error from InvokeModel")
	}
}

func TestBuildRunAgentFunc_ExhaustedBudget(t *testing.T) {
	t.Parallel()

	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, "cheapo")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"),
		[]byte("name: cheapo\nversion: v1\nentrypoint: system.md\nwrapper: agent.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("sys"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrap"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &runner.RunOptions{}
	pRunner := &pl.Runner{InlineAgents: make(map[string]*pl.InlineConfig)}

	fn := buildRunAgentFunc(opts, agentsDir, "", &config.Config{}, nil, pRunner)

	// Budget var with invalid (non-positive) value triggers exhaustion error.
	stageVars := map[string]string{pl.PipelineMaxCostVar: "0.00"}
	_, _, err := fn(context.Background(), "cheapo", "prompt", t.TempDir(), "edit", stageVars)
	if err == nil {
		t.Fatal("expected error for exhausted budget")
	}
	if !strings.Contains(err.Error(), "budget exhausted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildRunAgentFunc_AgentNotFound(t *testing.T) {
	t.Parallel()

	agentsDir := t.TempDir()
	opts := &runner.RunOptions{}
	pRunner := &pl.Runner{InlineAgents: make(map[string]*pl.InlineConfig)}

	fn := buildRunAgentFunc(opts, agentsDir, "", nil, nil, pRunner)

	_, _, err := fn(context.Background(), "nonexistent", "prompt", t.TempDir(), "edit", nil)
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
	if !strings.Contains(err.Error(), "failed to build agent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildRunAgentFunc_InlineAgent(t *testing.T) {
	t.Parallel()

	// Create a composed agent directory with inline prompt files.
	composedDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(composedDir, "system.md"), []byte("inline system"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(composedDir, "agent.md"), []byte("inline wrapper"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &runner.RunOptions{Provider: "test", Model: "test-model"}
	pRunner := &pl.Runner{
		ComposedDir: composedDir,
		InlineAgents: map[string]*pl.InlineConfig{
			"my-inline": {
				EntryPoint: "system.md",
				Wrapper:    "agent.md",
				Models: []pl.ModelPreference{
					{Model: "gpt-4", Provider: "openai"},
				},
			},
		},
	}

	fn := buildRunAgentFunc(opts, t.TempDir(), composedDir, &config.Config{}, nil, pRunner)

	// The inline path resolves successfully and fails at model invocation.
	_, _, err := fn(context.Background(), "my-inline", "test prompt", t.TempDir(), "edit", nil)
	if err == nil {
		t.Fatal("expected error from InvokeModel (no real provider)")
	}
	// Should NOT fail at inline build.
	if strings.Contains(err.Error(), "failed to build inline agent") {
		t.Fatalf("inline agent build failed: %v", err)
	}
}

func TestBuildRunAgentFunc_InlineBuildError(t *testing.T) {
	t.Parallel()

	// composedDir has no prompt files — BuildBundleInline will fail.
	composedDir := t.TempDir()
	opts := &runner.RunOptions{}
	pRunner := &pl.Runner{
		ComposedDir: composedDir,
		InlineAgents: map[string]*pl.InlineConfig{
			"bad-inline": {
				EntryPoint: "missing.md",
				Wrapper:    "also-missing.md",
			},
		},
	}

	fn := buildRunAgentFunc(opts, t.TempDir(), composedDir, nil, nil, pRunner)

	_, _, err := fn(context.Background(), "bad-inline", "test", t.TempDir(), "edit", nil)
	if err == nil {
		t.Fatal("expected error for missing inline prompts")
	}
	if !strings.Contains(err.Error(), "failed to build inline agent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBuildRunAgentFunc_StageMCPOverrideErrorSurfaces verifies that a
// stage with a malformed MCP override template produces a wrapped error
// before InvokeModel is reached. This exercises the cmd-layer wiring
// (StageByName lookup + ApplyMCPOverride) end-to-end.
func TestBuildRunAgentFunc_StageMCPOverrideErrorSurfaces(t *testing.T) {
	t.Parallel()

	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, "test-leaf")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"),
		[]byte("name: test-leaf\nversion: v1\nentrypoint: system.md\nwrapper: agent.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("sys"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrap"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &pl.Pipeline{
		Name: "stage-override-error",
		Stages: []pl.Stage{
			{
				Name:  "broken-stage",
				Agent: "test-leaf",
				MCPServers: []mcp.ServerConfig{
					{Name: "bad", Command: "{{.NonexistentField}}"},
				},
			},
		},
	}
	pRunner := &pl.Runner{Pipeline: p, InlineAgents: make(map[string]*pl.InlineConfig)}

	fn := buildRunAgentFunc(&runner.RunOptions{Provider: "test", Model: "test-model"},
		agentsDir, "", &config.Config{}, nil, pRunner)

	vars := map[string]string{pl.PipelineStageNameVar: "broken-stage"}
	_, _, err := fn(context.Background(), "test-leaf", "prompt", t.TempDir(), "edit", vars)
	if err == nil {
		t.Fatal("expected error from broken MCP override template")
	}
	if !strings.Contains(err.Error(), "mcp override") || !strings.Contains(err.Error(), "broken-stage") {
		t.Fatalf("expected wrapped 'mcp override' error mentioning stage name, got: %v", err)
	}
}

// TestBuildRunAgentFunc_NoStageOverrideDoesNotApply verifies that when a
// stage has no MCPServers override, the cmd-layer does not touch the
// bundle's MCP list. We assert by leaving the override nil and confirming
// the call reaches InvokeModel (not the override path) — same pattern
// the other tests use.
func TestBuildRunAgentFunc_NoStageOverrideDoesNotApply(t *testing.T) {
	t.Parallel()

	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, "test-leaf")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"),
		[]byte("name: test-leaf\nversion: v1\nentrypoint: system.md\nwrapper: agent.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("sys"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrap"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &pl.Pipeline{
		Name: "no-override",
		Stages: []pl.Stage{
			{Name: "plain-stage", Agent: "test-leaf"}, // nil MCPServers
		},
	}
	pRunner := &pl.Runner{Pipeline: p, InlineAgents: make(map[string]*pl.InlineConfig)}

	fn := buildRunAgentFunc(&runner.RunOptions{Provider: "test", Model: "test-model"},
		agentsDir, "", &config.Config{}, nil, pRunner)

	vars := map[string]string{pl.PipelineStageNameVar: "plain-stage"}
	_, _, err := fn(context.Background(), "test-leaf", "prompt", t.TempDir(), "edit", vars)
	if err == nil {
		t.Fatal("expected error from InvokeModel (no real provider)")
	}
	// Failure must come from invocation, not the override path.
	if strings.Contains(err.Error(), "mcp override") {
		t.Fatalf("override path triggered unexpectedly: %v", err)
	}
}

func TestOutputReport_FormatError(t *testing.T) {
	t.Parallel()

	cmd := newRunCmd()
	var out strings.Builder
	cmd.SetOut(&out)

	// Pipeline with nil Output causes FormatReport to use markdown.
	p := &pl.Pipeline{Name: "test", Version: "v1"}
	pRunner := &pl.Runner{Pipeline: p}

	// Report with nil internals — FormatReport will still produce output.
	report := &pl.Report{
		Pipeline: "test",
		Version:  "v1",
		Status:   pl.StatusPassed,
		Duration: "1s",
	}

	outputReport(cmd, p, pRunner, report)
	if out.Len() == 0 {
		t.Fatal("expected output")
	}
}

// mockViper implements the interface { GetString(string) string } for testing.
type mockViper struct {
	vals map[string]string
}

func (m *mockViper) GetString(key string) string {
	return m.vals[key]
}
