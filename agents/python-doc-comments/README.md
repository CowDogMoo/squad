# python-doc-comments

Generate or improve Python documentation, docstrings, and type hints following PEP standards.

## Overview

This agent specializes in creating high-quality Python documentation that follows:

- [PEP 8](https://peps.python.org/pep-0008/) - Style Guide for Python Code
- [PEP 257](https://peps.python.org/pep-0257/) - Docstring Conventions
- [PEP 484](https://peps.python.org/pep-0484/) - Type Hints
- [Google Python Style Guide](https://google.github.io/styleguide/pyguide.html)

## Capabilities

- **Add missing docstrings** for modules, classes, functions, and methods
- **Improve existing documentation** to follow PEP conventions
- **Add type hints** for parameters and return values
- **Use Google-style docstrings** (can adapt to NumPy/Sphinx if detected)
- **Add meaningful comments** that explain "why" not "what"
- **Preserve code logic** - only modifies documentation

## Usage

```bash
# Document a single file
squad run python-doc-comments path/to/file.py

# Document multiple files
squad run python-doc-comments path/to/*.py

# Document entire package
squad run python-doc-comments path/to/package/
```

## What Gets Documented

The agent documents all public (non-underscore) declarations:

- **Modules** - Overview and usage examples
- **Classes** - Purpose, attributes, and behavior
- **Functions/Methods** - Summary, Args, Returns, Raises sections
- **Type Hints** - Parameters and return types
- **Block Comments** - Explain complex logic
- **Codetags** - TODO, FIXME, NOTE with context

## Documentation Standards

The agent follows these key principles:

1. **Triple double quotes** (`"""`) for all docstrings
2. **Imperative mood** for one-liners: "Return X" not "Returns X"
3. **Complete sentences** with proper punctuation
4. **User focus** - explain "why" not implementation details
5. **Type hints** complement docstrings
6. **No redundancy** - avoid obvious comments

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

    Raises
    ------
    ValueError
        When validation fails.
    """
    pass
```

## Examples

### Before

```python
class Cache:
    def __init__(self, size):
        self.size = size
        self.data = {}

    def get(self, key):
        return self.data.get(key)
```

### After

```python
class Cache:
    """Thread-safe in-memory cache with size limits.

    Attributes:
        size: Maximum number of items to cache.
        data: Internal storage dictionary.
    """

    def __init__(self, size: int) -> None:
        """Initialize cache with maximum size.

        Args:
            size: Maximum number of items to store.
        """
        self.size = size
        self.data: dict[str, Any] = {}

    def get(self, key: str) -> Any | None:
        """Retrieve value for key from cache.

        Args:
            key: Cache key to look up.

        Returns:
            Cached value if present, None otherwise.
        """
        return self.data.get(key)
```

## Reference

The agent uses a comprehensive knowledge base covering:

- Docstring conventions (one-line and multi-line formats)
- Google, NumPy, and Sphinx docstring styles
- Block and inline comment guidelines
- Type hints from the `typing` module
- Codetags (TODO, FIXME, NOTE, BUG, HACK)
- Linter directives (mypy, flake8, pylint, black)
- Common mistakes and anti-patterns
- Quality checklist

See [references/python-documentation-standards.md](references/python-documentation-standards.md) for the complete reference.

## Related Agents

- **python-review** - Review Python code for best practices
- **python-tests** - Generate Python tests
