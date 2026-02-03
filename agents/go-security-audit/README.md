# go-security-audit

A comprehensive Fabric pattern for performing security audits on Go codebases. This pattern identifies vulnerabilities, security anti-patterns, and potential exploits, providing actionable remediation guidance based on industry best practices.

## Features

- **Comprehensive Vulnerability Detection**: Scans for OWASP Top 10 and Go-specific vulnerabilities
- **Severity-Based Reporting**: Categorizes findings as CRITICAL, HIGH, MEDIUM, LOW, or INFO
- **Actionable Remediation**: Provides vulnerable code snippets alongside secure implementations
- **Knowledge Base Integration**: Leverages extensive Go security knowledge covering:
  - Injection attacks (SQL, Command, NoSQL)
  - Cross-Site Scripting (XSS)
  - Path traversal vulnerabilities
  - Memory safety issues (buffer overflow, integer overflow, unsafe package)
  - gRPC security best practices
  - Cryptography standards
  - Concurrency and race conditions
  - Dependency security and supply chain attacks
- **Tool Recommendations**: Suggests appropriate security tools (govulncheck, gosec, go vet)
- **Tailored Checklists**: Generates security checklists specific to your codebase
- **Quick Wins**: Identifies high-impact, low-effort security improvements

## Installation

This pattern requires [Fabric](https://github.com/danielmiessler/fabric) to be installed.

```bash
# Clone the patterns hub
git clone https://github.com/yourusername/fabric-patterns-hub.git

# Link or copy to your Fabric patterns directory
ln -s $(pwd)/fabric-patterns-hub/patterns/go-security-audit ~/.config/fabric/patterns/go-security-audit

# Or copy directly
cp -r fabric-patterns-hub/patterns/go-security-audit ~/.config/fabric/patterns/
```

## Usage

### Audit a Single Go File

```bash
cat main.go | fabric -p go-security-audit
```

### Audit an Entire Package

```bash
cat pkg/auth/*.go | fabric -p go-security-audit
```

### Audit Multiple Files

```bash
cat $(find . -name "*.go") | fabric -p go-security-audit
```

### Audit with Security Tool Output

```bash
# Combine your code with gosec output for enhanced analysis
cat main.go <(gosec ./...) | fabric -p go-security-audit
```

### Save Report to File

```bash
cat main.go | fabric -p go-security-audit > security-audit-report.md
```

### Audit Specific Package

```bash
# Using go list to get package files
go list -f '{{range .GoFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}' ./... | xargs cat | fabric -p go-security-audit
```

## Example Input

```bash
cat << 'EOF' | fabric -p go-security-audit
package main

import (
    "database/sql"
    "fmt"
    "net/http"
)

func getUserByUsername(db *sql.DB, username string) error {
    // Vulnerable to SQL injection
    query := fmt.Sprintf("SELECT * FROM users WHERE username='%s'", username)
    rows, err := db.Query(query)
    if err != nil {
        return err
    }
    defer rows.Close()
    return nil
}

func main() {
    http.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
        username := r.URL.Query().Get("username")
        // Missing input validation and error handling
        getUserByUsername(nil, username)
    })
    http.ListenAndServe(":8080", nil)
}
EOF
```

## Example Output

The pattern generates a comprehensive security audit report including:

```markdown
# Go Security Audit Report

## Executive Summary

**Code Type:** API Server
**Audit Date:** 2026-01-12
**Go Version:** 1.21+

### Findings Overview

| Severity | Count | Status |
|----------|-------|--------|
| CRITICAL | 1 | 🔴 Immediate action required |
| HIGH | 0 | 🟠 Address urgently |
| MEDIUM | 2 | 🟡 Schedule remediation |
| LOW | 0 | 🔵 Consider fixing |
| INFO | 1 | ℹ️ Recommendations |

**Total Issues:** 4

### Most Critical Issues

1. SQL Injection vulnerability in getUserByUsername function
2. Missing input validation on user-supplied username parameter
3. Improper error handling exposing internal errors

## CRITICAL Severity Findings

### SQL Injection - CWE-89

**File:** `main.go:12`

**Description:**
The code uses string formatting to construct SQL queries with user input,
allowing attackers to manipulate the query structure and execute arbitrary SQL commands.

**Vulnerable Code:**
```go
query := fmt.Sprintf("SELECT * FROM users WHERE username='%s'", username)
rows, err := db.Query(query)
```

**Security Impact:**

- Attackers can bypass authentication
- Unauthorized data access or modification
- Potential database compromise

**Secure Implementation:**

```go
query := "SELECT * FROM users WHERE username=$1"
rows, err := db.Query(query, username)
```

**Remediation Steps:**

1. Replace string formatting with parameterized queries
2. Use database/sql package's parameterized query methods
3. Validate and sanitize all user input
4. Run gosec to detect similar issues: `gosec -include=G201 ./...`

**Detection Tool:** `gosec -include=G201,G202 ./...`

**References:**

- [Official: Avoiding SQL injection risk](https://go.dev/doc/database/sql-injection)
- golang-security-guide.md: SQL Injection section
...

```

## When to Use

Use this pattern when you need to:

- **Security audit** Go codebases before production deployment
- **Code review** with security focus for pull requests
- **Compliance** checks against security standards
- **Vulnerability assessment** of third-party Go libraries
- **Security training** by analyzing example vulnerable code
- **Penetration testing** preparation and threat modeling
- **Regular security scanning** as part of CI/CD pipeline

## When NOT to Use

This pattern is not suitable for:

- **Automated security gates** - While comprehensive, this pattern provides educational context best reviewed by humans
- **Real-time production monitoring** - Use dedicated security monitoring tools instead
- **Performance profiling** - This focuses on security, not performance
- **Functional correctness** - This checks security, not business logic
- **Dependency vulnerability scanning only** - Use govulncheck directly for faster dependency-only scans

## Integration with Security Tools

This pattern complements but doesn't replace dedicated security tools:

### govulncheck (Official Vulnerability Scanner)
```bash
# Run govulncheck first, then analyze with pattern
govulncheck ./... > vulns.txt
cat main.go vulns.txt | fabric -p go-security-audit
```

### gosec (Static Security Analysis)

```bash
# Combine gosec findings with code audit
gosec -fmt=text ./... > gosec.txt
cat $(find . -name "*.go") gosec.txt | fabric -p go-security-audit
```

### go vet (Built-in Analysis)

```bash
# Include go vet output
go vet ./... 2>&1 | tee vet.txt
cat main.go vet.txt | fabric -p go-security-audit
```

## Best Practices

1. **Run Regularly**: Integrate into your development workflow

   ```bash
   # Git pre-commit hook
   git diff HEAD --name-only --diff-filter=ACM | grep '\.go$' | xargs cat | fabric -p go-security-audit
   ```

2. **Prioritize Findings**: Address CRITICAL and HIGH severity issues first

3. **Combine with Tools**: Use alongside govulncheck and gosec for comprehensive coverage

4. **Review Context**: This pattern provides educational context - review findings with your security requirements

5. **Update Knowledge**: Keep the golang-security-guide.md knowledge base updated with latest CVEs

6. **Customize Checks**: Modify system.md to add organization-specific security requirements

7. **Track Remediation**: Save reports to track progress over time

   ```bash
   cat main.go | fabric -p go-security-audit > audits/$(date +%Y-%m-%d)-audit.md
   ```

## Understanding Severity Levels

- **CRITICAL**: Immediate remote exploitation possible (RCE, auth bypass, SQL injection)
- **HIGH**: Significant security risk requiring urgent attention (XSS, path traversal)
- **MEDIUM**: Security weaknesses that should be fixed (missing validation, weak error handling)
- **LOW**: Minor issues or information disclosure
- **INFO**: Best practice recommendations and hardening opportunities

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Security Audit
on: [push, pull_request]

jobs:
  security-audit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Install Fabric
        run: |
          curl -sL https://get.fabric.sh | bash
          export PATH="$HOME/.local/bin:$PATH"

      - name: Install Pattern
        run: |
          mkdir -p ~/.config/fabric/patterns
          cp -r patterns/go-security-audit ~/.config/fabric/patterns/

      - name: Run Security Audit
        run: |
          find . -name "*.go" -not -path "*/vendor/*" | xargs cat | fabric -p go-security-audit > security-audit.md

      - name: Upload Report
        uses: actions/upload-artifact@v3
        with:
          name: security-audit-report
          path: security-audit.md

      - name: Check for Critical Issues
        run: |
          if grep -q "CRITICAL" security-audit.md; then
            echo "::error::Critical security issues found"
            exit 1
          fi
```

## Troubleshooting

### Pattern Not Found

```bash
# Verify pattern installation
ls ~/.config/fabric/patterns/go-security-audit

# Refresh Fabric patterns
fabric --update
```

### Large Codebase Timeout

```bash
# Audit packages individually
for pkg in $(go list ./...); do
  echo "Auditing $pkg"
  go list -f '{{range .GoFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}' $pkg | xargs cat | fabric -p go-security-audit > audit-$(basename $pkg).md
done
```

### False Positives

The pattern is designed to be thorough. Review each finding in context - some may not apply to your specific use case. Consider:

- Input validation already performed elsewhere
- Code running in trusted contexts only
- False positives from security tools included in input

## Related Patterns

- **go-review**: General code quality review for Go
- **go-refactor**: Refactoring Go code to idiomatic standards
- **go-tests**: Generate comprehensive tests for Go code

## Knowledge Base

This pattern includes `golang-security-guide.md`, a comprehensive 1400+ line security reference covering:

- Security tools and analysis
- Common vulnerabilities with CVE references
- Memory safety and overflow vulnerabilities
- gRPC security implementation
- Cryptography best practices
- Concurrency and race conditions
- Dependency security and supply chain attacks
- Complete security checklists

The knowledge base is continuously updated with the latest Go security research and CVEs.

## Contributing

To improve this pattern:

1. Update `golang-security-guide.md` with new vulnerabilities and CVEs
2. Enhance `system.md` with additional vulnerability detection patterns
3. Add test cases in `test-vulnerable.go` and `test-secure.go`
4. Submit pull requests with improvements

## References

- [Go Security Policy](https://go.dev/doc/security/)
- [Go Security Best Practices](https://go.dev/doc/security/best-practices)
- [OWASP Go Secure Coding Practices](https://owasp.org/www-project-go-secure-coding-practices-guide/)
- [Go Vulnerability Database](https://pkg.go.dev/vuln/)
- [govulncheck Documentation](https://go.dev/doc/tutorial/govulncheck)
- [gosec GitHub Repository](https://github.com/securego/gosec)

## License

This pattern is part of the Fabric Patterns Hub and follows the same license terms.

## Support

For issues, questions, or contributions:

- Open an issue in the fabric-patterns-hub repository
- Refer to the comprehensive `golang-security-guide.md` for detailed security information
- Check official Go security resources for the latest updates
