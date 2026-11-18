// Package telemetry provides OpenTelemetry tracing initialization for squad.
//
// When an OTLP endpoint is configured (via --otel-endpoint flag or the
// OTEL_EXPORTER_OTLP_ENDPOINT env var), traces are exported over OTLP/HTTP.
// When no endpoint is set, spans still get trace and span ids, so session
// events can name the span they belong to, but they are neither recorded nor
// exported.
package telemetry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "squad"

// Tracer returns the global tracer for squad instrumentation.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// Init initializes the OpenTelemetry tracer provider with an OTLP HTTP exporter.
// If endpoint is empty, it falls back to the OTEL_EXPORTER_OTLP_ENDPOINT env var
// (handled by the SDK). If neither is set, a provider that samples nothing is
// installed: spans get ids for local correlation and are otherwise dropped.
//
// The returned shutdown function flushes pending spans and must be called
// before the process exits.
func Init(ctx context.Context, serviceName, endpoint string) (shutdown func(context.Context) error, err error) {
	if endpoint == "" && !hasOTLPEnv() {
		// NeverSample still assigns trace and span ids but records nothing,
		// so the cost stays close to a no-op provider's.
		tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
		otel.SetTracerProvider(tp)
		return tp.Shutdown, nil
	}

	// Route OTEL SDK internal errors through the logging package instead of
	// Go's default log.Printf (which spams stderr on transient failures).
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logging.Debug("otel: %v", err)
	}))

	// Strip scheme from endpoint — otlptracehttp.WithEndpoint expects host:port.
	// The scheme is used to determine TLS vs plaintext.
	cleanEP := stripScheme(endpoint)

	var opts []otlptracehttp.Option
	if cleanEP != "" {
		opts = append(opts, otlptracehttp.WithEndpoint(cleanEP))
	}
	// Use insecure transport only for plaintext endpoints; TLS endpoints
	// (https:// scheme or OTEL_EXPORTER_OTLP_INSECURE explicitly "false")
	// use the default secure transport.
	if shouldUseInsecure(endpoint) {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	opts = append(opts, otlptracehttp.WithTimeout(5*time.Second))

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp.Shutdown, nil
}

// shouldUseInsecure returns true when the OTLP exporter should use plaintext
// HTTP. It checks, in order:
//  1. The OTEL_EXPORTER_OTLP_INSECURE env var (explicit user override).
//  2. Whether the endpoint (flag or env var) starts with "https://".
//  3. Whether the endpoint targets localhost (default dev setup).
//
// If none of the above match, it defaults to secure (TLS).
func shouldUseInsecure(endpoint string) bool {
	if v := os.Getenv("OTEL_EXPORTER_OTLP_INSECURE"); v != "" {
		return strings.EqualFold(v, "true")
	}

	ep := endpoint
	if ep == "" {
		ep = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if ep == "" {
		ep = os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	}

	if strings.HasPrefix(ep, "https://") {
		return false
	}
	if strings.HasPrefix(ep, "http://") {
		return true
	}

	host := strings.Split(ep, ":")[0]
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// stripScheme removes http:// or https:// from an endpoint string so it can
// be passed to otlptracehttp.WithEndpoint (which expects host or host:port).
func stripScheme(endpoint string) string {
	ep := strings.TrimPrefix(endpoint, "https://")
	ep = strings.TrimPrefix(ep, "http://")
	return ep
}
