package tools

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestSystemInfoTool(t *testing.T) {
	t.Parallel()
	ex := &mockExecutor{
		responses: map[string]string{
			"hostname":  "test-host",
			"uname -sr": "Linux 5.15.0",
			"uname -m":  "x86_64",
			"nproc":     "4",
			"whoami":    "root",
		},
	}

	tool := systemInfoTool(ex)
	output, err := tool(context.Background(), nil)
	if err != nil {
		t.Fatalf("systemInfoTool: %v", err)
	}

	if !strings.Contains(output, "test-host") {
		t.Fatalf("output missing hostname: %s", output)
	}
	if !strings.Contains(output, "x86_64") {
		t.Fatalf("output missing architecture: %s", output)
	}
}

func TestSystemInfoToolLocal(t *testing.T) {
	t.Parallel()
	ex := &testLocalExecutor{dir: t.TempDir()}

	tool := systemInfoTool(ex)
	output, err := tool(context.Background(), nil)
	if err != nil {
		t.Fatalf("systemInfoTool: %v", err)
	}

	if !strings.Contains(output, "hostname") {
		t.Fatalf("output missing hostname field: %s", output)
	}
	if !strings.Contains(output, "kernel") {
		t.Fatalf("output missing kernel field: %s", output)
	}
}

func TestExecTrimError(t *testing.T) {
	t.Parallel()
	ex := &failingExecutor{}
	result := execTrim(context.Background(), ex, "anything")
	if result != "" {
		t.Fatalf("expected empty string on error, got: %q", result)
	}
}

// mockExecutor returns canned responses based on substring matching.
type mockExecutor struct {
	responses map[string]string
}

func (e *mockExecutor) Execute(_ context.Context, command string) ([]byte, error) {
	for key, val := range e.responses {
		if strings.Contains(command, key) {
			return []byte(val), nil
		}
	}
	return []byte("unknown"), nil
}

func (e *mockExecutor) Close() error                   { return nil }
func (e *mockExecutor) Type() string                   { return "mock" }
func (e *mockExecutor) EnvironmentDescription() string { return "mock executor" }

// testLocalExecutor runs bash commands for testing.
type testLocalExecutor struct {
	dir string
}

func (e *testLocalExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = e.dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

func (e *testLocalExecutor) Close() error                   { return nil }
func (e *testLocalExecutor) Type() string                   { return "local-test" }
func (e *testLocalExecutor) EnvironmentDescription() string { return "local test" }

// failingExecutor always returns an error.
type failingExecutor struct{}

func (e *failingExecutor) Execute(_ context.Context, _ string) ([]byte, error) {
	return nil, exec.ErrNotFound
}

func (e *failingExecutor) Close() error                   { return nil }
func (e *failingExecutor) Type() string                   { return "failing" }
func (e *failingExecutor) EnvironmentDescription() string { return "always fails" }
