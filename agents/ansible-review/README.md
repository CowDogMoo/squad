# Ansible Review Pattern

Review Ansible collections, roles, playbooks, and Molecule tests for best
practices compliance.

## Usage

```bash
# Review a playbook
cat playbook.yml | fabric --pattern ansible-review

# Review a role's tasks
cat roles/nginx/tasks/main.yml | fabric --pattern ansible-review

# Review multiple files
cat roles/nginx/tasks/*.yml | fabric --pattern ansible-review

# Review with a specific model
cat playbook.yml | fabric --pattern ansible-review --model gpt-4
```

## Features

- **Collection Structure** - Validates galaxy.yml, FQCN usage, plugin organization
- **Role Design** - Checks single responsibility, argument specs, dependencies
- **Variable Management** - Reviews naming conventions, defaults vs vars
- **Task Quality** - Verifies FQCN, descriptive names, idempotency
- **Security** - Detects hardcoded secrets, missing no_log, vault usage
- **Molecule Testing** - Evaluates test coverage and scenario structure
- **Linting** - Checks ansible-lint production profile compliance

## Output

The pattern generates a structured review with:

1. **Summary** - High-level assessment of code quality
2. **Critical Issues** - Must-fix items affecting correctness or security
3. **Improvements** - Non-critical enhancements for better idiomatic code
4. **Positive Observations** - What the code does well
5. **Recommendations** - General suggestions for improvement

## Severity Levels

| Level | Description |
|-------|-------------|
| CRITICAL | Affects correctness, security, or causes failures |
| HIGH | Significant reliability or maintainability issues |
| MEDIUM | Best practice violations, non-idiomatic patterns |
| LOW | Minor improvements, style suggestions |
| INFO | Recommendations for optimization |

## Review Categories

1. Collection Structure
2. Role Design
3. Variable Management
4. Playbook Structure
5. Task Quality
6. Handlers
7. Templates & Files
8. Tags
9. Molecule Testing
10. Linting
11. Security
12. Documentation

## Examples

### Reviewing a Playbook

```bash
cat site.yml | fabric --pattern ansible-review
```

Sample output:

```markdown
## Summary

The playbook has several best practice violations including missing FQCN usage
and potential security issues with hardcoded credentials. Task naming is
inconsistent and some commands lack idempotency guards.

## Critical Issues

### Hardcoded Password in User Creation

**Severity:** CRITICAL
**Category:** Security
**File:** site.yml:15
**Impact:** Credentials exposed in version control

**Problem:**
​```yaml
- name: create database user
  user:
    name: dbadmin
    password: "supersecretpassword123"
​```

**Solution:**
​```yaml
- name: Create database user
  ansible.builtin.user:
    name: dbadmin
    password: "{{ db_admin_password | password_hash('sha512') }}"
  no_log: true
​```

**Explanation:** Never store passwords in plain text. Use Ansible Vault for
sensitive data and always set no_log: true for tasks handling credentials.
```

### Reviewing a Role

```bash
cat roles/nginx/tasks/main.yml | fabric --pattern ansible-review
```

## Testing

Run the test script to validate the pattern:

```bash
./test-pattern.sh
```

This will:
1. Test against a playbook with common anti-patterns
2. Test against a well-structured playbook
3. Validate the filter script functionality

## Knowledge Base

The pattern references `ansible-standards.md` which contains comprehensive
guidelines for:

- The Zen of Ansible philosophy
- Collection and role structure
- Variable management patterns
- Molecule testing configuration
- ansible-lint rules
- Security best practices
- Common anti-patterns to avoid

## Tips

- Provide complete files for better context
- Include related files (vars, defaults) when reviewing roles
- For collection reviews, include galaxy.yml and meta/runtime.yml
- Review Molecule scenarios alongside role tasks
- Use with CI/CD to automate code review

## Related Patterns

- `changelog` - Generate changelog entries for Ansible collections
- `commit` - Create conventional commit messages
