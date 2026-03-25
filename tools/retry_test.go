package tools

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

func TestClassifyError_RawStrings(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want llms.ErrorCode
	}{
		{"500 error", fmt.Errorf("anthropic: 500 Internal Server Error"), llms.ErrCodeProviderUnavailable},
		{"503 overloaded", fmt.Errorf("overloaded"), llms.ErrCodeProviderUnavailable},
		{"429 rate limit", fmt.Errorf("429 too many requests"), llms.ErrCodeRateLimit},
		{"rate limit text", fmt.Errorf("rate limit exceeded"), llms.ErrCodeRateLimit},
		{"401 auth", fmt.Errorf("401 unauthorized"), llms.ErrCodeAuthentication},
		{"invalid api key", fmt.Errorf("invalid api key"), llms.ErrCodeAuthentication},
		{"400 bad request", fmt.Errorf("400 bad request"), llms.ErrCodeInvalidRequest},
		{"quota exceeded", fmt.Errorf("quota exceeded"), llms.ErrCodeQuotaExceeded},
		{"content blocked", fmt.Errorf("content blocked by filter"), llms.ErrCodeContentFilter},
		{"unknown error", fmt.Errorf("something weird happened"), llms.ErrCodeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyError(tt.err)
			if got.Code != tt.want {
				t.Errorf("classifyError(%q) = %s, want %s", tt.err, got.Code, tt.want)
			}
		})
	}
}

func TestClassifyError_WrappedLLMError(t *testing.T) {
	original := llms.NewError(llms.ErrCodeRateLimit, "anthropic", "rate limited")
	wrapped := fmt.Errorf("GenerateContent failed: %w", original)
	got := classifyError(wrapped)
	if got.Code != llms.ErrCodeRateLimit {
		t.Errorf("classifyError(wrapped *llms.Error) = %s, want %s", got.Code, llms.ErrCodeRateLimit)
	}
}

func TestClassifyError_Nil(t *testing.T) {
	if got := classifyError(nil); got != nil {
		t.Errorf("classifyError(nil) = %v, want nil", got)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"500", fmt.Errorf("500 internal server error"), true},
		{"503", fmt.Errorf("503 service unavailable"), true},
		{"overloaded", fmt.Errorf("overloaded"), true},
		{"429", fmt.Errorf("429 too many requests"), true},
		{"rate limit", fmt.Errorf("rate limit exceeded"), true},
		{"connection reset", fmt.Errorf("connection reset by peer"), true},
		{"eof", fmt.Errorf("unexpected EOF"), true},
		{"auth 401", fmt.Errorf("401 unauthorized"), false},
		{"invalid request", fmt.Errorf("invalid request"), false},
		{"content blocked", fmt.Errorf("content blocked"), false},
		{"quota exceeded", fmt.Errorf("quota exceeded"), false},
		{"context canceled", context.Canceled, false},
		{"deadline exceeded", context.DeadlineExceeded, false},
		{"wrapped llms rate limit", llms.NewError(llms.ErrCodeRateLimit, "anthropic", "rate limited"), true},
		{"wrapped llms auth", llms.NewError(llms.ErrCodeAuthentication, "anthropic", "bad key"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryable(tt.err); got != tt.want {
				t.Errorf("isRetryable(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestBackoffDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},
		{1, 4 * time.Second},
		{2, 8 * time.Second},
		{3, 16 * time.Second},
		{10, retryMaxDelay}, // capped
	}
	for _, tt := range tests {
		got := backoffDelay(tt.attempt)
		if got != tt.want {
			t.Errorf("backoffDelay(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

// mockLLM is a test double for llms.Model that returns preconfigured responses.
type mockLLM struct {
	calls     int
	responses []*llms.ContentResponse
	errors    []error
}

func (m *mockLLM) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	i := m.calls
	m.calls++
	if i >= len(m.errors) {
		return nil, fmt.Errorf("unexpected call %d", i)
	}
	return m.responses[i], m.errors[i]
}

func (m *mockLLM) Call(_ context.Context, _ string, _ ...llms.CallOption) (string, error) {
	return "", nil
}

func TestRetryGenerateContent_SuccessFirstTry(t *testing.T) {
	resp := &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}
	mock := &mockLLM{
		responses: []*llms.ContentResponse{resp},
		errors:    []error{nil},
	}
	got, err := retryGenerateContent(context.Background(), mock, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != resp {
		t.Errorf("got %v, want %v", got, resp)
	}
	if mock.calls != 1 {
		t.Errorf("calls = %d, want 1", mock.calls)
	}
}

func TestRetryGenerateContent_TransientThenSuccess(t *testing.T) {
	resp := &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}
	mock := &mockLLM{
		responses: []*llms.ContentResponse{nil, nil, resp},
		errors:    []error{fmt.Errorf("500 internal server error"), fmt.Errorf("overloaded"), nil},
	}

	// Override delay to speed up test — we test via a short-lived context.
	origBase := retryBaseDelay
	origMax := retryMaxDelay
	defer func() {
		// These are package-level consts; we can't reassign them.
		// Instead we use a context with generous timeout.
		_ = origBase
		_ = origMax
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got, err := retryGenerateContent(ctx, mock, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != resp {
		t.Errorf("got %v, want %v", got, resp)
	}
	if mock.calls != 3 {
		t.Errorf("calls = %d, want 3", mock.calls)
	}
}

func TestRetryGenerateContent_NonRetryable(t *testing.T) {
	mock := &mockLLM{
		responses: []*llms.ContentResponse{nil},
		errors:    []error{fmt.Errorf("401 unauthorized")},
	}
	_, err := retryGenerateContent(context.Background(), mock, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if mock.calls != 1 {
		t.Errorf("calls = %d, want 1 (should not retry non-retryable errors)", mock.calls)
	}
}

func TestRetryGenerateContent_ExhaustsRetries(t *testing.T) {
	errs := make([]error, maxRetries+1)
	resps := make([]*llms.ContentResponse, maxRetries+1)
	for i := range errs {
		errs[i] = fmt.Errorf("503 service unavailable")
	}
	mock := &mockLLM{responses: resps, errors: errs}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, err := retryGenerateContent(ctx, mock, nil, nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if mock.calls != maxRetries+1 {
		t.Errorf("calls = %d, want %d", mock.calls, maxRetries+1)
	}
}

func TestRetryGenerateContent_ContextCanceled(t *testing.T) {
	mock := &mockLLM{
		responses: []*llms.ContentResponse{nil, nil},
		errors:    []error{fmt.Errorf("500 internal server error"), fmt.Errorf("500 internal server error")},
	}
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay so the backoff wait is interrupted.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := retryGenerateContent(ctx, mock, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
