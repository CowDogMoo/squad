# Python Review Pattern

A comprehensive fabric pattern for reviewing Python code against industry best practices, PEP 8, and the Google Python Style Guide. This pattern analyzes Python code and provides constructive, actionable feedback with severity-based findings.

## Pattern Structure

This pattern includes:

- **`system.md`** - The review framework and prompt engineering for LLM
- **`python-review-criteria.md`** - Comprehensive reference document with detailed criteria (source of truth)
- **`filter.sh`** - Post-processing to clean up output formatting
- **`README.md`** - This documentation
- **`test-code-issues.py`** - Sample code with various issues for testing
- **`test-pattern.sh`** - Automated testing script

The pattern is designed with **separation of concerns**: the `python-review-criteria.md` contains the comprehensive knowledge base (what to look for), while `system.md` contains the review framework (how to analyze and report). This makes the pattern easier to maintain and customize.

## Style Guide References

This pattern incorporates guidelines from:

- **[PEP 8](https://peps.python.org/pep-0008/)** - Python's official style guide
- **[Google Python Style Guide](https://google.github.io/styleguide/pyguide.html)** - Google's enterprise Python standards

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
- Documentation quality assessment (docstrings, type hints)
- Positive observations to recognize good practices

## Review Categories

1. **Code Formatting & Style** - PEP 8, imports, naming conventions
2. **Error & Exception Handling** - specific exceptions, context, cleanup
3. **Type Annotations** - hints, Optional, Union, generics
4. **Data Structures** - comprehensions, generators, mutability
5. **Function & Class Design** - single responsibility, default arguments
6. **Code Structure** - early returns, variable scope, complexity
7. **API Design** - decorators, context managers, protocols
8. **Performance** - string operations, loops, memory
9. **Module Organization** - naming, scope, globals
10. **Documentation** - docstrings, comments, type hints
11. **Security** - input validation, SQL, secrets, subprocess
12. **Testing** - coverage, quality, pytest patterns

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
fabric --pattern /path/to/fabric-patterns-hub/patterns/python-review
```

## Usage

### Single File Review

Review a single Python file:

```bash
cat myfile.py | fabric --pattern python-review > review.md
```

### Review from Clipboard

Review code from clipboard (macOS):

```bash
pbpaste | fabric --pattern python-review
```

### Review Pull Request Changes

Review changed files in a PR:

```bash
git diff main...HEAD --name-only | grep '\.py$' | while read file; do
  echo "## Review: $file"
  cat "$file" | fabric --pattern python-review
  echo ""
done > pr-review.md
```

### Pre-Commit Hook

Use as a pre-commit hook for code review:

```bash
#!/bin/bash
# .git/hooks/pre-commit

staged_files=$(git diff --cached --name-only | grep '\.py$')
if [ -n "$staged_files" ]; then
  for file in $staged_files; do
    review=$(cat "$file" | fabric --pattern python-review)
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

for file in $(git diff --name-only HEAD~1 | grep '\.py$'); do
  echo "Reviewing $file..."
  REVIEW=$(cat "$file" | fabric --pattern python-review)

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
- Clear explanation with references to PEP 8/Google Style Guide

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

The code demonstrates good use of type hints but has critical security issues
with SQL injection vulnerabilities and mutable default arguments. Documentation
is incomplete for public functions.

## Critical Issues

### SQL Injection Vulnerability

**Severity:** CRITICAL
**Category:** Security
**Impact:** Allows attackers to execute arbitrary SQL queries

**Problem:**
```python
def get_user(user_id: str) -> User:
    query = f"SELECT * FROM users WHERE id = {user_id}"
    cursor.execute(query)
```

**Solution:**

```python
def get_user(user_id: str) -> User:
    query = "SELECT * FROM users WHERE id = %s"
    cursor.execute(query, (user_id,))
```

**Explanation:** String interpolation in SQL queries allows injection attacks.
Use parameterized queries per Google Python Style Guide security guidelines.

---

### Mutable Default Argument

**Severity:** CRITICAL
**Category:** Function Design
**Impact:** Shared mutable state causes unexpected behavior

**Problem:**

```python
def append_item(item, items=[]):
    items.append(item)
    return items
```

**Solution:**

```python
def append_item(item, items=None):
    if items is None:
        items = []
    items.append(item)
    return items
```

**Explanation:** Mutable default arguments are shared across calls per PEP 8.

---

## Improvements

### Use Early Returns to Reduce Nesting

**Severity:** MEDIUM
**Category:** Code Structure

**Current:**

```python
if user is not None:
    if user.is_active:
        process(user)
```

**Suggested:**

```python
if user is None:
    return
if not user.is_active:
    return
process(user)
```

**Why:** Early returns improve readability per Google Style Guide.

---

## Positive Observations

- Good use of type hints throughout the codebase
- Consistent naming conventions following PEP 8

---

## Recommendations

- Add mypy to CI pipeline for static type checking
- Consider using pytest parametrize for test cases
- Add ruff for automated linting and formatting

```

## Customization

### Extending Review Criteria

To add or modify review criteria, edit the `python-review-criteria.md` file:

1. **Add new categories** for organization-specific requirements
2. **Modify existing criteria** to match your standards
3. **Adjust severity levels** based on your priorities

Example addition:
```markdown
## Company-Specific Standards

### Required Logging

All public functions must include logging:
- Use structured logging with `logging` module
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
cat myfile.py | fabric --pattern python-review | grep -A 20 "## Critical Issues"
```

### Issue: Missing context in review

**Solution:** Provide more code context. The pattern works best with complete functions or files rather than snippets.

### Issue: Review seems incomplete

**Solution:** Ensure the full file is being passed. Large files may need to be reviewed in sections.

## Related Patterns

- **python-refactor** - Transform code (action, not analysis)
- **python-tests** - Generate tests for Python code
- **python-doc-comments** - Generate documentation comments

## References

### Pattern Documentation

- **`python-review-criteria.md`** - Comprehensive review criteria reference

### External Resources

- [PEP 8 - Style Guide for Python Code](https://peps.python.org/pep-0008/)
- [Google Python Style Guide](https://google.github.io/styleguide/pyguide.html)
- [PEP 257 - Docstring Conventions](https://peps.python.org/pep-0257/)
- [PEP 484 - Type Hints](https://peps.python.org/pep-0484/)
- [Real Python - Code Quality](https://realpython.com/python-code-quality/)

## Contributing

Contributions are welcome! If you have ideas for improving review criteria or adding new checks, please submit a PR or open an issue.

## License

This pattern is part of the fabric-patterns-hub and follows the same license as the parent repository.

---

**Version:** 1.0.0
**Last Updated:** 2026-01-18
**Maintainer:** fabric-patterns-hub contributors
