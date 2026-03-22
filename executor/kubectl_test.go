package executor

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// fakeStreamExecutor implements remotecommand.Executor for testing.
type fakeStreamExecutor struct {
	stdout string
	stderr string
	err    error
}

func (f *fakeStreamExecutor) StreamWithContext(_ context.Context, opts remotecommand.StreamOptions) error {
	if opts.Stdout != nil && f.stdout != "" {
		_, _ = io.WriteString(opts.Stdout, f.stdout)
	}
	if opts.Stderr != nil && f.stderr != "" {
		_, _ = io.WriteString(opts.Stderr, f.stderr)
	}
	return f.err
}

func (f *fakeStreamExecutor) Stream(_ remotecommand.StreamOptions) error {
	return f.err
}

func newFakeSPDYCreator(stdout, stderr string, err error) spdyCreator {
	return func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		return &fakeStreamExecutor{stdout: stdout, stderr: stderr, err: err}, nil
	}
}

func newFailingSPDYCreator(spdyErr error) spdyCreator {
	return func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		return nil, spdyErr
	}
}

// testRestConfig returns a rest.Config suitable for test clients.
var testRestConfig = &rest.Config{Host: "https://fake:6443"}

// testClient returns a real kubernetes.Interface backed by testRestConfig.
// Unlike fake.NewSimpleClientset(), this has a non-nil RESTClient.
func testClient(t *testing.T) kubernetes.Interface {
	t.Helper()
	c, err := kubernetes.NewForConfig(testRestConfig)
	if err != nil {
		t.Fatalf("NewForConfig: %v", err)
	}
	return c
}

func TestKubeExecutor_Execute(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod":       "test-pod",
			"namespace": "test-ns",
			"container": "main",
		},
	}

	ex := newKubeExecutor(testClient(t), testRestConfig, cfg, newFakeSPDYCreator("hello world\n", "", nil))

	out, err := ex.Execute(context.Background(), "echo hello world")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "hello world") {
		t.Fatalf("output = %q, want containing 'hello world'", string(out))
	}
}

func TestKubeExecutor_ExecuteWithStderr(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod": "test-pod",
		},
	}

	ex := newKubeExecutor(testClient(t), testRestConfig, cfg, newFakeSPDYCreator("out", "warn", nil))

	out, err := ex.Execute(context.Background(), "cmd")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "out") || !strings.Contains(string(out), "warn") {
		t.Fatalf("output = %q, want both stdout and stderr", string(out))
	}
}

func TestKubeExecutor_ExecuteError(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod": "test-pod",
		},
	}

	ex := newKubeExecutor(testClient(t), testRestConfig, cfg, newFakeSPDYCreator("partial", "err", fmt.Errorf("exit code 1")))

	out, err := ex.Execute(context.Background(), "failing-cmd")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "command failed") {
		t.Fatalf("error = %q, want 'command failed'", err)
	}
	if len(out) == 0 {
		t.Fatal("expected partial output on error")
	}
}

func TestKubeExecutor_SPDYCreationError(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod": "test-pod",
		},
	}

	ex := newKubeExecutor(testClient(t), testRestConfig, cfg, newFailingSPDYCreator(fmt.Errorf("connection refused")))

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "SPDY executor") {
		t.Fatalf("error = %q, want 'SPDY executor'", err)
	}
}

func TestKubeExecutor_DefaultNamespace(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod": "test-pod",
		},
	}

	ex := newKubeExecutor(fake.NewSimpleClientset(), testRestConfig, cfg, newFakeSPDYCreator("", "", nil))

	if ex.namespace != "default" {
		t.Fatalf("namespace = %q, want 'default'", ex.namespace)
	}
}

func TestKubeExecutor_DefaultShell(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod": "test-pod",
		},
	}

	ex := newKubeExecutor(fake.NewSimpleClientset(), testRestConfig, cfg, newFakeSPDYCreator("", "", nil))

	if ex.shell != "/bin/sh" {
		t.Fatalf("shell = %q, want '/bin/sh'", ex.shell)
	}
}

func TestKubeExecutor_Close(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod": "test-pod",
		},
	}

	ex := newKubeExecutor(fake.NewSimpleClientset(), testRestConfig, cfg, newFakeSPDYCreator("", "", nil))

	if err := ex.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNewKubeExecutor_MissingPod(t *testing.T) {
	t.Parallel()
	_, err := NewKubeExecutor(&Config{
		Type:    "kubectl",
		Options: map[string]string{},
	})
	if err == nil {
		t.Fatal("expected error for missing pod")
	}
	if !strings.Contains(err.Error(), "pod") {
		t.Fatalf("error = %q, want mention of 'pod'", err)
	}
}
