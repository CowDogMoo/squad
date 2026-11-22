package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/pipeline"
	"github.com/cowdogmoo/squad/routine"
	"github.com/cowdogmoo/squad/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Exercise the real HTTP client and every layer that records the resulting
// error. The bare credential is deliberately outside the redactor's known
// key/value forms: the default policy must omit content, not guess secrets.
func TestProviderErrorSpanContent(t *testing.T) {
	const secret = "sk-live-private-provider-body-123456789"
	const token = "private-token-987654321"
	body := `{"error":"` + strings.Repeat("rejected ", 30) + secret + `","token":"` + token + `"}`
	for _, capture := range []string{"", "1"} {
		t.Run("capture="+capture, func(t *testing.T) {
			t.Setenv(telemetry.ExportCommandsEnv, capture)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(tp)
			t.Cleanup(func() {
				otel.SetTracerProvider(previous)
				_ = tp.Shutdown(context.Background())
			})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			dir := t.TempDir()
			var providerErr error
			p := &pipeline.Runner{
				Pipeline: &pipeline.Pipeline{Name: "test", Stages: []pipeline.Stage{{Name: "test", Agent: "test"}}},
				RunAgent: func(ctx context.Context, _, _, _, _ string, _ map[string]string) (string, *metrics.Metrics, error) {
					out, m, err := InvokeModel(ctx, &RunOptions{
						Provider: "ollama", Model: "test", BaseURL: srv.URL,
						MaxIterations: 1, MaxIterationsExplicit: true,
					}, &agent.Bundle{WorkDir: dir, DisableTask: true})
					providerErr = err
					// CLI providers record their outcome on agent.invoke rather
					// than llm.call; test that path with the same provider error.
					_, span := telemetry.Tracer().Start(ctx, "agent.invoke.cli")
					recordInvokeOutcome(span, m, err)
					span.End()
					return out, m, err
				},
			}
			store := routine.NewStore()
			ref := routine.Ref{Scope: routine.ScopeGlobal, ID: "test"}
			if _, err := store.Create(ref, &routine.Routine{ID: "test", Agent: "test", Schedule: "@daily"}); err != nil {
				t.Fatal(err)
			}
			scheduler, err := routine.NewScheduler(store, func(ctx context.Context, _ routine.Entry) (string, error) {
				if _, err := p.Run(ctx); err == nil {
					t.Error("pipeline should fail")
				}
				return "", providerErr
			}, routine.SchedulerOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = scheduler.Shutdown(context.Background()) }()
			if err := scheduler.RunNow(context.Background(), ref); err == nil || !strings.Contains(err.Error(), secret) {
				t.Fatalf("caller should still receive provider body: %v", err)
			}
			checkRunErrorSpans(t, rec, capture, secret, token)
		})
	}
}

func checkRunErrorSpans(t *testing.T, rec *tracetest.SpanRecorder, capture, secret, token string) {
	t.Helper()
	want := map[string]bool{"llm.call": false, "agent.invoke.cli": false, "tool.loop": false,
		"llm.generate": false, "chat test": false, "pipeline.agent": false, "routine.fire": false}
	for _, span := range rec.Ended() {
		text := exportedText(span)
		if strings.Contains(text, token) || (capture == "" && strings.Contains(text, secret)) {
			t.Errorf("%s leaked error content: %s", span.Name(), text)
		}
		if _, ok := want[span.Name()]; !ok {
			continue
		}
		want[span.Name()] = true
		if span.Status().Code != codes.Error {
			t.Errorf("%s missing error status", span.Name())
		}
		attrs := make(map[string]string)
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value.String()
		}
		if attrs["http.response.status_code"] != "400" {
			t.Errorf("%s missing safe HTTP metadata: %v", span.Name(), attrs)
		}
		if capture == "1" && !strings.Contains(text, secret) {
			t.Errorf("%s missing opt-in body: %s", span.Name(), text)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing span %s", name)
		}
	}
}
