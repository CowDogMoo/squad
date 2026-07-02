package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
)

// TestResolveModelPrecedenceDryRunWithoutCredentials pins the rule-4a-dry
// behavior: a --dry-run with a real-provider manifest and no credentials
// anywhere proceeds on the manifest's top model with a warning (bundle
// assembly never calls the model), while a real run still aborts with the
// env-var guidance.
func TestResolveModelPrecedenceDryRunWithoutCredentials(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_COMPAT_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	bundle := &agent.Bundle{Models: []agent.ModelPreference{
		{Model: "claude-sonnet-4-6", Provider: "anthropic"},
		{Model: "gemini-2.5-flash", Provider: "google"},
	}}

	opts := &RunOptions{DryRun: true}
	warn, err := ResolveModelPrecedence(context.Background(), opts, bundle)
	if err != nil {
		t.Fatalf("dry-run without credentials should not error, got: %v", err)
	}
	if opts.Model != "claude-sonnet-4-6" || opts.Provider != "anthropic" {
		t.Fatalf("dry-run should pick manifest top model, got %s (%s)", opts.Model, opts.Provider)
	}
	if !strings.Contains(warn, "Dry run proceeding") {
		t.Fatalf("expected dry-run warning, got %q", warn)
	}

	real := &RunOptions{}
	_, err = ResolveModelPrecedence(context.Background(), real, bundle)
	if err == nil {
		t.Fatal("real run without credentials should still error")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("error should list env vars, got: %v", err)
	}
}

// TestResolveModelPrecedenceDryRunBundleBaseURL covers the bundle-level
// BaseURL fallback: when neither the options nor the selected manifest entry
// carry a base URL, the dry-run pick inherits the bundle's.
func TestResolveModelPrecedenceDryRunBundleBaseURL(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_COMPAT_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	bundle := &agent.Bundle{
		BaseURL: "https://llm.internal/api",
		Models: []agent.ModelPreference{
			{Model: "claude-sonnet-4-6", Provider: "anthropic"},
		},
	}

	opts := &RunOptions{DryRun: true}
	warn, err := ResolveModelPrecedence(context.Background(), opts, bundle)
	if err != nil {
		t.Fatalf("dry-run without credentials should not error, got: %v", err)
	}
	if opts.BaseURL != "https://llm.internal/api" {
		t.Fatalf("dry-run should inherit bundle BaseURL, got %q", opts.BaseURL)
	}
	if warn == "" {
		t.Fatal("expected a dry-run warning")
	}
}
