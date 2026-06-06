package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
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

func TestRepoSpec_unmarshalRejectsSequence(t *testing.T) {
	t.Parallel()
	// A sequence node is neither the legacy string form nor the pinned
	// mapping form; UnmarshalYAML must surface that as an error.
	input := `repositories:
  bad:
    - https://example.com/a.git
    - https://example.com/b.git
`
	var got struct {
		Repositories map[string]RepoSpec `yaml:"repositories"`
	}
	err := yaml.Unmarshal([]byte(input), &got)
	if err == nil {
		t.Fatal("expected error for sequence-shaped RepoSpec")
	}
}

func TestDecodeHooks_acceptsStringForm(t *testing.T) {
	t.Parallel()
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(`
agents:
  repositories:
    official: https://github.com/cowdogmoo/squad-agents.git
`)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	var cfg Config
	if err := v.Unmarshal(&cfg, DecodeHooks()); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := cfg.Agents.Repositories["official"]
	if got.URL != "https://github.com/cowdogmoo/squad-agents.git" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Ref != "" {
		t.Errorf("plain-string form should not pin, got Ref=%q", got.Ref)
	}
}

func TestDecodeHooks_acceptsMappingForm(t *testing.T) {
	t.Parallel()
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(`
skills:
  repositories:
    team:
      url: https://example.com/team-skills.git
      ref: v1.2.0
`)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	var cfg Config
	if err := v.Unmarshal(&cfg, DecodeHooks()); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := cfg.Skills.Repositories["team"]
	if got.URL != "https://example.com/team-skills.git" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Ref != "v1.2.0" {
		t.Errorf("Ref = %q, want v1.2.0", got.Ref)
	}
}

func TestRepoSpec_unmarshalMappingPropagatesInnerError(t *testing.T) {
	t.Parallel()
	// url must decode into a string; a sequence here forces yaml.Node.Decode
	// to fail, exercising the mapping-form error path.
	input := `repositories:
  bad:
    url: [/not, /a, /string]
`
	var got struct {
		Repositories map[string]RepoSpec `yaml:"repositories"`
	}
	if err := yaml.Unmarshal([]byte(input), &got); err == nil {
		t.Fatal("expected error when mapping fields have wrong inner types")
	}
}

// callHook invokes the repo-spec decode hook directly.
func callHook(t *testing.T, from, to reflect.Type, data any) any {
	t.Helper()
	got, err := repoSpecDecodeHook()(from, to, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return got
}

func TestRepoSpecDecodeHook_fallsThroughForOtherTypes(t *testing.T) {
	t.Parallel()
	// The hook only converts string and map[string]any into RepoSpec.
	// Other source types must pass through untouched so other hooks can
	// see them.
	intVal := 42
	got := callHook(t, reflect.TypeOf(intVal), reflect.TypeOf(RepoSpec{}), intVal)
	if got != intVal {
		t.Fatalf("got %v, want %v", got, intVal)
	}
}

func TestRepoSpecDecodeHook_noopWhenTargetIsNotRepoSpec(t *testing.T) {
	t.Parallel()
	// When the destination is not RepoSpec, the hook must hand the data
	// back untouched even when the source type is string.
	other := reflect.TypeOf("")
	got := callHook(t, other, other, "passthrough")
	if got != "passthrough" {
		t.Fatalf("got %v (%T)", got, got)
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
