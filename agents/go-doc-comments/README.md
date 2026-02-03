# go-doc-comments

Generate or improve Go documentation comments following the official Go Doc Comments specification.

## Overview

This agent specializes in creating high-quality Go documentation comments that follow:

- Official [Go Doc Comments specification](https://go.dev/doc/comment)
- Go community conventions and idioms
- Modern doc comment features (Go 1.19+)
- 80-character line length limit

## Capabilities

- **Add missing documentation** for all exported declarations
- **Improve existing comments** to follow Go conventions
- **Use modern features** like headings, links, lists, and code blocks
- **Document concurrency**, error conditions, and cleanup requirements
- **Preserve code logic** - only modifies comments

## Usage

```bash
# Document a single file
squad run go-doc-comments path/to/file.go

# Document multiple files
squad run go-doc-comments path/to/*.go

# Document entire package
squad run go-doc-comments path/to/package/
```

## What Gets Documented

The agent documents all exported (capitalized) declarations:

- **Packages** - Overview and usage
- **Functions** - Behavior, parameters, returns
- **Types** - Purpose and zero value behavior
- **Methods** - Receiver methods
- **Constants** - Purpose and value
- **Variables** - Purpose and usage

## Documentation Standards

The agent follows these key principles:

1. **Complete sentences** starting with the declared name
2. **User focus** - explain what, not how
3. **Concurrency safety** - document thread safety when relevant
4. **Error conditions** - document when errors are returned
5. **Cleanup requirements** - document resource management
6. **Modern syntax** - uses Go 1.19+ features appropriately

## Examples

### Before

```go
type Config struct {
    Timeout int
}

func NewConfig() *Config {
    return &Config{Timeout: 30}
}
```

### After

```go
// Config holds connection configuration settings.
// A Config must not be copied after first use.
type Config struct {
    // Timeout specifies the maximum wait time in seconds.
    Timeout int
}

// NewConfig creates a Config with sensible defaults.
// The default timeout is 30 seconds.
func NewConfig() *Config {
    return &Config{Timeout: 30}
}
```

## Reference

The agent uses a comprehensive knowledge base covering:

- Package, function, type, constant, and variable documentation
- Modern doc comment features (headings, links, lists, code blocks)
- Concurrency, error, and cleanup documentation patterns
- Common mistakes and anti-patterns
- Quality checklist

See [references/go-documentation-standards.md](references/go-documentation-standards.md) for the complete reference.

## Related Agents

- **go-review** - Review Go code for best practices
- **go-refactor** - Refactor Go code to idiomatic patterns
- **go-tests** - Generate Go tests
