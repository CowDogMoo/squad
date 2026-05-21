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

// MaxBodyBytes caps the on-disk size of a SKILL.md body. The spec targets
// <5k tokens for L2 content; 25 KiB is roughly 5x that ceiling in characters
// and matches the cap announced in PLAN.md.
const MaxBodyBytes = 25 * 1024

// WarnBodyBytes is the soft cap surfaced by Validate as a warning. Skills
// past this size still load but the author is told to consider splitting.
const WarnBodyBytes = 5 * 1024

// reservedSubstrings are forbidden inside a skill name. The spec disallows
// these so authors don't impersonate first-party Anthropic skills.
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
	// Body is the markdown content after the frontmatter block, with leading
	// and trailing whitespace trimmed. Returned by the Skill tool at L2.
	Body string `yaml:"-"`
}

// ValidateName reports whether s is a syntactically valid skill name per the
// open Skills spec.
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
	lower := strings.ToLower(s)
	for _, banned := range reservedSubstrings {
		if strings.Contains(lower, banned) {
			return fmt.Errorf("name %q contains reserved substring %q", s, banned)
		}
	}
	if strings.Contains(s, "<") || strings.Contains(s, ">") {
		return fmt.Errorf("name %q must not contain XML tag characters", s)
	}
	return nil
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
