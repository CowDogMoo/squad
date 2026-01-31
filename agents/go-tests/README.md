# Go Tests Pattern

A comprehensive fabric pattern for generating Go tests following community best practices. This pattern analyzes Go code and creates table-driven tests with proper coverage of normal operation and edge cases.

## Pattern Structure

This pattern includes:
- **`system.md`** - The test generation framework and prompt engineering for LLM
- **`go-testing-patterns.md`** - Comprehensive reference document with testing patterns (source of truth)
- **`filter.sh`** - Post-processing to clean up output formatting
- **`README.md`** - This documentation
- **`test-code.go`** - Sample code for testing
- **`test-pattern.sh`** - Automated testing script

The pattern is designed with **separation of concerns**: the `go-testing-patterns.md` contains the knowledge base (testing patterns), while `system.md` contains the execution framework (how to generate tests).

## Purpose

This pattern helps you:
- **Generate tests** for existing Go code
- **Follow best practices** for Go testing
- **Create table-driven tests** for multiple scenarios
- **Cover edge cases** systematically
- **Learn testing patterns** through examples

## Features

- Table-driven test generation
- Subtest organization
- Error case coverage
- Context and timeout testing
- HTTP handler testing patterns
- Benchmark generation
- Clear test documentation

## Testing Categories

1. **Table-Driven Tests** - Multiple similar test cases
2. **Subtests** - Grouping related tests
3. **Error Testing** - Testing error conditions
4. **Context Testing** - Cancellation and timeouts
5. **HTTP Testing** - Handler and client tests
6. **Benchmarks** - Performance testing

## Installation

This pattern is part of the fabric-patterns-hub. Ensure you have fabric installed:

```bash
# Install fabric if you haven't already
pip install fabric-ai

# Add this patterns repository to fabric
fabric --add-pattern-source /path/to/fabric-patterns-hub/patterns
```

Or use it directly:

```bash
fabric --pattern /path/to/fabric-patterns-hub/patterns/go-tests
```

## Usage

### Generate Tests for a File

```bash
cat myfile.go | fabric --pattern go-tests > myfile_test.go
```

### Generate Tests from Clipboard

```bash
pbpaste | fabric --pattern go-tests | pbcopy
```

### Generate Tests for All Files

```bash
for file in *.go; do
  if [[ "$file" != *_test.go ]]; then
    echo "Generating tests for $file..."
    cat "$file" | fabric --pattern go-tests > "${file%.go}_test.go"
  fi
done
```

### CI/CD Integration

```bash
#!/bin/bash
# Generate tests for new files

for file in $(git diff --name-only HEAD~1 | grep '\.go$' | grep -v '_test\.go$'); do
  test_file="${file%.go}_test.go"
  if [ ! -f "$test_file" ]; then
    echo "Generating tests for $file..."
    cat "$file" | fabric --pattern go-tests > "$test_file"
  fi
done
```

## Output Format

### Test File

Complete, runnable Go test file with:
- Proper package declaration (`package_test`)
- Necessary imports
- Table-driven tests where appropriate
- Clear test names and documentation

### Test Summary

- Count of functions tested
- Count of test cases generated
- Coverage areas

### Notes

Test design decisions and improvement suggestions.

## Example

### Input

```go
package math

func Add(a, b int) int {
    return a + b
}

func Divide(a, b int) (int, error) {
    if b == 0 {
        return 0, errors.New("division by zero")
    }
    return a / b, nil
}
```

### Output

```go
package math_test

import (
    "testing"

    "example.com/math"
)

// TestAdd verifies Add returns the correct sum for various inputs.
func TestAdd(t *testing.T) {
    tests := []struct {
        name string
        a, b int
        want int
    }{
        {"positive numbers", 2, 3, 5},
        {"negative numbers", -2, -3, -5},
        {"zero", 0, 0, 0},
        {"mixed signs", -2, 3, 1},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := math.Add(tt.a, tt.b)
            if got != tt.want {
                t.Errorf("Add(%d, %d) = %d, want %d",
                    tt.a, tt.b, got, tt.want)
            }
        })
    }
}

// TestDivide verifies Divide handles normal cases and division by zero.
func TestDivide(t *testing.T) {
    tests := []struct {
        name    string
        a, b    int
        want    int
        wantErr bool
    }{
        {"normal division", 10, 2, 5, false},
        {"division by zero", 10, 0, 0, true},
        {"zero dividend", 0, 5, 0, false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := math.Divide(tt.a, tt.b)
            if (err != nil) != tt.wantErr {
                t.Errorf("Divide() error = %v, wantErr %v",
                    err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("Divide(%d, %d) = %d, want %d",
                    tt.a, tt.b, got, tt.want)
            }
        })
    }
}
```

## Best Practices

### When to Use This Pattern

**Good use cases:**
- Generating initial test coverage
- Adding tests to legacy code
- Learning Go testing patterns
- Ensuring consistent test structure

**Not ideal for:**
- Complex integration tests (need more context)
- Tests requiring specific mocking strategies
- Performance-critical benchmark design

### Tips for Best Results

1. **Provide complete code** - More context yields better tests
2. **Include types** - Pattern needs to understand data structures
3. **Review output** - May need domain-specific adjustments
4. **Run tests** - Verify generated tests compile and pass

## Customization

### Extending Testing Patterns

Edit `go-testing-patterns.md` to add organization-specific patterns:

```markdown
## Company-Specific Patterns

### Database Testing

All database tests must:
- Use test containers
- Clean up after each test
- Use transaction rollback where possible
```

### Adjusting Output Format

Edit the `# OUTPUT FORMAT` section in `system.md` to customize test structure.

## Troubleshooting

### Issue: Tests don't compile

**Solution:** Ensure all necessary types and imports are included in the input.

### Issue: Missing edge cases

**Solution:** The pattern covers common cases. Add domain-specific edge cases manually.

### Issue: Tests are too verbose

**Solution:** Edit generated tests to remove unnecessary cases.

## Related Patterns

- **go-refactor** - Refactor Go code
- **go-review** - Review Go code (checks test coverage)
- **go-doc-comments** - Document Go code

## References

### Pattern Documentation

- **`go-testing-patterns.md`** - Comprehensive testing patterns reference

### External Resources

- [Go Testing Package](https://pkg.go.dev/testing)
- [Table-Driven Tests](https://go.dev/wiki/TableDrivenTests)
- [testify Package](https://github.com/stretchr/testify)

## Contributing

Contributions welcome! Submit PRs for new testing patterns or improvements.

## License

Part of fabric-patterns-hub, follows parent repository license.

---

**Version:** 1.0.0
**Last Updated:** 2026-01-10
**Maintainer:** fabric-patterns-hub contributors
