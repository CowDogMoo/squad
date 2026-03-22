package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// fakeCmdRunner implements cmdRunner for testing.
type fakeCmdRunner struct {
	calls   []fakeCall
	results []fakeResult
	idx     int
}

type fakeCall struct {
	Name string
	Args []string
	Dir  string
}

type fakeResult struct {
	Output []byte
	Err    error
}

func (r *fakeCmdRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, fakeCall{Name: name, Args: args})
	if r.idx < len(r.results) {
		result := r.results[r.idx]
		r.idx++
		return result.Output, result.Err
	}
	return nil, nil
}

func (r *fakeCmdRunner) RunInDir(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, fakeCall{Name: name, Args: args, Dir: dir})
	if r.idx < len(r.results) {
		result := r.results[r.idx]
		r.idx++
		return result.Output, result.Err
	}
	return nil, nil
}

func TestBuildDockerRunArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cfg        *Config
		workingDir string
		wantParts  []string
	}{
		{
			name: "basic",
			cfg: &Config{
				Options: map[string]string{"image": "ubuntu:22.04"},
			},
			workingDir: "/host/work",
			wantParts:  []string{"run", "-d", "--rm", "-w", "/work", "-v", "/host/work:/work"},
		},
		{
			name: "custom working dir",
			cfg: &Config{
				Options: map[string]string{"image": "ubuntu:22.04", "working_dir": "/app"},
			},
			workingDir: "/host",
			wantParts:  []string{"-w", "/app", "-v", "/host:/app"},
		},
		{
			name: "with volumes",
			cfg: &Config{
				Options: map[string]string{"image": "ubuntu:22.04", "volumes": "/data:/data,/config:/config"},
			},
			workingDir: "/host",
			wantParts:  []string{"-v", "/data:/data", "-v", "/config:/config"},
		},
		{
			name: "with env vars",
			cfg: &Config{
				Options: map[string]string{"image": "ubuntu:22.04", "env": "FOO=bar,BAZ=qux"},
			},
			workingDir: "/host",
			wantParts:  []string{"-e", "FOO=bar", "-e", "BAZ=qux"},
		},
		{
			name: "with platform",
			cfg: &Config{
				Options: map[string]string{"image": "ubuntu:22.04", "platform": "linux/amd64"},
			},
			workingDir: "/host",
			wantParts:  []string{"--platform", "linux/amd64"},
		},
		{
			name: "empty volume entries skipped",
			cfg: &Config{
				Options: map[string]string{"image": "ubuntu:22.04", "volumes": "/data:/data, , "},
			},
			workingDir: "/host",
			wantParts:  []string{"-v", "/data:/data"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := buildDockerRunArgs(tt.cfg, tt.workingDir)
			joined := strings.Join(args, " ")
			for _, part := range tt.wantParts {
				if !strings.Contains(joined, part) {
					t.Errorf("args = %v, want to contain %q", args, part)
				}
			}
		})
	}
}

func TestNewDockerExecutor_MissingImage(t *testing.T) {
	t.Parallel()
	_, err := newDockerExecutor(&Config{Options: map[string]string{}}, "/tmp", &fakeCmdRunner{})
	if err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("expected image error, got: %v", err)
	}
}

func TestNewDockerExecutor_Success(t *testing.T) {
	t.Parallel()
	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: []byte("abc123\n")},
		},
	}

	ex, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04"},
	}, "/tmp", runner)
	if err != nil {
		t.Fatalf("newDockerExecutor: %v", err)
	}
	if ex.containerID != "abc123" {
		t.Fatalf("containerID = %q, want abc123", ex.containerID)
	}
	if ex.shell != "/bin/sh" {
		t.Fatalf("shell = %q, want /bin/sh", ex.shell)
	}

	// Verify docker run was called.
	if len(runner.calls) != 1 || runner.calls[0].Name != "docker" {
		t.Fatalf("expected docker call, got %v", runner.calls)
	}
}

func TestNewDockerExecutor_CustomShell(t *testing.T) {
	t.Parallel()
	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: []byte("abc123\n")},
		},
	}

	ex, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04", "shell": "/bin/bash"},
	}, "/tmp", runner)
	if err != nil {
		t.Fatalf("newDockerExecutor: %v", err)
	}
	if ex.shell != "/bin/bash" {
		t.Fatalf("shell = %q, want /bin/bash", ex.shell)
	}
}

func TestNewDockerExecutor_RunFailure(t *testing.T) {
	t.Parallel()
	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: []byte("error msg"), Err: fmt.Errorf("exit code 1")},
		},
	}

	_, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04"},
	}, "/tmp", runner)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to start docker") {
		t.Fatalf("error = %q, want 'failed to start docker'", err)
	}
}

func TestNewDockerExecutor_EmptyContainerID(t *testing.T) {
	t.Parallel()
	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: []byte("  \n")},
		},
	}

	_, err := newDockerExecutor(&Config{
		Options: map[string]string{"image": "ubuntu:22.04"},
	}, "/tmp", runner)
	if err == nil || !strings.Contains(err.Error(), "empty container ID") {
		t.Fatalf("expected empty container ID error, got: %v", err)
	}
}

func TestDockerExecutor_Execute(t *testing.T) {
	t.Parallel()
	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: []byte("hello world\n")},
		},
	}

	ex := &DockerExecutor{
		containerID: "abc123",
		shell:       "/bin/sh",
		runner:      runner,
	}

	out, err := ex.Execute(context.Background(), "echo hello world")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "hello world") {
		t.Fatalf("output = %q, want 'hello world'", string(out))
	}

	// Verify correct docker exec args.
	call := runner.calls[0]
	if call.Name != "docker" {
		t.Fatalf("expected docker command")
	}
	if call.Args[0] != "exec" || call.Args[1] != "abc123" {
		t.Fatalf("args = %v, want [exec abc123 ...]", call.Args)
	}
}

func TestDockerExecutor_ExecuteError(t *testing.T) {
	t.Parallel()
	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: []byte("error output"), Err: fmt.Errorf("exit code 1")},
		},
	}

	ex := &DockerExecutor{
		containerID: "abc123",
		shell:       "/bin/sh",
		runner:      runner,
	}

	out, err := ex.Execute(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(out) == 0 {
		t.Fatal("expected output even on error")
	}
}

func TestDockerExecutor_Close(t *testing.T) {
	t.Parallel()
	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: []byte("abc123\n")},
		},
	}

	ex := &DockerExecutor{
		containerID: "abc123",
		shell:       "/bin/sh",
		runner:      runner,
	}

	if err := ex.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify docker rm -f was called.
	call := runner.calls[0]
	if call.Name != "docker" {
		t.Fatalf("expected docker command")
	}
	if call.Args[0] != "rm" || call.Args[1] != "-f" || call.Args[2] != "abc123" {
		t.Fatalf("args = %v, want [rm -f abc123]", call.Args)
	}
}
