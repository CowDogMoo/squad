# Python Tests Pattern

Generate comprehensive, idiomatic Python tests using pytest following best practices and community standards.

## Overview

This pattern analyzes Python source code and generates well-structured test files using pytest. It focuses on:

- Parametrized tests for multiple input/output cases
- Fixture-based test organization
- Clear error case coverage
- Edge case handling
- Proper pytest conventions and idioms

## Installation

Ensure the pattern is available in your fabric patterns directory:

```bash
# Copy to fabric patterns directory
cp -r python-tests ~/.config/fabric/patterns/
```

## Usage

### Basic Usage

```bash
# Generate tests for a single file
cat calculator.py | fabric --pattern python-tests

# Save output directly to test file
cat calculator.py | fabric --pattern python-tests > test_calculator.py

# With filtering for clean output
cat calculator.py | fabric --pattern python-tests | ./filter.sh > test_calculator.py
```

### Batch Processing

```bash
# Generate tests for all Python files in a directory
for file in *.py; do
  [[ "$file" != test_* ]] && cat "$file" | fabric --pattern python-tests > "test_${file}"
done
```

### CI/CD Integration

```bash
# In your CI pipeline
fabric --pattern python-tests < src/new_module.py > tests/test_new_module.py
pytest tests/test_new_module.py
```

## Example

### Input (calculator.py)

```python
class Calculator:
    def __init__(self, precision: int = 2):
        self.precision = precision

    def add(self, a: float, b: float) -> float:
        return round(a + b, self.precision)

    def divide(self, a: float, b: float) -> float:
        if b == 0:
            raise ValueError("cannot divide by zero")
        return round(a / b, self.precision)
```

### Output (test_calculator.py)

```python
"""Tests for calculator module."""
import pytest
from calculator import Calculator


class TestCalculator:
    """Tests for Calculator class."""

    @pytest.fixture
    def calc(self):
        """Create a Calculator instance for testing."""
        return Calculator(precision=2)

    @pytest.mark.parametrize("a,b,expected", [
        (2, 3, 5),
        (0, 0, 0),
        (-1, 1, 0),
        (0.1, 0.2, 0.3),
    ])
    def test_add(self, calc, a, b, expected):
        """Test add returns correct sum."""
        assert calc.add(a, b) == expected

    @pytest.mark.parametrize("a,b,expected", [
        (10, 2, 5),
        (7, 2, 3.5),
        (-10, 2, -5),
    ])
    def test_divide(self, calc, a, b, expected):
        """Test divide returns correct quotient."""
        assert calc.divide(a, b) == expected

    def test_divide_by_zero_raises_error(self, calc):
        """Test divide raises ValueError when dividing by zero."""
        with pytest.raises(ValueError, match="cannot divide by zero"):
            calc.divide(10, 0)
```

## Pattern Structure

```
python-tests/
├── README.md                      # This documentation
├── system.md                      # LLM instruction framework
├── python-testing-patterns.md    # Knowledge base with testing patterns
├── test-code.py                   # Sample code for pattern testing
├── filter.sh                      # Post-processing script
└── test-pattern.sh                # Automated testing script
```

## Best Practices Applied

1. **Parametrized Tests**: Uses `@pytest.mark.parametrize` for multiple test cases
2. **Fixtures**: Leverages pytest fixtures for setup and teardown
3. **Clear Naming**: Test names describe what is being tested and expected behavior
4. **Error Testing**: Uses `pytest.raises` with match patterns for exception testing
5. **Test Organization**: Groups related tests in classes
6. **Minimal Mocking**: Only mocks external dependencies when necessary
7. **Behavior Testing**: Tests observable behavior, not implementation details

## Customization

### Modify Knowledge Base

Edit `python-testing-patterns.md` to add or modify testing patterns for your specific needs.

### Adjust System Prompt

Edit `system.md` to change the test generation behavior or add specific constraints.

## Troubleshooting

### Tests don't run

Ensure pytest is installed:
```bash
pip install pytest
```

### Import errors in generated tests

The pattern assumes the source file is in the same directory or properly installed. Adjust imports as needed.

### Missing edge cases

The pattern focuses on common cases. Review generated tests and add domain-specific edge cases manually.

## Related Patterns

- `go-tests` - Generate Go tests
- `python-review` - Review Python code for best practices
- `python-refactor` - Refactor Python code

## Version

- **Version**: 1.0.0
- **Last Updated**: 2026-01-18
- **Compatibility**: Python 3.8+, pytest 7.0+
