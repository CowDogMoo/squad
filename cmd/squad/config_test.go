package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = w

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	os.Stdout = orig

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return string(data)
}

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
	cfg := config.Defaults()
	cfg.Model.MaxTokens = 1234
	cmd.SetContext(withConfig(context.Background(), cfg))

	out := captureStdout(t, func() {
		if err := runConfigShow(cmd, nil); err != nil {
			t.Fatalf("runConfigShow() error = %v", err)
		}
	})

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
	cmd := &cobra.Command{}
	out := captureStdout(t, func() {
		versionCmd.Run(cmd, nil)
	})

	if !strings.Contains(out, fmt.Sprintf("squad version %s", version)) {
		t.Fatalf("unexpected version output: %s", out)
	}
}
