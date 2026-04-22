package executor

import (
	"context"
	"strings"
	"testing"
)

func TestLocalExecutorMethods(t *testing.T) {
	tests := []struct {
		name       string
		workingDir string
		checkFn    func(t *testing.T, e *LocalExecutor)
	}{
		{
			name:       "Type returns local",
			workingDir: "/tmp",
			checkFn: func(t *testing.T, e *LocalExecutor) {
				if e.Type() != "local" {
					t.Errorf("Type() = %q, want %q", e.Type(), "local")
				}
			},
		},
		{
			name:       "Close returns nil",
			workingDir: "/tmp",
			checkFn: func(t *testing.T, e *LocalExecutor) {
				if err := e.Close(); err != nil {
					t.Errorf("Close() returned error: %v", err)
				}
			},
		},
		{
			name:       "EnvironmentDescription contains working dir and local",
			workingDir: "/my/work/dir",
			checkFn: func(t *testing.T, e *LocalExecutor) {
				desc := e.EnvironmentDescription()
				if !strings.Contains(desc, "/my/work/dir") {
					t.Errorf("EnvironmentDescription() = %q, want to contain working dir", desc)
				}
				if !strings.Contains(desc, "local") {
					t.Errorf("EnvironmentDescription() = %q, want to mention 'local'", desc)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &LocalExecutor{WorkingDir: tt.workingDir}
			tt.checkFn(t, e)
		})
	}
}

func TestLocalExecutorExecute(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		wantOutput string
		wantErr    bool
	}{
		{
			name:       "echo command",
			command:    "echo hello",
			wantOutput: "hello",
		},
		{
			name:       "pwd in temp dir",
			command:    "pwd",
			wantOutput: t.TempDir(),
		},
		{
			name:    "failing command",
			command: "exit 1",
			wantErr: true,
		},
		{
			name:       "stderr captured",
			command:    "echo error >&2",
			wantOutput: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			e := &LocalExecutor{WorkingDir: workDir}

			// Override workDir for pwd test.
			if tt.name == "pwd in temp dir" {
				e.WorkingDir = tt.wantOutput
			}

			out, err := e.Execute(context.Background(), tt.command)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantOutput != "" && !strings.Contains(string(out), tt.wantOutput) {
				t.Errorf("output %q does not contain %q", string(out), tt.wantOutput)
			}
		})
	}
}

func TestLocalExecutorContextCancellation(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"cancelled context", "sleep 10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &LocalExecutor{WorkingDir: t.TempDir()}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			_, err := e.Execute(ctx, tt.command)
			if err == nil {
				t.Error("expected error for cancelled context, got nil")
			}
		})
	}
}
