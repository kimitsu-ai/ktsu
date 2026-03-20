package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	return path
}

// --- LoadWorkflow ---

func TestLoadWorkflow_parsesValidYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "workflow.yaml", `
name: my-workflow
description: test workflow
trigger:
  type: http
steps:
  - id: step1
    type: inlet
    depends: []
`)
	cfg, err := LoadWorkflow(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "my-workflow" {
		t.Errorf("expected name %q, got %q", "my-workflow", cfg.Name)
	}
	if cfg.Trigger.Type != "http" {
		t.Errorf("expected trigger type %q, got %q", "http", cfg.Trigger.Type)
	}
	if len(cfg.Steps) != 1 || cfg.Steps[0].ID != "step1" {
		t.Errorf("expected 1 step with id 'step1', got %v", cfg.Steps)
	}
}

func TestLoadWorkflow_errorsOnMissingFile(t *testing.T) {
	_, err := LoadWorkflow("/nonexistent/path/workflow.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadWorkflow_errorsOnMalformedYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "workflow.yaml", "name: [unclosed")
	_, err := LoadWorkflow(path)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}
}

// --- LoadAgent ---

func TestLoadAgent_parsesValidYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "agent.yaml", `
name: my-agent
model: claude-3-5-sonnet
max_turns: 10
servers:
  - name: ktsu/kv
    path: ./kv
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "my-agent" {
		t.Errorf("expected name %q, got %q", "my-agent", cfg.Name)
	}
	if cfg.Model != "claude-3-5-sonnet" {
		t.Errorf("expected model %q, got %q", "claude-3-5-sonnet", cfg.Model)
	}
	if cfg.MaxTurns != 10 {
		t.Errorf("expected max_turns 10, got %d", cfg.MaxTurns)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "ktsu/kv" {
		t.Errorf("expected 1 server 'ktsu/kv', got %v", cfg.Servers)
	}
}

func TestLoadAgent_errorsOnMissingFile(t *testing.T) {
	_, err := LoadAgent("/nonexistent/agent.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadAgent_errorsOnMalformedYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "agent.yaml", "name: [unclosed")
	_, err := LoadAgent(path)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}
}

// --- LoadEnv ---

func TestLoadEnv_parsesValidYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "dev.env.yaml", `
name: dev
variables:
  LOG_LEVEL: debug
providers:
  - name: anthropic
    type: anthropic
    config:
      api_key: sk-test
state:
  driver: sqlite
  dsn: ./dev.db
`)
	cfg, err := LoadEnv(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "dev" {
		t.Errorf("expected name %q, got %q", "dev", cfg.Name)
	}
	if cfg.Variables["LOG_LEVEL"] != "debug" {
		t.Errorf("expected LOG_LEVEL=debug, got %q", cfg.Variables["LOG_LEVEL"])
	}
	if cfg.State.Driver != "sqlite" {
		t.Errorf("expected state driver %q, got %q", "sqlite", cfg.State.Driver)
	}
}

func TestLoadEnv_errorsOnMissingFile(t *testing.T) {
	_, err := LoadEnv("/nonexistent/env.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadEnv_errorsOnMalformedYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "env.yaml", "name: [unclosed")
	_, err := LoadEnv(path)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}
}

// --- LoadGateway ---

func TestLoadGateway_parsesValidYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "gateway.yaml", `
providers:
  - name: anthropic
    type: anthropic
model_groups:
  - name: fast
    models:
      - claude-haiku-4-5
    strategy: round_robin
`)
	cfg, err := LoadGateway(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].Name != "anthropic" {
		t.Errorf("expected 1 provider 'anthropic', got %v", cfg.Providers)
	}
	if len(cfg.ModelGroups) != 1 || cfg.ModelGroups[0].Name != "fast" {
		t.Errorf("expected 1 model group 'fast', got %v", cfg.ModelGroups)
	}
	if cfg.ModelGroups[0].Strategy != "round_robin" {
		t.Errorf("expected strategy %q, got %q", "round_robin", cfg.ModelGroups[0].Strategy)
	}
}

func TestLoadGateway_errorsOnMissingFile(t *testing.T) {
	_, err := LoadGateway("/nonexistent/gateway.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadGateway_errorsOnMalformedYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "gateway.yaml", "providers: [unclosed")
	_, err := LoadGateway(path)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}
}

// --- LoadServerManifest ---

func TestLoadServerManifest_parsesValidYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "servers.yaml", `
servers:
  - name: ktsu/kv
    description: Key-value store
    url: http://localhost:9101
  - name: ktsu/blob
    description: Blob store
    url: http://localhost:9102
`)
	manifest, err := LoadServerManifest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifest.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(manifest.Servers))
	}
	if manifest.Servers[0].Name != "ktsu/kv" || manifest.Servers[1].Name != "ktsu/blob" {
		t.Errorf("unexpected server names: %v", manifest.Servers)
	}
}

func TestLoadServerManifest_errorsOnMissingFile(t *testing.T) {
	_, err := LoadServerManifest("/nonexistent/servers.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadServerManifest_errorsOnMalformedYAML(t *testing.T) {
	path := writeFile(t, t.TempDir(), "servers.yaml", "servers: [unclosed")
	_, err := LoadServerManifest(path)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}
}
