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
pipeline:
  - id: fetch
    inlet: inlets/fetch.inlet.yaml@1.0.0
    depends_on: []
  - id: process
    transform:
      inputs:
        - from: fetch
      ops:
        - filter: {expr: "status == 'active'"}
    depends_on: [fetch]
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
	if len(cfg.Pipeline) != 2 {
		t.Fatalf("expected 2 pipeline steps, got %d", len(cfg.Pipeline))
	}
	if cfg.Pipeline[0].ID != "fetch" {
		t.Errorf("expected pipeline[0].ID %q, got %q", "fetch", cfg.Pipeline[0].ID)
	}
	if cfg.Pipeline[0].Inlet != "inlets/fetch.inlet.yaml@1.0.0" {
		t.Errorf("expected pipeline[0].Inlet %q, got %q", "inlets/fetch.inlet.yaml@1.0.0", cfg.Pipeline[0].Inlet)
	}
	if cfg.Pipeline[1].Transform == nil {
		t.Fatal("expected pipeline[1].Transform to be non-nil")
	}
	if len(cfg.Pipeline[1].Transform.Inputs) != 1 || cfg.Pipeline[1].Transform.Inputs[0].From != "fetch" {
		t.Errorf("expected transform input from 'fetch', got %v", cfg.Pipeline[1].Transform.Inputs)
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

// --- LoadInlet ---

func TestLoadInlet_webhook(t *testing.T) {
	path := writeFile(t, t.TempDir(), "inlet.yaml", `
kind: inlet
name: fetch-webhook
version: "1.0.0"
trigger:
  type: webhook
  path: /webhook/fetch
mapping:
  envelope:
    request_id: headers."x-request-id"
  output:
    data: body.data
output:
  schema:
    required: [data]
`)
	cfg, err := LoadInlet(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Kind != "inlet" {
		t.Errorf("expected kind %q, got %q", "inlet", cfg.Kind)
	}
	if cfg.Name != "fetch-webhook" {
		t.Errorf("expected name %q, got %q", "fetch-webhook", cfg.Name)
	}
	if cfg.Trigger.Type != "webhook" {
		t.Errorf("expected trigger type %q, got %q", "webhook", cfg.Trigger.Type)
	}
	if cfg.Trigger.Path != "/webhook/fetch" {
		t.Errorf("expected trigger path %q, got %q", "/webhook/fetch", cfg.Trigger.Path)
	}
	if cfg.Mapping.Output["data"] != "body.data" {
		t.Errorf("expected mapping output data %q, got %q", "body.data", cfg.Mapping.Output["data"])
	}
}

func TestLoadInlet_error_missing_file(t *testing.T) {
	_, err := LoadInlet("/nonexistent/inlet.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// --- LoadOutlet ---

func TestLoadOutlet_http_post(t *testing.T) {
	path := writeFile(t, t.TempDir(), "outlet.yaml", `
kind: outlet
name: post-result
version: "1.0.0"
inputs:
  - from: process
    optional: false
mapping:
  action:
    type: http_post
    url: https://api.example.com/results
    body:
      result: process.output
output:
  schema: {}
`)
	cfg, err := LoadOutlet(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Kind != "outlet" {
		t.Errorf("expected kind %q, got %q", "outlet", cfg.Kind)
	}
	if cfg.Name != "post-result" {
		t.Errorf("expected name %q, got %q", "post-result", cfg.Name)
	}
	if len(cfg.Inputs) != 1 || cfg.Inputs[0].From != "process" {
		t.Errorf("expected 1 input from 'process', got %v", cfg.Inputs)
	}
	if cfg.Mapping.Action.Type != "http_post" {
		t.Errorf("expected action type %q, got %q", "http_post", cfg.Mapping.Action.Type)
	}
	if cfg.Mapping.Action.URL != "https://api.example.com/results" {
		t.Errorf("expected action URL %q, got %q", "https://api.example.com/results", cfg.Mapping.Action.URL)
	}
}

func TestLoadOutlet_error_missing_file(t *testing.T) {
	_, err := LoadOutlet("/nonexistent/outlet.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
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
