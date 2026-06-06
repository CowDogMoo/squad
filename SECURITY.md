# Reporting and Fixing Security Issues

Please do not open GitHub issues or pull requests for security
vulnerabilities — that makes the problem visible to everyone, including
malicious actors.

Report security issues privately by emailing the maintainers at
**jayson.e.grace@gmail.com** with the subject prefix `[squad-security]`.

When reporting, please include:

- A description of the vulnerability and its potential impact
- Steps to reproduce, or a minimal proof-of-concept
- The squad version (`squad version`) and your platform (`uname -a`)
- Any mitigating factors you're aware of

We'll acknowledge the report within three business days and aim to ship a
fix or coordinated disclosure within 30 days of confirmation.

## Static Analysis with Semgrep

This project uses Semgrep for automated security analysis. The scanner
runs on:

- All pull requests targeting `main`
- Direct pushes to `main`
- Weekly scheduled scans (Sundays at 00:00 UTC)

### Enabled Rulesets

| Ruleset                | Purpose                                   |
| ---------------------- | ----------------------------------------- |
| `p/security-audit`     | General security best practices           |
| `p/secrets`            | Detection of hard-coded secrets           |
| `p/ci`                 | CI configuration risks                    |
| `p/supply-chain`       | Supply-chain security checks              |
| `p/golang`             | Go-specific checks                        |
| `p/trailofbits-go`     | Trail of Bits Go security ruleset         |

Findings are surfaced in the GitHub Security tab and block merges on PRs.

## Dependency Scanning

`govulncheck` runs in the pre-commit pipeline and on CI. Vulnerable
dependencies without an upstream fix are tracked in
[`.hooks/govulncheck-scan.sh`](.hooks/govulncheck-scan.sh) so the build
stays green while still surfacing the advisory text.

`renovate` opens PRs for upstream version bumps; the Renovate workflow is
configured to auto-merge low-risk patch upgrades after CI passes.

## Supply Chain

- All third-party GitHub Actions are pinned to commit SHAs (not tag refs)
- Dependencies are tracked in `go.sum` with hash verification
- Release binaries are built via `goreleaser` on tagged commits only
- No `replace` directives are allowed in `go.mod` (enforced by
  [`.hooks/go-no-replacement.sh`](.hooks/go-no-replacement.sh))
