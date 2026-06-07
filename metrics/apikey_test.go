package metrics

import "testing"

func TestKeyStatusProviderEnvTable(t *testing.T) {
	cases := []struct {
		provider string
		envVar   string
	}{
		{"", "OPENAI_API_KEY"},
		{"openai", "OPENAI_API_KEY"},
		{"openai-responses", "OPENAI_API_KEY"},
		{"openai-compat", "OPENAI_COMPAT_API_KEY"},
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"gemini", "GOOGLE_API_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("OPENAI_COMPAT_API_KEY", "")
			t.Setenv("ANTHROPIC_API_KEY", "")
			t.Setenv("GOOGLE_API_KEY", "")
			got := KeyStatus(tc.provider, "")
			if got.State != APIKeyMissing || got.EnvVar != tc.envVar || got.Source != APIKeySourceNone {
				t.Fatalf("KeyStatus(%q) = %+v, want missing %s from none", tc.provider, got, tc.envVar)
			}
		})
	}
}

func TestKeyStatusOpenAICompatFallback(t *testing.T) {
	// Compat-specific var wins when both are set.
	t.Setenv("OPENAI_COMPAT_API_KEY", "compat-token")
	t.Setenv("OPENAI_API_KEY", "openai-token")
	got := KeyStatus("openai-compat", "")
	if got.State != APIKeyOK || got.EnvVar != "OPENAI_COMPAT_API_KEY" || got.Source != APIKeySourceEnv {
		t.Fatalf("compat-specific env should win: got %+v", got)
	}

	// Falls back to OPENAI_API_KEY when compat-specific is unset.
	t.Setenv("OPENAI_COMPAT_API_KEY", "")
	got = KeyStatus("openai-compat", "")
	if got.State != APIKeyOK || got.EnvVar != "OPENAI_API_KEY" || got.Source != APIKeySourceEnv {
		t.Fatalf("fallback to OPENAI_API_KEY: got %+v", got)
	}
}

func TestKeyStatusPrecedence(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-token")
	got := KeyStatus("openai", "config-token")
	if got.State != APIKeyOK || got.Source != APIKeySourceConfig || got.EnvVar != "OPENAI_API_KEY" {
		t.Fatalf("config token should win: got %+v", got)
	}

	got = KeyStatus("openai", "")
	if got.State != APIKeyOK || got.Source != APIKeySourceEnv || got.EnvVar != "OPENAI_API_KEY" {
		t.Fatalf("env token should be used: got %+v", got)
	}
}

func TestKeyStatusOllamaNotNeeded(t *testing.T) {
	got := KeyStatus("ollama", "")
	if got.State != APIKeyNotNeeded || got.EnvVar != "" || got.Source != APIKeySourceNone {
		t.Fatalf("ollama status = %+v, want not-needed/no env", got)
	}
}

func TestKeyStatusUnknownProviderNotNeeded(t *testing.T) {
	got := KeyStatus("not-a-real-provider", "")
	if got.State != APIKeyNotNeeded || got.EnvVar != "" || got.Source != APIKeySourceNone {
		t.Fatalf("unknown provider status = %+v, want not-needed/no env", got)
	}
}
