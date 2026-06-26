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
