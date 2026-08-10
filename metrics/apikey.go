package metrics

import (
	"os"
	"os/exec"
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
	APIKeySourceBinary APIKeySource = "binary"
	APIKeySourceNone   APIKeySource = "none"
)

// AgenticCLIBinaries maps subscription-based agentic CLI providers to the
// binary each one shells out to. These providers need no API key — the CLI
// carries its own authentication (typically a subscription login) — so
// credential detection checks that the binary is installed instead.
var AgenticCLIBinaries = map[string]string{
	"claude-code": "claude",
	"agy":         "agy",
}

// IsAgenticCLI reports whether provider runs through a local agentic CLI
// binary rather than a direct model API.
func IsAgenticCLI(provider string) bool {
	_, ok := AgenticCLIBinaries[strings.ToLower(strings.TrimSpace(provider))]
	return ok
}

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
	// "google" is the provider name agent manifests commonly use for
	// Gemini models; the runner aliases it to "gemini" (see
	// runner.normalizeProvider). Listing it here keeps credential
	// detection consistent — without this entry an unknown provider is
	// treated as "no key needed" and fallback selection picks it even
	// when GOOGLE_API_KEY is absent.
	"google": {"GOOGLE_API_KEY"},
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
	// Agentic CLI providers authenticate through the installed binary, so
	// "credential present" means "binary on PATH". This lets manifest model
	// walks fall back to an API provider when the CLI isn't installed.
	if bin, ok := AgenticCLIBinaries[provider]; ok {
		if _, err := exec.LookPath(bin); err != nil {
			return APIKeyStatus{State: APIKeyMissing, EnvVar: bin + " CLI", Source: APIKeySourceNone}
		}
		return APIKeyStatus{State: APIKeyOK, EnvVar: bin + " CLI", Source: APIKeySourceBinary}
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
