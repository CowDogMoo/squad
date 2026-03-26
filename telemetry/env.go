package telemetry

import "os"

// hasOTLPEnv checks whether the standard OTLP environment variables are set.
func hasOTLPEnv() bool {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}
