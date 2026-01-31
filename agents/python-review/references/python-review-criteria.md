# Python Code Review Criteria

A comprehensive guide to reviewing Python code for quality, correctness, performance, and adherence to best practices. This document serves as the knowledge base for the python-review pattern and incorporates guidelines from PEP 8 and the Google Python Style Guide.

## Table of Contents

1. [Review Philosophy](#review-philosophy)
2. [Code Formatting and Style](#code-formatting-and-style)
3. [Error and Exception Handling](#error-and-exception-handling)
4. [Type Annotations](#type-annotations)
5. [Data Structures](#data-structures)
6. [Function and Class Design](#function-and-class-design)
7. [Code Structure](#code-structure)
8. [API Design Patterns](#api-design-patterns)
9. [Performance](#performance)
10. [Module Organization](#module-organization)
11. [Documentation](#documentation)
12. [Security Considerations](#security-considerations)
13. [Testing](#testing)
14. [Severity Classification](#severity-classification)

---

## Review Philosophy

### Core Principles

**Readability Counts (PEP 20)**
Code is read much more often than it is written. Clarity and maintainability trump clever abstractions.

**Explicit is Better than Implicit**
Prefer clear, explicit code over magic or implicit behavior that requires deep understanding.

**Simple is Better than Complex**
Choose straightforward solutions. If something is hard to explain, it's probably too complex.

**Constructive Feedback**
- Be educational, not critical
- Explain the "why" behind suggestions
- Provide concrete examples with code
- Acknowledge good practices
- Prioritize actionable feedback
- Focus on idiomatic Python patterns, not personal preferences

### Style Guide Hierarchy

1. **PEP 8** - Python's official style guide (baseline)
2. **Google Python Style Guide** - Additional recommendations for enterprise code
3. **Project-specific conventions** - Local team standards

---

## Code Formatting and Style

### PEP 8 Mandatory Checks

| Check | Severity | Reference |
|-------|----------|-----------|
| 4 spaces per indentation level | HIGH | PEP 8 |
| Maximum 79 characters per line | MEDIUM | PEP 8 |
| Maximum 72 characters for docstrings/comments | LOW | PEP 8 |
| Two blank lines around top-level definitions | MEDIUM | PEP 8 |
| One blank line between methods | MEDIUM | PEP 8 |
| No trailing whitespace | LOW | PEP 8 |

### Import Organization

Imports should be in three groups separated by blank lines (PEP 8, Google Style):

```python
# Standard library
import os
import sys
from typing import Optional

# Third-party packages
import requests
from flask import Flask

# Local packages
from myproject.utils import helper
from myproject.models import User
```

**Rules:**
- One import per line for regular imports
- Multiple items OK for `from x import y, z`
- Absolute imports preferred over relative imports
- Never use wildcard imports (`from module import *`)
- Sort lexicographically within groups (Google Style)

### Naming Conventions

**PEP 8 Naming:**

| Type | Convention | Example |
|------|------------|---------|
| Modules | `lowercase_underscore` | `user_service.py` |
| Packages | `lowercase` | `mypackage` |
| Classes | `CapWords` | `UserService` |
| Exceptions | `CapWords` + `Error` suffix | `ValidationError` |
| Functions | `lowercase_underscore` | `get_user()` |
| Methods | `lowercase_underscore` | `calculate_total()` |
| Constants | `UPPER_CASE_UNDERSCORE` | `MAX_RETRIES` |
| Variables | `lowercase_underscore` | `user_count` |
| Protected | `_single_underscore` | `_internal_value` |
| Private | `__double_underscore` | `__name_mangled` |

**Google Style Additions:**
- Avoid single-character names except for counters (`i`, `j`, `k`) and exceptions (`e`)
- Don't encode type information in names: `names` not `names_list`
- Avoid dashes in module names

**Good:**
```python
def calculate_total_price(items: list[Item]) -> float:
    pass

class UserRepository:
    MAX_RETRIES = 3

    def _validate_user(self, user: User) -> bool:
        pass
```

**Bad:**
```python
def CalcTotalPrice(itemsList):  # Wrong case, type in name
    pass

class user_repository:  # Should be CapWords
    maxRetries = 3  # Should be UPPER_CASE
```

### Whitespace Rules (PEP 8)

**Good:**
```python
spam(ham[1], {eggs: 2})
x = 1
y = 2
long_variable = 3

def complex(real, imag=0.0):
    return magic(r=real, i=imag)
```

**Bad:**
```python
spam( ham[ 1 ], { eggs: 2 } )  # Extra whitespace
x             = 1  # Aligned equals
long_variable = 3

def complex(real, imag = 0.0):  # Space around = in default
    return magic(r = real, i = imag)  # Space around = in keyword
```

### Line Continuation

**Preferred - implicit continuation:**
```python
# Aligned with opening delimiter
result = function_name(arg_one, arg_two,
                       arg_three, arg_four)

# Hanging indent
result = function_name(
    arg_one, arg_two,
    arg_three, arg_four)

# Break before binary operators
income = (gross_wages
          + taxable_interest
          + (dividends - qualified_dividends)
          - ira_deduction)
```

---

## Error and Exception Handling

### Critical Rules

| Rule | Severity | Reference |
|------|----------|-----------|
| Never use bare `except:` | CRITICAL | PEP 8, Google |
| Catch specific exceptions | HIGH | Google Style |
| Minimize code in try block | HIGH | Google Style |
| Use `finally` for cleanup | MEDIUM | Google Style |
| Use `raise X from Y` for chaining | MEDIUM | PEP 8 |

### Exception Patterns

**Good:**
```python
try:
    value = collection[key]
except KeyError as e:
    raise ConfigurationError(f"Missing key: {key}") from e
```

**Bad:**
```python
try:
    # Too much code in try block
    data = fetch_data()
    process_data(data)
    save_data(data)
except:  # Bare except - catches everything including SystemExit
    pass  # Silently ignoring errors
```

### Resource Cleanup

**Use context managers:**
```python
# Good
with open(filename) as f:
    content = f.read()

# Good - multiple resources
with open(input_file) as fin, open(output_file, 'w') as fout:
    fout.write(fin.read())
```

**Custom cleanup:**
```python
# Good - contextlib for cleanup
from contextlib import contextmanager

@contextmanager
def managed_resource():
    resource = acquire_resource()
    try:
        yield resource
    finally:
        release_resource(resource)
```

### Exception Design

**Good:**
```python
class ValidationError(Exception):
    """Raised when validation fails."""

    def __init__(self, message: str, field: str | None = None):
        super().__init__(message)
        self.field = field
```

**Raising exceptions:**
```python
# Good - specific exception with context
if not user.is_active:
    raise PermissionError(f"User {user.id} is not active")

# Good - exception chaining
try:
    config = load_config(path)
except FileNotFoundError as e:
    raise ConfigurationError(f"Config file not found: {path}") from e
```

---

## Type Annotations

### Guidelines (Google Style, PEP 484)

| Check | Severity | Rationale |
|-------|----------|-----------|
| Annotate public APIs | HIGH | Documentation and tooling |
| Use `X | None` not `Optional[X]` (3.10+) | LOW | Modern syntax |
| Annotate complex functions | MEDIUM | Clarity |
| Import types correctly | MEDIUM | Avoid runtime overhead |

### Basic Annotations

```python
from typing import Any
from collections.abc import Sequence, Mapping

def process_items(
    items: Sequence[str],
    config: Mapping[str, Any],
    limit: int | None = None,
) -> list[str]:
    """Process items according to config."""
    pass

class UserService:
    def __init__(self, db: Database) -> None:
        self._db = db

    def get_user(self, user_id: int) -> User | None:
        pass
```

### Type Aliases

```python
# Good - type aliases for complex types
UserId = int
UserMapping = dict[UserId, User]
Callback = Callable[[str, int], bool]

def process_users(users: UserMapping) -> None:
    pass
```

### Generics

```python
from typing import TypeVar

T = TypeVar('T')

def first_or_none(items: Sequence[T]) -> T | None:
    return items[0] if items else None
```

---

## Data Structures

### Comprehensions (Google Style)

**Rules:**
- Use for simple transformations
- Avoid multiple `for` clauses or complex filter expressions
- Keep readable - if hard to understand, use a loop

**Good:**
```python
# Simple comprehension
squares = [x * x for x in numbers]

# Simple filtering
evens = [x for x in numbers if x % 2 == 0]

# Dict comprehension
user_map = {u.id: u for u in users}
```

**Bad:**
```python
# Too complex - use explicit loop
result = [
    transform(x, y)
    for x in outer
    for y in inner
    if condition(x)
    if other_condition(y)
]
```

### Generator Expressions

```python
# Good - memory efficient for large sequences
total = sum(x * x for x in large_sequence)

# Good - lazy evaluation
first_match = next((x for x in items if predicate(x)), None)
```

### Mutability Concerns

**Critical - mutable default arguments:**
```python
# Bad - shared mutable default
def append_to(item, target=[]):  # Bug!
    target.append(item)
    return target

# Good - None default with initialization
def append_to(item, target=None):
    if target is None:
        target = []
    target.append(item)
    return target
```

**Defensive copying:**
```python
class DataHolder:
    def __init__(self, items: list[str]) -> None:
        self._items = list(items)  # Copy to prevent external modification

    @property
    def items(self) -> list[str]:
        return list(self._items)  # Return copy
```

---

## Function and Class Design

### Function Guidelines

**Google Style - keep functions small and focused:**
- Prefer functions under 40 lines
- Single responsibility principle
- Limit parameters (consider dataclass/dict for many args)

**Good:**
```python
def calculate_order_total(
    items: list[OrderItem],
    discount: Discount | None = None,
    tax_rate: float = 0.0,
) -> Money:
    """Calculate the total price for an order.

    Args:
        items: List of items in the order.
        discount: Optional discount to apply.
        tax_rate: Tax rate as decimal (e.g., 0.08 for 8%).

    Returns:
        Total price including tax and discount.
    """
    subtotal = sum(item.price * item.quantity for item in items)
    if discount:
        subtotal = discount.apply(subtotal)
    return Money(subtotal * (1 + tax_rate))
```

### Class Guidelines

```python
class UserService:
    """Service for user management operations.

    Attributes:
        repository: The user repository for persistence.
        cache: Optional cache for user lookups.
    """

    def __init__(
        self,
        repository: UserRepository,
        cache: Cache | None = None,
    ) -> None:
        self._repository = repository
        self._cache = cache

    def get_user(self, user_id: int) -> User | None:
        """Retrieve a user by ID.

        Args:
            user_id: The unique user identifier.

        Returns:
            The user if found, None otherwise.
        """
        if self._cache:
            cached = self._cache.get(f"user:{user_id}")
            if cached:
                return cached
        return self._repository.find_by_id(user_id)
```

### Properties (Google Style)

Use properties when:
- Access is cheap and straightforward
- No side effects expected
- Behavior is obvious from the name

```python
class Circle:
    def __init__(self, radius: float) -> None:
        self._radius = radius

    @property
    def radius(self) -> float:
        return self._radius

    @radius.setter
    def radius(self, value: float) -> None:
        if value < 0:
            raise ValueError("Radius cannot be negative")
        self._radius = value

    @property
    def area(self) -> float:
        """Calculate area (cheap computation)."""
        return math.pi * self._radius ** 2
```

---

## Code Structure

### Early Returns

**Good:**
```python
def process_user(user: User | None) -> Result:
    if user is None:
        return Result.error("No user provided")

    if not user.is_active:
        return Result.error("User is inactive")

    if not user.has_permission("process"):
        return Result.error("Permission denied")

    # Main logic - not nested
    return perform_processing(user)
```

**Bad - deep nesting:**
```python
def process_user(user: User | None) -> Result:
    if user is not None:
        if user.is_active:
            if user.has_permission("process"):
                return perform_processing(user)
            else:
                return Result.error("Permission denied")
        else:
            return Result.error("User is inactive")
    else:
        return Result.error("No user provided")
```

### Boolean Checks (PEP 8, Google Style)

**Good:**
```python
# Use truthiness for sequences
if items:  # Not: if len(items) > 0
    process(items)

# Explicit None check
if value is None:
    value = default

# Avoid comparing to True/False
if enabled:  # Not: if enabled == True
    run()
```

### String Checks

```python
# Good - use methods
if filename.endswith('.py'):
    pass

if text.startswith('http'):
    pass

# Bad - slicing
if filename[-3:] == '.py':  # Use .endswith()
    pass
```

---

## API Design Patterns

### Decorators

```python
import functools
from typing import Callable, TypeVar

T = TypeVar('T', bound=Callable)

def retry(max_attempts: int = 3) -> Callable[[T], T]:
    """Retry a function on failure."""
    def decorator(func: T) -> T:
        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            last_error = None
            for attempt in range(max_attempts):
                try:
                    return func(*args, **kwargs)
                except Exception as e:
                    last_error = e
            raise last_error
        return wrapper  # type: ignore
    return decorator

@retry(max_attempts=3)
def fetch_data(url: str) -> dict:
    pass
```

### Context Managers

```python
from contextlib import contextmanager
from typing import Iterator

@contextmanager
def timer(name: str) -> Iterator[None]:
    """Time a code block."""
    start = time.perf_counter()
    try:
        yield
    finally:
        elapsed = time.perf_counter() - start
        logger.info(f"{name} took {elapsed:.3f}s")

# Usage
with timer("data processing"):
    process_data()
```

### Protocols (Structural Typing)

```python
from typing import Protocol

class Readable(Protocol):
    def read(self, size: int = -1) -> bytes: ...

class Writable(Protocol):
    def write(self, data: bytes) -> int: ...

def copy_data(source: Readable, dest: Writable) -> int:
    """Copy data from source to destination."""
    data = source.read()
    return dest.write(data)
```

---

## Performance

### String Operations

| Pattern | Performance | Use Case |
|---------|-------------|----------|
| f-strings | Fastest | Simple formatting |
| `str.join()` | Fast | Multiple concatenations |
| `%` formatting | Medium | Logging with deferred evaluation |
| `+` in loop | Slow | Avoid |

**Good:**
```python
# f-strings for formatting
message = f"User {user.name} logged in at {timestamp}"

# join for multiple strings
result = "".join(parts)
result = ", ".join(names)

# Logging with % for deferred evaluation
logger.debug("Processing user %s", user_id)
```

**Bad:**
```python
# String concatenation in loop
result = ""
for item in items:
    result += str(item)  # Creates new string each time
```

### Loop Optimization

```python
# Good - list comprehension (faster than loop)
squares = [x ** 2 for x in numbers]

# Good - generator for large data
total = sum(x ** 2 for x in large_numbers)

# Good - avoid repeated attribute lookup
append = result.append
for item in items:
    append(process(item))

# Good - use built-ins
if any(predicate(x) for x in items):
    pass
```

### Memory Efficiency

```python
# Good - generator for large sequences
def read_large_file(path: str) -> Iterator[str]:
    with open(path) as f:
        for line in f:
            yield line.strip()

# Good - slots for many instances
class Point:
    __slots__ = ('x', 'y')

    def __init__(self, x: float, y: float) -> None:
        self.x = x
        self.y = y
```

---

## Module Organization

### Module Structure (Google Style)

```python
"""Module docstring describing purpose and usage.

Typical usage example:

    from mymodule import main_function
    result = main_function(data)
"""

# Standard library imports
import os
import sys

# Third-party imports
import requests

# Local imports
from .utils import helper

# Module-level constants
DEFAULT_TIMEOUT = 30
MAX_RETRIES = 3

# Module-level type aliases
Config = dict[str, Any]

# Classes and functions
class MainClass:
    pass

def main_function():
    pass

# Main execution
if __name__ == '__main__':
    main()
```

### Global Variables

| Check | Severity | Rationale |
|-------|----------|-----------|
| Avoid mutable global state | HIGH | Testing difficulty |
| Constants are acceptable | LOW | Immutable values |
| Prefix internal globals with `_` | MEDIUM | Clear intent |

**Good:**
```python
# Constants are fine
MAX_CONNECTIONS = 100
DEFAULT_CONFIG = {"timeout": 30}  # Don't mutate!

# Internal module state prefixed
_cache: dict[str, Any] = {}

def get_cached(key: str) -> Any:
    return _cache.get(key)
```

---

## Documentation

### Docstring Format (Google Style)

```python
def fetch_user(
    user_id: int,
    include_inactive: bool = False,
) -> User | None:
    """Fetch a user from the database.

    Retrieves user information by ID, optionally including
    inactive users in the search.

    Args:
        user_id: The unique identifier for the user.
        include_inactive: Whether to include inactive users.
            Defaults to False.

    Returns:
        The User object if found, None otherwise.

    Raises:
        DatabaseError: If the database connection fails.
        ValueError: If user_id is negative.

    Example:
        >>> user = fetch_user(123)
        >>> print(user.name)
        'John Doe'
    """
    pass
```

### Class Docstrings

```python
class UserRepository:
    """Repository for user persistence operations.

    Provides CRUD operations for User entities with caching
    and validation support.

    Attributes:
        connection: Database connection instance.
        cache_ttl: Time-to-live for cached entries in seconds.

    Example:
        >>> repo = UserRepository(connection)
        >>> user = repo.find_by_id(123)
    """

    def __init__(
        self,
        connection: Connection,
        cache_ttl: int = 300,
    ) -> None:
        """Initialize the repository.

        Args:
            connection: Database connection to use.
            cache_ttl: Cache TTL in seconds. Defaults to 300.
        """
        self.connection = connection
        self.cache_ttl = cache_ttl
```

### Comment Quality

| Rule | Severity |
|------|----------|
| Document "why", not "what" | MEDIUM |
| Keep comments updated | HIGH |
| No commented-out code | LOW |
| Use TODO format: `# TODO(username): description` | LOW |

---

## Security Considerations

### Critical Checks

| Check | Severity | Impact |
|-------|----------|--------|
| Input validation | CRITICAL | Injection attacks |
| SQL parameterization | CRITICAL | SQL injection |
| Subprocess shell=False | CRITICAL | Command injection |
| Secret management | CRITICAL | Credential exposure |
| HTTPS verification | HIGH | MITM attacks |

### Input Validation

```python
# Good - validate and sanitize
def process_username(username: str) -> str:
    if not username:
        raise ValueError("Username cannot be empty")
    if len(username) > 100:
        raise ValueError("Username too long")
    # Sanitize: only allow alphanumeric and underscore
    if not re.match(r'^[a-zA-Z0-9_]+$', username):
        raise ValueError("Invalid characters in username")
    return username.lower()
```

### SQL Queries

**Good:**
```python
# Parameterized query
cursor.execute(
    "SELECT * FROM users WHERE id = %s AND active = %s",
    (user_id, True)
)

# Using ORM
user = session.query(User).filter(User.id == user_id).first()
```

**Bad:**
```python
# SQL injection vulnerability!
cursor.execute(f"SELECT * FROM users WHERE id = {user_id}")
cursor.execute("SELECT * FROM users WHERE name = '%s'" % name)
```

### Subprocess Security

**Good:**
```python
# shell=False with list arguments
subprocess.run(["git", "status"], check=True)

# With capture
result = subprocess.run(
    ["ls", "-la", path],
    capture_output=True,
    text=True,
    check=True,
)
```

**Bad:**
```python
# Command injection vulnerability!
subprocess.run(f"git clone {url}", shell=True)
os.system(f"rm -rf {directory}")
```

### Secrets Management

```python
# Good - environment variables
import os

api_key = os.environ.get("API_KEY")
if not api_key:
    raise ConfigurationError("API_KEY not set")

# Good - secrets file
from pathlib import Path

secrets_file = Path("/run/secrets/api_key")
api_key = secrets_file.read_text().strip()
```

**Bad:**
```python
# Hardcoded secrets
API_KEY = "sk-1234567890abcdef"  # Never do this!
```

---

## Testing

### Coverage Expectations

| Type | Target | Priority |
|------|--------|----------|
| Unit tests | 80%+ | HIGH |
| Integration tests | Critical paths | MEDIUM |
| Property tests | Edge cases | LOW |

### Testing Tools (2025)

| Tool | Use Case |
|------|----------|
| `pytest` | Primary test framework |
| `pytest-cov` | Coverage reporting |
| `pytest-mock` | Mocking support |
| `hypothesis` | Property-based testing |
| `mypy` | Static type checking |
| `ruff` | Linting and formatting |

### Test Quality

```python
import pytest
from unittest.mock import Mock, patch

class TestUserService:
    """Tests for UserService."""

    @pytest.fixture
    def service(self) -> UserService:
        """Create a UserService with mock repository."""
        repository = Mock(spec=UserRepository)
        return UserService(repository)

    def test_get_user_returns_user_when_found(
        self,
        service: UserService,
    ) -> None:
        """Should return user when ID exists."""
        expected = User(id=1, name="Test")
        service._repository.find_by_id.return_value = expected

        result = service.get_user(1)

        assert result == expected
        service._repository.find_by_id.assert_called_once_with(1)

    def test_get_user_returns_none_when_not_found(
        self,
        service: UserService,
    ) -> None:
        """Should return None when ID doesn't exist."""
        service._repository.find_by_id.return_value = None

        result = service.get_user(999)

        assert result is None

    @pytest.mark.parametrize("user_id,expected_error", [
        (-1, ValueError),
        (0, ValueError),
    ])
    def test_get_user_raises_for_invalid_id(
        self,
        service: UserService,
        user_id: int,
        expected_error: type,
    ) -> None:
        """Should raise ValueError for invalid IDs."""
        with pytest.raises(expected_error):
            service.get_user(user_id)
```

---

## Severity Classification

### CRITICAL

Issues that affect correctness, security, or cause crashes:
- SQL injection vulnerabilities
- Command injection vulnerabilities
- Bare `except:` clauses
- Mutable default arguments
- Unhandled exceptions in critical paths
- Security credential exposure

### HIGH

Significant issues affecting reliability or maintainability:
- Missing type annotations on public APIs
- Poor error handling
- Resource leaks (unclosed files, connections)
- Global mutable state
- Missing critical tests

### MEDIUM

Best practice violations:
- PEP 8 style violations
- Google Style Guide violations
- Missing docstrings
- Inconsistent naming
- Complex comprehensions
- Magic numbers

### LOW

Minor improvements:
- Import ordering
- Whitespace issues
- Comment quality
- Code organization
- Additional type hints

### INFO

Suggestions for optimization:
- Performance improvements
- Alternative patterns
- Tooling recommendations

---

## Quick Reference Checklist

### Before Approving

- [ ] All tests pass
- [ ] No critical or high severity issues
- [ ] Error handling is complete
- [ ] Resources are properly managed (context managers)
- [ ] Public API is documented
- [ ] No security vulnerabilities
- [ ] Type hints on public functions
- [ ] Code passes linting (ruff, mypy)

### Common Issues to Watch

1. Mutable default arguments
2. Bare `except:` clauses
3. SQL/command injection vulnerabilities
4. Deep nesting instead of early returns
5. Missing context managers for resources
6. Global mutable state
7. Missing type annotations
8. Hardcoded secrets/credentials

---

## References

- [PEP 8 - Style Guide for Python Code](https://peps.python.org/pep-0008/)
- [Google Python Style Guide](https://google.github.io/styleguide/pyguide.html)
- [PEP 257 - Docstring Conventions](https://peps.python.org/pep-0257/)
- [PEP 484 - Type Hints](https://peps.python.org/pep-0484/)
- [PEP 20 - The Zen of Python](https://peps.python.org/pep-0020/)
- [Real Python - Code Quality](https://realpython.com/python-code-quality/)

---

*Last updated: 2026-01-18*
