# Go Security: Comprehensive Guide to Identifying and Patching Vulnerabilities

## Table of Contents
1. [Introduction](#introduction)
2. [Security Tools and Analysis](#security-tools-and-analysis)
3. [Common Vulnerabilities](#common-vulnerabilities)
4. [Memory Safety and Overflow Vulnerabilities](#memory-safety-and-overflow-vulnerabilities)
5. [gRPC Security](#grpc-security)
6. [Cryptography Best Practices](#cryptography-best-practices)
7. [Concurrency and Race Conditions](#concurrency-and-race-conditions)
8. [Dependency Security and Supply Chain](#dependency-security-and-supply-chain)
9. [Security Checklist](#security-checklist)

---

## Introduction

Go is designed with security in mind, featuring built-in memory safety, garbage collection, and strong typing. However, vulnerabilities can still occur through improper use of language features, dependencies, or external integrations. In 2025, there have been 9 vulnerabilities in Go with an average score of 6.5 out of ten.

### Key Security Principles
- Keep Go updated (security fixes issued to the two most recent major releases)
- Use official security tools (govulncheck, gosec, go vet)
- Validate and sanitize all user input
- Minimize dependency tree
- Never execute untrusted code during build/fetch
- Follow secure coding practices for your domain

---

## Security Tools and Analysis

### 1. govulncheck - Official Vulnerability Scanner

**govulncheck** is backed by the Go vulnerability database and analyzes your codebase to surface only vulnerabilities that actually affect you, based on which functions in your code are transitively calling vulnerable functions.

**Installation:**
```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
```

**Usage:**
```bash
# Scan your code
govulncheck ./...

# Scan specific packages
govulncheck ./pkg/...
```

**CI/CD Integration:**
- GitHub Action available on GitHub Marketplace
- Can be integrated into any CI/CD pipeline
- VS Code Go extension checks dependencies and surfaces vulnerabilities

**Resources:**
- [Go Vulnerability Management](https://go.dev/doc/security/vuln/)
- [Govulncheck Tutorial](https://go.dev/doc/tutorial/govulncheck)
- [Vulnerability Scanning in Go With Govulncheck](https://semaphore.io/blog/govulncheck)

### 2. gosec - Static Analysis Security Tool

**gosec** scans Go code for security problems by inspecting the AST (Abstract Syntax Tree) and matching against security rules.

**Installation:**
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

**Usage:**
```bash
# Scan entire project
gosec ./...

# Output as JSON
gosec -fmt=json -out=results.json ./...

# Scan with specific rules
gosec -include=G401,G501 ./...
```

**Key Features:**
- Maps issues to CWE (Common Weakness Enumeration)
- Multiple output formats (text, JSON, SARIF)
- Configurable rules and severity filtering
- Supports exclusion of directories/packages

**Common Security Checks:**
- Hard-coded credentials (G101, G102)
- SQL injection vulnerabilities (G201, G202)
- Weak cryptographic algorithms (G401, G501, G502)
- Insecure usage of os/exec (G204)
- File permissions and path traversal (G301-G306)
- Use of unsafe package (G103)

**Limitations:**
- Only static analysis (cannot detect runtime vulnerabilities)
- Should be complemented with dynamic analysis and manual reviews

**Resources:**
- [gosec GitHub Repository](https://github.com/securego/gosec)
- [Secure Your Go Code: A Deep Dive into Gosec](https://www.security.land/secure-your-go-code-a-deep-dive-into-gosec-for-static-analysis/)
- [Find security issues in Go code using gosec](https://opensource.com/article/20/9/gosec)

### 3. go vet - Built-in Code Analysis

**go vet** examines Go source code and reports suspicious constructs that might lead to runtime problems.

**Usage:**
```bash
# Check current package
go vet

# Check specific packages
go vet ./pkg/...

# Check with race detector
go vet -race ./...
```

**What it catches:**
- Unreachable code
- Unused variables
- Common mistakes with goroutines
- Printf-family functions with wrong arguments
- Suspicious constructs

### 4. Additional Security Tools

**staticcheck:**
```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
staticcheck ./...
```

**golangci-lint** (aggregates multiple linters):
```bash
golangci-lint run
```

**osv-scanner** (Google's vulnerability scanner):
```bash
go install github.com/google/osv-scanner/cmd/osv-scanner@latest
osv-scanner --lockfile=go.mod
```

---

## Common Vulnerabilities

### 1. Injection Attacks

#### SQL Injection

**Vulnerability:**
Occurs when untrusted data is sent to an interpreter as part of a command or query.

**Vulnerable Code:**
```go
// NEVER DO THIS
query := fmt.Sprintf("SELECT * FROM users WHERE username='%s'", username)
rows, err := db.Query(query)
```

**Secure Code:**
```go
// Use parameterized queries
query := "SELECT * FROM users WHERE username=$1"
rows, err := db.Query(query, username)

// Or use prepared statements
stmt, err := db.Prepare("SELECT * FROM users WHERE username=?")
if err != nil {
    return err
}
defer stmt.Close()
rows, err := stmt.Query(username)
```

**Key Points:**
- SQL parameter values as function arguments prevent injection
- Use `database/sql` package's parameterized query methods
- Placeholder format varies by driver (?, $1, etc.)
- Parameterized queries guarantee proper escaping

**Resources:**
- [Official: Avoiding SQL injection risk](https://go.dev/doc/database/sql-injection)
- [Golang SQL Injection Guide](https://www.stackhawk.com/blog/golang-sql-injection-guide-examples-and-prevention/)
- [SQL injection in Go](https://docs.fluidattacks.com/criteria/fixes/go/146/)

#### Command Injection

**Vulnerability:**
Untrusted input passed directly to system shell allows arbitrary command execution.

**Vulnerable Code:**
```go
// NEVER DO THIS
cmd := exec.Command("/bin/sh", "-c", "ls "+userInput)

// OR THIS
cmd := exec.Command("bash", "-c", "myCommand "+userInput)
```

**Secure Code:**
```go
// Use parameterization - separate arguments
cmd := exec.Command("/path/to/myCommand", "myArg1", inputValue)

// If shell is absolutely necessary, use allowlist
allowedCommands := map[string]bool{
    "list":   true,
    "status": true,
}
if !allowedCommands[userInput] {
    return errors.New("invalid command")
}
cmd := exec.Command("/bin/sh", "-c", userInput)

// Better: Avoid shell invocation when possible
cmd := exec.Command("/bin/ls", "-la", filepath.Clean(userPath))
```

**Prevention Strategies:**
1. Use parameterization instead of string concatenation
2. Avoid shell invocation when possible (Go lacks proper shell-escaping)
3. Use allowlists for permitted commands
4. Validate and sanitize all user inputs

**Resources:**
- [Command Injection in Go](https://semgrep.dev/docs/cheat-sheets/go-command-injection)
- [Golang Command Injection: Examples and Prevention](https://www.stackhawk.com/blog/golang-command-injection-examples-and-prevention/)
- [Understanding command injection vulnerabilities in Go](https://snyk.io/blog/understanding-go-command-injection-vulnerabilities/)

### 2. Cross-Site Scripting (XSS)

**Vulnerability:**
User-generated content executed as JavaScript or HTML in browsers.

**Vulnerable Code:**
```go
// NEVER DO THIS - using text/template
import "text/template"

tmpl := template.Must(template.New("page").Parse(`
    <div>{{.UserContent}}</div>
`))
tmpl.Execute(w, data) // No escaping!

// OR THIS - writing directly to response
io.WriteString(w, "<div>"+userContent+"</div>")
```

**Secure Code:**
```go
// Use html/template for automatic escaping
import "html/template"

tmpl := template.Must(template.New("page").Parse(`
    <div>{{.UserContent}}</div>
`))
tmpl.Execute(w, data) // Automatically escaped!
```

**Contextual Auto-Escaping:**
```go
// html/template understands context
template := `
    <script>
        var data = {{.JSData}};  // JavaScript context
    </script>
    <div class="{{.CSSClass}}">  // CSS context
        <a href="{{.URL}}">{{.Text}}</a>  // URL and HTML context
    </div>
`
// Each value is escaped appropriately for its context
```

**Dangerous Types to AVOID:**
```go
// These bypass escaping - BAN THEM
template.HTML      // Unescaped HTML
template.HTMLAttr  // Unescaped HTML attributes
template.JS        // Unescaped JavaScript
template.URL       // Unescaped URLs
template.CSS       // Unescaped CSS
```

**Best Practices:**
1. **Always use `html/template`, NEVER `text/template`** for HTML output
2. Ban dangerous bypass types in code reviews
3. Set appropriate Content-Type headers
4. Never write user content directly to response without template engine

**Resources:**
- [Cross-site scripting: Explanation and prevention with Go](https://developers.redhat.com/articles/2022/06/28/cross-site-scripting-explanation-and-prevention-go)
- [Understanding XSS Protection in Go's html/template](https://leapcell.io/blog/understanding-xss-protection-in-go-s-html-template)
- [Preventing Cross-Site Scripting (XSS) Attacks in Go](https://medium.com/@mgm06bm/preventing-cross-site-scripting-xss-attacks-in-go-a-step-by-step-guide-6b8e3cf01d9f)

### 3. Path Traversal

**Vulnerability:**
Attackers control file system access through manipulated file paths.

**Vulnerable Code:**
```go
// NEVER DO THIS
filepath := "/var/www/files/" + userInput
content, err := ioutil.ReadFile(filepath)
// userInput could be "../../etc/passwd"
```

**Secure Code (Go 1.20+):**
```go
import "path/filepath"

// Method 1: Use filepath.Join and filepath.Clean
basePath := "/var/www/files"
requestedFile := filepath.Clean(userInput)
fullPath := filepath.Join(basePath, requestedFile)

// Verify the path stays within basePath
if !strings.HasPrefix(fullPath, filepath.Clean(basePath)+string(os.PathSeparator)) {
    return errors.New("invalid path")
}

// Method 2: Use filepath.IsLocal (Go 1.20+)
if !filepath.IsLocal(userInput) {
    return errors.New("path escapes directory")
}
fullPath := filepath.Join(basePath, userInput)

// Read the file
content, err := os.ReadFile(fullPath)
```

**Secure Code (Go 1.24+):**
```go
// Method 3: Use os.Root (Go 1.24+) - Recommended
root := os.Root("/var/www/files")
content, err := root.ReadFile(userInput)
// Automatically prevents traversal outside /var/www/files
```

**Critical Vulnerability (CVE-2022-41722):**
On Windows, `filepath.Clean` could transform invalid paths like "a/../c:/b" into valid path "c:\b", enabling directory traversal. Fixed in Go 1.19.6 and 1.20.1.

**Best Practices:**
1. Always validate and clean user-supplied file paths
2. Use `filepath.Join` and `filepath.Clean` together
3. Verify final path stays within expected directory
4. Consider `filepath.IsLocal` (Go 1.20+) or `os.Root` (Go 1.24+)
5. Never trust user input for file operations

**Resources:**
- [Golang Path Traversal Guide](https://www.stackhawk.com/blog/golang-path-traversal-guide-examples-and-prevention/)
- [CVE-2022-41722](https://github.com/golang/go/issues/57274)
- [Traversal-resistant file APIs](https://go.dev/blog/osroot)

---

## Memory Safety and Overflow Vulnerabilities

### 1. Buffer Overflow

**Overview:**
Go is generally memory-safe with built-in bounds checking, preventing traditional buffer overflow vulnerabilities common in C/C++. However, vulnerabilities can still occur.

**Go's Protection Mechanisms:**
- Bounds-checked arrays and slices
- No dangling pointers
- Garbage collection
- Automatic memory management

**Known CVEs:**
- **CVE-2022-24675**: Buffer overflow via large arguments in WASM function invocation (Go < 1.16.9, 1.17.2)
- **CVE-2021-38297**: Decode stack overflow via large PEM data in encoding/pem (Go < 1.17.9, 1.18.1)

**Bounds Checking in Go:**
```go
// Go automatically checks bounds
arr := []int{1, 2, 3, 4, 5}
value := arr[10] // panic: runtime error: index out of range

// Bounds check before access
if index < len(arr) && index >= 0 {
    value := arr[index]
}
```

**Bounds Check Elimination (BCE):**
Go compiler can optimize away redundant bounds checks when it can prove safety.

```go
// Compiler eliminates subsequent checks after first check
if len(slice) >= 4 {
    _ = slice[3] // bounds check
    a := slice[0] // no check needed
    b := slice[1] // no check needed
    c := slice[2] // no check needed
    d := slice[3] // no check needed
}

// Debug BCE optimizations
// go build -gcflags="-d=ssa/check_bce/debug=1"
```

**Resources:**
- [Go: Memory Safety with Bounds Check](https://medium.com/a-journey-with-go/go-memory-safety-with-bounds-check-1397bef748b5)
- [Bounds Check Elimination In Go](https://www.ardanlabs.com/blog/2018/04/bounds-check-elimination-in-go.html)
- [CISA: Eliminating Buffer Overflow Vulnerabilities](https://www.cisa.gov/resources-tools/resources/secure-design-alert-eliminating-buffer-overflow-vulnerabilities)

### 2. Integer Overflow and Underflow

**Vulnerability:**
Go doesn't check for integer overflow, similar to C, C++, and Java. Operations wrap around silently.

**Behavior:**
- Unsigned integers: operations computed modulo 2^n upon overflow
- Signed integers: computed using two's complement arithmetic, truncated to bit width
- No exception raised on overflow

**Vulnerable Code:**
```go
// Silent overflow
var x uint8 = 255
x = x + 1 // x becomes 0, no error

// Dangerous in size calculations
size := userInput1 * userInput2
buffer := make([]byte, size) // If overflow, buffer could be too small

// Offset calculation vulnerability
offset := baseAddr + userOffset // Could wrap around
ptr := unsafe.Pointer(offset) // Dangerous!
```

**Security Implications:**
1. Programs making system calls may pass corrupted data structures to kernel
2. Marshaled data sent over network may be silently corrupted
3. Programs using `unsafe` are vulnerable to same bugs as C
4. Bounds-checking on slices mitigates some but not all harmful effects

**Secure Code:**
```go
// Check before arithmetic operations
func SafeAdd(a, b uint32) (uint32, error) {
    if a > math.MaxUint32 - b {
        return 0, errors.New("integer overflow")
    }
    return a + b, nil
}

func SafeMultiply(a, b uint32) (uint32, error) {
    if a != 0 && b > math.MaxUint32/a {
        return 0, errors.New("integer overflow")
    }
    return a * b, nil
}

// Use math/big for arbitrary-precision arithmetic
import "math/big"

bigA := big.NewInt(9223372036854775807)
bigB := big.NewInt(9223372036854775807)
result := new(big.Int).Add(bigA, bigB) // No overflow
```

**Known Vulnerability (CVE):**
Integer overflow in crypto/elliptic allows DoS via specially crafted scalar input > 32 bytes to P256().ScalarMult or P256().ScalarBaseMult.

**Resources:**
- [Integer Overflows in Golang](https://blog.rene.sh/blog/2020/06/22/int-overflow/)
- [Understanding Integer Overflow](https://www.infosecinstitute.com/resources/secure-coding/what-is-is-integer-overflow-and-underflow/)
- [Go Integer Overflow Vulnerability](https://www.cybersecurity-help.cz/vulnerabilities/64269/)

### 3. The `unsafe` Package

**Risk Level: CRITICAL**

The `unsafe` package allows programs to defeat the type system and bypass Go's memory safety guarantees.

**Dangers:**
- Defeats memory safety
- Can read/write arbitrary memory
- No compiler enforcement of safety
- Combined with user data, can enable attackers to break memory safety

**Common Use Cases (and risks):**
```go
import "unsafe"

// Pointer arithmetic (DANGEROUS)
ptr := unsafe.Pointer(&someVar)
ptr = unsafe.Pointer(uintptr(ptr) + offset) // offset from user = vulnerability

// Type conversion bypassing type system (DANGEROUS)
var f float64 = 3.14
bits := *(*uint64)(unsafe.Pointer(&f))

// Size of types
size := unsafe.Sizeof(someStruct) // Safer usage
```

**CGO Risks:**
When combining `unsafe` with CGO:

1. **Memory Leaks**: Passing unsafe.Pointer to C functions can leak memory
2. **Garbage Collection Issues**: Go may GC or move pointers actively used by C
3. **Pointer Passing Rules**:
   - Cannot store Go pointer to unpinned memory in C memory
   - Go functions called by C must ensure Go memory remains pinned
4. **Manual Memory Management**: C.CString() allocates C memory that must be manually freed

**Safer CGO Alternative:**
```go
import "runtime/cgo"

// Use cgo.Handle instead of unsafe.Pointer
h := cgo.NewHandle(goValue)
defer h.Delete()
// Pass h as C pointer equivalent
```

**Data Races Breaking Memory Safety:**
Data races can break Go's memory safety guarantees even without `unsafe`.

**Best Practices:**
1. Avoid `unsafe` unless absolutely necessary
2. Thoroughly audit all `unsafe` usage
3. Never use `unsafe` with user-controlled data
4. Use `cgo.Handle` for Go-C interop
5. Run with race detector during testing
6. Document and justify every use of `unsafe`

**Resources:**
- [unsafe package documentation](https://pkg.go.dev/unsafe)
- [Golang data races to break memory safety](https://blog.stalkr.net/2015/04/golang-data-races-to-break-memory-safety.html)
- [Exploitation Exercise with unsafe.Pointer](https://dev.to/jlauinger/exploitation-exercise-with-unsafe-pointer-in-go-information-leak-part-1-1kga)
- [CGO Memory Issues](https://github.com/golang/go/issues/40636)

---

## gRPC Security

### 1. Common gRPC Vulnerabilities

#### Plaintext Communication (CRITICAL)

**Vulnerability:**
gRPC servers by default allow plaintext communication, exposing data to eavesdropping and tampering.

**Vulnerable Code:**
```go
// CLIENT - INSECURE
conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())

// SERVER - INSECURE
lis, err := net.Listen("tcp", ":50051")
grpcServer := grpc.NewServer()
grpcServer.Serve(lis)
```

**Secure Code:**
```go
// CLIENT - TLS
creds, err := credentials.NewClientTLSFromFile("server.crt", "")
if err != nil {
    log.Fatal(err)
}
conn, err := grpc.Dial("localhost:50051",
    grpc.WithTransportCredentials(creds))

// SERVER - TLS
creds, err := credentials.NewServerTLSFromFile("server.crt", "server.key")
if err != nil {
    log.Fatal(err)
}
grpcServer := grpc.NewServer(grpc.Creds(creds))
```

**Resources:**
- [How to secure gRPC connection with SSL/TLS in Go](https://dev.to/techschoolguru/how-to-secure-grpc-connection-with-ssl-tls-in-go-4ph)
- [gRPC API Security Best Practices](https://www.stackhawk.com/blog/best-practices-for-grpc-security/)

#### HTTP/2 Rapid Reset Attack (CVE-2023-44487)

**Vulnerability:**
Attackers send and cancel HTTP/2 requests rapidly, causing excessive concurrent method handlers and resource exhaustion.

**Vulnerable Code:**
```go
// No limits on concurrent streams
grpcServer := grpc.NewServer()
```

**Secure Code:**
```go
// Limit concurrent streams
grpcServer := grpc.NewServer(
    grpc.MaxConcurrentStreams(100),
)
```

**Resources:**
- [gRPC-Go HTTP/2 Rapid Reset vulnerability](https://github.com/grpc/grpc-go/security/advisories/GHSA-m425-mq94-257g)
- [CVE-2023-44487](https://www.resolvedsecurity.com/vulnerability-catalog/CVE-2023-44487)

#### Denial of Service via Large Messages

**Vulnerability:**
Arbitrarily large messages can exhaust server memory.

**Secure Code:**
```go
// CLIENT - Set limits
conn, err := grpc.Dial("localhost:50051",
    grpc.WithTransportCredentials(creds),
    grpc.WithDefaultCallOptions(
        grpc.MaxCallRecvMsgSize(4*1024*1024), // 4MB
        grpc.MaxCallSendMsgSize(4*1024*1024),
    ),
)

// SERVER - Set limits
grpcServer := grpc.NewServer(
    grpc.MaxRecvMsgSize(4*1024*1024), // 4MB
    grpc.MaxSendMsgSize(4*1024*1024),
    grpc.MaxConcurrentStreams(100),
)
```

### 2. Authentication and Authorization

#### Token-Based Authentication

```go
import (
    "context"
    "google.golang.org/grpc/metadata"
    "google.golang.org/grpc/credentials"
)

// CLIENT - Add token to requests
func (t *tokenAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
    return map[string]string{
        "authorization": "Bearer " + t.token,
    }, nil
}

func (t *tokenAuth) RequireTransportSecurity() bool {
    return true // Require TLS
}

// Use with connection
perRPC := &tokenAuth{token: "your-jwt-token"}
conn, err := grpc.Dial("localhost:50051",
    grpc.WithTransportCredentials(creds),
    grpc.WithPerRPCCredentials(perRPC),
)

// SERVER - Extract and validate token
func authenticateToken(ctx context.Context) error {
    md, ok := metadata.FromIncomingContext(ctx)
    if !ok {
        return errors.New("missing metadata")
    }

    tokens := md["authorization"]
    if len(tokens) == 0 {
        return errors.New("missing token")
    }

    token := strings.TrimPrefix(tokens[0], "Bearer ")
    // Validate token (JWT, OAuth, etc.)
    if !validateToken(token) {
        return errors.New("invalid token")
    }

    return nil
}

// Use as interceptor
func authInterceptor(ctx context.Context, req interface{},
    info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

    if err := authenticateToken(ctx); err != nil {
        return nil, status.Error(codes.Unauthenticated, err.Error())
    }

    return handler(ctx, req)
}

grpcServer := grpc.NewServer(
    grpc.UnaryInterceptor(authInterceptor),
)
```

#### Mutual TLS (mTLS)

```go
// SERVER - Require client certificates
cert, err := tls.LoadX509KeyPair("server.crt", "server.key")
if err != nil {
    log.Fatal(err)
}

certPool := x509.NewCertPool()
ca, err := ioutil.ReadFile("ca.crt")
if err != nil {
    log.Fatal(err)
}
certPool.AppendCertsFromPEM(ca)

creds := credentials.NewTLS(&tls.Config{
    Certificates: []tls.Certificate{cert},
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    certPool,
})

grpcServer := grpc.NewServer(grpc.Creds(creds))

// CLIENT - Provide client certificate
cert, err := tls.LoadX509KeyPair("client.crt", "client.key")
if err != nil {
    log.Fatal(err)
}

certPool := x509.NewCertPool()
ca, err := ioutil.ReadFile("ca.crt")
if err != nil {
    log.Fatal(err)
}
certPool.AppendCertsFromPEM(ca)

creds := credentials.NewTLS(&tls.Config{
    Certificates: []tls.Certificate{cert},
    RootCAs:      certPool,
})

conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(creds))
```

**Resources:**
- [gRPC Authentication](https://grpc.io/docs/guides/auth/)
- [gRPC-Go Authentication Support](https://github.com/grpc/grpc-go/blob/master/Documentation/grpc-auth-support.md)
- [gRPC Client Authentication](https://jbrandhorst.com/post/grpc-auth/)

### 3. Input Validation with Protovalidate

**Modern Approach (Recommended):**

**Proto Definition:**
```protobuf
syntax = "proto3";

import "buf/validate/validate.proto";

message User {
  string email = 1 [(buf.validate.field).string.email = true];

  string username = 2 [
    (buf.validate.field).string = {
      min_len: 3,
      max_len: 30,
      pattern: "^[a-zA-Z0-9_]+$"
    }
  ];

  int32 age = 3 [
    (buf.validate.field).int32 = {
      gte: 0,
      lte: 150
    }
  ];
}
```

**Server Implementation:**
```go
import (
    "context"
    "github.com/bufbuild/protovalidate-go"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

func validationInterceptor(ctx context.Context, req interface{},
    info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

    validator, err := protovalidate.New()
    if err != nil {
        return nil, status.Error(codes.Internal, "validation setup failed")
    }

    if msg, ok := req.(proto.Message); ok {
        if err := validator.Validate(msg); err != nil {
            return nil, status.Error(codes.InvalidArgument, err.Error())
        }
    }

    return handler(ctx, req)
}

grpcServer := grpc.NewServer(
    grpc.UnaryInterceptor(validationInterceptor),
)
```

**Legacy Approach:**
```go
// Using grpc_validator from go-grpc-middleware
import (
    grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
    grpc_validator "github.com/grpc-ecosystem/go-grpc-middleware/validator"
)

grpcServer := grpc.NewServer(
    grpc.UnaryInterceptor(
        grpc_middleware.ChainUnaryServer(
            grpc_validator.UnaryServerInterceptor(),
            // other interceptors...
        ),
    ),
)
```

**Resources:**
- [Protovalidate for gRPC and Go](https://protovalidate.com/quickstart/grpc-go/)
- [Know your inputs or gRPC request validation](https://medium.com/swlh/know-your-inputs-or-grpc-request-validation-8eb29a0ebc31)
- [Learning Protocol Buffers: Validations](https://mariocarrion.com/2023/11/13/learning-grpc-protobuf-validation.html)

### 4. Rate Limiting

**Interceptor-Based Rate Limiting:**

```go
import (
    "context"
    "golang.org/x/time/rate"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "sync"
)

type rateLimiter struct {
    limiters map[string]*rate.Limiter
    mu       sync.RWMutex
    rate     rate.Limit
    burst    int
}

func newRateLimiter(r rate.Limit, b int) *rateLimiter {
    return &rateLimiter{
        limiters: make(map[string]*rate.Limiter),
        rate:     r,
        burst:    b,
    }
}

func (rl *rateLimiter) getLimiter(key string) *rate.Limiter {
    rl.mu.Lock()
    defer rl.mu.Unlock()

    limiter, exists := rl.limiters[key]
    if !exists {
        limiter = rate.NewLimiter(rl.rate, rl.burst)
        rl.limiters[key] = limiter
    }

    return limiter
}

func (rl *rateLimiter) unaryInterceptor(ctx context.Context, req interface{},
    info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

    // Extract client IP from context/metadata
    clientIP := getClientIP(ctx)

    limiter := rl.getLimiter(clientIP)
    if !limiter.Allow() {
        return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
    }

    return handler(ctx, req)
}

// Use it
rl := newRateLimiter(rate.Limit(10), 20) // 10 req/sec, burst of 20
grpcServer := grpc.NewServer(
    grpc.UnaryInterceptor(rl.unaryInterceptor),
)
```

**Resources:**
- [OWASP gRPC Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/gRPC_Security_Cheat_Sheet.html)
- [envoyproxy/ratelimit](https://github.com/envoyproxy/ratelimit)
- [rate-limiter-grpc-go](https://github.com/tommy-sho/rate-limiter-grpc-go)

### 5. gRPC Security Checklist

- [ ] Use TLS for all connections (never use grpc.WithInsecure() in production)
- [ ] Implement authentication (JWT, OAuth, mTLS)
- [ ] Add authorization checks for sensitive operations
- [ ] Validate all input using protovalidate
- [ ] Set message size limits (MaxRecvMsgSize, MaxSendMsgSize)
- [ ] Limit concurrent streams (MaxConcurrentStreams)
- [ ] Implement rate limiting per client
- [ ] Use interceptors for cross-cutting concerns
- [ ] Log security events (auth failures, rate limit hits)
- [ ] Keep gRPC dependencies updated
- [ ] Regular security testing and audits

**Resources:**
- [Enhancing gRPC Security Best Practices](https://www.bytesizego.com/blog/grpc-security)
- [Protecting gRPC Against OWASP's Top Ten API Risks](https://nordicapis.com/protecting-grpc-against-owasps-top-ten-api-risks/)
- [gRPC Security Series: Part 3](https://medium.com/@ibm_ptc_security/grpc-security-series-part-3-c92f3b687dd9)

---

## Cryptography Best Practices

### 1. Use Strong Algorithms

**AVOID (Weak/Deprecated):**
- MD5 (broken)
- SHA-1 (deprecated)
- RC4 (insecure)
- DES, 3DES (weak)
- RSA < 2048 bits

**USE (Secure):**
- SHA-256, SHA-512 (hashing)
- AES-256 (encryption)
- RSA >= 2048 bits (preferably 4096)
- ECDSA with P-256 or higher
- Ed25519 (signing)
- Argon2, bcrypt, scrypt (password hashing)

### 2. Secure Encryption Example

**AES-GCM (Authenticated Encryption):**
```go
import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "io"
)

func encrypt(plaintext []byte, key []byte) ([]byte, error) {
    // Key must be 16, 24, or 32 bytes (AES-128, AES-192, AES-256)
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }

    // GCM provides authenticated encryption
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

    // Generate random nonce
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, err
    }

    // Encrypt and authenticate
    ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
    return ciphertext, nil
}

func decrypt(ciphertext []byte, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

    nonceSize := gcm.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, errors.New("ciphertext too short")
    }

    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, err
    }

    return plaintext, nil
}
```

### 3. Key Generation

**Generate Cryptographically Secure Keys:**
```go
import "crypto/rand"

// Generate random key
func generateKey(size int) ([]byte, error) {
    key := make([]byte, size)
    _, err := rand.Read(key)
    if err != nil {
        return nil, err
    }
    return key, nil
}

// Use crypto/rand, NEVER math/rand for security
```

### 4. Password Hashing

**Use bcrypt or Argon2:**
```go
import "golang.org/x/crypto/bcrypt"

// Hash password
func hashPassword(password string) (string, error) {
    // Cost parameter (10-12 recommended for bcrypt)
    hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
    if err != nil {
        return "", err
    }
    return string(hash), nil
}

// Verify password
func verifyPassword(password, hash string) bool {
    err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
    return err == nil
}
```

### 5. Cryptography Audit

In 2024, Google contracted Trail of Bits for an independent security audit of Go's core cryptography packages. The audit covered:
- Key exchange (ECDH, ML-KEM)
- Digital signatures (ECDSA, RSA, Ed25519)
- Encryption (AES-GCM, AES-CBC, AES-CTR)
- Hashing (SHA-1, SHA-2, SHA-3)
- Key derivation (HKDF, PBKDF2)
- Authentication (HMAC)

**Result:** One low-severity finding in legacy Go+BoringCrypto integration.

**Resources:**
- [Go Cryptography Security Audit](https://go.dev/blog/tob-crypto-audit)
- [crypto package](https://pkg.go.dev/crypto)
- [Implementing Cryptography in Go](https://codezup.com/implementing-cryptography-in-go-with-tls-and-gos-cryptographic-library/)

---

## Concurrency and Race Conditions

### 1. Data Races

**What is a Data Race?**
A data race occurs when two goroutines access the same variable concurrently and at least one access is a write.

**Security Implications:**
- Can break Go's memory safety guarantees
- Unauthorized access or modification of data
- Denial of service
- Privilege escalation
- Unpredictable behavior

**Vulnerable Code:**
```go
package main

var counter int

func increment() {
    counter++ // RACE CONDITION
}

func main() {
    for i := 0; i < 1000; i++ {
        go increment()
    }
}
```

### 2. Detection with Race Detector

**Usage:**
```bash
# Run tests with race detector
go test -race ./...

# Run program with race detector
go run -race main.go

# Build with race detector
go build -race
```

**Limitations:**
- Only works on 64-bit systems
- Only detects data races (not race conditions)
- Requires the race to actually occur during execution
- Cannot detect race conditions (logical bugs)

### 3. Solutions

**Solution 1: Channels (Preferred in Go)**
```go
package main

func increment(counter chan int) {
    for {
        count := <-counter
        count++
        counter <- count
    }
}

func main() {
    counter := make(chan int, 1)
    counter <- 0

    go increment(counter)

    // Pass data through channel
    counter <- <-counter + 1
}
```

**Solution 2: Mutex**
```go
import "sync"

type SafeCounter struct {
    mu    sync.Mutex
    value int
}

func (c *SafeCounter) Increment() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value++
}

func (c *SafeCounter) Value() int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.value
}
```

**Solution 3: Atomic Operations**
```go
import "sync/atomic"

var counter int64

func increment() {
    atomic.AddInt64(&counter, 1)
}

func value() int64 {
    return atomic.LoadInt64(&counter)
}
```

### 4. Best Practices

1. **Share memory by communicating** (use channels)
2. **Don't communicate by sharing memory** (avoid shared state)
3. Always run tests with `-race` flag
4. Use mutexes when you must share memory
5. Use atomic operations for simple counters/flags
6. Design concurrent code to minimize shared state
7. Document which goroutines access which data

**Resources:**
- [Data Race Detector](https://go.dev/doc/articles/race_detector)
- [Introducing the Go Race Detector](https://go.dev/blog/race-detector)
- [Data races in Go and how to fix them](https://www.sohamkamani.com/golang/data-races/)
- [Race Conditions Can Exist in Go](https://checkmarx.com/blog/race-conditions-can-exist-in-go/)

---

## Dependency Security and Supply Chain

### 1. How Go Mitigates Supply Chain Attacks

**go.sum File:**
Contains cryptographic hashes (SHA-256) of each dependency, ensuring:
- Completely consistent dependency content for every build
- Detection of any tampering
- Build fails if checksum mismatch detected

**Checksum Database (sum.golang.org):**
- Global append-only list of go.sum entries
- Maintained by Google
- Ensures everyone uses same dependency contents
- Makes targeted attacks (backdoors) impossible

**Key Design Decisions:**
1. **Deterministic Builds**: Version of every dependency fully determined by go.mod
2. **No Post-Install Hooks**: Code cannot execute during fetch or build
3. **Immutable Versions**: Published versions cannot change

### 2. Verify Dependencies

**Check Module Integrity:**
```bash
# Verify modules haven't been altered
go mod verify

# Update dependencies
go get -u ./...

# Tidy dependencies
go mod tidy

# Vendor dependencies (optional)
go mod vendor
```

### 3. Scan for Vulnerabilities

**Using govulncheck:**
```bash
# Install
go install golang.org/x/vuln/cmd/govulncheck@latest

# Scan project
govulncheck ./...

# JSON output
govulncheck -json ./...
```

**Using OSV Scanner:**
```bash
# Install
go install github.com/google/osv-scanner/cmd/osv-scanner@latest

# Scan
osv-scanner --lockfile=go.mod
```

### 4. Real-World Supply Chain Attacks

**Typosquatting Examples:**
- Malicious MongoDB Go module: `github.com/qiniiu/qmgo` (vs legitimate `github.com/qiniu/qmgo`)
- Backdoored BoltDB typosquat exploited Go Module Proxy caching

**Prevention:**
1. Carefully verify package names
2. Check package popularity and maintenance
3. Review code of new dependencies
4. Use `GOPRIVATE` for internal modules
5. Monitor dependency updates
6. Maintain minimal dependency tree

### 5. Security Best Practices

```go
// go.mod - Pin to specific versions
module myapp

go 1.21

require (
    github.com/trusted/package v1.2.3 // Specific version
    // NOT: github.com/trusted/package latest
)

// Use GOPRIVATE for internal packages
// export GOPRIVATE=github.com/mycompany/*

// Exclude malicious versions
exclude github.com/bad/package v1.0.0
```

**CI/CD Integration:**
```yaml
# .github/workflows/security.yml
name: Security Scan
on: [push, pull_request]
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - name: Run govulncheck
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@latest
          govulncheck ./...
      - name: Verify modules
        run: go mod verify
```

**Resources:**
- [How Go Mitigates Supply Chain Attacks](https://go.dev/blog/supply-chain)
- [GitLab catches MongoDB Go module supply chain attack](https://about.gitlab.com/blog/gitlab-catches-mongodb-go-module-supply-chain-attack/)
- [Go Supply Chain Attack: Malicious Package Exploits Go Module](https://socket.dev/blog/malicious-package-exploits-go-module-proxy-caching-for-persistence)

---

## Security Checklist

### General Security
- [ ] Keep Go updated to latest stable version
- [ ] Run `govulncheck ./...` regularly
- [ ] Run `gosec ./...` on all code
- [ ] Run `go vet ./...` before commits
- [ ] Run `go test -race ./...` to detect data races
- [ ] Use `golangci-lint` or `staticcheck` for linting
- [ ] Review all uses of `unsafe` package (avoid if possible)
- [ ] Never use `text/template` for HTML output
- [ ] Set appropriate Content-Type headers
- [ ] Validate and sanitize ALL user input

### Input Validation
- [ ] Use parameterized queries for SQL (never string concatenation)
- [ ] Use `html/template` for HTML output (never `text/template`)
- [ ] Avoid shell invocation with user input
- [ ] Use allowlists for commands, not denylists
- [ ] Clean file paths with `filepath.Clean` and `filepath.Join`
- [ ] Verify paths stay within expected directories
- [ ] Validate gRPC inputs with protovalidate

### Cryptography
- [ ] Use crypto/rand, NEVER math/rand for security
- [ ] Use AES-256-GCM for encryption
- [ ] Use bcrypt/Argon2 for password hashing (never plain SHA)
- [ ] Avoid MD5, SHA-1, RC4, DES, 3DES
- [ ] Use TLS 1.2+ for all network communication
- [ ] Generate keys with sufficient entropy (256 bits)
- [ ] Rotate keys regularly

### gRPC Security
- [ ] Never use `grpc.WithInsecure()` in production
- [ ] Implement TLS for all gRPC connections
- [ ] Add authentication (JWT, mTLS, OAuth)
- [ ] Validate all RPC inputs
- [ ] Set message size limits
- [ ] Limit concurrent streams
- [ ] Implement rate limiting
- [ ] Log authentication failures

### Concurrency
- [ ] Run tests with `-race` flag
- [ ] Use channels for sharing data between goroutines
- [ ] Protect shared state with mutexes
- [ ] Use atomic operations for simple counters
- [ ] Avoid shared mutable state when possible
- [ ] Document goroutine ownership of data

### Dependencies
- [ ] Run `go mod verify` regularly
- [ ] Use `govulncheck` in CI/CD
- [ ] Pin dependencies to specific versions
- [ ] Review code of new dependencies
- [ ] Keep dependencies updated
- [ ] Minimize dependency tree
- [ ] Use `GOPRIVATE` for internal modules

### Error Handling
- [ ] Never expose internal errors to users
- [ ] Log errors with context (but not sensitive data)
- [ ] Return generic error messages to clients
- [ ] Handle all error returns (don't ignore)
- [ ] Use custom error types for better handling

### Configuration
- [ ] Never hardcode credentials (use environment variables)
- [ ] Use secret management systems (Vault, AWS Secrets Manager)
- [ ] Validate all configuration values
- [ ] Use secure defaults
- [ ] Document security-sensitive configuration

### Testing
- [ ] Write security-focused tests
- [ ] Test with malicious inputs
- [ ] Fuzz test critical functions (`go test -fuzz`)
- [ ] Test authentication/authorization paths
- [ ] Test error handling paths
- [ ] Regular penetration testing

---

## Additional Resources

### Official Go Security
- [Go Security Policy](https://go.dev/doc/security/)
- [Security Best Practices for Go Developers](https://go.dev/doc/security/best-practices)
- [Go Vulnerability Database](https://pkg.go.dev/vuln/)
- [Go Vulnerability Management](https://go.dev/doc/security/vuln/)

### OWASP Resources
- [OWASP Go Secure Coding Practices Guide](https://owasp.org/www-project-go-secure-coding-practices-guide/)
- [OWASP Go-SCP GitHub](https://github.com/OWASP/Go-SCP)
- [OWASP Top Ten Guide for Go Developers](https://medium.com/@erwindev/the-owasp-top-ten-guide-for-go-developers-e80786dc4400)
- [OWASP gRPC Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/gRPC_Security_Cheat_Sheet.html)

### Security Tools
- [gosec - Go security checker](https://github.com/securego/gosec)
- [govulncheck - Official vulnerability scanner](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)
- [staticcheck - Advanced linter](https://staticcheck.io/)
- [golangci-lint - Linter aggregator](https://golangci-lint.run/)
- [osv-scanner - Google's vulnerability scanner](https://github.com/google/osv-scanner)

### Community Resources
- [Awesome Golang Security](https://github.com/guardrailsio/awesome-golang-security)
- [Go Security Cheatsheet - Snyk](https://snyk.io/blog/go-security-cheatsheet-for-go-developers/)
- [Secure Coding in Go: OWASP Top 10](https://www.hitechtrends.com/2025/05/25/secure-coding-in-go-owasp-top-10-exploits-and-fixes/)

---

## Conclusion

Security is an ongoing process, not a one-time task. This guide covers the major vulnerability classes in Go applications, but new vulnerabilities are discovered regularly. Stay informed about:

1. **Security advisories** from the Go team
2. **CVE databases** for Go and your dependencies
3. **Community security discussions** and best practices
4. **Regular security audits** of your codebase
5. **Emerging attack patterns** in the Go ecosystem

Remember: **Defense in depth** is key. Don't rely on a single security measure; implement multiple layers of protection. When in doubt, consult security experts and follow the principle of least privilege.

For the latest security updates, always refer to the official Go security resources and maintain a security-first mindset in your development process.

---

**Document Version:** 1.0
**Last Updated:** December 2024
**Author:** Compiled from official Go documentation, OWASP resources, and community security research
**License:** This guide is provided for educational purposes
