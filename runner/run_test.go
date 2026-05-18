package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/spf13/cobra"
)

func TestReadPrompt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		stdin   string
		want    string
		wantErr bool
	}{
		{"from args", []string{"hello", "world"}, "", "hello world", false},
		// Note: stdin piped detection only works with *os.File, not strings.Reader.
		// When stdin is a strings.Reader, readPrompt returns empty string.
		// This is expected behavior - the agent's default user_prompt will be used.
		{"no args no stdin uses agent default", nil, "", "", false},
		{"whitespace only uses agent default", nil, "   \n", "", false}, // No longer an error
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			if tt.stdin != "" || (tt.args == nil && tt.stdin == "") {
				cmd.SetIn(strings.NewReader(tt.stdin))
			}
			got, err := readPrompt(cmd, tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("readPrompt() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("readPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveWorkingDir(t *testing.T) {
	t.Parallel()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	tmp := t.TempDir()
	tests := []struct {
		name string
		dir  string
		want string
	}{
		{"empty returns cwd", "", cwd},
		{"explicit path", tmp, tmp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolved, err := resolveWorkingDir(tt.dir)
			if err != nil {
				t.Fatalf("resolveWorkingDir(%q) error = %v", tt.dir, err)
			}
			if resolved != tt.want {
				t.Fatalf("resolveWorkingDir(%q) = %q, want %q", tt.dir, resolved, tt.want)
			}
		})
	}
}

func TestResolveRunWorkingDir_Standard(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dir, cleanup, err := resolveRunWorkingDir(&RunOptions{WorkingDir: tmp})
	if err != nil {
		t.Fatalf("resolveRunWorkingDir: %v", err)
	}
	defer cleanup()
	if dir != tmp {
		t.Fatalf("dir = %q, want %q", dir, tmp)
	}
	// Standard mode: cleanup is a no-op (directory survives).
	cleanup()
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("standard working dir was unexpectedly removed: %v", err)
	}
}

func TestResolveRunWorkingDir_RemoteOnly(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "remote")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := "name: remote\nversion: 1\nworking_dir: none\nprompt: hi\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	opts := &RunOptions{Agent: "remote", AgentsDir: filepath.Join(tmp, "agents")}
	dir, cleanup, err := resolveRunWorkingDir(opts)
	if err != nil {
		t.Fatalf("resolveRunWorkingDir: %v", err)
	}
	if dir == "" {
		t.Fatal("expected a temp dir, got empty string")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("temp dir not created: %v", err)
	}
	if !strings.Contains(dir, "squad-remote-") {
		t.Errorf("temp dir name = %q, want it to contain squad-remote-", dir)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temp dir should have been removed, stat err = %v", err)
	}
}

// TestAgentIsRemoteOnly_FindAgentDirError covers the FindAgentDir failure
// branch (config==nil with no explicit dir).
func TestAgentIsRemoteOnly_FindAgentDirError(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{Agent: "ghost"} // no AgentsDir, no Config — FindAgentDir errors
	if got := agentIsRemoteOnly(opts); got {
		t.Fatal("expected false when FindAgentDir errors")
	}
}

// TestResolveRunWorkingDir_MkdirTempFailure covers the os.MkdirTemp error
// path by pointing TMPDIR at a non-existent directory.
func TestResolveRunWorkingDir_MkdirTempFailure(t *testing.T) {
	// Cannot use t.Parallel — modifies TMPDIR via t.Setenv.
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "remote")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := "name: remote\nversion: 1\nworking_dir: none\nprompt: hi\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("TMPDIR", filepath.Join(tmp, "does-not-exist"))
	opts := &RunOptions{Agent: "remote", AgentsDir: tmp}
	_, cleanup, err := resolveRunWorkingDir(opts)
	if err == nil {
		cleanup()
		t.Fatal("expected MkdirTemp failure for non-existent TMPDIR")
	}
	if !strings.Contains(err.Error(), "failed to create temp working dir") {
		t.Fatalf("error = %q, want temp-dir creation failure", err.Error())
	}
}

// TestAgentIsRemoteOnly_ManifestParseError covers the LoadManifest failure
// branch — the directory exists but agent.yaml is malformed.
func TestAgentIsRemoteOnly_ManifestParseError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "broken")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("bad: ["), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	opts := &RunOptions{Agent: "broken", AgentsDir: tmp}
	if got := agentIsRemoteOnly(opts); got {
		t.Fatal("expected false when manifest is malformed")
	}
}

func TestAgentIsRemoteOnly(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	cases := []struct {
		name     string
		manifest string
		want     bool
	}{
		{"remote-only", "name: x\nversion: 1\nworking_dir: none\nprompt: hi\n", true},
		{"standard leaf", "name: x\nversion: 1\nentrypoint: sys.md\nwrapper: wrap.md\n", false},
		{"missing agent", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := &RunOptions{}
			if tc.manifest != "" {
				agentDir := filepath.Join(tmp, tc.name)
				if err := os.MkdirAll(agentDir, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(tc.manifest), 0o644); err != nil {
					t.Fatalf("write manifest: %v", err)
				}
				opts.Agent = tc.name
				opts.AgentsDir = tmp
			}
			if got := agentIsRemoteOnly(opts); got != tc.want {
				t.Errorf("agentIsRemoteOnly() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWriteResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		opts       *RunOptions
		response   string
		wantStdout bool
		wantFile   bool
	}{
		{"print to stdout", &RunOptions{Print: true}, "hello", true, false},
		{"write to file", &RunOptions{}, "file output", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			var buf bytes.Buffer
			cmd.SetOut(&buf)

			if tt.wantFile {
				tt.opts.Output = filepath.Join(t.TempDir(), "response.txt")
			}
			if err := writeResponse(cmd, tt.response, tt.opts); err != nil {
				t.Fatalf("writeResponse() error = %v", err)
			}
			if tt.wantStdout && !strings.Contains(buf.String(), tt.response) {
				t.Fatalf("expected stdout to contain %q", tt.response)
			}
			if tt.wantFile {
				data, err := os.ReadFile(tt.opts.Output)
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				if string(data) != tt.response {
					t.Fatalf("file content = %q, want %q", string(data), tt.response)
				}
				if buf.Len() != 0 {
					t.Fatalf("expected no stdout output when writing to file")
				}
			}
		})
	}
}

func TestHandleResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		opts            *RunOptions
		response        string
		wantErr         bool
		wantErrContains string
		wantStdout      bool
		wantFile        bool
	}{
		{
			name:            "requires actionable error",
			opts:            &RunOptions{RequireActionable: true},
			response:        "plain text",
			wantErr:         true,
			wantErrContains: "not actionable",
		},
		{
			name:       "apply no changes writes file",
			opts:       &RunOptions{Apply: true},
			response:   "No changes",
			wantErr:    false,
			wantFile:   true,
			wantStdout: false,
		},
		{
			name:       "prints response",
			opts:       &RunOptions{Print: true},
			response:   "hello",
			wantErr:    false,
			wantStdout: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			if tt.wantFile {
				tt.opts.Output = filepath.Join(t.TempDir(), "response.txt")
			}
			ctx := context.Background()
			cmd.SetContext(ctx)
			err := handleResponse(cmd, tt.opts, tt.response, t.TempDir())
			if (err != nil) != tt.wantErr {
				t.Fatalf("handleResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.wantErrContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("handleResponse() error = %q, want to contain %q", err.Error(), tt.wantErrContains)
				}
			}
			if tt.wantStdout && !strings.Contains(buf.String(), tt.response) {
				t.Fatalf("expected stdout to contain %q", tt.response)
			}
			if tt.wantFile {
				data, err := os.ReadFile(tt.opts.Output)
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				if string(data) != tt.response {
					t.Fatalf("file content = %q, want %q", string(data), tt.response)
				}
				if buf.Len() != 0 {
					t.Fatalf("expected no stdout output when writing to file")
				}
			}
		})
	}
}

func TestPrepareBundle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		opts            *RunOptions
		wantErr         bool
		wantErrContains string
		wantBundle      bool
		checkOutput     bool
	}{
		{
			name: "dry run with bundle output",
			opts: &RunOptions{
				Agent:       "go-tests",
				AgentsDir:   filepath.Join("..", "agents"),
				PrintBundle: true,
				DryRun:      true,
			},
			wantErr:     false,
			wantBundle:  false,
			checkOutput: true,
		},
		{
			name: "config not available",
			opts: &RunOptions{
				Agent:           "go-tests",
				AgentsDir:       filepath.Join("..", "agents"),
				ConfigAvailable: false,
			},
			wantErr:         true,
			wantErrContains: "config not available",
			wantBundle:      false,
			checkOutput:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			agentsDir := writeTestAgent(t, tt.opts.Agent)
			tt.opts.AgentsDir = agentsDir
			if tt.checkOutput {
				tt.opts.BundleOut = filepath.Join(t.TempDir(), "bundle.txt")
			}
			bundle, err := prepareBundle(cmd, tt.opts, "prompt", t.TempDir())
			if (err != nil) != tt.wantErr {
				t.Fatalf("prepareBundle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.wantErrContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("prepareBundle() error = %q, want to contain %q", err.Error(), tt.wantErrContains)
				}
			}
			if (bundle != nil) != tt.wantBundle {
				t.Fatalf("prepareBundle() bundle = %v, wantBundle %v", bundle != nil, tt.wantBundle)
			}
			if tt.checkOutput {
				if buf.Len() == 0 {
					t.Fatalf("expected stdout output")
				}
				if _, err := os.Stat(tt.opts.BundleOut); err != nil {
					t.Fatalf("expected bundle file: %v", err)
				}
			}
		})
	}
}

func TestResolveModelPrecedenceNilGuard(t *testing.T) {
	t.Parallel()
	// Calling with either argument nil must be a no-op rather than a panic;
	// pipeline code passes a bundle that may be nil on early-exit paths.
	ResolveModelPrecedence(context.Background(), nil, nil)
	ResolveModelPrecedence(context.Background(), &RunOptions{}, nil)
	ResolveModelPrecedence(context.Background(), nil, &agent.Bundle{})

	opts := &RunOptions{Model: "preset", Provider: "preset"}
	res := ResolveModelPrecedence(context.Background(), opts, &agent.Bundle{Model: "manifest", Provider: "manifest"})
	if res.Model != "preset" || res.Provider != "preset" {
		t.Fatalf("explicit values must win over manifest: Model=%q Provider=%q", res.Model, res.Provider)
	}
}

// TestResolveModelPrecedenceBaseURL verifies BaseURL propagation for the
// openai-compat provider path, where a base URL must flow from either the
// manifest bundle or a config-matched model preference into RunOptions.
func TestResolveModelPrecedenceBaseURL(t *testing.T) {
	t.Parallel()
	const compatURL = "https://api.deepinfra.com/v1/openai"

	t.Run("copies bundle BaseURL when opts has none", func(t *testing.T) {
		t.Parallel()
		opts := &RunOptions{}
		bundle := &agent.Bundle{
			Model:    "meta-llama/Meta-Llama-3-70B-Instruct",
			Provider: "openai-compat",
			BaseURL:  compatURL,
		}
		res := ResolveModelPrecedence(context.Background(), opts, bundle)
		if res.BaseURL != compatURL {
			t.Fatalf("BaseURL = %q, want %q", res.BaseURL, compatURL)
		}
	})

	t.Run("does not overwrite explicit opts BaseURL", func(t *testing.T) {
		t.Parallel()
		const explicit = "https://my-endpoint.example.com/v1"
		opts := &RunOptions{BaseURL: explicit}
		bundle := &agent.Bundle{
			Model:    "meta-llama/Meta-Llama-3-70B-Instruct",
			Provider: "openai-compat",
			BaseURL:  compatURL,
		}
		res := ResolveModelPrecedence(context.Background(), opts, bundle)
		if res.BaseURL != explicit {
			t.Fatalf("BaseURL = %q, want explicit %q", res.BaseURL, explicit)
		}
	})

	t.Run("propagates BaseURL from config-matched model preference", func(t *testing.T) {
		t.Parallel()
		opts := &RunOptions{
			ConfigProvider: "openai-compat",
			ConfigModel:    "meta-llama/Meta-Llama-3-70B-Instruct",
		}
		bundle := &agent.Bundle{
			Models: []agent.ModelPreference{
				{
					Provider: "openai-compat",
					Model:    "meta-llama/Meta-Llama-3-70B-Instruct",
					BaseURL:  compatURL,
				},
			},
		}
		res := ResolveModelPrecedence(context.Background(), opts, bundle)
		if res.BaseURL != compatURL {
			t.Fatalf("BaseURL = %q, want %q", res.BaseURL, compatURL)
		}
		if res.Provider != "openai-compat" {
			t.Fatalf("Provider = %q, want openai-compat", res.Provider)
		}
	})
}

func runAndAssertBundle(t *testing.T, opts *RunOptions, wantModel, wantProvider string) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_, err := prepareBundle(cmd, opts, "prompt", t.TempDir())
	if err != nil {
		t.Fatalf("prepareBundle() error = %v", err)
	}
	if opts.Model != wantModel {
		t.Fatalf("expected Model=%q, got %q", wantModel, opts.Model)
	}
	if opts.Provider != wantProvider {
		t.Fatalf("expected Provider=%q, got %q", wantProvider, opts.Provider)
	}
}

func TestPrepareBundleManifestModelProvider(t *testing.T) {
	t.Parallel()

	t.Run("manifest model and provider applied when opts empty", func(t *testing.T) {
		t.Parallel()
		agentsDir := writeTestAgentWithModels(t, "model-agent", "manifest-model", "manifest-provider")
		opts := &RunOptions{
			Agent:       "model-agent",
			AgentsDir:   agentsDir,
			DryRun:      true,
			PrintBundle: true,
			BundleOut:   filepath.Join(t.TempDir(), "bundle.txt"),
		}
		runAndAssertBundle(t, opts, "manifest-model", "manifest-provider")
	})

	t.Run("config default wins over manifest even when not listed", func(t *testing.T) {
		t.Parallel()
		agentsDir := writeTestAgentWithModels(t, "manifest-vs-config", "manifest-model", "manifest-provider")
		opts := &RunOptions{
			Agent:          "manifest-vs-config",
			AgentsDir:      agentsDir,
			ConfigModel:    "config-model",
			ConfigProvider: "config-provider",
			DryRun:         true,
			PrintBundle:    true,
			BundleOut:      filepath.Join(t.TempDir(), "bundle.txt"),
		}
		runAndAssertBundle(t, opts, "config-model", "config-provider")
	})

	t.Run("config defaults fill in when manifest is silent", func(t *testing.T) {
		t.Parallel()
		agentsDir := writeTestAgentNoModels(t, "config-fallback")
		opts := &RunOptions{
			Agent:          "config-fallback",
			AgentsDir:      agentsDir,
			ConfigModel:    "config-model",
			ConfigProvider: "config-provider",
			DryRun:         true,
			PrintBundle:    true,
			BundleOut:      filepath.Join(t.TempDir(), "bundle.txt"),
		}
		runAndAssertBundle(t, opts, "config-model", "config-provider")
	})

	t.Run("config default wins when it is listed in manifest", func(t *testing.T) {
		t.Parallel()
		// Agent manifest lists exactly the config default model/provider.
		agentsDir := writeTestAgentWithModels(t, "config-in-manifest", "config-model", "config-provider")
		opts := &RunOptions{
			Agent:          "config-in-manifest",
			AgentsDir:      agentsDir,
			ConfigModel:    "config-model",
			ConfigProvider: "config-provider",
			DryRun:         true,
			PrintBundle:    true,
			BundleOut:      filepath.Join(t.TempDir(), "bundle.txt"),
		}
		runAndAssertBundle(t, opts, "config-model", "config-provider")
	})

	t.Run("config default wins when listed as non-first model", func(t *testing.T) {
		t.Parallel()
		agentsDir := writeTestAgentWithMultiModels(t, "multi-model-agent",
			[]struct{ model, provider string }{
				{"claude-sonnet-4-6", "anthropic"},
				{"qwen3-coder-480b", "nvidia"},
			},
		)
		opts := &RunOptions{
			Agent:          "multi-model-agent",
			AgentsDir:      agentsDir,
			ConfigModel:    "qwen3-coder-480b",
			ConfigProvider: "nvidia",
			DryRun:         true,
			PrintBundle:    true,
			BundleOut:      filepath.Join(t.TempDir(), "bundle.txt"),
		}
		runAndAssertBundle(t, opts, "qwen3-coder-480b", "nvidia")
	})

	t.Run("CLI flags take precedence over manifest", func(t *testing.T) {
		t.Parallel()
		agentsDir := writeTestAgentWithModels(t, "cli-agent", "manifest-model", "manifest-provider")
		opts := &RunOptions{
			Agent:       "cli-agent",
			AgentsDir:   agentsDir,
			Model:       "cli-model",
			Provider:    "cli-provider",
			DryRun:      true,
			PrintBundle: true,
			BundleOut:   filepath.Join(t.TempDir(), "bundle.txt"),
		}
		runAndAssertBundle(t, opts, "cli-model", "cli-provider")
	})

	t.Run("config default not in manifest emits warning but still runs with config default", func(t *testing.T) {
		t.Parallel()
		agentsDir := writeTestAgentWithModels(t, "warn-fallback", "manifest-model", "manifest-provider")
		opts := &RunOptions{
			Agent:          "warn-fallback",
			AgentsDir:      agentsDir,
			ConfigModel:    "other-model",
			ConfigProvider: "other-provider",
			DryRun:         true,
			PrintBundle:    true,
			BundleOut:      filepath.Join(t.TempDir(), "bundle.txt"),
		}
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		if _, err := prepareBundle(cmd, opts, "prompt", t.TempDir()); err != nil {
			t.Fatalf("prepareBundle() error = %v", err)
		}
		if opts.Model != "other-model" || opts.Provider != "other-provider" {
			t.Fatalf("expected config default to be used, got Model=%q Provider=%q", opts.Model, opts.Provider)
		}
		if !strings.Contains(stderr.String(), "not a preferred model for this agent") {
			t.Fatalf("expected warning on stderr, got %q", stderr.String())
		}
	})
}

func TestInvokeModelMissingAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	opts := &RunOptions{Provider: "openai-responses", Model: "gpt-5"}
	bundle := &agent.Bundle{System: "sys", User: "user", WorkDir: "."}
	_, _, err := InvokeModel(context.Background(), opts, bundle)
	if err == nil {
		t.Fatalf("expected error for missing API key")
	}
	if !strings.Contains(err.Error(), "API key required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildTaskConfig(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{AgentsDir: "agents", WorkingDir: "work", MaxIterations: 3}
	cfg := buildTaskConfig(opts)
	if cfg == nil {
		t.Fatalf("expected task config")
	}
	if cfg.AgentsDir != opts.AgentsDir {
		t.Fatalf("AgentsDir = %q, want %q", cfg.AgentsDir, opts.AgentsDir)
	}
	if cfg.WorkingDir != opts.WorkingDir {
		t.Fatalf("WorkingDir = %q, want %q", cfg.WorkingDir, opts.WorkingDir)
	}
	if cfg.MaxIterations != opts.MaxIterations {
		t.Fatalf("MaxIterations = %d, want %d", cfg.MaxIterations, opts.MaxIterations)
	}
	if cfg.CallModel == nil {
		t.Fatalf("expected CallModel function")
	}
}

func TestExecuteRunDryRun(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	agentsDir := writeTestAgent(t, "go-tests")
	opts := &RunOptions{
		Agent:     "go-tests",
		AgentsDir: agentsDir,
		DryRun:    true,
	}
	if err := ExecuteRun(cmd, []string{"hello"}, opts); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
}

func TestExecuteRunPromptError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(""))
	agentsDir := writeTestAgent(t, "go-tests")
	opts := &RunOptions{Agent: "go-tests", AgentsDir: agentsDir, ConfigAvailable: false}
	err := ExecuteRun(cmd, nil, opts)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "config not available") {
		t.Fatalf("ExecuteRun() error = %q, want to contain %q", err.Error(), "config not available")
	}
}

func TestExecuteRunInvokeModelError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	agentsDir := writeTestAgent(t, "go-tests")
	opts := &RunOptions{
		Agent:           "go-tests",
		AgentsDir:       agentsDir,
		Provider:        "openai-responses",
		Model:           "gpt-5",
		MaxIterations:   1,
		ConfigAvailable: true,
	}
	err := ExecuteRun(cmd, []string{"hello"}, opts)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "API key required") {
		t.Fatalf("ExecuteRun() error = %q, want to contain %q", err.Error(), "API key required")
	}
}

// TestExecuteRunRemoteOnlyDisablesRequireActionable verifies that when a
// remote-only agent is run with --require-actionable, the flag is cleared
// (remote-only agents never edit files, so the guard is meaningless).
func TestExecuteRunRemoteOnlyDisablesRequireActionable(t *testing.T) {
	// Stand up a minimal Ollama-compatible server so ExecuteRun reaches the
	// post-bundle branch where RequireActionable is reset.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"mistral","message":{"role":"assistant","content":"ok"},"done":true}`))
	}))
	defer server.Close()

	tmp := t.TempDir()
	agentsDir := filepath.Join(tmp, "agents")
	agentDir := filepath.Join(agentsDir, "remote")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := "name: remote\nversion: 1\nworking_dir: none\nprompt: hi\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	opts := &RunOptions{
		Agent:             "remote",
		AgentsDir:         agentsDir,
		Provider:          "ollama",
		Model:             "mistral",
		BaseURL:           server.URL,
		MaxIterations:     1,
		ConfigAvailable:   true,
		RequireActionable: true,
	}
	if err := ExecuteRun(cmd, []string{"hello"}, opts); err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
	if opts.RequireActionable {
		t.Fatal("expected RequireActionable to be cleared for remote-only agent")
	}
}

func TestExecuteRunSuccessOllama(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"model":"mistral","message":{"role":"assistant","content":"ok"},"done":true}`)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	agentsDir := writeTestAgent(t, "go-tests")
	opts := &RunOptions{
		Agent:           "go-tests",
		AgentsDir:       agentsDir,
		Provider:        "ollama",
		Model:           "mistral",
		BaseURL:         server.URL,
		MaxIterations:   1,
		ConfigAvailable: true,
	}
	if err := ExecuteRun(cmd, []string{"hello"}, opts); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if !strings.Contains(buf.String(), "ok") {
		t.Fatalf("expected response output, got %q", buf.String())
	}
}

func TestPrintMetrics(t *testing.T) {
	t.Parallel()
	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errBuf)

	m := metrics.New("openai", "gpt-4o")
	m.AddTokens(1000, 500)
	m.IncrementIterations()
	m.Finish()

	if err := printMetrics(cmd, m); err != nil {
		t.Fatalf("printMetrics() returned error: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "Agent Metrics") {
		t.Fatalf("printMetrics() missing header in output: %q", output)
	}
	if !strings.Contains(output, "gpt-4o") {
		t.Fatalf("printMetrics() missing model in output: %q", output)
	}
	if !strings.Contains(output, "openai") {
		t.Fatalf("printMetrics() missing provider in output: %q", output)
	}
	if !strings.Contains(output, "1000 input") {
		t.Fatalf("printMetrics() missing input tokens in output: %q", output)
	}
	if !strings.Contains(output, "500 output") {
		t.Fatalf("printMetrics() missing output tokens in output: %q", output)
	}
}

func TestPrintMetricsNil(t *testing.T) {
	t.Parallel()
	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errBuf)

	if err := printMetrics(cmd, nil); err != nil {
		t.Fatalf("printMetrics(nil) returned error: %v", err)
	}

	if errBuf.Len() != 0 {
		t.Fatalf("printMetrics(nil) should not output anything, got: %q", errBuf.String())
	}
}

func TestFindAgentDir(t *testing.T) {
	t.Parallel()

	t.Run("explicit dir", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		got, err := FindAgentDir("myagent", tmp, nil)
		if err != nil {
			t.Fatalf("FindAgentDir() error = %v", err)
		}
		want := filepath.Join(tmp, "myagent")
		if got != want {
			t.Fatalf("FindAgentDir() = %q, want %q", got, want)
		}
	})

	t.Run("nil config returns error", func(t *testing.T) {
		t.Parallel()
		_, err := FindAgentDir("myagent", "", nil)
		if err == nil {
			t.Fatal("FindAgentDir() expected error with nil config, got nil")
		}
	})

	t.Run("config with no sources returns error", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		_, err := FindAgentDir("myagent", "", cfg)
		if err == nil {
			t.Fatal("FindAgentDir() expected error with empty config, got nil")
		}
	})
}

func TestLogRunHistory(t *testing.T) {
	t.Run("nil metrics", func(t *testing.T) {
		t.Parallel()
		opts := &RunOptions{Agent: "test"}
		// Should not panic.
		logRunHistory(opts, nil)
	})

	t.Run("empty agent", func(t *testing.T) {
		t.Parallel()
		m := metrics.New("openai", "gpt-4o")
		opts := &RunOptions{Agent: ""}
		// Should return early without panic.
		logRunHistory(opts, m)
	})

	t.Run("valid run", func(t *testing.T) {
		// Cannot use t.Parallel() here because t.Setenv is used.
		tmp := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", tmp)

		m := metrics.New("openai", "gpt-4o")
		m.AddTokens(100, 50)
		m.Finish()

		opts := &RunOptions{Agent: "test-agent"}
		logRunHistory(opts, m)
		// No error means it ran the logging path successfully.
	})
}

func TestWriteResponsePrintAndFile(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	outFile := filepath.Join(t.TempDir(), "out.txt")
	opts := &RunOptions{Print: true, Output: outFile}
	if err := writeResponse(cmd, "both", opts); err != nil {
		t.Fatalf("writeResponse() error = %v", err)
	}
	if !strings.Contains(buf.String(), "both") {
		t.Fatalf("expected stdout to contain response")
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "both" {
		t.Fatalf("file content = %q, want %q", string(data), "both")
	}
}

func writeTestAgent(t *testing.T, agentName string) string {
	t.Helper()
	return writeTestAgentNoModels(t, agentName)
}

func writeTestAgentNoModels(t *testing.T, agentName string) string {
	t.Helper()
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := fmt.Sprintf("name: %s\nversion: 0.0.0\nentrypoint: system.md\nwrapper: wrapper.md\n", agentName)
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile agent.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("System prompt."), 0o644); err != nil {
		t.Fatalf("WriteFile system.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "wrapper.md"), []byte("Wrapper prompt."), 0o644); err != nil {
		t.Fatalf("WriteFile wrapper.md: %v", err)
	}
	return agentsDir
}

func writeTestAgentWithModels(t *testing.T, agentName, model, provider string) string {
	t.Helper()
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := fmt.Sprintf("name: %s\nversion: 0.0.0\nentrypoint: system.md\nwrapper: wrapper.md\nmodels:\n  - model: %s\n    provider: %s\n", agentName, model, provider)
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile agent.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("System prompt."), 0o644); err != nil {
		t.Fatalf("WriteFile system.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "wrapper.md"), []byte("Wrapper prompt."), 0o644); err != nil {
		t.Fatalf("WriteFile wrapper.md: %v", err)
	}
	return agentsDir
}

func writeTestAgentWithMultiModels(t *testing.T, agentName string, models []struct{ model, provider string }) string {
	t.Helper()
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	var sb strings.Builder
	for _, m := range models {
		fmt.Fprintf(&sb, "  - model: %s\n    provider: %s\n", m.model, m.provider)
	}
	modelsYAML := sb.String()
	manifest := fmt.Sprintf("name: %s\nversion: 0.0.0\nentrypoint: system.md\nwrapper: wrapper.md\nmodels:\n%s", agentName, modelsYAML)
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile agent.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("System prompt."), 0o644); err != nil {
		t.Fatalf("WriteFile system.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "wrapper.md"), []byte("Wrapper prompt."), 0o644); err != nil {
		t.Fatalf("WriteFile wrapper.md: %v", err)
	}
	return agentsDir
}

// writeTestAgentWithIsolation creates an agent fixture whose manifest
// declares the given isolation mode.
func writeTestAgentWithIsolation(t *testing.T, agentName, isolation string) string {
	t.Helper()
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := fmt.Sprintf(
		"name: %s\nversion: 0.0.0\nentrypoint: system.md\n"+
			"wrapper: wrapper.md\nisolation: %s\n",
		agentName, isolation,
	)
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile agent.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("System prompt."), 0o644); err != nil {
		t.Fatalf("WriteFile system.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "wrapper.md"), []byte("Wrapper prompt."), 0o644); err != nil {
		t.Fatalf("WriteFile wrapper.md: %v", err)
	}
	return agentsDir
}

// TestHandleBudgetExceeded verifies the three branches inside
// handleBudgetExceeded: silent no-op, warning-only, and partial apply.
func TestHandleBudgetExceeded(t *testing.T) {
	t.Parallel()

	t.Run("non-budget error is a no-op", func(t *testing.T) {
		t.Parallel()
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		m := metrics.New("openai", "gpt-4o")
		m.Finish()
		handleBudgetExceeded(
			cmd, &RunOptions{}, m, "", t.TempDir(),
			errors.New("some other error"),
		)
		if errBuf.Len() != 0 {
			t.Fatalf(
				"expected no output for non-budget error, got %q",
				errBuf.String(),
			)
		}
	})

	t.Run("budget exceeded with empty response writes warning only", func(t *testing.T) {
		t.Parallel()
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		m := metrics.New("openai", "gpt-4o")
		m.SetMaxCost(0.001)
		m.Finish()
		handleBudgetExceeded(
			cmd, &RunOptions{MaxCost: 0.001}, m, "", t.TempDir(),
			metrics.ErrBudgetExceeded,
		)
		if !strings.Contains(errBuf.String(), "cost budget") {
			t.Fatalf("expected budget warning, got %q", errBuf.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("expected no stdout for empty response, got %q", stdout.String())
		}
	})

	t.Run("budget exceeded with response applies partial output", func(t *testing.T) {
		t.Parallel()
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		m := metrics.New("openai", "gpt-4o")
		m.Finish()
		opts := &RunOptions{MaxCost: 0.001, Print: true}
		handleBudgetExceeded(
			cmd, opts, m, "partial response", t.TempDir(),
			metrics.ErrBudgetExceeded,
		)
		if !strings.Contains(stdout.String(), "partial response") {
			t.Fatalf(
				"expected partial response in stdout, got %q",
				stdout.String(),
			)
		}
	})
}

// TestManifestIsolation verifies that manifestIsolation returns the isolation
// field from an agent manifest, or empty string on any failure.
func TestManifestIsolation(t *testing.T) {
	t.Parallel()

	t.Run("empty agent name returns empty", func(t *testing.T) {
		t.Parallel()
		if got := manifestIsolation(&RunOptions{}); got != "" {
			t.Fatalf("expected empty for empty agent, got %q", got)
		}
	})

	t.Run("missing agent directory returns empty", func(t *testing.T) {
		t.Parallel()
		opts := &RunOptions{Agent: "nonexistent", AgentsDir: t.TempDir()}
		if got := manifestIsolation(opts); got != "" {
			t.Fatalf("expected empty for missing agent, got %q", got)
		}
	})

	t.Run("manifest with isolation field returns it", func(t *testing.T) {
		t.Parallel()
		agentsDir := writeTestAgentWithIsolation(t, "iso-agent", "worktree")
		opts := &RunOptions{Agent: "iso-agent", AgentsDir: agentsDir}
		if got := manifestIsolation(opts); got != "worktree" {
			t.Fatalf("manifestIsolation() = %q, want worktree", got)
		}
	})

	t.Run("manifest without isolation field returns empty", func(t *testing.T) {
		t.Parallel()
		agentsDir := writeTestAgentNoModels(t, "no-iso-agent")
		opts := &RunOptions{Agent: "no-iso-agent", AgentsDir: agentsDir}
		if got := manifestIsolation(opts); got != "" {
			t.Fatalf("manifestIsolation() = %q, want empty", got)
		}
	})
}
