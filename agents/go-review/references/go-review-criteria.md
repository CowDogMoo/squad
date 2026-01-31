# Go Code Review Criteria

A comprehensive guide to reviewing Go code for quality, correctness, performance, and adherence to best practices. This document serves as the knowledge base for the go-review pattern.

## Table of Contents

1. [Review Philosophy](#review-philosophy)
2. [Code Formatting and Style](#code-formatting-and-style)
3. [Error Handling](#error-handling)
4. [Concurrency Patterns](#concurrency-patterns)
5. [Data Management](#data-management)
6. [Interface and Type Design](#interface-and-type-design)
7. [Code Structure](#code-structure)
8. [API Design Patterns](#api-design-patterns)
9. [Performance](#performance)
10. [Package Organization](#package-organization)
11. [Documentation](#documentation)
12. [Security Considerations](#security-considerations)
13. [Testing](#testing)
14. [Severity Classification](#severity-classification)

---

## Review Philosophy

### Core Principles

**Simplicity Over Complexity**
Go's coding conventions embody "simplicity beats complexity." Clarity and maintainability trump clever abstractions.

**Share Memory by Communicating**
Don't communicate by sharing memory; use channels to coordinate goroutines.

**Constructive Feedback**
- Be educational, not critical
- Explain the "why" behind suggestions
- Provide concrete examples with code
- Acknowledge good practices
- Prioritize actionable feedback
- Focus on idiomatic Go patterns, not personal preferences

---

## Code Formatting and Style

### Mandatory Checks

| Check | Severity | Rationale |
|-------|----------|-----------|
| Code formatted with gofmt | HIGH | Non-negotiable Go standard |
| Imports organized with goimports | MEDIUM | Standard library → third-party → local |
| MixedCaps naming (never underscores) | MEDIUM | Go naming convention |
| Short, meaningful names | LOW | Readability |
| Package names lowercase, concise | MEDIUM | Avoid util, common, helpers |

### Import Organization

Imports should be in three groups separated by blank lines:

```go
import (
    // Standard library
    "context"
    "fmt"

    // Third-party packages
    "github.com/gin-gonic/gin"

    // Local packages
    "yourproject/internal/handlers"
)
```

### Naming Conventions

**Good:**
```go
fetchUser        // short, clear verb
userID           // acronyms capitalized
httpClient       // well-known abbreviation
```

**Bad:**
```go
getUserDataFromDatabase  // too verbose
user_id                  // underscores not idiomatic
HTTPclient               // inconsistent capitalization
```

### Interface Naming

- Single-method interfaces use -er suffix: `Reader`, `Writer`, `Formatter`
- Multi-method interfaces describe capability: `FileSystem`, `Handler`

### Getters

- Omit "Get" prefix: use `Owner()` not `GetOwner()`
- Setters use "Set" prefix: `SetOwner()`

---

## Error Handling

### Critical Rules

| Rule | Severity | Example |
|------|----------|---------|
| Never ignore errors with `_` | CRITICAL | `_ = file.Close()` |
| Handle errors once at source | HIGH | Don't log and return |
| Return errors as last value | MEDIUM | `func X() (T, error)` |

### Error Wrapping (Go 1.13+)

**Good:**
```go
if err != nil {
    return fmt.Errorf("failed to fetch user %d: %w", id, err)
}
```

**Bad:**
```go
if err != nil {
    return errors.New("failed to fetch user")  // loses context
}
```

### Type Assertions

**Always check second value:**
```go
val, ok := x.(Type)
if !ok {
    // handle type mismatch
}
```

**Never:**
```go
val := x.(Type)  // panics if wrong type
```

---

## Concurrency Patterns

### Critical Checks

| Check | Severity | Impact |
|-------|----------|--------|
| Goroutines have clear exit conditions | CRITICAL | Prevents leaks |
| No goroutines in init() | HIGH | Startup unpredictability |
| Context.Context for lifecycle | HIGH | Graceful shutdown |
| No fire-and-forget goroutines | HIGH | Resource leaks |

### Worker Pool Pattern

**Good:**
```go
func worker(ctx context.Context, jobs <-chan Task, results chan<- Result) {
    for {
        select {
        case <-ctx.Done():
            return
        case job, ok := <-jobs:
            if !ok {
                return
            }
            results <- process(job)
        }
    }
}
```

### Context Usage

**Good:**
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
```

**Bad:**
```go
ctx := context.Background()  // no timeout, no cancellation
```

### Error Groups

**Good:**
```go
g, ctx := errgroup.WithContext(ctx)
g.Go(func() error { return task1(ctx) })
g.Go(func() error { return task2(ctx) })
if err := g.Wait(); err != nil {
    // First error cancels all
}
```

### Channel Sizing

- Unbuffered (size 0) = synchronization
- Buffered = throughput management
- Prefer unbuffered or size 1

---

## Data Management

### Slice/Map Boundaries

**Good - copy at boundaries:**
```go
func processItems(items []Item) {
    localItems := make([]Item, len(items))
    copy(localItems, items)
    // work with localItems
}
```

### Resource Cleanup

**Always use defer:**
```go
file, err := os.Open(filename)
if err != nil {
    return err
}
defer file.Close()
```

### Zero Values

Zero values should be meaningful:
```go
var mu sync.Mutex      // Ready to use
var buf bytes.Buffer   // Ready to use
var count int          // Zero is valid
```

### Preallocation

**Good:**
```go
items := make([]Item, 0, expectedSize)
cache := make(map[string]Value, expectedSize)
```

---

## Interface and Type Design

### Critical Checks

| Check | Severity | Rationale |
|-------|----------|-----------|
| No pointers to interfaces | HIGH | Almost never needed |
| Interfaces in consumer packages | MEDIUM | Loose coupling |
| Consistent receiver types | MEDIUM | Method set clarity |

### Compile-Time Interface Verification

```go
var _ http.Handler = (*MyHandler)(nil)
```

### Receiver Guidelines

- **Value receivers**: method doesn't modify, works on copies
- **Pointer receivers**: method modifies receiver, or receiver is large
- **Consistency**: if one method uses pointer, all should

---

## Code Structure

### Early Returns

**Good:**
```go
if err != nil {
    return err
}
// continue with normal flow
```

**Bad - deep nesting:**
```go
if err == nil {
    if result != nil {
        if result.Valid {
            // deeply nested logic
        }
    }
}
```

### Variable Scope

- Declare variables close to usage
- Minimize scope

### Type Switches

**Good:**
```go
switch v := x.(type) {
case int:
    // v is int
case string:
    // v is string
default:
    // handle unknown
}
```

---

## API Design Patterns

### Repository Pattern

```go
type UserRepository interface {
    GetByID(ctx context.Context, id string) (*User, error)
    Create(ctx context.Context, user *User) error
    Update(ctx context.Context, user *User) error
    Delete(ctx context.Context, id string) error
}
```

### Middleware Pattern

```go
func Logging(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        log.Printf("%s %s", r.Method, r.URL.Path)
        next.ServeHTTP(w, r)
    })
}
```

### Functional Options

```go
type Option func(*Server)

func WithPort(port int) Option {
    return func(s *Server) { s.port = port }
}

func NewServer(opts ...Option) *Server {
    s := &Server{port: 8080}
    for _, opt := range opts {
        opt(s)
    }
    return s
}
```

---

## Performance

### String Operations

| Pattern | Performance | Use Case |
|---------|-------------|----------|
| `strconv.Itoa()` | Fast | int to string |
| `fmt.Sprintf()` | Slower | Complex formatting |
| `strings.Builder` | Fast | Multiple concatenations |
| `+` operator | Slow | Avoid in loops |

### Time Handling

- Use `time.Time` for instants
- Use `time.Duration` for periods
- **Never** use `int` for time values

---

## Package Organization

### Checks

| Check | Severity | Rationale |
|-------|----------|-----------|
| Small, focused packages | MEDIUM | Single responsibility |
| Avoid generic names | MEDIUM | util, common, helpers |
| Limited global variables | HIGH | Testing difficulty |

### Good Package Names

- `user`, `auth`, `config` - clear purpose
- `httputil` - extension of http

### Bad Package Names

- `util`, `common`, `helpers` - unclear purpose
- `misc`, `shared` - grab bags

---

## Documentation

### Requirements

Every exported name must have doc comments:

```go
// UserService handles user-related operations.
// It provides methods for CRUD operations on user entities.
type UserService struct {
    repo UserRepository
}

// GetByID returns the user with the given ID.
// It returns ErrNotFound if the user doesn't exist.
func (s *UserService) GetByID(ctx context.Context, id string) (*User, error)
```

### Comment Quality

| Rule | Severity |
|------|----------|
| Complete sentences starting with declared name | MEDIUM |
| Focus on what/why for users | LOW |
| Document concurrency safety | MEDIUM |
| No blank line between comment and declaration | LOW |

---

## Security Considerations

### Critical Checks

| Check | Severity | Impact |
|-------|----------|--------|
| Input validation | CRITICAL | Injection attacks |
| SQL parameterization | CRITICAL | SQL injection |
| Secret management | CRITICAL | Credential exposure |
| TLS configuration | HIGH | Data in transit |
| Crypto usage | HIGH | Weak algorithms |

### Input Validation

```go
// Good - validate and sanitize
func ProcessInput(input string) error {
    if len(input) > MaxInputLength {
        return ErrInputTooLong
    }
    input = strings.TrimSpace(input)
    // continue processing
}
```

### SQL Queries

**Good:**
```go
db.Query("SELECT * FROM users WHERE id = $1", userID)
```

**Bad:**
```go
db.Query("SELECT * FROM users WHERE id = " + userID)  // SQL injection!
```

---

## Testing

### Coverage Expectations

| Type | Target | Priority |
|------|--------|----------|
| Unit tests | 70%+ | HIGH |
| Integration tests | Critical paths | MEDIUM |
| Benchmark tests | Hot paths | LOW |

### Testing Tools (2025)

| Tool | Adoption | Use Case |
|------|----------|----------|
| `testing` (stdlib) | Most common | Built-in testing package |
| [testify](https://github.com/stretchr/testify) | 27% | Assertions and mocking |
| [gomock](https://github.com/uber-go/mock) | 21% | Enterprise mock generation |

### Test Quality

- Table-driven tests for multiple cases
- Subtests for organization
- Clear test names describing scenario
- Test edge cases and error paths

### Example

```go
func TestGetUser(t *testing.T) {
    tests := []struct {
        name    string
        id      int
        want    *User
        wantErr bool
    }{
        {"valid user", 1, &User{ID: 1}, false},
        {"invalid id", -1, nil, true},
        {"not found", 999, nil, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := GetUser(tt.id)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
            }
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("got = %v, want %v", got, tt.want)
            }
        })
    }
}
```

---

## Severity Classification

### CRITICAL

Issues that affect correctness, security, or cause crashes:
- Ignored errors
- Goroutine leaks
- Race conditions
- Security vulnerabilities
- Panics in production code

### HIGH

Significant issues affecting reliability or maintainability:
- Poor error handling
- Missing context propagation
- Resource leaks
- Accessibility/usability issues
- Missing critical tests

### MEDIUM

Best practice violations:
- Missing documentation
- Inconsistent naming
- Non-idiomatic patterns
- Missing template variables
- Suboptimal performance

### LOW

Minor improvements:
- Naming convention tweaks
- Code organization
- Additional documentation
- Style consistency

### INFO

Suggestions for optimization:
- Advanced patterns
- Performance tuning
- Tooling recommendations

---

## Quick Reference Checklist

### Before Approving

- [ ] All tests pass
- [ ] No critical or high severity issues
- [ ] Error handling is complete
- [ ] Concurrency is safe
- [ ] Resources are properly cleaned up
- [ ] Public API is documented
- [ ] No security vulnerabilities
- [ ] Code is gofmt'd

### Common Issues to Watch

1. Ignored errors (`_ = something()`)
2. Goroutines without cancellation
3. Deep nesting instead of early returns
4. Missing error context
5. Unchecked type assertions
6. Global state
7. Missing defer for cleanup
8. Hardcoded values (magic numbers)

---

## References

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- [Go Proverbs](https://go-proverbs.github.io/)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- [Google Go Style Guide](https://google.github.io/styleguide/go/)

---

*Last updated: 2026-01-10*
