package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestInitNoopWhenNoEndpoint(t *testing.T) {
	// Unset env vars for this test.
	for _, key := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"} {
		t.Setenv(key, "")
	}

	ctx := context.Background()
	shutdown, err := Init(ctx, "test-service", "")
	if err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}
	defer func() { _ = shutdown(ctx) }()

	// When no endpoint is set, spans should not be recording.
	tracer := Tracer()
	_, span := tracer.Start(ctx, "test-span")
	defer span.End()
	if span.IsRecording() {
		t.Error("expected non-recording span when no endpoint is configured")
	}
	if span.SpanContext().TraceID() != (trace.TraceID{}) {
		t.Error("expected zero TraceID from noop tracer")
	}
}

func TestTracerReturnsNonNil(t *testing.T) {
	tracer := Tracer()
	if tracer == nil {
		t.Fatal("Tracer() returned nil")
	}
}

func TestHasOTLPEnvFalseByDefault(t *testing.T) {
	for _, key := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"} {
		t.Setenv(key, "")
	}
	if hasOTLPEnv() {
		t.Error("hasOTLPEnv() should return false when env vars are unset")
	}
}

func TestHasOTLPEnvTrue(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	if !hasOTLPEnv() {
		t.Error("hasOTLPEnv() should return true when OTEL_EXPORTER_OTLP_ENDPOINT is set")
	}
}

func TestStripScheme(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"https://tempo.example.com", "tempo.example.com"},
		{"http://localhost:4318", "localhost:4318"},
		{"tempo.example.com:4318", "tempo.example.com:4318"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := stripScheme(tt.input); got != tt.want {
			t.Errorf("stripScheme(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestShouldUseInsecure(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		envVars  map[string]string
		want     bool
	}{
		{
			name:     "localhost is insecure",
			endpoint: "localhost:4318",
			want:     true,
		},
		{
			name:     "127.0.0.1 is insecure",
			endpoint: "127.0.0.1:4318",
			want:     true,
		},
		{
			name:     "http scheme is insecure",
			endpoint: "http://tempo.example.com:4318",
			want:     true,
		},
		{
			name:     "https scheme is secure",
			endpoint: "https://tempo.example.com",
			want:     false,
		},
		{
			name:     "bare hostname defaults to secure",
			endpoint: "tempo.example.com:4318",
			want:     false,
		},
		{
			name:     "empty endpoint defaults to insecure (localhost dev)",
			endpoint: "",
			want:     true,
		},
		{
			name:     "env var override insecure=true",
			endpoint: "https://tempo.example.com",
			envVars:  map[string]string{"OTEL_EXPORTER_OTLP_INSECURE": "true"},
			want:     true,
		},
		{
			name:     "env var override insecure=false",
			endpoint: "localhost:4318",
			envVars:  map[string]string{"OTEL_EXPORTER_OTLP_INSECURE": "false"},
			want:     false,
		},
		{
			name:     "env endpoint with https",
			endpoint: "",
			envVars:  map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://tempo.example.com"},
			want:     false,
		},
		{
			name:     "env endpoint with http",
			endpoint: "",
			envVars:  map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://tempo.local:4318"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all relevant env vars.
			for _, key := range []string{
				"OTEL_EXPORTER_OTLP_INSECURE",
				"OTEL_EXPORTER_OTLP_ENDPOINT",
				"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
			} {
				t.Setenv(key, "")
			}
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			if got := shouldUseInsecure(tt.endpoint); got != tt.want {
				t.Errorf("shouldUseInsecure(%q) = %v, want %v", tt.endpoint, got, tt.want)
			}
		})
	}
}
