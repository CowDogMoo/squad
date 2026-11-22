package telemetry

import (
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// maxExportedErrorBytes caps the opt-in error text on a span status.
const maxExportedErrorBytes = 200

// contentErrorStatus returns the span status description for an error whose
// text may carry an agent's tool arguments, such as a path or command text.
// By default it is fixed. With SQUAD_OTEL_EXPORT_COMMANDS=1 it is text after
// RedactCommand, truncated to 200 bytes, or fixed when text is empty.
func contentErrorStatus(text, fixed string) string {
	if text == "" || !exportFullCommands() {
		return fixed
	}
	return truncate(RedactCommand(text), maxExportedErrorBytes)
}

// RecordContentError marks span as failed by err, whose text may carry an
// agent's tool arguments, such as a path or command text. By default the
// status description is fixed; with SQUAD_OTEL_EXPORT_COMMANDS=1 it is
// err.Error() after RedactCommand, truncated to 200 bytes. It adds the same
// "exception" event span.RecordError would, with the error's Go type and
// that description as its message, so err.Error() reaches the span only
// under the opt-in. A nil err does nothing. It is for a call's Go error,
// not for tool output, which must stay off spans.
func RecordContentError(span trace.Span, err error, fixed string) {
	if err == nil {
		return
	}
	desc := contentErrorStatus(err.Error(), fixed)
	recordError(span, err, desc, desc)
}

// RecordRunError records a run or provider failure. Free-form error text,
// including HTTP response bodies, is content: by default only the Go error
// type, a fixed message and any structured provider/status metadata are
// exported. SQUAD_OTEL_EXPORT_COMMANDS=1 includes the full error in the
// exception event after RedactCommand; the status is capped at 200 bytes.
// The original error returned to callers and local logs is unchanged.
func RecordRunError(span trace.Span, err error, fixed string) {
	if err == nil {
		return
	}
	var httpErr interface {
		HTTPStatusCode() int
		ProviderName() string
	}
	if errors.As(err, &httpErr) {
		span.SetAttributes(
			attribute.Int("http.response.status_code", httpErr.HTTPStatusCode()),
			attribute.String("gen_ai.provider.name", httpErr.ProviderName()),
		)
	}
	message := fixed
	if exportFullCommands() {
		message = RedactCommand(err.Error())
	}
	recordError(span, err, message, truncate(message, maxExportedErrorBytes))
}

func recordError(span trace.Span, err error, message, status string) {
	span.AddEvent("exception", trace.WithAttributes(
		attribute.String("exception.type", fmt.Sprintf("%T", err)),
		attribute.String("exception.message", message),
	))
	span.SetStatus(codes.Error, status)
}
