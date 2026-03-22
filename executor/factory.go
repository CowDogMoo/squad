package executor

import "fmt"

// New creates an Executor from the given config.
// If cfg is nil or has an empty/local type, a LocalExecutor is returned.
func New(cfg *Config, workingDir string) (Executor, error) {
	if cfg == nil || cfg.Type == "" || cfg.Type == "local" {
		return &LocalExecutor{WorkingDir: workingDir}, nil
	}

	switch cfg.Type {
	case "docker":
		return NewDockerExecutor(cfg, workingDir)
	case "ssm":
		return NewSSMExecutor(cfg)
	case "kubectl":
		return NewKubectlExecutor(cfg)
	default:
		return nil, fmt.Errorf("unknown executor type: %q", cfg.Type)
	}
}
