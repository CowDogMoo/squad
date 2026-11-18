package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
		{"pipeline", `curl -H 'Authorization: Bearer ` + secret + `' https://lab.test | jq -r .token && sshpass -p ` + secret + ` ssh root@10.0.0.5`, "curl"},
		{"comment first", "# Let's try the creds we found\nsshpass -p 'ab;" + secret + "' ssh admin@10.0.0.5 id", telemetry.ComplexCommand},
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
