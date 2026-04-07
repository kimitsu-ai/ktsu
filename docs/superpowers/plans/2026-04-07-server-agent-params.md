# Server & Agent Params Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `params:` block to `agent.yaml` and `server.yaml` that enables runtime parameterization — agent params interpolate into `prompt.system`, server params are passed as MCP initialization config.

**Architecture:** Config types gain `ParamDecl`/`PromptConfig`/`StepParams` structs. A new `internal/config/params.go` handles resolution (env vars, defaults, step overrides) and prompt interpolation. The orchestrator dispatcher resolves params and interpolates system prompts before dispatching; the MCP client gains an `Initialize` method that sends server params on connection.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, standard library `strings`/`os`/`regexp`

---

## File Map

| Action | File | Responsibility |
|---|---|---|
| Modify | `internal/config/types.go` | Add `ParamDecl`, `PromptConfig`, `StepParams`; update `AgentConfig`, `ToolServerConfig`, `ServerRef`, `PipelineStep` |
| Create | `internal/config/params.go` | Env resolution, prompt ref validation, interpolation, param resolution |
| Create | `internal/config/params_test.go` | Unit tests for all param logic |
| Modify | `internal/config/loader_test.go` | Update agent/server load tests for new schema |
| Modify | `internal/runtime/agent/types.go` | Add `Params map[string]any` to `ToolServerSpec` |
| Modify | `internal/runtime/agent/mcp/client.go` | Add `Initialize` method |
| Modify | `internal/runtime/agent/mcp/client_test.go` | Test `Initialize` |
| Modify | `internal/runtime/agent/loop.go` | Call `Initialize` before `DiscoverTools` when params present |
| Modify | `internal/orchestrator/server.go` | Wire param resolution and prompt interpolation into dispatch |
| Modify | `examples/hello/agents/greeter.agent.yaml` | Migrate `system:` → `prompt.system:` |
| Modify | `docs/yaml-spec/agent.md` | Document `params:` and `prompt:` blocks |
| Modify | `docs/yaml-spec/server.md` | Document `params:` block |
| Modify | `docs/yaml-spec/workflow.md` | Document namespaced `params.agent`/`params.server` |
| Modify | `docs/kimitsu-tool-servers.md` | Document server params and MCP initialization |

---

## Task 1: Update config types

**Files:**
- Modify: `internal/config/types.go`
- Modify: `internal/config/loader_test.go`

- [ ] **Step 1: Write failing tests for new agent YAML schema**

Add to `internal/config/loader_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/... -run "TestLoadAgent_promptBlock|TestLoadAgent_serverRefParams|TestLoadToolServer_withParams|TestLoadWorkflow_stepParams" -v
```

Expected: FAIL — fields do not exist on structs yet.

- [ ] **Step 3: Update `internal/config/types.go`**

Replace the entire file content:

```go
package config

// ParamDecl declares a named parameter on an agent or server.
// Default nil means the param is required.
type ParamDecl struct {
	Description string  `yaml:"description"`
	Default     *string `yaml:"default,omitempty"`
}

// PromptConfig holds the LLM-facing prompt configuration for an agent.
type PromptConfig struct {
	System string `yaml:"system"`
}

// StepParams holds namespaced parameter values for a workflow pipeline step.
type StepParams struct {
	Agent  map[string]any            `yaml:"agent,omitempty"`
	Server map[string]map[string]any `yaml:"server,omitempty"`
}

// WorkflowConfig represents a workflow.yaml file (kind: workflow)
type WorkflowConfig struct {
	Kind        string         `yaml:"kind"`
	Name        string         `yaml:"name"`
	Version     string         `yaml:"version"`
	Description string         `yaml:"description"`
	Input       WorkflowInput  `yaml:"input,omitempty"`
	Pipeline    []PipelineStep `yaml:"pipeline"`
	ModelPolicy *ModelPolicy   `yaml:"model_policy,omitempty"`
}

// WorkflowInput declares the expected input schema for a workflow.
type WorkflowInput struct {
	Schema map[string]interface{} `yaml:"schema,omitempty"`
}

// PipelineStep is one entry in pipeline[]. Exactly one of Agent/Transform/Webhook is set.
type PipelineStep struct {
	ID                  string                 `yaml:"id"`
	Agent               string                 `yaml:"agent,omitempty"`
	Transform           *TransformSpec         `yaml:"transform,omitempty"`
	Webhook             *WebhookSpec           `yaml:"webhook,omitempty"`
	ForEach             *ForEachSpec           `yaml:"for_each,omitempty"`
	DependsOn           []string               `yaml:"depends_on,omitempty"`
	Condition           string                 `yaml:"condition,omitempty"`
	ConfidenceThreshold float64                `yaml:"confidence_threshold,omitempty"`
	Params              StepParams             `yaml:"params,omitempty"`
	Model               *ModelSpec             `yaml:"model,omitempty"`
	Consolidation       string                 `yaml:"consolidation,omitempty"`
	Output              *OutputSpec            `yaml:"output,omitempty"`
}

// ForEachSpec configures fanout iteration for an agent step.
type ForEachSpec struct {
	From        string `yaml:"from"`
	MaxItems    int    `yaml:"max_items,omitempty"`
	Concurrency int    `yaml:"concurrency,omitempty"`
	MaxFailures int    `yaml:"max_failures,omitempty"`
}

// WebhookSpec declares an HTTP webhook call.
type WebhookSpec struct {
	URL      string                 `yaml:"url"`
	Method   string                 `yaml:"method,omitempty"`
	Body     map[string]interface{} `yaml:"body,omitempty"`
	TimeoutS int                    `yaml:"timeout_s,omitempty"`
}

type TransformSpec struct {
	Inputs []TransformInput         `yaml:"inputs"`
	Ops    []map[string]interface{} `yaml:"ops"`
}

type TransformInput struct {
	From string `yaml:"from"`
}

type ModelSpec struct {
	Group     string `yaml:"group"`
	MaxTokens int    `yaml:"max_tokens"`
}

type ModelPolicy struct {
	CostBudgetUSD float64           `yaml:"cost_budget_usd"`
	ForceGroup    string            `yaml:"force_group,omitempty"`
	GroupMap      map[string]string `yaml:"group_map,omitempty"`
	TimeoutS      int               `yaml:"timeout_s,omitempty"`
}

type OutputSpec struct {
	Schema map[string]interface{} `yaml:"schema"`
}

// AgentConfig represents an agent config block.
// prompt.system replaces the former top-level system field.
type AgentConfig struct {
	Name        string                `yaml:"name"`
	Description string                `yaml:"description"`
	Model       string                `yaml:"model"`
	Params      map[string]ParamDecl  `yaml:"params,omitempty"`
	Prompt      PromptConfig          `yaml:"prompt"`
	Servers     []ServerRef           `yaml:"servers"`
	SubAgents   []string              `yaml:"sub_agents"`
	MaxTurns    int                   `yaml:"max_turns"`
	Output      *OutputSpec           `yaml:"output,omitempty"`
}

type ServerRef struct {
	Name   string            `yaml:"name"`
	Path   string            `yaml:"path"`
	Params map[string]string `yaml:"params,omitempty"`
	Access AccessConfig      `yaml:"access"`
}

type AccessConfig struct {
	Allowlist []string `yaml:"allowlist"`
}

// EnvConfig represents environments/*.env.yaml
type EnvConfig struct {
	Name      string            `yaml:"name"`
	Variables map[string]string `yaml:"variables"`
	Providers []ProviderConfig  `yaml:"providers"`
	State     StateConfig       `yaml:"state"`
}

type ProviderConfig struct {
	Name   string            `yaml:"name"`
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

type StateConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

// GatewayConfig represents gateway.yaml
type GatewayConfig struct {
	Providers   []ProviderConfig   `yaml:"providers"`
	ModelGroups []ModelGroupConfig `yaml:"model_groups"`
}

type ModelGroupConfig struct {
	Name               string          `yaml:"name"`
	Models             []string        `yaml:"models"`
	Strategy           string          `yaml:"strategy"`
	DefaultTemperature float64         `yaml:"default_temperature,omitempty"`
	Pricing            []PricingConfig `yaml:"pricing,omitempty"`
}

type PricingConfig struct {
	Model            string  `yaml:"model"`
	InputPerMillion  float64 `yaml:"input_per_million"`
	OutputPerMillion float64 `yaml:"output_per_million"`
}

// ServerManifest represents servers.yaml (marketplace)
type ServerManifest struct {
	Servers []ToolServerConfig `yaml:"servers"`
}

// ToolServerConfig represents a tool server definition.
type ToolServerConfig struct {
	Name        string               `yaml:"name"`
	Description string               `yaml:"description"`
	URL         string               `yaml:"url"`
	Auth        string               `yaml:"auth,omitempty"`
	Params      map[string]ParamDecl `yaml:"params,omitempty"`
}
```

- [ ] **Step 4: Fix the compile error in `internal/orchestrator/server.go`**

On line 377, `agentCfg.System` no longer exists. Temporarily update it to use the new field so the project compiles:

Find:
```go
system = agentCfg.System
```

Replace with:
```go
system = agentCfg.Prompt.System
```

- [ ] **Step 5: Run the new tests to verify they pass, then run the full suite**

```bash
go test ./internal/config/... -run "TestLoadAgent_promptBlock|TestLoadAgent_serverRefParams|TestLoadToolServer_withParams|TestLoadWorkflow_stepParams" -v
```

Expected: PASS

```bash
go test ./...
```

Expected: All pass (no other code reads `step.Params` or `agentCfg.System`).

- [ ] **Step 6: Commit**

```bash
git add internal/config/types.go internal/config/loader_test.go internal/orchestrator/server.go
git commit -m "feat: add ParamDecl/PromptConfig/StepParams types; migrate AgentConfig.System to Prompt.System"
```

---

## Task 2: Param resolution and validation logic

**Files:**
- Create: `internal/config/params.go`
- Create: `internal/config/params_test.go`

- [ ] **Step 1: Write failing tests in `internal/config/params_test.go`**

```go
package config

import (
	"os"
	"testing"
)

// --- ResolveEnvValue ---

func TestResolveEnvValue_envRef(t *testing.T) {
	os.Setenv("TEST_PARAM_VAR", "resolved-value")
	defer os.Unsetenv("TEST_PARAM_VAR")
	got := ResolveEnvValue("env:TEST_PARAM_VAR")
	if got != "resolved-value" {
		t.Errorf("got %q want %q", got, "resolved-value")
	}
}

func TestResolveEnvValue_literal(t *testing.T) {
	got := ResolveEnvValue("plain-value")
	if got != "plain-value" {
		t.Errorf("got %q want %q", got, "plain-value")
	}
}

func TestResolveEnvValue_unsetEnvReturnsEmpty(t *testing.T) {
	os.Unsetenv("DEFINITELY_NOT_SET_XYZ")
	got := ResolveEnvValue("env:DEFINITELY_NOT_SET_XYZ")
	if got != "" {
		t.Errorf("expected empty string for unset env var, got %q", got)
	}
}

// --- ValidatePromptRefs ---

func TestValidatePromptRefs_allDeclared(t *testing.T) {
	err := ValidatePromptRefs("Hello {{name}}.", map[string]ParamDecl{
		"name": {Description: "user name"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePromptRefs_noRefs(t *testing.T) {
	err := ValidatePromptRefs("Hello world.", map[string]ParamDecl{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePromptRefs_undeclaredRef(t *testing.T) {
	err := ValidatePromptRefs("Hello {{unknown}}.", map[string]ParamDecl{})
	if err == nil {
		t.Fatal("expected error for undeclared ref, got nil")
	}
}

func TestValidatePromptRefs_multipleUndeclared(t *testing.T) {
	err := ValidatePromptRefs("{{a}} and {{b}}", map[string]ParamDecl{"a": {Description: "x"}})
	if err == nil {
		t.Fatal("expected error for undeclared ref 'b', got nil")
	}
}

// --- InterpolatePrompt ---

func TestInterpolatePrompt_replacesAll(t *testing.T) {
	got, err := InterpolatePrompt("Hello {{name}}, domain is {{domain}}.", map[string]string{
		"name":   "Alice",
		"domain": "billing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Hello Alice, domain is billing."
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestInterpolatePrompt_noRefs(t *testing.T) {
	got, err := InterpolatePrompt("No placeholders here.", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "No placeholders here." {
		t.Errorf("got %q", got)
	}
}

func TestInterpolatePrompt_missingValueReturnsError(t *testing.T) {
	_, err := InterpolatePrompt("Hello {{name}}.", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing value, got nil")
	}
}

func TestInterpolatePrompt_repeatedRef(t *testing.T) {
	got, err := InterpolatePrompt("{{name}} and {{name}} again.", map[string]string{"name": "Bob"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Bob and Bob again."
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// --- ResolveAgentParams ---

func strPtr(s string) *string { return &s }

func TestResolveAgentParams_usesDefault(t *testing.T) {
	declared := map[string]ParamDecl{
		"persona": {Description: "...", Default: strPtr("helpful assistant")},
	}
	got, err := ResolveAgentParams(declared, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["persona"] != "helpful assistant" {
		t.Errorf("got %q want %q", got["persona"], "helpful assistant")
	}
}

func TestResolveAgentParams_stepOverridesDefault(t *testing.T) {
	declared := map[string]ParamDecl{
		"persona": {Description: "...", Default: strPtr("helpful assistant")},
	}
	got, err := ResolveAgentParams(declared, map[string]any{"persona": "support rep"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["persona"] != "support rep" {
		t.Errorf("got %q want %q", got["persona"], "support rep")
	}
}

func TestResolveAgentParams_missingRequiredReturnsError(t *testing.T) {
	declared := map[string]ParamDecl{
		"domain": {Description: "..."},
	}
	_, err := ResolveAgentParams(declared, nil)
	if err == nil {
		t.Fatal("expected error for missing required param, got nil")
	}
}

func TestResolveAgentParams_resolvesEnvDefault(t *testing.T) {
	os.Setenv("TEST_PERSONA", "admin")
	defer os.Unsetenv("TEST_PERSONA")
	declared := map[string]ParamDecl{
		"persona": {Description: "...", Default: strPtr("env:TEST_PERSONA")},
	}
	got, err := ResolveAgentParams(declared, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["persona"] != "admin" {
		t.Errorf("got %q want %q", got["persona"], "admin")
	}
}

func TestResolveAgentParams_resolvesEnvStepOverride(t *testing.T) {
	os.Setenv("TEST_DOMAIN", "finance")
	defer os.Unsetenv("TEST_DOMAIN")
	declared := map[string]ParamDecl{
		"domain": {Description: "..."},
	}
	got, err := ResolveAgentParams(declared, map[string]any{"domain": "env:TEST_DOMAIN"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["domain"] != "finance" {
		t.Errorf("got %q want %q", got["domain"], "finance")
	}
}

func TestResolveAgentParams_emptyDeclared(t *testing.T) {
	got, err := ResolveAgentParams(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

// --- ResolveServerParams ---

func TestResolveServerParams_usesServerDefault(t *testing.T) {
	declared := map[string]ParamDecl{
		"namespace": {Description: "...", Default: strPtr("global")},
	}
	got, err := ResolveServerParams(declared, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["namespace"] != "global" {
		t.Errorf("got %q want %q", got["namespace"], "global")
	}
}

func TestResolveServerParams_agentRefOverridesDefault(t *testing.T) {
	declared := map[string]ParamDecl{
		"namespace": {Description: "...", Default: strPtr("global")},
	}
	got, err := ResolveServerParams(declared, map[string]string{"namespace": "team-ns"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["namespace"] != "team-ns" {
		t.Errorf("got %q want %q", got["namespace"], "team-ns")
	}
}

func TestResolveServerParams_stepOverridesAgentRef(t *testing.T) {
	declared := map[string]ParamDecl{
		"namespace": {Description: "...", Default: strPtr("global")},
	}
	got, err := ResolveServerParams(
		declared,
		map[string]string{"namespace": "team-ns"},
		map[string]any{"namespace": "user-123"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["namespace"] != "user-123" {
		t.Errorf("got %q want %q", got["namespace"], "user-123")
	}
}

func TestResolveServerParams_missingRequiredReturnsError(t *testing.T) {
	declared := map[string]ParamDecl{
		"namespace": {Description: "..."},
	}
	_, err := ResolveServerParams(declared, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing required server param, got nil")
	}
}
```

- [ ] **Step 2: Run to verify all fail**

```bash
go test ./internal/config/... -run "TestResolveEnvValue|TestValidatePromptRefs|TestInterpolatePrompt|TestResolveAgentParams|TestResolveServerParams" -v
```

Expected: FAIL — functions not defined yet.

- [ ] **Step 3: Implement `internal/config/params.go`**

```go
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// ResolveEnvValue resolves an "env:VAR_NAME" reference to its environment value.
// Non-env: values are returned unchanged.
func ResolveEnvValue(v string) string {
	if strings.HasPrefix(v, "env:") {
		return os.Getenv(strings.TrimPrefix(v, "env:"))
	}
	return v
}

// ValidatePromptRefs returns an error if prompt.system references any {{key}}
// not declared in params. Call this at boot to catch misconfigured agents.
func ValidatePromptRefs(system string, params map[string]ParamDecl) error {
	matches := placeholderRe.FindAllStringSubmatch(system, -1)
	for _, m := range matches {
		key := m[1]
		if _, ok := params[key]; !ok {
			return fmt.Errorf("prompt references undeclared param %q", key)
		}
	}
	return nil
}

// InterpolatePrompt replaces {{key}} placeholders in system with values from resolved.
// Returns an error if any placeholder key is missing from resolved.
func InterpolatePrompt(system string, resolved map[string]string) (string, error) {
	var replaceErr error
	result := placeholderRe.ReplaceAllStringFunc(system, func(match string) string {
		key := placeholderRe.FindStringSubmatch(match)[1]
		v, ok := resolved[key]
		if !ok && replaceErr == nil {
			replaceErr = fmt.Errorf("prompt references param %q which has no resolved value", key)
		}
		return v
	})
	if replaceErr != nil {
		return "", replaceErr
	}
	return result, nil
}

// ResolveAgentParams resolves final string values for all declared agent params.
// Resolution order: ParamDecl.Default < stepAgentParams (last wins).
// Returns an error if any required param (nil Default) has no value in stepAgentParams.
// Resolves env:VAR_NAME in both defaults and step-provided values.
func ResolveAgentParams(declared map[string]ParamDecl, stepAgentParams map[string]any) (map[string]string, error) {
	result := make(map[string]string, len(declared))
	for name, decl := range declared {
		if decl.Default != nil {
			result[name] = ResolveEnvValue(*decl.Default)
		}
		if v, ok := stepAgentParams[name]; ok {
			if s, ok := v.(string); ok {
				result[name] = ResolveEnvValue(s)
			}
		}
		if _, ok := result[name]; !ok {
			return nil, fmt.Errorf("required agent param %q has no value", name)
		}
	}
	return result, nil
}

// ResolveServerParams resolves final string values for all declared server params.
// Resolution order: ParamDecl.Default < agentRefParams < stepServerParams (last wins).
// Returns an error if any required param (nil Default) has no value anywhere.
// Resolves env:VAR_NAME at each level.
func ResolveServerParams(declared map[string]ParamDecl, agentRefParams map[string]string, stepServerParams map[string]any) (map[string]string, error) {
	result := make(map[string]string, len(declared))
	for name, decl := range declared {
		if decl.Default != nil {
			result[name] = ResolveEnvValue(*decl.Default)
		}
		if v, ok := agentRefParams[name]; ok {
			result[name] = ResolveEnvValue(v)
		}
		if v, ok := stepServerParams[name]; ok {
			if s, ok := v.(string); ok {
				result[name] = ResolveEnvValue(s)
			}
		}
		if _, ok := result[name]; !ok {
			return nil, fmt.Errorf("required server param %q has no value", name)
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Run all param tests**

```bash
go test ./internal/config/... -v
```

Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add internal/config/params.go internal/config/params_test.go
git commit -m "feat: add param resolution, env interpolation, and prompt interpolation logic"
```

---

## Task 3: Add Params to ToolServerSpec

**Files:**
- Modify: `internal/runtime/agent/types.go`

- [ ] **Step 1: Add `Params` field to `ToolServerSpec`**

In `internal/runtime/agent/types.go`, update `ToolServerSpec`:

```go
// ToolServerSpec declares a tool server the agent may call.
type ToolServerSpec struct {
	Name      string         `json:"name"`
	URL       string         `json:"url"`
	Allowlist []string       `json:"allowlist"`
	AuthToken string         `json:"auth_token,omitempty"` // resolved bearer token
	Params    map[string]any `json:"params,omitempty"`     // resolved server params for MCP init
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: Builds cleanly (no callers set Params yet).

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/agent/types.go
git commit -m "feat: add Params field to ToolServerSpec for MCP initialization config"
```

---

## Task 4: MCP client — Initialize method

**Files:**
- Modify: `internal/runtime/agent/mcp/client.go`
- Modify: `internal/runtime/agent/mcp/client_test.go`

- [ ] **Step 1: Write failing test for Initialize**

Add to `internal/runtime/agent/mcp/client_test.go`:

```go
func TestInitialize_sendsConfigInParams(t *testing.T) {
	var receivedConfig map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if params, ok := req["params"].(map[string]any); ok {
			receivedConfig, _ = params["config"].(map[string]any)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{},
		})
	}))
	defer srv.Close()

	c := newClient()
	err := c.Initialize(context.Background(), srv.URL, "", map[string]any{"namespace": "user-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedConfig == nil {
		t.Fatal("expected config to be sent in initialize params")
	}
	if receivedConfig["namespace"] != "user-123" {
		t.Errorf("got config %v, want namespace=user-123", receivedConfig)
	}
}

func TestInitialize_withAuthToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer srv.Close()

	c := newClient()
	err := c.Initialize(context.Background(), srv.URL, "my-token", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer my-token" {
		t.Errorf("got auth %q want %q", gotAuth, "Bearer my-token")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/runtime/agent/mcp/... -run "TestInitialize" -v
```

Expected: FAIL — method does not exist.

- [ ] **Step 3: Add `Initialize` to `internal/runtime/agent/mcp/client.go`**

Add this method after `CallTool`:

```go
// Initialize sends an MCP initialize request with optional config params.
// Use this before DiscoverTools when the server requires per-connection configuration.
// config is sent under the "config" key in the initialize params.
// authToken, if non-empty, is sent as a Bearer token in the Authorization header.
func (c *Client) Initialize(ctx context.Context, url, authToken string, config map[string]any) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "ktsu", "version": "1.0"},
		"config":          config,
	}
	_, err := c.rpc(ctx, url, authToken, "initialize", params)
	return err
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/runtime/agent/mcp/... -v
```

Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/agent/mcp/client.go internal/runtime/agent/mcp/client_test.go
git commit -m "feat: add mcp.Client.Initialize for sending server config params on connection"
```

---

## Task 5: Agent loop — call Initialize before DiscoverTools

**Files:**
- Modify: `internal/runtime/agent/loop.go`

- [ ] **Step 1: Update the discovery goroutine in `loop.go`**

Find the goroutine in `loop.go` (lines ~122–128):

```go
go func(i int, srv ToolServerSpec) {
    defer discWg.Done()
    discovered, err := l.mcpClient.DiscoverTools(ctx, srv.URL, srv.AuthToken, srv.Allowlist)
    discResults[i] = discoveryResult{srv: srv, tools: discovered, err: err}
}(i, srv)
```

Replace with:

```go
go func(i int, srv ToolServerSpec) {
    defer discWg.Done()
    if len(srv.Params) > 0 {
        if err := l.mcpClient.Initialize(ctx, srv.URL, srv.AuthToken, srv.Params); err != nil {
            discResults[i] = discoveryResult{srv: srv, err: fmt.Errorf("initialize server %s: %w", srv.Name, err)}
            return
        }
    }
    discovered, err := l.mcpClient.DiscoverTools(ctx, srv.URL, srv.AuthToken, srv.Allowlist)
    discResults[i] = discoveryResult{srv: srv, tools: discovered, err: err}
}(i, srv)
```

- [ ] **Step 2: Run the full test suite**

```bash
go test ./...
```

Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/agent/loop.go
git commit -m "feat: call MCP Initialize before DiscoverTools when server params are present"
```

---

## Task 6: Orchestrator dispatch — wire param resolution and interpolation

**Files:**
- Modify: `internal/orchestrator/server.go`

- [ ] **Step 1: Update `runtimeDispatcher.Dispatch` to resolve params and interpolate**

In `internal/orchestrator/server.go`, update the section that builds `system` and `toolServers` (lines ~376–413).

Replace:

```go
agentName = agentCfg.Name
system = agentCfg.System
modelGroup = agentCfg.Model
```

With:

```go
agentName = agentCfg.Name
modelGroup = agentCfg.Model

// Resolve agent params and interpolate system prompt.
resolvedAgentParams, resolveErr := config.ResolveAgentParams(agentCfg.Params, step.Params.Agent)
if resolveErr != nil {
    return nil, zero, fmt.Errorf("agent %s param resolution: %w", agentCfg.Name, resolveErr)
}
var interpErr error
system, interpErr = config.InterpolatePrompt(agentCfg.Prompt.System, resolvedAgentParams)
if interpErr != nil {
    return nil, zero, fmt.Errorf("agent %s prompt interpolation: %w", agentCfg.Name, interpErr)
}
```

Then, inside the `for _, srv := range agentCfg.Servers` loop, replace:

```go
toolServers = append(toolServers, agent.ToolServerSpec{
    Name:      serverCfg.Name,
    URL:       serverCfg.URL,
    Allowlist: srv.Access.Allowlist,
    AuthToken: authToken,
})
```

With:

```go
// Resolve server params (nil map access returns nil safely).
resolvedServerParams, serverParamErr := config.ResolveServerParams(
    serverCfg.Params,
    srv.Params,
    step.Params.Server[srv.Name],
)
if serverParamErr != nil {
    return nil, zero, fmt.Errorf("server %s param resolution: %w", srv.Name, serverParamErr)
}
serverParamsAny := make(map[string]any, len(resolvedServerParams))
for k, v := range resolvedServerParams {
    serverParamsAny[k] = v
}
toolServers = append(toolServers, agent.ToolServerSpec{
    Name:      serverCfg.Name,
    URL:       serverCfg.URL,
    Allowlist: srv.Access.Allowlist,
    AuthToken: authToken,
    Params:    serverParamsAny,
})
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
go build ./...
```

Expected: Builds cleanly.

- [ ] **Step 3: Run full test suite**

```bash
go test ./...
```

Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrator/server.go
git commit -m "feat: resolve agent/server params and interpolate system prompt in orchestrator dispatch"
```

---

## Task 7: Update examples

**Files:**
- Modify: `examples/hello/agents/greeter.agent.yaml`

- [ ] **Step 1: Migrate `system:` to `prompt.system:`**

Replace the contents of `examples/hello/agents/greeter.agent.yaml`:

```yaml
name: greeter
description: Generates a friendly greeting from a name.
model: standard
max_turns: 1
prompt:
  system: |
    You receive a JSON input containing a "name" field under the "input" key.
    Generate a friendly, one-sentence greeting for that person.
    Return exactly the JSON output described in your schema — nothing else.
output:
  schema:
    type: object
    required: [greeting]
    properties:
      greeting:
        type: string
```

- [ ] **Step 2: Verify the project still builds**

```bash
go build ./...
```

Expected: Builds cleanly.

- [ ] **Step 3: Commit**

```bash
git add examples/hello/agents/greeter.agent.yaml
git commit -m "chore: migrate greeter example agent to prompt.system block"
```

---

## Task 8: Update docs

**Files:**
- Modify: `docs/yaml-spec/agent.md`
- Modify: `docs/yaml-spec/server.md`
- Modify: `docs/yaml-spec/workflow.md`
- Modify: `docs/kimitsu-tool-servers.md`

- [ ] **Step 1: Update `docs/yaml-spec/agent.md`**

Replace the annotated example's `system:` field with the new `params:` + `prompt:` blocks:

In the **Annotated Example** code block, replace:
```yaml
system: |
  You are a triage agent. The full pipeline envelope is provided as JSON in the first user message.
  Reference upstream step outputs as <step-id>.<field> (e.g. parse.intent).
  Workflow input fields are under input.<field> (e.g. input.message).
```

With:
```yaml
params:                          # declared parameters — required unless default is set
  persona:
    description: "The role this agent plays"
    default: "triage specialist"
  escalation_team:
    description: "Team name for escalations — required, no default"

prompt:
  system: |
    You are a {{persona}}. The full pipeline envelope is provided as JSON in the first user message.
    Escalate critical issues to the {{escalation_team}} team.
    Reference upstream step outputs as <step-id>.<field> (e.g. parse.intent).
    Workflow input fields are under input.<field> (e.g. input.message).
```

In the **server reference block**, add `params:`:
```yaml
servers:
  - name: wiki-search
    path: servers/wiki-search.server.yaml
    params:                      # values for this server's declared params
      region: "us-east"
    access:
```

In the **Fields table**, replace the `system` row:

| Field | Type | Required | Description |
|---|---|---|---|
| `params` | map | no | Declared parameters. Each entry: `description` (required) and `default` (optional). Params without a default are required — a missing value is a boot error. Values support `env:VAR_NAME` syntax. |
| `params.<name>.description` | string | yes | Human-readable explanation |
| `params.<name>.default` | string | no | Default value. Omit to make the param required. |
| `prompt.system` | string | yes | System prompt. May reference declared params as `{{param_name}}`. |
| `servers[].params` | map | no | String values for the server's declared params. Overrides server defaults; may be overridden by workflow step `params.server.<name>.*`. Supports `env:VAR_NAME`. |

Remove the old `system` row from the table.

- [ ] **Step 2: Update `docs/yaml-spec/server.md`**

Add a `params:` block to the **Annotated Example**:

```yaml
name: wiki-search
description: "..."
url: "https://mcp.internal/wiki"
auth: "env:WIKI_TOKEN"
params:                          # optional — omit if server needs no configuration
  region:
    description: "AWS region to query"
    default: "us-east-1"
```

Add to the **Fields table**:

| Field | Type | Required | Description |
|---|---|---|---|
| `params` | map | no | Declared parameters passed as MCP initialization config. Each entry has `description` (required) and `default` (optional). Params without a default are required — missing values are boot errors. Supports `env:VAR_NAME` in defaults. |
| `params.<name>.description` | string | yes | Human-readable explanation |
| `params.<name>.default` | string | no | Default value used when no value is provided by the agent or workflow step. |

Add a **Notes** entry: `auth` operates at the HTTP transport layer (Authorization header) and is separate from `params`, which are passed as MCP initialization config.

- [ ] **Step 3: Update `docs/yaml-spec/workflow.md`**

In the **Annotated Example**, replace the `params` block for the `parse` step:

```yaml
  - id: parse
    agent: ktsu/secure-parser@1.0.0
    params:
      agent:                     # params.agent.* — values for the agent's declared params
        source_field: message
        extract:
          intent: { type: string, enum: [billing, technical, legal, other] }
```

Add a new example step showing server params:

```yaml
  - id: recall
    agent: ./agents/recall.agent.yaml
    params:
      agent:
        persona: "billing specialist"
      server:
        memory:                  # server name as declared in the agent file
          namespace: "env:USER_NAMESPACE"
    depends_on: [parse]
```

In the **Agent Step Fields table**, update the `params` row:

| Field | Type | Required | Description |
|---|---|---|---|
| `params.agent` | map | no | Values for the agent's declared params. Overrides agent-file defaults. Supports `env:VAR_NAME`. |
| `params.server.<name>` | map | no | Values for the named server's declared params. `<name>` matches `servers[].name` in the agent file. Overrides agent-file server ref params and server defaults. Supports `env:VAR_NAME`. |

Remove the old row: `params | object | no | Parameters for built-in agents`.

- [ ] **Step 4: Update `docs/kimitsu-tool-servers.md`**

In the **Local Tool Server Files** section, add after the Fields table:

```markdown
### Server Params

Tool servers may declare parameters under `params:`. These are passed to the server as MCP initialization config when the agent runtime connects, allowing per-invocation configuration such as storage namespace or region.

```yaml
name: memory
url: "http://localhost:9200"
params:
  namespace:
    description: "Storage namespace for this connection"
    default: "global"
```

Params are resolved in this order (last wins):
1. Default declared in `server.yaml`
2. `params` on the server reference in `agent.yaml`
3. `params.server.<name>.*` in the workflow pipeline step

Required params (no `default`) must be satisfied somewhere in this chain — a missing value is a boot error. Values support `env:VAR_NAME` syntax.

**Note:** `auth` is separate from `params`. Auth is an HTTP Authorization header; params are MCP protocol-level config sent during the `initialize` handshake.
```

- [ ] **Step 5: Commit**

```bash
git add docs/yaml-spec/agent.md docs/yaml-spec/server.md docs/yaml-spec/workflow.md docs/kimitsu-tool-servers.md
git commit -m "docs: document params blocks for agents and servers, update workflow step params schema"
```

---

## Final Verification

- [ ] **Run full test suite**

```bash
go test ./...
```

Expected: All tests pass with no failures.

- [ ] **Build the binary**

```bash
go build ./...
```

Expected: Builds cleanly with no errors.
