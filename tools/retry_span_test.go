package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/ollama"
	"github.com/cowdogmoo/squad/telemetry"
	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRetryExhaustionErrorContent(t *testing.T) {
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
			failure := &ollama.HTTPError{StatusCode: 503, Body: body}
			model := &mockLLM{responses: []*llms.ContentResponse{nil}, errors: []error{failure}}
			if _, err := retryGenerateContent(context.Background(), model, nil, nil, 0); err != failure {
				t.Fatalf("error = %v, want original provider error", err)
			}
			spans := rec.Ended()
			if len(spans) != 1 || spans[0].Status().Code != codes.Error {
				t.Fatalf("expected one failed span, got %v", spans)
			}
			span := spans[0]
			exported := span.Status().Description
			for _, attr := range span.Attributes() {
				exported += attr.Value.String()
			}
			for _, event := range span.Events() {
				exported += event.Name
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
