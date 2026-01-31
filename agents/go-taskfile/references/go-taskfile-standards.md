# Idiomatic Go-Task Guide

A comprehensive guide to writing clean, maintainable, and idiomatic Taskfiles using go-task.

## Table of Contents

- [Introduction](#introduction)
- [File Naming and Structure](#file-naming-and-structure)
- [Style Guide](#style-guide)
- [Organization Best Practices](#organization-best-practices)
- [Variables and Environment](#variables-and-environment)
- [Task Dependencies](#task-dependencies)
- [Cross-Platform Compatibility](#cross-platform-compatibility)
- [Change Detection](#change-detection)
- [Advanced Features](#advanced-features)
- [CI/CD Integration](#cicd-integration)
- [Monorepo Management](#monorepo-management)
- [Common Patterns](#common-patterns)
- [References](#references)

## Introduction

Go-Task (Task) is a modern, cross-platform task runner and build tool written in Go. It serves as a lightweight alternative to Make, with YAML-based configuration that emphasizes simplicity, cross-platform compatibility, and developer experience.

## File Naming and Structure

### Standard File Names

Use one of these standard names for your Task files:
- `Taskfile.yaml` (recommended)
- `Taskfile.yml`

Using these standard names enables JSON Schema support in modern editors, providing autocompletion and validation.

### Recommended YAML Structure

Follow this conventional order for sections in your Taskfile:

```yaml
version: '3'

includes:
  # Included taskfiles

# Optional configurations
output: prefixed
silent: false

vars:
  # Global variables

env:
  # Global environment variables

tasks:
  # Your tasks
```

This structure keeps Taskfiles clean and familiar to other users.

## Style Guide

### Task Descriptions

Always add descriptions to tasks that will be used by team members:

```yaml
tasks:
  build:
    desc: Build the application binary
    cmds:
      - go build -o app
```

Tasks with descriptions appear in `task --list`, making them discoverable for onboarding.

### Default Task

Create a `default` task for the most common operation:

```yaml
tasks:
  default:
    desc: Build and test the application
    cmds:
      - task: build
      - task: test
```

Users can simply run `task` without arguments to execute the default task.

### Commit Your Taskfile

Always commit your Taskfile to version control and document common tasks in your README. This significantly reduces onboarding friction for new team members.

## Organization Best Practices

### Task Grouping with Includes

Use includes to organize related tasks and create namespaces:

```yaml
version: '3'

includes:
  docker:
    taskfile: ./docker/Taskfile.yml
    dir: ./docker
  lint:
    taskfile: ./lint/Taskfile.yml

tasks:
  build:
    desc: Build everything
    cmds:
      - task: docker:build
      - task: lint:all
```

Benefits:
- Prevents task name conflicts
- Groups related functionality
- Enables reusable task modules

### Subdirectory Taskfiles

Place Taskfile.yaml files in subdirectories to provide context-specific tasks:

```
project/
├── Taskfile.yaml          # Root tasks
├── backend/
│   └── Taskfile.yaml      # Backend-specific tasks
└── frontend/
    └── Taskfile.yaml      # Frontend-specific tasks
```

When you `cd` into a subdirectory and run `task`, it uses the local Taskfile, allowing context-appropriate task definitions.

### Variables in Includes

Pass variables to included Taskfiles for customization:

```yaml
includes:
  api:
    taskfile: ./services/Taskfile.yml
    vars:
      SERVICE_NAME: api
      PORT: 8080
  worker:
    taskfile: ./services/Taskfile.yml
    vars:
      SERVICE_NAME: worker
      PORT: 8081
```

This enables reusable Taskfiles that can be included multiple times with different configurations.

## Variables and Environment

### Variable Types

**Static Variables:**
```yaml
vars:
  APP_NAME: myapp
  VERSION: 1.0.0
```

**Dynamic Variables (Shell Commands):**
```yaml
vars:
  GIT_COMMIT:
    sh: git rev-parse --short HEAD
  BUILD_TIME:
    sh: date -u +"%Y-%m-%dT%H:%M:%SZ"
```

**Task-Specific Variables:**
```yaml
tasks:
  build:
    vars:
      BUILD_FLAGS: -ldflags "-X main.version={{.VERSION}}"
    cmds:
      - go build {{.BUILD_FLAGS}} -o {{.APP_NAME}}
```

### Variable Precedence

Variables follow this priority order (highest to lowest):
1. Command-line variables (`task VAR=value taskname`)
2. Task-specific variables
3. Global variables
4. Environment variables

**Principle:** The closer to the task definition, the higher the priority.

### Environment Variables

**Global Environment:**
```yaml
env:
  GO111MODULE: on
  CGO_ENABLED: 0
```

**Task-Specific Environment:**
```yaml
tasks:
  test:
    env:
      TEST_TIMEOUT: 10m
    cmds:
      - go test ./...
```

**DotEnv Files:**
```yaml
dotenv: ['.env', '{{.HOME}}/.env']

tasks:
  run:
    cmds:
      - ./app
```

### Special Variables

Task provides these built-in variables:

- `{{.CHECKSUM}}` - Checksum of sources (when using checksum method)
- `{{.TIMESTAMP}}` - Timestamp of sources (when using timestamp method)
- `{{.TASK}}` - Current task name
- `{{.ROOT_DIR}}` - Root directory of the Taskfile
- `{{.TASKFILE_DIR}}` - Directory of the current Taskfile
- `{{.USER_WORKING_DIR}}` - Directory from which task was called

## Task Dependencies

### Basic Dependencies

```yaml
tasks:
  build:
    deps: [clean, generate]
    cmds:
      - go build -o app

  clean:
    cmds:
      - rm -rf dist/

  generate:
    cmds:
      - go generate ./...
```

Dependencies run before the task's commands. By default, Task waits for all dependencies to complete.

### Dependency Execution Order

Dependencies can run in parallel for better performance:

```yaml
tasks:
  test:
    deps: [lint, unit-test, integration-test]
    cmds:
      - echo "All tests passed"

  lint:
    cmds:
      - golangci-lint run

  unit-test:
    cmds:
      - go test -short ./...

  integration-test:
    cmds:
      - go test -tags=integration ./...
```

All three tasks (`lint`, `unit-test`, `integration-test`) run in parallel.

### Fail Fast

Stop on the first dependency failure:

```yaml
tasks:
  deploy:
    deps: [build, test]
    vars:
      failfast: true
    cmds:
      - ./deploy.sh
```

Or use the `--failfast` CLI flag: `task --failfast deploy`

### Dependencies with Variables

Pass variables to dependencies:

```yaml
tasks:
  build-all:
    cmds:
      - task: build
        vars:
          PLATFORM: linux
      - task: build
        vars:
          PLATFORM: darwin

  build:
    cmds:
      - GOOS={{.PLATFORM}} go build -o app-{{.PLATFORM}}
```

## Cross-Platform Compatibility

### Avoid Shell-Specific Constructs

❌ **Bad:**
```yaml
tasks:
  clean:
    cmds:
      - rm -rf dist/    # Fails on Windows
```

✅ **Good:**
```yaml
tasks:
  clean:
    cmds:
      - rm -rf dist/
    platforms: [linux, darwin]

  clean:windows:
    cmds:
      - del /Q /S dist\
    platforms: [windows]
```

### Platform-Specific Tasks

Restrict tasks to specific platforms:

```yaml
tasks:
  build:linux:
    platforms: [linux]
    cmds:
      - GOOS=linux go build

  build:darwin:
    platforms: [darwin]
    cmds:
      - GOOS=darwin go build

  build:windows:
    platforms: [windows]
    cmds:
      - set GOOS=windows && go build
```

Valid values: Any valid GOOS/GOARCH combination (e.g., `linux`, `darwin`, `windows`, `linux/amd64`, `darwin/arm64`)

### Use Cross-Platform Tools

Prefer Go-based or cross-platform tools:
- Use `go run` instead of shell scripts
- Use task built-ins for common operations
- Consider tools like `sh` task runner for portable shell commands

### Declare Target Shell

```yaml
tasks:
  build:
    cmds:
      - go build
    shopt: ['-e', '-u', '-o', 'pipefail']  # Bash options
```

Or set globally:
```yaml
set: [errexit, nounset, pipefail]

tasks:
  # Tasks inherit shell options
```

## Change Detection

### Sources and Generates

Inform Task about source files and generated outputs to skip unnecessary runs:

```yaml
tasks:
  build:
    sources:
      - src/**/*.go
      - go.mod
      - go.sum
    generates:
      - dist/app
    cmds:
      - go build -o dist/app
```

Task tracks these files and only runs if sources changed or generates are missing.

### Checksum Method (Recommended)

```yaml
tasks:
  build:
    method: checksum
    sources:
      - '**/*.go'
    generates:
      - app
    cmds:
      - go build -o app
```

Checksum method:
- Takes checksums of dependencies
- Saves them to `.task/` directory
- Compares current vs saved checksums
- More reliable than timestamps
- Works across file copies and git operations

### Timestamp Method

```yaml
tasks:
  build:
    method: timestamp
    sources:
      - '**/*.go'
    generates:
      - app
    cmds:
      - go build -o app
```

Timestamp method:
- Compares file modification times
- Simpler but less reliable
- Issues with file copies, git operations

### Status Commands

Custom conditions for task freshness:

```yaml
tasks:
  build:
    cmds:
      - go build -o app
    status:
      - test -f app
      - test "$(find src -newer app | wc -l)" -eq 0
```

Task runs only if all status commands fail (non-zero exit).

## Advanced Features

### Watch Mode

Automatically rerun tasks when files change:

```yaml
tasks:
  dev:
    watch: true
    sources:
      - '**/*.go'
    cmds:
      - go run main.go
```

Run with: `task dev`

### Silent Tasks

Suppress output:

```yaml
tasks:
  setup:
    silent: true
    cmds:
      - echo "Setting up..."
      - mkdir -p dist/
```

Or per-command:
```yaml
tasks:
  build:
    cmds:
      - cmd: echo "Building..."
        silent: true
      - go build
```

### Ignore Errors

Continue on command failure:

```yaml
tasks:
  lint:
    cmds:
      - cmd: golangci-lint run
        ignore_error: true
      - staticcheck ./...
```

### Interactive Commands

For commands requiring user input:

```yaml
tasks:
  deploy:
    interactive: true
    cmds:
      - kubectl apply -f manifest.yaml
```

### Deferred Commands

Run cleanup commands even if task fails:

```yaml
tasks:
  test:
    cmds:
      - docker-compose up -d
      - defer: docker-compose down
      - go test ./...
```

The deferred command runs after the task completes or fails.

### Task Aliases

```yaml
tasks:
  build:
    aliases: [b, compile]
    cmds:
      - go build
```

Run with: `task b` or `task compile`

### Output Modes

Control how task output is displayed:

```yaml
output: prefixed  # Default, prefix each line with task name

# Or per-task:
tasks:
  logs:
    output: group
    cmds:
      - docker logs myapp
```

Options:
- `interleaved` - Output as it comes (default for single task)
- `group` - Buffer output, print when complete
- `prefixed` - Prefix each line with task name (default for multiple tasks)

## CI/CD Integration

### Pin Task Version

In CI, always pin the Task version:

```yaml
# GitHub Actions
- name: Install Task
  uses: arduino/setup-task@v1
  with:
    version: 3.x

# Or download specific version
- run: |
    curl -sL https://taskfile.dev/install.sh | sh -s -- -b /usr/local/bin v3.34.1
```

### Cache Task Binary

```yaml
# GitHub Actions
- name: Cache Task
  uses: actions/cache@v3
  with:
    path: ~/bin/task
    key: ${{ runner.os }}-task-v3.34.1
```

### Run Tasks in PR Checks

```yaml
# GitHub Actions
- name: Run checks
  run: |
    task lint
    task test
    task build
```

Benefits:
- Consistent local and CI environments
- Single source of truth for build commands
- Easier to test CI changes locally

### Environment-Specific Tasks

```yaml
tasks:
  test:ci:
    desc: Run tests in CI with coverage
    env:
      CI: true
    cmds:
      - go test -v -race -coverprofile=coverage.out ./...
      - go tool cover -html=coverage.out -o coverage.html

  test:
    desc: Run tests locally
    cmds:
      - go test -short ./...
```

## Monorepo Management

### Root Taskfile with Includes

```yaml
# Root Taskfile.yaml
version: '3'

includes:
  backend:
    taskfile: ./services/backend/Taskfile.yml
    dir: ./services/backend
  frontend:
    taskfile: ./services/frontend/Taskfile.yml
    dir: ./services/frontend
  shared:
    taskfile: ./shared/Taskfile.yml
    dir: ./shared

tasks:
  default:
    desc: Build all services
    cmds:
      - task: backend:build
      - task: frontend:build

  test:
    desc: Test all services
    deps: [backend:test, frontend:test, shared:test]
```

### Dynamic Includes

For discovering Taskfiles automatically:

```yaml
version: '3'

vars:
  SERVICES:
    sh: find services -name Taskfile.yml -exec dirname {} \;

tasks:
  test-all:
    cmds:
      - for: { var: SERVICES, split: '\n' }
        cmd: task -d {{.ITEM}} test
```

### Shared Variables Across Monorepo

```yaml
# Root Taskfile.yaml
version: '3'

vars:
  GO_VERSION: 1.21
  DOCKER_REGISTRY: myregistry.io

includes:
  backend:
    taskfile: ./backend/Taskfile.yml
    vars:
      SERVICE_NAME: backend
      GO_VERSION: '{{.GO_VERSION}}'
      REGISTRY: '{{.DOCKER_REGISTRY}}'
```

## Common Patterns

### Build Pattern

```yaml
tasks:
  build:
    desc: Build the application
    sources:
      - '**/*.go'
      - go.mod
      - go.sum
    generates:
      - dist/{{.APP_NAME}}
    vars:
      APP_NAME: myapp
      BUILD_FLAGS: -ldflags "-X main.version={{.VERSION}} -X main.commit={{.GIT_COMMIT}}"
      VERSION:
        sh: git describe --tags --always
      GIT_COMMIT:
        sh: git rev-parse --short HEAD
    cmds:
      - mkdir -p dist
      - go build {{.BUILD_FLAGS}} -o dist/{{.APP_NAME}}
```

### Test Pattern

```yaml
tasks:
  test:
    desc: Run all tests
    deps: [test:unit, test:integration]

  test:unit:
    desc: Run unit tests
    cmds:
      - go test -short -race ./...

  test:integration:
    desc: Run integration tests
    cmds:
      - go test -tags=integration ./...

  test:coverage:
    desc: Run tests with coverage
    cmds:
      - go test -race -coverprofile=coverage.out ./...
      - go tool cover -html=coverage.out -o coverage.html
```

### Docker Pattern

```yaml
tasks:
  docker:build:
    desc: Build Docker image
    vars:
      IMAGE_NAME: myapp
      IMAGE_TAG:
        sh: git rev-parse --short HEAD
    cmds:
      - docker build -t {{.IMAGE_NAME}}:{{.IMAGE_TAG}} .
      - docker tag {{.IMAGE_NAME}}:{{.IMAGE_TAG}} {{.IMAGE_NAME}}:latest

  docker:push:
    desc: Push Docker image
    deps: [docker:build]
    cmds:
      - docker push {{.IMAGE_NAME}}:{{.IMAGE_TAG}}
      - docker push {{.IMAGE_NAME}}:latest
```

### Cleanup Pattern

```yaml
tasks:
  clean:
    desc: Clean build artifacts
    cmds:
      - rm -rf dist/
      - rm -rf .task/
      - go clean -cache

  clean:all:
    desc: Deep clean including dependencies
    deps: [clean]
    cmds:
      - rm -rf vendor/
      - go clean -modcache
```

### Development Pattern

```yaml
tasks:
  dev:
    desc: Start development server with hot reload
    watch: true
    sources:
      - '**/*.go'
    cmds:
      - go run main.go

  dev:deps:
    desc: Start development dependencies (DB, Redis, etc.)
    cmds:
      - docker-compose -f docker-compose.dev.yml up -d

  dev:stop:
    desc: Stop development dependencies
    cmds:
      - docker-compose -f docker-compose.dev.yml down
```

### Lint Pattern

```yaml
tasks:
  lint:
    desc: Run all linters
    deps: [lint:go, lint:yaml, lint:docker]

  lint:go:
    desc: Lint Go code
    sources:
      - '**/*.go'
    cmds:
      - golangci-lint run ./...

  lint:yaml:
    desc: Lint YAML files
    sources:
      - '**/*.yml'
      - '**/*.yaml'
    cmds:
      - yamllint .

  lint:docker:
    desc: Lint Dockerfiles
    sources:
      - '**/Dockerfile*'
    cmds:
      - hadolint Dockerfile

  lint:fix:
    desc: Auto-fix linting issues
    cmds:
      - golangci-lint run --fix ./...
```

### Install Dependencies Pattern

```yaml
tasks:
  deps:
    desc: Install all dependencies
    deps: [deps:go, deps:tools]

  deps:go:
    desc: Install Go dependencies
    sources:
      - go.mod
      - go.sum
    cmds:
      - go mod download
      - go mod verify

  deps:tools:
    desc: Install development tools
    cmds:
      - go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
      - go install golang.org/x/tools/cmd/goimports@latest
```

### Release Pattern

```yaml
tasks:
  release:
    desc: Create a new release
    deps: [test, build]
    cmds:
      - task: version:bump
      - task: git:tag
      - task: docker:push
      - task: git:push

  version:bump:
    desc: Bump version
    vars:
      NEW_VERSION:
        sh: echo {{.VERSION}} | awk -F. '{print $1"."$2"."$3+1}'
    cmds:
      - echo "{{.NEW_VERSION}}" > VERSION

  git:tag:
    desc: Create git tag
    cmds:
      - git tag -a v{{.VERSION}} -m "Release v{{.VERSION}}"

  git:push:
    desc: Push changes and tags
    cmds:
      - git push origin main
      - git push origin --tags
```

## References

### Official Documentation
- [Task Official Documentation](https://taskfile.dev/docs/guide)
- [Taskfile Schema Reference](https://taskfile.dev/docs/reference/schema)
- [Task Style Guide](https://taskfile.dev/docs/styleguide)
- [Task Usage Guide](https://taskfile.dev/usage/)

### Articles and Tutorials
- [Why you should be using Go-Task](https://medium.com/@lorique/why-you-should-be-using-go-task-3cd30897f8d8)
- [Taskfiles for Go Developers](https://tutorialedge.net/golang/taskfiles-for-go-developers/)
- [Taskfile replaces make and Makefiles](https://tomharrisonjr.medium.com/taskfile-replaces-make-and-makefiles-abf564708f81)
- [Demystification of taskfile variables](https://medium.com/@TianchenW/demystification-of-taskfile-variables-29b751950393)
- [Streamlining Your Go Projects with Taskfile](https://www.codingexplorations.com/blog/taskfile-automation-for-go-development)

### Community Resources
- [Monorepo Discussion](https://github.com/go-task/task/discussions/1517)
- [Go-Task GitHub Repository](https://refft.com/en/go-task_task.html)
- [Applied Go: Just Make a Task](https://appliedgo.net/spotlight/just-make-a-task/)

### Go Packages
- [github.com/go-task/task/v3](https://pkg.go.dev/github.com/go-task/task/v3)
- [github.com/go-task/task/v3/taskfile](https://pkg.go.dev/github.com/go-task/task/v3/taskfile)

---

**Last Updated:** January 2026

This guide is a living document. Contributions and improvements are welcome!
