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

func TestKubeExecutor_CustomShell(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod":   "test-pod",
			"shell": "/bin/bash",
		},
	}

	ex := newKubeExecutor(fake.NewSimpleClientset(), testRestConfig, cfg, newFakeSPDYCreator("", "", nil))

	if ex.shell != "/bin/bash" {
		t.Fatalf("shell = %q, want '/bin/bash'", ex.shell)
	}
}

func TestKubeExecutor_CustomNamespace(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod":       "test-pod",
			"namespace": "kube-system",
		},
	}

	ex := newKubeExecutor(fake.NewSimpleClientset(), testRestConfig, cfg, newFakeSPDYCreator("", "", nil))

	if ex.namespace != "kube-system" {
		t.Fatalf("namespace = %q, want 'kube-system'", ex.namespace)
	}
}

func TestKubeExecutor_StdoutOnly(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod": "test-pod",
		},
	}

	ex := newKubeExecutor(testClient(t), testRestConfig, cfg, newFakeSPDYCreator("just stdout", "", nil))

	out, err := ex.Execute(context.Background(), "echo test")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(out) != "just stdout" {
		t.Fatalf("output = %q, want 'just stdout'", string(out))
	}
}

func TestBuildRestConfig_InvalidKubeconfig(t *testing.T) {
	t.Parallel()
	// buildRestConfig with a nonexistent kubeconfig and no in-cluster env
	// should fail (we're not in a cluster and the file doesn't exist).
	_, err := buildRestConfig("/nonexistent/kubeconfig", "")
	if err == nil {
		t.Fatal("expected error for invalid kubeconfig path")
	}
}

func TestBuildRestConfig_WithContext(t *testing.T) {
	t.Parallel()
	// buildRestConfig with a nonexistent kubeconfig and a context override
	// should still fail, but exercises the context override path.
	_, err := buildRestConfig("/nonexistent/kubeconfig", "fake-context")
	if err == nil {
		t.Fatal("expected error for invalid kubeconfig path")
	}
}

func TestKubeExecutor_Type(t *testing.T) {
	t.Parallel()
	ex := &KubeExecutor{}
	if got := ex.Type(); got != "kubectl" {
		t.Fatalf("Type() = %q, want 'kubectl'", got)
	}
}

func TestKubeExecutor_EnvironmentDescription(t *testing.T) {
	t.Parallel()
	ex := &KubeExecutor{
		pod:       "my-pod",
		namespace: "my-ns",
		shell:     "/bin/sh",
	}
	desc := ex.EnvironmentDescription()
	if !strings.Contains(desc, "my-ns/my-pod") {
		t.Fatalf("expected namespace/pod in description, got: %q", desc)
	}
	if !strings.Contains(desc, "/bin/sh") {
		t.Fatalf("expected shell in description, got: %q", desc)
	}
	if strings.Contains(desc, "Container:") {
		t.Fatalf("expected no container in description when empty, got: %q", desc)
	}
}

func TestKubeExecutor_EnvironmentDescriptionWithContainer(t *testing.T) {
	t.Parallel()
	ex := &KubeExecutor{
		pod:       "my-pod",
		namespace: "default",
		container: "sidecar",
		shell:     "/bin/bash",
	}
	desc := ex.EnvironmentDescription()
	if !strings.Contains(desc, "default/my-pod") {
		t.Fatalf("expected namespace/pod in description, got: %q", desc)
	}
	if !strings.Contains(desc, "Container: sidecar") {
		t.Fatalf("expected container name in description, got: %q", desc)
	}
	if !strings.Contains(desc, "/bin/bash") {
		t.Fatalf("expected shell in description, got: %q", desc)
	}
}

func TestNewKubeExecutor_DefaultsApplied(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod": "mypod",
			// namespace and shell intentionally omitted to test defaults
		},
	}
	ex := newKubeExecutor(fake.NewSimpleClientset(), testRestConfig, cfg, newFakeSPDYCreator("", "", nil))
	if ex.namespace != "default" {
		t.Errorf("namespace = %q, want 'default'", ex.namespace)
	}
	if ex.shell != "/bin/sh" {
		t.Errorf("shell = %q, want '/bin/sh'", ex.shell)
	}
	if ex.pod != "mypod" {
		t.Errorf("pod = %q, want 'mypod'", ex.pod)
	}
}

func TestBuildRestConfig_EmptyPath(t *testing.T) {
	t.Parallel()
	// Empty path + no in-cluster env falls back to default loading rules.
	// On a dev machine with ~/.kube/config this may succeed; on CI it fails.
	// Either outcome is acceptable — we just verify no panic.
	_, _ = buildRestConfig("", "")
}

func TestNewKubeExecutor_MissingPodOption(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type:    "kubectl",
		Options: map[string]string{},
	}
	_, err := NewKubeExecutor(cfg)
	if err == nil {
		t.Fatal("expected error for missing pod option")
	}
	if !strings.Contains(err.Error(), "pod") {
		t.Errorf("error %q should mention 'pod'", err.Error())
	}
}

func TestNewKubeExecutorInternal_DefaultsApplied(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	cfg := &Config{
		Type:    "kubectl",
		Options: map[string]string{"pod": "mypod"},
	}
	ex := newKubeExecutor(client, testRestConfig, cfg, newFakeSPDYCreator("", "", nil))
	if ex.namespace != "default" {
		t.Errorf("namespace = %q, want 'default'", ex.namespace)
	}
	if ex.shell != "/bin/sh" {
		t.Errorf("shell = %q, want '/bin/sh'", ex.shell)
	}
	if ex.pod != "mypod" {
		t.Errorf("pod = %q, want 'mypod'", ex.pod)
	}
}

func TestKubeExecutor_ExecuteContainerSet(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod":       "mypod",
			"container": "mycontainer",
			"namespace": "mynamespace",
		},
	}
	ex := newKubeExecutor(testClient(t), testRestConfig, cfg, newFakeSPDYCreator("hello", "", nil))
	out, err := ex.Execute(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("output = %q, want 'hello'", string(out))
	}
}

func TestKubeExecutor_EnvironmentDescriptionContainerField(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	cfg := &Config{
		Type: "kubectl",
		Options: map[string]string{
			"pod":       "testpod",
			"container": "testcontainer",
			"namespace": "testns",
		},
	}
	ex := newKubeExecutor(client, testRestConfig, cfg, newFakeSPDYCreator("", "", nil))
	desc := ex.EnvironmentDescription()
	if !strings.Contains(desc, "testcontainer") {
		t.Errorf("EnvironmentDescription = %q, want 'testcontainer'", desc)
	}
	if !strings.Contains(desc, "testns") {
		t.Errorf("EnvironmentDescription = %q, want 'testns'", desc)
	}
}
