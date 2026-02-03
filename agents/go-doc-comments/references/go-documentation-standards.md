# Go Documentation Standards

A comprehensive guide to Go documentation comments following the official "Go Doc Comments" specification and community best practices. This document serves as the knowledge base for the go-doc-comments pattern.

## Table of Contents

1. [Core Principles](#core-principles)
2. [Syntax by Declaration Type](#syntax-by-declaration-type)
3. [Modern Doc Comment Features](#modern-doc-comment-features)
4. [What to Document](#what-to-document)
5. [Common Mistakes](#common-mistakes)
6. [Quality Checklist](#quality-checklist)

---

## Core Principles

### The Golden Rule

Doc comments appear **immediately before** top-level declarations with **no intervening blank lines**. All exported (capitalized) names must have doc comments.

### Philosophy

| Principle | Description |
|-----------|-------------|
| Complete sentences | Start with the declared name |
| Explain what | Not how it works internally |
| User focus | Focus on user needs, not implementation |
| Searchable | Clear, explicit, searchable text |
| No redundancy | Avoid "ProcessData processes the data" |
| Add value | Provide info beyond the function signature |

### Line Length

All comment lines should stay within **80 characters** for readability.

---

## Syntax by Declaration Type

### Package Comments

```go
// Package regexp implements regular expression search.
//
// The syntax of the regular expressions accepted is the same
// general syntax used by Perl, Python, and other languages.
package regexp
```

**Rules:**

- Start with "Package [name]"
- Only include in ONE file of multi-file packages
- Directly adjacent to package clause (no blank line)
- For commands, describe program behavior

### Functions and Methods

```go
// Join concatenates the elements of paths to create a single path.
// Any empty strings are ignored.
func Join(paths ...string) string

// Open reports whether the file is currently open for reading.
func (f *File) Open() bool
```

**Rules:**

- Start with function/method name
- For boolean returns: "reports whether [condition]" (omit "or not")
- Reference parameters/results without special syntax
- Describe return values and side effects

**Boolean Functions:**

```go
// IsValid reports whether the configuration is valid.
func (c *Config) IsValid() bool

// Contains reports whether s contains the substring substr.
func Contains(s, substr string) bool
```

### Types

```go
// A Request represents an HTTP request received by a server
// or to be sent by a client.
//
// The field semantics differ slightly between client and server
// usage. All exported fields are safe for concurrent use.
type Request struct {
    Method string // HTTP method (GET, POST, PUT, etc.)
    URL    *url.URL
}
```

**Rules:**

- Use "A [Type] represents..." or "[Type] is..."
- Document concurrency safety if relevant
- Explain zero value behavior if non-obvious
- Document exported fields (in type comment or per-field)

### Constants and Variables

**Grouped constants:**

```go
const (
    // MaxSize is the maximum allowed file size in bytes.
    MaxSize = 1024 * 1024

    StatusOK    = 200 // Request succeeded
    StatusError = 500 // Server error occurred
)
```

**Ungrouped:**

```go
// ErrNotFound is returned when a resource cannot be located.
var ErrNotFound = errors.New("not found")
```

**Rules:**

- Grouped: single doc comment + end-of-line comments
- Ungrouped: full doc comments with complete sentences

---

## Modern Doc Comment Features

### Headings (Go 1.19+)

Use `#` (with space) on single unindented line, surrounded by blank lines:

```go
// Package strings provides UTF-8 string manipulation.
//
// # Numeric Conversions
//
// The most common conversions are...
```

### Doc Links (to Go identifiers)

```go
// Parse returns a [Time] value or returns an error if parsing fails.
// See [time.RFC3339] for the expected format.
// Use [*Time.Format] to convert back to strings.
```

**Syntax:**

| Pattern | Description |
|---------|-------------|
| `[Name]` | Local identifier |
| `[Name.Method]` | Method of local type |
| `[pkg.Name]` | Identifier in another package |
| `[*Type]` | Pointer to type |

### URL Links

```go
// See [RFC 7159] for details.
//
// [RFC 7159]: https://tools.ietf.org/html/rfc7159
```

### Lists

**Bullet lists** (indent 2 spaces before marker, 4 for continuation):

```go
// Features:
//   - Fast performance
//   - Memory efficient
//   - Thread safe
```

**Numbered lists:**

```go
// Usage:
//  1. Initialize the client
//  2. Configure options
//  3. Call Execute
```

**Rules:**

- Use `-` for bullets (gofmt normalizes)
- No nested lists (not supported)
- Only paragraphs allowed in lists

### Code Blocks

Indent lines that aren't list markers:

```go
// Example usage:
//
// client := NewClient()
// err := client.Connect("localhost:8080")
// if err != nil {
//  log.Fatal(err)
// }
```

---

## What to Document

### Concurrency Safety

Document when non-obvious or when stronger guarantees than default:

```go
// Cache provides thread-safe access to cached data.
// All methods are safe for concurrent use by multiple goroutines.
type Cache struct { ... }

// Buffer provides efficient byte slice manipulation.
// A Buffer must not be copied after first use.
type Buffer struct { ... }
```

### Error Values

```go
// ErrTimeout is returned when an operation exceeds its deadline.
// Callers can use [errors.Is] to check for this error.
var ErrTimeout = errors.New("operation timeout")
```

### Cleanup Requirements

```go
// Close closes the file and releases associated resources.
// The client must call Close when done to prevent resource leaks.
func (f *File) Close() error
```

### Context Behavior

Document when non-standard:

```go
// Execute runs the command with the given context.
// Unlike most context-aware functions, Execute may return
// errors other than ctx.Err() even when the context is cancelled.
func (c *Command) Execute(ctx context.Context) error
```

### Parameter Constraints

```go
// NewClient creates a client with the given options.
// The timeout value must be positive; zero means no timeout.
// The retryCount controls automatic retries on network errors.
func NewClient(timeout time.Duration, retryCount int) *Client
```

### Return Values

```go
// Find searches for the pattern in text and returns the index
// of the first match, or -1 if no match is found.
func Find(text, pattern string) int
```

### Side Effects

```go
// Shutdown gracefully stops the server and waits for all
// active connections to complete. It blocks until shutdown
// is complete or the context is cancelled.
func (s *Server) Shutdown(ctx context.Context) error
```

---

## Common Mistakes

### Redundant Comments

**Bad:**

```go
// Process processes the data
func Process(data []byte) error
```

**Good:**

```go
// Process validates and transforms the input data according
// to the configured rules, returning an error if validation fails.
func Process(data []byte) error
```

### Implementation Details

**Bad:**

```go
// GetUser queries the database using a prepared statement
func GetUser(id int) (*User, error)
```

**Good:**

```go
// GetUser returns the user with the given ID or returns
// an error if the user doesn't exist.
func GetUser(id int) (*User, error)
```

### Missing Declared Name

**Bad:**

```go
// Returns the current time in UTC format
func Now() time.Time
```

**Good:**

```go
// Now returns the current time in UTC.
func Now() time.Time
```

### Improper Indentation

**Bad (breaks list):**

```go
// Uses:
//   - Feature one. This wraps
// to the next line  // ← Breaks list!
```

**Good:**

```go
// Uses:
//   - Feature one. This wraps
//     to the next line with proper indentation
```

### Blank Line Between Comment and Declaration

**Bad:**

```go
// GetUser returns the user with the given ID.

func GetUser(id int) (*User, error)
```

**Good:**

```go
// GetUser returns the user with the given ID.
func GetUser(id int) (*User, error)
```

---

## Special Syntax

### Deprecation

```go
// Deprecated: Use [NewClient] instead. This function doesn't
// properly handle context cancellation and will be removed in v2.0.
func OldClient() *Client
```

Start paragraph with `Deprecated:` - tools warn on usage.

### TODO/BUG/FIXME

```go
// TODO(username): refactor to use generics
// BUG(username): doesn't handle Unicode properly
```

Format: `MARKER(uid): description`

### Directives

```go
// Op represents a regex operator.
//
//go:generate stringer -type Op
type Op uint8
```

**Rules:**

- gofmt moves directives after blank line at end of comment
- Not part of godoc output
- Common directives: `//go:generate`, `//go:embed`, `//go:build`

---

## Tools and Automation

### gofmt

Automatically formats doc comments:

- Normalizes list indentation (converts `*`, `+`, `•` to `-`)
- Converts backticks and quotes to Unicode
- Removes common indentation prefix
- Adds blank lines around code blocks
- Moves link definitions and directives to end of comment

```bash
gofmt -w .
```

### go doc

View formatted documentation in terminal:

```bash
go doc pkgname           # Package documentation
go doc pkgname.TypeName  # Type documentation
go doc pkgname.FuncName  # Function documentation
```

### godoc-lint

Linter for doc comment consistency (validates against Go Doc Comments spec):

```bash
go install golang.org/x/tools/cmd/godoc-lint@latest
godoc-lint ./...
```

### pkg.go.dev

Online documentation rendering at https://pkg.go.dev - automatically extracts and renders doc comments for public packages.

---

## Signal Boosting Comments

From Google Style Guide: add clarifying comments for unusual patterns that might confuse readers.

**Example - unusual condition:**

```go
if err == nil {
    // if NO error, proceed with cleanup
    return cleanup()
}
```

**When to add signal boosting:**

- Unusual control flow (checking for success instead of failure)
- Non-obvious side effects
- Intentionally empty blocks
- Counter-intuitive logic

---

## Quality Checklist

### Before Submitting

- [ ] All exported names have doc comments
- [ ] Comments start with the declared name
- [ ] Complete sentences with proper punctuation
- [ ] No blank line between comment and declaration
- [ ] Lines within 80 characters
- [ ] Modern doc features used when beneficial (links, lists)
- [ ] Concurrency safety documented when relevant
- [ ] Error conditions documented
- [ ] Cleanup requirements documented
- [ ] gofmt compatible

### Common Patterns

| Declaration | Pattern |
|-------------|---------|
| Function returning value | "[Name] returns..." |
| Function returning bool | "[Name] reports whether..." |
| Function with side effects | "[Name] [action]s..." |
| Type | "A [Type] represents..." or "[Type] is..." |
| Error variable | "[Name] is returned when..." |
| Constant | "[Name] is the..." |

---

## References

- [Go Doc Comments Specification](https://go.dev/doc/comment)
- [Effective Go - Commentary](https://go.dev/doc/effective_go#commentary)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments#doc-comments)
- [Go Blog - Godoc](https://go.dev/blog/godoc)
- [Google Go Style Guide](https://google.github.io/styleguide/go/)
- [Google Go Best Practices](https://google.github.io/styleguide/go/best-practices)

---

*Last updated: 2026-01-10*
