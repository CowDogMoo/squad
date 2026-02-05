# Taskfile Best Practices

A comprehensive guide to writing effective Taskfile.yaml files following
community best practices and the Taskfile philosophy. This document serves as
the knowledge base for the go-taskfile agent.

## Table of Contents

1. [Taskfile Philosophy](#taskfile-philosophy)
2. [File Structure](#file-structure)
3. [Variable Management](#variable-management)
4. [Task Design](#task-design)
5. [Command Execution](#command-execution)
6. [Dependencies and Ordering](#dependencies-and-ordering)
7. [Output and Logging](#output-and-logging)
8. [Security Considerations](#security-considerations)
9. [Error Handling](#error-handling)
10. [Includes and Composition](#includes-and-composition)
11. [Common Anti-Patterns](#common-anti-patterns)
12. [Severity Classification](#severity-classification)

---

## Taskfile Philosophy

### Core Principles

| Principle | Description |
|-----------|-------------|
| Simplicity | Tasks should be easy to understand at a glance |
| Reproducibility | Same inputs = same outputs across environments |
| Composability | Small, focused tasks that combine well |
| Documentation | Every task should be self-documenting |
| Idempotency | Running a task twice should not break anything |

### Key Benefits Over Make

- YAML syntax is more readable than Makefile syntax
- Native cross-platform support (no shell dependency issues)
- Built-in variable templating with Go templates
- Better handling of task dependencies and ordering
- Schema validation support

---

## File Structure

### Standard Layout

```yaml
---
# yaml-language-server: $schema=https://taskfile.dev/schema.json
version: "3"

vars:
  # Global variables at the top
  BINARY_NAME: myapp
  CMD_PATH: ./cmd/myapp

includes:
  # External taskfiles (optional)
  go:
    taskfile: ./build/Taskfile.go.yaml

tasks:
  default:
    desc: Default task (runs when no task specified)
    cmds:
      - task: build

  build:
    desc: Build the application
    cmds:
      - go build -o {{.BINARY_NAME}} {{.CMD_PATH}}
```

### Required Elements

| Element | Required | Purpose |
|---------|----------|---------|
| `version: "3"` | Yes | Taskfile schema version |
| Schema comment | Recommended | Enables IDE validation |
| `vars:` section | If using variables | Define all variables at top |
| `desc:` on tasks | Yes | Documents what task does |

### File Naming

- Main file: `Taskfile.yaml` (preferred) or `Taskfile.yml`
- Included files: `Taskfile.<purpose>.yaml` (e.g., `Taskfile.go.yaml`)
- Placed in repository root or clearly documented location

---

## Variable Management

### Variable Declaration

**Good - Variables at top:**

```yaml
vars:
  BINARY_NAME: myapp
  VERSION:
    sh: git describe --tags --always --dirty
  BUILD_FLAGS: -ldflags "-X main.version={{.VERSION}}"
```

**Bad - Hardcoded values in commands:**

```yaml
tasks:
  build:
    cmds:
      - go build -o myapp ./cmd/myapp  # Hardcoded!
```

### Variable Types

| Type | Syntax | Use Case |
|------|--------|----------|
| Static | `VAR: value` | Constants |
| Dynamic | `VAR: { sh: command }` | Runtime values |
| Environment | `VAR: '{{.ENV_VAR \| default "fallback"}}'` | User overrides |

### Environment Variable Handling

**Good - Default with override:**

```yaml
vars:
  PROVIDER: '{{.PROVIDER | default "local"}}'
  API_KEY: '{{.API_KEY | default ""}}'
  _API_KEY_FLAG: '{{if .API_KEY}}--api-key {{.API_KEY}}{{end}}'
```

**Anti-pattern - Hardcoded secrets:**

```yaml
vars:
  API_KEY: sk-secret-key-hardcoded  # NEVER do this
```

### Conditional Flags

Use underscore-prefixed internal variables for conditional command construction:

```yaml
vars:
  BASE_URL: '{{.BASE_URL | default ""}}'
  _BASE_URL_FLAG: '{{if .BASE_URL}}--base-url {{.BASE_URL}}{{end}}'

tasks:
  run:
    cmds:
      - myapp {{._BASE_URL_FLAG}} --other-flag
```

---

## Task Design

### Task Anatomy

```yaml
tasks:
  build:
    desc: "Build the application binary"
    summary: |
      Compiles the Go application with version information embedded.

      The binary is output to ./bin/{{.BINARY_NAME}}.
    vars:
      OUTPUT_DIR: ./bin
    deps:
      - generate
    preconditions:
      - sh: test -f go.mod
        msg: "go.mod not found - run from repository root"
    cmds:
      - mkdir -p {{.OUTPUT_DIR}}
      - go build -o {{.OUTPUT_DIR}}/{{.BINARY_NAME}} {{.CMD_PATH}}
    sources:
      - "**/*.go"
    generates:
      - "{{.OUTPUT_DIR}}/{{.BINARY_NAME}}"
```

### Required Task Properties

| Property | When Required | Purpose |
|----------|---------------|---------|
| `desc:` | Always | One-line description for `task --list` |
| `summary:` | Complex tasks | Detailed explanation |
| `preconditions:` | When assumptions exist | Fail fast with clear message |
| `cmds:` | Always | The actual commands |

### Task Naming Conventions

| Pattern | Use Case | Example |
|---------|----------|---------|
| `verb` | Simple actions | `build`, `test`, `clean` |
| `noun:verb` | Namespaced actions | `docker:build`, `go:test` |
| `run:name` | Running agents/tools | `run:go-review`, `run:lint` |
| `check:name` | Validation tasks | `check:fmt`, `check:lint` |

**Good naming:**

```yaml
tasks:
  build:           # Simple
  test:            # Simple
  docker:build:    # Namespaced
  run:go-review:   # Agent runner
  check:fmt:       # Validation
```

**Bad naming:**

```yaml
tasks:
  BuildApp:        # PascalCase - not idiomatic
  DO_THE_BUILD:    # SCREAMING_CASE - not idiomatic
  b:               # Too abbreviated
```

---

## Command Execution

### Multi-line Commands

**Good - Pipe for readability:**

```yaml
tasks:
  build:
    cmds:
      - |
        go build \
          -ldflags "{{.BUILD_FLAGS}}" \
          -o {{.BINARY_NAME}} \
          {{.CMD_PATH}}
```

**Good - Separate commands for clarity:**

```yaml
tasks:
  setup:
    cmds:
      - mkdir -p ./bin
      - go mod download
      - go generate ./...
```

### Command Chaining

| Operator | Meaning | Use Case |
|----------|---------|----------|
| `&&` | Continue if success | Dependent commands |
| `;` | Always continue | Independent commands |
| `\|` | Pipe output | Data transformation |

**Good:**

```yaml
cmds:
  - go build ./... && go test ./...  # Test only if build succeeds
```

### Silent Mode

Use `silent: true` for tasks with intentional output:

```yaml
tasks:
  run:
    desc: Run the application
    silent: true  # Only show application output
    cmds:
      - ./bin/myapp
```

### Ignore Errors

Use sparingly, only when errors are truly acceptable:

```yaml
cmds:
  - cmd: rm -rf ./tmp
    ignore_error: true  # OK if directory doesn't exist
```

---

## Dependencies and Ordering

### Task Dependencies

```yaml
tasks:
  build:
    deps:
      - generate
      - lint
    cmds:
      - go build ./...

  generate:
    cmds:
      - go generate ./...

  lint:
    cmds:
      - golangci-lint run
```

### Dependency Properties

| Property | Behavior |
|----------|----------|
| `deps:` | Run in parallel before task |
| `task: X` in cmds | Run sequentially as part of task |
| `preconditions:` | Check before running anything |

### Sequential vs Parallel

**Parallel (default for deps):**

```yaml
deps:
  - lint      # These run in parallel
  - generate
```

**Sequential:**

```yaml
cmds:
  - task: generate  # This runs first
  - task: build     # This waits for generate
```

---

## Output and Logging

### Output Control

| Setting | Effect |
|---------|--------|
| `silent: true` | Suppress task name prefix |
| `set: [pipefail]` | Fail on pipe errors |
| `shopt: [globstar]` | Enable ** glob |

### Echo Control

```yaml
tasks:
  verbose:
    cmds:
      - echo "Starting build..."  # Informational
      - go build ./...
      - echo "Build complete."

  quiet:
    silent: true
    cmds:
      - go build ./...  # No extra output
```

### Progress Indicators

```yaml
tasks:
  build:
    cmds:
      - echo "Building {{.BINARY_NAME}}..."
      - go build -o {{.BINARY_NAME}} {{.CMD_PATH}}
      - echo "Build complete: {{.BINARY_NAME}}"
```

---

## Security Considerations

### Secret Handling

**Good - Environment variable with no default:**

```yaml
vars:
  API_KEY: '{{.API_KEY | default ""}}'

tasks:
  deploy:
    preconditions:
      - sh: test -n "{{.API_KEY}}"
        msg: "API_KEY environment variable is required"
    cmds:
      - deploy --api-key {{.API_KEY}}
```

**Good - External secret tool:**

```yaml
vars:
  API_KEY:
    sh: op item get "MyService" --fields password 2>/dev/null || echo ""
```

**Bad - Hardcoded secrets:**

```yaml
vars:
  API_KEY: sk-abc123secretkey  # CRITICAL: Never commit secrets
```

### Input Validation

```yaml
tasks:
  run:
    vars:
      PROMPT: '{{.PROMPT | default ""}}'
    preconditions:
      - sh: test -n "{{.PROMPT}}"
        msg: "PROMPT is required. Usage: task run PROMPT='...'"
    cmds:
      - echo "Running with prompt: {{.PROMPT}}"
```

### Path Safety

**Good - Use variables for paths:**

```yaml
vars:
  OUTPUT_DIR: ./bin

tasks:
  clean:
    cmds:
      - rm -rf {{.OUTPUT_DIR}}  # Safe - controlled path
```

**Dangerous - User-controlled paths:**

```yaml
tasks:
  clean:
    cmds:
      - rm -rf {{.USER_PATH}}  # DANGEROUS if USER_PATH is "../.."
```

---

## Error Handling

### Preconditions

```yaml
tasks:
  build:
    preconditions:
      - sh: command -v go >/dev/null
        msg: "Go is not installed"
      - sh: test -f go.mod
        msg: "go.mod not found - run from repository root"
      - sh: test -d ./cmd
        msg: "cmd/ directory not found"
```

### Status Checks

Use `status:` for idempotent tasks:

```yaml
tasks:
  generate:
    cmds:
      - go generate ./...
    sources:
      - "**/*.go"
    generates:
      - "**/mock_*.go"
    status:
      - test -f ./internal/mock_service.go
```

### Fail Fast

Default behavior is fail-fast. Override only when necessary:

```yaml
tasks:
  cleanup:
    cmds:
      - cmd: rm -rf ./tmp
        ignore_error: true
      - cmd: docker rm -f test-container
        ignore_error: true
```

---

## Includes and Composition

### External Taskfiles

```yaml
includes:
  go:
    taskfile: ./build/Taskfile.go.yaml
    vars:
      BINARY_NAME: myapp
  docker:
    taskfile: https://example.com/Taskfile.docker.yaml
```

### Remote Includes

```yaml
includes:
  go:
    taskfile: "https://raw.githubusercontent.com/org/taskfiles/main/go/Taskfile.yaml"
    vars:
      BINARY_NAME: myapp
      CMD_PATH: ./cmd/myapp
```

### Variable Passing

```yaml
includes:
  go:
    taskfile: ./Taskfile.go.yaml
    vars:
      # Pass parent vars to included taskfile
      BINARY_NAME: '{{.BINARY_NAME}}'
      VERSION: '{{.VERSION}}'
```

---

## Common Anti-Patterns

### CRITICAL

| Anti-Pattern | Issue | Fix |
|--------------|-------|-----|
| Hardcoded secrets | Security breach risk | Use environment variables |
| No version field | Schema undefined | Add `version: "3"` |
| Unquoted template vars | YAML parsing errors | Quote: `'{{.VAR}}'` |

### HIGH

| Anti-Pattern | Issue | Fix |
|--------------|-------|-----|
| Missing `desc:` | Unusable with `task --list` | Add description |
| Hardcoded paths | Not portable | Use variables |
| Missing preconditions | Confusing failures | Add input validation |
| Complex inline scripts | Hard to maintain | Extract to shell script |

### MEDIUM

| Anti-Pattern | Issue | Fix |
|--------------|-------|-----|
| Inconsistent naming | Hard to discover tasks | Use conventions |
| Duplicate commands | Maintenance burden | Extract to shared task |
| No `silent:` on runners | Noisy output | Add `silent: true` |
| Missing `summary:` | Complex tasks undocumented | Add detailed summary |

### LOW

| Anti-Pattern | Issue | Fix |
|--------------|-------|-----|
| No schema comment | No IDE validation | Add schema comment |
| Unused variables | Clutter | Remove them |
| Overly long commands | Hard to read | Use multi-line or script |

---

## Severity Classification

### CRITICAL

Issues that affect security or cause immediate failures:

- Hardcoded secrets or credentials
- Missing `version:` field
- Syntax errors in YAML or templates
- Unvalidated user input in dangerous commands

### HIGH

Issues that affect reliability or maintainability:

- Missing `desc:` on tasks
- Hardcoded values that should be variables
- Missing preconditions for required inputs
- Commands that fail silently

### MEDIUM

Best practice violations:

- Inconsistent task naming
- Missing `summary:` on complex tasks
- Duplicate command patterns
- No `silent:` on runner tasks

### LOW

Minor improvements:

- Missing schema comment
- Overly long single-line commands
- Unused variables
- Style inconsistencies

### INFO

Suggestions for optimization:

- Using `sources:` and `generates:` for caching
- Parallel task execution opportunities
- Remote include consolidation

---

## Quick Reference Checklist

### Before Committing

- [ ] `version: "3"` present
- [ ] Schema comment at top
- [ ] All tasks have `desc:`
- [ ] No hardcoded secrets
- [ ] Variables for all paths/values
- [ ] Preconditions for required inputs
- [ ] Consistent task naming
- [ ] Complex commands documented

### Common Commands

```bash
# List all tasks
task --list

# Run with variable override
task build VERSION=1.0.0

# Dry run
task --dry

# Force run (ignore status)
task --force build
```

---

## References

- [Taskfile Official Docs](https://taskfile.dev)
- [Taskfile Schema](https://taskfile.dev/schema.json)
- [Taskfile GitHub](https://github.com/go-task/task)
- [Taskfile Best Practices (Community)](https://taskfile.dev/usage/#best-practices)

---

*Last updated: 2026-02-04*
