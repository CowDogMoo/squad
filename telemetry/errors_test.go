package telemetry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const refusedPath = `path "/tmp/webapp-map/candidates.jsonl" is outside working directory`

func TestContentErrorStatus(t *testing.T) {
	long := strings.Repeat("x", 300)
	tests := []struct {
		name, env, text, want string
	}{
		{"default hides text", "", refusedPath, "fixed"},
		{"other value hides text", "true", refusedPath, "fixed"},
		{"opt-in shows text", "1", refusedPath, refusedPath},
		{"opt-in redacts", "1", "token=" + fakeSecret + " rejected", "token=" + RedactedMarker + " rejected"},
		{"opt-in truncates", "1", long, long[:maxExportedErrorBytes] + "..."},
		{"opt-in empty text", "1", "", "fixed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(ExportCommandsEnv, tt.env)
			if got := ContentErrorStatus(tt.text, "fixed"); got != tt.want {
				t.Errorf("ContentErrorStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// recordOne records err on a fresh span and returns the ended span.
func recordOne(t *testing.T, err error) sdktrace.ReadOnlySpan {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	_, span := tp.Tracer("test").Start(context.Background(), "tool.Read")
	RecordContentError(span, err, "tool returned an error")
	span.End()
	return rec.Ended()[0]
}

// exceptionAttrs returns the type and message of s's single exception event.
func exceptionAttrs(t *testing.T, s sdktrace.ReadOnlySpan) (typ, msg string) {
	t.Helper()
	evs := s.Events()
	if len(evs) != 1 || evs[0].Name != "exception" {
		t.Fatalf("events = %+v, want one exception event", evs)
	}
	m := attrMap(evs[0].Attributes)
	return m["exception.type"].AsString(), m["exception.message"].AsString()
}

func TestRecordContentError(t *testing.T) {
	err := errors.New(refusedPath)
	tests := []struct {
		name, env, want string
	}{
		{"default", "", "tool returned an error"},
		{"opt-in", "1", refusedPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(ExportCommandsEnv, tt.env)
			s := recordOne(t, err)
			if s.Status().Code != codes.Error || s.Status().Description != tt.want {
				t.Errorf("status = %+v, want error %q", s.Status(), tt.want)
			}
			typ, msg := exceptionAttrs(t, s)
			if typ != "*errors.errorString" || msg != tt.want {
				t.Errorf("exception = (%q, %q), want (*errors.errorString, %q)", typ, msg, tt.want)
			}
		})
	}
}

func TestRecordContentErrorNil(t *testing.T) {
	s := recordOne(t, nil)
	if s.Status().Code != codes.Unset || len(s.Events()) != 0 {
		t.Errorf("nil error recorded: status %+v, events %+v", s.Status(), s.Events())
	}
}
