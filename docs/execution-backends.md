# Execution Backends

Agents can run commands in different execution environments. The backend is
declared in the agent's `agent.yaml` manifest.

## Supported Backends

| Backend   | Description                 | Key Options                        |
| --------- | --------------------------- | ---------------------------------- |
| `local`   | Default local shell         | _(none)_                           |
| `docker`  | Docker container            | `image`, `volumes`, `env`, `shell` |
| `ssm`     | AWS Systems Manager (EC2)   | `instance_id`, `region`, `profile` |
| `kubectl` | Kubernetes pod              | `pod`, `namespace`, `container`    |

## Docker Backend

```yaml
# agent.yaml
environment:
  type: docker
  options:
    image: golang:1.26
    volumes: ".:/workspace"
    working_dir: /workspace
    shell: /bin/bash
```

## Kubernetes Backend

```yaml
# agent.yaml
environment:
  type: kubectl
  options:
    pod: build-pod
    namespace: ci
    container: golang
    shell: /bin/bash
```

## AWS SSM Backend

```yaml
# agent.yaml
environment:
  type: ssm
  options:
    instance_id: i-0123456789abcdef0
    region: us-west-2
    profile: my-profile
```
