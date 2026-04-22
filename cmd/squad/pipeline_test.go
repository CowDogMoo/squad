package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
	pl "github.com/cowdogmoo/squad/pipeline"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// newTestPipelineRunCmd creates a pipeline run command with a context that
// includes a Viper instance and an optional config, mirroring what the
// real root command sets up.
func newTestPipelineRunCmd(cfg *config.Config) *cobra.Command {
	cmd := newPipelineRunCmd()
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
	cmd := newPipelineRunCmd()
	// Without changing flags, flagOrViper should return empty string.
	val := flagOrViper(cmd, "provider", nil, "provider.default")
	if val != "" {
		t.Fatalf("expected empty, got %q", val)
	}
}

func TestPipelineRunDryRun(t *testing.T) {
	// Create a minimal pipeline file.
	dir := t.TempDir()
	pipelinePath := filepath.Join(dir, "test.yaml")
	content := `
name: test-pipeline
version: v1
stages:
  - name: review
    agent: go-review
`
	if err := os.WriteFile(pipelinePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"pipeline", "run", pipelinePath, "--dry-run"})

	var out strings.Builder
	rootCmd.SetOut(&out)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(out.String(), "validated") {
		t.Fatalf("expected validation message, got: %s", out.String())
	}
}

func TestResolveWorkingDir(t *testing.T) {
	t.Parallel()

	t.Run("default uses cwd", func(t *testing.T) {
		t.Parallel()
		cmd := newPipelineRunCmd()
		dir, err := resolveWorkingDir(cmd)
		if err != nil {
			// os.Getwd can fail when parallel tests chdir to
			// temp dirs that are later removed; not our bug.
			t.Skipf("os.Getwd unavailable in parallel test env: %v", err)
		}
		if !filepath.IsAbs(dir) {
			t.Fatalf("expected absolute path, got %q", dir)
		}
	})

	t.Run("flag set returns absolute path", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		cmd := newPipelineRunCmd()
		if err := cmd.Flags().Set("working-dir", tmp); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		dir, err := resolveWorkingDir(cmd)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		abs, _ := filepath.Abs(tmp)
		if dir != abs {
			t.Fatalf("expected %q, got %q", abs, dir)
		}
	})
}

func TestResolveAgentsDir(t *testing.T) {
	t.Parallel()

	t.Run("flag set returns absolute path", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		cmd := newPipelineRunCmd()
		if err := cmd.Flags().Set("agents-dir", tmp); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		dir, err := resolveAgentsDir(cmd, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		abs, _ := filepath.Abs(tmp)
		if dir != abs {
			t.Fatalf("expected %q, got %q", abs, dir)
		}
	})
}

func TestBuildPipelineRunOpts(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		cmd := newTestPipelineRunCmd(cfg)
		opts := buildPipelineRunOpts(cmd, cfg)
		if opts == nil {
			t.Fatal("expected non-nil opts")
		}
		if opts.MaxIterations != 100 {
			t.Fatalf("expected MaxIterations=100, got %d", opts.MaxIterations)
		}
		if opts.MaxCost != 10 {
			t.Fatalf("expected MaxCost=10, got %f", opts.MaxCost)
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
		cmd := newTestPipelineRunCmd(nil)
		opts := buildPipelineRunOpts(cmd, nil)
		if opts.ConfigAvailable {
			t.Fatal("expected ConfigAvailable=false")
		}
	})

	t.Run("max-iterations clamped low", func(t *testing.T) {
		t.Parallel()
		cmd := newTestPipelineRunCmd(nil)
		_ = cmd.Flags().Set("max-iterations", "3")
		opts := buildPipelineRunOpts(cmd, nil)
		if opts.MaxIterations != 10 {
			t.Fatalf("expected MaxIterations=10, got %d", opts.MaxIterations)
		}
	})

	t.Run("max-iterations clamped high", func(t *testing.T) {
		t.Parallel()
		cmd := newTestPipelineRunCmd(nil)
		_ = cmd.Flags().Set("max-iterations", "5000")
		opts := buildPipelineRunOpts(cmd, nil)
		if opts.MaxIterations != 1000 {
			t.Fatalf("expected MaxIterations=1000, got %d", opts.MaxIterations)
		}
	})

	t.Run("negative max-cost becomes zero", func(t *testing.T) {
		t.Parallel()
		cmd := newTestPipelineRunCmd(nil)
		_ = cmd.Flags().Set("max-cost", "-5")
		opts := buildPipelineRunOpts(cmd, nil)
		if opts.MaxCost != 0 {
			t.Fatalf("expected MaxCost=0, got %f", opts.MaxCost)
		}
	})

	t.Run("flag values propagate", func(t *testing.T) {
		t.Parallel()
		cmd := newTestPipelineRunCmd(nil)
		_ = cmd.Flags().Set("provider", "openai")
		_ = cmd.Flags().Set("model", "gpt-4")
		_ = cmd.Flags().Set("api-key", "sk-test")
		_ = cmd.Flags().Set("temperature", "0.7")
		_ = cmd.Flags().Set("max-tokens", "2048")
		opts := buildPipelineRunOpts(cmd, nil)
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
		cmd := newPipelineRunCmd()
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
		cmd := newPipelineRunCmd()
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
		cmd := newPipelineRunCmd()
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
		cmd := newPipelineRunCmd()
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
		cmd := newPipelineRunCmd()
		_ = cmd.Flags().Set("print", "false")
		var out strings.Builder
		cmd.SetOut(&out)

		p := &pl.Pipeline{Name: "test", Version: "v1"}
		pRunner := &pl.Runner{Pipeline: p}
		report := makeReport()

		outputReport(cmd, p, pRunner, report)
		// When print=false but out="" the condition (printOut || outFile == "") is true
		// because outFile is empty, so it still prints.
		if out.Len() == 0 {
			t.Fatal("expected output when no out file is specified")
		}
	})
}

func TestResolveAgentsDirForPipeline(t *testing.T) {
	t.Parallel()

	t.Run("nil config returns error", func(t *testing.T) {
		t.Parallel()
		_, err := resolveAgentsDirForPipeline(nil)
		if err == nil {
			t.Fatal("expected error with nil config, got nil")
		}
	})

	t.Run("empty config returns error", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		_, err := resolveAgentsDirForPipeline(cfg)
		if err == nil {
			t.Fatal("expected error with empty config, got nil")
		}
	})
}

func TestFindAgentDirForPipeline(t *testing.T) {
	t.Parallel()

	t.Run("nil config returns default dir", func(t *testing.T) {
		t.Parallel()
		defaultDir := "/some/default/agents"
		dir, err := findAgentDirForPipeline("my-agent", defaultDir, nil)
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
		dir, err := findAgentDirForPipeline("nonexistent-agent", defaultDir, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dir != defaultDir {
			t.Fatalf("expected %q, got %q", defaultDir, dir)
		}
	})
}

func TestNewPipelineCmd(t *testing.T) {
	t.Parallel()
	cmd := newPipelineCmd()
	if cmd.Use != "pipeline" {
		t.Fatalf("expected Use=pipeline, got %q", cmd.Use)
	}
	if !cmd.HasSubCommands() {
		t.Fatal("expected pipeline cmd to have subcommands")
	}
	// Verify 'run' subcommand exists.
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "run <pipeline.yaml> [prompt]" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'run' subcommand")
	}
}

func TestNewPipelineRunCmd(t *testing.T) {
	t.Parallel()
	cmd := newPipelineRunCmd()
	if cmd.Use != "run <pipeline.yaml> [prompt]" {
		t.Fatalf("expected Use='run <pipeline.yaml> [prompt]', got %q", cmd.Use)
	}

	// Verify key flags exist.
	expectedFlags := []string{
		"agents-dir", "working-dir", "api-key", "base-url",
		"provider", "model", "temperature", "max-tokens",
		"max-iterations", "max-cost", "out", "print",
		"dry-run", "json", "num-ctx", "var",
	}
	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected flag %q to exist", name)
		}
	}
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

// mockViper implements the interface { GetString(string) string } for testing.
type mockViper struct {
	vals map[string]string
}

func (m *mockViper) GetString(key string) string {
	return m.vals[key]
}
