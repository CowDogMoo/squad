package config

import (
	"fmt"
	"reflect"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// RepoSpec describes a git-hosted agent or skill repository. URL is required;
// Ref optionally pins the source to a commit SHA, tag, or branch so the same
// agent/skill resolves to the same content on every clone or update. An empty
// Ref tracks the default branch — the original pre-pinning behavior.
type RepoSpec struct {
	URL string `yaml:"url" mapstructure:"url"`
	Ref string `yaml:"ref,omitempty" mapstructure:"ref"`
}

// IsPinned reports whether the spec is locked to a specific ref. Pinned
// sources are skipped by `update` unless the caller forces a re-resolve.
func (r RepoSpec) IsPinned() bool {
	return r.Ref != ""
}

// UnmarshalYAML accepts both the legacy plain-string form
// (`official: https://...`) and the explicit struct form
// (`official: {url: ..., ref: v1.0.0}`). The string form is the historical
// configuration; the struct form is opt-in for pinning.
func (r *RepoSpec) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		r.URL = node.Value
		r.Ref = ""
		return nil
	case yaml.MappingNode:
		type rawSpec struct {
			URL string `yaml:"url"`
			Ref string `yaml:"ref"`
		}
		var raw rawSpec
		if err := node.Decode(&raw); err != nil {
			return err
		}
		r.URL = raw.URL
		r.Ref = raw.Ref
		return nil
	default:
		return fmt.Errorf("RepoSpec: expected string or mapping, got %v", node.Tag)
	}
}

// MarshalYAML keeps the on-disk config compact: a spec with no Ref serializes
// back to a plain string, matching configs that pre-date the pinning feature.
// Specs with a Ref serialize as a mapping.
func (r RepoSpec) MarshalYAML() (any, error) {
	if r.Ref == "" {
		return r.URL, nil
	}
	return struct {
		URL string `yaml:"url"`
		Ref string `yaml:"ref"`
	}{URL: r.URL, Ref: r.Ref}, nil
}

// DecodeHooks returns the viper.DecoderConfigOption that registers every
// custom mapstructure hook the loader needs. The default viper hooks
// (StringToTimeDurationHookFunc, StringToSliceHookFunc) are re-installed
// alongside repoSpecDecodeHook so env-var slice expansion still works.
//
// Callers that re-unmarshal viper state into a Config — typically to apply
// late CLI-flag overrides — must thread this option through their
// v.Unmarshal call, otherwise repository entries written as plain strings
// will fail to decode.
func DecodeHooks() viper.DecoderConfigOption {
	return viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		repoSpecDecodeHook(),
	))
}

// repoSpecDecodeHook lets viper / mapstructure consume both the legacy
// `string` form and the `{url, ref}` mapping form when decoding YAML into a
// Config. Used inside Unmarshal via [decodeHooks].
func repoSpecDecodeHook() mapstructure.DecodeHookFunc {
	target := reflect.TypeOf(RepoSpec{})
	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if to != target {
			return data, nil
		}
		switch v := data.(type) {
		case string:
			return RepoSpec{URL: v}, nil
		case map[string]any:
			spec := RepoSpec{}
			if url, ok := v["url"].(string); ok {
				spec.URL = url
			}
			if ref, ok := v["ref"].(string); ok {
				spec.Ref = ref
			}
			return spec, nil
		}
		return data, nil
	}
}
