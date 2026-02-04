package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cowdogmoo/squad/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func TestRunConfigInit(t *testing.T) {
	tests := []struct {
		name      string
		precreate bool
		force     bool
		wantErr   bool
	}{
		{
			name:      "create config",
			precreate: false,
			force:     false,
			wantErr:   false,
		},
		{
			name:      "existing without force",
			precreate: true,
			force:     false,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", baseDir)
			t.Setenv("HOME", baseDir)

			cmd := &cobra.Command{}
			cmd.Flags().Bool("force", tt.force, "")
			ctx := withConfig(context.Background(), config.Defaults())
			cmd.SetContext(ctx)

			configPath, err := config.ConfigFile("config.yaml")
			if err != nil {
				t.Fatalf("ConfigFile: %v", err)
			}
			if tt.precreate {
				if err := os.WriteFile(configPath, []byte("existing"), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}

			err = runConfigInit(cmd, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("runConfigInit() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				data, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				if len(data) == 0 {
					t.Fatalf("expected config file content")
				}
			}
		})
	}
}

func TestRunConfigShow(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cfg := config.Defaults()
	cfg.Model.MaxTokens = 1234
	cmd.SetContext(withConfig(context.Background(), cfg))

	if err := runConfigShow(cmd, nil); err != nil {
		t.Fatalf("runConfigShow() error = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Current Squad Configuration") {
		t.Fatalf("expected header in output")
	}
	if !strings.Contains(out, "max_tokens: 1234") {
		t.Fatalf("expected max_tokens in output, got: %s", out)
	}
}

func TestRunConfigPath(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := runConfigPath(cmd, nil); err != nil {
		t.Fatalf("runConfigPath() error = %v", err)
	}

	expected := filepath.Join(baseDir, "squad", "config.yaml")
	got := strings.TrimSpace(buf.String())
	if got != expected {
		t.Fatalf("path = %q, want %q", got, expected)
	}
}

func TestRunConfigSet(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)

	cfg := config.Defaults()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	configPath, err := config.ConfigFile("config.yaml")
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	if err := runConfigSet(cmd, []string{"model.max_tokens", "2048"}); err != nil {
		t.Fatalf("runConfigSet() error = %v", err)
	}

	updated, err := config.LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if updated.Model.MaxTokens != 2048 {
		t.Fatalf("MaxTokens = %d, want 2048", updated.Model.MaxTokens)
	}
}

func TestRunConfigGet(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		want    string
		wantErr bool
	}{
		{
			name:    "existing key",
			key:     "model.max_tokens",
			want:    "900",
			wantErr: false,
		},
		{
			name:    "missing key",
			key:     "missing.key",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			var buf bytes.Buffer
			cmd.SetOut(&buf)

			cfg := config.Defaults()
			cfg.Model.MaxTokens = 900
			cmd.SetContext(withConfig(context.Background(), cfg))

			err := runConfigGet(cmd, []string{tt.key})
			if (err != nil) != tt.wantErr {
				t.Fatalf("runConfigGet() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if got := strings.TrimSpace(buf.String()); got != tt.want {
					t.Fatalf("value = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestCompletionCommand(t *testing.T) {
	root := NewRootCmd()
	var completion *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "completion" {
			completion = cmd
			break
		}
	}
	if completion == nil {
		t.Fatalf("expected completion command")
	}

	tests := []struct {
		name    string
		shell   string
		wantErr bool
	}{
		{
			name:    "bash",
			shell:   "bash",
			wantErr: false,
		},
		{
			name:    "unsupported",
			shell:   "ksh",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			completion.SetOut(&buf)
			err := completion.RunE(completion, []string{tt.shell})
			if (err != nil) != tt.wantErr {
				t.Fatalf("completion.RunE() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && buf.Len() == 0 {
				t.Fatalf("expected completion output")
			}
		})
	}
}

func TestVersionCommand(t *testing.T) {
	var buf bytes.Buffer
	versionCmd.SetOut(&buf)
	defer versionCmd.SetOut(nil)
	if err := versionCmd.RunE(versionCmd, nil); err != nil {
		t.Fatalf("versionCmd.RunE() error = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, fmt.Sprintf("squad version %s", version)) {
		t.Fatalf("unexpected version output: %s", out)
	}
}

func TestRunConfigInitForceOverwrite(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)

	cmd := &cobra.Command{}
	cmd.Flags().Bool("force", true, "")
	cmd.SetContext(withConfig(context.Background(), config.Defaults()))

	configPath, err := config.ConfigFile("config.yaml")
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := runConfigInit(cmd, nil); err != nil {
		t.Fatalf("runConfigInit() error = %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.TrimSpace(string(data)) == "existing" {
		t.Fatalf("expected config file to be overwritten")
	}
}

func TestRunConfigInitMissingConfig(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)

	cmd := &cobra.Command{}
	cmd.Flags().Bool("force", false, "")
	cmd.SetContext(context.Background())

	if err := runConfigInit(cmd, nil); err == nil {
		t.Fatalf("expected error for missing config")
	}
}

func TestRunConfigInitMissingForceFlag(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)

	cmd := &cobra.Command{}
	cmd.SetContext(withConfig(context.Background(), config.Defaults()))

	configPath, err := config.ConfigFile("config.yaml")
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := runConfigInit(cmd, nil); err == nil {
		t.Fatalf("expected error for missing force flag")
	}
}

func TestRunConfigShowMissingConfig(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	if err := runConfigShow(cmd, nil); err == nil {
		t.Fatalf("expected error for missing config")
	}
}

type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("write failed")
}

type failAfterWriter struct {
	failAfter int32
	writes    atomic.Int32
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.writes.Add(1) > w.failAfter {
		return 0, fmt.Errorf("write failed")
	}
	return len(p), nil
}

func TestRunConfigShowWriteError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(errWriter{})
	cmd.SetContext(withConfig(context.Background(), config.Defaults()))

	if err := runConfigShow(cmd, nil); err == nil {
		t.Fatalf("expected write error")
	}
}

func TestRunConfigShowWriteFailures(t *testing.T) {
	tests := []struct {
		name      string
		failAfter int32
	}{
		{"fail after header", 1},
		{"fail after description", 2},
		{"fail after spacer", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &failAfterWriter{failAfter: tt.failAfter}
			cmd := &cobra.Command{}
			cmd.SetOut(writer)
			cmd.SetContext(withConfig(context.Background(), config.Defaults()))
			if err := runConfigShow(cmd, nil); err == nil {
				t.Fatalf("expected write error")
			}
		})
	}
}

func TestRunConfigPathWriteError(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)

	cmd := &cobra.Command{}
	cmd.SetOut(errWriter{})

	if err := runConfigPath(cmd, nil); err == nil {
		t.Fatalf("expected write error")
	}
}

func TestRunConfigPathMissingHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	cmd := &cobra.Command{}
	if err := runConfigPath(cmd, nil); err == nil {
		t.Fatalf("expected error for missing config home")
	}
}

func TestRunConfigSetErrors(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)

	cfg := config.Defaults()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	configPath, err := config.ConfigFile("config.yaml")
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	tests := []struct {
		name  string
		args  []string
		setup func(t *testing.T)
	}{
		{
			name: "missing file",
			args: []string{"model.max_tokens", "42"},
			setup: func(t *testing.T) {
				t.Helper()
				_ = os.Remove(configPath)
			},
		},
		{
			name: "invalid value",
			args: []string{"model.max_tokens", ":["},
			setup: func(t *testing.T) {
				t.Helper()
				if err := os.WriteFile(configPath, data, 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}
			err := runConfigSet(cmd, tt.args)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}

func TestRunConfigGetMissingConfig(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	if err := runConfigGet(cmd, []string{"model.max_tokens"}); err == nil {
		t.Fatalf("expected error for missing config")
	}
}

func TestRunConfigGetWriteError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(errWriter{})
	cmd.SetContext(withConfig(context.Background(), config.Defaults()))

	if err := runConfigGet(cmd, []string{"model.max_tokens"}); err == nil {
		t.Fatalf("expected write error")
	}
}

func TestRunConfigInitLegacyConfig(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)
	t.Setenv("USERPROFILE", baseDir)

	legacyPath := filepath.Join(baseDir, ".squad", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("force", false, "")
	cmd.SetContext(withConfig(context.Background(), config.Defaults()))

	if err := runConfigInit(cmd, nil); err != nil {
		t.Fatalf("runConfigInit() error = %v", err)
	}
}
