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
}

// NewDockerExecutor creates a Docker container from the given config
// and returns an executor that runs commands inside it.
func NewDockerExecutor(cfg *Config, workingDir string) (*DockerExecutor, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker executable not found: %w", err)
	}

	image := cfg.Options["image"]
	if image == "" {
		return nil, fmt.Errorf("docker executor requires 'image' option")
	}

	shell := cfg.Options["shell"]
	if shell == "" {
		shell = "/bin/sh"
	}

	containerWorkDir := cfg.Options["working_dir"]
	if containerWorkDir == "" {
		containerWorkDir = "/work"
	}

	// Build docker run args for a long-lived container.
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

	// Image and entrypoint that keeps the container alive.
	args = append(args, image, "tail", "-f", "/dev/null")

	var out bytes.Buffer
	cmd := exec.Command("docker", args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to start docker container: %s: %w", strings.TrimSpace(out.String()), err)
	}

	containerID := strings.TrimSpace(out.String())
	if containerID == "" {
		return nil, fmt.Errorf("docker run returned empty container ID")
	}

	return &DockerExecutor{
		containerID: containerID,
		shell:       shell,
	}, nil
}

// Execute runs a command inside the Docker container.
func (e *DockerExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	args := []string{"exec", e.containerID, e.shell, "-c", command}
	cmd := exec.CommandContext(ctx, "docker", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// Close stops and removes the Docker container.
func (e *DockerExecutor) Close() error {
	cmd := exec.Command("docker", "rm", "-f", e.containerID)
	return cmd.Run()
}
