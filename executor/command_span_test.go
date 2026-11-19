package executor

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/cowdogmoo/squad/telemetry"
	dockerclient "github.com/moby/moby/client"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// TestExecutorSpansDescribeAgentCommand runs each span-emitting executor with
// a command wrapped the way the Bash tools wrap it when a skill is active.
// The backend must receive the wrapped command, while the executor span's
// command attributes describe the agent's command recorded on ctx.
func TestExecutorSpansDescribeAgentCommand(t *testing.T) {
	t.Setenv(telemetry.ExportCommandsEnv, "")
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		_ = tp.Shutdown(context.Background())
	})

	agentCmd := "curl -s https://lab.test/api | jq .data"
	wrapped := "export SQUAD_SKILL_DIR='/skills/alpha'; " + agentCmd

	type run func(t *testing.T, ctx context.Context, command string) (sent string)
	executors := []struct {
		span string
		run  run
	}{
		{"executor.docker", func(t *testing.T, ctx context.Context, command string) string {
			client := &fakeDockerClient{
				execCreateFn: func(context.Context, string, dockerclient.ExecCreateOptions) (dockerclient.ExecCreateResult, error) {
					return dockerclient.ExecCreateResult{}, fmt.Errorf("stop")
				},
			}
			ex := &DockerExecutor{containerID: "abc123", shell: "/bin/sh", client: client}
			_, _ = ex.Execute(ctx, command)
			return client.execCmds[0][2]
		}},
		{"executor.ssm", func(t *testing.T, ctx context.Context, command string) string {
			client := &fakeSSMClient{
				sendCommandFn: func(context.Context, *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
					return nil, fmt.Errorf("stop")
				},
			}
			ex, err := newSSMExecutor(&Config{Options: map[string]string{"instance_id": "i-abc123"}}, "", client)
			if err != nil {
				t.Fatalf("newSSMExecutor: %v", err)
			}
			_, _ = ex.Execute(ctx, command)
			return client.sendCalls[0].Parameters["commands"][0]
		}},
		{"executor.kubectl", func(t *testing.T, ctx context.Context, command string) string {
			var sent string
			spdy := func(_ *rest.Config, _ string, u *url.URL) (remotecommand.Executor, error) {
				sent = u.Query()["command"][2]
				return &fakeStreamExecutor{}, nil
			}
			cfg := &Config{Type: "kubectl", Options: map[string]string{"pod": "p", "namespace": "ns"}}
			if _, err := newKubeExecutor(testClient(t), testRestConfig, cfg, spdy).Execute(ctx, command); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			return sent
		}},
	}
	cases := []struct {
		name     string
		ctx      context.Context
		wantName string
		wantLen  int
	}{
		{"agent command recorded", telemetry.WithAgentCommand(context.Background(), agentCmd), "curl", len(agentCmd)},
		{"no agent command recorded", context.Background(), "export", len(wrapped)},
	}
	for _, ex := range executors {
		for _, tc := range cases {
			t.Run(ex.span+"/"+tc.name, func(t *testing.T) {
				rec.Reset()
				if sent := ex.run(t, tc.ctx, wrapped); sent != wrapped {
					t.Errorf("backend received %q, want %q", sent, wrapped)
				}
				spans := rec.Ended()
				if len(spans) != 1 || spans[0].Name() != ex.span {
					t.Fatalf("recorded %d spans, want one %s span", len(spans), ex.span)
				}
				attrs := make(map[attribute.Key]attribute.Value)
				for _, kv := range spans[0].Attributes() {
					attrs[kv.Key] = kv.Value
				}
				if got := attrs["squad.executor.command"].AsString(); got != tc.wantName {
					t.Errorf("squad.executor.command = %q, want %q", got, tc.wantName)
				}
				if got := attrs["squad.executor.command_length"].AsInt64(); got != int64(tc.wantLen) {
					t.Errorf("squad.executor.command_length = %d, want %d", got, tc.wantLen)
				}
			})
		}
	}
}

// failingReader fails every read with err.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// TestExecutorSpanErrorHidesBackendText checks that a backend error quoting
// the command reaches the executor span's status only under the commands
// opt-in, and that the returned error keeps it either way.
func TestExecutorSpanErrorHidesBackendText(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		_ = tp.Shutdown(context.Background())
	})
	backendErr := fmt.Errorf(`exec "cat /srv/notes.txt": stream reset`)
	for _, tc := range []struct{ env, want string }{
		{"", commandFailedStatus},
		{"1", backendErr.Error()},
	} {
		t.Run("opt-in="+tc.env, func(t *testing.T) {
			t.Setenv(telemetry.ExportCommandsEnv, tc.env)
			rec.Reset()
			client := &fakeDockerClient{
				execAttachFn: func(context.Context, string) (dockerclient.HijackedResponse, error) {
					return dockerclient.HijackedResponse{Reader: bufio.NewReader(failingReader{backendErr})}, nil
				},
			}
			ex := &DockerExecutor{containerID: "abc123", shell: "/bin/sh", client: client}
			if _, err := ex.Execute(context.Background(), "cat /srv/notes.txt"); err == nil || !strings.Contains(err.Error(), "/srv/notes.txt") {
				t.Fatalf("Execute error = %v, want the backend error", err)
			}
			spans := rec.Ended()
			if len(spans) != 1 {
				t.Fatalf("recorded %d spans, want 1", len(spans))
			}
			if got := spans[0].Status(); got.Code != codes.Error || got.Description != tc.want {
				t.Errorf("span status = %+v, want error %q", got, tc.want)
			}
		})
	}
}
