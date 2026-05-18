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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/runner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// setupTestAgent creates a temporary agent directory with the given manifest
// YAML and supporting files. Returns the agents dir path.
func setupTestAgent(t *testing.T, name, manifestYAML string, files map[string]string) string {
	t.Helper()
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, name)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	for relPath, content := range files {
		full := filepath.Join(agentDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return agentsDir
}

func TestBuildAgentBundle_DefaultMode(t *testing.T) {
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
references:
  - refs/criteria.md
`
	// Use Go text/template conditionals for mode-specific content
	files := map[string]string{
		"system.md": `common system content
{{if eq .Mode "edit"}}
edit mode system content
{{end}}
{{if eq .Mode "readonly"}}
readonly system content
{{end}}`,
		"agent.md": `common wrapper content
{{if eq .Mode "edit"}}
edit mode wrapper
{{end}}
{{if eq .Mode "readonly"}}
readonly wrapper
{{end}}`,
		"refs/criteria.md": "review criteria content",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "review this", "/tmp", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Default mode is "edit"
	if !strings.Contains(bundle.System, "edit mode system content") {
		t.Error("expected edit mode system content in bundle")
	}
	if !strings.Contains(bundle.System, "edit mode wrapper") {
		t.Error("expected edit mode wrapper in bundle")
	}
	if strings.Contains(bundle.System, "readonly") {
		t.Error("did not expect readonly content in default/edit mode")
	}
	if !strings.Contains(bundle.System, "review criteria content") {
		t.Error("expected references in bundle")
	}
	if !strings.Contains(string(bundle.Combined), "review this") {
		t.Error("expected user prompt in combined bundle")
	}
	if !strings.Contains(bundle.System, "Mode: edit") {
		t.Error("expected Mode: edit header in bundle")
	}
}

func TestBuildAgentBundle_ReadonlyMode(t *testing.T) {
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
references:
  - refs/criteria.md
`
	// Use Go text/template conditionals for mode-specific content
	files := map[string]string{
		"system.md": `common system content
{{if eq .Mode "edit"}}
edit mode system content
{{end}}
{{if eq .Mode "readonly"}}
readonly system content
{{end}}`,
		"agent.md": `common wrapper content
{{if eq .Mode "edit"}}
edit mode wrapper
{{end}}
{{if eq .Mode "readonly"}}
readonly wrapper
{{end}}`,
		"refs/criteria.md": "review criteria content",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "review this", "/tmp", "readonly", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(bundle.System, "readonly system content") {
		t.Error("expected readonly system content in bundle")
	}
	if !strings.Contains(bundle.System, "readonly wrapper") {
		t.Error("expected readonly wrapper in bundle")
	}
	if strings.Contains(bundle.System, "edit mode") {
		t.Error("did not expect edit mode content in readonly mode")
	}
	if !strings.Contains(bundle.System, "review criteria content") {
		t.Error("expected references in bundle")
	}
	if !strings.Contains(bundle.System, "Mode: readonly") {
		t.Error("expected Mode: readonly header in bundle")
	}
}

func TestBuildAgentBundle_CommonContentPreserved(t *testing.T) {
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
`
	// Content without conditionals should be preserved
	files := map[string]string{
		"system.md": `This is common content that appears in all modes.
{{if eq .Mode "edit"}}
This is edit-only content.
{{end}}
{{if eq .Mode "readonly"}}
This is readonly-only content.
{{end}}
This is also common content.`,
		"agent.md": "common wrapper",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "test", "/tmp", "readonly", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(bundle.System, "This is common content that appears in all modes") {
		t.Error("expected common content at beginning")
	}
	if !strings.Contains(bundle.System, "This is also common content") {
		t.Error("expected common content at end")
	}
	if !strings.Contains(bundle.System, "readonly-only content") {
		t.Error("expected readonly content")
	}
	if strings.Contains(bundle.System, "edit-only content") {
		t.Error("did not expect edit content in readonly mode")
	}
}

func TestBuildAgentBundle_NoConditionalBlocks(t *testing.T) {
	// Agents without conditional blocks should work with any mode
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
`
	files := map[string]string{
		"system.md": "plain system content without conditionals",
		"agent.md":  "plain wrapper without conditionals",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	// Test with readonly mode on agent without conditional blocks
	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "test", "/tmp", "readonly", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(bundle.System, "plain system content") {
		t.Error("expected plain system content")
	}
	if !strings.Contains(bundle.System, "plain wrapper") {
		t.Error("expected plain wrapper")
	}
	if !strings.Contains(bundle.System, "Mode: readonly") {
		t.Error("expected Mode: readonly header even without conditionals")
	}
}

func TestBuildAgentBundle_CustomMode(t *testing.T) {
	// Test that custom modes work with conditional blocks
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
`
	files := map[string]string{
		"system.md": `common content
{{if eq .Mode "custom"}}
custom mode content
{{end}}
{{if eq .Mode "edit"}}
edit mode content
{{end}}`,
		"agent.md": "wrapper",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "test", "/tmp", "custom", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(bundle.System, "custom mode content") {
		t.Error("expected custom mode content")
	}
	if strings.Contains(bundle.System, "edit mode content") {
		t.Error("did not expect edit mode content in custom mode")
	}
	if !strings.Contains(bundle.System, "Mode: custom") {
		t.Error("expected Mode: custom header")
	}
}

func commandWithViper(v *viper.Viper) *cobra.Command {
	cmd := &cobra.Command{}
	ctx := withViper(context.Background(), v)
	cmd.SetContext(ctx)
	return cmd
}

func TestViperContext(t *testing.T) {
	tests := []struct {
		name      string
		cmd       *cobra.Command
		wantPanic bool
	}{
		{
			name:      "with viper",
			cmd:       func() *cobra.Command { return commandWithViper(viper.New()) }(),
			wantPanic: false,
		},
		{
			name:      "missing viper",
			cmd:       &cobra.Command{},
			wantPanic: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if tt.wantPanic && recovered == nil {
					t.Fatalf("expected panic")
				}
				if !tt.wantPanic && recovered != nil {
					t.Fatalf("unexpected panic: %v", recovered)
				}
			}()
			got := viperFromContext(tt.cmd)
			if !tt.wantPanic && got == nil {
				t.Fatalf("expected viper instance")
			}
		})
	}
}

func TestNewRunOptions(t *testing.T) {
	tests := []struct {
		name        string
		maxIter     int
		wantIter    int
		wantAgent   string
		wantMode    string
		wantMaxTok  int
		wantTemp    float64
		wantHasConf bool
	}{
		{
			name:        "min iterations",
			maxIter:     3,
			wantIter:    10,
			wantAgent:   "go-tests",
			wantMode:    "readonly",
			wantMaxTok:  512,
			wantTemp:    0.9,
			wantHasConf: true,
		},
		{
			name:        "max iterations",
			maxIter:     1200,
			wantIter:    1000,
			wantAgent:   "go-tests",
			wantMode:    "readonly",
			wantMaxTok:  512,
			wantTemp:    0.9,
			wantHasConf: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			v.Set("run.agent", "go-tests")
			v.Set("run.mode", "readonly")
			v.Set("run.max_iterations", tt.maxIter)
			v.Set("model.max_tokens", 512)
			v.Set("model.temperature", 0.9)
			v.Set("provider.token", "token")
			v.Set("provider.base_url", "http://example.com")
			v.Set("provider.organization", "org")
			v.Set("provider.api_version", "v1")
			v.Set("provider.api_type", "openai")
			v.Set("provider.openai_compat_max_tokens", true)
			v.Set("provider.num_ctx", 2048)

			cmd := commandWithViper(v)
			ctx := withConfig(cmd.Context(), config.Defaults())
			cmd.SetContext(ctx)

			opts := newRunOptions(cmd)
			if opts.MaxIterations != tt.wantIter {
				t.Fatalf("MaxIterations = %d, want %d", opts.MaxIterations, tt.wantIter)
			}
			if opts.Agent != tt.wantAgent {
				t.Fatalf("Agent = %q, want %q", opts.Agent, tt.wantAgent)
			}
			if opts.Mode != tt.wantMode {
				t.Fatalf("Mode = %q, want %q", opts.Mode, tt.wantMode)
			}
			if opts.MaxTokens != tt.wantMaxTok {
				t.Fatalf("MaxTokens = %d, want %d", opts.MaxTokens, tt.wantMaxTok)
			}
			if opts.Temperature != tt.wantTemp {
				t.Fatalf("Temperature = %v, want %v", opts.Temperature, tt.wantTemp)
			}
			if opts.ConfigAvailable != tt.wantHasConf {
				t.Fatalf("ConfigAvailable = %v, want %v", opts.ConfigAvailable, tt.wantHasConf)
			}
		})
	}
}

func TestBindRunFlags(t *testing.T) {
	cmd := newRunCmd()
	v := viper.New()
	if err := bindRunFlags(cmd, v); err != nil {
		t.Fatalf("bindRunFlags() error = %v", err)
	}
	if err := cmd.Flags().Set("agent", "go-tests"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("provider", "openai"); err != nil {
		t.Fatalf("Set provider: %v", err)
	}
	if got := v.GetString("run.agent"); got != "go-tests" {
		t.Fatalf("run.agent = %q, want go-tests", got)
	}
	if got := v.GetString("provider.default"); got != "openai" {
		t.Fatalf("provider.default = %q, want openai", got)
	}
}

func TestNewRunOptionsProviderModelPrecedence(t *testing.T) {
	t.Run("changed flags route to explicit fields", func(t *testing.T) {
		cmd := newRunCmd()
		v := viper.New()
		if err := bindRunFlags(cmd, v); err != nil {
			t.Fatalf("bindRunFlags: %v", err)
		}
		// Setting the flag propagates through BindPFlag into viper; no v.Set
		// here so we don't override the flag value with viper's own store.
		if err := cmd.Flags().Set("provider", "cli-provider"); err != nil {
			t.Fatalf("Set provider: %v", err)
		}
		if err := cmd.Flags().Set("model", "cli-model"); err != nil {
			t.Fatalf("Set model: %v", err)
		}

		ctx := withViper(context.Background(), v)
		ctx = withConfig(ctx, config.Defaults())
		cmd.SetContext(ctx)

		opts := newRunOptions(cmd)
		if opts.Provider != "cli-provider" {
			t.Fatalf("Provider = %q, want cli-provider", opts.Provider)
		}
		if opts.Model != "cli-model" {
			t.Fatalf("Model = %q, want cli-model", opts.Model)
		}
		if opts.ConfigProvider != "" {
			t.Fatalf("ConfigProvider = %q, want empty when flag is set", opts.ConfigProvider)
		}
		if opts.ConfigModel != "" {
			t.Fatalf("ConfigModel = %q, want empty when flag is set", opts.ConfigModel)
		}
	})

	t.Run("unchanged flags route to config fields", func(t *testing.T) {
		cmd := newRunCmd()
		v := viper.New()
		if err := bindRunFlags(cmd, v); err != nil {
			t.Fatalf("bindRunFlags: %v", err)
		}
		v.Set("provider.default", "config-provider")
		v.Set("model.default", "config-model")

		ctx := withViper(context.Background(), v)
		ctx = withConfig(ctx, config.Defaults())
		cmd.SetContext(ctx)

		opts := newRunOptions(cmd)
		if opts.Provider != "" {
			t.Fatalf("Provider = %q, want empty when flag unchanged", opts.Provider)
		}
		if opts.Model != "" {
			t.Fatalf("Model = %q, want empty when flag unchanged", opts.Model)
		}
		if opts.ConfigProvider != "config-provider" {
			t.Fatalf("ConfigProvider = %q, want config-provider", opts.ConfigProvider)
		}
		if opts.ConfigModel != "config-model" {
			t.Fatalf("ConfigModel = %q, want config-model", opts.ConfigModel)
		}
	})
}

func TestBindRunFlagsMissingFlag(t *testing.T) {
	cmd := &cobra.Command{}
	v := viper.New()
	if err := bindRunFlags(cmd, v); err == nil {
		t.Fatalf("expected bindRunFlags error")
	}
}

func TestHasPipedInput(t *testing.T) {
	tests := []struct {
		name   string
		reader func(t *testing.T) (io.Reader, func())
		want   bool
	}{
		{
			name: "buffer with data",
			reader: func(*testing.T) (io.Reader, func()) {
				return bytes.NewBufferString("data"), func() {}
			},
			want: true,
		},
		{
			name: "empty buffer",
			reader: func(*testing.T) (io.Reader, func()) {
				return bytes.NewBuffer(nil), func() {}
			},
			want: false,
		},
		{
			name: "string reader",
			reader: func(*testing.T) (io.Reader, func()) {
				return strings.NewReader("data"), func() {}
			},
			want: false,
		},
		{
			name: "file reader",
			reader: func(t *testing.T) (io.Reader, func()) {
				file, err := os.CreateTemp(t.TempDir(), "input")
				if err != nil {
					t.Fatalf("CreateTemp: %v", err)
				}
				return file, func() { _ = file.Close() }
			},
			want: true,
		},
		{
			name: "closed file",
			reader: func(t *testing.T) (io.Reader, func()) {
				file, err := os.CreateTemp(t.TempDir(), "input")
				if err != nil {
					t.Fatalf("CreateTemp: %v", err)
				}
				if err := file.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}
				return file, func() {}
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, cleanup := tt.reader(t)
			defer cleanup()
			if got := hasPipedInput(reader); got != tt.want {
				t.Fatalf("hasPipedInput() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompleteAgentNames(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	baseDir := t.TempDir()
	t.Setenv("HOME", baseDir)
	if err := os.Chdir(baseDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join("agents", "go-tests"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(filepath.Join("agents", "python-tests"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	names, directive := completeAgentNames(nil, nil, "go-")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want no file comp", directive)
	}
	if len(names) != 1 || names[0] != "go-tests" {
		t.Fatalf("names = %v, want [go-tests]", names)
	}
}

func TestCompleteProviderNames(t *testing.T) {
	names, directive := completeProviderNames(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want no file comp", directive)
	}
	if len(names) != len(metrics.SupportedProviders) {
		t.Fatalf("names len = %d, want %d", len(names), len(metrics.SupportedProviders))
	}
	want := map[string]bool{"openai-compat": false}
	for _, n := range names {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, found := range want {
		if !found {
			t.Errorf("provider %q missing from completion list", n)
		}
	}
}

func TestInitConfigQuietVerbose(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)

	root := NewRootCmd()
	if err := root.PersistentFlags().Set("quiet", "true"); err != nil {
		t.Fatalf("Set quiet: %v", err)
	}
	if err := root.PersistentFlags().Set("verbose", "true"); err != nil {
		t.Fatalf("Set verbose: %v", err)
	}

	err := initConfig(root, nil)
	if err == nil {
		t.Fatalf("expected error for quiet and verbose")
	}
	if !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFindRunCmd(t *testing.T) {
	tests := []struct {
		name    string
		root    *cobra.Command
		wantNil bool
	}{
		{
			name:    "has run",
			root:    NewRootCmd(),
			wantNil: false,
		},
		{
			name:    "missing run",
			root:    &cobra.Command{},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := findRunCmd(tt.root)
			if (cmd == nil) != tt.wantNil {
				t.Fatalf("findRunCmd() nil = %v, want %v", cmd == nil, tt.wantNil)
			}
		})
	}
}

func TestNewRunCmdArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		input   io.Reader
		wantErr bool
	}{
		{
			name:    "args provided",
			args:    []string{"prompt"},
			input:   bytes.NewBuffer(nil),
			wantErr: false,
		},
		{
			name:    "piped input",
			args:    nil,
			input:   bytes.NewBufferString("hello"),
			wantErr: false,
		},
		{
			name:    "no prompt uses agent default",
			args:    nil,
			input:   bytes.NewBuffer(nil),
			wantErr: false, // No longer an error - agent's default user_prompt will be used
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRunCmd()
			cmd.SetIn(tt.input)
			err := cmd.Args(cmd, tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Args() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInitConfigWithConfigPath(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)

	cfg := config.Defaults()
	cfg.Log.Level = "debug"
	path := filepath.Join(baseDir, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCmd()
	root.SetContext(context.Background())
	if err := root.PersistentFlags().Set("config", path); err != nil {
		t.Fatalf("Set config: %v", err)
	}

	if err := initConfig(root, nil); err != nil {
		t.Fatalf("initConfig() error = %v", err)
	}
	loaded := configFromContext(root)
	if loaded == nil {
		t.Fatalf("expected config in context")
	}
	if loaded.Log.Level != "debug" {
		t.Fatalf("Log.Level = %q, want %q", loaded.Log.Level, "debug")
	}
}

func TestInitConfigMissingConfigFile(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)

	missingPath := filepath.Join(baseDir, "missing.yaml")
	root := NewRootCmd()
	root.SetContext(context.Background())
	if err := root.PersistentFlags().Set("config", missingPath); err != nil {
		t.Fatalf("Set config: %v", err)
	}

	if err := initConfig(root, nil); err != nil {
		t.Fatalf("initConfig() error = %v", err)
	}
	loaded := configFromContext(root)
	if loaded == nil {
		t.Fatalf("expected config in context")
	}
	defaults := config.Defaults()
	if loaded.Log.Level != defaults.Log.Level {
		t.Fatalf("Log.Level = %q, want %q", loaded.Log.Level, defaults.Log.Level)
	}
}

func TestInitConfigMissingRunCommand(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)

	root := NewRootCmd()
	root.SetContext(context.Background())
	if runCmd := findRunCmd(root); runCmd != nil {
		root.RemoveCommand(runCmd)
	}

	if err := initConfig(root, nil); err == nil {
		t.Fatalf("expected error for missing run command")
	}
}

func TestConfigFromContext(t *testing.T) {
	tests := []struct {
		name    string
		cmd     *cobra.Command
		wantNil bool
	}{
		{
			name: "missing config",
			cmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.SetContext(context.Background())
				return cmd
			}(),
			wantNil: true,
		},
		{
			name: "config present",
			cmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.SetContext(withConfig(context.Background(), config.Defaults()))
				return cmd
			}(),
			wantNil: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := configFromContext(tt.cmd)
			if (cfg == nil) != tt.wantNil {
				t.Fatalf("configFromContext() nil = %v, want %v", cfg == nil, tt.wantNil)
			}
		})
	}
}

func TestParseMCPServers(t *testing.T) {
	tests := []struct {
		name          string
		specs         []string
		wantLen       int
		wantFirst     string
		wantCmd       string
		wantArgs      []string
		wantTransport string
		wantURL       string
	}{
		{
			name:    "nil input",
			specs:   nil,
			wantLen: 0,
		},
		{
			name:    "empty input",
			specs:   []string{},
			wantLen: 0,
		},
		{
			name:      "name and command only",
			specs:     []string{"burpsuite:burp-mcp"},
			wantLen:   1,
			wantFirst: "burpsuite",
			wantCmd:   "burp-mcp",
		},
		{
			name:      "name command and args",
			specs:     []string{"chrome:chrome-mcp:--headless,--port=9222"},
			wantLen:   1,
			wantFirst: "chrome",
			wantCmd:   "chrome-mcp",
			wantArgs:  []string{"--headless", "--port=9222"},
		},
		{
			name:    "invalid spec (no colon)",
			specs:   []string{"invalid"},
			wantLen: 0,
		},
		{
			name:      "multiple servers",
			specs:     []string{"a:cmd1", "b:cmd2:arg1"},
			wantLen:   2,
			wantFirst: "a",
		},
		{
			name:      "empty args section",
			specs:     []string{"srv:cmd:"},
			wantLen:   1,
			wantFirst: "srv",
			wantCmd:   "cmd",
		},
		{
			name:          "sse transport",
			specs:         []string{"burpsuite:sse:http://localhost:9876"},
			wantLen:       1,
			wantFirst:     "burpsuite",
			wantTransport: "sse",
			wantURL:       "http://localhost:9876",
		},
		{
			name:          "sse transport uppercase",
			specs:         []string{"myserver:SSE:http://example.com/mcp"},
			wantLen:       1,
			wantFirst:     "myserver",
			wantTransport: "sse",
			wantURL:       "http://example.com/mcp",
		},
		{
			name:    "sse missing url",
			specs:   []string{"bad:sse:"},
			wantLen: 0,
		},
		{
			name:    "sse no url part",
			specs:   []string{"bad:sse"},
			wantLen: 0,
		},
		{
			name:          "mixed stdio and sse",
			specs:         []string{"grafana:mcp-grafana", "burp:sse:http://localhost:9876"},
			wantLen:       2,
			wantFirst:     "grafana",
			wantTransport: "", // first is stdio (default)
		},
		{
			name:          "streamable http transport",
			specs:         []string{"gcal:http:https://calendarmcp.googleapis.com/mcp/v1"},
			wantLen:       1,
			wantFirst:     "gcal",
			wantTransport: "streamable_http",
			wantURL:       "https://calendarmcp.googleapis.com/mcp/v1",
		},
		{
			name:          "streamable_http alias",
			specs:         []string{"x:streamable_http:https://example.com/mcp"},
			wantLen:       1,
			wantFirst:     "x",
			wantTransport: "streamable_http",
			wantURL:       "https://example.com/mcp",
		},
		{
			name:    "streamable http missing url",
			specs:   []string{"bad:http:"},
			wantLen: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMCPServers(tt.specs)
			if len(got) != tt.wantLen {
				t.Fatalf("parseMCPServers() len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen > 0 {
				if got[0].Name != tt.wantFirst {
					t.Errorf("Name = %q, want %q", got[0].Name, tt.wantFirst)
				}
				if tt.wantCmd != "" && got[0].Command != tt.wantCmd {
					t.Errorf("Command = %q, want %q", got[0].Command, tt.wantCmd)
				}
				if tt.wantTransport != "" && got[0].Transport != tt.wantTransport {
					t.Errorf("Transport = %q, want %q", got[0].Transport, tt.wantTransport)
				}
				if tt.wantURL != "" && got[0].URL != tt.wantURL {
					t.Errorf("URL = %q, want %q", got[0].URL, tt.wantURL)
				}
				if tt.wantArgs != nil {
					if len(got[0].Args) != len(tt.wantArgs) {
						t.Fatalf("Args len = %d, want %d", len(got[0].Args), len(tt.wantArgs))
					}
					for i, a := range tt.wantArgs {
						if got[0].Args[i] != a {
							t.Errorf("Args[%d] = %q, want %q", i, got[0].Args[i], a)
						}
					}
				}
			}
		})
	}
}

func TestValidateComposedFlags(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		value   string
		wantErr bool
	}{
		{"system", "system", "override", true},
		{"print-bundle", "print-bundle", "true", true},
		{"bundle-out", "bundle-out", "/tmp/b", true},
		{"apply", "apply", "true", true},
		{"apply-fallback", "apply-fallback", "true", true},
		{"require-actionable", "require-actionable", "true", true},
		{"stream", "stream", "true", true},
		{"max-cost allowed", "max-cost", "5.00", false},
		{"json allowed", "json", "true", false},
		{"out allowed", "out", "/tmp/report.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRunCmd()
			if err := cmd.Flags().Set(tt.flag, tt.value); err != nil {
				t.Fatalf("Set %s: %v", tt.flag, err)
			}
			err := validateComposedFlags(cmd)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateComposedFlags() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !strings.Contains(err.Error(), "not applicable to composed agents") {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestComposedDryRun(t *testing.T) {
	manifest := &agent.Manifest{
		Name:    "test-composed",
		Version: "1.0",
		Stages: []agent.ComposedStage{
			{Name: "analyze", Agents: []string{"scanner-a", "scanner-b"}},
			{Name: "fix", Agent: "fixer", DependsOn: []string{"analyze"}, Mode: "edit"},
		},
		Gates: []agent.ComposedGate{
			{After: "analyze", Command: "go vet ./..."},
			{After: "fix", Command: "go test ./...", OnFailure: "revert"},
		},
	}

	p, err := runner.ManifestToPipeline(manifest)
	if err != nil {
		t.Fatalf("ManifestToPipeline: %v", err)
	}

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := composedDryRun(cmd, manifest, p); err != nil {
		t.Fatalf("composedDryRun: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "test-composed") {
		t.Error("expected agent name in output")
	}
	if !strings.Contains(output, "2 stages") {
		t.Error("expected stage count in output")
	}
	if !strings.Contains(output, "scanner-a, scanner-b") {
		t.Error("expected parallel agents in output")
	}
	if !strings.Contains(output, "fixer") {
		t.Error("expected fix agent in output")
	}
	if !strings.Contains(output, "depends_on: analyze") {
		t.Error("expected dependency info in output")
	}
	if !strings.Contains(output, "go vet ./...") {
		t.Error("expected first gate command in output")
	}
	if !strings.Contains(output, "on_failure=stop") {
		t.Error("expected default on_failure=stop for gate without OnFailure")
	}
	if !strings.Contains(output, "on_failure=revert") {
		t.Error("expected on_failure=revert for second gate")
	}
}

func TestComposedDryRun_NoGates(t *testing.T) {
	manifest := &agent.Manifest{
		Name:    "no-gates",
		Version: "1.0",
		Stages:  []agent.ComposedStage{{Name: "s1", Agent: "a1"}},
	}

	p, err := runner.ManifestToPipeline(manifest)
	if err != nil {
		t.Fatalf("ManifestToPipeline: %v", err)
	}

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := composedDryRun(cmd, manifest, p); err != nil {
		t.Fatalf("composedDryRun: %v", err)
	}

	if strings.Contains(buf.String(), "Gates:") {
		t.Error("should not print Gates section when there are none")
	}
}

func TestComposedDryRun_ExplicitMode(t *testing.T) {
	// Stage with explicit mode set (not default "edit").
	manifest := &agent.Manifest{
		Name:    "mode-test",
		Version: "1.0",
		Stages: []agent.ComposedStage{
			{Name: "s1", Agent: "a1", Mode: "readonly"},
		},
	}

	p, err := runner.ManifestToPipeline(manifest)
	if err != nil {
		t.Fatalf("ManifestToPipeline: %v", err)
	}

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := composedDryRun(cmd, manifest, p); err != nil {
		t.Fatalf("composedDryRun: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "mode=readonly") {
		t.Error("expected mode=readonly in output")
	}
}

func TestRunComposedAgent_DryRun(t *testing.T) {
	manifest := `
name: test-composed
version: "1.0"
description: Test composed agent

stages:
  - name: analyze
    agents:
      - scanner-a
      - scanner-b
  - name: fix
    agent: fixer
    depends_on: [analyze]
    mode: edit

gates:
  - after: fix
    command: "go test ./..."
    on_failure: stop
`
	agentsDir := setupTestAgent(t, "test-composed", manifest, nil)

	v := viper.New()
	v.Set("run.agent", "test-composed")
	v.Set("run.agents_dir", agentsDir)
	v.Set("run.dry_run", true)

	cmd := newRunCmd()
	ctx := withViper(context.Background(), v)
	ctx = withConfig(ctx, config.Defaults())
	cmd.SetContext(ctx)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Flags().Set("agent", "test-composed"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("agents-dir", agentsDir); err != nil {
		t.Fatalf("Set agents-dir: %v", err)
	}
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("Set dry-run: %v", err)
	}

	// Execute RunE directly.
	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "test-composed") {
		t.Errorf("expected agent name in output, got: %s", output)
	}
	if !strings.Contains(output, "2 stages") {
		t.Errorf("expected stage count in output, got: %s", output)
	}
}

func TestRunComposedAgent_RejectsIncompatibleFlags(t *testing.T) {
	manifest := `
name: test-composed
version: "1.0"

stages:
  - name: s1
    agent: a1
`
	agentsDir := setupTestAgent(t, "test-composed", manifest, nil)

	v := viper.New()
	v.Set("run.agent", "test-composed")
	v.Set("run.agents_dir", agentsDir)
	v.Set("run.dry_run", true)

	cmd := newRunCmd()
	ctx := withViper(context.Background(), v)
	ctx = withConfig(ctx, config.Defaults())
	cmd.SetContext(ctx)

	if err := cmd.Flags().Set("agent", "test-composed"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("agents-dir", agentsDir); err != nil {
		t.Fatalf("Set agents-dir: %v", err)
	}
	if err := cmd.Flags().Set("system", "override"); err != nil {
		t.Fatalf("Set system: %v", err)
	}

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for --system with composed agent")
	}
	if !strings.Contains(err.Error(), "not applicable to composed agents") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunComposedAgent_ExecutionPath(t *testing.T) {
	t.Parallel()

	// Create a composed agent whose sub-agent exists on disk.
	// Execution will fail at model invocation but exercises the full
	// setup path: config, prompt, workingDir, vars, runner construction.
	baseDir := t.TempDir()

	// Create the sub-agent.
	subDir := filepath.Join(baseDir, "agents", "leaf-agent")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "agent.yaml"),
		[]byte("name: leaf-agent\nversion: v1\nentrypoint: system.md\nwrapper: agent.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "system.md"), []byte("sys"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "agent.md"), []byte("wrap"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create the composed agent.
	composedDir := filepath.Join(baseDir, "agents", "composed-exec")
	if err := os.MkdirAll(composedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	composedManifest := `
name: composed-exec
version: "1.0"

stages:
  - name: s1
    agent: leaf-agent
`
	if err := os.WriteFile(filepath.Join(composedDir, "agent.yaml"), []byte(composedManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(baseDir, "agents")

	v := viper.New()
	v.Set("run.agent", "composed-exec")
	v.Set("run.agents_dir", agentsDir)

	cmd := newRunCmd()
	ctx := withViper(context.Background(), v)
	ctx = withConfig(ctx, config.Defaults())
	cmd.SetContext(ctx)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Flags().Set("agent", "composed-exec"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("agents-dir", agentsDir); err != nil {
		t.Fatalf("Set agents-dir: %v", err)
	}
	if err := cmd.Flags().Set("working-dir", t.TempDir()); err != nil {
		t.Fatalf("Set working-dir: %v", err)
	}

	// Run will fail at model invocation (no real provider) — that's expected.
	// We're testing the setup path, not the model call.
	err := cmd.RunE(cmd, []string{"test prompt"})
	if err == nil {
		t.Fatal("expected error from model invocation")
	}
	// Should NOT fail at config/setup level.
	if strings.Contains(err.Error(), "config not available") {
		t.Fatalf("setup failed unexpectedly: %v", err)
	}
}

func TestRunComposedAgent_StdinPrompt(t *testing.T) {
	t.Parallel()
	manifest := `
name: test-stdin
version: "1.0"

stages:
  - name: s1
    agent: a1
`
	agentsDir := setupTestAgent(t, "test-stdin", manifest, nil)

	v := viper.New()
	v.Set("run.agent", "test-stdin")
	v.Set("run.agents_dir", agentsDir)

	cmd := newRunCmd()
	ctx := withViper(context.Background(), v)
	ctx = withConfig(ctx, config.Defaults())
	cmd.SetContext(ctx)

	// Provide piped input via a buffer.
	cmd.SetIn(bytes.NewBufferString("piped prompt content"))

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := cmd.Flags().Set("agent", "test-stdin"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("agents-dir", agentsDir); err != nil {
		t.Fatalf("Set agents-dir: %v", err)
	}
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("Set dry-run: %v", err)
	}

	// dry-run with stdin — exercises the prompt reading path.
	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
}

func TestRunComposedAgent_WithInlineStage(t *testing.T) {
	t.Parallel()

	// Create a composed agent with an inline stage that has prompt files.
	baseDir := t.TempDir()
	composedDir := filepath.Join(baseDir, "agents", "inline-composed")
	if err := os.MkdirAll(composedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(composedDir, "system.md"), []byte("inline sys"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(composedDir, "agent.md"), []byte("inline wrap"), 0o644); err != nil {
		t.Fatal(err)
	}

	composedManifest := `
name: inline-composed
version: "1.0"

stages:
  - name: review
    entrypoint: system.md
    wrapper: agent.md
`
	if err := os.WriteFile(filepath.Join(composedDir, "agent.yaml"), []byte(composedManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(baseDir, "agents")
	v := viper.New()
	v.Set("run.agent", "inline-composed")
	v.Set("run.agents_dir", agentsDir)

	cmd := newRunCmd()
	ctx := withViper(context.Background(), v)
	ctx = withConfig(ctx, config.Defaults())
	cmd.SetContext(ctx)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Flags().Set("agent", "inline-composed"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("agents-dir", agentsDir); err != nil {
		t.Fatalf("Set agents-dir: %v", err)
	}
	if err := cmd.Flags().Set("working-dir", t.TempDir()); err != nil {
		t.Fatalf("Set working-dir: %v", err)
	}

	// Execution will fail at model invocation but exercises inline agent
	// collection in runComposedAgent (lines 387-392).
	err := cmd.RunE(cmd, []string{"audit"})
	if err == nil {
		t.Fatal("expected error from model invocation")
	}
	if strings.Contains(err.Error(), "config not available") {
		t.Fatalf("setup failed: %v", err)
	}
}

func TestRunComposedAgent_StdinAndDefaultWorkDir(t *testing.T) {
	t.Parallel()

	// Create a composed agent with a real sub-agent so the non-dry-run path
	// proceeds through stdin reading and default workingDir resolution.
	baseDir := t.TempDir()

	subDir := filepath.Join(baseDir, "agents", "leaf")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "agent.yaml"),
		[]byte("name: leaf\nversion: v1\nentrypoint: system.md\nwrapper: agent.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "system.md"), []byte("sys"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "agent.md"), []byte("wrap"), 0o644); err != nil {
		t.Fatal(err)
	}

	composedDir := filepath.Join(baseDir, "agents", "stdin-composed")
	if err := os.MkdirAll(composedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	composedManifest := "name: stdin-composed\nversion: \"1.0\"\nstages:\n  - name: s1\n    agent: leaf\n"
	if err := os.WriteFile(filepath.Join(composedDir, "agent.yaml"), []byte(composedManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(baseDir, "agents")

	v := viper.New()
	v.Set("run.agent", "stdin-composed")
	v.Set("run.agents_dir", agentsDir)

	cmd := newRunCmd()
	ctx := withViper(context.Background(), v)
	ctx = withConfig(ctx, config.Defaults())
	cmd.SetContext(ctx)

	// Piped stdin with NO args — exercises lines 356-362 (stdin reading path).
	cmd.SetIn(bytes.NewBufferString("piped prompt"))

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Flags().Set("agent", "stdin-composed"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("agents-dir", agentsDir); err != nil {
		t.Fatalf("Set agents-dir: %v", err)
	}
	// Deliberately NOT setting --working-dir to exercise os.Getwd default (lines 365-366).

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error from model invocation")
	}
	// Should NOT fail at setup level.
	if strings.Contains(err.Error(), "config not available") {
		t.Fatalf("setup failed: %v", err)
	}
}

func TestInitConfigMissingConfigFlag(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := initConfig(cmd, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "failed to read config flag") {
		t.Fatalf("error = %q, want config flag error", err.Error())
	}
}

func TestParseVars(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   []string
		wantNil bool
		wantLen int
		wantKey string
		wantVal string
	}{
		{"nil", nil, true, 0, "", ""},
		{"empty", []string{}, true, 0, "", ""},
		{"single", []string{"FOO=bar"}, false, 1, "FOO", "bar"},
		{"multiple", []string{"A=1", "B=2"}, false, 2, "A", "1"},
		{"value with equals", []string{"CMD=echo a=b"}, false, 1, "CMD", "echo a=b"},
		{"no equals skipped", []string{"INVALID"}, false, 0, "", ""},
		{"empty key skipped", []string{"=value"}, false, 0, "", ""},
		{"mixed valid and invalid", []string{"GOOD=val", "BAD"}, false, 1, "GOOD", "val"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parseVars(tt.input)
			if tt.wantNil {
				if result != nil {
					t.Fatalf("expected nil, got %v", result)
				}
				return
			}
			if len(result) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(result), tt.wantLen)
			}
			if tt.wantKey != "" {
				if result[tt.wantKey] != tt.wantVal {
					t.Fatalf("result[%q] = %q, want %q", tt.wantKey, result[tt.wantKey], tt.wantVal)
				}
			}
		})
	}
}

func TestRunComposedAgent_NoConfig(t *testing.T) {
	t.Parallel()
	manifest := `
name: test-composed
version: "1.0"

stages:
  - name: s1
    agent: a1
`
	agentsDir := setupTestAgent(t, "test-composed", manifest, nil)

	v := viper.New()
	v.Set("run.agent", "test-composed")
	v.Set("run.agents_dir", agentsDir)

	cmd := newRunCmd()
	ctx := withViper(context.Background(), v)
	// Do NOT set config — this should trigger "config not available" error.
	cmd.SetContext(ctx)

	if err := cmd.Flags().Set("agent", "test-composed"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("agents-dir", agentsDir); err != nil {
		t.Fatalf("Set agents-dir: %v", err)
	}

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing config")
	}
	if !strings.Contains(err.Error(), "config not available") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunComposedAgent_WithPromptArgs(t *testing.T) {
	t.Parallel()
	manifest := `
name: test-composed
version: "1.0"

stages:
  - name: s1
    agent: a1
`
	agentsDir := setupTestAgent(t, "test-composed", manifest, nil)

	v := viper.New()
	v.Set("run.agent", "test-composed")
	v.Set("run.agents_dir", agentsDir)

	cmd := newRunCmd()
	ctx := withViper(context.Background(), v)
	ctx = withConfig(ctx, config.Defaults())
	cmd.SetContext(ctx)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := cmd.Flags().Set("agent", "test-composed"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("agents-dir", agentsDir); err != nil {
		t.Fatalf("Set agents-dir: %v", err)
	}
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("Set dry-run: %v", err)
	}

	// Pass args (prompt) through to the composed agent dry-run.
	err := cmd.RunE(cmd, []string{"audit", "this", "repo"})
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(buf.String(), "test-composed") {
		t.Error("expected agent name in output")
	}
}

func TestRunComposedAgent_WithWorkingDir(t *testing.T) {
	t.Parallel()
	manifest := `
name: test-composed
version: "1.0"

stages:
  - name: s1
    agent: a1
`
	agentsDir := setupTestAgent(t, "test-composed", manifest, nil)

	v := viper.New()
	v.Set("run.agent", "test-composed")
	v.Set("run.agents_dir", agentsDir)

	cmd := newRunCmd()
	ctx := withViper(context.Background(), v)
	ctx = withConfig(ctx, config.Defaults())
	cmd.SetContext(ctx)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := cmd.Flags().Set("agent", "test-composed"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("agents-dir", agentsDir); err != nil {
		t.Fatalf("Set agents-dir: %v", err)
	}
	if err := cmd.Flags().Set("working-dir", t.TempDir()); err != nil {
		t.Fatalf("Set working-dir: %v", err)
	}
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("Set dry-run: %v", err)
	}

	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
}

func TestRunComposedAgent_WithVars(t *testing.T) {
	t.Parallel()
	manifest := `
name: test-composed
version: "1.0"

stages:
  - name: s1
    agent: a1
`
	agentsDir := setupTestAgent(t, "test-composed", manifest, nil)

	v := viper.New()
	v.Set("run.agent", "test-composed")
	v.Set("run.agents_dir", agentsDir)

	cmd := newRunCmd()
	ctx := withViper(context.Background(), v)
	ctx = withConfig(ctx, config.Defaults())
	cmd.SetContext(ctx)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := cmd.Flags().Set("agent", "test-composed"); err != nil {
		t.Fatalf("Set agent: %v", err)
	}
	if err := cmd.Flags().Set("agents-dir", agentsDir); err != nil {
		t.Fatalf("Set agents-dir: %v", err)
	}
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("Set dry-run: %v", err)
	}

	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
}
