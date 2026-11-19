package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// maxExportedErrorBytes caps the opt-in error text on a span status.
const maxExportedErrorBytes = 200

// ContentErrorStatus returns the span status description for an error whose
// text may carry an agent's tool content: arguments such as paths, command
// text, or tool output. By default it is fixed. With
// SQUAD_OTEL_EXPORT_COMMANDS=1 it is text after RedactCommand, truncated to
// 200 bytes, or fixed when text is empty.
func ContentErrorStatus(text, fixed string) string {
	if text == "" || !exportFullCommands() {
		return fixed
	}
	return truncate(RedactCommand(text), maxExportedErrorBytes)
}

// RecordContentError marks span as failed by err, whose text may carry an
// agent's tool content. It sets the status description from
// ContentErrorStatus and adds the same "exception" event span.RecordError
// would, with the error's Go type and that description as its message, so
// err.Error() reaches the span only under the opt-in, redacted and
// truncated. A nil err does nothing.
func RecordContentError(span trace.Span, err error, fixed string) {
	if err == nil {
		return
	}
	desc := ContentErrorStatus(err.Error(), fixed)
	span.AddEvent("exception", trace.WithAttributes(
		attribute.String("exception.type", fmt.Sprintf("%T", err)),
		attribute.String("exception.message", desc),
	))
	span.SetStatus(codes.Error, desc)
}
