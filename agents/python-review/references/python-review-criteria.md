# Python Code Review Criteria

A comprehensive guide to reviewing Python code for quality, correctness, performance, and adherence to best practices. This document serves as the knowledge base for the python-review pattern and incorporates guidelines from PEP 8 and the Google Python Style Guide.

## Table of Contents

1. [Review Philosophy](#review-philosophy)
2. [Python 3.12-3.13 Features](#python-312-313-features)
3. [Code Formatting and Style](#code-formatting-and-style)
4. [Error and Exception Handling](#error-and-exception-handling)
5. [Type Annotations](#type-annotations)
6. [Structural Pattern Matching](#structural-pattern-matching)
7. [Data Structures](#data-structures)
8. [Dataclasses, Pydantic, and attrs](#dataclasses-pydantic-and-attrs)
9. [Function and Class Design](#function-and-class-design)
10. [Async/Await Patterns](#asyncawait-patterns)
11. [Code Structure](#code-structure)
12. [API Design Patterns](#api-design-patterns)
13. [Performance](#performance)
14. [Module Organization](#module-organization)
15. [Project Structure and Tooling](#project-structure-and-tooling)
16. [Documentation](#documentation)
17. [Security Considerations](#security-considerations)
18. [Severity Classification](#severity-classification)

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

## Python 3.12-3.13 Features

### Python 3.12 Key Features

| Feature                        | PEP     | Impact                                    |
| ------------------------------ | ------- | ----------------------------------------- |
| Relaxed f-strings              | PEP 701 | Nested quotes, multiline, comments        |
| Per-interpreter GIL            | PEP 684 | True multi-core with sub-interpreters     |
| `type` statement for aliases   | PEP 695 | Cleaner type alias syntax                 |
| Improved error messages        | —       | Better tracebacks with suggestions        |

**Relaxed f-strings (PEP 701):**

```python
# Python 3.12+ - nested quotes, multiline, comments allowed
name = "world"
message = f"Hello {
    name.upper()  # Can include comments now
}"

# Nested quotes without escaping
data = f"User: {user['name']}"  # Previously required: user[\"name\"]

# Multiline expressions
report = f"""
Total: {
    sum(
        item.price
        for item in items
    )
}
"""
```

**Type statement (PEP 695):**

```python
# Python 3.12+ - cleaner type alias syntax
type Vector = list[float]
type UserID = int
type Callback[T] = Callable[[T], None]

# Generic type aliases
type ListOrSet[T] = list[T] | set[T]
```

### Python 3.13 Key Features

| Feature                   | PEP     | Impact                            |
| ------------------------- | ------- | --------------------------------- |
| Free-threaded mode        | PEP 703 | Experimental no-GIL builds        |
| JIT compiler              | PEP 744 | Experimental performance boost    |
| Improved REPL             | —       | Multiline editing, colors         |
| `locals()` defined semantics | PEP 667 | Predictable mutation behavior  |
| Dead batteries removed    | —       | Legacy modules fully removed      |

**New REPL features:**

- Multiline editing with history preservation
- Direct support for `help`, `exit`, `quit` without parentheses
- Color prompts and tracebacks by default

**Removed modules (dead batteries):**

The following were deprecated in 3.11-3.12 and removed in 3.13:
`aifc`, `audioop`, `cgi`, `cgitb`, `chunk`, `crypt`, `imghdr`, `mailcap`,
`msilib`, `nis`, `nntplib`, `ossaudiodev`, `pipes`, `sndhdr`, `spwd`,
`sunau`, `telnetlib`, `uu`, `xdrlib`

---

## Code Formatting and Style

### PEP 8 Mandatory Checks

| Check                                         | Severity | Reference |
| --------------------------------------------- | -------- | --------- |
| 4 spaces per indentation level                | HIGH     | PEP 8     |
| Maximum 79 characters per line                | MEDIUM   | PEP 8     |
| Maximum 72 characters for docstrings/comments | LOW      | PEP 8     |
| Two blank lines around top-level definitions  | MEDIUM   | PEP 8     |
| One blank line between methods                | MEDIUM   | PEP 8     |
| No trailing whitespace                        | LOW      | PEP 8     |

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

| Type       | Convention                  | Example             |
| ---------- | --------------------------- | ------------------- |
| Modules    | `lowercase_underscore`      | `user_service.py`   |
| Packages   | `lowercase`                 | `mypackage`         |
| Classes    | `CapWords`                  | `UserService`       |
| Exceptions | `CapWords` + `Error` suffix | `ValidationError`   |
| Functions  | `lowercase_underscore`      | `get_user()`        |
| Methods    | `lowercase_underscore`      | `calculate_total()` |
| Constants  | `UPPER_CASE_UNDERSCORE`     | `MAX_RETRIES`       |
| Variables  | `lowercase_underscore`      | `user_count`        |
| Protected  | `_single_underscore`        | `_internal_value`   |
| Private    | `__double_underscore`       | `__name_mangled`    |

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

| Rule                              | Severity | Reference     |
| --------------------------------- | -------- | ------------- |
| Never use bare `except:`          | CRITICAL | PEP 8, Google |
| Catch specific exceptions         | HIGH     | Google Style  |
| Minimize code in try block        | HIGH     | Google Style  |
| Use `finally` for cleanup         | MEDIUM   | Google Style  |
| Use `raise X from Y` for chaining | MEDIUM   | PEP 8         |

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

### Exception Notes (Python 3.11+)

Add contextual information as exceptions propagate:

```python
def process_file(path: str) -> dict:
    try:
        return parse_config(path)
    except ValueError as e:
        e.add_note(f"While processing: {path}")
        e.add_note(f"Check file format and encoding")
        raise
```

### Exception Groups (Python 3.11+)

Handle multiple exceptions from concurrent operations:

```python
# Catching exception groups
try:
    async with asyncio.TaskGroup() as tg:
        tg.create_task(fetch_user(1))
        tg.create_task(fetch_user(2))
except* ConnectionError as eg:
    for exc in eg.exceptions:
        logger.error(f"Connection failed: {exc}")
except* ValueError as eg:
    for exc in eg.exceptions:
        logger.error(f"Invalid data: {exc}")
```

### Error Handling Strategy

**"Raise low, catch high":**

- Lower-level functions raise specific exceptions
- Catch and handle at application boundaries (CLI, web handlers, event loops)
- Add context as exceptions bubble up

```python
# Low level - raise specific error
def parse_user_id(raw: str) -> int:
    try:
        return int(raw)
    except ValueError as e:
        raise InvalidUserIDError(f"Invalid user ID: {raw!r}") from e

# High level - catch at boundary
def handle_request(request: Request) -> Response:
    try:
        user_id = parse_user_id(request.params["id"])
        user = fetch_user(user_id)
        return Response.ok(user)
    except InvalidUserIDError as e:
        return Response.bad_request(str(e))
    except UserNotFoundError:
        return Response.not_found("User not found")
```

---

## Type Annotations

### Guidelines (Google Style, PEP 484)

| Check                                     | Severity | Rationale                 |
| ----------------------------------------- | -------- | ------------------------- |
| Annotate public APIs                      | HIGH     | Documentation and tooling |
| Use `X \| None` not `Optional[X]` (3.10+) | LOW      | Modern syntax             |
| Annotate complex functions                | MEDIUM   | Clarity                   |
| Import types correctly                    | MEDIUM   | Avoid runtime overhead    |

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

### Modern Type Hints (Python 3.9-3.12)

**Use built-in generics, not typing aliases:**

```python
# Good (Python 3.9+)
def process(items: list[str], config: dict[str, int]) -> tuple[str, int]:
    pass

# Avoid (legacy)
from typing import List, Dict, Tuple
def process(items: List[str], config: Dict[str, int]) -> Tuple[str, int]:
    pass
```

**Use union syntax, not Union/Optional:**

```python
# Good (Python 3.10+)
def fetch(id: int | str) -> User | None:
    pass

# Avoid (legacy)
from typing import Union, Optional
def fetch(id: Union[int, str]) -> Optional[User]:
    pass
```

**Type aliases with `type` statement (Python 3.12+):**

```python
# Good (Python 3.12+)
type UserID = int
type UserMapping = dict[UserID, User]

# Alternative for older Python
from typing import TypeAlias
UserID: TypeAlias = int
```

---

## Structural Pattern Matching

### Overview (PEP 634-636, Python 3.10+)

Structural pattern matching allows matching on the **structure** of data, not just values.

### Basic Patterns

```python
def handle_command(command: list[str]) -> str:
    match command:
        case ["quit"]:
            return "Goodbye"
        case ["hello", name]:
            return f"Hello, {name}!"
        case ["add", *numbers]:
            return str(sum(int(n) for n in numbers))
        case _:
            return "Unknown command"
```

### Object Patterns

```python
from dataclasses import dataclass

@dataclass
class Point:
    x: float
    y: float

def describe_point(point: Point) -> str:
    match point:
        case Point(x=0, y=0):
            return "Origin"
        case Point(x=0, y=y):
            return f"On Y-axis at {y}"
        case Point(x=x, y=0):
            return f"On X-axis at {x}"
        case Point(x=x, y=y) if x == y:
            return f"On diagonal at {x}"
        case _:
            return f"Point at ({point.x}, {point.y})"
```

### Guard Clauses

```python
def classify_number(n: int) -> str:
    match n:
        case x if x < 0:
            return "negative"
        case 0:
            return "zero"
        case x if x % 2 == 0:
            return "positive even"
        case _:
            return "positive odd"
```

### Best Practices

| Practice                        | Severity | Rationale                              |
| ------------------------------- | -------- | -------------------------------------- |
| Always include `case _:` default | HIGH    | Handles unexpected input               |
| Order specific before general   | HIGH     | First match wins                       |
| Use guards for conditions       | MEDIUM   | Express relationships between captures |
| Validate external data at edges | MEDIUM   | Match on typed objects internally      |

**When to use match vs if/elif:**

- **Use match**: Destructuring, multiple patterns, type-based dispatch
- **Use if/elif**: Simple boolean conditions, range checks

**Avoid:**

```python
# Bad - using match for simple conditions
match x:
    case x if x > 0:
        return "positive"
    case _:
        return "non-positive"

# Better - use if/else
if x > 0:
    return "positive"
return "non-positive"
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

## Dataclasses, Pydantic, and attrs

### When to Use Each

| Tool        | Use Case                                    | Key Strength                |
| ----------- | ------------------------------------------- | --------------------------- |
| `dataclass` | Simple internal data grouping               | Built-in, no dependencies   |
| `attrs`     | Performance + custom validation             | Fine-grained control, fast  |
| `Pydantic`  | API validation, serialization, settings     | Comprehensive validation    |

### Dataclasses (stdlib)

**Best for:** Internal data transfer objects, configuration, simple records.

```python
from dataclasses import dataclass, field

# Modern dataclass with slots (Python 3.10+)
@dataclass(slots=True, frozen=True)
class Point:
    x: float
    y: float

# With defaults and factory
@dataclass
class Config:
    name: str
    tags: list[str] = field(default_factory=list)
    timeout: int = 30

    def __post_init__(self) -> None:
        if self.timeout < 0:
            raise ValueError("timeout must be non-negative")
```

**Key options:**

| Option     | Effect                                |
| ---------- | ------------------------------------- |
| `slots`    | Memory efficient, faster attr access  |
| `frozen`   | Immutable (hashable)                  |
| `kw_only`  | All fields keyword-only (3.10+)       |
| `order`    | Generate comparison methods           |

### attrs

**Best for:** Performance-critical code, complex validation without JSON serialization.

```python
import attrs

@attrs.define
class User:
    name: str = attrs.field(validator=attrs.validators.instance_of(str))
    age: int = attrs.field(validator=[
        attrs.validators.instance_of(int),
        attrs.validators.ge(0),
    ])
    email: str = attrs.field()

    @email.validator
    def _validate_email(self, attribute, value):
        if "@" not in value:
            raise ValueError(f"Invalid email: {value}")
```

### Pydantic

**Best for:** API request/response models, settings management, JSON serialization.

```python
from pydantic import BaseModel, Field, field_validator

class UserCreate(BaseModel):
    name: str = Field(..., min_length=1, max_length=100)
    email: str
    age: int = Field(..., ge=0, le=150)

    @field_validator("email")
    @classmethod
    def validate_email(cls, v: str) -> str:
        if "@" not in v:
            raise ValueError("Invalid email format")
        return v.lower()

    model_config = {"str_strip_whitespace": True}
```

### Performance Comparison

Performance differences are minimal for most use cases. Choose based on features needed:

- **Fastest:** `attrs` or `dataclass` without validation
- **Most features:** Pydantic (validation, serialization, settings)
- **No dependencies:** `dataclass`

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

## Async/Await Patterns

### Core Concepts

Python's `asyncio` enables concurrent I/O-bound operations without threads.

### Best Practices

| Practice                              | Severity | Rationale                           |
| ------------------------------------- | -------- | ----------------------------------- |
| Use `asyncio.run()` as entry point    | HIGH     | Proper event loop management        |
| Use `TaskGroup` for concurrent tasks  | HIGH     | Structured concurrency (3.11+)      |
| Use `async with` for resources        | HIGH     | Proper async cleanup                |
| Don't block the event loop            | CRITICAL | Defeats purpose of async            |

### Entry Point

```python
import asyncio

async def main() -> None:
    result = await fetch_data()
    await process_data(result)

# Always use asyncio.run() to start
if __name__ == "__main__":
    asyncio.run(main())
```

### TaskGroup (Python 3.11+, Recommended)

**Good - structured concurrency:**

```python
async def fetch_all_users(user_ids: list[int]) -> list[User]:
    async with asyncio.TaskGroup() as tg:
        tasks = [tg.create_task(fetch_user(uid)) for uid in user_ids]
    return [task.result() for task in tasks]
```

TaskGroup advantages:

- Automatic task lifecycle management
- Proper exception handling (cancels remaining on failure)
- Cleaner than `asyncio.gather()`

**Legacy approach (avoid in new code):**

```python
# Less safe - exceptions can leave tasks running
results = await asyncio.gather(
    fetch_user(1),
    fetch_user(2),
    return_exceptions=True,
)
```

### Async Context Managers

```python
import aiofiles

async def read_config(path: str) -> dict:
    async with aiofiles.open(path) as f:
        content = await f.read()
    return json.loads(content)
```

### Async Generators

```python
async def fetch_pages(url: str) -> AsyncIterator[dict]:
    """Fetch paginated data lazily."""
    page = 1
    while True:
        data = await fetch_page(url, page)
        if not data["items"]:
            break
        for item in data["items"]:
            yield item
        page += 1

# Usage
async for item in fetch_pages(api_url):
    process(item)
```

### CPU-Bound Work

**Never block the event loop with CPU-bound operations:**

```python
import asyncio
from concurrent.futures import ProcessPoolExecutor

async def process_image(image_path: str) -> bytes:
    loop = asyncio.get_running_loop()
    # Offload CPU-bound work to process pool
    with ProcessPoolExecutor() as pool:
        result = await loop.run_in_executor(
            pool, cpu_intensive_transform, image_path
        )
    return result
```

### Exception Groups (Python 3.11+)

Handle multiple concurrent failures:

```python
async def fetch_all(urls: list[str]) -> list[Response]:
    try:
        async with asyncio.TaskGroup() as tg:
            tasks = [tg.create_task(fetch(url)) for url in urls]
        return [t.result() for t in tasks]
    except* ConnectionError as eg:
        # Handle all ConnectionErrors from the group
        for exc in eg.exceptions:
            logger.error(f"Connection failed: {exc}")
        raise
    except* TimeoutError as eg:
        logger.warning(f"{len(eg.exceptions)} requests timed out")
        raise
```

### Common Anti-Patterns

```python
# Bad - blocking call in async function
async def bad_fetch(url: str) -> str:
    return requests.get(url).text  # Blocks event loop!

# Good - use async HTTP client
async def good_fetch(url: str) -> str:
    async with aiohttp.ClientSession() as session:
        async with session.get(url) as response:
            return await response.text()

# Bad - fire-and-forget tasks
async def bad_background():
    asyncio.create_task(some_task())  # No await, no tracking!

# Good - track background tasks
async def good_background():
    task = asyncio.create_task(some_task())
    try:
        await do_main_work()
    finally:
        await task  # Ensure task completes
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

| Pattern        | Performance | Use Case                         |
| -------------- | ----------- | -------------------------------- |
| f-strings      | Fastest     | Simple formatting                |
| `str.join()`   | Fast        | Multiple concatenations          |
| `%` formatting | Medium      | Logging with deferred evaluation |
| `+` in loop    | Slow        | Avoid                            |

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

| Check                            | Severity | Rationale          |
| -------------------------------- | -------- | ------------------ |
| Avoid mutable global state       | HIGH     | Testing difficulty |
| Constants are acceptable         | LOW      | Immutable values   |
| Prefix internal globals with `_` | MEDIUM   | Clear intent       |

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

## Project Structure and Tooling

### Modern Project Structure (2026)

```
my-project/
├── pyproject.toml          # Single source of truth
├── uv.lock                  # Lockfile (auto-generated by uv)
├── src/
│   └── mypackage/
│       ├── __init__.py
│       ├── core.py
│       └── utils.py
├── tests/
│   ├── conftest.py
│   └── test_core.py
└── README.md
```

### pyproject.toml (PEP 621)

```toml
[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[project]
name = "mypackage"
version = "1.0.0"
description = "My awesome package"
readme = "README.md"
requires-python = ">=3.12"
dependencies = [
    "httpx>=0.27",
    "pydantic>=2.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=8.0",
    "pytest-cov>=4.0",
    "pytest-asyncio>=0.23",
    "mypy>=1.8",
    "ruff>=0.3",
]

[tool.ruff]
line-length = 88
target-version = "py312"

[tool.ruff.lint]
select = ["E", "F", "I", "UP", "B", "SIM", "ASYNC"]

[tool.mypy]
python_version = "3.12"
strict = true

[tool.pytest.ini_options]
asyncio_mode = "auto"
testpaths = ["tests"]
```

### uv - Modern Package Manager (2024+)

**uv** is a Rust-based package manager that's 10-100x faster than pip:

```bash
# Create new project
uv init my-project
cd my-project

# Add dependencies
uv add httpx pydantic
uv add --dev pytest ruff mypy

# Run commands in virtual environment
uv run pytest
uv run python -m mypackage

# Sync dependencies from lockfile
uv sync
```

**Key benefits:**

- 10-100x faster than pip
- Automatic virtual environment management
- Cross-platform lockfile (`uv.lock`)
- Drop-in pip replacement

### Ruff - Linter and Formatter (2024+)

**Ruff replaces:** Flake8, Black, isort, pyupgrade, autoflake, pydocstyle

```bash
# Check for issues
ruff check .

# Auto-fix issues
ruff check --fix .

# Format code (Black-compatible)
ruff format .
```

**Key rule categories:**

| Code | Category         | Examples                           |
| ---- | ---------------- | ---------------------------------- |
| E/W  | pycodestyle      | Whitespace, line length            |
| F    | Pyflakes         | Unused imports, undefined names    |
| I    | isort            | Import sorting                     |
| UP   | pyupgrade        | Modern Python syntax               |
| B    | flake8-bugbear   | Common bugs                        |
| SIM  | flake8-simplify  | Code simplification                |
| ASYNC| flake8-async     | Async anti-patterns                |

### Type Checking with mypy

```bash
# Run type checker
mypy src/

# With strict mode (recommended)
mypy --strict src/
```

**Common mypy configuration:**

```toml
[tool.mypy]
python_version = "3.12"
strict = true
warn_return_any = true
warn_unused_configs = true

[[tool.mypy.overrides]]
module = "tests.*"
disallow_untyped_defs = false
```

### Pre-commit Hooks

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/astral-sh/ruff-pre-commit
    rev: v0.3.0
    hooks:
      - id: ruff
        args: [--fix]
      - id: ruff-format

  - repo: https://github.com/pre-commit/mirrors-mypy
    rev: v1.8.0
    hooks:
      - id: mypy
        additional_dependencies: [pydantic]
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

| Rule                                             | Severity |
| ------------------------------------------------ | -------- |
| Document "why", not "what"                       | MEDIUM   |
| Keep comments updated                            | HIGH     |
| No commented-out code                            | LOW      |
| Use TODO format: `# TODO(username): description` | LOW      |

---

## Security Considerations

### Critical Checks

| Check                  | Severity | Impact              |
| ---------------------- | -------- | ------------------- |
| Input validation       | CRITICAL | Injection attacks   |
| SQL parameterization   | CRITICAL | SQL injection       |
| Subprocess shell=False | CRITICAL | Command injection   |
| Secret management      | CRITICAL | Credential exposure |
| HTTPS verification     | HIGH     | MITM attacks        |

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

## Severity Classification

### CRITICAL

Issues that affect correctness, security, or cause crashes:

- SQL injection vulnerabilities
- Command injection vulnerabilities (`shell=True`, `os.system`)
- Bare `except:` clauses (catches `SystemExit`, `KeyboardInterrupt`)
- Mutable default arguments
- Unhandled exceptions in critical paths
- Security credential exposure
- Blocking calls in async event loop
- Path traversal vulnerabilities

### HIGH

Significant issues affecting reliability or maintainability:

- Missing type annotations on public APIs
- Poor error handling (swallowing exceptions, no context)
- Resource leaks (unclosed files, connections, sessions)
- Global mutable state
- Missing critical tests
- Fire-and-forget async tasks
- Missing `case _:` default in match statements
- HTTPS verification disabled

### MEDIUM

Best practice violations:

- PEP 8 style violations
- Google Style Guide violations
- Missing docstrings
- Inconsistent naming
- Complex comprehensions (3+ nested)
- Magic numbers
- Legacy type syntax (`List`, `Optional`, `Union`)
- Using `asyncio.gather()` instead of `TaskGroup` (3.11+)

### LOW

Minor improvements:

- Import ordering
- Whitespace issues
- Comment quality
- Code organization
- Additional type hints on internal functions
- Could use newer Python features

### INFO

Suggestions for optimization:

- Performance improvements
- Alternative patterns (dataclass vs attrs vs Pydantic)
- Tooling recommendations (ruff, uv, mypy)
- Modern Python feature adoption

---

## Quick Reference Checklist

### Before Approving

- [ ] All tests pass (including async tests)
- [ ] No critical or high severity issues
- [ ] Error handling is complete and specific
- [ ] Resources are properly managed (context managers)
- [ ] Public API is documented (Google-style docstrings)
- [ ] No security vulnerabilities
- [ ] Type hints on public functions (modern syntax)
- [ ] Code passes linting (`ruff check`, `ruff format`, `mypy --strict`)
- [ ] Uses modern Python features where appropriate (3.12+)

### Common Issues to Watch

1. Mutable default arguments (`def f(items=[]): ...`)
2. Bare `except:` clauses (catches `SystemExit`, `KeyboardInterrupt`)
3. SQL/command injection vulnerabilities (`shell=True`, f-string SQL)
4. Deep nesting instead of early returns
5. Missing context managers for resources
6. Global mutable state
7. Missing or legacy type annotations (`List` instead of `list`)
8. Hardcoded secrets/credentials
9. Blocking calls in async functions (defeats concurrency)
10. Fire-and-forget tasks without tracking
11. Using `Optional[X]` instead of `X | None` (Python 3.10+)
12. Missing `case _:` default in match statements
13. String concatenation in hot loops (use `"".join()`)
14. Over-complex comprehensions (3+ nested for/if)

---

## Anti-Patterns to Avoid

### Critical Anti-Patterns

| Anti-Pattern                    | Severity | Fix                                      |
| ------------------------------- | -------- | ---------------------------------------- |
| Mutable default arguments       | CRITICAL | Use `None` with initialization           |
| Bare `except:`                  | CRITICAL | Catch specific exceptions                |
| `from module import *`          | HIGH     | Use explicit imports                     |
| Inconsistent return types       | HIGH     | Single return type + exceptions          |
| `try/except` as control flow    | MEDIUM   | Use proper conditionals                  |
| String `+` in loops             | MEDIUM   | Use `"".join()` or f-strings             |
| 3+ nested comprehensions        | MEDIUM   | Use explicit loops                       |

### Examples

```python
# Bad - mutable default
def add_item(item, items=[]):  # Bug: shared list!
    items.append(item)
    return items

# Good
def add_item(item, items=None):
    if items is None:
        items = []
    items.append(item)
    return items

# Bad - bare except
try:
    risky_operation()
except:  # Catches SystemExit, KeyboardInterrupt!
    pass

# Good
try:
    risky_operation()
except SpecificError as e:
    logger.error("Operation failed: %s", e)
    raise

# Bad - inconsistent returns
def find_user(user_id):
    if user_id < 0:
        return "Invalid ID"  # str
    user = db.find(user_id)
    if user:
        return user  # User object
    return None  # None

# Good - consistent return type
def find_user(user_id: int) -> User | None:
    if user_id < 0:
        raise ValueError(f"Invalid user ID: {user_id}")
    return db.find(user_id)
```

---

## References

### Official Documentation

- [PEP 8 - Style Guide for Python Code](https://peps.python.org/pep-0008/)
- [PEP 20 - The Zen of Python](https://peps.python.org/pep-0020/)
- [PEP 257 - Docstring Conventions](https://peps.python.org/pep-0257/)
- [PEP 484 - Type Hints](https://peps.python.org/pep-0484/)
- [PEP 636 - Structural Pattern Matching Tutorial](https://peps.python.org/pep-0636/)
- [PEP 695 - Type Parameter Syntax](https://peps.python.org/pep-0695/)
- [What's New in Python 3.12](https://docs.python.org/3/whatsnew/3.12.html)
- [What's New in Python 3.13](https://docs.python.org/3/whatsnew/3.13.html)
- [Typing Best Practices](https://typing.python.org/en/latest/reference/best_practices.html)
- [asyncio Documentation](https://docs.python.org/3/library/asyncio.html)

### Style Guides

- [Google Python Style Guide](https://google.github.io/styleguide/pyguide.html)
- [The Hitchhiker's Guide to Python](https://docs.python-guide.org/)

### Tooling

- [Ruff Documentation](https://docs.astral.sh/ruff/)
- [uv Documentation](https://docs.astral.sh/uv/)
- [mypy Documentation](https://mypy.readthedocs.io/)
- [pytest Documentation](https://docs.pytest.org/)

### Additional Resources

- [Real Python - Code Quality](https://realpython.com/python-code-quality/)
- [Real Python - Pytest Testing](https://realpython.com/pytest-python-testing/)
- [Python Anti-Patterns](https://docs.quantifiedcode.com/python-anti-patterns/)
- [The State of Python Packaging in 2026](https://learn.repoforge.io/posts/the-state-of-python-packaging-in-2026/)

---

_Last updated: 2026-02-04_
