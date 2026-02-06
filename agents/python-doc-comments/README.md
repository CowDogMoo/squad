# python-doc-comments

Autonomous Python documentation agent that discovers public declarations, adds
or improves docstrings following PEP 257 and Google Style conventions, and
verifies linting passes.

## Overview

This agent specializes in creating high-quality Python documentation that
follows:

- [PEP 257](https://peps.python.org/pep-0257/) — Docstring Conventions
- [PEP 8](https://peps.python.org/pep-0008/) — Style Guide for Python Code
- [Google Python Style Guide](https://google.github.io/styleguide/pyguide.html)
- [PEP 484](https://peps.python.org/pep-0484/) — Type Hints

## Modes

### Normal (edit) mode

Discovers all Python source files, catalogs missing or deficient docstrings on
public declarations, applies fixes, verifies linting, and reports results.

```bash
task run:python-doc-comments WORKING_DIR=/path/to/repo
task run:python-doc-comments WORKING_DIR=/path/to/repo MODEL=gpt-4o PROVIDER=openai
```

### Readonly (analysis) mode

Produces a prioritized report of docstring gaps without modifying any files.

```bash
task run:python-doc-comments-analyze WORKING_DIR=/path/to/repo
task run:python-doc-comments-analyze WORKING_DIR=/path/to/repo MODEL=gpt-4o PROVIDER=openai
```

## What Gets Documented

The agent documents all public (non-underscore) declarations:

- **Modules** — First statement describing module purpose
- **Classes** — What instances represent, Attributes section
- **Functions/Methods** — Summary, Args, Returns, Raises sections
- **Properties** — What the property represents
- **Constants** — Purpose and valid values

## What Gets Skipped

- Private (leading underscore) functions/methods/classes
- Code logic, signatures, or behavior (only docstrings are modified)
- Test files (`test_*.py`, `*_test.py`) and virtual environments
- Trivial functions where a docstring would just restate the name
- `-> None` return annotations (always inferable)
- `__pycache__` directories

## Hard Rules

Key constraints that prevent common failure modes:

1. **Only modify docstrings** — never change code logic
2. **Triple double quotes** — always use `"""`, never `'''`
3. **Start with summary line** — imperative mood for functions
4. **No blank line before docstring** — immediately follows def/class
5. **Complete sentences** — fragments are not docstrings
6. **No redundant docstrings** — "Process processes the data" = skip
7. **Proportional** — match docstring length to function complexity
8. **Focus on WHAT, not HOW** — no implementation details
9. **Match existing style** — NumPy/Sphinx if present, else Google
10. **NEVER add `-> None`** — it's always inferable

## Output Format

### Normal mode

- Changes Summary
- Docstrings Added (with file, line, category, docstring, reason)
- Docstrings Improved (with before/after)
- Declarations Skipped (table with reasons)
- Files Touched
- Validation (`python -m py_compile`, `ruff check`)

### Readonly mode

- Analysis Summary (files analyzed, finding counts by severity)
- Findings (with severity, category, file, line, suggested docstring)
- Priority Order (ranked by impact)
- Recommendations

## Docstring Styles

### Google Style (Default)

```python
def function(arg1: int, arg2: str) -> bool:
    """Summary line.

    Args:
        arg1: Description of arg1.
        arg2: Description of arg2.

    Returns:
        Description of return value.

    Raises:
        ValueError: When validation fails.
    """
    pass
```

### NumPy Style

```python
def function(arg1: int, arg2: str) -> bool:
    """Summary line.

    Parameters
    ----------
    arg1 : int
        Description of arg1.
    arg2 : str
        Description of arg2.

    Returns
    -------
    bool
        Description of return value.
    """
    pass
```

## Reference

See [references/python-documentation-standards.md](references/python-documentation-standards.md)
for the complete knowledge base covering PEP 257, docstring styles, comment
rules, type hints, codetags, linter directives, and quality checklist.

## Related Agents

- **python-review** — review Python code for best practices
- **python-tests** — generate Python tests
