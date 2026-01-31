# Python Documentation Standards

A comprehensive guide to Python documentation following PEP 8, PEP 257, and modern best practices. This document serves as the knowledge base for the python-doc-comments pattern.

## Table of Contents

1. [Core Principles](#core-principles)
2. [Docstrings (PEP 257)](#docstrings-pep-257)
3. [Comments (PEP 8)](#comments-pep-8)
4. [Type Hints](#type-hints)
5. [Codetags](#codetags)
6. [Magic Comments](#magic-comments)
7. [Linter Directives](#linter-directives)
8. [Common Mistakes](#common-mistakes)
9. [Quality Checklist](#quality-checklist)

---

## Core Principles

### The Golden Rules

| Principle | Description |
|-----------|-------------|
| Triple double quotes | Always use `"""` for docstrings |
| Complete sentences | Proper capitalization and punctuation |
| Explain "why" | Code shows "what", comments explain "why" |
| User focus | Document for users, not implementation |
| Synchronize | Outdated comments are worse than none |
| Use sparingly | Prefer clear code over excessive comments |

### Philosophy

- **Self-documenting code** is the first priority
- Comments should add value, not state the obvious
- Type hints complement docstrings, not replace them
- Professional tone in all documentation

---

## Docstrings (PEP 257)

Docstrings become the `__doc__` attribute of modules, functions, classes, and methods.

### One-Line Docstrings

For simple, obvious cases:

```python
def kos_root():
    """Return the pathname of the KOS root directory."""
    global _kos_root
    return _kos_root
```

**Rules:**

- Closing quotes on the **same line**
- **Imperative mood**: "Return X" not "Returns X"
- End with a **period**
- No blank lines before or after

### Multi-Line Docstrings

For complex documentation:

```python
def complex(real=0.0, imag=0.0):
    """Form a complex number.

    Keyword arguments:
    real -- the real part (default 0.0)
    imag -- the imaginary part (default 0.0)
    """
    if imag == 0.0 and real == 0.0:
        return complex_zero
```

**Structure:**

1. Summary line (fits on one line)
2. Blank line
3. Detailed description
4. Closing quotes on **separate line**

### Module Docstrings

```python
"""A one-line summary of the module, terminated by a period.

Leave a blank line. The rest of this docstring should contain an
overall description of the module. Optionally, it may also contain
a brief description of exported classes and functions.

Typical usage example:

    foo = ClassFoo()
    bar = foo.function_bar()
"""

import os
import sys
```

**Placement:**

- Must be the **first statement** in the module
- Comes **after** magic comments (shebang, encoding)
- Comes **before** imports

### Function/Method Docstrings (Google Style)

```python
def fetch_bigtable_rows(big_table, keys, other_silly_variable=None):
    """Fetch rows from a Bigtable.

    Retrieve rows pertaining to the given keys from the Table instance
    represented by big_table.

    Args:
        big_table: An open Bigtable Table instance.
        keys: A sequence of strings representing the key of each table
            row to fetch.
        other_silly_variable: Another optional variable, that has a much
            longer name than the other args.

    Returns:
        A dict mapping keys to the corresponding table row data fetched.
        Each row is represented as a tuple of strings. For example:

        {'Serak': ('Rigel VII', 'Preparer'),
         'Zim': ('Irk', 'Invader')}

    Raises:
        IOError: An error occurred accessing the bigtable.Table object.
    """
    pass
```

**Sections:**

| Section | Purpose |
|---------|---------|
| Args | List each parameter with description |
| Returns | Describe the return value and its type |
| Yields | For generators |
| Raises | Document exceptions that may be raised |

### Class Docstrings

```python
class SampleClass:
    """Summary of class here.

    Longer class information and detailed description.

    Attributes:
        likes_spam: A boolean indicating if we like SPAM or not.
        eggs: An integer count of the eggs we have laid.
    """

    def __init__(self, likes_spam=False):
        """Initialize the instance based on spam preference.

        Args:
            likes_spam: Defines if instance exhibits this preference.
        """
        self.likes_spam = likes_spam
        self.eggs = 0
```

**Key Points:**

- Summarize what instances represent
- List public attributes in **Attributes** section
- Document `__init__` separately

### Docstring Styles Comparison

**Google Style** (recommended):

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

**NumPy Style** (scientific computing):

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

    Raises
    ------
    ValueError
        When validation fails.
    """
    pass
```

**Sphinx/reStructuredText Style**:

```python
def function(arg1: int, arg2: str) -> bool:
    """Summary line.

    :param arg1: Description of arg1
    :type arg1: int
    :param arg2: Description of arg2
    :type arg2: str
    :return: Description of return value
    :rtype: bool
    :raises ValueError: When validation fails
    """
    pass
```

---

## Comments (PEP 8)

### Block Comments

Block comments apply to code that follows them.

```python
# Filter out inactive users and sort by registration date.
# This is necessary because the database query doesn't
# guarantee ordering for performance reasons.
active_users = [u for u in users if u.is_active]
return sorted(active_users, key=lambda u: u.registered_at)
```

**Rules:**

- Start with `#` followed by a single space
- Indent to the same level as the code
- Separate paragraphs with a line containing only `#`

### Inline Comments

Inline comments appear on the same line as code. Use sparingly.

```python
# Good: Explains non-obvious reasoning
x = x + 1  # Compensate for border offset in rendering

# Good: Clarifies business logic
discount = 0.9  # 10% discount for premium members

# Bad: States the obvious
x = x + 1  # Increment x
```

**Rules:**

- Separate from code by **at least 2 spaces**
- Start with `#` and a single space
- Maximum **72 characters** recommended
- Use sparingly - only when adding real value

### When Comments Add Value

```python
# Explains business logic
discount = 0.25 if user.is_grandfathered else 0.20

# Warns about gotchas
# Note: This modifies the list in-place
items.sort()

# Documents performance implications
# Using dict lookup O(1) instead of list search O(n)
user_map = {user.id: user for user in users}

# Explains non-obvious algorithms
# Boyer-Moore voting algorithm - O(n) time, O(1) space
candidate = None
```

---

## Type Hints

Type hints complement docstrings by providing static type information.

### Basic Types

```python
def greeting(name: str) -> str:
    """Return a greeting message."""
    return f'Hello {name}'
```

### Complex Types

```python
from typing import List, Dict, Optional, Union

def process_items(
    items: List[str],
    config: Optional[Dict[str, Union[str, int]]] = None
) -> List[Dict[str, str]]:
    """Process items with optional configuration.

    Args:
        items: List of item names to process.
        config: Optional configuration dictionary.

    Returns:
        List of processed item dictionaries.
    """
    pass
```

### Modern Python (3.10+)

```python
def process(items: list[str], config: dict[str, str | int] | None = None) -> list[dict[str, str]]:
    """Process items with optional configuration."""
    pass
```

### Key Points

| Aspect | Description |
|--------|-------------|
| Static analysis | Used by mypy, pyright, IDEs |
| Documentation | Human-readable, stays in code |
| Runtime | Type hints don't enforce at runtime |
| Combination | Use both type hints AND docstrings |

---

## Codetags

Standardized annotations that flag code requiring attention.

### Common Tags

| Tag | Purpose | Example |
|-----|---------|---------|
| `TODO` | Pending tasks | `# TODO: Add input validation` |
| `FIXME` | Code needing fixes | `# FIXME: Memory leak in loop` |
| `XXX` | Synonym for FIXME | `# XXX: This breaks with unicode` |
| `BUG` | Known defects | `# BUG: Tracked as JIRA-456` |
| `HACK` | Temporary workarounds | `# HACK: Works around API bug` |
| `NOTE` | Important clarifications | `# NOTE: Requires Python 3.8+` |

### Format with Metadata

```python
# TODO: Refactor this function. <JD d:2025-01-15 p:2>
def legacy_code():
    pass

# FIXME: Memory leak in loop. <AS t:JIRA-789 p:1>
def leaky_function():
    pass
```

**Metadata Fields:**

- `<initials>` - Owner/author
- `d:date` - Due date (ISO 8601)
- `p:0-3` - Priority (0=highest)
- `t:id` - Tracker reference

### Best Practices

1. Include ownership (initials or name)
2. Add context (ticket numbers, due dates)
3. Be specific about the problem
4. For serious issues, create tickets in issue tracker
5. Keep TODOs temporary - fix or move to tracker

---

## Magic Comments

Special comments that provide instructions to the interpreter.

### Shebang Line

```python
#!/usr/bin/env python3
```

**Rules:**

- Must be the **first line**
- `#!/usr/bin/env python3` is portable (uses PATH)
- Only needed for executable scripts

### Encoding Declaration (PEP 263)

```python
# -*- coding: utf-8 -*-
```

**Rules:**

- Must be on first or second line
- Usually **unnecessary** (UTF-8 is default in Python 3)
- Required only for non-UTF-8 encodings

### Complete File Header

```python
#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Module docstring comes after magic comments.

This is the proper order for file headers in Python.
"""

import os
import sys
```

---

## Linter Directives

Special comments to control static analysis tools.

### Type Checking (mypy, pyright)

```python
# type: ignore
result = some_untyped_function()  # type: ignore

# Specific error
value = process(wrong_type)  # type: ignore[arg-type]
```

### Style Checking (flake8)

```python
# noqa
long_line = "Very long line"  # noqa

# Specific rule
another_long_line = "Long line"  # noqa: E501

# Unused import
from module import unused  # noqa: F401
```

### Linting (pylint)

```python
# pylint: disable=line-too-long
very_long_line = "Intentionally long"

# pylint: disable=broad-except
try:
    risky()
except Exception:  # pylint: disable=broad-except
    handle()
```

### Code Coverage

```python
def debug_function():  # pragma: no cover
    """Only used during development."""
    print("Debug")

if __name__ == "__main__":  # pragma: no cover
    main()
```

### Formatting (black)

```python
# fmt: off
matrix = [
    [1, 0, 0],
    [0, 1, 0],
    [0, 0, 1],
]
# fmt: on
```

### Best Practices for Directives

1. Be specific - use exact error codes
2. Add explanations - why ignoring?
3. Minimize scope - apply to smallest section
4. Review regularly - suppressed warnings may hide issues

---

## Common Mistakes

### Redundant Comments

**Bad:**

```python
x = x + 1  # Increment x
name = "John"  # Set name to John
```

**Good:**

```python
x = x + 1  # Compensate for zero-based indexing
```

### Redundant Docstrings

**Bad:**

```python
def add(a, b):
    """Add two numbers.

    This function takes two numbers and adds them together.
    It returns the sum of a and b.
    """
    return a + b
```

**Good:**

```python
def add(a, b):
    """Return the sum of a and b."""
    return a + b
```

### Commented-Out Code

**Bad:**

```python
def current():
    # old_way()
    # previous_version()
    return new_way()
```

**Good:**

```python
def current():
    return new_way()
```

### Lying Comments

**Bad:**

```python
# Return the user's full name
def get_username(user):
    return user.email  # Actually returns email!
```

### Over-Commenting

**Bad:**

```python
def process_order(order):
    # Validate the order
    if not order.is_valid():
        # Raise exception if invalid
        raise ValueError("Invalid order")

    # Calculate the total
    total = order.calculate_total()

    # Return the final total
    return total
```

**Good:**

```python
def process_order(order):
    if not order.is_valid():
        raise ValueError("Invalid order")
    return order.calculate_total()
```

### Missing Type Context

**Old way (duplicates type info):**

```python
def process_user(user):
    """Process a user.

    Args:
        user (dict): User dictionary with 'id', 'name', 'email'

    Returns:
        bool: True if successful
    """
    pass
```

**Modern way (types in signature):**

```python
from typing import TypedDict

class User(TypedDict):
    id: int
    name: str
    email: str

def process_user(user: User) -> bool:
    """Process a user and update the database."""
    pass
```

---

## Tools and Automation

### Recommended Tools

| Tool | Purpose |
|------|---------|
| [black](https://github.com/psf/black) | Code formatting |
| [isort](https://github.com/PyCQA/isort) | Import sorting |
| [mypy](https://github.com/python/mypy) | Type checking |
| [pylint](https://github.com/pylint-dev/pylint) | Linting |
| [flake8](https://github.com/PyCQA/flake8) | Style checking |
| [pydocstyle](https://github.com/PyCQA/pydocstyle) | Docstring checking |

### pyproject.toml Configuration

```toml
[tool.black]
line-length = 88

[tool.mypy]
strict = true

[tool.pylint.messages_control]
max-line-length = 88

[tool.pydocstyle]
convention = "google"
```

### Documentation Generators

| Tool | Description |
|------|-------------|
| [Sphinx](https://www.sphinx-doc.org/) | Full documentation suite |
| [MkDocs](https://www.mkdocs.org/) | Markdown-based docs |
| [pdoc](https://pdoc.dev/) | Auto-generated API docs |

---

## Quality Checklist

### Docstrings

- [ ] Triple double quotes (`"""`)
- [ ] All public modules, classes, and functions documented
- [ ] One-line format for simple cases
- [ ] Multi-line format with summary, blank line, details
- [ ] Args, Returns, Raises sections for functions
- [ ] Attributes section for classes
- [ ] Imperative mood for one-liners

### Comments

- [ ] Block comments indented with code
- [ ] Inline comments used sparingly (2+ spaces)
- [ ] Explain "why" not "what"
- [ ] No obvious or redundant comments
- [ ] Complete sentences with proper grammar
- [ ] Updated when code changes

### Type Hints

- [ ] Used for function parameters and returns
- [ ] Complex types imported from `typing`
- [ ] Complement docstrings, not replace them

### Tool Integration

- [ ] Linter directives used sparingly
- [ ] Specific error codes when possible
- [ ] Code formatted consistently

### Code Quality

- [ ] Code is self-documenting with clear names
- [ ] No commented-out code
- [ ] TODOs include context and ownership
- [ ] Professional and respectful tone

---

## References

- [PEP 8 - Style Guide for Python Code](https://peps.python.org/pep-0008/)
- [PEP 257 - Docstring Conventions](https://peps.python.org/pep-0257/)
- [PEP 263 - Source Code Encodings](https://peps.python.org/pep-0263/)
- [PEP 350 - Codetags](https://peps.python.org/pep-0350/)
- [PEP 484 - Type Hints](https://peps.python.org/pep-0484/)
- [Google Python Style Guide](https://google.github.io/styleguide/pyguide.html)

---

*Last updated: 2026-01-11*
