package routine

import (
	"strings"
	"testing"
)

func TestParseQualified(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in    string
		scope Scope
		id    string
		ok    bool
	}{
		{"global:nightly", ScopeGlobal, "nightly", true},
		{"repo:audit", ScopeRepo, "audit", true},
		{"nightly", "", "", false},
		{"bogus:nightly", "", "", false},
		{"global:BadID", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		ref, ok := ParseQualified(c.in)
		if ok != c.ok {
			t.Errorf("ParseQualified(%q) ok=%v want=%v", c.in, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if ref.Scope != c.scope || ref.ID != c.id {
			t.Errorf("ParseQualified(%q)=%+v want scope=%s id=%s", c.in, ref, c.scope, c.id)
		}
	}
}

func TestResolveSingleCandidate(t *testing.T) {
	t.Parallel()
	candidates := []Ref{{Scope: ScopeGlobal, ID: "nightly"}}
	ref, err := Resolve(candidates, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Scope != ScopeGlobal {
		t.Errorf("got scope %s", ref.Scope)
	}
}

func TestResolveAmbiguous(t *testing.T) {
	t.Parallel()
	candidates := []Ref{
		{Scope: ScopeGlobal, ID: "nightly"},
		{Scope: ScopeRepo, Root: "/r/a", ID: "nightly"},
	}
	_, err := Resolve(candidates, "")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("err should mention ambiguity, got %q", err)
	}
	if !strings.Contains(err.Error(), "global:nightly") || !strings.Contains(err.Error(), "repo:nightly") {
		t.Errorf("err should list options, got %q", err)
	}
}

func TestResolveBiasesToCurrentRepo(t *testing.T) {
	t.Parallel()
	candidates := []Ref{
		{Scope: ScopeGlobal, ID: "audit"},
		{Scope: ScopeRepo, Root: "/r/a", ID: "audit"},
		{Scope: ScopeRepo, Root: "/r/b", ID: "audit"},
	}
	ref, err := Resolve(candidates, "/r/b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Scope != ScopeRepo || ref.Root != "/r/b" {
		t.Errorf("got %+v, expected repo /r/b", ref)
	}
}

func TestResolveNone(t *testing.T) {
	t.Parallel()
	_, err := Resolve(nil, "")
	if err == nil {
		t.Fatal("expected error for empty candidate set")
	}
}

func TestRefDisplay(t *testing.T) {
	t.Parallel()
	g := Ref{Scope: ScopeGlobal, ID: "x"}
	if g.Display() != "global" {
		t.Errorf("global display: %q", g.Display())
	}
	r := Ref{Scope: ScopeRepo, Root: "/home/me/code/api", ID: "x"}
	if r.Display() != "repo:api" {
		t.Errorf("repo display: %q", r.Display())
	}
}

func TestRefQualified(t *testing.T) {
	t.Parallel()
	r := Ref{Scope: ScopeRepo, Root: "/r/a", ID: "x"}
	if r.Qualified() != "repo:x" {
		t.Errorf("got %q", r.Qualified())
	}
}
