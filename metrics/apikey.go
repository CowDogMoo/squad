package metrics

import (
	"os"
	"strings"
)

// APIKeyState is the launch-readiness state for a provider credential.
type APIKeyState string

const (
	APIKeyOK        APIKeyState = "ok"
	APIKeyMissing   APIKeyState = "missing"
	APIKeyNotNeeded APIKeyState = "notNeeded"
)

// APIKeySource describes where the usable credential came from.
type APIKeySource string

const (
	APIKeySourceConfig APIKeySource = "config"
	APIKeySourceEnv    APIKeySource = "env"
	APIKeySourceNone   APIKeySource = "none"
)

// APIKeyStatus reports whether a provider has the credential it needs.
type APIKeyStatus struct {
	State  APIKeyState
	EnvVar string
	Source APIKeySource
}

var providerAPIKeyEnv = map[string]string{
	"openai":           "OPENAI_API_KEY",
	"openai-responses": "OPENAI_API_KEY",
	"anthropic":        "ANTHROPIC_API_KEY",
	"gemini":           "GOOGLE_API_KEY",
	"nvidia":           "NVIDIA_API_KEY",
	"databricks":       "DATABRICKS_TOKEN",
}

// KeyStatus mirrors provider token precedence used by the runner: an
// explicitly resolved provider.token wins, then the provider-specific env var.
// Ollama is local and does not need an API key.
func KeyStatus(provider, providerToken string) APIKeyStatus {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "openai"
	}
	if provider == "ollama" {
		return APIKeyStatus{State: APIKeyNotNeeded, Source: APIKeySourceNone}
	}
	envVar, ok := providerAPIKeyEnv[provider]
	if !ok {
		return APIKeyStatus{State: APIKeyNotNeeded, Source: APIKeySourceNone}
	}
	if strings.TrimSpace(providerToken) != "" {
		return APIKeyStatus{State: APIKeyOK, EnvVar: envVar, Source: APIKeySourceConfig}
	}
	if os.Getenv(envVar) != "" {
		return APIKeyStatus{State: APIKeyOK, EnvVar: envVar, Source: APIKeySourceEnv}
	}
	return APIKeyStatus{State: APIKeyMissing, EnvVar: envVar, Source: APIKeySourceNone}
}
