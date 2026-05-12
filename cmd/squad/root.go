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

// Package main implements the squad CLI for building, sharing, and running AI agents.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/telemetry"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewRootCmd constructs the root cobra command for the squad CLI.
// It wires subcommands, persistent flags, and completion handlers.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "squad",
		Short: "Fabric for AI agents",
		Long: `Build, share, and run AI agents from the command line.
Define an agent in markdown and YAML, point it at any LLM, and turn it loose on a codebase.`,
		Version:           version,
		PersistentPreRunE: initConfig,
	}

	rootCmd.PersistentFlags().StringP("config", "c", "", "Config file (default is $HOME/.config/squad/config.yaml)")
	rootCmd.PersistentFlags().String("log-level", "", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().String("log-format", "", "Log format (text, json, color)")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "Quiet mode - only show errors")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Verbose mode - show debug output")
	rootCmd.PersistentFlags().String("otel-endpoint", "", "OpenTelemetry OTLP endpoint (e.g. localhost:4318). Enables trace export.")

	_ = rootCmd.RegisterFlagCompletionFunc("log-level", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = rootCmd.RegisterFlagCompletionFunc("log-format", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"text", "json", "color"}, cobra.ShellCompDirectiveNoFileComp
	})

	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(newGradeCmd())
	rootCmd.AddCommand(agentsCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(completionCmd)
	rootCmd.AddCommand(newRoutineCmd())
	rootCmd.AddCommand(newRoutinedCmd())

	return rootCmd
}

// otelShutdown holds the current telemetry shutdown function. It is package-level
// so that initConfig can replace it when --otel-endpoint overrides the env-based
// provider, and Execute's defer always calls the latest one.
var otelShutdown func(context.Context) error

// Execute runs the root command.
// It installs signal handling so the CLI exits cleanly on interruption.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Initialize telemetry early so spans are captured from the start.
	// The endpoint flag isn't parsed yet, so Init reads OTEL_EXPORTER_OTLP_ENDPOINT
	// from the environment. If --otel-endpoint is provided, a second Init happens
	// in initConfig (which replaces the provider).
	var err error
	otelShutdown, err = telemetry.Init(ctx, "squad", "")
	if err != nil {
		logging.Warn("failed to initialize telemetry: %v", err)
	}
	defer func() {
		if otelShutdown != nil {
			// Use a short deadline so a broken exporter endpoint doesn't
			// block process exit (the batch exporter would otherwise retry
			// until its own 30 s timeout expires).
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := otelShutdown(shutCtx); err != nil {
				logging.Warn("telemetry shutdown error: %v", err)
			}
		}
	}()

	rootCmd := NewRootCmd()
	rootCmd.SetContext(ctx)
	return rootCmd.Execute()
}

func initConfig(cmd *cobra.Command, _ []string) error {
	var cfg *config.Config
	var v *viper.Viper
	var err error
	// Resolve config file path directly from flags to avoid global mutable flag state
	configPath, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return fmt.Errorf("failed to read config flag: %w", err)
	}
	if configPath != "" {
		cfg, v, err = config.LoadFromPath(configPath)
	} else {
		cfg, v, err = config.Load()
	}

	if err != nil {
		logging.Warn("failed to load config, using defaults: %v", err)
		cfg = config.Defaults()
		v = viper.New()
		config.SetDefaults(v)
		v.SetEnvPrefix("SQUAD")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		v.AutomaticEnv()
	}

	if err := bindPersistentFlags(cmd, v); err != nil {
		return err
	}
	// Pipeline command flags are resolved directly via flagOrViper, no binding needed.

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

	// Re-unmarshal so flag/env overrides are reflected in the config struct.
	if err := v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("failed to apply config overrides: %w", err)
	}

	// Re-initialize telemetry if --otel-endpoint was explicitly provided.
	// This replaces the package-level otelShutdown so Execute's defer
	// flushes the correct provider.
	otelEndpoint := v.GetString("otel.endpoint")
	if otelEndpoint != "" {
		newShutdown, otelErr := telemetry.Init(cmd.Context(), "squad", otelEndpoint)
		if otelErr != nil {
			logging.Warn("failed to re-initialize telemetry with endpoint %s: %v", otelEndpoint, otelErr)
		} else {
			otelShutdown = newShutdown
		}
	}

	logger := logging.FromContext(cmd.Context())
	ctx := withConfig(cmd.Context(), cfg)
	ctx = withViper(ctx, v)
	ctx = logging.WithLogger(ctx, logger)
	cmd.SetContext(ctx)

	return nil
}

// bindPersistentFlags binds root persistent flags and run-command flags to Viper.
func bindPersistentFlags(cmd *cobra.Command, v *viper.Viper) error {
	bindings := []struct {
		key  string
		flag string
	}{
		{"config", "config"},
		{"log.level", "log-level"},
		{"log.format", "log-format"},
		{"quiet", "quiet"},
		{"verbose", "verbose"},
		{"otel.endpoint", "otel-endpoint"},
	}
	for _, b := range bindings {
		if err := v.BindPFlag(b.key, cmd.Root().PersistentFlags().Lookup(b.flag)); err != nil {
			return fmt.Errorf("failed to bind %s flag: %w", b.flag, err)
		}
	}

	runCmd := findRunCmd(cmd.Root())
	if runCmd == nil {
		return fmt.Errorf("run command not found for flag binding")
	}
	return bindRunFlags(runCmd, v)
}

func findRunCmd(root *cobra.Command) *cobra.Command {
	for _, sub := range root.Commands() {
		if sub.Name() == "run" {
			return sub
		}
	}
	return nil
}
