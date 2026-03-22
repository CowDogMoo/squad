package executor

import (
	"bytes"
	"context"
	"fmt"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

// spdyCreator abstracts remotecommand.NewSPDYExecutor for testing.
type spdyCreator func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error)

// KubeExecutor runs commands in a Kubernetes pod via the client-go API.
type KubeExecutor struct {
	client    kubernetes.Interface
	config    *rest.Config
	pod       string
	namespace string
	container string
	shell     string
	newSPDY   spdyCreator
	codec     runtime.ParameterCodec
}

// NewKubeExecutor creates an executor targeting a Kubernetes pod.
// It resolves kubeconfig from in-cluster config, explicit kubeconfig path,
// or the default loading rules (~/.kube/config, KUBECONFIG env).
func NewKubeExecutor(cfg *Config) (*KubeExecutor, error) {
	if cfg.Options["pod"] == "" {
		return nil, fmt.Errorf("kubectl executor requires 'pod' option")
	}

	restConfig, err := buildRestConfig(cfg.Options["kubeconfig"], cfg.Options["context"])
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return newKubeExecutor(clientset, restConfig, cfg, remotecommand.NewSPDYExecutor), nil
}

// newKubeExecutor wires up a KubeExecutor from its parts.
// Exported only for testing; production callers use NewKubeExecutor.
func newKubeExecutor(client kubernetes.Interface, restConfig *rest.Config, cfg *Config, spdy spdyCreator) *KubeExecutor {
	namespace := cfg.Options["namespace"]
	if namespace == "" {
		namespace = "default"
	}
	shell := cfg.Options["shell"]
	if shell == "" {
		shell = "/bin/sh"
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	return &KubeExecutor{
		client:    client,
		config:    restConfig,
		pod:       cfg.Options["pod"],
		namespace: namespace,
		container: cfg.Options["container"],
		shell:     shell,
		newSPDY:   spdy,
		codec:     runtime.NewParameterCodec(scheme),
	}
}

// Execute runs a command in the Kubernetes pod via SPDY exec streaming.
func (e *KubeExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	execOpts := &corev1.PodExecOptions{
		Command: []string{e.shell, "-c", command},
		Stdout:  true,
		Stderr:  true,
	}
	if e.container != "" {
		execOpts.Container = e.container
	}

	req := e.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(e.pod).
		Namespace(e.namespace).
		SubResource("exec").
		VersionedParams(execOpts, e.codec)

	spdy, err := e.newSPDY(e.config, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = spdy.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	combined := stdout.Bytes()
	if stderr.Len() > 0 {
		combined = append(combined, stderr.Bytes()...)
	}

	if err != nil {
		return combined, fmt.Errorf("command failed: %w", err)
	}

	return combined, nil
}

// Close is a no-op (pod lifecycle is managed externally).
func (e *KubeExecutor) Close() error { return nil }

// buildRestConfig resolves Kubernetes config from in-cluster, explicit path,
// or default loading rules.
func buildRestConfig(kubeconfigPath, kubeContext string) (*rest.Config, error) {
	// Try in-cluster config first.
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}

	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}
