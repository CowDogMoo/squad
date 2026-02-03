# Python Testing Patterns with pytest

This document serves as the knowledge base for Python testing conventions and patterns using pytest.

## Testing Philosophy

### Core Principles

1. **Simple and readable**: Tests should be easy to understand at a glance
2. **Test behavior, not implementation**: Focus on what the code does, not how
3. **Avoid over-mocking**: Only mock external dependencies
4. **Arrange-Act-Assert**: Structure tests clearly with setup, execution, verification
5. **One concept per test**: Each test should verify one thing

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

## Test Coverage

### Running Coverage

```bash
# Run tests with coverage
pytest --cov=mypackage tests/

# Generate HTML report
pytest --cov=mypackage --cov-report=html tests/

# Fail if coverage below threshold
pytest --cov=mypackage --cov-fail-under=80 tests/
```

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
