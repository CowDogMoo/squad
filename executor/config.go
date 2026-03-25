package executor

// Config is parsed from the agent.yaml `environment` field.
type Config struct {
	// Type selects the executor backend: "local", "docker", "ssm", "kubectl".
	// Empty or "local" uses the default local shell executor.
	Type string `yaml:"type"`

	// Options holds backend-specific configuration as key-value pairs.
	//
	// Docker options:
	//   image        - container image (required)
	//   volumes      - comma-separated host:container mount pairs
	//   env          - comma-separated KEY=VALUE environment variables
	//   working_dir  - working directory inside the container
	//   shell        - shell to use (default: "/bin/sh")
	//   platform     - platform flag (e.g., "linux/amd64")
	//
	// SSM options:
	//   instance_id  - EC2 instance ID (required)
	//   region       - AWS region
	//   profile      - AWS CLI profile
	//   timeout      - command timeout in seconds (default: 600)
	//
	// Kubectl options:
	//   pod          - pod name (required)
	//   namespace    - Kubernetes namespace (default: "default")
	//   container    - container name (for multi-container pods)
	//   shell        - shell to use (default: "/bin/sh")
	Options map[string]string `yaml:"options,omitempty"`
}
