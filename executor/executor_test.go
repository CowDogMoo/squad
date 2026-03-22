package executor

import (
	"context"
	"strings"
	"testing"
)

// Compile-time interface compliance checks.
var (
	_ Executor = (*LocalExecutor)(nil)
	_ Executor = (*DockerExecutor)(nil)
	_ Executor = (*SSMExecutor)(nil)
	_ Executor = (*KubeExecutor)(nil)
)

func TestLocalExecutor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &LocalExecutor{WorkingDir: dir}
	defer ex.Close()

	out, err := ex.Execute(context.Background(), "printf 'hello'")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// bash -l may emit profile noise on stderr before our output
	if !strings.HasSuffix(string(out), "hello") {
		t.Fatalf("output should end with %q, got %q", "hello", string(out))
	}
}

func TestLocalExecutor_WorkingDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &LocalExecutor{WorkingDir: dir}
	defer ex.Close()

	out, err := ex.Execute(context.Background(), "pwd")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// bash -l may emit profile noise on stderr; check the last line
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	lastLine := lines[len(lines)-1]
	if lastLine != dir {
		t.Fatalf("working dir = %q, want %q", lastLine, dir)
	}
}

func TestLocalExecutor_CommandError(t *testing.T) {
	t.Parallel()
	ex := &LocalExecutor{WorkingDir: t.TempDir()}
	defer ex.Close()

	_, err := ex.Execute(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error from 'false' command")
	}
}

func TestFactory_NilConfig(t *testing.T) {
	t.Parallel()
	ex, err := New(nil, "/tmp")
	if err != nil {
		t.Fatalf("New(nil): %v", err)
	}
	if ex == nil {
		t.Fatal("expected non-nil executor")
	}
	ex.Close()
}

func TestFactory_EmptyType(t *testing.T) {
	t.Parallel()
	ex, err := New(&Config{Type: ""}, "/tmp")
	if err != nil {
		t.Fatalf("New(empty): %v", err)
	}
	if ex == nil {
		t.Fatal("expected non-nil executor")
	}
	ex.Close()
}

func TestFactory_LocalType(t *testing.T) {
	t.Parallel()
	ex, err := New(&Config{Type: "local"}, "/tmp")
	if err != nil {
		t.Fatalf("New(local): %v", err)
	}
	if ex == nil {
		t.Fatal("expected non-nil executor")
	}
	ex.Close()
}

func TestFactory_UnknownType(t *testing.T) {
	t.Parallel()
	_, err := New(&Config{Type: "quantum"}, "/tmp")
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "unknown executor type") {
		t.Fatalf("error = %q, want 'unknown executor type'", err)
	}
}

func TestFactory_DockerMissingImage(t *testing.T) {
	t.Parallel()
	_, err := New(&Config{Type: "docker", Options: map[string]string{}}, "/tmp")
	if err == nil {
		t.Fatal("expected error for missing docker image")
	}
}

func TestFactory_SSMMissingInstanceID(t *testing.T) {
	t.Parallel()
	_, err := New(&Config{Type: "ssm", Options: map[string]string{}}, "/tmp")
	if err == nil {
		t.Fatal("expected error for missing instance_id")
	}
}

func TestFactory_KubectlMissingPod(t *testing.T) {
	t.Parallel()
	_, err := New(&Config{Type: "kubectl", Options: map[string]string{}}, "/tmp")
	if err == nil {
		t.Fatal("expected error for missing pod")
	}
}
