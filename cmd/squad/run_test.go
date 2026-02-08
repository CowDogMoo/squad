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

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "review this", "/tmp", "")
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

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "review this", "/tmp", "readonly")
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

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "test", "/tmp", "readonly")
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
	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "test", "/tmp", "readonly")
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

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "test", "/tmp", "custom")
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
