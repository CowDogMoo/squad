package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DockerExecutor runs commands inside a long-lived Docker container.
// The container is started on creation and removed on Close.
type DockerExecutor struct {
	containerID string
	shell       string
	runner      cmdRunner
}

// cmdRunner abstracts os/exec for testing.
type cmdRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	RunInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// execRunner implements cmdRunner using real os/exec.
type execRunner struct{}

func (r *execRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

func (r *execRunner) RunInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// NewDockerExecutor creates a Docker container from the given config
// and returns an executor that runs commands inside it.
func NewDockerExecutor(cfg *Config, workingDir string) (*DockerExecutor, error) {
	return newDockerExecutor(cfg, workingDir, &execRunner{})
}

// newDockerExecutor is the internal constructor, injectable for testing.
func newDockerExecutor(cfg *Config, workingDir string, runner cmdRunner) (*DockerExecutor, error) {
	image := cfg.Options["image"]
	if image == "" {
		return nil, fmt.Errorf("docker executor requires 'image' option")
	}

	shell := cfg.Options["shell"]
	if shell == "" {
		shell = "/bin/sh"
	}

	args := buildDockerRunArgs(cfg, workingDir)
	args = append(args, image, "tail", "-f", "/dev/null")

	out, err := runner.Run(context.Background(), "docker", args...)
	if err != nil {
		return nil, fmt.Errorf("failed to start docker container: %s: %w", strings.TrimSpace(string(out)), err)
	}

	containerID := strings.TrimSpace(string(out))
	if containerID == "" {
		return nil, fmt.Errorf("docker run returned empty container ID")
	}

	return &DockerExecutor{
		containerID: containerID,
		shell:       shell,
		runner:      runner,
	}, nil
}

// buildDockerRunArgs constructs the docker run arguments from config.
func buildDockerRunArgs(cfg *Config, workingDir string) []string {
	containerWorkDir := cfg.Options["working_dir"]
	if containerWorkDir == "" {
		containerWorkDir = "/work"
	}

	args := []string{"run", "-d", "--rm", "-w", containerWorkDir}

	// Mount volumes.
	if vols := cfg.Options["volumes"]; vols != "" {
		for vol := range strings.SplitSeq(vols, ",") {
			vol = strings.TrimSpace(vol)
			if vol != "" {
				args = append(args, "-v", vol)
			}
		}
	}

	// Mount the host working directory by default so file tools stay useful.
	args = append(args, "-v", workingDir+":"+containerWorkDir)

	// Environment variables.
	if envs := cfg.Options["env"]; envs != "" {
		for kv := range strings.SplitSeq(envs, ",") {
			kv = strings.TrimSpace(kv)
			if kv != "" {
				args = append(args, "-e", kv)
			}
		}
	}

	// Platform override.
	if platform := cfg.Options["platform"]; platform != "" {
		args = append(args, "--platform", platform)
	}

	return args
}

// Execute runs a command inside the Docker container.
func (e *DockerExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	return e.runner.RunInDir(ctx, "", "docker", "exec", e.containerID, e.shell, "-c", command)
}

// Close stops and removes the Docker container.
func (e *DockerExecutor) Close() error {
	_, err := e.runner.Run(context.Background(), "docker", "rm", "-f", e.containerID)
	return err
}
