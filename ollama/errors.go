package ollama

import "fmt"

// HTTPError preserves the provider response for callers while exposing safe
// status metadata separately from the response body for tracing.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("ollama returned %d: %s", e.StatusCode, e.Body)
}

// HTTPStatusCode returns the response status without inspecting error text.
func (e *HTTPError) HTTPStatusCode() int { return e.StatusCode }

// ProviderName identifies the provider that returned the error.
func (e *HTTPError) ProviderName() string { return "ollama" }
