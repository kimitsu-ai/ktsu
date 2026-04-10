package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadWorkflow(path string) (*WorkflowConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow: %w", err)
	}
	var cfg WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	return &cfg, nil
}

func LoadAgent(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent: %w", err)
	}
	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse agent: %w", err)
	}
	return &cfg, nil
}

func LoadEnv(path string) (*EnvConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read env: %w", err)
	}
	var cfg EnvConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}
	return &cfg, nil
}

func LoadGateway(path string) (*GatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read gateway: %w", err)
	}
	var cfg GatewayConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse gateway: %w", err)
	}
	return &cfg, nil
}

func LoadToolServer(path string) (*ToolServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tool server: %w", err)
	}
	var cfg ToolServerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse tool server: %w", err)
	}
	return &cfg, nil
}

func LoadServerManifest(path string) (*ServerManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read server manifest: %w", err)
	}
	var manifest ServerManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse server manifest: %w", err)
	}
	return &manifest, nil
}

// StripVersion trims the @version suffix from a ref string.
// e.g. "inlets/fetch.inlet.yaml@1.0.0" → "inlets/fetch.inlet.yaml"
func StripVersion(ref string) string {
	if idx := strings.LastIndex(ref, "@"); idx != -1 {
		return ref[:idx]
	}
	return ref
}

// LoadHubLock reads ktsuhub.lock.yaml from path.
func LoadHubLock(path string) (*HubLockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock HubLockFile
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &lock, nil
}

// SaveHubLock writes lock to path as YAML, creating parent directories as needed.
func SaveHubLock(path string, lock *HubLockFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(lock)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadHubManifest reads ktsuhub.yaml from path.
func LoadHubManifest(path string) (*HubManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest HubManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &manifest, nil
}
