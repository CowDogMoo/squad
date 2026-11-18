package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// bashSpanAttrs runs command through bashTool inside a recorded span and
// returns that span's attributes.
func bashSpanAttrs(t *testing.T, command string) map[attribute.Key]attribute.Value {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("test").Start(context.Background(), "tool.Bash")
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	if _, err := bashTool(&mockExecutor{})(ctx, args); err != nil {
		t.Fatalf("bashTool() error = %v", err)
	}
	span.End()

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	attrs := make(map[attribute.Key]attribute.Value)
	for _, kv := range spans[0].Attributes() {
		attrs[kv.Key] = kv.Value
	}
	return attrs
}

func TestBashToolSpanExportsOnlyExecutableName(t *testing.T) {
	t.Setenv(telemetry.ExportCommandsEnv, "")
	secret := "s3cr3t-" + "fixture-value"
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"pipeline", `curl -H 'Authorization: Bearer ` + secret + `' https://lab.test | jq -r .token && sshpass` + ` -p ` + secret + ` ssh root@10.0.0.5`, "curl"},
		{"comment first", "# Let's try the creds we found\nsshpass" + " -p 'ab;" + secret + "' ssh admin@10.0.0.5 id", telemetry.ComplexCommand},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := bashSpanAttrs(t, tt.command)
			got := attrs["squad.tool.command"].AsString()
			if strings.Contains(got, secret) {
				t.Fatal("squad.tool.command carries argument text")
			}
			if got != tt.want {
				t.Errorf("squad.tool.command = %q, want %q", got, tt.want)
			}
			if n := attrs["squad.tool.command_length"].AsInt64(); n != int64(len(tt.command)) {
				t.Errorf("squad.tool.command_length = %d, want %d", n, len(tt.command))
			}
		})
	}
}

func TestBashToolSpanOptInExportsRedactedCommand(t *testing.T) {
	t.Setenv(telemetry.ExportCommandsEnv, "1")
	secret := "s3cr3t-" + "fixture-value"
	command := `curl -H 'Authorization: Bearer ` + secret + `' 'https://lab.test/?api_key=` + secret + `'`

	got := bashSpanAttrs(t, command)["squad.tool.command"].AsString()
	want := `curl -H 'Authorization: Bearer [REDACTED]' 'https://lab.test/?api_key=[REDACTED]'`
	if got != want {
		t.Errorf("squad.tool.command = %q, want %q", got, want)
	}
}

// agentCommandExecutor records, for the command it receives, the agent
// command that executor spans would describe.
type agentCommandExecutor struct {
	mockExecutor
	got chan [2]string
}

func (e *agentCommandExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	e.got <- [2]string{command, telemetry.AgentCommand(ctx, command)}
	return nil, nil
}

func TestBashToolsPassAgentCommandToExecutor(t *testing.T) {
	agentCmd := "curl -s https://lab.test/api | jq .data"
	tools := map[string]func(executor.Executor) func(context.Context, []byte) (string, error){
		"Bash":           bashTool,
		"BashBackground": bashBackgroundTool,
	}
	for name, tool := range tools {
		t.Run(name, func(t *testing.T) {
			ctx := InitBgCommandRegistry(pushedSkillCtx(t.TempDir()))
			ex := &agentCommandExecutor{got: make(chan [2]string, 1)}
			args, err := json.Marshal(map[string]string{"command": agentCmd})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			if _, err := tool(ex)(ctx, args); err != nil {
				t.Fatalf("%s error = %v", name, err)
			}
			select {
			case got := <-ex.got:
				if !strings.HasPrefix(got[0], "export SQUAD_SKILL_DIR=") || !strings.HasSuffix(got[0], "; "+agentCmd) {
					t.Errorf("executor received %q, want the skill-env-wrapped command", got[0])
				}
				if got[1] != agentCmd {
					t.Errorf("AgentCommand() on the executor ctx = %q, want %q", got[1], agentCmd)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("executor was not called")
			}
		})
	}
}
