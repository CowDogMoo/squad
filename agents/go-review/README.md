# Go Review Pattern

A comprehensive fabric pattern for reviewing Go code against industry best practices and idiomatic patterns. This pattern analyzes Go code and provides constructive, actionable feedback with severity-based findings.

## Pattern Structure

This pattern includes:
- **`system.md`** - The review framework and prompt engineering for LLM
- **`go-review-criteria.md`** - Comprehensive reference document with detailed criteria (source of truth)
- **`filter.sh`** - Post-processing to clean up output formatting
- **`README.md`** - This documentation
- **`test-code-issues.go`** - Sample code with various issues for testing
- **`test-pattern.sh`** - Automated testing script

The pattern is designed with **separation of concerns**: the `go-review-criteria.md` contains the comprehensive knowledge base (what to look for), while `system.md` contains the review framework (how to analyze and report). This makes the pattern easier to maintain and customize.

## Purpose

This pattern helps you:
- **Review code quality** against established best practices
- **Identify issues** with severity classification (Critical, High, Medium, Low, Info)
- **Provide constructive feedback** with specific examples and fixes
- **Learn best practices** through detailed explanations
- **Maintain consistency** in code reviews across teams
- **Support CI/CD** integration for automated code review

## Features

- Comprehensive analysis across 12 key categories
- Severity-based findings (Critical, High, Medium, Low, Info)
- Constructive feedback with code examples
- Security vulnerability detection
- Performance optimization suggestions
- Documentation quality assessment
- Positive observations to recognize good practices

## Review Categories

1. **Code Formatting & Style** - gofmt, imports, naming conventions
2. **Error Handling** - wrapping, handling once, type assertions
3. **Concurrency Patterns** - context, goroutine lifecycle, channels
4. **Data Management** - slice boundaries, resource cleanup, zero values
5. **Interface & Type Design** - consumer interfaces, receivers
6. **Code Structure** - early returns, variable scope, type switches
7. **API Design** - repository, middleware, functional options
8. **Performance** - string operations, time handling, allocations
9. **Package Organization** - naming, scope, globals
10. **Documentation** - exported names, comment quality
11. **Security** - input validation, SQL, secrets, crypto
12. **Testing** - coverage, quality, table-driven tests

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
fabric --pattern /path/to/fabric-patterns-hub/patterns/go-review
```

## Usage

### Single File Review

Review a single Go file:

```bash
cat myfile.go | fabric --pattern go-review > review.md
```

### Review from Clipboard

Review code from clipboard (macOS):

```bash
pbpaste | fabric --pattern go-review
```

### Review Pull Request Changes

Review changed files in a PR:

```bash
git diff main...HEAD --name-only | grep '\.go$' | while read file; do
  echo "## Review: $file"
  cat "$file" | fabric --pattern go-review
  echo ""
done > pr-review.md
```

### Pre-Commit Hook

Use as a pre-commit hook for code review:

```bash
#!/bin/bash
# .git/hooks/pre-commit

staged_files=$(git diff --cached --name-only | grep '\.go$')
if [ -n "$staged_files" ]; then
  for file in $staged_files; do
    review=$(cat "$file" | fabric --pattern go-review)
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
# .github/workflows/code-review.sh

for file in $(git diff --name-only HEAD~1 | grep '\.go$'); do
  echo "Reviewing $file..."
  REVIEW=$(cat "$file" | fabric --pattern go-review)

  if echo "$REVIEW" | grep -q "## Critical Issues"; then
    echo "Critical issues found in $file!"
    echo "$REVIEW"
    exit 1
  fi
done

echo "Code review passed!"
```

## Output Format

The pattern generates a structured markdown report with:

### Summary
- 2-3 sentence overview of code quality
- Main concerns highlighted

### Critical Issues
- Must-fix items affecting correctness or safety
- Code snippets showing problem and solution
- Clear explanation with references to criteria

### Improvements
- Non-critical enhancements
- Best practice suggestions
- Performance optimizations

### Positive Observations
- Good practices already implemented
- Recognition of well-written code

### Recommendations
- General suggestions for improvement
- Tooling recommendations

## Example Output

```markdown
## Summary

The code demonstrates good use of error handling patterns but has
critical concurrency issues with goroutine leaks. Documentation
is incomplete for exported functions.

## Critical Issues

### Goroutine Leak in Worker Function

**Severity:** CRITICAL
**Category:** Concurrency Patterns
**Impact:** Memory leak and resource exhaustion over time

**Problem:**
```go
func StartWorker() {
    go func() {
        for {
            doWork()
            time.Sleep(time.Second)
        }
    }()
}
```

**Solution:**
```go
func StartWorker(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                doWork()
            }
        }
    }()
}
```

**Explanation:** Goroutines without cancellation support cause leaks.
Use context.Context for lifecycle management per go-review-criteria.md.

---

## Improvements

### Use Early Returns to Reduce Nesting

**Severity:** MEDIUM
**Category:** Code Structure

**Current:**
```go
if err == nil {
    if result != nil {
        // nested logic
    }
}
```

**Suggested:**
```go
if err != nil {
    return err
}
if result == nil {
    return ErrNilResult
}
// flat logic
```

**Why:** Early returns improve readability per Code Structure guidelines.

---

## Positive Observations

- Good use of error wrapping with fmt.Errorf and %w
- Consistent naming conventions following Go standards

---

## Recommendations

- Add golangci-lint to CI pipeline for automated checks
- Consider adding table-driven tests for complex functions
```

## Customization

### Extending Review Criteria

To add or modify review criteria, edit the `go-review-criteria.md` file:

1. **Add new categories** for organization-specific requirements
2. **Modify existing criteria** to match your standards
3. **Adjust severity levels** based on your priorities

Example addition:
```markdown
## Company-Specific Standards

### Required Logging

All exported functions must log entry and exit:
- Use structured logging with log/slog
- Include correlation IDs in all log entries
- Log errors at appropriate levels
```

### Adjusting Severity Levels

Modify the `# SEVERITY LEVELS` section in `system.md` to match your team's priorities.

### Changing Output Format

Edit the `# OUTPUT FORMAT` section in `system.md` to customize the report structure.

## Troubleshooting

### Issue: Too many findings

**Solution:** Focus on critical and high severity issues first:
```bash
cat myfile.go | fabric --pattern go-review | grep -A 20 "## Critical Issues"
```

### Issue: Missing context in review

**Solution:** Provide more code context. The pattern works best with complete functions or files rather than snippets.

### Issue: Review seems incomplete

**Solution:** Ensure the full file is being passed. Large files may need to be reviewed in sections.

## Related Patterns

- **go-refactor** - Transform code (action, not analysis)
- **go-tests** - Generate tests for Go code
- **go-doc-comments** - Generate documentation comments

## References

### Pattern Documentation

- **`go-review-criteria.md`** - Comprehensive review criteria reference

### External Resources

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- [Go Proverbs](https://go-proverbs.github.io/)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- [Google Go Style Guide](https://google.github.io/styleguide/go/)

## Contributing

Contributions are welcome! If you have ideas for improving review criteria or adding new checks, please submit a PR or open an issue.

## License

This pattern is part of the fabric-patterns-hub and follows the same license as the parent repository.

---

**Version:** 1.0.0
**Last Updated:** 2026-01-10
**Maintainer:** fabric-patterns-hub contributors
