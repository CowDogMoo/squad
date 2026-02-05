# Go Taskfile Agent

Autonomous Taskfile review agent that discovers configuration issues, fixes
best-practice violations, and verifies the result parses correctly.

## Usage

### Fix Mode (default)

Review and fix all Taskfile issues:

```bash
task run:go-taskfile MODEL=gpt-5.2-codex PROVIDER=openai API_KEY="$(op item get 'OpenAI' --reveal --fields personal-api-key)"
```

### Analyze Mode (readonly)

Analyze without applying fixes:

```bash
task run:go-taskfile-analyze MODEL=gpt-5.2-codex PROVIDER=openai API_KEY="$(op item get 'OpenAI' --reveal --fields personal-api-key)"
```

## What It Reviews

- **Structure** - version field, schema comment, file organization
- **Variables** - declaration, scoping, hardcoded values, secrets
- **Task Design** - naming, desc, summary, preconditions
- **Commands** - execution, chaining, multi-line, silent mode
- **Dependencies** - ordering, circular deps, parallel vs sequential
- **Error Handling** - preconditions, status checks, ignore_error usage
- **Security** - secrets, input validation, path safety
- **Includes** - external taskfiles, variable passing, remote includes
- **Output** - logging, echo, silent mode usage

## Severity Levels

| Level | Description |
|-------|-------------|
| CRITICAL | Security issues, syntax errors that break parsing |
| HIGH | Missing required elements, hardcoded values, no error handling |
| MEDIUM | Best practice violations, inconsistent naming |
| LOW | Minor improvements, style consistency |
| INFO | Suggestions for optimization |

## Output

- Fix mode: Applies fixes and produces a report to `/tmp/squad-go-taskfile.txt`
- Analyze mode: Produces a report to `/tmp/squad-go-taskfile-analysis.txt`

## References

The agent uses these knowledge bases:

- `references/taskfile-best-practices.md` - Review criteria and anti-patterns
- `references/go-taskfile-standards.md` - Idiomatic Taskfile patterns
