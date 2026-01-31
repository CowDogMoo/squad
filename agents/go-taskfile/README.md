# Go Taskfile Pattern

Generate idiomatic Taskfile.yaml configurations for Go projects using Go-Task
best practices.

## Usage

```bash
cat requirements.txt | fabric --pattern go-taskfile
```

## Features

- Clean, idiomatic Taskfile structure and naming
- Descriptive tasks with a sensible default
- Variable, environment, and include organization
- Cross-platform task guidance
- Change detection with sources/generates when relevant

## Output

- `Taskfile.yaml` in YAML format
- Optional Notes and Assumptions sections

## Tips

- Provide existing commands and tools for accuracy
- Mention target platforms (linux, darwin, windows)
- Include repo layout details for includes or namespaces
