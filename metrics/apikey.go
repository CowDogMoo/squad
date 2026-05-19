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

// providerAPIKeyEnv lists env vars checked for each provider, in the same
// order the runner consults them (see runner/model.go). The first entry is
// the canonical name reported back to callers for display.
var providerAPIKeyEnv = map[string][]string{
	"openai":           {"OPENAI_API_KEY"},
	"openai-responses": {"OPENAI_API_KEY"},
	"openai-compat":    {"OPENAI_COMPAT_API_KEY", "OPENAI_API_KEY"},
	"anthropic":        {"ANTHROPIC_API_KEY"},
	"gemini":           {"GOOGLE_API_KEY"},
}

// KeyStatus mirrors provider token precedence used by the runner: an
// explicitly resolved provider.token wins, then the provider-specific env
// var(s). Ollama is local and does not need an API key.
func KeyStatus(provider, providerToken string) APIKeyStatus {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "openai"
	}
	if provider == "ollama" {
		return APIKeyStatus{State: APIKeyNotNeeded, Source: APIKeySourceNone}
	}
	envVars, ok := providerAPIKeyEnv[provider]
	if !ok || len(envVars) == 0 {
		return APIKeyStatus{State: APIKeyNotNeeded, Source: APIKeySourceNone}
	}
	canonical := envVars[0]
	if strings.TrimSpace(providerToken) != "" {
		return APIKeyStatus{State: APIKeyOK, EnvVar: canonical, Source: APIKeySourceConfig}
	}
	for _, env := range envVars {
		if os.Getenv(env) != "" {
			return APIKeyStatus{State: APIKeyOK, EnvVar: env, Source: APIKeySourceEnv}
		}
	}
	return APIKeyStatus{State: APIKeyMissing, EnvVar: canonical, Source: APIKeySourceNone}
}
