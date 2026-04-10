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

func TestLoadWorkflow_pipeline_format(t *testing.T) {
	path := writeFile(t, t.TempDir(), "workflow.yaml", `
kind: workflow
name: my-workflow
version: "1.0.0"
input:
  schema:
    type: object
    required: [data]
    properties:
      data: {type: string}
pipeline:
  - id: notify
    webhook:
      url: https://hooks.example.com/notify
      method: POST
      body:
        text: input.data
    depends_on: []
  - id: process
    transform:
      inputs:
        - from: notify
      ops:
        - filter: {expr: "status == 'active'"}
    depends_on: [notify]
`)
	cfg, err := LoadWorkflow(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Kind != "workflow" {
		t.Errorf("expected kind %q, got %q", "workflow", cfg.Kind)
	}
	if cfg.Name != "my-workflow" {
		t.Errorf("expected name %q, got %q", "my-workflow", cfg.Name)
	}
	if cfg.Version != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", cfg.Version)
	}
	if cfg.Input.Schema == nil {
		t.Fatal("expected input schema to be set")
	}
	if len(cfg.Pipeline) != 2 {
		t.Fatalf("expected 2 pipeline steps, got %d", len(cfg.Pipeline))
	}
	if cfg.Pipeline[0].ID != "notify" {
		t.Errorf("expected pipeline[0].ID %q, got %q", "notify", cfg.Pipeline[0].ID)
	}
	if cfg.Pipeline[0].Webhook == nil {
		t.Fatal("expected pipeline[0].Webhook to be non-nil")
	}
	if cfg.Pipeline[0].Webhook.URL != "https://hooks.example.com/notify" {
		t.Errorf("expected webhook URL %q, got %q", "https://hooks.example.com/notify", cfg.Pipeline[0].Webhook.URL)
	}
	if cfg.Pipeline[1].Transform == nil {
		t.Fatal("expected pipeline[1].Transform to be non-nil")
	}
	if len(cfg.Pipeline[1].Transform.Inputs) != 1 || cfg.Pipeline[1].Transform.Inputs[0].From != "notify" {
		t.Errorf("expected transform input from 'notify', got %v", cfg.Pipeline[1].Transform.Inputs)
	}
}

func TestLoadWorkflow_error_missing_file(t *testing.T) {
	_, err := LoadWorkflow("/nonexistent/path/workflow.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadWorkflow_error_malformed(t *testing.T) {
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

func TestLoadAgent_promptBlock(t *testing.T) {
	path := writeFile(t, t.TempDir(), "agent.yaml", `
name: chat
model: standard
max_turns: 5
params:
  persona:
    description: "The persona the agent adopts"
    default: "helpful assistant"
  domain:
    description: "The subject matter domain"
prompt:
  system: "You are a {{persona}} assistant focused on {{domain}}."
output:
  schema:
    type: object
    required: [reply]
    properties:
      reply: {type: string}
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Prompt.System == "" {
		t.Error("expected prompt.system to be set")
	}
	if len(cfg.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(cfg.Params))
	}
	if cfg.Params["persona"].Description != "The persona the agent adopts" {
		t.Errorf("unexpected persona description: %q", cfg.Params["persona"].Description)
	}
	if cfg.Params["persona"].Default == nil || *cfg.Params["persona"].Default != "helpful assistant" {
		t.Errorf("expected persona default %q", "helpful assistant")
	}
	if cfg.Params["domain"].Default != nil {
		t.Error("expected domain to have no default (required)")
	}
}

func TestLoadAgent_serverRefParams(t *testing.T) {
	path := writeFile(t, t.TempDir(), "agent.yaml", `
name: recall
model: standard
servers:
  - name: memory
    path: servers/memory.server.yaml
    params:
      namespace: "default-ns"
    access:
      allowlist: ["*"]
prompt:
  system: "You have memory access."
output:
  schema:
    type: object
    required: [result]
    properties:
      result: {type: string}
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].Params["namespace"] != "default-ns" {
		t.Errorf("unexpected server ref param: %q", cfg.Servers[0].Params["namespace"])
	}
}

func TestLoadToolServer_withParams(t *testing.T) {
	path := writeFile(t, t.TempDir(), "memory.server.yaml", `
name: memory
url: "http://localhost:9200"
params:
  namespace:
    description: "The memory namespace"
    default: "global"
`)
	cfg, err := LoadToolServer(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(cfg.Params))
	}
	if cfg.Params["namespace"].Description != "The memory namespace" {
		t.Errorf("unexpected description: %q", cfg.Params["namespace"].Description)
	}
	if cfg.Params["namespace"].Default == nil || *cfg.Params["namespace"].Default != "global" {
		t.Errorf("unexpected default: %v", cfg.Params["namespace"].Default)
	}
}

func TestLoadWorkflow_stepParams(t *testing.T) {
	path := writeFile(t, t.TempDir(), "workflow.yaml", `
kind: workflow
name: w
version: "1.0.0"
pipeline:
  - id: chat
    agent: agents/chat.agent.yaml
    params:
      agent:
        persona: "support rep"
        domain: "billing"
      server:
        memory:
          namespace: "user-123"
`)
	cfg, err := LoadWorkflow(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	step := cfg.Pipeline[0]
	if step.Params.Agent["persona"] != "support rep" {
		t.Errorf("unexpected agent param: %v", step.Params.Agent["persona"])
	}
	if step.Params.Server["memory"]["namespace"] != "user-123" {
		t.Errorf("unexpected server param: %v", step.Params.Server["memory"]["namespace"])
	}
}

// --- StripVersion ---

func TestStripVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"inlets/fetch.inlet.yaml@1.0.0", "inlets/fetch.inlet.yaml"},
		{"inlets/fetch.inlet.yaml", "inlets/fetch.inlet.yaml"},
		{"", ""},
	}
	for _, c := range cases {
		got := StripVersion(c.in)
		if got != c.want {
			t.Errorf("StripVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- HubLockFile / HubManifest ---

func TestLoadHubLock_roundTrip(t *testing.T) {
	dir := t.TempDir()
	lock := &HubLockFile{
		Entries: []HubLockEntry{
			{
				Name:    "kyle/support-triage",
				Version: "1.2.0",
				Source:  "github.com/kyle/workflows",
				Ref:     "v1.2.0",
				SHA:     "abc123def456",
				Cache:   filepath.Join(dir, "kyle/support-triage"),
				Mutable: false,
			},
			{
				Name:    "kyle/dev-workflow",
				Version: "0.1.0",
				Source:  "github.com/kyle/workflows",
				Ref:     "main",
				SHA:     "deadbeef1234",
				Cache:   filepath.Join(dir, "kyle/dev-workflow"),
				Mutable: true,
			},
		},
	}
	path := filepath.Join(dir, "ktsuhub.lock.yaml")
	if err := SaveHubLock(path, lock); err != nil {
		t.Fatalf("SaveHubLock: %v", err)
	}
	got, err := LoadHubLock(path)
	if err != nil {
		t.Fatalf("LoadHubLock: %v", err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.Entries))
	}
	if got.Entries[0].Name != "kyle/support-triage" {
		t.Errorf("expected name kyle/support-triage, got %q", got.Entries[0].Name)
	}
	if got.Entries[0].SHA != "abc123def456" {
		t.Errorf("expected SHA abc123def456, got %q", got.Entries[0].SHA)
	}
	if got.Entries[0].Mutable != false {
		t.Errorf("expected Mutable false for first entry, got %v", got.Entries[0].Mutable)
	}
	if got.Entries[1].Name != "kyle/dev-workflow" {
		t.Errorf("expected name kyle/dev-workflow, got %q", got.Entries[1].Name)
	}
	if got.Entries[1].Mutable != true {
		t.Errorf("expected Mutable true for second entry, got %v", got.Entries[1].Mutable)
	}
}

func TestLoadHubLock_missingFile(t *testing.T) {
	_, err := LoadHubLock("/nonexistent/ktsuhub.lock.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadHubLock_malformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ktsuhub.lock.yaml")
	if err := os.WriteFile(path, []byte("entries: [invalid: yaml: :::"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadHubLock(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoadHubManifest_malformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ktsuhub.yaml")
	if err := os.WriteFile(path, []byte("workflows: [invalid: yaml: :::"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadHubManifest(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoadHubManifest_roundTrip(t *testing.T) {
	dir := t.TempDir()
	content := `workflows:
  - name: kyle/support-triage
    version: "1.2.0"
    description: "Triages support tickets"
    tags: [support, nlp]
    entrypoint: workflows/support-triage.workflow.yaml
`
	path := filepath.Join(dir, "ktsuhub.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	manifest, err := LoadHubManifest(path)
	if err != nil {
		t.Fatalf("LoadHubManifest: %v", err)
	}
	if len(manifest.Workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(manifest.Workflows))
	}
	if manifest.Workflows[0].Entrypoint != "workflows/support-triage.workflow.yaml" {
		t.Errorf("unexpected entrypoint: %q", manifest.Workflows[0].Entrypoint)
	}
}
