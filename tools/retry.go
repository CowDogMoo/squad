package tools

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/telemetry"
	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// DefaultMaxRetries is the number of additional attempts after a failed
	// LLM call when no explicit override is supplied. Total attempts =
	// DefaultMaxRetries + 1.
	DefaultMaxRetries = 3
	retryBaseDelay    = 2 * time.Second
	retryMaxDelay     = 30 * time.Second
)

// retryableErrorCodes lists llms.ErrorCode values that warrant a retry.
var retryableErrorCodes = map[llms.ErrorCode]bool{
	llms.ErrCodeProviderUnavailable: true,
	llms.ErrCodeRateLimit:           true,
	llms.ErrCodeTimeout:             true,
}

// errorMapping pairs string patterns with an llms.ErrorCode.
type errorMapping struct {
	patterns []string
	code     llms.ErrorCode
}

// errorMappings classifies raw error strings into structured error codes.
// Patterns are checked case-insensitively against err.Error().
var errorMappings = []errorMapping{
	{patterns: []string{"invalid api key", "authentication failed", "401"}, code: llms.ErrCodeAuthentication},
	{patterns: []string{"rate limit", "too many requests", "429"}, code: llms.ErrCodeRateLimit},
	{patterns: []string{"model not found", "invalid model"}, code: llms.ErrCodeResourceNotFound},
	{patterns: []string{"maximum tokens", "context window"}, code: llms.ErrCodeTokenLimit},
	{patterns: []string{"content blocked", "safety violation"}, code: llms.ErrCodeContentFilter},
	{patterns: []string{"credit limit", "quota exceeded"}, code: llms.ErrCodeQuotaExceeded},
	{patterns: []string{"invalid request", "400"}, code: llms.ErrCodeInvalidRequest},
	{patterns: []string{"overloaded", "503", "service unavailable", "500", "internal server error"}, code: llms.ErrCodeProviderUnavailable},
}

// transientNetworkPatterns are substrings of raw error messages that indicate
// transient network-level failures worth retrying. The first group covers raw
// net errors; the second covers the generic strings langchaingo's OpenAI client
// substitutes for them in sanitizeHTTPError (which discards the underlying
// "connection refused" etc. before we ever see it).
var transientNetworkPatterns = []string{
	"connection reset",
	"connection refused",
	"connection timed out",
	"broken pipe",
	"eof",
	"temporary failure",
	"no such host",
	"i/o timeout",
	"network is unreachable",
	"tls handshake",
	// langchaingo sanitizeHTTPError replacements:
	"network error",   // "network error: failed to reach API server"
	"failed to reach", // belt-and-suspenders for the same message
	"request timeout", // "request timeout: ..." (deadline or net timeout)
}

// classifyError extracts or infers an llms.Error from any error.
// If err already wraps an *llms.Error, it is returned directly.
// Otherwise the error string is matched against known patterns.
func classifyError(err error) *llms.Error {
	if err == nil {
		return nil
	}
	var llmErr *llms.Error
	if errors.As(err, &llmErr) {
		return llmErr
	}

	lower := strings.ToLower(err.Error())
	for _, m := range errorMappings {
		for _, p := range m.patterns {
			if strings.Contains(lower, p) {
				return llms.NewError(m.code, "", err.Error()).WithCause(err)
			}
		}
	}
	return llms.NewError(llms.ErrCodeUnknown, "", err.Error()).WithCause(err)
}

// isRetryable reports whether an error is transient and should be retried.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	classified := classifyError(err)
	if retryableErrorCodes[classified.Code] {
		return true
	}

	if classified.Code == llms.ErrCodeUnknown {
		lower := strings.ToLower(err.Error())
		for _, p := range transientNetworkPatterns {
			if strings.Contains(lower, p) {
				return true
			}
		}
	}
	return false
}

// retryGenerateContent wraps llm.GenerateContent with retry and exponential
// backoff for transient errors. It returns the first successful response or
// the last error after exhausting retries. A maxRetries <= 0 falls back to
// DefaultMaxRetries.
func retryGenerateContent(ctx context.Context, llm llms.Model, messages []llms.MessageContent, callOpts []llms.CallOption, maxRetries int) (*llms.ContentResponse, error) {
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}
	ctx, span := telemetry.Tracer().Start(ctx, "llm.generate",
		trace.WithAttributes(
			attribute.Int("llm.retry.max_attempts", maxRetries+1),
		),
	)
	defer span.End()

	var lastErr error
	attempts := maxRetries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Log a heartbeat every 30s while waiting for the API so the
		// operator knows the process isn't stuck.
		callStart := time.Now()
		heartbeatDone := make(chan struct{})
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-heartbeatDone:
					return
				case <-ctx.Done():
					return
				case t := <-ticker.C:
					elapsed := t.Sub(callStart).Round(time.Second)
					logging.InfoContext(ctx, "  … waiting for model response (%s elapsed)", elapsed)
				}
			}
		}()

		resp, err := llm.GenerateContent(ctx, messages, callOpts...)
		close(heartbeatDone)
		if err == nil {
			span.SetAttributes(attribute.Int("llm.retry.attempts", attempt+1))
			if attempt > 0 {
				logging.InfoContext(ctx, "LLM call succeeded on attempt %d/%d", attempt+1, attempts)
			}
			return resp, nil
		}
		lastErr = err

		if !isRetryable(err) {
			classified := classifyError(err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.SetAttributes(
				attribute.Int("llm.retry.attempts", attempt+1),
				attribute.String("llm.error.code", string(classified.Code)),
			)
			logging.InfoContext(ctx, "LLM call failed (non-retryable, code=%s): %v", classified.Code, err)
			return nil, err
		}

		if attempt == maxRetries {
			break
		}

		delay := backoffDelay(attempt)
		classified := classifyError(err)
		span.AddEvent("llm.retry", trace.WithAttributes(
			attribute.Int("llm.retry.attempt", attempt+1),
			attribute.String("llm.error.code", string(classified.Code)),
			attribute.String("llm.retry.delay", delay.String()),
		))
		logging.InfoContext(ctx, "LLM call failed (attempt %d/%d, code=%s, retrying in %s): %v",
			attempt+1, attempts, classified.Code, delay.Round(time.Millisecond), err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	classified := classifyError(lastErr)
	span.RecordError(lastErr)
	span.SetStatus(codes.Error, lastErr.Error())
	span.SetAttributes(
		attribute.Int("llm.retry.attempts", attempts),
		attribute.String("llm.error.code", string(classified.Code)),
	)
	logging.InfoContext(ctx, "LLM call failed after %d attempts (code=%s): %v", attempts, classified.Code, lastErr)
	return nil, lastErr
}

// backoffDelay returns the wait duration for the given zero-indexed attempt
// using exponential backoff: retryBaseDelay * 2^attempt, capped at retryMaxDelay.
func backoffDelay(attempt int) time.Duration {
	delay := time.Duration(float64(retryBaseDelay) * math.Pow(2, float64(attempt)))
	if delay > retryMaxDelay {
		delay = retryMaxDelay
	}
	return delay
}
