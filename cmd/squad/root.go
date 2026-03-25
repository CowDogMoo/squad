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

// NewRootCmd constructs the root cobra command for the squad CLI.
// It wires subcommands, persistent flags, and completion handlers.
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

	_ = rootCmd.RegisterFlagCompletionFunc("log-level", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = rootCmd.RegisterFlagCompletionFunc("log-format", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"text", "json", "color"}, cobra.ShellCompDirectiveNoFileComp
	})

	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newPipelineCmd())
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(newGradeCmd())
	rootCmd.AddCommand(agentsCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(completionCmd)

	return rootCmd
}

// Execute runs the root command.
// It installs signal handling so the CLI exits cleanly on interruption.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
