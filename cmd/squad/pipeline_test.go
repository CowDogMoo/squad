package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
