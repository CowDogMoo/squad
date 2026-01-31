# Python Refactor Pattern

A comprehensive fabric pattern for refactoring Python code to be more idiomatic, maintainable, and aligned with Python best practices. This pattern analyzes Python code for anti-patterns and code smells, then transforms it into cleaner, more Pythonic versions while preserving functionality.

## Pattern Structure

This pattern includes:
- **`system.md`** - The refactoring framework and prompt engineering for LLM
- **`python-best-practices.md`** - Comprehensive reference document with detailed patterns (source of truth)
- **`filter.sh`** - Post-processing to clean up output formatting
- **`README.md`** - This documentation
- **`test-code-before.py`** - Sample code with anti-patterns for testing
- **`test-code-after.py`** - Expected refactored output
- **`test-pattern.sh`** - Automated testing script

The pattern is designed with **separation of concerns**: the `python-best-practices.md` contains the comprehensive knowledge base (what patterns to apply), while `system.md` contains the refactoring framework (how to analyze and transform). This eliminates duplication and makes the pattern easier to maintain and extend.

## Style Guide References

This pattern incorporates guidelines from:
- **[PEP 8](https://peps.python.org/pep-0008/)** - Python's official style guide
- **[PEP 20](https://peps.python.org/pep-0020/)** - The Zen of Python
- **[Google Python Style Guide](https://google.github.io/styleguide/pyguide.html)** - Google's enterprise Python standards

## Purpose

This pattern helps you:
- **Identify anti-patterns** in Python code (deep nesting, mutable defaults, poor error handling, etc.)
- **Apply idiomatic patterns** following official Python guidelines
- **Improve code quality** without changing behavior
- **Learn best practices** through detailed explanations
- **Maintain consistency** across codebases
- **Support CI/CD** integration for code quality gates

## Features

- Code structure improvements (early returns, variable scope)
- Error handling best practices (specific exceptions, context managers)
- Data structure patterns (comprehensions, generators, avoiding mutable defaults)
- Function and class design improvements (type hints, dataclasses, properties)
- Performance optimizations (f-strings, str.join(), enumerate)
- API design patterns (decorators, context managers, protocols)
- Documentation improvements following Google-style conventions
- Security fixes (SQL injection, command injection, hardcoded secrets)

## Refactoring Categories

1. **Code Structure** - Early returns, variable scope, named constants
2. **Error Handling** - Specific exceptions, context managers, exception chaining
3. **Data Structures** - List comprehensions, generators, mutable default fix
4. **Function Design** - Type hints, single responsibility, keyword-only args
5. **Class Design** - Dataclasses, properties, protocols
6. **Performance** - f-strings, str.join(), any()/all(), Counter
7. **API Design** - Decorators, contextlib, functools
8. **Documentation** - Google-style docstrings, meaningful comments

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
fabric --pattern /path/to/fabric-patterns-hub/patterns/python-refactor
```

## Usage

### Single File Refactoring

Refactor a single Python file:

```bash
cat myfile.py | fabric --pattern python-refactor > myfile_refactored.py
```

### Refactor from Clipboard

Refactor code from clipboard (macOS):

```bash
pbpaste | fabric --pattern python-refactor | pbcopy
```

### Refactor Specific Function

Extract and refactor a specific function:

```bash
sed -n '/^def my_function/,/^def /p' myfile.py | head -n -1 | fabric --pattern python-refactor
```

### Batch Refactoring

Refactor all Python files in a directory:

```bash
for file in *.py; do
  echo "Refactoring $file..."
  cat "$file" | fabric --pattern python-refactor > "${file%.py}_refactored.py"
done
```

### CI/CD Integration

Use in your CI pipeline to suggest refactoring improvements:

```bash
#!/bin/bash
# .github/workflows/refactor-check.sh

for file in $(git diff --name-only HEAD~1 | grep '\.py$'); do
  echo "Analyzing $file for refactoring opportunities..."
  cat "$file" | fabric --pattern python-refactor > "/tmp/refactored_$(basename $file)"

  if ! diff -q "$file" "/tmp/refactored_$(basename $file)" > /dev/null; then
    echo "Refactoring suggestions available for $file"
  fi
done
```

## Output Format

The pattern generates output with:

### Refactored Code
- Complete, runnable Python code
- All improvements applied
- Proper formatting (ruff/black-compatible)

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

```python
def get_user(user_id):
    if user_id is not None:
        if user_id > 0:
            user = fetch_user(user_id)
            if user is not None:
                if user.is_active:
                    return user
                else:
                    return None
            else:
                return None
        else:
            return None
    else:
        return None


def append_to_list(item, target=[]):
    target.append(item)
    return target


def read_file(path):
    f = open(path)
    content = f.read()
    f.close()
    return content
```

### After

```python
def get_user(user_id: int) -> User | None:
    """Retrieve a user by ID.

    Args:
        user_id: The unique identifier for the user.

    Returns:
        The User object if found and active, None otherwise.
    """
    if user_id is None or user_id <= 0:
        return None

    user = fetch_user(user_id)
    if user is None:
        return None

    if not user.is_active:
        return None

    return user


def append_to_list(item: Any, target: list | None = None) -> list:
    """Append an item to a target list.

    Args:
        item: The item to append.
        target: The list to append to. Creates a new list if None.

    Returns:
        The list with the item appended.
    """
    if target is None:
        target = []
    target.append(item)
    return target


def read_file(path: str) -> str:
    """Read and return the contents of a file.

    Args:
        path: The path to the file to read.

    Returns:
        The file contents as a string.
    """
    with open(path) as f:
        return f.read()
```

### Changes Made

1. **Applied early returns** - Flattened nested conditionals using guard clauses (Code Structure pattern #1)
2. **Fixed mutable default argument** - Changed `target=[]` to `target=None` with initialization (Data Structures pattern #13)
3. **Added context manager** - Used `with` statement for file handling (Error Handling pattern #7)
4. **Added type hints** - Added parameter and return type annotations (Function Design pattern #17)
5. **Improved documentation** - Added Google-style docstrings (Documentation pattern #36)

## Best Practices

### When to Use This Pattern

**Good use cases:**
- Refactoring legacy Python code
- Code review preparation
- Learning idiomatic Python patterns
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

To add or modify refactoring patterns, edit the `python-best-practices.md` file:

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
echo "# Only fix error handling\n$(cat myfile.py)" | fabric --pattern python-refactor
```

### Issue: Output doesn't pass linting

**Solution:** Run ruff or black on the output:
```bash
cat myfile.py | fabric --pattern python-refactor | ruff format -
```

### Issue: Pattern removes necessary code

**Solution:** The pattern preserves functionality by design. If something is removed, it was likely dead code. Review the "Notes" section for assumptions made.

## Related Patterns

- **python-review** - Code review feedback (analysis, not transformation)
- **python-tests** - Generate tests for Python code
- **python-doc-comments** - Generate documentation comments

## References

### Pattern Documentation

- **`python-best-practices.md`** - Comprehensive best practices reference included with this pattern

### External Resources

- [PEP 8 - Style Guide for Python Code](https://peps.python.org/pep-0008/)
- [PEP 20 - The Zen of Python](https://peps.python.org/pep-0020/)
- [Google Python Style Guide](https://google.github.io/styleguide/pyguide.html)
- [Real Python - Code Quality](https://realpython.com/python-code-quality/)

## Contributing

Contributions are welcome! If you have ideas for improving refactoring patterns or adding new checks, please submit a PR or open an issue.

## License

This pattern is part of the fabric-patterns-hub and follows the same license as the parent repository.

---

**Version:** 1.0.0
**Last Updated:** 2026-01-19
**Maintainer:** fabric-patterns-hub contributors
