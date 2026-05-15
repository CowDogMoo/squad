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
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"gemini", "GOOGLE_API_KEY"},
		{"nvidia", "NVIDIA_API_KEY"},
		{"databricks", "DATABRICKS_TOKEN"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			t.Setenv(tc.envVar, "")
			got := KeyStatus(tc.provider, "")
			if got.State != APIKeyMissing || got.EnvVar != tc.envVar || got.Source != APIKeySourceNone {
				t.Fatalf("KeyStatus(%q) = %+v, want missing %s from none", tc.provider, got, tc.envVar)
			}
		})
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
