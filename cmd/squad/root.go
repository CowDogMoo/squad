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
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "squad",
		Short: "squad - model-agnostic agent CLI",
		Long: `squad is a model-agnostic agent CLI built on LangChainGo.
It provides a clean config + logging foundation for agent workflows.`,
		Version:           version,
		PersistentPreRunE: initConfig,
	}

	rootCmd.PersistentFlags().StringP("config", "c", "", "Config file (default is $HOME/.config/squad/config.yaml)")
	rootCmd.PersistentFlags().String("log-level", "", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().String("log-format", "", "Log format (text, json, color)")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "Quiet mode - only show errors")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Verbose mode - show debug output")

	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(completionCmd)

	return rootCmd
}

// Execute runs the root command.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rootCmd := NewRootCmd()
	rootCmd.SetContext(ctx)
	return rootCmd.Execute()
}

func initConfig(cmd *cobra.Command, _ []string) error {
	var cfg *config.Config
	var err error
	// Resolve config file path directly from flags to avoid global mutable flag state
	configPath, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return fmt.Errorf("failed to read config flag: %w", err)
	}
	if configPath != "" {
		cfg, err = config.LoadFromPath(configPath)
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
	v.SetDefault("provider.token", cfg.Provider.Token)
	v.SetDefault("provider.num_ctx", cfg.Provider.NumCtx)
	v.SetDefault("model.default", cfg.Model.Default)
	v.SetDefault("model.temperature", cfg.Model.Temperature)
	v.SetDefault("model.max_tokens", cfg.Model.MaxTokens)

	v.SetEnvPrefix("SQUAD")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Bind persistent (root) flags.
	if err := v.BindPFlag("config", cmd.Root().PersistentFlags().Lookup("config")); err != nil {
		return fmt.Errorf("failed to bind config flag: %w", err)
	}
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

	// Bind run command flags so Viper can resolve them via env/config.
	runCmd := findRunCmd(cmd.Root())
	if runCmd == nil {
		return fmt.Errorf("run command not found for flag binding")
	}
	if err := bindRunFlags(runCmd, v); err != nil {
		return err
	}

	logLevel := v.GetString("log.level")
	logFormat := v.GetString("log.format")
	quiet := v.GetBool("quiet")
	verbose := v.GetBool("verbose")

	if quiet && verbose {
		return fmt.Errorf("--quiet and --verbose cannot be used together")
	}

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
	cfg.Provider.Token = v.GetString("provider.token")
	cfg.Provider.NumCtx = v.GetInt("provider.num_ctx")
	cfg.Model.Default = v.GetString("model.default")
	cfg.Model.Temperature = v.GetFloat64("model.temperature")
	cfg.Model.MaxTokens = v.GetInt("model.max_tokens")

	logger := logging.FromContext(cmd.Context())
	ctx := withConfig(cmd.Context(), cfg)
	ctx = withViper(ctx, v)
	ctx = logging.WithLogger(ctx, logger)
	cmd.SetContext(ctx)

	return nil
}

func findRunCmd(root *cobra.Command) *cobra.Command {
	for _, sub := range root.Commands() {
		if sub.Name() == "run" {
			return sub
		}
	}
	return nil
}
