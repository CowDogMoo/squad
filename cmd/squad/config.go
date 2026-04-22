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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:     "config",
	Aliases: []string{"cfg"},
	Short:   "Manage squad configuration",
	Long: `Manage squad's global configuration file.

Configuration locations (searched in order):
1. $XDG_CONFIG_HOME/squad/config.yaml (typically ~/.config/squad/config.yaml)
2. ./config.yaml (current directory)

Configuration precedence (highest to lowest):
1. CLI flags
2. Environment variables (SQUAD_*)
3. Configuration file
4. Built-in defaults`,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize default configuration file",
	Long: `Create a new configuration file with default values.

This will create an XDG-compliant config file at:
  $XDG_CONFIG_HOME/squad/config.yaml (typically ~/.config/squad/config.yaml)

If a legacy config exists at ~/.squad/config.yaml, you'll be notified.
If the file already exists, it will be overwritten only with --force.`,
	RunE: runConfigInit,
	Args: cobra.NoArgs,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Long: `Display the current configuration with values from all sources.

This shows the effective configuration after merging:
- Built-in defaults
- Configuration file values
- Environment variables
- CLI flag overrides`,
	RunE: runConfigShow,
	Args: cobra.NoArgs,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show configuration file path",
	RunE:  runConfigPath,
	Args:  cobra.NoArgs,
}

var configSetCmd = &cobra.Command{
	Use:   "set KEY VALUE",
	Short: "Set a configuration value",
	Long: `Set a configuration value in the config file.

Examples:
  squad config set log.level debug
  squad config set provider.default openai
  squad config set model.default gpt-4.1-mini

Use dot notation to set nested values.`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:   "get KEY",
	Short: "Get a configuration value",
	Long: `Get a specific configuration value.

Examples:
  squad config get log.level
  squad config get provider.default
  squad config get model.default`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigGet,
}

func init() {
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)

	configInitCmd.Flags().BoolP("force", "f", false, "Overwrite existing config file")
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	configPath, err := config.ConfigFile("config.yaml")
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}

	ctx := cmd.Context()
	if _, err := os.Stat(configPath); err == nil {
		force, err := cmd.Flags().GetBool("force")
		if err != nil {
			return fmt.Errorf("failed to get force flag: %w", err)
		}
		if !force {
			return fmt.Errorf("config file already exists at %s (use --force to overwrite)", configPath)
		}
		logging.WarnContext(ctx, "Overwriting existing config file at %s", configPath)
		logging.WarnContext(ctx, "This will reset all custom settings to defaults!")
	}

	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available in context")
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), config.DirPermReadWriteExec); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.WriteFile(configPath, data, config.FilePermReadWrite); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	logging.InfoContext(ctx, "Configuration file created at: %s", configPath)
	logging.InfoContext(ctx, "Edit this file to customize your squad settings")

	return nil
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available in context")
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	w := cmd.OutOrStdout()
	if _, err := fmt.Fprintln(w, "# Current Squad Configuration"); err != nil {
		return fmt.Errorf("failed to write config header: %w", err)
	}
	if _, err := fmt.Fprintln(w, "# Sources: defaults -> config file -> environment variables -> CLI flags"); err != nil {
		return fmt.Errorf("failed to write config header: %w", err)
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return fmt.Errorf("failed to write config header: %w", err)
	}
	if _, err := fmt.Fprint(w, string(data)); err != nil {
		return fmt.Errorf("failed to write config data: %w", err)
	}
	return nil
}

func runConfigPath(cmd *cobra.Command, args []string) error {
	path, err := config.ConfigFile("config.yaml")
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
	return err
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	configPath, err := config.ConfigFile("config.yaml")
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}

	cfg, _, err := config.LoadFromPath(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
		return fmt.Errorf("failed to load config into viper: %w", err)
	}

	var value any
	if err := yaml.Unmarshal([]byte(args[1]), &value); err != nil {
		return fmt.Errorf("failed to parse value: %w", err)
	}
	v.Set(args[0], value)

	// Unmarshal back into typed config to preserve structure and tags
	var newCfg config.Config
	if err := v.Unmarshal(&newCfg); err != nil {
		return fmt.Errorf("failed to unmarshal updated config: %w", err)
	}

	out, err := yaml.Marshal(&newCfg)
	if err != nil {
		return fmt.Errorf("failed to marshal updated config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), config.DirPermReadWriteExec); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.WriteFile(configPath, out, config.FilePermReadWrite); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	logging.InfoContext(cmd.Context(), "Updated %s in %s", args[0], configPath)
	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available in context")
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
		return fmt.Errorf("failed to load config into viper: %w", err)
	}

	value := v.Get(args[0])
	if value == nil {
		return fmt.Errorf("key not found: %s", args[0])
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), value)
	return err
}
