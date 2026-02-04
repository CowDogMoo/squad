package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestExecuteVersion(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)
	t.Setenv("USERPROFILE", baseDir)

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"squad", "version"}

	if err := Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestRootFlagCompletionFuncs(t *testing.T) {
	root := NewRootCmd()
	tests := []struct {
		name string
		flag string
		want []string
	}{
		{
			name: "log level",
			flag: "log-level",
			want: []string{"debug", "info", "warn", "error"},
		},
		{
			name: "log format",
			flag: "log-format",
			want: []string{"text", "json", "color"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compFunc, ok := root.GetFlagCompletionFunc(tt.flag)
			if !ok {
				t.Fatalf("expected completion func for %s", tt.flag)
			}
			values, directive := compFunc(root, nil, "")
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf("directive = %v, want %v", directive, cobra.ShellCompDirectiveNoFileComp)
			}
			if !reflect.DeepEqual(values, tt.want) {
				t.Fatalf("values = %v, want %v", values, tt.want)
			}
		})
	}
}
