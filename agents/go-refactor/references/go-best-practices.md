# Go Best Practices Reference

A comprehensive guide to idiomatic Go patterns, refactoring techniques, and best practices. This document serves as the knowledge base for the go-refactor pattern and can be referenced by other Go-related patterns.

## Table of Contents

1. [Core Principles](#core-principles)
2. [Code Structure Patterns](#code-structure-patterns)
3. [Error Handling](#error-handling)
4. [Concurrency Patterns](#concurrency-patterns)
5. [Resource Management](#resource-management)
6. [Performance Optimization](#performance-optimization)
7. [API Design](#api-design)
8. [Type Design](#type-design)
9. [Documentation Standards](#documentation-standards)
10. [When to Apply](#when-to-apply)

---

## Core Principles

### Simplicity Over Complexity

Go's philosophy emphasizes clarity and simplicity over clever abstractions. When refactoring:

- Favor clear, simple solutions that are easy to understand
- If refactored code is harder to understand, revert to simpler approach
- Avoid premature optimization and over-engineering
- Write code that can be read by humans first, machines second

### Preserve Functionality

Refactoring should never alter behavior:

- The refactored code must behave identically to the original
- Never change the public API unless explicitly requested
- Maintain backwards compatibility
- Keep changes focused - don't over-refactor

### Go Proverbs

Key principles from Rob Pike's Go Proverbs:

- "Clear is better than clever"
- "Don't communicate by sharing memory, share memory by communicating"
- "A little copying is better than a little dependency"
- "The bigger the interface, the weaker the abstraction"
- "Make the zero value useful"

---

## Code Structure Patterns

### 1. Reduce Nesting with Early Returns

Deep nesting reduces readability. Use guard clauses to handle edge cases first.

**Anti-pattern:**
```go
func GetUser(id int) (*User, error) {
    if id > 0 {
        user, err := fetchUser(id)
        if err == nil {
            if user.Active {
                return user, nil
            } else {
                return nil, errors.New("user inactive")
            }
        } else {
            return nil, err
        }
    } else {
        return nil, errors.New("invalid id")
    }
}
```

**Idiomatic:**
```go
func GetUser(id int) (*User, error) {
    if id <= 0 {
        return nil, errors.New("invalid id")
    }

    user, err := fetchUser(id)
    if err != nil {
        return nil, err
    }

    if !user.Active {
        return nil, errors.New("user inactive")
    }

    return user, nil
}
```

**Why:** Early returns flatten the code structure, making the happy path clear and reducing cognitive load.

### 2. Minimize Variable Scope

Declare variables close to where they're used.

**Anti-pattern:**
```go
func ProcessData() error {
    var data []byte
    var err error
    var result string

    data, err = loadData()
    if err != nil {
        return err
    }

    result, err = transform(data)
    if err != nil {
        return err
    }

    return save(result)
}
```

**Idiomatic:**
```go
func ProcessData() error {
    data, err := loadData()
    if err != nil {
        return err
    }

    result, err := transform(data)
    if err != nil {
        return err
    }

    return save(result)
}
```

**Why:** Smaller scope reduces the cognitive load and prevents accidental misuse of variables.

### 3. Replace Magic Numbers with Constants

Named constants make code self-documenting.

**Anti-pattern:**
```go
func ValidateInput(s string) error {
    if len(s) > 100 {
        return errors.New("input too long")
    }
    if len(s) < 5 {
        return errors.New("input too short")
    }
    return nil
}
```

**Idiomatic:**
```go
const (
    MinInputLength = 5
    MaxInputLength = 100
)

func ValidateInput(s string) error {
    if len(s) > MaxInputLength {
        return errors.New("input too long")
    }
    if len(s) < MinInputLength {
        return errors.New("input too short")
    }
    return nil
}
```

**Why:** Named constants communicate intent and make updates easier.

---

## Error Handling

### 4. Proper Error Wrapping (Go 1.13+)

Wrap errors with context using `fmt.Errorf` and `%w`.

**Anti-pattern:**
```go
func ProcessFile(path string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return errors.New("failed to read file")
    }
    // process data
    return nil
}
```

**Idiomatic:**
```go
func ProcessFile(path string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return fmt.Errorf("failed to read file %s: %w", path, err)
    }
    // process data
    return nil
}
```

**Why:** Error wrapping preserves the error chain, enabling `errors.Is()` and `errors.As()` checks while adding context.

### 5. Handle Errors Once

Don't log and return the same error - handle it once at the appropriate level.

**Anti-pattern:**
```go
func DoSomething() error {
    err := operation()
    if err != nil {
        log.Printf("operation failed: %v", err)
        return err
    }
    return nil
}
```

**Idiomatic:**
```go
// Either log it (at the top level)
func main() {
    if err := DoSomething(); err != nil {
        log.Fatal(err)
    }
}

// Or return it (in library code)
func DoSomething() error {
    if err := operation(); err != nil {
        return fmt.Errorf("doing something: %w", err)
    }
    return nil
}
```

**Why:** Logging and returning creates duplicate log entries and makes debugging harder.

### 6. Always Check Type Assertions

Unchecked type assertions can panic.

**Anti-pattern:**
```go
val := x.(string) // panics if x is not a string
```

**Idiomatic:**
```go
val, ok := x.(string)
if !ok {
    return errors.New("expected string type")
}
```

**Why:** The comma-ok idiom prevents panics and allows graceful error handling.

---

## Concurrency Patterns

### 7. Use Context for Goroutine Lifecycle

Never launch goroutines without a way to stop them.

**Anti-pattern:**
```go
func StartWorker() {
    go func() {
        for {
            doWork()
            time.Sleep(time.Second)
        }
    }()
}
```

**Idiomatic:**
```go
func StartWorker(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(time.Second)
        defer ticker.Stop()

        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                doWork()
            }
        }
    }()
}
```

**Why:** Context provides a standard way to signal cancellation and prevents goroutine leaks.

### 8. Use Error Groups for Concurrent Operations

Replace manual WaitGroup + error handling with errgroup.

**Anti-pattern:**
```go
func FetchAll(ids []int) ([]*User, error) {
    var wg sync.WaitGroup
    users := make([]*User, len(ids))
    var firstErr error
    var errOnce sync.Once

    for i, id := range ids {
        wg.Add(1)
        go func(i, id int) {
            defer wg.Done()
            user, err := fetchUser(id)
            if err != nil {
                errOnce.Do(func() { firstErr = err })
                return
            }
            users[i] = user
        }(i, id)
    }

    wg.Wait()
    return users, firstErr
}
```

**Idiomatic:**
```go
func FetchAll(ctx context.Context, ids []int) ([]*User, error) {
    g, ctx := errgroup.WithContext(ctx)
    users := make([]*User, len(ids))

    for i, id := range ids {
        i, id := i, id // capture loop variables
        g.Go(func() error {
            user, err := fetchUser(ctx, id)
            if err != nil {
                return err
            }
            users[i] = user
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        return nil, err
    }
    return users, nil
}
```

**Why:** errgroup handles synchronization, error propagation, and context cancellation cleanly.

---

## Resource Management

### 9. Use defer for Resource Cleanup

Always use defer to ensure resources are released.

**Anti-pattern:**
```go
func ProcessFile(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }

    data, err := io.ReadAll(file)
    if err != nil {
        file.Close()
        return err
    }

    err = process(data)
    file.Close()
    return err
}
```

**Idiomatic:**
```go
func ProcessFile(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()

    data, err := io.ReadAll(file)
    if err != nil {
        return err
    }

    return process(data)
}
```

**Why:** defer ensures cleanup happens even if the function panics or returns early.

### 10. Copy Slices at Boundaries

Prevent callers from modifying internal state.

**Anti-pattern:**
```go
func ProcessItems(items []Item) []Item {
    for i := range items {
        items[i].Process()
    }
    return items
}
```

**Idiomatic:**
```go
func ProcessItems(items []Item) []Item {
    // Copy to prevent modifying caller's slice
    result := make([]Item, len(items))
    copy(result, items)

    for i := range result {
        result[i].Process()
    }
    return result
}
```

**Why:** Slices are reference types; modifying them affects the original data.

---

## Performance Optimization

### 11. Preallocate Slices with Known Capacity

Avoid repeated allocations during append.

**Anti-pattern:**
```go
func ProcessItems(items []Item) []Result {
    var results []Result
    for _, item := range items {
        results = append(results, process(item))
    }
    return results
}
```

**Idiomatic:**
```go
func ProcessItems(items []Item) []Result {
    results := make([]Result, 0, len(items))
    for _, item := range items {
        results = append(results, process(item))
    }
    return results
}
```

**Why:** Preallocation avoids O(n) reallocations, improving performance for large slices.

### 12. Use strings.Builder for Concatenation

String concatenation with `+` creates new strings each time.

**Anti-pattern:**
```go
func BuildMessage(parts []string) string {
    msg := ""
    for _, part := range parts {
        msg += part + " "
    }
    return msg
}
```

**Idiomatic:**
```go
func BuildMessage(parts []string) string {
    var builder strings.Builder
    for i, part := range parts {
        builder.WriteString(part)
        if i < len(parts)-1 {
            builder.WriteString(" ")
        }
    }
    return builder.String()
}
```

**Why:** strings.Builder minimizes allocations by maintaining a growing buffer.

### 13. Use strconv Instead of fmt for Conversions

strconv is faster than fmt.Sprintf for simple conversions.

**Anti-pattern:**
```go
func FormatID(id int) string {
    return fmt.Sprintf("%d", id)
}
```

**Idiomatic:**
```go
func FormatID(id int) string {
    return strconv.Itoa(id)
}
```

**Why:** strconv avoids the overhead of format string parsing.

### 14. Use time.Duration Instead of int

Type safety prevents unit confusion.

**Anti-pattern:**
```go
func WaitForTask(seconds int) {
    time.Sleep(time.Duration(seconds) * time.Second)
}
```

**Idiomatic:**
```go
func WaitForTask(d time.Duration) {
    time.Sleep(d)
}
```

**Why:** time.Duration is self-documenting and prevents unit mistakes.

---

## API Design

### 15. Replace Global Variables with Dependencies

Dependency injection makes code testable and explicit.

**Anti-pattern:**
```go
var db *sql.DB

func Init() {
    db = connectDatabase()
}

func GetUser(id int) (*User, error) {
    return queryUser(db, id)
}
```

**Idiomatic:**
```go
type UserService struct {
    db *sql.DB
}

func NewUserService(db *sql.DB) *UserService {
    return &UserService{db: db}
}

func (s *UserService) GetUser(id int) (*User, error) {
    return queryUser(s.db, id)
}
```

**Why:** Explicit dependencies make testing easier and eliminate hidden state.

### 16. Use Functional Options for Configuration

Flexible configuration without breaking changes.

**Anti-pattern:**
```go
func NewServer(host string, port int, timeout time.Duration, maxConns int) *Server {
    return &Server{
        Host: host,
        Port: port,
        Timeout: timeout,
        MaxConns: maxConns,
    }
}
```

**Idiomatic:**
```go
type Server struct {
    host     string
    port     int
    timeout  time.Duration
    maxConns int
}

type Option func(*Server)

func WithPort(port int) Option {
    return func(s *Server) { s.port = port }
}

func WithTimeout(timeout time.Duration) Option {
    return func(s *Server) { s.timeout = timeout }
}

func WithMaxConns(maxConns int) Option {
    return func(s *Server) { s.maxConns = maxConns }
}

func NewServer(host string, opts ...Option) *Server {
    s := &Server{
        host:     host,
        port:     8080,           // default
        timeout:  30 * time.Second, // default
        maxConns: 100,            // default
    }
    for _, opt := range opts {
        opt(s)
    }
    return s
}
```

**Why:** Functional options allow adding new options without breaking existing callers.

---

## Type Design

### 17. Define Interfaces in Consumer Packages

Interfaces belong where they're used, not where they're implemented.

**Anti-pattern:**
```go
// In database package
type UserRepository interface {
    GetUser(id int) (*User, error)
}

type PostgresRepo struct{}

func (r *PostgresRepo) GetUser(id int) (*User, error) {
    // implementation
}
```

**Idiomatic:**
```go
// In service package (consumer)
type UserRepository interface {
    GetUser(id int) (*User, error)
}

type UserService struct {
    repo UserRepository
}

// In database package
type PostgresRepo struct{}

func (r *PostgresRepo) GetUser(id int) (*User, error) {
    // implementation
}
```

**Why:** Consumer-defined interfaces enable loose coupling and easier testing.

### 18. Use Compile-Time Interface Verification

Verify interface implementation at compile time.

```go
// Verify that MyHandler implements http.Handler
var _ http.Handler = (*MyHandler)(nil)
```

**Why:** Catches interface implementation errors at compile time rather than runtime.

### 19. Consistent Receiver Types

If one method uses a pointer receiver, all methods should.

**Anti-pattern:**
```go
func (u User) Name() string { return u.name }
func (u *User) SetName(name string) { u.name = name }
```

**Idiomatic:**
```go
func (u *User) Name() string { return u.name }
func (u *User) SetName(name string) { u.name = name }
```

**Why:** Consistency prevents confusion about method sets and interface implementation.

---

## Documentation Standards

### 20. Improve Documentation Comments

Follow Go's documentation conventions.

**Anti-pattern:**
```go
// gets a user
func GetUser(id int) (*User, error) {
```

**Idiomatic:**
```go
// GetUser returns the user with the given ID or returns an error
// if the user doesn't exist.
func GetUser(id int) (*User, error) {
```

**Rules:**
- Start with the declared name
- Use complete sentences
- Explain what, not how
- Document concurrency safety when relevant
- No blank line between comment and declaration

---

## When to Apply

### High Impact Patterns

Apply these first - they have the greatest effect on code quality:

1. **Early returns** - Dramatically improves readability
2. **Error wrapping** - Essential for debugging
3. **Context for goroutines** - Prevents resource leaks
4. **defer for cleanup** - Prevents resource leaks

### Medium Impact Patterns

Apply when the codebase is stable:

1. **Functional options** - When APIs need flexibility
2. **Dependency injection** - When testability matters
3. **Preallocation** - When dealing with known-size collections
4. **Consumer-defined interfaces** - When designing for testability

### Apply Cautiously

These patterns have tradeoffs:

1. **strings.Builder** - Only for hot paths with many concatenations
2. **strconv over fmt** - Only for performance-critical code
3. **Slice copying** - Balance between safety and performance

---

## Modern Go Ecosystem (2025)

### Import Organization

Always organize imports in three groups separated by blank lines:

```go
// Standard library
import (
    "context"
    "fmt"
)

// Third-party packages
import (
    "github.com/gin-gonic/gin"
)

// Local packages
import (
    "yourproject/internal/handlers"
)
```

### Web Frameworks

| Framework | Adoption | Use Case |
|-----------|----------|----------|
| [Gin](https://github.com/gin-gonic/gin) | 48% | Most popular, full-featured |
| [Echo](https://github.com/labstack/echo) | 16% | Minimalist, high performance |
| [Fiber](https://github.com/gofiber/fiber) | 11% | Express-inspired, fast-growing |
| net/http (Go 1.22+) | - | Standard library with pattern routing |

### Testing Tools

| Tool | Adoption | Purpose |
|------|----------|---------|
| `testing` (stdlib) | Most common | Built-in testing package |
| [testify](https://github.com/stretchr/testify) | 27% | Assertions and mocking |
| [gomock](https://github.com/uber-go/mock) | 21% | Enterprise mock generation |

### Common Libraries

| Category | Library | Notes |
|----------|---------|-------|
| Logging | `log/slog` | Use for new projects (structured logging) |
| Configuration | [viper](https://github.com/spf13/viper) | Industry standard |
| ORM | [GORM](https://gorm.io/) | Most popular ORM |
| CLI (complex) | [cobra](https://github.com/spf13/cobra) | Feature-rich CLI framework |
| CLI (lightweight) | [urfave/cli](https://github.com/urfave/cli) | Simple CLI apps |
| Kubernetes | `k8s.io/client-go`, `controller-runtime` | Infrastructure tooling |

### Development Environment

| Tool | Preference |
|------|------------|
| GoLand IDE | 47% |
| VS Code + Go extension | Popular alternative |
| AI coding assistants | 70%+ developers use |

---

## Quick Reference Checklist

When refactoring Go code, check for:

- [ ] Deep nesting that can be flattened with early returns
- [ ] Variables declared far from usage
- [ ] Magic numbers without named constants
- [ ] Errors being discarded or not wrapped
- [ ] Goroutines without cancellation support
- [ ] Resources not cleaned up with defer
- [ ] Slices modified in place that shouldn't be
- [ ] Global variables that could be dependencies
- [ ] Inefficient string concatenation in loops
- [ ] Missing or inadequate documentation

---

## References

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- [Go Proverbs](https://go-proverbs.github.io/)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- [Standard Go Project Layout](https://github.com/golang-standards/project-layout)

---

*Last updated: 2026-01-10*
