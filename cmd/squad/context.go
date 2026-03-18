package main

import (
	"context"

	"github.com/cowdogmoo/squad/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// configKeyType identifies the config value stored in a context.
type configKeyType struct{}

// viperKeyType identifies the Viper value stored in a context.
type viperKeyType struct{}

var (
	configKey = configKeyType{}
	viperKey  = viperKeyType{}
)

func withConfig(ctx context.Context, cfg *config.Config) context.Context {
	return context.WithValue(ctx, configKey, cfg)
}

func withViper(ctx context.Context, v *viper.Viper) context.Context {
	return context.WithValue(ctx, viperKey, v)
}

func configFromContext(cmd *cobra.Command) *config.Config {
	if cfg, ok := cmd.Context().Value(configKey).(*config.Config); ok {
		return cfg
	}
	return nil
}

func viperFromContext(cmd *cobra.Command) *viper.Viper {
	if v, ok := cmd.Context().Value(viperKey).(*viper.Viper); ok {
		return v
	}
	return nil
}
