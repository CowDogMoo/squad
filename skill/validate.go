package skill

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Severity classifies a validation finding. Errors block successful
// validation; warnings are surfaced but do not fail.
type Severity int

const (
	// SeverityError is a fatal validation finding.
	SeverityError Severity = iota
	// SeverityWarning is a non-fatal finding the author should address.
	SeverityWarning
)

// String renders the severity for CLI output.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return "unknown"
	}
}

// Finding is a single spec-conformance issue found by Validate.
type Finding struct {
	Severity Severity
	// Path is the file the finding applies to, relative to the skill dir
	// when known (e.g. "SKILL.md", "scripts/foo.py") or absolute otherwise.
	Path    string
	Message string
}

// ValidationReport collects findings from validating a single skill dir.
// HasErrors reports whether any finding is a fatal error.
type ValidationReport struct {
	SkillDir string
	Findings []Finding
}

// HasErrors reports whether the report contains at least one error-severity
// finding.
func (r ValidationReport) HasErrors() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Errors returns only the error-severity findings.
func (r ValidationReport) Errors() []Finding {
	return filterFindings(r.Findings, SeverityError)
}

// Warnings returns only the warning-severity findings.
func (r ValidationReport) Warnings() []Finding {
	return filterFindings(r.Findings, SeverityWarning)
}

func filterFindings(in []Finding, sev Severity) []Finding {
	out := make([]Finding, 0)
	for _, f := range in {
		if f.Severity == sev {
			out = append(out, f)
		}
	}
	return out
}

// Validate runs every spec-conformance check on the skill directory at path.
// It returns the report even when checks fail — callers render the report
// regardless. An error is returned only when path itself cannot be read.
func Validate(path string) (*ValidationReport, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}

	report := &ValidationReport{SkillDir: abs}

	manifestPath := filepath.Join(abs, FileName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			report.add(SeverityError, FileName, "missing SKILL.md")
			return report, nil
		}
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}

	m, err := ParseManifest(data)
	if err != nil {
		report.add(SeverityError, FileName, err.Error())
		// Without a parsed manifest we can't run the rest of the checks
		// (body lookups, name match), so stop early.
		return report, nil
	}

	if m.Name != filepath.Base(abs) {
		report.add(SeverityError, FileName,
			fmt.Sprintf("manifest name %q does not match directory name %q", m.Name, filepath.Base(abs)))
	}

	if len(m.Body) > WarnBodyBytes {
		report.add(SeverityWarning, FileName,
			fmt.Sprintf("body is %d bytes; spec target is <5 KiB — consider splitting into references/", len(m.Body)))
	}

	for _, link := range findMarkdownLinks(m.Body) {
		if isPathTraversal(link) {
			report.add(SeverityError, FileName,
				fmt.Sprintf("relative link %q escapes the skill directory", link))
		}
	}

	if err := validateScripts(abs, report); err != nil {
		return nil, fmt.Errorf("scan scripts/: %w", err)
	}

	return report, nil
}

func (r *ValidationReport) add(sev Severity, path, msg string) {
	r.Findings = append(r.Findings, Finding{Severity: sev, Path: path, Message: msg})
}

// markdownLinkPattern matches a `[label](target)` markdown link. Captures the
// target so callers don't need to re-split.
var markdownLinkPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)`)

// findMarkdownLinks returns every link target referenced in the body.
func findMarkdownLinks(body string) []string {
	matches := markdownLinkPattern.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// isPathTraversal reports whether a markdown link target tries to escape the
// skill directory. Absolute paths and URLs are ignored — they don't traverse
// the local tree.
func isPathTraversal(target string) bool {
	if target == "" {
		return false
	}
	if strings.Contains(target, "://") {
		return false
	}
	if strings.HasPrefix(target, "/") {
		return false
	}
	// Strip any fragment / query so anchors don't trip the check.
	if idx := strings.IndexAny(target, "?#"); idx >= 0 {
		target = target[:idx]
	}
	cleaned := filepath.Clean(target)
	return cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

// validateScripts checks that every regular file under scripts/ is invocable
// — either marked executable (Unix only) or carrying a recognizable shebang
// on its first line. Missing scripts/ directories are fine.
func validateScripts(skillDir string, report *ValidationReport) error {
	scriptsDir := filepath.Join(skillDir, "scripts")
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		full := filepath.Join(scriptsDir, entry.Name())
		rel := filepath.Join("scripts", entry.Name())
		info, err := entry.Info()
		if err != nil {
			report.add(SeverityWarning, rel, fmt.Sprintf("stat: %v", err))
			continue
		}
		executable := runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
		hasShebang, err := fileHasShebang(full)
		if err != nil {
			report.add(SeverityWarning, rel, fmt.Sprintf("read: %v", err))
			continue
		}
		if !executable && !hasShebang {
			report.add(SeverityWarning, rel,
				"script is neither executable nor shebanged — agent must know which interpreter to use")
		}
	}
	return nil
}

// fileHasShebang reports whether path's first line begins with "#!".
func fileHasShebang(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	first, err := r.Peek(2)
	if err != nil {
		// Empty or unreadable file — not shebanged.
		return false, nil
	}
	return string(first) == "#!", nil
}
