/*
Copyright © 2026 Jayson Grace <jayson.e.grace@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

// Package config loads and validates squad configuration settings.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

const (
	// FilePermReadWrite is the file mode used for config files.
	FilePermReadWrite = 0o600
	// DirPermReadWriteExec is the directory mode used for config folders.
	DirPermReadWriteExec = 0o700
)

// Config represents the global squad configuration.
// Its zero value is not useful; use [Defaults] or [Load] to initialize it.
type Config struct {
	Log      LogConfig      `mapstructure:"log" yaml:"log"`
	Provider ProviderConfig `mapstructure:"provider" yaml:"provider"`
	Model    ModelConfig    `mapstructure:"model" yaml:"model"`
	Agents   AgentsConfig   `mapstructure:"agents" yaml:"agents"`
	Otel     OtelConfig     `mapstructure:"otel" yaml:"otel"`
	Run      RunConfig      `mapstructure:"run" yaml:"run"`
}

// RunConfig holds run-time defaults that apply to every agent invocation.
type RunConfig struct {
	// Isolation is the default isolation mode when neither the CLI flag nor
	// the agent manifest specifies one. Valid values: "" (none) or "worktree".
	Isolation string `mapstructure:"isolation" yaml:"isolation"`
}

// OtelConfig holds OpenTelemetry configuration.
type OtelConfig struct {
	// Endpoint is the OTLP gRPC endpoint (e.g., "localhost:4317").
	// An empty string disables telemetry export.
	Endpoint string `mapstructure:"endpoint" yaml:"endpoint"`
}

// AgentsConfig holds agent source configuration.
type AgentsConfig struct {
	// CacheDir is where cloned git repositories are cached.
	CacheDir string `mapstructure:"cache_dir" yaml:"cache_dir"`
	// Repositories maps repository names to Git URLs.
	// Example:
	//   repositories:
	//     official: https://github.com/cowdogmoo/squad-agents.git
	//     private: git@github.com:myorg/private-agents.git
	Repositories map[string]string `mapstructure:"repositories" yaml:"repositories"`
	// LocalPaths lists local directories to search for agents.
	// Example:
	//   local_paths:
	//     - /opt/shared/agents
	//     - ~/dev/my-agents
	LocalPaths []string `mapstructure:"local_paths" yaml:"local_paths"`
}

// LogConfig holds logging configuration.
type LogConfig struct {
	// Level is the minimum log level: debug, info, warn, or error.
	Level string `mapstructure:"level" yaml:"level"`
	// Format is the log output format: text or json.
	Format string `mapstructure:"format" yaml:"format"`
}

// ProviderConfig holds provider defaults.
type ProviderConfig struct {
	// Default is the provider name used when none is specified on the
	// command line or in an agent manifest (e.g., "openai", "anthropic").
	Default string `mapstructure:"default" yaml:"default"`
	// BaseURL overrides the provider's default API endpoint URL.
	BaseURL string `mapstructure:"base_url" yaml:"base_url"`
	// Organization is the OpenAI organization ID, if required.
	Organization string `mapstructure:"organization" yaml:"organization"`
	// APIVersion is used by Azure OpenAI and other versioned APIs.
	APIVersion string `mapstructure:"api_version" yaml:"api_version"`
	// APIType selects the API variant (e.g., "azure" for Azure OpenAI).
	APIType string `mapstructure:"api_type" yaml:"api_type"`
	// OpenAICompatMaxTokens enforces max_tokens in OpenAI-compatible
	// requests even when the provider does not strictly require it.
	OpenAICompatMaxTokens bool `mapstructure:"openai_compat_max_tokens" yaml:"openai_compat_max_tokens"`
	// Token is the API key or bearer token. It supports command
	// substitution ($(cmd)) and env-var references ($VAR).
	Token string `mapstructure:"token" yaml:"token"`
	// NumCtx is the context window size for Ollama models.
	NumCtx int `mapstructure:"num_ctx" yaml:"num_ctx"`
}

// ModelConfig holds model defaults.
type ModelConfig struct {
	// Default is the model identifier used when none is specified on the
	// command line or in an agent manifest.
	Default string `mapstructure:"default" yaml:"default"`
	// Temperature controls sampling randomness (0.0 = deterministic).
	Temperature float64 `mapstructure:"temperature" yaml:"temperature"`
	// MaxTokens is the default output-token budget per request.
	MaxTokens int `mapstructure:"max_tokens" yaml:"max_tokens"`
	// ReasoningPrefixes lists model name prefixes that support extended
	// reasoning; requests to these models use higher token budgets.
	ReasoningPrefixes []string `mapstructure:"reasoning_prefixes" yaml:"reasoning_prefixes"`
}

// Defaults returns a config populated with sensible defaults.
// All default values are defined once in SetDefaults; this function
// derives the struct from that single source of truth.
func Defaults() *Config {
	v := viper.New()
	SetDefaults(v)
	cfg := &Config{}
	_ = v.Unmarshal(cfg)
	return cfg
}

// Load loads config from standard locations with env and defaults.
// It returns the resolved Config and the Viper instance that produced it,
// so callers can bind additional flags without recreating the precedence chain.
func Load() (*Config, *viper.Viper, error) {
	return loadConfigWithViper(func(v *viper.Viper) error {
		v.SetConfigName("config")
		v.SetConfigType("yaml")

		for _, dir := range GetConfigDirs() {
			v.AddConfigPath(dir)
		}
		v.AddConfigPath(".")

		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return err
			}
		}

		return nil
	})
}

// LoadFromPath loads config from an explicit path.
// It returns the resolved Config and the Viper instance that produced it.
func LoadFromPath(path string) (*Config, *viper.Viper, error) {
	return loadConfigWithViper(func(v *viper.Viper) error {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return err
		}
		return nil
	})
}

func loadConfigWithViper(setup func(*viper.Viper) error) (*Config, *viper.Viper, error) {
	v := viper.New()
	v.SetConfigType("yaml")

	SetDefaults(v)

	v.SetEnvPrefix("SQUAD")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := setup(v); err != nil {
		return nil, nil, fmt.Errorf("config load failed: %w", err)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, nil, fmt.Errorf("config unmarshal failed: %w", err)
	}

	// Resolve dynamic values (env vars, command substitution) in the token field.
	if cfg.Provider.Token != "" {
		resolved, err := ResolveValue(cfg.Provider.Token)
		if err != nil {
			return nil, nil, fmt.Errorf("resolving provider.token: %w", err)
		}
		cfg.Provider.Token = resolved
	}

	return cfg, v, nil
}

// SetDefaults registers all hardcoded default values on a Viper instance.
func SetDefaults(v *viper.Viper) {
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")
	v.SetDefault("provider.default", "openai")
	v.SetDefault("provider.base_url", "")
	v.SetDefault("provider.organization", "")
	v.SetDefault("provider.api_version", "")
	v.SetDefault("provider.api_type", "")
	v.SetDefault("provider.openai_compat_max_tokens", false)
	v.SetDefault("provider.num_ctx", 32768)
	v.SetDefault("model.default", "")
	v.SetDefault("model.temperature", 0.2)
	v.SetDefault("model.max_tokens", 1024)
	v.SetDefault("model.reasoning_prefixes", []string{"gpt-5"})
	v.SetDefault("run.max_cost", 5.0)
	v.SetDefault("run.isolation", "")
	v.SetDefault("agents.cache_dir", "")
	v.SetDefault("agents.repositories", map[string]string{
		"official": "https://github.com/cowdogmoo/squad-agents.git",
	})
	v.SetDefault("agents.local_paths", []string{})
	v.SetDefault("otel.endpoint", "")
}
