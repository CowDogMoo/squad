# Python Testing Patterns with pytest

This document serves as the knowledge base for Python testing conventions and patterns using pytest.

## Testing Philosophy

### Core Principles

1. **Simple and readable**: Tests should be easy to understand at a glance
2. **Test behavior, not implementation**: Focus on what the code does, not how
3. **Avoid over-mocking**: Only mock external dependencies
4. **Arrange-Act-Assert**: Structure tests clearly with setup, execution, verification
5. **One concept per test**: Each test should verify one thing
6. **DRY applies to test setup**: If stub/mock code appears in 2+ test files, extract to `conftest.py`
7. **Self-documenting tests**: Each test function gets a one-line docstring stating what behavior it verifies

### F.I.R.S.T. Principles

- **Fast**: Tests should run quickly for frequent execution
- **Independent**: No dependencies between tests; run in any order
- **Repeatable**: Same results every time (deterministic)
- **Self-validating**: Clear pass/fail outcomes
- **Timely**: Written close to the code they test

### Test Pyramid

Aim for a balanced test distribution:

- **~50% Unit tests**: Fast, isolated, test single functions/classes
- **~30% Integration tests**: Test component interactions
- **~20% End-to-end tests**: Test full user workflows

### Testing Tools (2026)

| Tool             | Use Case                              |
| ---------------- | ------------------------------------- |
| `pytest`         | Primary test framework (standard)     |
| `pytest-cov`     | Coverage reporting                    |
| `pytest-asyncio` | Async test support                    |
| `pytest-xdist`   | Parallel test execution (`-n auto`)   |
| `pytest-mock`    | Mocking support (`mocker` fixture)    |
| `hypothesis`     | Property-based testing                |
| `syrupy`         | Snapshot testing (external files)     |
| `inline-snapshot`| Snapshot testing (inline values)      |
| `pytest-timeout` | Fail tests that hang                  |
| `pytest-randomly`| Randomize test order                  |

## Parametrized Tests

### Basic Structure

```python
@pytest.mark.parametrize("input_val,expected", [
    (1, 2),
    (2, 4),
    (0, 0),
    (-1, -2),
])
def test_double(input_val, expected):
    """Test double returns correct value."""
    assert double(input_val) == expected
```

### Multiple Parameters

```python
@pytest.mark.parametrize("a,b,expected", [
    (2, 3, 5),
    (0, 0, 0),
    (-1, 1, 0),
])
def test_add(a, b, expected):
    """Test add returns correct sum."""
    assert add(a, b) == expected
```

### With IDs for Clarity

```python
@pytest.mark.parametrize("input_val,expected", [
    pytest.param(1, 2, id="positive"),
    pytest.param(0, 0, id="zero"),
    pytest.param(-1, -2, id="negative"),
])
def test_double_with_ids(input_val, expected):
    """Test double with descriptive test IDs."""
    assert double(input_val) == expected
```

### With Marks on Individual Cases

```python
@pytest.mark.parametrize("val,expected", [
    pytest.param(1, 2, id="normal"),
    pytest.param(None, 0, marks=pytest.mark.xfail, id="none-input"),
    pytest.param(-1, -2, marks=pytest.mark.slow, id="negative"),
])
def test_process(val, expected):
    """Test with markers on specific cases."""
    assert process(val) == expected
```

### Stacking Decorators for Combinations

```python
@pytest.mark.parametrize("x", [1, 2])
@pytest.mark.parametrize("y", ["a", "b"])
def test_combinations(x, y):
    """Runs 4 times: (1,a), (1,b), (2,a), (2,b)."""
    result = combine(x, y)
    assert result is not None
```

### When to Use Parametrized Tests

**Good for:**

- Functions with multiple valid inputs and outputs
- Validation logic with many cases
- Mathematical operations
- String transformations
- Boundary testing

**When not needed:**

- Single test case with unique setup
- Tests requiring different assertions
- Complex integration scenarios

## Fixtures

### Basic Fixture

```python
@pytest.fixture
def calculator():
    """Create a Calculator instance for testing."""
    return Calculator(precision=2)


def test_add(calculator):
    """Test calculator add method."""
    assert calculator.add(2, 3) == 5
```

### Fixture with Cleanup

```python
@pytest.fixture
def temp_file(tmp_path):
    """Create a temporary file for testing."""
    file_path = tmp_path / "test.txt"
    file_path.write_text("test content")
    yield file_path
    # Cleanup happens automatically with tmp_path
```

### Fixture with Parameters

```python
@pytest.fixture(params=[2, 4, 8])
def precision(request):
    """Provide different precision values."""
    return request.param


def test_calculator_precision(precision):
    """Test calculator with different precisions."""
    calc = Calculator(precision=precision)
    result = calc.divide(1, 3)
    assert len(str(result).split(".")[-1]) <= precision
```

### Class-scoped Fixture

```python
@pytest.fixture(scope="class")
def database_connection():
    """Create a database connection shared across test class."""
    conn = create_connection()
    yield conn
    conn.close()
```

### When to Use Fixtures

**Good for:**

- Object creation used by multiple tests
- Resource setup and teardown
- Sharing state within a scope
- Parameterized setup

**When not needed:**

- Simple inline setup
- One-off test data
- When it obscures test clarity

## Exception Testing

### Basic Exception Test

```python
def test_divide_by_zero_raises_error():
    """Test divide raises ValueError when dividing by zero."""
    calc = Calculator()
    with pytest.raises(ValueError):
        calc.divide(10, 0)
```

### With Message Matching

```python
def test_divide_by_zero_error_message():
    """Test divide raises ValueError with correct message."""
    calc = Calculator()
    with pytest.raises(ValueError, match="cannot divide by zero"):
        calc.divide(10, 0)
```

### Checking Exception Attributes

```python
def test_custom_exception_attributes():
    """Test custom exception has correct attributes."""
    with pytest.raises(ValidationError) as exc_info:
        validate_data(invalid_data)

    assert exc_info.value.field == "email"
    assert exc_info.value.code == "invalid_format"
```

### Parametrized Exception Tests

```python
@pytest.mark.parametrize("input_val,error_match", [
    (-1, "must be non-negative"),
    (None, "cannot be None"),
    ("abc", "must be numeric"),
])
def test_invalid_inputs_raise_errors(input_val, error_match):
    """Test invalid inputs raise appropriate errors."""
    with pytest.raises(ValueError, match=error_match):
        process(input_val)
```

## Test Classes

### Basic Test Class

```python
class TestCalculator:
    """Tests for Calculator class."""

    @pytest.fixture
    def calc(self):
        """Create a Calculator instance."""
        return Calculator()

    def test_add(self, calc):
        """Test add returns correct sum."""
        assert calc.add(2, 3) == 5

    def test_subtract(self, calc):
        """Test subtract returns correct difference."""
        assert calc.subtract(5, 3) == 2
```

### Nested Test Classes

```python
class TestCalculator:
    """Tests for Calculator class."""

    class TestArithmetic:
        """Tests for arithmetic operations."""

        def test_add(self):
            calc = Calculator()
            assert calc.add(2, 3) == 5

    class TestAdvanced:
        """Tests for advanced operations."""

        def test_power(self):
            calc = Calculator()
            assert calc.power(2, 3) == 8
```

## Mocking

### Basic Mock

```python
from unittest.mock import Mock, patch


def test_send_email(mocker):
    """Test send_email calls smtp client correctly."""
    mock_smtp = mocker.patch("module.smtp_client")

    send_email("test@example.com", "Hello")

    mock_smtp.send.assert_called_once_with(
        to="test@example.com",
        body="Hello"
    )
```

### Mock Return Value

```python
def test_get_user_from_api(mocker):
    """Test get_user handles API response correctly."""
    mock_response = {"id": 1, "name": "Alice"}
    mocker.patch("module.api_client.get", return_value=mock_response)

    user = get_user(1)

    assert user.name == "Alice"
```

### Mock Side Effects

```python
def test_retry_on_failure(mocker):
    """Test function retries on transient failures."""
    mock_call = mocker.patch("module.external_call")
    mock_call.side_effect = [ConnectionError(), ConnectionError(), "success"]

    result = retry_call()

    assert result == "success"
    assert mock_call.call_count == 3
```

### Using autospec for Safety

Always use `autospec=True` to ensure mocks respect actual method signatures:

```python
def test_with_autospec(mocker):
    """Test with autospec to catch signature mismatches."""
    # Without autospec: mock accepts ANY arguments (dangerous)
    # With autospec: mock validates method signatures exist and match
    mock_client = mocker.patch("module.Client", autospec=True)
    mock_client.return_value.fetch.return_value = {"data": 1}

    result = get_data()

    # This would fail if fetch() doesn't exist on real Client
    mock_client.return_value.fetch.assert_called_once_with(id=123)
```

### AsyncMock for Async Functions

Use `AsyncMock` (Python 3.8+) for mocking async functions:

```python
from unittest.mock import AsyncMock


def test_async_dependency(mocker):
    """Test mocking an async function."""
    mock_fetch = mocker.patch("module.fetch_data", new_callable=AsyncMock)
    mock_fetch.return_value = {"status": "ok"}

    # The mock can now be awaited
    result = await process_request()

    assert result["status"] == "ok"
    mock_fetch.assert_awaited_once()


@pytest.mark.asyncio
async def test_async_side_effect(mocker):
    """Test async mock with side effects."""
    mock_api = mocker.patch("module.api_call", new_callable=AsyncMock)
    mock_api.side_effect = [TimeoutError(), {"data": 1}]

    result = await retry_api_call()

    assert result == {"data": 1}
    assert mock_api.await_count == 2
```

### When to Mock

**Good for:**

- External API calls
- Database operations
- File system operations
- Time-dependent code
- Random number generation
- Environment variables

**When NOT to mock:**

- Simple data classes
- Standard library functions (usually)
- Internal implementation details
- Code you're testing

### Import-time Stubbing (sys.modules)

When a module imports an unavailable package, stub the package BEFORE importing
the module under test.

**CRITICAL: For pytest, stubs MUST be at MODULE LEVEL in conftest.py, NOT inside
fixtures.** pytest imports conftest.py BEFORE collecting test files. If stubs
are inside fixtures, test file imports fail with `ModuleNotFoundError`.

**CORRECT pattern — stubs at module level in conftest.py:**

```python
# tests/conftest.py
import sys
import types

# ========== MODULE-LEVEL STUBS — MUST BE AT TOP ==========
# Applied when conftest.py is imported (before test collection)
# Add stubs for any unavailable packages the source code imports

external_pkg = types.ModuleType("external_package")
external_pkg.SomeClass = type("SomeClass", (), {})
external_pkg.decorator = lambda *a, **k: lambda x: x
sys.modules["external_package"] = external_pkg

# For nested modules: parent.child
parent = types.ModuleType("parent")
child = types.ModuleType("parent.child")
child.func = lambda *a, **k: None
parent.child = child
sys.modules["parent"] = parent
sys.modules["parent.child"] = child

# ========== END MODULE-LEVEL STUBS ==========

import pytest

# Fixtures go AFTER the stubs
@pytest.fixture
def sample_data():
    return {"key": "value"}
```

**WRONG pattern — stubs inside fixtures (causes collection failures):**

```python
# tests/conftest.py — WRONG! Don't do this.
import pytest

@pytest.fixture
def api_module(monkeypatch):
    # TOO LATE! Test files already failed to import
    import sys, types
    pkg = types.ModuleType("external_package")
    monkeypatch.setitem(sys.modules, "external_package", pkg)
    from myapp import api  # This line never runs
    return api
```

**Key insight:** Importing an unfamiliar package is NOT a reason to skip testing.
Stub the import at module level in conftest.py and test the business logic.

## Async Testing

### Basic Async Test

```python
import pytest


@pytest.mark.asyncio
async def test_async_fetch():
    """Test async fetch returns data."""
    result = await fetch_data("https://api.example.com")
    assert result["status"] == "ok"
```

### Async Fixture

```python
@pytest.fixture
async def async_client():
    """Create an async HTTP client."""
    client = AsyncClient()
    yield client
    await client.close()


@pytest.mark.asyncio
async def test_with_async_fixture(async_client):
    """Test using async fixture."""
    response = await async_client.get("/api/data")
    assert response.status_code == 200
```

### Async Exception Testing

```python
@pytest.mark.asyncio
async def test_async_timeout():
    """Test async function raises timeout error."""
    with pytest.raises(asyncio.TimeoutError):
        await fetch_with_timeout(timeout=0.001)
```

### Concurrent Tests with TaskGroup (Python 3.11+)

```python
@pytest.mark.asyncio
async def test_concurrent_fetches() -> None:
    """Test multiple concurrent fetches."""
    async with asyncio.TaskGroup() as tg:
        task1 = tg.create_task(fetch_user(1))
        task2 = tg.create_task(fetch_user(2))

    assert task1.result().id == 1
    assert task2.result().id == 2
```

### Async Database Fixture Pattern

```python
# conftest.py
from collections.abc import AsyncIterator

@pytest.fixture
async def db_session() -> AsyncIterator[AsyncSession]:
    """Provide a database session that rolls back after test."""
    async with engine.begin() as conn:
        session = AsyncSession(bind=conn)
        try:
            yield session
        finally:
            await session.rollback()
```

## Property-Based Testing with Hypothesis

Instead of writing specific test cases, define properties that should hold for all inputs.

### Basic Property Test

```python
from hypothesis import given
from hypothesis import strategies as st


@given(st.integers(), st.integers())
def test_addition_is_commutative(a, b):
    """Addition should be commutative for all integers."""
    assert a + b == b + a


@given(st.lists(st.integers()))
def test_sort_is_idempotent(xs):
    """Sorting twice should equal sorting once."""
    assert sorted(sorted(xs)) == sorted(xs)


@given(st.text(), st.text())
def test_concat_length(a, b):
    """Concatenation length equals sum of lengths."""
    assert len(a + b) == len(a) + len(b)
```

### Using Strategies

```python
from hypothesis import given, assume
from hypothesis import strategies as st


@given(st.integers(min_value=1, max_value=100))
def test_positive_integers(n):
    """Test with constrained integers."""
    assert n > 0
    assert process(n) >= 0


@given(st.emails())
def test_email_validation(email):
    """Test email validation with generated emails."""
    assert is_valid_email(email)


@given(st.dictionaries(st.text(), st.integers()))
def test_dict_processing(d):
    """Test with generated dictionaries."""
    result = process_dict(d)
    assert len(result) == len(d)
```

### Composite Strategies

```python
from hypothesis import given
from hypothesis import strategies as st


@st.composite
def user_strategy(draw):
    """Generate valid User objects."""
    name = draw(st.text(min_size=1, max_size=50))
    age = draw(st.integers(min_value=0, max_value=150))
    email = draw(st.emails())
    return User(name=name, age=age, email=email)


@given(user_strategy())
def test_user_serialization(user):
    """Test user serialization round-trip."""
    serialized = user.to_dict()
    restored = User.from_dict(serialized)
    assert restored == user
```

### When to Use Property-Based Testing

**Good for:**

- Mathematical properties (commutativity, associativity)
- Serialization/deserialization round-trips
- Parsers and formatters
- Data structure invariants
- Finding edge cases you wouldn't think of

**When not needed:**

- Simple CRUD operations
- UI-specific behavior
- Tests requiring specific business scenarios

## Snapshot Testing

For testing complex outputs (JSON, HTML, data structures) without manually specifying expected values.

### Syrupy (External Snapshot Files)

```python
def test_report_generation(snapshot):
    """Test report output matches snapshot."""
    report = generate_report(data)
    assert report == snapshot


def test_api_response(snapshot):
    """Test API response structure."""
    response = client.get("/api/users")
    assert response.json() == snapshot
```

Snapshots are stored in `__snapshots__/` directories and should be committed to version control.

### Updating Snapshots

```bash
# Update all snapshots
pytest --snapshot-update

# Update specific test snapshots
pytest test_reports.py --snapshot-update
```

### When to Use Snapshots

**Good for:**

- Complex nested data structures
- API response validation
- Generated HTML/JSON output
- Configuration objects

**Avoid when:**

- Output changes frequently
- Simple scalar values
- Security-sensitive data

## Test Markers

### Skip Tests

```python
@pytest.mark.skip(reason="Feature not implemented yet")
def test_future_feature():
    """Test for upcoming feature."""
    pass


@pytest.mark.skipif(
    sys.platform == "win32",
    reason="Not supported on Windows"
)
def test_unix_only():
    """Test Unix-specific functionality."""
    pass
```

### Expected Failures

```python
@pytest.mark.xfail(reason="Known bug, fix pending")
def test_known_bug():
    """Test that exposes a known bug."""
    assert buggy_function() == expected_value
```

### Custom Markers

```python
@pytest.mark.slow
def test_large_dataset():
    """Test with large dataset (slow)."""
    pass


@pytest.mark.integration
def test_database_connection():
    """Integration test requiring database."""
    pass
```

### Registering Markers in pytest.ini

Always register custom markers to catch typos:

```ini
# pytest.ini
[pytest]
markers =
    slow: marks tests as slow (deselect with '-m "not slow"')
    integration: marks tests requiring external services
    e2e: marks end-to-end tests
strict_markers = true
```

With `strict_markers = true`, using an unregistered marker raises an error.

## Test Coverage

### Running Coverage

```bash
# Run tests with line coverage
pytest --cov=mypackage tests/

# Run with branch coverage (recommended)
pytest --cov=mypackage --cov-branch tests/

# Generate HTML report
pytest --cov=mypackage --cov-branch --cov-report=html tests/

# Fail if coverage below threshold
pytest --cov=mypackage --cov-fail-under=80 tests/

# Per-test coverage context (identify which tests cover which lines)
pytest --cov=mypackage --cov-context=test tests/
```

### Branch vs Line Coverage

**Line coverage** only checks if a line was executed.
**Branch coverage** checks if all paths through conditionals were taken.

```python
def categorize(x):
    if x > 0:
        return "positive"
    return "non-positive"

# Line coverage: calling categorize(1) covers all lines
# Branch coverage: also requires categorize(0) to cover the else branch
```

Always use `--cov-branch` for meaningful coverage.

### Coverage Targets

- General code: 70-80%
- Critical paths: 90%+
- Utility functions: 80%+

### What Coverage Doesn't Measure

- Quality of assertions
- Edge cases covered
- Error message clarity
- Test maintainability

## Common Patterns

### Testing Data Classes

```python
def test_user_creation():
    """Test User dataclass creation."""
    user = User(name="Alice", email="alice@example.com")

    assert user.name == "Alice"
    assert user.email == "alice@example.com"


def test_user_equality():
    """Test User equality comparison."""
    user1 = User(name="Alice", email="alice@example.com")
    user2 = User(name="Alice", email="alice@example.com")

    assert user1 == user2
```

### Testing Context Managers

```python
def test_context_manager_cleanup():
    """Test context manager performs cleanup."""
    with ResourceManager() as manager:
        manager.do_something()

    assert manager.is_closed


def test_context_manager_exception_handling():
    """Test context manager handles exceptions."""
    with pytest.raises(ValueError):
        with ResourceManager() as manager:
            raise ValueError("test error")

    assert manager.is_closed  # Cleanup still happens
```

### Testing Generators

```python
def test_number_generator():
    """Test number generator yields expected values."""
    gen = number_generator(3)

    assert next(gen) == 1
    assert next(gen) == 2
    assert next(gen) == 3

    with pytest.raises(StopIteration):
        next(gen)


def test_generator_as_list():
    """Test generator output as list."""
    result = list(number_generator(3))
    assert result == [1, 2, 3]
```

### Testing with Temporary Files

```python
def test_file_processing(tmp_path):
    """Test file processing with temporary directory."""
    # Create test file
    test_file = tmp_path / "input.txt"
    test_file.write_text("line1\nline2\nline3")

    # Process file
    result = process_file(test_file)

    assert result == ["line1", "line2", "line3"]


def test_file_writing(tmp_path):
    """Test file writing functionality."""
    output_file = tmp_path / "output.txt"

    write_data(output_file, ["a", "b", "c"])

    assert output_file.read_text() == "a\nb\nc\n"
```

## Project Structure

Organize tests by type for clarity and selective execution:

```
myproject/
├── src/
│   └── mypackage/
│       ├── __init__.py
│       └── core.py
├── tests/
│   ├── conftest.py          # Shared fixtures (DB, clients)
│   ├── unit/
│   │   ├── conftest.py      # Unit-specific fixtures
│   │   └── test_core.py
│   ├── integration/
│   │   ├── conftest.py      # Integration fixtures
│   │   └── test_api.py
│   └── e2e/
│       └── test_workflows.py
├── pytest.ini               # Marker registration, options
└── pyproject.toml
```

Run tests selectively:

```bash
# Run only unit tests
pytest tests/unit/

# Run only integration tests
pytest tests/integration/

# Run everything except slow tests
pytest -m "not slow"
```

## conftest.py Organization

### When to Create conftest.py

Create `tests/conftest.py` when ANY of these appear in 2+ test files:

- Module stubs (`sys.modules` manipulation, fake classes for external deps)
- Mock/stub class definitions (e.g., `DummyResponse`, `FakeClient`)
- Complex fixture setup (database connections, API clients)
- Shared test data factories

**Signal**: If you copy-paste setup code between test files, stop and move it to conftest.py.

### Hierarchical Structure

Each `conftest.py` provides fixtures to its directory and subdirectories:

- `tests/conftest.py` — shared across all tests
- `tests/unit/conftest.py` — only for unit tests
- `tests/integration/conftest.py` — only for integration tests

Fixtures are auto-discovered; no imports needed.

### Fixture Composition

Fixtures can request other fixtures:

```python
# tests/conftest.py
@pytest.fixture(scope="session")
def database_url():
    """Database URL for test database."""
    return "postgresql://localhost/test_db"


@pytest.fixture
def db_connection(database_url):
    """Database connection using the URL fixture."""
    conn = create_connection(database_url)
    yield conn
    conn.close()


@pytest.fixture
def user_repository(db_connection):
    """Repository fixture depending on db_connection."""
    return UserRepository(db_connection)
```

### Example conftest.py

Place shared fixtures in `conftest.py` at the appropriate level:

```python
# tests/conftest.py
import pytest
from collections.abc import AsyncIterator

@pytest.fixture
def sample_user() -> User:
    """Create a sample user for testing."""
    return User(id=1, name="Test User", email="test@example.com")

@pytest.fixture(scope="module")
def api_client() -> TestClient:
    """Shared test client for the module."""
    return TestClient(app)

@pytest.fixture
async def db_session() -> AsyncIterator[AsyncSession]:
    """Provide a database session that rolls back after test."""
    async with engine.begin() as conn:
        session = AsyncSession(bind=conn)
        try:
            yield session
        finally:
            await session.rollback()
```

**Fixture scope hierarchy:**

- `function` (default): New instance per test
- `class`: Shared across test class
- `module`: Shared across test module
- `session`: Shared across entire test run

## What NOT to Test

1. **Trivial getters/setters**: Properties that just return a value
2. **Third-party code**: Libraries you don't control
3. **Language features**: Python's built-in behavior
4. **Private methods**: Implementation details (prefix `_`)
5. **Type checking**: Let type checkers handle this
6. **Framework internals**: Django/Flask built-in functionality

## Quick Reference

### File Naming

- Test files: `test_<module>.py` or `<module>_test.py`
- Test classes: `Test<ClassName>`
- Test functions: `test_<function>_<scenario>`

### Common Assertions

```python
# Equality
assert result == expected

# Boolean
assert is_valid
assert not is_empty

# Containment
assert item in collection
assert key in dictionary

# Type checking
assert isinstance(obj, ExpectedType)

# Approximate equality (floats)
assert result == pytest.approx(expected, rel=1e-3)

# None checking
assert result is None
assert result is not None
```

### pytest Built-in Fixtures

- `tmp_path`: Temporary directory (Path object)
- `tmp_path_factory`: Factory for temporary directories
- `capsys`: Capture stdout/stderr
- `caplog`: Capture log messages
- `monkeypatch`: Modify objects, dicts, environment
- `request`: Request object for fixtures

### Essential CLI Options

```bash
pytest -v                    # Verbose output
pytest -x                    # Stop on first failure
pytest -k "test_user"        # Run tests matching pattern
pytest -m "not slow"         # Run tests without marker
pytest --lf                  # Re-run last failed tests
pytest --ff                  # Run failed tests first
pytest -n auto               # Parallel execution (pytest-xdist)
pytest --timeout=10          # Fail tests after 10s (pytest-timeout)
pytest -p randomly           # Randomize test order
```
