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

// Package main implements the squad CLI for model-agnostic agent workflows.
package main

import (
	"context"
	"fmt"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Context key type for storing config.
type configKeyType struct{}

var (
	configKey = configKeyType{}
	cfgFile   string
)

var rootCmd = &cobra.Command{
	Use:   "squad",
	Short: "squad - model-agnostic agent CLI",
	Long: `squad is a model-agnostic agent CLI built on LangChainGo.
It provides a clean config + logging foundation for agent workflows.`,
	Version:           version,
	PersistentPreRunE: initConfig,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "Config file (default is $HOME/.config/squad/config.yaml)")
	rootCmd.PersistentFlags().String("log-level", "", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().String("log-format", "", "Log format (text, json, color)")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "Quiet mode - only show errors")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Verbose mode - show debug output")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(completionCmd)
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func configFromContext(cmd *cobra.Command) *config.Config {
	if cfg, ok := cmd.Context().Value(configKey).(*config.Config); ok {
		return cfg
	}
	return nil
}

func initConfig(cmd *cobra.Command, _ []string) error {
	var cfg *config.Config
	var err error
	if cfgFile != "" {
		cfg, err = config.LoadFromPath(cfgFile)
	} else {
		cfg, err = config.Load()
	}

	if err != nil {
		logging.Warn("failed to load config, using defaults: %v", err)
		cfg = config.Defaults()
	}

	v := viper.New()

	v.SetDefault("log.level", cfg.Log.Level)
	v.SetDefault("log.format", cfg.Log.Format)
	v.SetDefault("provider.default", cfg.Provider.Default)
	v.SetDefault("provider.base_url", cfg.Provider.BaseURL)
	v.SetDefault("provider.organization", cfg.Provider.Organization)
	v.SetDefault("provider.api_version", cfg.Provider.APIVersion)
	v.SetDefault("provider.api_type", cfg.Provider.APIType)
	v.SetDefault("provider.openai_compat_max_tokens", cfg.Provider.OpenAICompatMaxTokens)
	v.SetDefault("model.default", cfg.Model.Default)
	v.SetDefault("model.temperature", cfg.Model.Temperature)
	v.SetDefault("model.max_tokens", cfg.Model.MaxTokens)

	v.SetEnvPrefix("SQUAD")
	v.AutomaticEnv()

	if err := v.BindPFlag("log.level", cmd.Root().PersistentFlags().Lookup("log-level")); err != nil {
		return fmt.Errorf("failed to bind log-level flag: %w", err)
	}
	if err := v.BindPFlag("log.format", cmd.Root().PersistentFlags().Lookup("log-format")); err != nil {
		return fmt.Errorf("failed to bind log-format flag: %w", err)
	}
	if err := v.BindPFlag("quiet", cmd.Root().PersistentFlags().Lookup("quiet")); err != nil {
		return fmt.Errorf("failed to bind quiet flag: %w", err)
	}
	if err := v.BindPFlag("verbose", cmd.Root().PersistentFlags().Lookup("verbose")); err != nil {
		return fmt.Errorf("failed to bind verbose flag: %w", err)
	}

	logLevel := v.GetString("log.level")
	logFormat := v.GetString("log.format")
	quiet := v.GetBool("quiet")
	verbose := v.GetBool("verbose")

	if err := logging.Initialize(logLevel, logFormat, quiet, verbose); err != nil {
		return fmt.Errorf("failed to initialize logging: %w", err)
	}

	cfg.Log.Level = logLevel
	cfg.Log.Format = logFormat
	cfg.Provider.Default = v.GetString("provider.default")
	cfg.Provider.BaseURL = v.GetString("provider.base_url")
	cfg.Provider.Organization = v.GetString("provider.organization")
	cfg.Provider.APIVersion = v.GetString("provider.api_version")
	cfg.Provider.APIType = v.GetString("provider.api_type")
	cfg.Provider.OpenAICompatMaxTokens = v.GetBool("provider.openai_compat_max_tokens")
	cfg.Model.Default = v.GetString("model.default")
	cfg.Model.Temperature = v.GetFloat64("model.temperature")
	cfg.Model.MaxTokens = v.GetInt("model.max_tokens")

	logger := logging.FromContext(cmd.Context())
	ctx := context.WithValue(cmd.Context(), configKey, cfg)
	ctx = logging.WithLogger(ctx, logger)
	cmd.SetContext(ctx)

	return nil
}
