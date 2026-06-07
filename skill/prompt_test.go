package skill

import (
	"strings"
	"testing"
)

func makeEntry(name, description string) Entry {
	return Entry{
		Manifest: &Manifest{Name: name, Description: description},
		Scope:    ScopeGlobal,
		Dir:      "/tmp/" + name,
	}
}

func TestRenderPromptBlockEmpty(t *testing.T) {
	if got := RenderPromptBlock(nil); got != "" {
		t.Errorf("expected empty string for nil input, got %q", got)
	}
	if got := RenderPromptBlock([]Entry{}); got != "" {
		t.Errorf("expected empty string for empty slice, got %q", got)
	}
}

func TestRenderPromptBlockSorted(t *testing.T) {
	in := []Entry{
		makeEntry("gamma", "Gamma description."),
		makeEntry("alpha", "Alpha description."),
		makeEntry("beta", "Beta description."),
	}
	got := RenderPromptBlock(in)
	idxAlpha := strings.Index(got, "alpha")
	idxBeta := strings.Index(got, "beta")
	idxGamma := strings.Index(got, "gamma")
	if idxAlpha >= idxBeta || idxBeta >= idxGamma {
		t.Errorf("entries not sorted: alpha=%d beta=%d gamma=%d", idxAlpha, idxBeta, idxGamma)
	}
}

func TestRenderPromptBlockShape(t *testing.T) {
	in := []Entry{makeEntry("grocery", "Adds groceries to the cart.")}
	got := RenderPromptBlock(in)
	if !strings.HasPrefix(got, PromptBlockHeader) {
		t.Errorf("missing header in:\n%s", got)
	}
	if !strings.Contains(got, "`Skill` tool") {
		t.Errorf("missing Skill tool reference:\n%s", got)
	}
	if !strings.Contains(got, "- **grocery**: Adds groceries to the cart.") {
		t.Errorf("missing entry bullet:\n%s", got)
	}
}

func TestRenderPromptBlockCollapsesWhitespace(t *testing.T) {
	in := []Entry{makeEntry("grocery", "Line 1.\n  Line 2 with   gaps.\n")}
	got := RenderPromptBlock(in)
	if !strings.Contains(got, "- **grocery**: Line 1. Line 2 with gaps.") {
		t.Errorf("description not collapsed correctly:\n%s", got)
	}
}

func TestRenderPromptBlockDeterministic(t *testing.T) {
	in := []Entry{
		makeEntry("alpha", "A."),
		makeEntry("beta", "B."),
	}
	first := RenderPromptBlock(in)
	second := RenderPromptBlock(in)
	if first != second {
		t.Errorf("output not deterministic:\n%q\nvs\n%q", first, second)
	}
}
