package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kimitsu-ai/ktsu/internal/config/builtins"
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

// ResolveWorkflowRef resolves a workflow step's workflow reference to a WorkflowConfig.
// Handles:
//   - "ktsu/name"              → shipped workflow (always sub-workflow visibility)
//   - "./path/to/file.yaml"    → local path relative to projectDir
//   - "author/name"            → hub-installed (not yet implemented — returns error)
func ResolveWorkflowRef(ref, projectDir string) (*WorkflowConfig, error) {
	if strings.HasPrefix(ref, "ktsu/") {
		data, err := builtins.ReadFile(ref)
		if err != nil {
			return nil, err
		}
		var cfg WorkflowConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse shipped workflow %q: %w", ref, err)
		}
		return &cfg, nil
	}
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") || filepath.IsAbs(ref) {
		path := ref
		if !filepath.IsAbs(ref) {
			path = filepath.Join(projectDir, ref)
		}
		return LoadWorkflow(path)
	}
	// Hub-installed: author/name — not implemented in this plan
	return nil, fmt.Errorf("hub-installed workflow references (%q) are not yet supported; use ./path/ for local or ktsu/ for shipped", ref)
}
