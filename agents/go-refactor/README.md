# Go Refactor Pattern

A comprehensive fabric pattern for refactoring Go code to be more idiomatic, maintainable, and aligned with Go best practices. This pattern analyzes Go code for anti-patterns and code smells, then transforms it into cleaner, more idiomatic versions while preserving functionality.

## Pattern Structure

This pattern includes:
- **`system.md`** - The refactoring framework and prompt engineering for LLM
- **`go-best-practices.md`** - Comprehensive reference document with detailed patterns (source of truth)
- **`filter.sh`** - Post-processing to clean up output formatting
- **`README.md`** - This documentation
- **`test-code-before.go`** - Sample code with anti-patterns for testing
- **`test-code-after.go`** - Expected refactored output
- **`test-pattern.sh`** - Automated testing script

The pattern is designed with **separation of concerns**: the `go-best-practices.md` contains the comprehensive knowledge base (what patterns to apply), while `system.md` contains the refactoring framework (how to analyze and transform). This eliminates duplication and makes the pattern easier to maintain and extend.

## Purpose

This pattern helps you:
- **Identify anti-patterns** in Go code (deep nesting, poor error handling, etc.)
- **Apply idiomatic patterns** following official Go guidelines
- **Improve code quality** without changing behavior
- **Learn best practices** through detailed explanations
- **Maintain consistency** across codebases
- **Support CI/CD** integration for code quality gates

## Features

- Code structure improvements (early returns, variable scope)
- Error handling best practices (wrapping, handling once)
- Concurrency patterns (context, error groups)
- Resource management (defer, slice boundaries)
- Performance optimizations (preallocation, strings.Builder)
- API design patterns (dependency injection, functional options)
- Type design improvements (consumer interfaces, consistent receivers)
- Documentation improvements following Go conventions

## Refactoring Categories

1. **Code Structure** - Early returns, variable scope, named constants
2. **Error Handling** - Error wrapping, handle once, type assertions
3. **Concurrency** - Context for lifecycle, error groups, channels
4. **Resource Management** - defer cleanup, slice boundaries
5. **Performance** - Preallocation, strings.Builder, strconv
6. **API Design** - Dependency injection, functional options
7. **Type Design** - Consumer interfaces, consistent receivers
8. **Documentation** - Complete sentences, explain what not how

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
fabric --pattern /path/to/fabric-patterns-hub/patterns/go-refactor
```

## Usage

### Single File Refactoring

Refactor a single Go file:

```bash
cat myfile.go | fabric --pattern go-refactor > myfile_refactored.go
```

### Refactor from Clipboard

Refactor code from clipboard (macOS):

```bash
pbpaste | fabric --pattern go-refactor | pbcopy
```

### Refactor Specific Function

Extract and refactor a specific function:

```bash
sed -n '/^func MyFunction/,/^func /p' myfile.go | head -n -1 | fabric --pattern go-refactor
```

### Batch Refactoring

Refactor all Go files in a directory:

```bash
for file in *.go; do
  echo "Refactoring $file..."
  cat "$file" | fabric --pattern go-refactor > "${file%.go}_refactored.go"
done
```

### CI/CD Integration

Use in your CI pipeline to suggest refactoring improvements:

```bash
#!/bin/bash
# .github/workflows/refactor-check.sh

for file in $(git diff --name-only HEAD~1 | grep '\.go$'); do
  echo "Analyzing $file for refactoring opportunities..."
  cat "$file" | fabric --pattern go-refactor > "/tmp/refactored_$(basename $file)"

  if ! diff -q "$file" "/tmp/refactored_$(basename $file)" > /dev/null; then
    echo "Refactoring suggestions available for $file"
  fi
done
```

## Output Format

The pattern generates output with:

### Refactored Code
- Complete, runnable Go code
- All improvements applied
- Proper formatting (gofmt-compatible)

### Changes Made
- Numbered list of changes
- Explanation of why each change was made
- Reference to pattern from knowledge base

### Notes
- Important assumptions made
- Potential follow-up improvements
- Behavior-preserving guarantees

## Example

### Before

```go
func GetUser(id int) (*User, error) {
    if id > 0 {
        user, err := fetchUser(id)
        if err == nil {
            if user.Active {
                return user, nil
            } else {
                return nil, errors.New("user inactive")
            }
        } else {
            return nil, err
        }
    } else {
        return nil, errors.New("invalid id")
    }
}
```

### After

```go
// GetUser returns the user with the given ID or returns an error
// if the user doesn't exist or is inactive.
func GetUser(id int) (*User, error) {
    if id <= 0 {
        return nil, errors.New("invalid id")
    }

    user, err := fetchUser(id)
    if err != nil {
        return nil, fmt.Errorf("fetching user %d: %w", id, err)
    }

    if !user.Active {
        return nil, errors.New("user inactive")
    }

    return user, nil
}
```

### Changes Made

1. **Applied early returns** - Flattened nested conditionals using guard clauses (Code Structure pattern #1)
2. **Added error wrapping** - Wrapped fetchUser error with context using %w (Error Handling pattern #4)
3. **Improved documentation** - Added godoc comment starting with function name (Documentation pattern #20)

## Best Practices

### When to Use This Pattern

**Good use cases:**
- Refactoring legacy Go code
- Code review preparation
- Learning idiomatic Go patterns
- Maintaining code quality standards
- Pre-commit code improvements

**Not ideal for:**
- Large-scale architectural changes
- Adding new functionality
- Performance-critical optimizations (use profiling instead)

### Preserving Functionality

The pattern is designed to:
- Never change the public API
- Maintain exact behavior
- Preserve backwards compatibility
- Focus changes on readability and maintainability

### Iterative Refactoring

For large files, consider:
1. Refactor one function at a time
2. Review and test each change
3. Commit incrementally
4. Re-run the pattern to catch remaining issues

## Customization

### Extending Best Practices

To add or modify refactoring patterns, edit the `go-best-practices.md` file:

1. **Add new patterns** with before/after examples
2. **Modify existing patterns** to match your team's standards
3. **Add "when to apply" guidance** for context

### Adjusting Output Format

To modify the output structure, edit the `# OUTPUT FORMAT` section in `system.md`:
- Add new sections
- Change formatting
- Adjust detail level

## Troubleshooting

### Issue: Pattern changes too much

**Solution:** The pattern tries to be comprehensive. For targeted changes, provide specific instructions:
```bash
echo "// Only fix error handling\n$(cat myfile.go)" | fabric --pattern go-refactor
```

### Issue: Output isn't gofmt-compatible

**Solution:** Run gofmt on the output:
```bash
cat myfile.go | fabric --pattern go-refactor | gofmt
```

### Issue: Pattern removes necessary code

**Solution:** The pattern preserves functionality by design. If something is removed, it was likely dead code. Review the "Notes" section for assumptions made.

## Related Patterns

- **go-review** - Code review feedback (analysis, not transformation)
- **go-tests** - Generate tests for Go code
- **go-doc-comments** - Generate documentation comments

## References

### Pattern Documentation

- **`go-best-practices.md`** - Comprehensive best practices reference included with this pattern

### External Resources

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- [Go Proverbs](https://go-proverbs.github.io/)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)

## Contributing

Contributions are welcome! If you have ideas for improving refactoring patterns or adding new checks, please submit a PR or open an issue.

## License

This pattern is part of the fabric-patterns-hub and follows the same license as the parent repository.

---

**Version:** 1.0.0
**Last Updated:** 2026-01-10
**Maintainer:** fabric-patterns-hub contributors
