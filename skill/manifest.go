// Package skill implements support for Anthropic Agent Skills (an open
// standard, December 2025): single-directory capabilities that a running
// agent can discover by name+description and load in full when relevant.
//
// A skill is a directory containing a SKILL.md file with YAML frontmatter:
//
//	---
//	name: my-skill-name
//	description: What this skill does and when to use it
//	---
//
//	# Body
//	Free-form markdown the agent reads when it triggers the skill.
//
// Squad implements the spec's three-level progressive disclosure:
//
//   - Level 1 (always): name + description injected into the agent's system
//     prompt at boot, so the agent knows the skill exists.
//   - Level 2 (on demand): full SKILL.md body returned by the Skill tool when
//     the agent decides it matches the task.
//   - Level 3 (on demand): bundled files under references/, scripts/, assets/
//     accessed by the agent via the existing Read and Bash tools.
//
// Phase 1 covers manifest parsing, validation, and multi-scope discovery.
// The runtime hooks (system prompt injection, Skill tool, skill stack) land
// in later phases.
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the standard skill manifest filename, per spec.
const FileName = "SKILL.md"

// MaxNameLen is the per-spec upper bound on a skill name.
const MaxNameLen = 64

// MaxDescriptionLen is the per-spec upper bound on a skill description.
const MaxDescriptionLen = 1024

// MaxCompatibilityLen is the per-spec upper bound on the compatibility field
// (Reference B of the Skills guide: 1–500 characters).
const MaxCompatibilityLen = 500

// MaxBodyBytes caps the on-disk size of a SKILL.md body. The spec targets
// <5k tokens for L2 content. Empirically the largest first-party skill
// Anthropic ships (skill-creator) is ~32 KiB, so the hard cap sits at 64
// KiB — generous enough for legitimate large skills, still tight enough
// to prevent runaway context consumption.
const MaxBodyBytes = 64 * 1024

// WarnBodyBytes is the soft cap surfaced by Validate as a warning. Skills
// past this size still load but the author is told to consider splitting.
// The guide's "<5,000 word" target maps to roughly 25 KiB by char count;
// 24 KiB rounds to that without being so tight that every real-world
// procedural skill (~7–16 KiB) trips a warning on every validate run.
const WarnBodyBytes = 24 * 1024

// reservedSubstrings carry the spec's anti-impersonation hint. We do not
// reject names containing them — Anthropic's own `claude-api` skill
// violates the rule, so treating it as fatal would break interop with
// first-party skills. Validation surfaces it as a warning instead.
var reservedSubstrings = []string{"anthropic", "claude"}

// namePattern enforces the per-spec character class: lowercase letters,
// digits, and hyphens, starting and ending with an alphanumeric. Single-char
// names are allowed by the alternation.
var namePattern = regexp.MustCompile(`^([a-z0-9]|[a-z0-9][a-z0-9-]{0,62}[a-z0-9])$`)

// Manifest is a parsed SKILL.md. Frontmatter fields are typed; the body is
// retained verbatim (without the leading frontmatter block) for L2 delivery.
type Manifest struct {
	// Name is the skill's stable identifier. Required.
	Name string `yaml:"name"`
	// Description is the prose summary used at Level 1 to decide whether the
	// skill matches a task. Required.
	Description string `yaml:"description"`
	// License is the optional SPDX-style license identifier (e.g. "MIT") for
	// open-source skills. Spec: free-form string.
	License string `yaml:"license,omitempty"`
	// Compatibility describes environment requirements — intended platform,
	// system packages, network access. Optional, 1–500 chars.
	Compatibility string `yaml:"compatibility,omitempty"`
	// AllowedTools restricts which tools the agent may call while this skill
	// is on the stack. Empty means no restriction. See AllowedTools for the
	// accepted YAML shapes and the matching semantics squad implements.
	AllowedTools AllowedTools `yaml:"allowed-tools,omitempty"`
	// Metadata holds the spec's free-form key/value bag (author, version,
	// mcp-server, tags, etc.). Preserved verbatim for callers that want to
	// surface it via `squad skill list` without re-reading the file.
	Metadata map[string]any `yaml:"metadata,omitempty"`
	// Body is the markdown content after the frontmatter block, with leading
	// and trailing whitespace trimmed. Returned by the Skill tool at L2.
	Body string `yaml:"-"`
}

// AllowedTools is the list of tool names a skill is permitted to invoke. In
// YAML it may appear as a space-separated string (the form shown in the
// Skills guide example: `"Bash(python:*) Bash(npm:*) WebFetch"`) or as a
// sequence of scalars. squad enforces tool-name matching only — the Claude
// Code permission-pattern syntax (`Bash(python:*)`) is normalized to the
// bare tool name preceding the parenthesis. Authors who need finer-grained
// constraints should bundle a script rather than rely on the field.
type AllowedTools []string

// UnmarshalYAML accepts either a scalar (space-separated tool names) or a
// sequence of scalars. Anything else is a parse error.
func (a *AllowedTools) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		*a = parseAllowedToolsString(value.Value)
		return nil
	case yaml.SequenceNode:
		out := AllowedTools{}
		for _, n := range value.Content {
			if n.Kind != yaml.ScalarNode {
				return fmt.Errorf("allowed-tools entries must be strings (line %d)", n.Line)
			}
			out = append(out, parseAllowedToolsString(n.Value)...)
		}
		*a = out
		return nil
	default:
		return fmt.Errorf("allowed-tools must be a string or list of strings (line %d)", value.Line)
	}
}

// Allows reports whether toolName is permitted. An empty/unset allow-list
// imposes no restriction.
func (a AllowedTools) Allows(toolName string) bool {
	if len(a) == 0 {
		return true
	}
	for _, n := range a {
		if n == toolName {
			return true
		}
	}
	return false
}

// parseAllowedToolsString splits a Claude-Code-style allow string into bare
// tool names. Both space-separated (the form shown in the Skills guide) and
// comma-separated (the form most authors actually write — including every
// first-party squad-skills entry) are accepted.
//
//	"Bash(python:*) Bash(npm:*) WebFetch" → ["Bash","Bash","WebFetch"]
//	"Read, Glob, Edit"                     → ["Read","Glob","Edit"]
//
// Duplicates are left in place; Allows treats the list as a set.
func parseAllowedToolsString(s string) []string {
	out := make([]string, 0, 4)
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if i := strings.IndexByte(tok, '('); i >= 0 {
			tok = tok[:i]
		}
		if tok = strings.TrimSpace(tok); tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// ValidateName reports whether s is a syntactically valid skill name per the
// open Skills spec. The reserved-substring rule ("anthropic"/"claude") is
// enforced as a warning in [Validate] rather than here, because Anthropic's
// own first-party skills violate it (e.g. `claude-api`).
func ValidateName(s string) error {
	if s == "" {
		return errors.New("name is required")
	}
	if len(s) > MaxNameLen {
		return fmt.Errorf("name %q exceeds %d characters", s, MaxNameLen)
	}
	if !namePattern.MatchString(s) {
		return fmt.Errorf("name %q is invalid: must be lowercase letters/digits/hyphens, must start and end with a letter or digit", s)
	}
	if strings.Contains(s, "<") || strings.Contains(s, ">") {
		return fmt.Errorf("name %q must not contain XML tag characters", s)
	}
	return nil
}

// HasReservedSubstring reports whether s contains a spec-reserved substring.
// Exported so the validator (and tests) can surface it as a warning.
func HasReservedSubstring(s string) (string, bool) {
	lower := strings.ToLower(s)
	for _, banned := range reservedSubstrings {
		if strings.Contains(lower, banned) {
			return banned, true
		}
	}
	return "", false
}

// ValidateDescription reports whether d satisfies the spec constraints:
// non-empty, ≤1024 chars, no XML-tag characters.
func ValidateDescription(d string) error {
	if d == "" {
		return errors.New("description is required")
	}
	if len(d) > MaxDescriptionLen {
		return fmt.Errorf("description exceeds %d characters (got %d)", MaxDescriptionLen, len(d))
	}
	if strings.Contains(d, "<") || strings.Contains(d, ">") {
		return errors.New("description must not contain XML tag characters")
	}
	return nil
}

// ValidateCompatibility reports whether c satisfies the per-spec 1–500
// character bound. Empty is allowed (the field itself is optional).
func ValidateCompatibility(c string) error {
	if c == "" {
		return nil
	}
	if len(c) > MaxCompatibilityLen {
		return fmt.Errorf("compatibility exceeds %d characters (got %d)", MaxCompatibilityLen, len(c))
	}
	return nil
}

// Validate runs every spec-required check on the manifest's frontmatter and
// body. It does not touch the filesystem; callers that load from disk should
// use LoadManifest, which calls Validate after parsing.
func (m *Manifest) Validate() error {
	if err := ValidateName(m.Name); err != nil {
		return err
	}
	if err := ValidateDescription(m.Description); err != nil {
		return err
	}
	if err := ValidateCompatibility(m.Compatibility); err != nil {
		return err
	}
	for _, tool := range m.AllowedTools {
		if strings.TrimSpace(tool) == "" {
			return errors.New("allowed-tools contains an empty entry")
		}
	}
	if len(m.Body) > MaxBodyBytes {
		return fmt.Errorf("body exceeds %d bytes (got %d)", MaxBodyBytes, len(m.Body))
	}
	return nil
}

// frontmatterFence is the line that opens and closes a YAML frontmatter block.
const frontmatterFence = "---"

// ParseManifest reads a SKILL.md byte buffer and returns the parsed manifest.
// The buffer is split on the YAML frontmatter fences; the remainder is the
// body. Anything other than the leading `---` … `---\n` shape is an error
// because the spec requires frontmatter.
func ParseManifest(data []byte) (*Manifest, error) {
	body, fm, err := splitFrontmatter(data)
	if err != nil {
		return nil, err
	}
	m := &Manifest{}
	if err := yaml.Unmarshal(fm, m); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	// Normalize whitespace in fields the L1 prompt block renders inline.
	// Trailing whitespace from YAML block scalars (e.g. `description: >`)
	// would otherwise leak into the rendered system prompt.
	m.Description = strings.TrimSpace(m.Description)
	m.License = strings.TrimSpace(m.License)
	m.Compatibility = strings.TrimSpace(m.Compatibility)
	m.Body = strings.TrimSpace(string(body))
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadManifest reads SKILL.md from path and returns the parsed manifest.
// path must be the full path to the SKILL.md file (callers in catalog.go
// already know the skill dir; they join FileName themselves).
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill %s: %w", path, err)
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

// IsSkillDir reports whether dir contains a readable SKILL.md. It does not
// validate the manifest; the caller should LoadManifest after.
func IsSkillDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, FileName))
	return err == nil && !info.IsDir()
}

// splitFrontmatter separates a leading `---\n…\n---\n` block from the rest of
// the file. The body retains any leading newline so callers can trim it as
// they see fit.
func splitFrontmatter(data []byte) (body, frontmatter []byte, err error) {
	// Strip an optional UTF-8 BOM; some editors add one on save.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	first, rest, ok := readLine(data)
	if !ok || strings.TrimRight(string(first), "\r") != frontmatterFence {
		return nil, nil, errors.New("missing YAML frontmatter (expected leading '---')")
	}
	// Find the closing fence at the start of a line.
	end := indexClosingFence(rest)
	if end < 0 {
		return nil, nil, errors.New("unterminated YAML frontmatter (no closing '---')")
	}
	frontmatter = rest[:end]
	body = consumeClosingLine(rest[end:])
	return body, frontmatter, nil
}

// readLine returns the first newline-delimited line of data (without the
// newline) and the remainder after the newline.
func readLine(data []byte) (line, rest []byte, ok bool) {
	idx := bytes.IndexByte(data, '\n')
	if idx < 0 {
		if len(data) == 0 {
			return nil, nil, false
		}
		return data, nil, true
	}
	return data[:idx], data[idx+1:], true
}

// indexClosingFence finds the offset within data where a `---` (optionally
// CR-terminated) appears at the start of a line. Returns -1 if not found.
func indexClosingFence(data []byte) int {
	for offset := 0; offset < len(data); {
		nl := bytes.IndexByte(data[offset:], '\n')
		var line []byte
		if nl < 0 {
			line = data[offset:]
		} else {
			line = data[offset : offset+nl]
		}
		if strings.TrimRight(string(line), "\r") == frontmatterFence {
			return offset
		}
		if nl < 0 {
			return -1
		}
		offset += nl + 1
	}
	return -1
}

// consumeClosingLine returns the slice of data immediately after the closing
// `---` line (skipping the trailing newline, if any).
func consumeClosingLine(data []byte) []byte {
	idx := bytes.IndexByte(data, '\n')
	if idx < 0 {
		return nil
	}
	return data[idx+1:]
}
