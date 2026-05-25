package tools

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode llms.ErrorCode
		wantNil  bool
	}{
		{"nil error", nil, "", true},
		{"rate limit", errors.New("rate limit exceeded"), llms.ErrCodeRateLimit, false},
		{"too many requests", errors.New("too many requests 429"), llms.ErrCodeRateLimit, false},
		{"invalid api key", errors.New("invalid api key provided"), llms.ErrCodeAuthentication, false},
		{"auth failed", errors.New("authentication failed"), llms.ErrCodeAuthentication, false},
		{"model not found", errors.New("model not found: gpt-99"), llms.ErrCodeResourceNotFound, false},
		{"context window", errors.New("context window exceeded"), llms.ErrCodeTokenLimit, false},
		{"content blocked", errors.New("content blocked by safety filter"), llms.ErrCodeContentFilter, false},
		{"quota exceeded", errors.New("credit limit reached"), llms.ErrCodeQuotaExceeded, false},
		{"invalid request", errors.New("invalid request: bad param"), llms.ErrCodeInvalidRequest, false},
		{"service unavailable", errors.New("service unavailable 503"), llms.ErrCodeProviderUnavailable, false},
		{"overloaded", errors.New("server overloaded"), llms.ErrCodeProviderUnavailable, false},
		{"unknown error", errors.New("some random error"), llms.ErrCodeUnknown, false},
		{"wrapped llms.Error passthrough", llms.NewError(llms.ErrCodeRateLimit, "provider", "rate limited"), llms.ErrCodeRateLimit, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyError(tt.err)
			if tt.wantNil {
				if result != nil {
					t.Errorf("classifyError(%v) = %v, want nil", tt.err, result)
				}
				return
			}
			if result == nil {
				t.Fatalf("classifyError(%v) returned nil", tt.err)
			}
			if result.Code != tt.wantCode {
				t.Errorf("classifyError(%q).Code = %q, want %q",
					tt.err, result.Code, tt.wantCode)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, false},
		{"rate limit", errors.New("rate limit exceeded"), true},
		{"service unavailable", errors.New("service unavailable 503"), true},
		{"provider unavailable", llms.NewError(llms.ErrCodeProviderUnavailable, "", "down"), true},
		{"rate limit code", llms.NewError(llms.ErrCodeRateLimit, "", "rate limited"), true},
		{"auth error", errors.New("invalid api key"), false},
		{"content filter", errors.New("content blocked"), false},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"broken pipe", errors.New("broken pipe"), true},
		{"eof", errors.New("unexpected eof"), true},
		{"unknown random", errors.New("some unknown error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryable(tt.err)
			if got != tt.want {
				t.Errorf("isRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestBackoffDelay(t *testing.T) {
	tests := []struct {
		attempt int
		wantMs  int64
	}{
		{0, 2000},
		{1, 4000},
		{2, 8000},
		{10, 30000}, // capped at 30s
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			d := backoffDelay(tt.attempt)
			ms := d.Milliseconds()
			if ms != tt.wantMs {
				t.Errorf("backoffDelay(%d) = %v (%dms), want %dms",
					tt.attempt, d, ms, tt.wantMs)
			}
		})
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

func TestRetryGenerateContent(t *testing.T) {
	okResp := &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}

	tests := []struct {
		name      string
		responses []*llms.ContentResponse
		errors    []error
		ctxFunc   func() (context.Context, context.CancelFunc)
		wantResp  *llms.ContentResponse
		wantErr   bool
		checkErr  func(t *testing.T, err error)
		wantCalls int
	}{
		{
			name:      "success first try",
			responses: []*llms.ContentResponse{okResp},
			errors:    []error{nil},
			wantResp:  okResp,
			wantCalls: 1,
		},
		{
			name:      "transient then success",
			responses: []*llms.ContentResponse{nil, nil, okResp},
			errors:    []error{fmt.Errorf("500 internal server error"), fmt.Errorf("overloaded"), nil},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 30*time.Second)
			},
			wantResp:  okResp,
			wantCalls: 3,
		},
		{
			name:      "non-retryable stops immediately",
			responses: []*llms.ContentResponse{nil},
			errors:    []error{fmt.Errorf("401 unauthorized")},
			wantErr:   true,
			wantCalls: 1,
		},
		{
			name: "exhausts all retries",
			responses: func() []*llms.ContentResponse {
				r := make([]*llms.ContentResponse, DefaultMaxRetries+1)
				return r
			}(),
			errors: func() []error {
				e := make([]error, DefaultMaxRetries+1)
				for i := range e {
					e[i] = fmt.Errorf("503 service unavailable")
				}
				return e
			}(),
			ctxFunc: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 60*time.Second)
			},
			wantErr:   true,
			wantCalls: DefaultMaxRetries + 1,
		},
		{
			name:      "context canceled during backoff",
			responses: []*llms.ContentResponse{nil, nil},
			errors:    []error{fmt.Errorf("500 internal server error"), fmt.Errorf("500 internal server error")},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					time.Sleep(100 * time.Millisecond)
					cancel()
				}()
				return ctx, cancel
			},
			wantErr: true,
			checkErr: func(t *testing.T, err error) {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("expected context.Canceled, got %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockLLM{responses: tt.responses, errors: tt.errors}

			ctx := context.Background()
			var cancel context.CancelFunc
			if tt.ctxFunc != nil {
				ctx, cancel = tt.ctxFunc()
				defer cancel()
			}

			got, err := retryGenerateContent(ctx, mock, nil, nil, 0)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.checkErr != nil {
					tt.checkErr(t, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.wantResp {
					t.Errorf("got %v, want %v", got, tt.wantResp)
				}
			}

			if tt.wantCalls > 0 && mock.calls != tt.wantCalls {
				t.Errorf("calls = %d, want %d", mock.calls, tt.wantCalls)
			}
		})
	}
}

func TestRetryGenerateContentCustomMaxRetries(t *testing.T) {
	tests := []struct {
		name       string
		maxRetries int
		wantCalls  int
	}{
		{"zero falls back to default", 0, DefaultMaxRetries + 1},
		{"negative falls back to default", -5, DefaultMaxRetries + 1},
		{"explicit 1 retry = 2 calls", 1, 2},
		{"explicit 2 retries = 3 calls", 2, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := tt.wantCalls
			responses := make([]*llms.ContentResponse, n)
			errs := make([]error, n)
			for i := range errs {
				errs[i] = fmt.Errorf("503 service unavailable")
			}
			mock := &mockLLM{responses: responses, errors: errs}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			_, err := retryGenerateContent(ctx, mock, nil, nil, tt.maxRetries)
			if err == nil {
				t.Fatal("expected error after exhausting retries")
			}
			if mock.calls != tt.wantCalls {
				t.Errorf("calls = %d, want %d", mock.calls, tt.wantCalls)
			}
		})
	}
}
