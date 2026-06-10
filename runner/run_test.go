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
	"github.com/cowdogmoo/squad/tools"
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

// TestPrepareBundle_RequiresPreflightFails wires an agent with a requires
// block that points at a tool guaranteed to be absent from PATH and
// confirms prepareBundle surfaces the preflight error rather than building
// a runnable bundle.
func TestPrepareBundle_RequiresPreflightFails(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	agentName := "needs-missing-tool"
	agentDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := fmt.Sprintf(`name: %s
version: 0.0.0
entrypoint: system.md
wrapper: wrapper.md
requires:
  commands:
    - name: definitely-not-a-real-binary-xyz123
      install:
        brew: fake-tool
`, agentName)
	for _, f := range []struct{ name, body string }{
		{"agent.yaml", manifest},
		{"system.md", "System prompt."},
		{"wrapper.md", "Wrapper prompt."},
	} {
		if err := os.WriteFile(filepath.Join(agentDir, f.name), []byte(f.body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", f.name, err)
		}
	}

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	opts := &RunOptions{
		Agent:           agentName,
		AgentsDir:       agentsDir,
		ConfigAvailable: true,
	}
	bundle, err := prepareBundle(cmd, opts, "prompt", t.TempDir())
	if err == nil {
		t.Fatal("expected prepareBundle to fail when a required tool is missing")
	}
	if bundle != nil {
		t.Fatal("expected nil bundle on preflight failure")
	}
	if !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("expected preflight error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-binary-xyz123") {
		t.Fatalf("expected missing-tool name in error, got: %v", err)
	}
}

func TestResolveModelPrecedenceNilGuard(t *testing.T) {
	t.Parallel()
	// Calling with either argument nil must be a silent no-op rather than a
	// panic; pipeline code passes a bundle that may be nil on early-exit
	// paths. The function must not emit a warning or an error in any of
	// these shapes.
	nilCases := []struct {
		name   string
		opts   *RunOptions
		bundle *agent.Bundle
	}{
		{"both nil", nil, nil},
		{"nil bundle", &RunOptions{}, nil},
		{"nil opts", nil, &agent.Bundle{}},
	}
	for _, tc := range nilCases {
		warn, err := ResolveModelPrecedence(context.Background(), tc.opts, tc.bundle)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if warn != "" {
			t.Fatalf("%s: unexpected warning: %q", tc.name, warn)
		}
	}

	opts := &RunOptions{Model: "preset", Provider: "preset"}
	warn, err := ResolveModelPrecedence(context.Background(), opts, &agent.Bundle{Model: "manifest", Provider: "manifest"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if warn != "" {
		t.Fatalf("unexpected warning: %q", warn)
	}
	if opts.Model != "preset" || opts.Provider != "preset" {
		t.Fatalf("explicit values must win over manifest: Model=%q Provider=%q", opts.Model, opts.Provider)
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
		if _, err := ResolveModelPrecedence(context.Background(), opts, bundle); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.BaseURL != compatURL {
			t.Fatalf("BaseURL = %q, want %q", opts.BaseURL, compatURL)
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
		if _, err := ResolveModelPrecedence(context.Background(), opts, bundle); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.BaseURL != explicit {
			t.Fatalf("BaseURL = %q, want explicit %q", opts.BaseURL, explicit)
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
		if _, err := ResolveModelPrecedence(context.Background(), opts, bundle); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.BaseURL != compatURL {
			t.Fatalf("BaseURL = %q, want %q", opts.BaseURL, compatURL)
		}
		if opts.Provider != "openai-compat" {
			t.Fatalf("Provider = %q, want openai-compat", opts.Provider)
		}
	})
}

// The TestResolveModelPrecedence_* family exercises the credential-aware
// resolver: a missing API key on the manifest's top-ranked entry should make
// the resolver walk down the list, surfacing a warning. When nothing has
// credentials, the resolver returns an error naming the env vars that would
// unblock the run. These tests mutate process env vars and therefore do not
// run in parallel.

func TestResolveModelPrecedence_WalksPastUncredentialedTop(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "gemini-key")
	t.Setenv("OPENAI_API_KEY", "")
	opts := &RunOptions{}
	bundle := &agent.Bundle{
		Model:    "claude-sonnet-4-6",
		Provider: "anthropic",
		Models: []agent.ModelPreference{
			{Model: "claude-sonnet-4-6", Provider: "anthropic"},
			{Model: "gemini-2.5-flash", Provider: "gemini"},
			{Model: "gpt-4.1-mini", Provider: "openai"},
		},
	}
	warn, err := ResolveModelPrecedence(context.Background(), opts, bundle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Model != "gemini-2.5-flash" || opts.Provider != "gemini" {
		t.Fatalf("expected gemini-2.5-flash/gemini, got %q/%q", opts.Model, opts.Provider)
	}
	if !strings.Contains(warn, "ANTHROPIC_API_KEY") {
		t.Fatalf("expected warning to name ANTHROPIC_API_KEY, got %q", warn)
	}
}

func TestResolveModelPrecedence_ErrorsWhenNoManifestProviderHasCreds(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	opts := &RunOptions{}
	bundle := &agent.Bundle{
		Model:    "claude-sonnet-4-6",
		Provider: "anthropic",
		Models: []agent.ModelPreference{
			{Model: "claude-sonnet-4-6", Provider: "anthropic"},
			{Model: "gpt-4.1-mini", Provider: "openai"},
		},
	}
	_, err := ResolveModelPrecedence(context.Background(), opts, bundle)
	if err == nil {
		t.Fatal("expected error when no provider has credentials")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ANTHROPIC_API_KEY") || !strings.Contains(msg, "OPENAI_API_KEY") {
		t.Fatalf("expected error to list missing env vars, got %q", msg)
	}
}

func TestResolveModelPrecedence_ExplicitModelFillsFromManifestMatch(t *testing.T) {
	opts := &RunOptions{Model: "gemini-2.5-flash"}
	bundle := &agent.Bundle{
		Models: []agent.ModelPreference{
			{Model: "claude-sonnet-4-6", Provider: "anthropic"},
			{Model: "gemini-2.5-flash", Provider: "gemini", BaseURL: "https://gemini.example/v1"},
		},
	}
	warn, err := ResolveModelPrecedence(context.Background(), opts, bundle)
	if err != nil || warn != "" {
		t.Fatalf("unexpected warn=%q err=%v", warn, err)
	}
	if opts.Provider != "gemini" {
		t.Fatalf("Provider = %q, want gemini", opts.Provider)
	}
	if opts.BaseURL != "https://gemini.example/v1" {
		t.Fatalf("BaseURL = %q, want propagated from manifest match", opts.BaseURL)
	}
}

// Bundle has no matching Models entry — Provider should come from
// bundle.Provider, and only fall back to ConfigProvider when the bundle has
// nothing either.
func TestResolveModelPrecedence_ExplicitModelFallsBackToBundleProvider(t *testing.T) {
	opts := &RunOptions{Model: "custom-model", ConfigProvider: "fallback-config"}
	bundle := &agent.Bundle{Provider: "bundle-primary", BaseURL: "https://bundle.example/v1"}
	warn, err := ResolveModelPrecedence(context.Background(), opts, bundle)
	if err != nil || warn != "" {
		t.Fatalf("unexpected warn=%q err=%v", warn, err)
	}
	if opts.Provider != "bundle-primary" {
		t.Fatalf("Provider = %q, want bundle-primary (bundle.Provider wins over config)", opts.Provider)
	}
	if opts.BaseURL != "https://bundle.example/v1" {
		t.Fatalf("BaseURL = %q, want propagated from bundle.BaseURL", opts.BaseURL)
	}
}

func TestResolveModelPrecedence_ExplicitModelFallsBackToConfigProvider(t *testing.T) {
	opts := &RunOptions{Model: "custom-model", ConfigProvider: "fallback-config"}
	bundle := &agent.Bundle{} // empty bundle
	if _, err := ResolveModelPrecedence(context.Background(), opts, bundle); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Provider != "fallback-config" {
		t.Fatalf("Provider = %q, want fallback-config (config last-resort)", opts.Provider)
	}
}

// Config default with creds, model not in manifest, bundle has a primary
// BaseURL — fillBaseURL should propagate that BaseURL.
func TestResolveModelPrecedence_ConfigOutsideManifestInheritsBundleBaseURL(t *testing.T) {
	t.Setenv("OPENAI_COMPAT_API_KEY", "compat-token")
	opts := &RunOptions{
		ConfigProvider: "openai-compat",
		ConfigModel:    "outside-manifest-model",
	}
	bundle := &agent.Bundle{
		BaseURL: "https://primary.example/v1",
		Models: []agent.ModelPreference{
			{Model: "claude-sonnet-4-6", Provider: "anthropic"},
		},
	}
	if _, err := ResolveModelPrecedence(context.Background(), opts, bundle); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.BaseURL != "https://primary.example/v1" {
		t.Fatalf("BaseURL = %q, want bundle primary fallback", opts.BaseURL)
	}
}

// Picked manifest entry has no BaseURL of its own — bundle.BaseURL should
// fill it in.
func TestResolveModelPrecedence_ManifestWalkInheritsBundleBaseURL(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	opts := &RunOptions{}
	bundle := &agent.Bundle{
		BaseURL: "https://primary.example/v1",
		Models: []agent.ModelPreference{
			{Model: "gpt-4.1-mini", Provider: "openai"},
		},
	}
	if _, err := ResolveModelPrecedence(context.Background(), opts, bundle); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.BaseURL != "https://primary.example/v1" {
		t.Fatalf("BaseURL = %q, want primary fallback", opts.BaseURL)
	}
}

// Two manifest entries share OPENAI_API_KEY; with no key set, the error
// message must list it exactly once.
func TestResolveModelPrecedence_NoCredsErrorDedupesEnvVars(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	opts := &RunOptions{}
	bundle := &agent.Bundle{
		Models: []agent.ModelPreference{
			{Model: "gpt-4.1-mini", Provider: "openai"},
			{Model: "gpt-4.1-nano", Provider: "openai"},
			{Model: "claude-sonnet-4-6", Provider: "anthropic"},
		},
	}
	_, err := ResolveModelPrecedence(context.Background(), opts, bundle)
	if err == nil {
		t.Fatal("expected error when no provider has credentials")
	}
	msg := err.Error()
	if strings.Count(msg, "OPENAI_API_KEY") != 1 {
		t.Fatalf("expected OPENAI_API_KEY listed exactly once, got %q", msg)
	}
}

// The user's primary use case: openai-compat with a custom hosted model
// (e.g. Qwen via Together AI). The model is not in the agent manifest, but
// the user has expressed explicit intent in config and supplied a token.
// Config wins and the resolver emits a warning.
func TestResolveModelPrecedence_ConfigTokenWinsOverManifestTop(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
	opts := &RunOptions{
		APIKey:         "compat-token",
		ConfigProvider: "openai-compat",
		ConfigModel:    "Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo",
		BaseURL:        "https://api.together.xyz/v1",
	}
	bundle := &agent.Bundle{
		Model:    "claude-sonnet-4-6",
		Provider: "anthropic",
		Models: []agent.ModelPreference{
			{Model: "claude-sonnet-4-6", Provider: "anthropic"},
		},
	}
	warn, err := ResolveModelPrecedence(context.Background(), opts, bundle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Model != "Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo" || opts.Provider != "openai-compat" {
		t.Fatalf("expected config default to win, got %q (%q)", opts.Model, opts.Provider)
	}
	if !strings.Contains(warn, "not listed in the agent manifest") {
		t.Fatalf("expected outside-manifest warning, got %q", warn)
	}
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

	t.Run("config default wins over manifest when it has credentials", func(t *testing.T) {
		t.Parallel()
		// Config default expresses explicit user intent (where keys, cost
		// ceilings, and provider routing actually live), so it beats the
		// manifest's preferred model. The unknown "config-provider" is
		// treated as not-missing by KeyStatus, satisfying the creds gate.
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

	t.Run("config default outside manifest proceeds with warning", func(t *testing.T) {
		t.Parallel()
		// Config default is outside the manifest's models: list. The user's
		// explicit intent wins — we proceed with the config model and warn
		// rather than overriding the user's choice with the manifest.
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
			t.Fatalf("expected config default to win, got Model=%q Provider=%q", opts.Model, opts.Provider)
		}
		if !strings.Contains(stderr.String(), "not listed in the agent manifest") {
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

func TestHandleBudgetExceeded_NonBudgetError(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	opts := &RunOptions{MaxCost: 1.0}
	m := &metrics.Metrics{}
	// A non-budget error should be a no-op.
	handleBudgetExceeded(cmd, opts, m, "", "", fmt.Errorf("some other error"))
	if buf.Len() != 0 {
		t.Errorf("expected no output for non-budget error, got %q", buf.String())
	}
}

func TestHandleBudgetExceeded_BudgetError(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	opts := &RunOptions{MaxCost: 0.5}
	m := &metrics.Metrics{}
	handleBudgetExceeded(cmd, opts, m, "", "", metrics.ErrBudgetExceeded)
	if !strings.Contains(buf.String(), "cost budget") {
		t.Errorf("expected budget message, got %q", buf.String())
	}
}

func TestApplyChildIterationCap_ManifestCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	childOpts := &RunOptions{}
	cfg := &tools.TaskConfig{}
	childBundle := &agent.Bundle{MaxIterations: 10}
	applyChildIterationCap(ctx, childOpts, cfg, childBundle, "my-agent")
	if childOpts.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d, want 10", childOpts.MaxIterations)
	}
}

func TestApplyChildIterationCap_CallbackOverridesManifest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	childOpts := &RunOptions{}
	cfg := &tools.TaskConfig{
		ChildMaxIter: func(name string) int { return 5 },
	}
	childBundle := &agent.Bundle{MaxIterations: 10}
	applyChildIterationCap(ctx, childOpts, cfg, childBundle, "my-agent")
	// Callback returns 5, manifest says 10; manifest wins only if smaller.
	// 5 < 10, so manifest would override to 10 — but callback set 5 first,
	// then manifest check: 10 > 5, so no override.
	if childOpts.MaxIterations != 5 {
		t.Errorf("MaxIterations = %d, want 5", childOpts.MaxIterations)
	}
}

func TestReadPrompt_FromArgs(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	got, err := readPrompt(cmd, []string{"hello", "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestReadPrompt_NoArgsNoStdin(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	got, err := readPrompt(cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestReportIsolationTeardown_Nil(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	// Should be a no-op and not panic.
	reportIsolationTeardown(cmd, nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for nil isolation, got %q", buf.String())
	}
}

func TestManifestIsolation_NoAgent(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{Agent: ""}
	got := manifestIsolation(opts)
	if got != "" {
		t.Errorf("expected empty isolation for empty agent, got %q", got)
	}
}

func TestManifestIsolation_AgentNotFound(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{
		Agent:     "nonexistent-agent-xyz",
		AgentsDir: t.TempDir(),
	}
	got := manifestIsolation(opts)
	if got != "" {
		t.Errorf("expected empty isolation when agent not found, got %q", got)
	}
}

func TestHandleBudgetExceeded_BudgetErrorWithResponse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cmd := &cobra.Command{}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	opts := &RunOptions{
		MaxCost:    0.5,
		WorkingDir: dir,
		Output:     "",
	}
	m := &metrics.Metrics{}
	// Passing a non-empty response exercises the handleResponse branch.
	handleBudgetExceeded(cmd, opts, m, "partial response", dir, metrics.ErrBudgetExceeded)
	if !strings.Contains(errBuf.String(), "cost budget") {
		t.Errorf("expected budget message in stderr, got %q", errBuf.String())
	}
}

func TestFindAgentDir_ExplicitDir(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	agentName := "my-agent"
	got, err := FindAgentDir(agentName, agentsDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(agentsDir, agentName)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFindAgentDir_NilConfigNoDir(t *testing.T) {
	t.Parallel()
	_, err := FindAgentDir("some-agent", "", nil)
	if err == nil {
		t.Fatal("expected error when no config and no explicit dir")
	}
	if !strings.Contains(err.Error(), "no config") {
		t.Errorf("error %q should mention 'no config'", err.Error())
	}
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
