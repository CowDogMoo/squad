// Package grading provides parsing and scoring for agent output reports.
package grading

import (
	"regexp"
	"strings"
)

// ParsedOutput represents the structured data extracted from agent output.
type ParsedOutput struct {
	// Report sections detected
	HasChangesSummary  bool
	HasAnalysisSummary bool
	HasFilesTouched    bool
	HasValidation      bool
	HasIssuesFixed     bool
	HasIssuesSkipped   bool
	HasFindings        bool

	// Extracted data
	FilesTouched     []string
	IssuesFixed      []ParsedIssue
	IssuesSkipped    []ParsedSkip
	Findings         []ParsedIssue
	ValidationPassed bool

	// Metadata
	Mode string // "edit" or "readonly"
}

// ParsedIssue represents a single issue found or fixed.
type ParsedIssue struct {
	Title    string
	Severity string
	Category string
	File     string
	Line     int
}

// ParsedSkip represents an issue that was skipped.
type ParsedSkip struct {
	Title  string
	File   string
	Reason string
}

// Parser extracts structured data from agent markdown output.
type Parser struct {
	sectionPatterns   map[string]*regexp.Regexp
	filePattern       *regexp.Regexp
	severityPattern   *regexp.Regexp
	validationPattern *regexp.Regexp
}

// NewParser creates a new output parser.

func NewParser() *Parser {
	return &Parser{
		sectionPatterns: map[string]*regexp.Regexp{
			"changes_summary":  regexp.MustCompile(`(?i)^##\s*Changes\s+Summary`),
			"analysis_summary": regexp.MustCompile(`(?i)^##\s*Analysis\s+Summary`),
			"files_touched":    regexp.MustCompile(`(?i)^##\s*Files\s+Touched`),
			"validation":       regexp.MustCompile(`(?i)^##\s*Validation`),
			"issues_fixed":     regexp.MustCompile(`(?i)^##\s*Issues\s+Found\s+and\s+Fixed`),
			"issues_skipped":   regexp.MustCompile(`(?i)^##\s*Issues\s+Found\s+but\s+Skipped`),
			"findings":         regexp.MustCompile(`(?i)^##\s*Findings`),
		},
		filePattern:       regexp.MustCompile("^-\\s*`([^`]+)`"),
		severityPattern:   regexp.MustCompile(`(?i)\*\*Severity:\*\*\s*(CRITICAL|HIGH|MEDIUM|LOW|INFO)`),
		validationPattern: regexp.MustCompile(`(?i)(PASS|FAIL)`),
	}
}

// parseState tracks the current parsing context.
type parseState struct {
	currentSection string
	inSkipTable    bool
}

// Parse extracts structured data from agent output markdown.
func (p *Parser) Parse(output string) *ParsedOutput {
	result := &ParsedOutput{}
	lines := strings.Split(output, "\n")
	state := &parseState{}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		p.detectSection(trimmed, result, state)
		p.parseLineContent(trimmed, lines, i, result, state)
	}

	p.detectMode(result)
	return result
}

func (p *Parser) detectSection(trimmed string, result *ParsedOutput, state *parseState) {
	for name, pattern := range p.sectionPatterns {
		if pattern.MatchString(trimmed) {
			state.currentSection = name
			p.markSectionPresent(result, name)
			state.inSkipTable = false
			return
		}
	}
}

func (p *Parser) parseLineContent(trimmed string, lines []string, idx int, result *ParsedOutput, state *parseState) {
	switch state.currentSection {
	case "files_touched":
		p.parseFileLine(trimmed, result)
	case "validation":
		p.parseValidationLine(trimmed, result)
	case "issues_fixed":
		p.parseIssueLine(trimmed, lines, idx, &result.IssuesFixed)
	case "findings":
		p.parseIssueLine(trimmed, lines, idx, &result.Findings)
	case "issues_skipped":
		p.parseSkipTableLine(trimmed, result, state)
	}
}

func (p *Parser) parseFileLine(trimmed string, result *ParsedOutput) {
	if matches := p.filePattern.FindStringSubmatch(trimmed); len(matches) > 1 {
		result.FilesTouched = append(result.FilesTouched, matches[1])
	}
}

func (p *Parser) parseValidationLine(trimmed string, result *ParsedOutput) {
	if matches := p.validationPattern.FindStringSubmatch(trimmed); len(matches) > 1 {
		result.ValidationPassed = strings.ToUpper(matches[1]) == "PASS"
	}
}

func (p *Parser) parseIssueLine(trimmed string, lines []string, idx int, issues *[]ParsedIssue) {
	if strings.HasPrefix(trimmed, "### ") {
		*issues = append(*issues, p.parseIssueBlock(lines, idx))
	}
}

func (p *Parser) parseSkipTableLine(trimmed string, result *ParsedOutput, state *parseState) {
	if !strings.HasPrefix(trimmed, "|") {
		return
	}
	if strings.Contains(trimmed, "Issue") {
		state.inSkipTable = true
		return
	}
	if strings.Contains(trimmed, "---") {
		return
	}
	if state.inSkipTable {
		if skip := p.parseSkipRow(trimmed); skip.Title != "" {
			result.IssuesSkipped = append(result.IssuesSkipped, skip)
		}
	}
}

func (p *Parser) detectMode(result *ParsedOutput) {
	if result.HasChangesSummary || result.HasIssuesFixed {
		result.Mode = "edit"
	} else if result.HasAnalysisSummary || result.HasFindings {
		result.Mode = "readonly"
	}
}

func (p *Parser) markSectionPresent(result *ParsedOutput, section string) {
	switch section {
	case "changes_summary":
		result.HasChangesSummary = true
	case "analysis_summary":
		result.HasAnalysisSummary = true
	case "files_touched":
		result.HasFilesTouched = true
	case "validation":
		result.HasValidation = true
	case "issues_fixed":
		result.HasIssuesFixed = true
	case "issues_skipped":
		result.HasIssuesSkipped = true
	case "findings":
		result.HasFindings = true
	}
}

func (p *Parser) parseIssueBlock(lines []string, startIdx int) ParsedIssue {
	issue := ParsedIssue{}

	// Title from ### header
	if startIdx < len(lines) {
		title := strings.TrimPrefix(strings.TrimSpace(lines[startIdx]), "### ")
		issue.Title = title
	}

	// Scan next few lines for metadata
	for i := startIdx + 1; i < len(lines) && i < startIdx+10; i++ {
		line := strings.TrimSpace(lines[i])

		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "## ") {
			break
		}

		if matches := p.severityPattern.FindStringSubmatch(line); len(matches) > 1 {
			issue.Severity = strings.ToUpper(matches[1])
		}

		if strings.HasPrefix(line, "**Category:**") {
			issue.Category = strings.TrimSpace(strings.TrimPrefix(line, "**Category:**"))
		}

		if strings.HasPrefix(line, "**File:**") {
			issue.File = strings.TrimSpace(strings.TrimPrefix(line, "**File:**"))
		}
	}

	return issue
}

func (p *Parser) parseSkipRow(row string) ParsedSkip {
	parts := strings.Split(row, "|")
	if len(parts) < 5 {
		return ParsedSkip{}
	}

	return ParsedSkip{
		Title:  strings.TrimSpace(parts[1]),
		File:   strings.TrimSpace(parts[3]),
		Reason: strings.TrimSpace(parts[4]),
	}
}

// RequiredSections returns the list of required sections for a given mode.
func RequiredSections(mode string) []string {
	if mode == "readonly" {
		return []string{"analysis_summary", "findings"}
	}
	return []string{"changes_summary", "issues_fixed", "files_touched", "validation"}
}
