package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		{"from stdin", nil, "from stdin", "from stdin", false},
		{"whitespace only stdin", nil, "   \n", "", true},
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

func TestResolveAgentsDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	tmp := t.TempDir()
	defer func() { _ = os.Chdir(cwd) }()

	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	t.Run("explicit path", func(t *testing.T) {
		explicit := filepath.Join(tmp, "explicit")
		explicitAbs, _ := filepath.Abs(explicit)
		resolved, err := resolveAgentsDir(explicit)
		if err != nil {
			t.Fatalf("resolveAgentsDir() error = %v", err)
		}
		if resolved != explicitAbs {
			t.Fatalf("resolved = %q, want %q", resolved, explicitAbs)
		}
	})

	t.Run("local agents dir", func(t *testing.T) {
		agentsDir := filepath.Join(tmp, "agents")
		if err := os.MkdirAll(agentsDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		defer os.RemoveAll(agentsDir)
		agentsAbs, _ := filepath.Abs(agentsDir)
		resolved, err := resolveAgentsDir("")
		if err != nil {
			t.Fatalf("resolveAgentsDir() error = %v", err)
		}
		resolvedEval, err := filepath.EvalSymlinks(resolved)
		if err != nil {
			resolvedEval = resolved
		}
		agentsEval, err := filepath.EvalSymlinks(agentsAbs)
		if err != nil {
			agentsEval = agentsAbs
		}
		if resolvedEval != agentsEval {
			t.Fatalf("resolved = %q, want %q", resolvedEval, agentsEval)
		}
	})

	t.Run("home config fallback", func(t *testing.T) {
		t.Setenv("HOME", tmp)
		resolved, err := resolveAgentsDir("")
		if err != nil {
			t.Fatalf("resolveAgentsDir() error = %v", err)
		}
		expected := filepath.Join(tmp, ".config", "squad", "agents")
		expectedAbs, _ := filepath.Abs(expected)
		resolvedEval, err := filepath.EvalSymlinks(resolved)
		if err != nil {
			resolvedEval = resolved
		}
		expectedEval, err := filepath.EvalSymlinks(expectedAbs)
		if err != nil {
			expectedEval = expectedAbs
		}
		if resolvedEval != expectedEval {
			t.Fatalf("resolved = %q, want %q", resolvedEval, expectedEval)
		}
	})
}
