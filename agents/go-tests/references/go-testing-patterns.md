# Go Testing Patterns

A comprehensive guide to writing effective Go tests following community best practices and the Go testing philosophy. This document serves as the knowledge base for the go-tests pattern.

## Table of Contents

1. [Testing Philosophy](#testing-philosophy)
2. [Table-Driven Tests](#table-driven-tests)
3. [Subtests](#subtests)
4. [Test Helpers](#test-helpers)
5. [Mocking Strategies](#mocking-strategies)
6. [Benchmarks](#benchmarks)
7. [Test Coverage](#test-coverage)
8. [Common Patterns](#common-patterns)
9. [What Not to Test](#what-not-to-test)

---

## Testing Philosophy

### Core Principles

| Principle | Description |
|-----------|-------------|
| Simple and readable | Tests should be easy to understand at a glance |
| Test behavior | Test what the code does, not how it does it |
| Avoid over-mocking | Only mock when truly necessary |
| Clarity over DRY | Prefer clarity to avoiding repetition in tests |
| One thing per test | Each test case should verify one behavior |

### Simplicity Guidelines

**DO:**
- Write tests that are easy to understand at a glance
- Use inline test data rather than complex fixtures
- Keep test setup minimal
- Use simple assertions
- Test one thing per test case

**DON'T:**
- Create test helpers unless used in many places
- Build complex test hierarchies
- Over-parameterize tests
- Add unnecessary interfaces or abstractions
- Write tests for unlikely edge cases
- Use reflection or advanced features unnecessarily

---

## Table-Driven Tests

Table-driven tests are idiomatic Go when you have multiple similar test cases.

### Basic Structure

```go
func TestParseURL(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    *URL
        wantErr bool
    }{
        {
            name:  "valid http url",
            input: "http://example.com",
            want:  &URL{Scheme: "http", Host: "example.com"},
        },
        {
            name:  "valid https url",
            input: "https://example.com/path",
            want:  &URL{Scheme: "https", Host: "example.com", Path: "/path"},
        },
        {
            name:    "invalid url",
            input:   "://invalid",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseURL(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("ParseURL() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("ParseURL() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### When to Use Table-Driven Tests

**Good for:**
- Functions with multiple input/output combinations
- Validation functions
- Parsing functions
- Mathematical operations

**Not needed for:**
- Single test case scenarios
- Complex setup/teardown requirements
- Tests with significantly different logic per case

---

## Subtests

### Basic Subtests

```go
func TestUser(t *testing.T) {
    t.Run("creation", func(t *testing.T) {
        user := NewUser("test")
        if user.Name != "test" {
            t.Errorf("Name = %q, want %q", user.Name, "test")
        }
    })

    t.Run("validation", func(t *testing.T) {
        user := &User{}
        if err := user.Validate(); err == nil {
            t.Error("expected error for empty user")
        }
    })
}
```

### Parallel Subtests

```go
func TestParallel(t *testing.T) {
    tests := []struct {
        name  string
        input int
        want  int
    }{
        {"case1", 1, 2},
        {"case2", 2, 4},
        {"case3", 3, 6},
    }

    for _, tt := range tests {
        tt := tt // capture range variable
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got := Double(tt.input)
            if got != tt.want {
                t.Errorf("Double(%d) = %d, want %d", tt.input, got, tt.want)
            }
        })
    }
}
```

---

## Test Helpers

### When to Create Helpers

Only create helpers when:
- Used in multiple test files
- Significantly reduces test code complexity
- Makes tests more readable

### Helper Pattern

```go
// testHelper marks the function as a test helper and reports
// the caller's location on failure.
func testHelper(t *testing.T) {
    t.Helper()
    // helper logic
}

// newTestUser creates a User for testing with sensible defaults.
func newTestUser(t *testing.T, name string) *User {
    t.Helper()
    user := &User{
        ID:        1,
        Name:      name,
        CreatedAt: time.Now(),
    }
    return user
}
```

### Cleanup Pattern

```go
func TestWithCleanup(t *testing.T) {
    tempDir := t.TempDir() // automatically cleaned up

    // Or manual cleanup
    file, err := os.CreateTemp("", "test")
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() {
        os.Remove(file.Name())
    })
}
```

---

## Mocking Strategies

### Interface-Based Mocking

```go
// Define interface in consumer package
type UserRepository interface {
    GetByID(ctx context.Context, id string) (*User, error)
}

// Mock implementation for tests
type mockUserRepo struct {
    users map[string]*User
    err   error
}

func (m *mockUserRepo) GetByID(ctx context.Context, id string) (*User, error) {
    if m.err != nil {
        return nil, m.err
    }
    return m.users[id], nil
}

func TestUserService(t *testing.T) {
    repo := &mockUserRepo{
        users: map[string]*User{"1": {ID: "1", Name: "Test"}},
    }
    service := NewUserService(repo)

    user, err := service.GetUser(context.Background(), "1")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if user.Name != "Test" {
        t.Errorf("Name = %q, want %q", user.Name, "Test")
    }
}
```

### When to Mock

**Do mock:**
- External services (databases, APIs)
- Time-dependent behavior
- Random/non-deterministic behavior

**Don't mock:**
- Simple value objects
- Standard library functions
- Internal implementation details

---

## Benchmarks

### Basic Benchmark

```go
func BenchmarkParseURL(b *testing.B) {
    input := "https://example.com/path?query=value"
    for i := 0; i < b.N; i++ {
        ParseURL(input)
    }
}
```

### Benchmark with Setup

```go
func BenchmarkProcess(b *testing.B) {
    // Setup (not timed)
    data := generateTestData(1000)

    b.ResetTimer() // Reset timer after setup
    for i := 0; i < b.N; i++ {
        Process(data)
    }
}
```

### Sub-Benchmarks

```go
func BenchmarkCache(b *testing.B) {
    sizes := []int{100, 1000, 10000}
    for _, size := range sizes {
        b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
            cache := NewCache(size)
            b.ResetTimer()
            for i := 0; i < b.N; i++ {
                cache.Get("key")
            }
        })
    }
}
```

### Memory Benchmarks

```go
func BenchmarkAlloc(b *testing.B) {
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _ = make([]byte, 1024)
    }
}
```

---

## Test Coverage

### Running Coverage

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...

# View coverage in browser
go tool cover -html=coverage.out

# Check coverage percentage
go test -cover ./...
```

### Coverage Goals

| Type | Target |
|------|--------|
| Unit tests | 70-80% |
| Critical paths | 90%+ |
| Edge cases | As needed |

### What Coverage Doesn't Tell You

- Quality of assertions
- Whether edge cases are tested
- Whether tests are meaningful
- Whether error paths are properly tested

---

## Common Patterns

### Error Testing

```go
func TestValidate_Errors(t *testing.T) {
    tests := []struct {
        name    string
        input   *User
        wantErr string
    }{
        {
            name:    "nil user",
            input:   nil,
            wantErr: "user is nil",
        },
        {
            name:    "empty name",
            input:   &User{},
            wantErr: "name is required",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := Validate(tt.input)
            if err == nil {
                t.Fatal("expected error, got nil")
            }
            if !strings.Contains(err.Error(), tt.wantErr) {
                t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
            }
        })
    }
}
```

### Context Testing

```go
func TestWithContext(t *testing.T) {
    t.Run("respects cancellation", func(t *testing.T) {
        ctx, cancel := context.WithCancel(context.Background())
        cancel() // Cancel immediately

        err := DoWork(ctx)
        if !errors.Is(err, context.Canceled) {
            t.Errorf("expected context.Canceled, got %v", err)
        }
    })

    t.Run("respects timeout", func(t *testing.T) {
        ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
        defer cancel()

        err := SlowWork(ctx)
        if !errors.Is(err, context.DeadlineExceeded) {
            t.Errorf("expected context.DeadlineExceeded, got %v", err)
        }
    })
}
```

### HTTP Handler Testing

```go
func TestHandler(t *testing.T) {
    handler := NewHandler()

    t.Run("GET returns user", func(t *testing.T) {
        req := httptest.NewRequest("GET", "/users/1", nil)
        w := httptest.NewRecorder()

        handler.ServeHTTP(w, req)

        if w.Code != http.StatusOK {
            t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
        }

        var user User
        if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
            t.Fatalf("failed to unmarshal: %v", err)
        }
        if user.ID != "1" {
            t.Errorf("ID = %q, want %q", user.ID, "1")
        }
    })
}
```

---

## What Not to Test

### Skip Testing

- **Trivial getters/setters** without logic
- **Third-party code** (trust the library)
- **Language features** (Go's behavior)
- **Unlikely scenarios** that can't reasonably occur

### Example: Don't Test This

```go
// Trivial getter - no need to test
func (u *User) Name() string {
    return u.name
}

// Simple delegation - test the underlying function instead
func (s *Service) GetUser(id string) (*User, error) {
    return s.repo.GetByID(id)
}
```

### Example: Do Test This

```go
// Has logic - should test
func (u *User) FullName() string {
    if u.MiddleName != "" {
        return fmt.Sprintf("%s %s %s", u.FirstName, u.MiddleName, u.LastName)
    }
    return fmt.Sprintf("%s %s", u.FirstName, u.LastName)
}

// Has validation - should test
func (s *Service) CreateUser(u *User) error {
    if err := u.Validate(); err != nil {
        return fmt.Errorf("invalid user: %w", err)
    }
    return s.repo.Create(u)
}
```

---

## Quick Reference

### Test File Naming

- `foo.go` → `foo_test.go`
- Test functions: `TestXxx(t *testing.T)`
- Benchmarks: `BenchmarkXxx(b *testing.B)`
- Examples: `ExampleXxx()`

### Common Assertions

```go
// Equality
if got != want {
    t.Errorf("got %v, want %v", got, want)
}

// Deep equality
if !reflect.DeepEqual(got, want) {
    t.Errorf("got %+v, want %+v", got, want)
}

// Error presence
if err != nil {
    t.Fatalf("unexpected error: %v", err)
}

// Error expected
if err == nil {
    t.Fatal("expected error, got nil")
}
```

---

## References

- [Go Testing Package](https://pkg.go.dev/testing)
- [Table-Driven Tests](https://go.dev/wiki/TableDrivenTests)
- [Testing Techniques](https://go.dev/doc/articles/testing-techniques)
- [testify Package](https://github.com/stretchr/testify)

---

*Last updated: 2026-01-10*
