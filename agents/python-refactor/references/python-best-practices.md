# Python Best Practices Reference

A comprehensive guide to idiomatic Python patterns, refactoring techniques, and best practices. This document serves as the knowledge base for the python-refactor pattern and can be referenced by other Python-related patterns.

## Table of Contents

1. [Core Principles](#core-principles)
2. [Code Structure Patterns](#code-structure-patterns)
3. [Error Handling](#error-handling)
4. [Data Structures](#data-structures)
5. [Function Design](#function-design)
6. [Class Design](#class-design)
7. [Performance Optimization](#performance-optimization)
8. [API Design](#api-design)
9. [Type Annotations](#type-annotations)
10. [Documentation Standards](#documentation-standards)
11. [When to Apply](#when-to-apply)

---

## Core Principles

### The Zen of Python (PEP 20)

Python's philosophy emphasizes clarity and simplicity:

- "Beautiful is better than ugly"
- "Explicit is better than implicit"
- "Simple is better than complex"
- "Complex is better than complicated"
- "Flat is better than nested"
- "Readability counts"
- "Errors should never pass silently"

### Preserve Functionality

Refactoring should never alter behavior:

- The refactored code must behave identically to the original
- Never change the public API unless explicitly requested
- Maintain backwards compatibility
- Keep changes focused - don't over-refactor

### Style Guide Hierarchy

1. **PEP 8** - Python's official style guide (baseline)
2. **Google Python Style Guide** - Additional recommendations for enterprise code
3. **Project-specific conventions** - Local team standards

---

## Code Structure Patterns

### 1. Reduce Nesting with Early Returns

Deep nesting reduces readability. Use guard clauses to handle edge cases first.

**Anti-pattern:**
```python
def get_user(user_id: int) -> User | None:
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
```

**Idiomatic:**
```python
def get_user(user_id: int) -> User | None:
    if user_id is None or user_id <= 0:
        return None

    user = fetch_user(user_id)
    if user is None:
        return None

    if not user.is_active:
        return None

    return user
```

**Why:** Early returns flatten the code structure, making the happy path clear and reducing cognitive load.

### 2. Minimize Variable Scope

Declare variables close to where they're used.

**Anti-pattern:**
```python
def process_data() -> str:
    data = None
    result = None
    error = None

    data = load_data()
    if data is None:
        return ""

    result = transform(data)
    if result is None:
        return ""

    return result
```

**Idiomatic:**
```python
def process_data() -> str:
    data = load_data()
    if data is None:
        return ""

    result = transform(data)
    if result is None:
        return ""

    return result
```

**Why:** Smaller scope reduces cognitive load and prevents accidental misuse of variables.

### 3. Replace Magic Numbers with Constants

Named constants make code self-documenting.

**Anti-pattern:**
```python
def validate_password(password: str) -> bool:
    if len(password) < 8:
        return False
    if len(password) > 128:
        return False
    return True
```

**Idiomatic:**
```python
MIN_PASSWORD_LENGTH = 8
MAX_PASSWORD_LENGTH = 128

def validate_password(password: str) -> bool:
    if len(password) < MIN_PASSWORD_LENGTH:
        return False
    if len(password) > MAX_PASSWORD_LENGTH:
        return False
    return True
```

**Why:** Named constants communicate intent and make updates easier.

### 4. Use Truthiness Appropriately

Python's truthiness rules make code more readable.

**Anti-pattern:**
```python
if len(items) > 0:
    process(items)

if value == None:
    value = default

if enabled == True:
    run()
```

**Idiomatic:**
```python
if items:
    process(items)

if value is None:
    value = default

if enabled:
    run()
```

**Why:** Pythonic code leverages truthiness for cleaner conditionals.

### 5. Use String Methods

Prefer built-in string methods over slicing.

**Anti-pattern:**
```python
if filename[-3:] == ".py":
    pass

if text[:5] == "http:":
    pass
```

**Idiomatic:**
```python
if filename.endswith(".py"):
    pass

if text.startswith("http:"):
    pass
```

**Why:** String methods are more readable and less error-prone.

---

## Error Handling

### 6. Use Specific Exceptions

Never use bare `except:` or catch `Exception` without re-raising.

**Anti-pattern:**
```python
try:
    data = fetch_data()
    process(data)
    save(data)
except:
    pass  # Silently ignores all errors
```

**Idiomatic:**
```python
try:
    data = fetch_data()
except ConnectionError as e:
    logger.error("Failed to fetch data: %s", e)
    raise
except ValueError as e:
    logger.warning("Invalid data format: %s", e)
    return None
```

**Why:** Catching specific exceptions allows proper error handling and prevents swallowing unexpected errors.

### 7. Use Context Managers for Resources

Context managers ensure proper cleanup.

**Anti-pattern:**
```python
def read_file(path: str) -> str:
    f = open(path)
    content = f.read()
    f.close()  # Can fail if read() raises
    return content
```

**Idiomatic:**
```python
def read_file(path: str) -> str:
    with open(path) as f:
        return f.read()
```

**Why:** Context managers ensure cleanup even if exceptions occur.

### 8. Chain Exceptions with `from`

Preserve the error chain when re-raising.

**Anti-pattern:**
```python
try:
    config = load_config(path)
except FileNotFoundError:
    raise ConfigurationError("Config file not found")  # Loses original traceback
```

**Idiomatic:**
```python
try:
    config = load_config(path)
except FileNotFoundError as e:
    raise ConfigurationError(f"Config file not found: {path}") from e
```

**Why:** Exception chaining preserves the original traceback for debugging.

### 9. Keep Try Blocks Minimal

Only wrap the code that can raise the expected exception.

**Anti-pattern:**
```python
try:
    data = fetch_data()
    processed = process_data(data)
    result = save_data(processed)
    notify_users(result)
except ConnectionError:
    handle_error()
```

**Idiomatic:**
```python
try:
    data = fetch_data()
except ConnectionError:
    handle_error()
    return

processed = process_data(data)
result = save_data(processed)
notify_users(result)
```

**Why:** Minimal try blocks make it clear which operation can fail.

---

## Data Structures

### 10. Use List Comprehensions

List comprehensions are more readable and faster than loops for simple transformations.

**Anti-pattern:**
```python
squares = []
for x in numbers:
    squares.append(x ** 2)
```

**Idiomatic:**
```python
squares = [x ** 2 for x in numbers]
```

**Why:** Comprehensions are more concise and often faster.

### 11. Avoid Complex Comprehensions

If a comprehension has multiple `for` clauses or complex filters, use explicit loops.

**Anti-pattern:**
```python
result = [
    transform(x, y)
    for outer in data
    for x in outer.items
    for y in outer.values
    if x.valid
    if y.active
]
```

**Idiomatic:**
```python
result = []
for outer in data:
    for x in outer.items:
        if not x.valid:
            continue
        for y in outer.values:
            if y.active:
                result.append(transform(x, y))
```

**Why:** Complex comprehensions are hard to read and debug.

### 12. Use Generators for Large Sequences

Generators save memory by yielding items one at a time.

**Anti-pattern:**
```python
def read_large_file(path: str) -> list[str]:
    with open(path) as f:
        return [line.strip() for line in f]  # Loads entire file
```

**Idiomatic:**
```python
def read_large_file(path: str) -> Iterator[str]:
    with open(path) as f:
        for line in f:
            yield line.strip()
```

**Why:** Generators use constant memory regardless of file size.

### 13. Avoid Mutable Default Arguments

Mutable defaults are shared across calls.

**Anti-pattern:**
```python
def append_to(item, target=[]):  # Bug!
    target.append(item)
    return target
```

**Idiomatic:**
```python
def append_to(item, target=None):
    if target is None:
        target = []
    target.append(item)
    return target
```

**Why:** Mutable defaults are evaluated once and shared, causing unexpected behavior.

### 14. Use Dictionary Methods

Leverage dict methods for cleaner code.

**Anti-pattern:**
```python
if key in d:
    value = d[key]
else:
    value = default

if key not in d:
    d[key] = []
d[key].append(item)
```

**Idiomatic:**
```python
value = d.get(key, default)

d.setdefault(key, []).append(item)
```

**Why:** Dictionary methods are more concise and avoid key lookup duplication.

### 15. Use enumerate() for Index and Value

Don't manually track indices.

**Anti-pattern:**
```python
for i in range(len(items)):
    item = items[i]
    process(i, item)
```

**Idiomatic:**
```python
for i, item in enumerate(items):
    process(i, item)
```

**Why:** enumerate() is cleaner and avoids index errors.

### 16. Use zip() for Parallel Iteration

Iterate over multiple sequences together.

**Anti-pattern:**
```python
for i in range(len(names)):
    print(f"{names[i]}: {scores[i]}")
```

**Idiomatic:**
```python
for name, score in zip(names, scores):
    print(f"{name}: {score}")
```

**Why:** zip() is cleaner and handles sequences of different lengths safely.

---

## Function Design

### 17. Use Type Hints

Type hints improve code clarity and enable static analysis.

**Anti-pattern:**
```python
def get_user(user_id):
    """Get a user by ID."""
    return db.find(user_id)
```

**Idiomatic:**
```python
def get_user(user_id: int) -> User | None:
    """Get a user by ID."""
    return db.find(user_id)
```

**Why:** Type hints document expected types and catch errors early.

### 18. Single Responsibility

Functions should do one thing well.

**Anti-pattern:**
```python
def process_user(user_id: int) -> None:
    user = fetch_user(user_id)
    validate_user(user)
    update_user(user)
    send_notification(user)
    log_activity(user)
```

**Idiomatic:**
```python
def process_user(user_id: int) -> None:
    user = fetch_user(user_id)
    if not is_valid_user(user):
        raise ValidationError("Invalid user")
    update_user(user)

def notify_user_update(user: User) -> None:
    send_notification(user)
    log_activity(user)
```

**Why:** Single responsibility makes functions easier to test and reuse.

### 19. Use Keyword-Only Arguments

Prevent positional argument confusion with keyword-only arguments.

**Anti-pattern:**
```python
def connect(host, port, timeout, use_ssl):
    pass

connect("localhost", 8080, 30, True)  # What is True?
```

**Idiomatic:**
```python
def connect(host: str, port: int, *, timeout: int = 30, use_ssl: bool = False):
    pass

connect("localhost", 8080, use_ssl=True)
```

**Why:** Keyword-only arguments make calls self-documenting.

### 20. Return Early, Return Often

Avoid storing results in variables just to return them.

**Anti-pattern:**
```python
def get_status(user: User) -> str:
    if user.is_admin:
        result = "admin"
    elif user.is_active:
        result = "active"
    else:
        result = "inactive"
    return result
```

**Idiomatic:**
```python
def get_status(user: User) -> str:
    if user.is_admin:
        return "admin"
    if user.is_active:
        return "active"
    return "inactive"
```

**Why:** Early returns reduce variable tracking and nesting.

---

## Class Design

### 21. Use Dataclasses for Data Containers

Dataclasses reduce boilerplate for simple classes.

**Anti-pattern:**
```python
class User:
    def __init__(self, id: int, name: str, email: str):
        self.id = id
        self.name = name
        self.email = email

    def __repr__(self):
        return f"User(id={self.id}, name={self.name}, email={self.email})"

    def __eq__(self, other):
        if not isinstance(other, User):
            return False
        return self.id == other.id and self.name == other.name
```

**Idiomatic:**
```python
from dataclasses import dataclass

@dataclass
class User:
    id: int
    name: str
    email: str
```

**Why:** Dataclasses auto-generate `__init__`, `__repr__`, `__eq__`, and more.

### 22. Use Properties for Computed Attributes

Properties provide a clean interface for computed values.

**Anti-pattern:**
```python
class Circle:
    def __init__(self, radius: float):
        self.radius = radius

    def get_area(self) -> float:
        return math.pi * self.radius ** 2
```

**Idiomatic:**
```python
class Circle:
    def __init__(self, radius: float):
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
        return math.pi * self._radius ** 2
```

**Why:** Properties allow validation and computed attributes with attribute syntax.

### 23. Use Protocols for Structural Typing

Protocols define interfaces without inheritance.

**Anti-pattern:**
```python
from abc import ABC, abstractmethod

class Readable(ABC):
    @abstractmethod
    def read(self, size: int = -1) -> bytes:
        pass

class MyFile(Readable):  # Must inherit
    def read(self, size: int = -1) -> bytes:
        pass
```

**Idiomatic:**
```python
from typing import Protocol

class Readable(Protocol):
    def read(self, size: int = -1) -> bytes: ...

class MyFile:  # No inheritance needed
    def read(self, size: int = -1) -> bytes:
        pass

def process(source: Readable) -> None:  # MyFile is compatible
    data = source.read()
```

**Why:** Protocols enable duck typing with type safety.

### 24. Use `__slots__` for Memory Efficiency

`__slots__` reduces memory usage for many instances.

**Anti-pattern:**
```python
class Point:
    def __init__(self, x: float, y: float):
        self.x = x
        self.y = y
```

**Idiomatic (for memory-critical cases):**
```python
class Point:
    __slots__ = ('x', 'y')

    def __init__(self, x: float, y: float):
        self.x = x
        self.y = y
```

**Why:** `__slots__` eliminates the instance `__dict__`, saving memory.

---

## Performance Optimization

### 25. Use f-strings for Formatting

f-strings are the fastest and most readable option.

**Anti-pattern:**
```python
message = "User %s logged in at %s" % (user.name, timestamp)
message = "User {} logged in at {}".format(user.name, timestamp)
```

**Idiomatic:**
```python
message = f"User {user.name} logged in at {timestamp}"
```

**Why:** f-strings are faster and more readable.

### 26. Use str.join() for Concatenation

String concatenation in loops creates many intermediate strings.

**Anti-pattern:**
```python
def build_report(items: list[str]) -> str:
    report = ""
    for item in items:
        report += f"- {item}\n"
    return report
```

**Idiomatic:**
```python
def build_report(items: list[str]) -> str:
    return "\n".join(f"- {item}" for item in items)
```

**Why:** `str.join()` is O(n) while `+=` is O(n^2).

### 27. Use any() and all()

Built-in functions are clearer and often faster.

**Anti-pattern:**
```python
def has_valid_item(items: list[Item]) -> bool:
    for item in items:
        if item.is_valid:
            return True
    return False
```

**Idiomatic:**
```python
def has_valid_item(items: list[Item]) -> bool:
    return any(item.is_valid for item in items)
```

**Why:** `any()` and `all()` are clearer and short-circuit.

### 28. Use collections.defaultdict

defaultdict eliminates key existence checks.

**Anti-pattern:**
```python
groups = {}
for item in items:
    if item.category not in groups:
        groups[item.category] = []
    groups[item.category].append(item)
```

**Idiomatic:**
```python
from collections import defaultdict

groups = defaultdict(list)
for item in items:
    groups[item.category].append(item)
```

**Why:** defaultdict automatically initializes missing keys.

### 29. Use collections.Counter

Counter simplifies counting.

**Anti-pattern:**
```python
counts = {}
for item in items:
    if item in counts:
        counts[item] += 1
    else:
        counts[item] = 1
```

**Idiomatic:**
```python
from collections import Counter

counts = Counter(items)
```

**Why:** Counter is purpose-built for counting.

---

## API Design

### 30. Use Decorators for Cross-Cutting Concerns

Decorators separate cross-cutting logic from business logic.

**Anti-pattern:**
```python
def get_user(user_id: int) -> User:
    start = time.perf_counter()
    try:
        return db.find(user_id)
    finally:
        elapsed = time.perf_counter() - start
        logger.info(f"get_user took {elapsed:.3f}s")
```

**Idiomatic:**
```python
import functools

def timed(func):
    @functools.wraps(func)
    def wrapper(*args, **kwargs):
        start = time.perf_counter()
        try:
            return func(*args, **kwargs)
        finally:
            elapsed = time.perf_counter() - start
            logger.info(f"{func.__name__} took {elapsed:.3f}s")
    return wrapper

@timed
def get_user(user_id: int) -> User:
    return db.find(user_id)
```

**Why:** Decorators enable reusable cross-cutting concerns.

### 31. Use contextlib for Custom Context Managers

contextlib simplifies context manager creation.

**Anti-pattern:**
```python
class Timer:
    def __init__(self, name: str):
        self.name = name

    def __enter__(self):
        self.start = time.perf_counter()
        return self

    def __exit__(self, *args):
        elapsed = time.perf_counter() - self.start
        logger.info(f"{self.name} took {elapsed:.3f}s")
```

**Idiomatic:**
```python
from contextlib import contextmanager

@contextmanager
def timer(name: str):
    start = time.perf_counter()
    try:
        yield
    finally:
        elapsed = time.perf_counter() - start
        logger.info(f"{name} took {elapsed:.3f}s")
```

**Why:** contextlib.contextmanager is more concise for simple cases.

### 32. Use functools.lru_cache for Memoization

Cache expensive function results.

**Anti-pattern:**
```python
_cache = {}

def expensive_computation(n: int) -> int:
    if n in _cache:
        return _cache[n]
    result = compute(n)
    _cache[n] = result
    return result
```

**Idiomatic:**
```python
from functools import lru_cache

@lru_cache(maxsize=128)
def expensive_computation(n: int) -> int:
    return compute(n)
```

**Why:** lru_cache handles caching, eviction, and thread safety.

---

## Type Annotations

### 33. Use Modern Union Syntax

Python 3.10+ supports `|` for unions.

**Anti-pattern:**
```python
from typing import Optional, Union

def get_user(user_id: int) -> Optional[User]:
    pass

def process(value: Union[str, int]) -> None:
    pass
```

**Idiomatic:**
```python
def get_user(user_id: int) -> User | None:
    pass

def process(value: str | int) -> None:
    pass
```

**Why:** The `|` syntax is cleaner and doesn't require imports.

### 34. Use TypeVar for Generics

TypeVar enables generic functions.

**Anti-pattern:**
```python
def first(items: list) -> Any:
    return items[0] if items else None
```

**Idiomatic:**
```python
from typing import TypeVar
from collections.abc import Sequence

T = TypeVar('T')

def first(items: Sequence[T]) -> T | None:
    return items[0] if items else None
```

**Why:** Generic functions preserve type information.

### 35. Use Type Aliases

Type aliases improve readability for complex types.

**Anti-pattern:**
```python
def process(
    handlers: dict[str, Callable[[dict[str, Any]], Awaitable[dict[str, Any]]]]
) -> None:
    pass
```

**Idiomatic:**
```python
from typing import TypeAlias

JsonDict: TypeAlias = dict[str, Any]
Handler: TypeAlias = Callable[[JsonDict], Awaitable[JsonDict]]

def process(handlers: dict[str, Handler]) -> None:
    pass
```

**Why:** Type aliases make complex signatures readable.

---

## Documentation Standards

### 36. Use Google-Style Docstrings

Google-style docstrings are clean and widely supported.

**Anti-pattern:**
```python
def get_user(user_id, include_inactive=False):
    """Get user by id. Returns None if not found."""
    pass
```

**Idiomatic:**
```python
def get_user(
    user_id: int,
    include_inactive: bool = False,
) -> User | None:
    """Retrieve a user from the database.

    Fetches user information by ID, optionally including
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
        >>> user = get_user(123)
        >>> print(user.name)
        'John Doe'
    """
    pass
```

**Why:** Structured docstrings enable documentation generation and IDE support.

### 37. Document Why, Not What

Comments should explain reasoning, not restate code.

**Anti-pattern:**
```python
# Increment counter by 1
counter += 1

# Check if user is active
if user.is_active:
    pass
```

**Idiomatic:**
```python
# Rate limit: max 100 requests per minute per user
counter += 1

# Only active users can access premium features
if user.is_active:
    pass
```

**Why:** Good comments explain the why, not the what.

---

## When to Apply

### High Impact Patterns

Apply these first - they have the greatest effect on code quality:

1. **Early returns** - Dramatically improves readability
2. **Context managers** - Prevents resource leaks
3. **Specific exceptions** - Enables proper error handling
4. **Mutable default fix** - Prevents subtle bugs

### Medium Impact Patterns

Apply when the codebase is stable:

1. **List comprehensions** - When transformation is simple
2. **Type hints** - On public APIs first
3. **Dataclasses** - For data container classes
4. **f-strings** - For all string formatting

### Apply Cautiously

These patterns have tradeoffs:

1. **Generators** - Only for large sequences or infinite streams
2. **`__slots__`** - Only for memory-critical applications
3. **lru_cache** - Only for pure functions with hashable arguments
4. **Protocols** - Only when duck typing is needed

---

## Modern Python Ecosystem (2025)

### Import Organization

Always organize imports in three groups separated by blank lines:

```python
# Standard library
import os
import sys
from typing import Any

# Third-party packages
import requests
from flask import Flask

# Local packages
from myproject.utils import helper
```

### Development Tools

| Tool | Purpose |
|------|---------|
| `ruff` | Fast linter and formatter (replaces flake8, isort, black) |
| `mypy` | Static type checking |
| `pytest` | Testing framework |
| `uv` | Fast package management |

### Common Libraries

| Category | Library | Notes |
|----------|---------|-------|
| HTTP | `httpx` | Modern async-capable HTTP client |
| CLI | `typer` | Type-hint based CLI framework |
| Data | `pydantic` | Data validation using type hints |
| Async | `asyncio` | Standard async framework |
| Web | `FastAPI` | Modern async web framework |

---

## Quick Reference Checklist

When refactoring Python code, check for:

- [ ] Deep nesting that can be flattened with early returns
- [ ] Mutable default arguments (`def foo(items=[])`)
- [ ] Bare `except:` clauses
- [ ] Resources not managed with context managers
- [ ] Magic numbers without named constants
- [ ] Manual index tracking instead of enumerate()
- [ ] String concatenation in loops instead of join()
- [ ] Missing type hints on public functions
- [ ] Complex comprehensions that should be loops
- [ ] Missing or inadequate docstrings
- [ ] Hardcoded credentials or secrets
- [ ] SQL string formatting (injection vulnerability)
- [ ] `shell=True` in subprocess calls

---

## References

- [PEP 8 - Style Guide for Python Code](https://peps.python.org/pep-0008/)
- [PEP 20 - The Zen of Python](https://peps.python.org/pep-0020/)
- [PEP 257 - Docstring Conventions](https://peps.python.org/pep-0257/)
- [PEP 484 - Type Hints](https://peps.python.org/pep-0484/)
- [Google Python Style Guide](https://google.github.io/styleguide/pyguide.html)

---

*Last updated: 2026-01-19*
