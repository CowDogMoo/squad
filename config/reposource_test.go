package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRepoSpec_unmarshalsPlainString(t *testing.T) {
	t.Parallel()
	input := `repositories:
  official: https://github.com/cowdogmoo/squad-agents.git
`
	var got struct {
		Repositories map[string]RepoSpec `yaml:"repositories"`
	}
	if err := yaml.Unmarshal([]byte(input), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	spec := got.Repositories["official"]
	if spec.URL != "https://github.com/cowdogmoo/squad-agents.git" {
		t.Fatalf("URL = %q", spec.URL)
	}
	if spec.Ref != "" {
		t.Fatalf("Ref = %q, want empty", spec.Ref)
	}
	if spec.IsPinned() {
		t.Fatal("plain-string spec should not be pinned")
	}
}

func TestRepoSpec_unmarshalsMapping(t *testing.T) {
	t.Parallel()
	input := `repositories:
  pinned:
    url: https://github.com/cowdogmoo/squad-agents.git
    ref: v0.4.2
`
	var got struct {
		Repositories map[string]RepoSpec `yaml:"repositories"`
	}
	if err := yaml.Unmarshal([]byte(input), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	spec := got.Repositories["pinned"]
	if spec.URL != "https://github.com/cowdogmoo/squad-agents.git" {
		t.Fatalf("URL = %q", spec.URL)
	}
	if spec.Ref != "v0.4.2" {
		t.Fatalf("Ref = %q", spec.Ref)
	}
	if !spec.IsPinned() {
		t.Fatal("mapping spec with ref should be pinned")
	}
}

func TestRepoSpec_marshalsUnpinnedAsString(t *testing.T) {
	t.Parallel()
	in := struct {
		Repositories map[string]RepoSpec `yaml:"repositories"`
	}{Repositories: map[string]RepoSpec{
		"official": {URL: "https://github.com/cowdogmoo/squad-agents.git"},
	}}
	out, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "official: https://github.com/cowdogmoo/squad-agents.git") {
		t.Fatalf("expected compact string form, got:\n%s", got)
	}
	if strings.Contains(got, "url:") {
		t.Fatalf("unpinned spec should not serialize as a mapping, got:\n%s", got)
	}
}

func TestRepoSpec_marshalsPinnedAsMapping(t *testing.T) {
	t.Parallel()
	in := struct {
		Repositories map[string]RepoSpec `yaml:"repositories"`
	}{Repositories: map[string]RepoSpec{
		"pinned": {URL: "https://example.com/repo.git", Ref: "v1.2.0"},
	}}
	out, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "url: https://example.com/repo.git") {
		t.Fatalf("expected mapping form with url:, got:\n%s", got)
	}
	if !strings.Contains(got, "ref: v1.2.0") {
		t.Fatalf("expected mapping form with ref:, got:\n%s", got)
	}
}

func TestRepoSpec_roundTrip(t *testing.T) {
	t.Parallel()
	original := map[string]RepoSpec{
		"unpinned": {URL: "https://example.com/a.git"},
		"pinned":   {URL: "https://example.com/b.git", Ref: "deadbeef"},
	}
	wrapper := struct {
		Repositories map[string]RepoSpec `yaml:"repositories"`
	}{Repositories: original}

	yamlBytes, err := yaml.Marshal(wrapper)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Repositories map[string]RepoSpec `yaml:"repositories"`
	}
	if err := yaml.Unmarshal(yamlBytes, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for name, want := range original {
		got := decoded.Repositories[name]
		if got != want {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
	}
}
