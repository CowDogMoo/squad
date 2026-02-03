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

package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

const (
	FilePermReadWrite    = 0o644
	DirPermReadWriteExec = 0o755
)

// Config represents the global squad configuration.
type Config struct {
	Log      LogConfig      `mapstructure:"log" yaml:"log"`
	Provider ProviderConfig `mapstructure:"provider" yaml:"provider"`
	Model    ModelConfig    `mapstructure:"model" yaml:"model"`
}

// LogConfig holds logging configuration.
type LogConfig struct {
	Level  string `mapstructure:"level" yaml:"level"`
	Format string `mapstructure:"format" yaml:"format"`
}

// ProviderConfig holds provider defaults.
type ProviderConfig struct {
	Default               string `mapstructure:"default" yaml:"default"`
	BaseURL               string `mapstructure:"base_url" yaml:"base_url"`
	Organization          string `mapstructure:"organization" yaml:"organization"`
	APIVersion            string `mapstructure:"api_version" yaml:"api_version"`
	APIType               string `mapstructure:"api_type" yaml:"api_type"`
	OpenAICompatMaxTokens bool   `mapstructure:"openai_compat_max_tokens" yaml:"openai_compat_max_tokens"`
	Token                 string `mapstructure:"token" yaml:"token"`
}

// ModelConfig holds model defaults.
type ModelConfig struct {
	Default     string  `mapstructure:"default" yaml:"default"`
	Temperature float64 `mapstructure:"temperature" yaml:"temperature"`
	MaxTokens   int     `mapstructure:"max_tokens" yaml:"max_tokens"`
}

// Defaults returns a config populated with sensible defaults.
func Defaults() *Config {
	cfg := &Config{}
	cfg.Log.Level = "info"
	cfg.Log.Format = "text"
	cfg.Provider.Default = "openai"
	cfg.Provider.BaseURL = ""
	cfg.Provider.Organization = ""
	cfg.Provider.APIVersion = ""
	cfg.Provider.APIType = ""
	cfg.Provider.OpenAICompatMaxTokens = false
	cfg.Model.Default = ""
	cfg.Model.Temperature = 0.2
	cfg.Model.MaxTokens = 1024
	return cfg
}

// Load loads config from standard locations with env and defaults.
func Load() (*Config, error) {
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
func LoadFromPath(path string) (*Config, error) {
	return loadConfigWithViper(func(v *viper.Viper) error {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return err
		}
		return nil
	})
}

func loadConfigWithViper(setup func(*viper.Viper) error) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")

	setDefaults(v)

	v.SetEnvPrefix("SQUAD")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := setup(v); err != nil {
		return nil, fmt.Errorf("config load failed: %w", err)
	}

	cfg := Defaults()
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("config unmarshal failed: %w", err)
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")
	v.SetDefault("provider.default", "openai")
	v.SetDefault("provider.base_url", "")
	v.SetDefault("provider.organization", "")
	v.SetDefault("provider.api_version", "")
	v.SetDefault("provider.api_type", "")
	v.SetDefault("provider.openai_compat_max_tokens", false)
	v.SetDefault("model.default", "")
	v.SetDefault("model.temperature", 0.2)
	v.SetDefault("model.max_tokens", 1024)
}
