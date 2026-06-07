package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

func TestApplyConfigOverrides_wrapsUnmarshalError(t *testing.T) {
	// Inject a value with a shape mapstructure can't fit into the target
	// (a string where a map[string]RepoSpec is expected) so v.Unmarshal
	// errors. The wrapped error must carry the operator-facing prefix.
	v := viper.New()
	v.Set("agents.repositories", "not-a-map")

	err := applyConfigOverrides(v, &config.Config{})
	if err == nil {
		t.Fatal("expected Unmarshal failure to surface as wrapped error")
	}
	if !strings.Contains(err.Error(), "failed to apply config overrides") {
		t.Fatalf("error not wrapped with expected prefix: %v", err)
	}
}

func TestApplyConfigOverrides_successPath(t *testing.T) {
	v := viper.New()
	cfg := &config.Config{}
	if err := applyConfigOverrides(v, cfg); err != nil {
		t.Fatalf("empty viper should unmarshal cleanly, got: %v", err)
	}
}

// TestInitConfig_unmarshalOverrideFailureSurfaced covers initConfig's
// `if err := applyConfigOverrides(...); err != nil { return err }`
// branch. Driving real viper into divergent first/second Unmarshal
// behavior isn't reproducible from outside initConfig — the function
// doesn't expose any seam between Load and the re-Unmarshal — so the
// test swaps the package-level helper to inject the error directly.
func TestInitConfig_unmarshalOverrideFailureSurfaced(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("XDG_CACHE_HOME", baseDir)
	t.Setenv("HOME", baseDir)
	t.Setenv("USERPROFILE", baseDir)

	orig := applyConfigOverrides
	t.Cleanup(func() { applyConfigOverrides = orig })
	applyConfigOverrides = func(*viper.Viper, *config.Config) error {
		return errors.New("synthetic override failure")
	}

	// Invoke initConfig directly: cobra's --help short-circuits before
	// PersistentPreRunE, so a `--help` Execute() doesn't reach the helper.
	root := NewRootCmd()
	if err := initConfig(root, nil); err == nil {
		t.Fatal("expected initConfig to propagate applyConfigOverrides' error")
	} else if !strings.Contains(err.Error(), "synthetic override failure") {
		t.Fatalf("error not propagated from helper: %v", err)
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
