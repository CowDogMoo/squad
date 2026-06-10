package main

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestViperFromContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cmd     *cobra.Command
		wantNil bool
	}{
		{
			name: "missing viper",
			cmd: func() *cobra.Command {
				c := &cobra.Command{}
				c.SetContext(context.Background())
				return c
			}(),
			wantNil: true,
		},
		{
			name: "viper present",
			cmd: func() *cobra.Command {
				c := &cobra.Command{}
				c.SetContext(withViper(context.Background(), viper.New()))
				return c
			}(),
			wantNil: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := viperFromContext(tt.cmd)
			if (got == nil) != tt.wantNil {
				t.Errorf("viperFromContext() nil = %v, want %v", got == nil, tt.wantNil)
			}
		})
	}
}
