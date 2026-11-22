package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/ollama"
	"github.com/cowdogmoo/squad/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestBackgroundTaskErrorContent(t *testing.T) {
	const body = "private-provider-response-sk-123456789"
	for _, capture := range []string{"", "1"} {
		t.Run("capture="+capture, func(t *testing.T) {
			t.Setenv(telemetry.ExportCommandsEnv, capture)
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(tp)
			defer func() {
				otel.SetTracerProvider(previous)
				_ = tp.Shutdown(context.Background())
			}()
			failure := &ollama.HTTPError{StatusCode: 400, Body: body}
			cfg := TaskConfig{
				AgentsDir:  "agents",
				WorkingDir: t.TempDir(),
				Registry:   NewBackgroundTaskRegistry(0),
				CallModel: func(context.Context, string, string, string, string, string) (string, *metrics.Metrics, error) {
					return "", nil, failure
				},
			}
			payload, _ := json.Marshal(map[string]any{"agent": "child", "prompt": "do work", "background": true})
			if _, err := taskTool(cfg)(context.Background(), payload); err != nil {
				t.Fatalf("taskTool background: %v", err)
			}
			resultPayload, _ := json.Marshal(map[string]string{"task_id": "bg-1"})
			if _, err := taskResultTool(cfg)(context.Background(), resultPayload); err == nil || !strings.Contains(err.Error(), body) {
				t.Fatalf("caller should still receive provider body, got %v", err)
			}
			var span sdktrace.ReadOnlySpan
			for _, s := range rec.Ended() {
				if s.Name() == "task.background" {
					span = s
				}
			}
			if span == nil || span.Status().Code != codes.Error {
				t.Fatalf("expected failed task.background span, got %v", rec.Ended())
			}
			exported := span.Status().Description
			for _, event := range span.Events() {
				for _, attr := range event.Attributes {
					exported += attr.Value.String()
				}
			}
			if got := strings.Contains(exported, body); got != (capture == "1") {
				t.Errorf("body exported = %v, capture = %q: %s", got, capture, exported)
			}
		})
	}
}
