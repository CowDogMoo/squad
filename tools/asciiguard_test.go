package tools

import (
	"context"
	"strings"
	"testing"
)

func TestValidateASCIIOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		old     string
		new     string
		wantErr bool
	}{
		{"plain ascii rewrite passes", "the old text", "the new text", false},
		{"introduces smart quote rejected", "the model's job", "the model’s job", true},
		{"introduces em dash rejected", "a - b", "a — b", true},
		{"introduces ellipsis char rejected", "wait...", "wait…", true},
		{"introduces nbhyphen rejected", "built-in", "built‑in", true},
		{"keeps existing non-ascii passes", "café menu here", "the café menu", false},
		{"removes non-ascii passes", "a — b dash", "a - b dash", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateASCIIOnly(tt.old, tt.new)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateASCIIOnly(%q,%q) err = %v, wantErr %v", tt.old, tt.new, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "ascii-only edit rejected") {
				t.Errorf("unexpected error text: %v", err)
			}
		})
	}
}

func TestEditGuardError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ctx     context.Context
		old     string
		new     string
		wantErr string // substring; "" means expect nil
	}{
		{"no modes active passes", context.Background(), "x := 1", "y := 2", ""},
		{"ascii-only rejects introduced char", InitASCIIOnlyMode(context.Background()), "a - b", "a — b", "ascii-only edit rejected"},
		{"ascii-only passes plain rewrite", InitASCIIOnlyMode(context.Background()), "old", "new", ""},
		{"comments-only rejects added code", InitCommentsOnlyMode(context.Background()), "x := 1", "x := 1\ny := 2", "comment"},
		{"comments-only passes comment edit", InitCommentsOnlyMode(context.Background()), "# old comment\ncode = 1", "code = 1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := editGuardError(tt.ctx, tt.old, tt.new)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("editGuardError() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("editGuardError() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestASCIIOnlyMode(t *testing.T) {
	t.Parallel()
	if IsASCIIOnlyMode(context.Background()) {
		t.Fatal("background ctx should not be ascii-only")
	}
	ctx := InitASCIIOnlyMode(context.Background())
	if !IsASCIIOnlyMode(ctx) {
		t.Fatal("InitASCIIOnlyMode did not set the mode")
	}
}
