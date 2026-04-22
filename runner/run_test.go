package runner

import (
	"bytes"
	"context"
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
	tests := []struct {
		name    string
		dir     string
		wantCwd bool
		wantErr bool
	}{
		{"empty returns cwd", "", true, false},
		{"explicit path", "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.wantCwd {
				cwd, _ := os.Getwd()
				resolved, err := resolveWorkingDir("")
				if err != nil {
					t.Fatalf("resolveWorkingDir() error = %v", err)
				}
				if resolved != cwd {
					t.Fatalf("resolveWorkingDir() = %q, want cwd %q", resolved, cwd)
				}
			} else {
				tmp := t.TempDir()
				resolved, err := resolveWorkingDir(tmp)
				if err != nil {
					t.Fatalf("resolveWorkingDir() error = %v", err)
				}
				if resolved != tmp {
					t.Fatalf("resolveWorkingDir() = %q, want %q", resolved, tmp)
				}
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

func TestExecuteRunSuccessOllama(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"mistral","message":{"role":"assistant","content":"ok"},"done":true}`))
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
		got, err := findAgentDir("myagent", tmp, nil)
		if err != nil {
			t.Fatalf("findAgentDir() error = %v", err)
		}
		want := filepath.Join(tmp, "myagent")
		if got != want {
			t.Fatalf("findAgentDir() = %q, want %q", got, want)
		}
	})

	t.Run("nil config returns error", func(t *testing.T) {
		t.Parallel()
		_, err := findAgentDir("myagent", "", nil)
		if err == nil {
			t.Fatal("findAgentDir() expected error with nil config, got nil")
		}
	})

	t.Run("config with no sources returns error", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		_, err := findAgentDir("myagent", "", cfg)
		if err == nil {
			t.Fatal("findAgentDir() expected error with empty config, got nil")
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
