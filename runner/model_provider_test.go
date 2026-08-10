package runner

import "testing"

// TestNormalizeProviderGoogleAlias pins the manifest-facing "google" spelling
// to the implemented "gemini" provider so buildLLM and telemetry treat them
// identically. Without the alias, fallback selection could pick a
// google-provider manifest entry and then fail at buildLLM with
// "provider not implemented: google".
func TestNormalizeProviderGoogleAlias(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"google":      "gemini",
		" Google ":    "gemini",
		"gemini":      "gemini",
		"anthropic":   "anthropic",
		"":            "",
		" OpenAI ":    "openai",
		"claude":      "claude-code",
		" Claude ":    "claude-code",
		"claude-code": "claude-code",
		"agy":         "agy",
	}
	for in, want := range cases {
		if got := normalizeProvider(in); got != want {
			t.Errorf("normalizeProvider(%q) = %q, want %q", in, got, want)
		}
	}
}
