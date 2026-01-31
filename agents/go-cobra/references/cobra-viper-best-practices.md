# Cobra & Viper Best Practices

A comprehensive reference guide for building idiomatic CLI applications in Go using Cobra and Viper. This document serves as the knowledge base for the go-cobra pattern.

## Table of Contents

1. [Command Design Philosophy](#command-design-philosophy)
2. [Project Structure](#project-structure)
3. [Command Implementation](#command-implementation)
4. [Flag Management](#flag-management)
5. [Viper Configuration](#viper-configuration)
6. [Cobra + Viper Integration](#cobra--viper-integration)
7. [Error Handling](#error-handling)
8. [Testing Strategies](#testing-strategies)
9. [Shell Completions](#shell-completions)
10. [Production Patterns](#production-patterns)
11. [Anti-Patterns](#anti-patterns)
12. [Severity Classification](#severity-classification)

---

## Command Design Philosophy

### Natural Command Syntax

Commands should read like natural sentences. Follow the pattern:

```
APPNAME VERB NOUN --ADJECTIVE
APPNAME COMMAND ARG --FLAG
```

**Good Examples:**
```bash
git clone URL --depth 1
kubectl get pods --namespace kube-system
docker run IMAGE --detach
myapp deploy production --dry-run
```

**Bad Examples:**
```bash
myapp --deploy production           # Flag instead of command
myapp production-deploy             # Hyphenated compound
myapp do_deployment --env=prod      # Underscores, verbose
```

### Command Hierarchy

| Level | Purpose | Example |
|-------|---------|---------|
| Root | Application entry point | `myapp` |
| Command | Primary action | `myapp serve` |
| Subcommand | Action refinement | `myapp config set` |
| Arguments | Required input | `myapp deploy prod` |
| Flags | Optional modifiers | `--verbose`, `--port 8080` |

### Naming Conventions

- **Commands**: Short, verb-based (`serve`, `run`, `get`, `set`)
- **Arguments**: Noun-based, clear purpose
- **Flags**: Descriptive, kebab-case (`--log-level`, `--dry-run`)
- **Aliases**: Provide short forms (`-v` for `--verbose`)

---

## Project Structure

### Recommended Layout

```
myapp/
├── main.go              # Minimal entry point
├── cmd/
│   ├── root.go          # Root command and global config
│   ├── serve.go         # Subcommand: serve
│   ├── migrate.go       # Subcommand: migrate
│   └── version.go       # Subcommand: version
├── internal/
│   └── app/             # Business logic (testable, CLI-independent)
└── config/
    └── config.go        # Configuration structs and loading
```

### Critical Rules

| Rule | Severity | Rationale |
|------|----------|-----------|
| Minimal main.go | HIGH | Entry point only initializes and executes |
| One command per file | MEDIUM | Maintainability and clarity |
| Business logic in internal/ | HIGH | Separation of concerns, testability |
| Config structs separate | MEDIUM | Reusable, type-safe configuration |

### Minimal main.go

**Good:**
```go
package main

import (
    "os"

    "myapp/cmd"
)

func main() {
    if err := cmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

**Bad:**
```go
package main

import (
    "fmt"
    "github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{...}  // Don't define commands in main.go

func main() {
    // Don't put business logic here
    if err := rootCmd.Execute(); err != nil {
        fmt.Println(err)  // Don't print errors in main
        os.Exit(1)
    }
}
```

---

## Command Implementation

### Command Structure

```go
var exampleCmd = &cobra.Command{
    Use:   "example [flags] <arg>",
    Short: "One-line description (shown in help lists)",
    Long: `Extended description with examples and details.
           Shown when running 'myapp example --help'.`,
    Example: `  myapp example foo
  myapp example bar --verbose`,
    Args:    cobra.ExactArgs(1),
    Aliases: []string{"ex", "eg"},
    RunE:    runExample,
}
```

### Critical Checks

| Check | Severity | Rationale |
|-------|----------|-----------|
| Use RunE not Run | HIGH | Proper error propagation |
| Args validation | MEDIUM | User feedback before execution |
| Short description | MEDIUM | Help readability |
| Example usage | LOW | User guidance |

### RunE vs Run

**Good - RunE:**
```go
RunE: func(cmd *cobra.Command, args []string) error {
    if err := doSomething(); err != nil {
        return fmt.Errorf("failed to process: %w", err)
    }
    return nil
}
```

**Bad - Run:**
```go
Run: func(cmd *cobra.Command, args []string) {
    doSomething()  // Errors are swallowed
}
```

### Argument Validation

**Built-in validators:**
```go
Args: cobra.NoArgs              // No arguments allowed
Args: cobra.ExactArgs(2)        // Exactly 2 arguments
Args: cobra.MinimumNArgs(1)     // At least 1 argument
Args: cobra.MaximumNArgs(3)     // At most 3 arguments
Args: cobra.RangeArgs(1, 3)     // Between 1 and 3 arguments
Args: cobra.OnlyValidArgs       // Only from ValidArgs list
```

**Custom validation:**
```go
Args: func(cmd *cobra.Command, args []string) error {
    if len(args) < 1 {
        return errors.New("requires at least one argument")
    }
    if !isValidInput(args[0]) {
        return fmt.Errorf("invalid input: %q", args[0])
    }
    return nil
}
```

### Command Lifecycle Hooks

Execution order:
1. `PersistentPreRun` (inherited by children)
2. `PreRun`
3. `Run` / `RunE`
4. `PostRun`
5. `PersistentPostRun` (inherited by children)

**Good - Use PersistentPreRunE for initialization:**
```go
var rootCmd = &cobra.Command{
    Use:   "myapp",
    Short: "My application",
    PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
        if err := initConfig(); err != nil {
            return fmt.Errorf("init config: %w", err)
        }
        return nil
    },
}
```

---

## Flag Management

### Persistent vs Local Flags

```go
func init() {
    // Persistent: Available to this command AND all subcommands
    rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
        "config file (default: $HOME/.myapp.yaml)")
    rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false,
        "verbose output")

    // Local: Only available to this specific command
    serveCmd.Flags().IntP("port", "p", 8080, "port to listen on")
    serveCmd.Flags().Duration("timeout", 30*time.Second, "request timeout")
}
```

### Flag Types Reference

| Type | Method | Example |
|------|--------|---------|
| String | `StringVarP` | `--name value` |
| Int | `IntVarP` | `--port 8080` |
| Bool | `BoolVarP` | `--verbose` |
| Duration | `DurationVarP` | `--timeout 30s` |
| StringSlice | `StringSliceVarP` | `--tag a,b --tag c` |
| StringArray | `StringArrayVarP` | `--tag a,b --tag c` (no comma split) |
| Count | `CountVarP` | `-v`, `-vv`, `-vvv` |

### Required Flags

```go
serveCmd.Flags().StringP("database", "d", "", "database connection string")
serveCmd.MarkFlagRequired("database")
```

### Flag Groups

```go
// Flags that must be used together
cmd.MarkFlagsRequiredTogether("username", "password")

// Flags that cannot be used together
cmd.MarkFlagsMutuallyExclusive("json", "yaml", "table")

// At least one of these flags must be provided
cmd.MarkFlagsOneRequired("config", "inline-config")
```

### Count Flags (Verbosity Pattern)

```go
var verbosity int

func init() {
    rootCmd.PersistentFlags().CountVarP(&verbosity, "verbose", "v",
        "increase verbosity (-v, -vv, -vvv)")
}

// Usage: myapp -v (1), myapp -vv (2), myapp -vvv (3)
```

### Slice Flags

```go
var tags []string

func init() {
    // StringSlice: comma-separated OR repeated flags
    cmd.Flags().StringSliceVar(&tags, "tag", []string{},
        "tags (can be repeated or comma-separated)")
    // myapp --tag=a,b --tag=c -> ["a", "b", "c"]

    // StringArray: only repeated flags (no comma splitting)
    cmd.Flags().StringArrayVar(&tags, "tag", []string{},
        "tags (can be repeated)")
    // myapp --tag=a,b --tag=c -> ["a,b", "c"]
}
```

---

## Viper Configuration

### Avoid the Global Instance

| Pattern | Severity | Status |
|---------|----------|--------|
| Use dedicated Viper instance | HIGH | Recommended |
| Global viper singleton | MEDIUM | Avoid |

**Good - Explicit instance:**
```go
func NewConfig() (*Config, error) {
    v := viper.New()
    v.SetConfigName("config")
    v.AddConfigPath(".")
    return loadConfig(v)
}
```

**Bad - Global singleton:**
```go
func init() {
    viper.SetConfigName("config")  // Hard to test
}
```

### Configuration Precedence

Viper respects this hierarchy (highest to lowest priority):

1. Explicit `Set()` calls
2. Command-line flags
3. Environment variables
4. Configuration files
5. Key/value stores (etcd, Consul)
6. Default values

### Type-Safe Configuration Structs

```go
type Config struct {
    Server   ServerConfig   `mapstructure:"server"`
    Database DatabaseConfig `mapstructure:"database"`
    Log      LogConfig      `mapstructure:"log"`
}

type ServerConfig struct {
    Host         string        `mapstructure:"host"`
    Port         int           `mapstructure:"port"`
    ReadTimeout  time.Duration `mapstructure:"read_timeout"`
    WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type DatabaseConfig struct {
    Host     string `mapstructure:"host"`
    Port     int    `mapstructure:"port"`
    Name     string `mapstructure:"name"`
    User     string `mapstructure:"user"`
    Password string `mapstructure:"password"` // Load from env!
}

type LogConfig struct {
    Level  string `mapstructure:"level"`
    Format string `mapstructure:"format"`
}

func LoadConfig(v *viper.Viper) (*Config, error) {
    var cfg Config
    if err := v.Unmarshal(&cfg); err != nil {
        return nil, fmt.Errorf("unmarshal config: %w", err)
    }
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("validate config: %w", err)
    }
    return &cfg, nil
}
```

### Environment Variables

```go
func setupViper(v *viper.Viper) {
    // Prefix all env vars: MYAPP_SERVER_PORT, MYAPP_DATABASE_HOST
    v.SetEnvPrefix("MYAPP")

    // Replace dots with underscores: server.port -> MYAPP_SERVER_PORT
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

    // Automatically read matching env vars
    v.AutomaticEnv()
}
```

### Multi-Location Config Search

```go
func setupConfigPaths(v *viper.Viper) {
    v.SetConfigName("config")
    v.SetConfigType("yaml")

    // Search order (first found wins)
    v.AddConfigPath(".")                    // Current directory
    v.AddConfigPath("$HOME/.myapp")         // User home
    v.AddConfigPath("/etc/myapp")           // System-wide

    // Optional: Allow explicit config file override
    if cfgFile != "" {
        v.SetConfigFile(cfgFile)
    }
}
```

### Defaults

```go
func setDefaults(v *viper.Viper) {
    v.SetDefault("server.host", "0.0.0.0")
    v.SetDefault("server.port", 8080)
    v.SetDefault("server.read_timeout", "30s")
    v.SetDefault("server.write_timeout", "30s")
    v.SetDefault("log.level", "info")
    v.SetDefault("log.format", "json")
}
```

### Configuration Validation

```go
func (c *Config) Validate() error {
    if c.Server.Port < 1 || c.Server.Port > 65535 {
        return fmt.Errorf("invalid port: %d (must be 1-65535)", c.Server.Port)
    }
    if c.Database.Host == "" {
        return errors.New("database.host is required")
    }
    if c.Database.Password == "" {
        return errors.New("database.password is required (set MYAPP_DATABASE_PASSWORD)")
    }
    return nil
}
```

---

## Cobra + Viper Integration

### The Correct Pattern

**Key insight**: Viper is the single source of truth. Don't read flag values directly - bind them to Viper and read from Viper.

```go
var (
    cfgFile string
    v       *viper.Viper
)

func init() {
    v = viper.New()
    cobra.OnInitialize(initConfig)

    // Define flags
    rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
        "config file (default: $HOME/.myapp.yaml)")
    rootCmd.PersistentFlags().String("log-level", "info",
        "log level (debug, info, warn, error)")

    // Bind flags to Viper
    v.BindPFlag("log.level", rootCmd.PersistentFlags().Lookup("log-level"))
}

func initConfig() {
    if cfgFile != "" {
        v.SetConfigFile(cfgFile)
    } else {
        home, _ := os.UserHomeDir()
        v.AddConfigPath(home)
        v.AddConfigPath(".")
        v.SetConfigName(".myapp")
        v.SetConfigType("yaml")
    }

    v.SetEnvPrefix("MYAPP")
    v.AutomaticEnv()

    if err := v.ReadInConfig(); err == nil {
        fmt.Fprintln(os.Stderr, "Using config file:", v.ConfigFileUsed())
    }
}
```

### Reading Values in Commands

**Good - Read from Viper:**
```go
func runServe(cmd *cobra.Command, args []string) error {
    // Read from Viper (respects precedence: flag > env > file > default)
    logLevel := v.GetString("log.level")
    port := v.GetInt("server.port")

    return startServer(port, logLevel)
}
```

**Bad - Read directly from flags:**
```go
func runServe(cmd *cobra.Command, args []string) error {
    // Bypasses precedence - ignores env vars and config file
    port, _ := cmd.Flags().GetInt("port")  // Avoid!
    return startServer(port)
}
```

---

## Error Handling

### Return Wrapped Errors

```go
func runMigrate(cmd *cobra.Command, args []string) error {
    db, err := connectDB()
    if err != nil {
        return fmt.Errorf("connect to database: %w", err)
    }
    defer db.Close()

    if err := db.Migrate(); err != nil {
        return fmt.Errorf("run migrations: %w", err)
    }
    return nil
}
```

### Custom Error Prefix

```go
func init() {
    rootCmd.SetErrPrefix("Error:")
    // Output: "Error: connect to database: connection refused"
}
```

### Actionable Error Messages

**Good:**
```go
return fmt.Errorf("config file not found at %s (create one or use --config)", path)
```

**Bad:**
```go
return errors.New("config not found")
```

---

## Testing Strategies

### Extract Business Logic

Separate CLI concerns from business logic:

```go
// cmd/serve.go - CLI layer
func runServe(cmd *cobra.Command, args []string) error {
    port := v.GetInt("server.port")
    timeout := v.GetDuration("server.timeout")
    return app.StartServer(port, timeout) // Delegate to testable code
}

// internal/app/server.go - Business logic (testable)
func StartServer(port int, timeout time.Duration) error {
    // Implementation
}
```

### Dependency Injection

```go
// Testable app struct
type App struct {
    Config *Config
    Out    io.Writer
    Err    io.Writer
}

func NewApp(cfg *Config) *App {
    return &App{
        Config: cfg,
        Out:    os.Stdout,
        Err:    os.Stderr,
    }
}

// In tests, inject test doubles
func TestApp(t *testing.T) {
    out := &bytes.Buffer{}
    app := &App{
        Config: testConfig(),
        Out:    out,
    }
    // Test and assert on out.String()
}
```

### Execute Commands in Tests

```go
func executeCommand(root *cobra.Command, args ...string) (string, error) {
    buf := new(bytes.Buffer)
    root.SetOut(buf)
    root.SetErr(buf)
    root.SetArgs(args)

    err := root.Execute()
    return buf.String(), err
}

func TestServeCommand(t *testing.T) {
    cmd := NewRootCmd()
    output, err := executeCommand(cmd, "serve", "--port", "9090")

    require.NoError(t, err)
    assert.Contains(t, output, "listening on :9090")
}
```

### Table-Driven Tests

```go
func TestCommands(t *testing.T) {
    tests := []struct {
        name    string
        args    []string
        wantErr bool
        wantOut string
    }{
        {
            name:    "serve with valid port",
            args:    []string{"serve", "--port", "8080"},
            wantErr: false,
        },
        {
            name:    "serve with invalid port",
            args:    []string{"serve", "--port", "99999"},
            wantErr: true,
        },
        {
            name:    "version",
            args:    []string{"version"},
            wantOut: "v1.0.0",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cmd := NewRootCmd()
            out, err := executeCommand(cmd, tt.args...)

            if tt.wantErr {
                require.Error(t, err)
            } else {
                require.NoError(t, err)
            }
            if tt.wantOut != "" {
                assert.Contains(t, out, tt.wantOut)
            }
        })
    }
}
```

### Reset Viper Between Tests

```go
func TestWithConfig(t *testing.T) {
    // Create fresh Viper instance per test
    v := viper.New()
    v.Set("server.port", 9090)

    cmd := NewRootCmdWithViper(v)
    // ...
}
```

---

## Shell Completions

### Add a Completion Command

```go
var completionCmd = &cobra.Command{
    Use:   "completion [bash|zsh|fish|powershell]",
    Short: "Generate shell completion script",
    Long: `Generate shell completion script for the specified shell.

To load completions:

Bash (Linux):
  $ myapp completion bash > /etc/bash_completion.d/myapp

Bash (macOS):
  $ myapp completion bash > /usr/local/etc/bash_completion.d/myapp

Zsh:
  $ myapp completion zsh > "${fpath[1]}/_myapp"

Fish:
  $ myapp completion fish > ~/.config/fish/completions/myapp.fish

PowerShell:
  PS> myapp completion powershell | Out-String | Invoke-Expression
`,
    ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
    Args:                  cobra.ExactArgs(1),
    DisableFlagsInUseLine: true,
    RunE: func(cmd *cobra.Command, args []string) error {
        switch args[0] {
        case "bash":
            return cmd.Root().GenBashCompletion(os.Stdout)
        case "zsh":
            return cmd.Root().GenZshCompletion(os.Stdout)
        case "fish":
            return cmd.Root().GenFishCompletion(os.Stdout, true)
        case "powershell":
            return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
        default:
            return fmt.Errorf("unsupported shell: %s", args[0])
        }
    },
}
```

### Dynamic Completions

```go
var deployCmd = &cobra.Command{
    Use:   "deploy [environment]",
    Short: "Deploy to an environment",
    ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) (
        []string, cobra.ShellCompDirective) {
        if len(args) != 0 {
            return nil, cobra.ShellCompDirectiveNoFileComp
        }
        // Fetch environments dynamically
        envs, err := fetchEnvironments()
        if err != nil {
            return nil, cobra.ShellCompDirectiveError
        }
        return envs, cobra.ShellCompDirectiveNoFileComp
    },
}
```

### Flag Completions

```go
func init() {
    deployCmd.Flags().StringP("region", "r", "", "AWS region")
    deployCmd.RegisterFlagCompletionFunc("region",
        func(cmd *cobra.Command, args []string, toComplete string) (
            []string, cobra.ShellCompDirective) {
            return []string{
                "us-east-1\tN. Virginia",
                "us-west-2\tOregon",
                "eu-west-1\tIreland",
            }, cobra.ShellCompDirectiveNoFileComp
        })
}
```

---

## Production Patterns

### Version Information

```go
var (
    version = "dev"
    commit  = "unknown"
    date    = "unknown"
)

var versionCmd = &cobra.Command{
    Use:   "version",
    Short: "Print version information",
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Printf("Version: %s\nCommit: %s\nBuilt: %s\n",
            version, commit, date)
    },
}

// Build with:
// go build -ldflags "-X main.version=v1.0.0 -X main.commit=$(git rev-parse HEAD)"
```

### Graceful Shutdown

```go
func runServe(cmd *cobra.Command, args []string) error {
    ctx, cancel := signal.NotifyContext(cmd.Context(),
        syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    srv := &http.Server{Addr: ":8080"}

    go func() {
        <-ctx.Done()
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        srv.Shutdown(shutdownCtx)
    }()

    return srv.ListenAndServe()
}
```

### Live Configuration Reload

```go
func watchConfig(v *viper.Viper, onReload func()) {
    v.WatchConfig()
    v.OnConfigChange(func(e fsnotify.Event) {
        log.Printf("Config reloaded: %s", e.Name)
        onReload()
    })
}
```

### Secrets Management

Never store secrets in config files:

```yaml
# config.yaml (safe to commit)
database:
  host: localhost
  port: 5432
  name: myapp
```

```bash
# Environment variables (set at runtime)
export MYAPP_DATABASE_USER=admin
export MYAPP_DATABASE_PASSWORD=secret
```

### Structured Logging Integration

```go
func initLogger(v *viper.Viper) *slog.Logger {
    level := slog.LevelInfo
    switch v.GetString("log.level") {
    case "debug":
        level = slog.LevelDebug
    case "warn":
        level = slog.LevelWarn
    case "error":
        level = slog.LevelError
    }

    opts := &slog.HandlerOptions{Level: level}

    var handler slog.Handler
    if v.GetString("log.format") == "json" {
        handler = slog.NewJSONHandler(os.Stderr, opts)
    } else {
        handler = slog.NewTextHandler(os.Stderr, opts)
    }

    return slog.New(handler)
}
```

---

## Anti-Patterns

### Common Mistakes to Avoid

| Anti-Pattern | Severity | Why It's Bad |
|--------------|----------|--------------|
| Using Run instead of RunE | HIGH | Errors are silently ignored |
| Reading flags directly in commands | HIGH | Bypasses config precedence |
| Global Viper singleton | MEDIUM | Hard to test |
| Business logic in cmd/ | HIGH | Tight coupling, hard to test |
| Complex main.go | MEDIUM | Entry point should be minimal |
| Goroutines in init() | HIGH | Unpredictable startup |
| Ignoring errors from BindPFlag | MEDIUM | Silent configuration issues |
| Hardcoded config paths | MEDIUM | Inflexible deployment |
| Missing shell completions | LOW | Reduced UX |
| Missing version command | LOW | Deployment debugging difficulty |

### Anti-Pattern Examples

**Bad - Complex main.go:**
```go
func main() {
    initLogging()      // Don't do setup here
    loadConfig()       // Don't do setup here
    connectDatabase()  // Definitely don't do this
    rootCmd.Execute()
}
```

**Good - Minimal main.go:**
```go
func main() {
    if err := cmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

**Bad - Flags bypass config:**
```go
func runServe(cmd *cobra.Command, args []string) error {
    port, _ := cmd.Flags().GetInt("port")  // Ignores env/config
}
```

**Good - Use Viper:**
```go
func runServe(cmd *cobra.Command, args []string) error {
    port := v.GetInt("server.port")  // Respects precedence
}
```

---

## Severity Classification

### CRITICAL

Issues that affect correctness, security, or cause crashes:
- Using Run instead of RunE with important error handling
- Missing argument validation that could cause panics
- Secrets stored in config files
- No graceful shutdown handling

### HIGH

Significant issues affecting reliability or maintainability:
- Reading flags directly instead of using Viper
- Business logic in cmd/ package
- Global Viper singleton
- Missing error context/wrapping
- No configuration validation

### MEDIUM

Best practice violations:
- Missing shell completions
- Missing version command
- Complex main.go
- Non-standard project structure
- Missing flag descriptions

### LOW

Minor improvements:
- Missing command aliases
- Missing Long descriptions
- Missing Example in commands
- Naming convention tweaks

### INFO

Suggestions for optimization:
- Live config reload
- Dynamic completions
- Structured logging integration
- Performance optimizations

---

## Quick Reference Checklist

### Before Shipping

- [ ] main.go is minimal (only Execute call)
- [ ] All commands use RunE not Run
- [ ] Flags bound to Viper
- [ ] Commands read from Viper not flags
- [ ] Configuration struct with validation
- [ ] Environment variable support
- [ ] Shell completion command
- [ ] Version command with build info
- [ ] Graceful shutdown handling
- [ ] Secrets loaded from environment
- [ ] Error messages are actionable
- [ ] Tests use fresh Viper instances

### Common Patterns Summary

| Pattern | Location | Purpose |
|---------|----------|---------|
| Root command | cmd/root.go | Global config, PersistentPreRunE |
| Subcommand | cmd/[name].go | One command per file |
| Config struct | config/config.go | Type-safe configuration |
| Business logic | internal/app/ | Testable, CLI-independent |
| Flag binding | init() | Bind flags to Viper |
| Config loading | PersistentPreRunE | Before any command runs |

---

## References

- [Cobra GitHub](https://github.com/spf13/cobra)
- [Cobra Documentation](https://cobra.dev/)
- [Viper GitHub](https://github.com/spf13/viper)
- [Cobra User Guide](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md)
- [Shell Completions Guide](https://cobra.dev/docs/how-to-guides/shell-completion/)

---

*Last updated: 2026-01-13*
