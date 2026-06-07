package config

import (
	"os"
	"testing"
)

func TestResolveValue_PlainString(t *testing.T) {
	t.Parallel()
	got, err := ResolveValue("hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("expected 'hello world', got %q", got)
	}
}

func TestResolveValue_EnvVar(t *testing.T) {
	t.Setenv("SQUAD_TEST_RESOLVE_VAR", "secret123")
	got, err := ResolveValue("key=$SQUAD_TEST_RESOLVE_VAR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "key=secret123" {
		t.Fatalf("expected 'key=secret123', got %q", got)
	}
}

func TestResolveValue_EnvVarBraces(t *testing.T) {
	t.Setenv("SQUAD_TEST_RESOLVE_BRACE", "val")
	got, err := ResolveValue("${SQUAD_TEST_RESOLVE_BRACE}_suffix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "val_suffix" {
		t.Fatalf("expected 'val_suffix', got %q", got)
	}
}

func TestResolveValue_CommandSubstitution(t *testing.T) {
	t.Parallel()
	got, err := ResolveValue("$(echo hello)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestResolveValue_MixedCommandAndEnv(t *testing.T) {
	t.Setenv("SQUAD_TEST_MIXED", "world")
	got, err := ResolveValue("$(echo hello) $SQUAD_TEST_MIXED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("expected 'hello world', got %q", got)
	}
}

func TestResolveValue_UnsetVar(t *testing.T) {
	t.Setenv("SQUAD_TEST_UNSET_VAR_XYZ", "")
	if err := os.Unsetenv("SQUAD_TEST_UNSET_VAR_XYZ"); err != nil {
		t.Fatalf("failed to unset env var: %v", err)
	}
	_, err := ResolveValue("$SQUAD_TEST_UNSET_VAR_XYZ")
	if err == nil {
		t.Fatal("expected error for unset variable")
	}
}

func TestResolveValue_FailedCommand(t *testing.T) {
	t.Parallel()
	_, err := ResolveValue("$(exit 1)")
	if err == nil {
		t.Fatal("expected error for failed command")
	}
}

func TestResolveValue_UnmatchedParen(t *testing.T) {
	t.Parallel()
	_, err := ResolveValue("$(echo hello")
	if err == nil {
		t.Fatal("expected error for unmatched $(")
	}
}

func TestResolveValue_UnmatchedBrace(t *testing.T) {
	t.Parallel()
	_, err := ResolveValue("${UNCLOSED")
	if err == nil {
		t.Fatal("expected error for unmatched ${")
	}
}

func TestResolveValue_LiteralDollar(t *testing.T) {
	t.Parallel()
	got, err := ResolveValue("price is $$5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "price is $5" {
		t.Fatalf("expected 'price is $5', got %q", got)
	}
}

func TestResolveValue_NestedCommand(t *testing.T) {
	t.Parallel()
	got, err := ResolveValue("$(echo $(echo nested))")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "nested" {
		t.Fatalf("expected 'nested', got %q", got)
	}
}

func TestResolveValue_NoSubstitution(t *testing.T) {
	t.Parallel()
	got, err := ResolveValue("no dollars here")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "no dollars here" {
		t.Fatalf("expected 'no dollars here', got %q", got)
	}
}

func TestResolveValue_LoneDollar(t *testing.T) {
	t.Parallel()
	got, err := ResolveValue("price is $")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "price is $" {
		t.Fatalf("expected 'price is $', got %q", got)
	}
}

func TestResolveValue_DollarBeforeNonVarChar(t *testing.T) {
	t.Parallel()
	got, err := ResolveValue("cost $! each")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "cost $! each" {
		t.Fatalf("expected 'cost $! each', got %q", got)
	}
}

func TestLoadFromPath_TokenResolution(t *testing.T) {
	t.Setenv("SQUAD_TEST_TOKEN_VAL", "resolved-token-123")
	dir := t.TempDir()
	cfgPath := dir + "/config.yaml"
	if err := os.WriteFile(cfgPath, []byte("provider:\n  token: $SQUAD_TEST_TOKEN_VAL\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadFromPath(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider.Token != "resolved-token-123" {
		t.Fatalf("expected resolved token, got %q", cfg.Provider.Token)
	}
}

func TestLoadFromPath_TokenResolutionError(t *testing.T) {
	t.Setenv("SQUAD_TEST_DUMMY", "")
	if err := os.Unsetenv("SQUAD_TEST_DUMMY"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}
	dir := t.TempDir()
	cfgPath := dir + "/config.yaml"
	if err := os.WriteFile(cfgPath, []byte("provider:\n  token: $SQUAD_TEST_DUMMY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadFromPath(cfgPath)
	if err == nil {
		t.Fatal("expected error for unresolvable token")
	}
}
