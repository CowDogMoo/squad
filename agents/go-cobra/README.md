# Go Cobra Pattern

A comprehensive fabric pattern for reviewing Go CLI applications built with Cobra and Viper against industry best practices and idiomatic patterns. This pattern analyzes CLI code and provides constructive, actionable feedback with severity-based findings.

## Pattern Structure

This pattern includes:
- **`system.md`** - The review framework and prompt engineering for LLM
- **`cobra-viper-best-practices.md`** - Comprehensive reference document with detailed criteria (source of truth)
- **`filter.sh`** - Post-processing to clean up output formatting
- **`README.md`** - This documentation
- **`test-cli-issues.go`** - Sample CLI code with various issues for testing
- **`test-pattern.sh`** - Automated testing script

The pattern is designed with **separation of concerns**: the `cobra-viper-best-practices.md` contains the comprehensive knowledge base (what to look for), while `system.md` contains the review framework (how to analyze and report). This makes the pattern easier to maintain and customize.

## Purpose

This pattern helps you:
- **Review CLI code quality** against established Cobra/Viper best practices
- **Identify issues** with severity classification (Critical, High, Medium, Low, Info)
- **Provide constructive feedback** with specific examples and fixes
- **Learn best practices** through detailed explanations
- **Maintain consistency** in CLI code reviews across teams
- **Support CI/CD** integration for automated code review

## Features

- Comprehensive analysis across 10 key categories
- Severity-based findings (Critical, High, Medium, Low, Info)
- Constructive feedback with code examples
- Anti-pattern detection
- Configuration management assessment
- Testing strategy recommendations
- Production readiness checks

## Review Categories

1. **Command Design** - Natural syntax, hierarchy, naming conventions
2. **Project Structure** - Minimal main.go, one command per file, separation
3. **Command Implementation** - RunE, Args validation, lifecycle hooks
4. **Flag Management** - Persistent vs local, groups, types
5. **Viper Configuration** - Precedence, type-safe structs, validation
6. **Integration** - Flag binding, reading from Viper, initialization
7. **Error Handling** - Wrapped errors, actionable messages
8. **Testing** - Command execution, dependency injection, table-driven
9. **Shell Completions** - Static, dynamic, flag completions
10. **Production Readiness** - Version, graceful shutdown, secrets

## Installation

This pattern is part of the fabric-patterns-hub. Ensure you have fabric installed:

```bash
# Install fabric if you haven't already
pip install fabric-ai

# Add this patterns repository to fabric
fabric --add-pattern-source /path/to/fabric-patterns-hub/patterns
```

Or use it directly by pointing to the pattern directory:

```bash
fabric --pattern /path/to/fabric-patterns-hub/patterns/go-cobra
```

## Usage

### Single File Review

Review a single Go CLI file:

```bash
cat cmd/root.go | fabric --pattern go-cobra > review.md
```

### Review from Clipboard

Review code from clipboard (macOS):

```bash
pbpaste | fabric --pattern go-cobra
```

### Review Multiple Command Files

Review all command files in a project:

```bash
cat cmd/*.go | fabric --pattern go-cobra > review.md
```

### Review Pull Request Changes

Review changed CLI files in a PR:

```bash
git diff main...HEAD --name-only | grep 'cmd/.*\.go$' | while read file; do
  echo "## Review: $file"
  cat "$file" | fabric --pattern go-cobra
  echo ""
done > pr-review.md
```

### Pre-Commit Hook

Use as a pre-commit hook for CLI code review:

```bash
#!/bin/bash
# .git/hooks/pre-commit

staged_files=$(git diff --cached --name-only | grep 'cmd/.*\.go$')
if [ -n "$staged_files" ]; then
  for file in $staged_files; do
    review=$(cat "$file" | fabric --pattern go-cobra)
    if echo "$review" | grep -q "CRITICAL"; then
      echo "Critical issues found in $file:"
      echo "$review" | grep -A 10 "CRITICAL"
      exit 1
    fi
  done
fi
```

### CI/CD Integration

Use in your CI pipeline:

```bash
#!/bin/bash
# .github/workflows/cli-review.sh

for file in $(git diff --name-only HEAD~1 | grep 'cmd/.*\.go$'); do
  echo "Reviewing $file..."
  REVIEW=$(cat "$file" | fabric --pattern go-cobra)

  if echo "$REVIEW" | grep -q "## Critical Issues"; then
    echo "Critical issues found in $file!"
    echo "$REVIEW"
    exit 1
  fi
done

echo "CLI code review passed!"
```

## Output Format

The pattern generates a structured markdown report with:

### Summary
- 2-3 sentence overview of CLI code quality
- Main concerns highlighted

### Critical Issues
- Must-fix items affecting correctness or safety
- Code snippets showing problem and solution
- Clear explanation with references to criteria

### Improvements
- Non-critical enhancements
- Best practice suggestions
- Pattern recommendations

### Positive Observations
- Good practices already implemented
- Recognition of well-written code

### Recommendations
- General suggestions for improvement
- Tooling recommendations

## Example Output

```markdown
## Summary

The CLI code demonstrates good command structure but has critical issues
with configuration handling. Flag values are read directly instead of
through Viper, bypassing the configuration precedence system.

## Critical Issues

### Flags Bypass Configuration Precedence

**Severity:** HIGH
**Category:** Integration
**Impact:** Environment variables and config files are ignored

**Problem:**
```go
func runServe(cmd *cobra.Command, args []string) error {
    port, _ := cmd.Flags().GetInt("port")
    return startServer(port)
}
```

**Solution:**
```go
func runServe(cmd *cobra.Command, args []string) error {
    port := v.GetInt("server.port")
    return startServer(port)
}
```

**Explanation:** Reading directly from flags bypasses Viper's precedence
system. Config files and environment variables are ignored. Always read
from Viper after binding flags per cobra-viper-best-practices.md.

---

## Improvements

### Add Shell Completion Command

**Severity:** LOW
**Category:** Shell Completions

**Current:**
No completion command available.

**Suggested:**
```go
var completionCmd = &cobra.Command{
    Use:   "completion [bash|zsh|fish|powershell]",
    Short: "Generate shell completion script",
    // ... full implementation
}
```

**Why:** Shell completions significantly improve UX and are expected
in production CLIs per Production Readiness guidelines.

---

## Positive Observations

- Good use of RunE for proper error propagation
- Commands follow natural VERB NOUN syntax

---

## Recommendations

- Add a version command with build info
- Consider using functional options for complex configuration
```

## Customization

### Extending Review Criteria

To add or modify review criteria, edit the `cobra-viper-best-practices.md` file:

1. **Add new categories** for organization-specific requirements
2. **Modify existing criteria** to match your standards
3. **Adjust severity levels** based on your priorities

Example addition:
```markdown
## Company-Specific Standards

### Required Telemetry

All commands must include telemetry hooks:
- Use OpenTelemetry for distributed tracing
- Include command duration metrics
- Log command invocations
```

### Adjusting Severity Levels

Modify the `# SEVERITY LEVELS` section in `system.md` to match your team's priorities.

### Changing Output Format

Edit the `# OUTPUT FORMAT` section in `system.md` to customize the report structure.

## Troubleshooting

### Issue: Too many findings

**Solution:** Focus on critical and high severity issues first:
```bash
cat cmd/root.go | fabric --pattern go-cobra | grep -A 20 "## Critical Issues"
```

### Issue: Missing context in review

**Solution:** Provide more code context. The pattern works best with complete command files rather than snippets. Consider reviewing multiple files together:
```bash
cat cmd/*.go | fabric --pattern go-cobra
```

### Issue: Review seems incomplete

**Solution:** Ensure the full file is being passed. Large codebases may need to be reviewed in sections.

## Related Patterns

- **go-review** - General Go code review (non-CLI specific)
- **go-refactor** - Transform code (action, not analysis)
- **go-tests** - Generate tests for Go code
- **go-doc-comments** - Generate documentation comments

## References

### Pattern Documentation

- **`cobra-viper-best-practices.md`** - Comprehensive best practices reference

### External Resources

- [Cobra GitHub](https://github.com/spf13/cobra)
- [Cobra Documentation](https://cobra.dev/)
- [Viper GitHub](https://github.com/spf13/viper)
- [Cobra User Guide](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md)
- [Shell Completions Guide](https://cobra.dev/docs/how-to-guides/shell-completion/)

## Contributing

Contributions are welcome! If you have ideas for improving review criteria or adding new checks, please submit a PR or open an issue.

## License

This pattern is part of the fabric-patterns-hub and follows the same license as the parent repository.

---

**Version:** 1.0.0
**Last Updated:** 2026-01-13
**Maintainer:** fabric-patterns-hub contributors
