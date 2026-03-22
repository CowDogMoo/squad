package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// KubectlExecutor runs commands in a Kubernetes pod via kubectl exec.
type KubectlExecutor struct {
	pod       string
	namespace string
	container string
	shell     string
}

// NewKubectlExecutor creates a kubectl executor targeting the given pod.
func NewKubectlExecutor(cfg *Config) (*KubectlExecutor, error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return nil, fmt.Errorf("kubectl not found: %w", err)
	}

	pod := cfg.Options["pod"]
	if pod == "" {
		return nil, fmt.Errorf("kubectl executor requires 'pod' option")
	}

	namespace := cfg.Options["namespace"]
	if namespace == "" {
		namespace = "default"
	}

	shell := cfg.Options["shell"]
	if shell == "" {
		shell = "/bin/sh"
	}

	return &KubectlExecutor{
		pod:       pod,
		namespace: namespace,
		container: cfg.Options["container"],
		shell:     shell,
	}, nil
}

// Execute runs a command in the Kubernetes pod.
func (e *KubectlExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	args := []string{"exec", e.pod, "-n", e.namespace}
	if e.container != "" {
		args = append(args, "-c", e.container)
	}
	args = append(args, "--", e.shell, "-c", command)

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// Close is a no-op for kubectl (the pod lifecycle is managed externally).
func (e *KubectlExecutor) Close() error { return nil }
