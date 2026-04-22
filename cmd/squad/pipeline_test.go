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
}

func TestFindAgentDirForComposed(t *testing.T) {
	t.Parallel()

	t.Run("nil config returns default dir", func(t *testing.T) {
		t.Parallel()
		defaultDir := "/some/default/agents"
		dir, err := findAgentDirForComposed("my-agent", defaultDir, nil)
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
		dir, err := findAgentDirForComposed("nonexistent-agent", defaultDir, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dir != defaultDir {
			t.Fatalf("expected %q, got %q", defaultDir, dir)
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

// mockViper implements the interface { GetString(string) string } for testing.
type mockViper struct {
	vals map[string]string
}

func (m *mockViper) GetString(key string) string {
	return m.vals[key]
}
