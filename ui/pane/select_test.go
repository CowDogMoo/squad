package pane

import "testing"

func TestTypeaheadCompletePrefixExtendsToCommonPrefix(t *testing.T) {
	// Two options sharing "alpha-" → Tab from "alp" should extend the
	// buffer to "alpha-" (still ambiguous; user picks next char).
	ta := newTypeahead("", "alp", 20, []string{"alpha-one/", "alpha-two/"})
	if !ta.CompletePrefix() {
		t.Fatal("expected completion to extend buffer")
	}
	if got := ta.Value(); got != "alpha-" {
		t.Errorf("got %q, want %q", got, "alpha-")
	}
}

func TestTypeaheadCompletePrefixStaysWhenLCPEqualsBuffer(t *testing.T) {
	// Buffer already at LCP — bash equivalent of "Tab does nothing".
	ta := newTypeahead("", "alpha-", 20, []string{"alpha-one/", "alpha-two/"})
	if ta.CompletePrefix() {
		t.Error("buffer already at LCP — should not report extension")
	}
}

func TestTypeaheadCompletePrefixSingleMatch(t *testing.T) {
	ta := newTypeahead("", "/Use", 20, []string{"/Users/", "/usr/"})
	if !ta.CompletePrefix() {
		t.Fatal("expected completion to extend buffer")
	}
	if got := ta.Value(); got != "/Users/" {
		t.Errorf("got %q, want %q", got, "/Users/")
	}
}

func TestTypeaheadCompletePrefixNoMatchesIsNoOp(t *testing.T) {
	ta := newTypeahead("", "zzz", 20, []string{"alpha", "beta"})
	if ta.CompletePrefix() {
		t.Error("no matches should leave buffer untouched")
	}
	if got := ta.Value(); got != "zzz" {
		t.Errorf("buffer mutated: got %q, want %q", got, "zzz")
	}
}

func TestTypeaheadCompletePrefixAlreadyAtLCP(t *testing.T) {
	ta := newTypeahead("", "~/cow", 20, []string{"~/cowdogmoo/", "~/cowboys/"})
	if ta.CompletePrefix() {
		t.Error("already at LCP — should not report an extension")
	}
	if got := ta.Value(); got != "~/cow" {
		t.Errorf("buffer mutated: got %q, want %q", got, "~/cow")
	}
}
