package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

// fakeConn implements net.Conn for testing the Close() path.
type fakeConn struct{}

func (c *fakeConn) Read(b []byte) (int, error)         { return 0, io.EOF }
func (c *fakeConn) Write(b []byte) (int, error)        { return len(b), nil }
func (c *fakeConn) Close() error                       { return nil }
func (c *fakeConn) LocalAddr() net.Addr                { return nil }
func (c *fakeConn) RemoteAddr() net.Addr               { return nil }
func (c *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(t time.Time) error { return nil }

// fakeDockerClient implements dockerAPI for testing.
type fakeDockerClient struct {
	createFn      func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, name string) (container.CreateResponse, error)
	startFn       func(ctx context.Context, containerID string) error
	removeFn      func(ctx context.Context, containerID string, options container.RemoveOptions) error
	execCreateFn  func(ctx context.Context, containerID string, options container.ExecOptions) (container.ExecCreateResponse, error)
	execAttachFn  func(ctx context.Context, execID string) (types.HijackedResponse, error)
	execInspectFn func(ctx context.Context, execID string) (container.ExecInspect, error)

	// Track calls for assertions.
	createCalls []container.Config
	startCalls  []string
	removeCalls []string
	execCmds    [][]string
}

func (f *fakeDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, name string) (container.CreateResponse, error) {
	if config != nil {
		f.createCalls = append(f.createCalls, *config)
	}
	if f.createFn != nil {
		return f.createFn(ctx, config, hostConfig, name)
	}
	return container.CreateResponse{ID: "fake-container-id"}, nil
}

func (f *fakeDockerClient) ContainerStart(ctx context.Context, containerID string) error {
	f.startCalls = append(f.startCalls, containerID)
	if f.startFn != nil {
		return f.startFn(ctx, containerID)
	}
	return nil
}

func (f *fakeDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	f.removeCalls = append(f.removeCalls, containerID)
	if f.removeFn != nil {
		return f.removeFn(ctx, containerID, options)
	}
	return nil
}

func (f *fakeDockerClient) ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (container.ExecCreateResponse, error) {
	f.execCmds = append(f.execCmds, options.Cmd)
	if f.execCreateFn != nil {
		return f.execCreateFn(ctx, containerID, options)
	}
	return container.ExecCreateResponse{ID: "fake-exec-id"}, nil
}

func (f *fakeDockerClient) ContainerExecAttach(ctx context.Context, execID string) (types.HijackedResponse, error) {
	if f.execAttachFn != nil {
		return f.execAttachFn(ctx, execID)
	}
	return types.HijackedResponse{
		Reader: bufio.NewReader(bytes.NewBufferString("fake output")),
		Conn:   nil,
	}, nil
}

func (f *fakeDockerClient) ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	if f.execInspectFn != nil {
		return f.execInspectFn(ctx, execID)
	}
	return container.ExecInspect{ExitCode: 0}, nil
}

func (f *fakeDockerClient) Close() error { return nil }

func TestNewDockerExecutor_MissingImage(t *testing.T) {
	t.Parallel()
	_, err := newDockerExecutor(&Config{Options: map[string]string{}}, "/tmp", &fakeDockerClient{})
	if err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("expected image error, got: %v", err)
	}
}

func TestNewDockerExecutor_Success(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{}

	ex, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04"},
	}, "/tmp", client)
	if err != nil {
		t.Fatalf("newDockerExecutor: %v", err)
	}
	if ex.containerID != "fake-container-id" {
		t.Fatalf("containerID = %q, want fake-container-id", ex.containerID)
	}
	if ex.shell != "/bin/sh" {
		t.Fatalf("shell = %q, want /bin/sh", ex.shell)
	}
	if len(client.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(client.createCalls))
	}
	if client.createCalls[0].Image != "ubuntu:22.04" {
		t.Fatalf("image = %q, want ubuntu:22.04", client.createCalls[0].Image)
	}
	if len(client.startCalls) != 1 {
		t.Fatalf("expected 1 start call, got %d", len(client.startCalls))
	}
}

func TestNewDockerExecutor_CustomShell(t *testing.T) {
	t.Parallel()
	ex, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04", "shell": "/bin/bash"},
	}, "/tmp", &fakeDockerClient{})
	if err != nil {
		t.Fatalf("newDockerExecutor: %v", err)
	}
	if ex.shell != "/bin/bash" {
		t.Fatalf("shell = %q, want /bin/bash", ex.shell)
	}
}

func TestNewDockerExecutor_CustomWorkingDir(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{}

	_, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04", "working_dir": "/app"},
	}, "/host", client)
	if err != nil {
		t.Fatalf("newDockerExecutor: %v", err)
	}
	if client.createCalls[0].WorkingDir != "/app" {
		t.Fatalf("WorkingDir = %q, want /app", client.createCalls[0].WorkingDir)
	}
}

func TestNewDockerExecutor_WithEnvVars(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{}

	_, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04", "env": "FOO=bar,BAZ=qux"},
	}, "/host", client)
	if err != nil {
		t.Fatalf("newDockerExecutor: %v", err)
	}
	env := client.createCalls[0].Env
	if len(env) != 2 || env[0] != "FOO=bar" || env[1] != "BAZ=qux" {
		t.Fatalf("Env = %v, want [FOO=bar BAZ=qux]", env)
	}
}

func TestNewDockerExecutor_CreateFailure(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{
		createFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{}, fmt.Errorf("image not found")
		},
	}

	_, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04"},
	}, "/tmp", client)
	if err == nil || !strings.Contains(err.Error(), "failed to create docker container") {
		t.Fatalf("expected create error, got: %v", err)
	}
}

func TestNewDockerExecutor_StartFailure(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{
		startFn: func(_ context.Context, _ string) error {
			return fmt.Errorf("cannot start")
		},
	}

	_, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04"},
	}, "/tmp", client)
	if err == nil || !strings.Contains(err.Error(), "failed to start docker container") {
		t.Fatalf("expected start error, got: %v", err)
	}
	// Should have tried to remove the container.
	if len(client.removeCalls) != 1 {
		t.Fatalf("expected cleanup remove call, got %d", len(client.removeCalls))
	}
}

func TestDockerExecutor_Execute(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{
		execAttachFn: func(_ context.Context, _ string) (types.HijackedResponse, error) {
			return types.HijackedResponse{
				Reader: bufio.NewReader(bytes.NewBufferString("hello world\n")),
			}, nil
		},
	}

	ex := &DockerExecutor{
		containerID: "abc123",
		shell:       "/bin/sh",
		client:      client,
	}

	out, err := ex.Execute(context.Background(), "echo hello world")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "hello world") {
		t.Fatalf("output = %q, want 'hello world'", string(out))
	}
	// Verify exec command was built correctly.
	if len(client.execCmds) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(client.execCmds))
	}
	cmd := client.execCmds[0]
	if cmd[0] != "/bin/sh" || cmd[1] != "-c" || cmd[2] != "echo hello world" {
		t.Fatalf("exec cmd = %v, want [/bin/sh -c echo hello world]", cmd)
	}
}

func TestDockerExecutor_ExecuteNonZeroExit(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{
		execAttachFn: func(_ context.Context, _ string) (types.HijackedResponse, error) {
			return types.HijackedResponse{
				Reader: bufio.NewReader(bytes.NewBufferString("error output")),
			}, nil
		},
		execInspectFn: func(_ context.Context, _ string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 1}, nil
		},
	}

	ex := &DockerExecutor{
		containerID: "abc123",
		shell:       "/bin/sh",
		client:      client,
	}

	out, err := ex.Execute(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(out) == 0 {
		t.Fatal("expected output even on error")
	}
}

func TestDockerExecutor_ExecCreateFailure(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{
		execCreateFn: func(_ context.Context, _ string, _ container.ExecOptions) (container.ExecCreateResponse, error) {
			return container.ExecCreateResponse{}, fmt.Errorf("container not running")
		},
	}

	ex := &DockerExecutor{containerID: "abc123", shell: "/bin/sh", client: client}
	_, err := ex.Execute(context.Background(), "ls")
	if err == nil || !strings.Contains(err.Error(), "exec create failed") {
		t.Fatalf("expected exec create error, got: %v", err)
	}
}

func TestDockerExecutor_Close(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{}

	ex := &DockerExecutor{
		containerID: "abc123",
		shell:       "/bin/sh",
		client:      client,
	}

	if err := ex.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(client.removeCalls) != 1 || client.removeCalls[0] != "abc123" {
		t.Fatalf("expected remove call for abc123, got %v", client.removeCalls)
	}
}

func TestDockerExecutor_EnvironmentDescription(t *testing.T) {
	t.Parallel()
	ex := &DockerExecutor{containerID: "abcdef123456789", shell: "/bin/bash"}
	desc := ex.EnvironmentDescription()
	if !strings.Contains(desc, "abcdef123456") {
		t.Fatalf("expected truncated container ID in description, got: %q", desc)
	}
	if !strings.Contains(desc, "/bin/bash") {
		t.Fatalf("expected shell in description, got: %q", desc)
	}
}

func TestDockerExecutor_ExecAttachFailure(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{
		execAttachFn: func(_ context.Context, _ string) (types.HijackedResponse, error) {
			return types.HijackedResponse{}, fmt.Errorf("connection reset")
		},
	}

	ex := &DockerExecutor{containerID: "abc123", shell: "/bin/sh", client: client}
	_, err := ex.Execute(context.Background(), "ls")
	if err == nil || !strings.Contains(err.Error(), "exec attach failed") {
		t.Fatalf("expected exec attach error, got: %v", err)
	}
}

func TestDockerExecutor_ExecInspectFailure(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{
		execAttachFn: func(_ context.Context, _ string) (types.HijackedResponse, error) {
			return types.HijackedResponse{
				Reader: bufio.NewReader(bytes.NewBufferString("some output")),
			}, nil
		},
		execInspectFn: func(_ context.Context, _ string) (container.ExecInspect, error) {
			return container.ExecInspect{}, fmt.Errorf("inspect unavailable")
		},
	}

	ex := &DockerExecutor{containerID: "abc123", shell: "/bin/sh", client: client}
	out, err := ex.Execute(context.Background(), "ls")
	if err == nil || !strings.Contains(err.Error(), "exec inspect failed") {
		t.Fatalf("expected exec inspect error, got: %v", err)
	}
	// Should still return partial output even on inspect failure.
	if !strings.Contains(string(out), "some output") {
		t.Fatalf("expected partial output, got: %q", string(out))
	}
}

func TestNewDockerExecutor_WithVolumes(t *testing.T) {
	t.Parallel()
	var capturedHostCfg *container.HostConfig
	client := &fakeDockerClient{
		createFn: func(_ context.Context, _ *container.Config, hc *container.HostConfig, _ string) (container.CreateResponse, error) {
			capturedHostCfg = hc
			return container.CreateResponse{ID: "vol-container"}, nil
		},
	}

	_, err := newDockerExecutor(&Config{
		Options: map[string]string{
			"image":   "ubuntu:22.04",
			"volumes": "/host/data:/data, /host/config:/config",
		},
	}, "/tmp", client)
	if err != nil {
		t.Fatalf("newDockerExecutor: %v", err)
	}
	if capturedHostCfg == nil {
		t.Fatal("expected hostConfig to be captured")
	}
	// Default bind + 2 additional volumes = 3 total.
	if len(capturedHostCfg.Binds) != 3 {
		t.Fatalf("expected 3 binds, got %d: %v", len(capturedHostCfg.Binds), capturedHostCfg.Binds)
	}
	if capturedHostCfg.Binds[1] != "/host/data:/data" {
		t.Fatalf("bind[1] = %q, want /host/data:/data", capturedHostCfg.Binds[1])
	}
	if capturedHostCfg.Binds[2] != "/host/config:/config" {
		t.Fatalf("bind[2] = %q, want /host/config:/config", capturedHostCfg.Binds[2])
	}
}

func TestDockerExecutor_EnvironmentDescriptionShortID(t *testing.T) {
	t.Parallel()
	ex := &DockerExecutor{containerID: "short123", shell: "/bin/sh"}
	desc := ex.EnvironmentDescription()
	if !strings.Contains(desc, "short123") {
		t.Fatalf("expected full short container ID in description, got: %q", desc)
	}
	if !strings.Contains(desc, "/bin/sh") {
		t.Fatalf("expected shell in description, got: %q", desc)
	}
}

func TestNewDockerExecutor_EnvWithWhitespace(t *testing.T) {
	t.Parallel()
	client := &fakeDockerClient{}

	_, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04", "env": " FOO=bar , , BAZ=qux "},
	}, "/host", client)
	if err != nil {
		t.Fatalf("newDockerExecutor: %v", err)
	}
	env := client.createCalls[0].Env
	// Should filter out empty entries after trimming
	if len(env) != 2 || env[0] != "FOO=bar" || env[1] != "BAZ=qux" {
		t.Fatalf("Env = %v, want [FOO=bar BAZ=qux]", env)
	}
}

func TestNewDockerExecutor_VolumesWithWhitespace(t *testing.T) {
	t.Parallel()
	var capturedHostCfg *container.HostConfig
	client := &fakeDockerClient{
		createFn: func(_ context.Context, _ *container.Config, hc *container.HostConfig, _ string) (container.CreateResponse, error) {
			capturedHostCfg = hc
			return container.CreateResponse{ID: "test-container"}, nil
		},
	}

	_, err := newDockerExecutor(&Config{
		Options: map[string]string{
			"image":   "ubuntu:22.04",
			"volumes": " /a:/b , , /c:/d ",
		},
	}, "/tmp", client)
	if err != nil {
		t.Fatalf("newDockerExecutor: %v", err)
	}
	// Default bind + 2 valid volumes = 3 (empty string filtered out)
	if len(capturedHostCfg.Binds) != 3 {
		t.Fatalf("expected 3 binds, got %d: %v", len(capturedHostCfg.Binds), capturedHostCfg.Binds)
	}
}

func TestDockerExecutor_ExecuteWithConn(t *testing.T) {
	t.Parallel()
	// Create a mock that returns a HijackedResponse with non-nil Conn
	// to exercise the defer Close() path.
	pr, pw := io.Pipe()
	go func() {
		if _, err := pw.Write([]byte("connected output")); err != nil {
			return
		}
		if err := pw.Close(); err != nil {
			return
		}
	}()

	client := &fakeDockerClient{
		execAttachFn: func(_ context.Context, _ string) (types.HijackedResponse, error) {
			return types.HijackedResponse{
				Reader: bufio.NewReader(pr),
				Conn:   &fakeConn{},
			}, nil
		},
	}

	ex := &DockerExecutor{
		containerID: "abc123",
		shell:       "/bin/sh",
		client:      client,
	}

	out, err := ex.Execute(context.Background(), "echo test")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "connected output") {
		t.Fatalf("output = %q, want 'connected output'", string(out))
	}
}

func TestDockerExecutor_Type(t *testing.T) {
	t.Parallel()
	ex := &DockerExecutor{}
	if got := ex.Type(); got != "docker" {
		t.Fatalf("Type() = %q, want 'docker'", got)
	}
}
