package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/telemetry"
	"github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// dockerAPI abstracts the Docker SDK calls for testing.
type dockerAPI interface {
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, containerName string) (dockerclient.ContainerCreateResult, error)
	ContainerStart(ctx context.Context, containerID string) error
	ContainerRemove(ctx context.Context, containerID string, options dockerclient.ContainerRemoveOptions) error
	ExecCreate(ctx context.Context, containerID string, options dockerclient.ExecCreateOptions) (dockerclient.ExecCreateResult, error)
	ExecAttach(ctx context.Context, execID string) (dockerclient.HijackedResponse, error)
	ExecInspect(ctx context.Context, execID string) (dockerclient.ExecInspectResult, error)
	Close() error
}

// realDockerClient wraps the real Docker SDK client and implements dockerAPI.
type realDockerClient struct {
	cli *dockerclient.Client
}

func (r *realDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, containerName string) (dockerclient.ContainerCreateResult, error) {
	return r.cli.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
		Name:       containerName,
	})
}

func (r *realDockerClient) ContainerStart(ctx context.Context, containerID string) error {
	_, err := r.cli.ContainerStart(ctx, containerID, dockerclient.ContainerStartOptions{})
	return err
}

func (r *realDockerClient) ContainerRemove(ctx context.Context, containerID string, options dockerclient.ContainerRemoveOptions) error {
	_, err := r.cli.ContainerRemove(ctx, containerID, options)
	return err
}

func (r *realDockerClient) ExecCreate(ctx context.Context, containerID string, options dockerclient.ExecCreateOptions) (dockerclient.ExecCreateResult, error) {
	return r.cli.ExecCreate(ctx, containerID, options)
}

func (r *realDockerClient) ExecAttach(ctx context.Context, execID string) (dockerclient.HijackedResponse, error) {
	resp, err := r.cli.ExecAttach(ctx, execID, dockerclient.ExecAttachOptions{})
	return resp.HijackedResponse, err
}

func (r *realDockerClient) ExecInspect(ctx context.Context, execID string) (dockerclient.ExecInspectResult, error) {
	return r.cli.ExecInspect(ctx, execID, dockerclient.ExecInspectOptions{})
}

func (r *realDockerClient) Close() error {
	return r.cli.Close()
}

// DockerExecutor runs commands inside a long-lived Docker container.
// The container is started on creation and removed on Close.
type DockerExecutor struct {
	containerID string
	shell       string
	client      dockerAPI
}

// NewDockerExecutor creates a Docker container from the given config
// and returns an executor that runs commands inside it.
func NewDockerExecutor(cfg *Config, workingDir string) (*DockerExecutor, error) {
	cli, err := dockerclient.New(dockerclient.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}
	return newDockerExecutor(cfg, workingDir, &realDockerClient{cli: cli})
}

// newDockerExecutor is the internal constructor. Pass a non-nil dockerAPI
// to override the real client (for testing).
func newDockerExecutor(cfg *Config, workingDir string, client dockerAPI) (*DockerExecutor, error) {
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

	containerCfg := &container.Config{
		Image:      image,
		Cmd:        []string{"tail", "-f", "/dev/null"},
		WorkingDir: containerWorkDir,
	}

	// Environment variables.
	if envs := cfg.Options["env"]; envs != "" {
		for kv := range strings.SplitSeq(envs, ",") {
			kv = strings.TrimSpace(kv)
			if kv != "" {
				containerCfg.Env = append(containerCfg.Env, kv)
			}
		}
	}

	hostCfg := &container.HostConfig{
		AutoRemove: true,
		Binds:      []string{workingDir + ":" + containerWorkDir},
	}

	// Additional volumes.
	if vols := cfg.Options["volumes"]; vols != "" {
		for vol := range strings.SplitSeq(vols, ",") {
			vol = strings.TrimSpace(vol)
			if vol != "" {
				hostCfg.Binds = append(hostCfg.Binds, vol)
			}
		}
	}

	ctx := context.Background()
	resp, err := client.ContainerCreate(ctx, containerCfg, hostCfg, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create docker container: %w", err)
	}

	if err := client.ContainerStart(ctx, resp.ID); err != nil {
		if rmErr := client.ContainerRemove(ctx, resp.ID, dockerclient.ContainerRemoveOptions{Force: true}); rmErr != nil {
			logging.Warn("docker executor cleanup failed: %v", rmErr)
		}
		return nil, fmt.Errorf("failed to start docker container: %w", err)
	}

	return &DockerExecutor{
		containerID: resp.ID,
		shell:       shell,
		client:      client,
	}, nil
}

// Execute runs a command inside the Docker container.
func (e *DockerExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	containerID := e.containerID
	shortID := containerID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}

	ctx, span := telemetry.Tracer().Start(ctx, "executor.docker",
		trace.WithAttributes(
			attribute.String("squad.executor.container_id", shortID),
			attribute.String("squad.executor.shell", e.shell),
			attribute.String("squad.executor.command", command),
		),
	)
	defer span.End()

	execResp, err := e.client.ExecCreate(ctx, containerID, dockerclient.ExecCreateOptions{
		Cmd:          []string{e.shell, "-c", command},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("docker exec create failed: %w", err)
	}

	attachResp, err := e.client.ExecAttach(ctx, execResp.ID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("docker exec attach failed: %w", err)
	}
	if attachResp.Conn != nil {
		defer attachResp.Close()
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, attachResp.Reader); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("docker exec read output failed: %w", err)
	}

	inspect, err := e.client.ExecInspect(ctx, execResp.ID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return buf.Bytes(), fmt.Errorf("docker exec inspect failed: %w", err)
	}
	span.SetAttributes(attribute.Int("squad.executor.exit_code", inspect.ExitCode))
	if inspect.ExitCode != 0 {
		err := fmt.Errorf("command exited with code %d", inspect.ExitCode)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return buf.Bytes(), err
	}

	return buf.Bytes(), nil
}

// Close stops and removes the Docker container.
func (e *DockerExecutor) Close() error {
	return e.client.ContainerRemove(context.Background(), e.containerID, dockerclient.ContainerRemoveOptions{Force: true})
}

// Type returns "docker".
func (e *DockerExecutor) Type() string { return "docker" }

// EnvironmentDescription returns a description of the Docker execution environment.
func (e *DockerExecutor) EnvironmentDescription() string {
	id := e.containerID
	if len(id) > 12 {
		id = id[:12]
	}
	return fmt.Sprintf(
		"Commands execute inside Docker container %s (shell: %s). "+
			"File paths are relative to the container filesystem, not the host.",
		id, e.shell,
	)
}
