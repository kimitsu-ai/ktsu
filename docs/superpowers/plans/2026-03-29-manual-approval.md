# Manual Approval for Dangerous Tool Calls — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-tool approval gating to the agent allowlist so operators can require human or automated sign-off before dangerous tool calls execute, with full message context always captured for debugging.

**Architecture:** The runtime checkpoints its conversation context and sends a `pending_approval` callback when it hits a gated tool; the orchestrator stores the approval record and keeps the dispatcher's channel open; when a decision arrives the orchestrator re-invokes the runtime with the restored context and the dispatcher's channel finally receives the final result. This keeps the runtime stateless and load-balancer-safe.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, standard `net/http`, existing `internal/orchestrator`, `internal/runtime/agent`, `pkg/types`, `internal/config` packages.

---

## File Map

| File | Change |
|------|--------|
| `internal/config/types.go` | Replace `AccessConfig.Allowlist []string` with `[]ToolAccess`; add `ToolAccess`, `ApprovalPolicy`; add `On` to `PipelineStep` |
| `internal/config/validate.go` | **NEW** — `ValidateApprovalPolicies(servers []ServerRef) error` |
| `internal/runtime/agent/types.go` | Add `ToolApprovalRule`, `PendingApproval`; extend `ToolServerSpec`, `InvokeRequest`, `CallbackPayload` |
| `internal/runtime/agent/loop.go` | `matchesPattern`; always include `Messages` in callback; approval check + checkpoint; resume from context |
| `internal/orchestrator/server.go` | Pass `ApprovalRules` from agent config in dispatcher; handle `pending_approval` in callback; add decide endpoints; cumulative metrics; `fireApprovalWebhooks` |
| `pkg/types/run.go` | Add `Approval`, `ApprovalStatus`; add `Messages json.RawMessage` to `Step` |
| `internal/orchestrator/state/store.go` | Extend `Store` interface with `Approval` CRUD |
| `internal/orchestrator/state/mem_store.go` | Implement `Approval` CRUD |

---

## Task 1 — Config: ToolAccess + ApprovalPolicy types with YAML unmarshal

**Files:**
- Modify: `internal/config/types.go`
- Test: `internal/config/types_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/config/types_test.go`:

```go
package config_test

import (
	"testing"
	"gopkg.in/yaml.v3"
	"github.com/kimitsu-ai/ktsu/internal/config"
)

func TestToolAccessUnmarshal_PlainString(t *testing.T) {
	in := `
allowlist:
  - "read-*"
  - "write-data"
`
	var ac config.AccessConfig
	if err := yaml.Unmarshal([]byte(in), &ac); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ac.Allowlist) != 2 {
		t.Fatalf("want 2 entries, got %d", len(ac.Allowlist))
	}
	if ac.Allowlist[0].Name != "read-*" {
		t.Errorf("want read-*, got %s", ac.Allowlist[0].Name)
	}
	if ac.Allowlist[0].RequireApproval != nil {
		t.Error("want nil RequireApproval for plain string entry")
	}
}

func TestToolAccessUnmarshal_ObjectForm(t *testing.T) {
	in := `
allowlist:
  - "read-*"
  - name: "delete-*"
    require_approval:
      on_reject: fail
      timeout: 30m
      timeout_behavior: reject
`
	var ac config.AccessConfig
	if err := yaml.Unmarshal([]byte(in), &ac); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ac.Allowlist) != 2 {
		t.Fatalf("want 2 entries, got %d", len(ac.Allowlist))
	}
	entry := ac.Allowlist[1]
	if entry.Name != "delete-*" {
		t.Errorf("want delete-*, got %s", entry.Name)
	}
	if entry.RequireApproval == nil {
		t.Fatal("want non-nil RequireApproval")
	}
	if entry.RequireApproval.OnReject != "fail" {
		t.Errorf("want fail, got %s", entry.RequireApproval.OnReject)
	}
	if entry.RequireApproval.TimeoutBehavior != "reject" {
		t.Errorf("want reject, got %s", entry.RequireApproval.TimeoutBehavior)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/config/... -run TestToolAccess -v
```

Expected: compile error — `ToolAccess` and `ApprovalPolicy` not defined.

- [ ] **Step 3: Add ToolAccess, ApprovalPolicy to `internal/config/types.go`**

Replace the existing `AccessConfig` struct and add the new types. In `types.go`, find:
```go
type AccessConfig struct {
	Allowlist []string `yaml:"allowlist"`
}
```

Replace with:
```go
// AccessConfig controls which tools an agent may call on a server.
type AccessConfig struct {
	Allowlist []ToolAccess `yaml:"allowlist"`
}

// ToolAccess is a single allowlist entry. It unmarshals from either a plain
// YAML string ("tool-name") or an object with optional require_approval policy.
type ToolAccess struct {
	Name            string          `yaml:"name"`
	RequireApproval *ApprovalPolicy `yaml:"require_approval,omitempty"`
}

// UnmarshalYAML implements yaml.Unmarshaler so that a plain scalar like
// "delete-*" is treated as ToolAccess{Name: "delete-*"}.
func (t *ToolAccess) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		t.Name = value.Value
		return nil
	}
	type toolAccessAlias ToolAccess
	var alias toolAccessAlias
	if err := value.Decode(&alias); err != nil {
		return err
	}
	*t = ToolAccess(alias)
	return nil
}

// ApprovalPolicy declares how the orchestrator should handle a required approval.
type ApprovalPolicy struct {
	OnReject        string        `yaml:"on_reject"`         // "fail" | "recover"
	Timeout         time.Duration `yaml:"timeout,omitempty"` // 0 = no timeout
	TimeoutBehavior string        `yaml:"timeout_behavior"`  // "fail" | "reject"
}
```

Add `"time"` to the imports in `types.go` if not already present.

- [ ] **Step 4: Run tests to confirm they pass**

```
go test ./internal/config/... -run TestToolAccess -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/types.go internal/config/types_test.go
git commit -m "feat(config): add ToolAccess and ApprovalPolicy types with mixed YAML unmarshal"
```

---

## Task 2 — Config: ValidateApprovalPolicies

**Files:**
- Create: `internal/config/validate.go`
- Test: `internal/config/validate_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/config/validate_test.go`:

```go
package config_test

import (
	"testing"
	"time"
	"github.com/kimitsu-ai/ktsu/internal/config"
)

func TestValidateApprovalPolicies_Valid(t *testing.T) {
	servers := []config.ServerRef{
		{
			Name: "my-server",
			Access: config.AccessConfig{
				Allowlist: []config.ToolAccess{
					{Name: "read-data"},
					{
						Name: "delete-*",
						RequireApproval: &config.ApprovalPolicy{
							OnReject:        "fail",
							Timeout:         30 * time.Minute,
							TimeoutBehavior: "reject",
						},
					},
				},
			},
		},
	}
	if err := config.ValidateApprovalPolicies(servers); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateApprovalPolicies_InvalidOnReject(t *testing.T) {
	servers := []config.ServerRef{
		{
			Name: "my-server",
			Access: config.AccessConfig{
				Allowlist: []config.ToolAccess{
					{
						Name: "delete-*",
						RequireApproval: &config.ApprovalPolicy{
							OnReject:        "maybe",
							TimeoutBehavior: "reject",
						},
					},
				},
			},
		},
	}
	err := config.ValidateApprovalPolicies(servers)
	if err == nil {
		t.Fatal("expected error for invalid on_reject, got nil")
	}
}

func TestValidateApprovalPolicies_InvalidTimeoutBehavior(t *testing.T) {
	servers := []config.ServerRef{
		{
			Name: "my-server",
			Access: config.AccessConfig{
				Allowlist: []config.ToolAccess{
					{
						Name: "delete-*",
						RequireApproval: &config.ApprovalPolicy{
							OnReject:        "fail",
							Timeout:         1 * time.Minute,
							TimeoutBehavior: "ignore",
						},
					},
				},
			},
		},
	}
	err := config.ValidateApprovalPolicies(servers)
	if err == nil {
		t.Fatal("expected error for invalid timeout_behavior, got nil")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/config/... -run TestValidateApproval -v
```

Expected: compile error — `ValidateApprovalPolicies` not defined.

- [ ] **Step 3: Implement `internal/config/validate.go`**

```go
package config

import "fmt"

// ValidateApprovalPolicies checks that all require_approval blocks within the
// given server refs have valid on_reject and timeout_behavior values.
func ValidateApprovalPolicies(servers []ServerRef) error {
	for _, srv := range servers {
		for _, ta := range srv.Access.Allowlist {
			if ta.RequireApproval == nil {
				continue
			}
			p := ta.RequireApproval
			if p.OnReject != "fail" && p.OnReject != "recover" {
				return fmt.Errorf(
					"server %q tool %q: require_approval.on_reject must be \"fail\" or \"recover\", got %q",
					srv.Name, ta.Name, p.OnReject,
				)
			}
			if p.Timeout > 0 {
				if p.TimeoutBehavior != "fail" && p.TimeoutBehavior != "reject" {
					return fmt.Errorf(
						"server %q tool %q: require_approval.timeout_behavior must be \"fail\" or \"reject\", got %q",
						srv.Name, ta.Name, p.TimeoutBehavior,
					)
				}
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```
go test ./internal/config/... -run TestValidateApproval -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/validate.go internal/config/validate_test.go
git commit -m "feat(config): add ValidateApprovalPolicies for boot-time approval policy checks"
```

---

## Task 3 — Runtime types: ToolApprovalRule, InvokeRequest, CallbackPayload

**Files:**
- Modify: `internal/runtime/agent/types.go`
- Test: `internal/runtime/agent/types_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/agent/types_test.go`:

```go
package agent_test

import (
	"encoding/json"
	"testing"
	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
)

func TestInvokeRequest_JSONRoundTrip_WithResume(t *testing.T) {
	req := agent.InvokeRequest{
		RunID:  "run_abc",
		StepID: "step_1",
		Messages: []agent.Message{
			{Role: "system", Content: "you are helpful"},
			{Role: "user", Content: `{"task":"delete records"}`},
		},
		ApprovedToolCalls: []string{"toolu_abc123"},
		IsResume:          true,
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got agent.InvokeRequest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !got.IsResume {
		t.Error("IsResume not preserved")
	}
	if len(got.Messages) != 2 {
		t.Errorf("want 2 messages, got %d", len(got.Messages))
	}
	if len(got.ApprovedToolCalls) != 1 || got.ApprovedToolCalls[0] != "toolu_abc123" {
		t.Error("ApprovedToolCalls not preserved")
	}
}

func TestCallbackPayload_JSONRoundTrip_WithPendingApproval(t *testing.T) {
	payload := agent.CallbackPayload{
		RunID:  "run_abc",
		StepID: "step_1",
		Status: "pending_approval",
		Messages: []agent.Message{
			{Role: "user", Content: "hello"},
		},
		PendingApproval: &agent.PendingApproval{
			ToolName:        "delete-records",
			ToolUseID:       "toolu_abc123",
			Arguments:       map[string]any{"table": "users"},
			OnReject:        "fail",
			TimeoutMS:       1800000,
			TimeoutBehavior: "reject",
		},
	}
	b, _ := json.Marshal(payload)
	var got agent.CallbackPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending_approval" {
		t.Error("Status not preserved")
	}
	if got.PendingApproval == nil {
		t.Fatal("PendingApproval is nil")
	}
	if got.PendingApproval.ToolUseID != "toolu_abc123" {
		t.Error("ToolUseID not preserved")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/runtime/agent/... -run TestInvokeRequest -v
go test ./internal/runtime/agent/... -run TestCallbackPayload -v
```

Expected: compile errors — fields not defined yet.

- [ ] **Step 3: Add new types to `internal/runtime/agent/types.go`**

Current `types.go` has `InvokeRequest`, `ToolServerSpec`, `CallbackPayload`, `Metrics`. Add new fields and types:

```go
// ToolApprovalRule describes approval requirements for tools matching a pattern
// on a specific server. Populated by the orchestrator from agent config.
type ToolApprovalRule struct {
	Pattern         string `json:"pattern"`          // exact, "prefix-*", or "*"
	OnReject        string `json:"on_reject"`         // "fail" | "recover"
	TimeoutMS       int64  `json:"timeout_ms"`        // 0 = no timeout
	TimeoutBehavior string `json:"timeout_behavior"`  // "fail" | "reject"
}

// PendingApproval is included in a CallbackPayload when status == "pending_approval".
type PendingApproval struct {
	ToolName        string         `json:"tool_name"`
	ToolUseID       string         `json:"tool_use_id"`
	Arguments       map[string]any `json:"arguments"`
	OnReject        string         `json:"on_reject"`
	TimeoutMS       int64          `json:"timeout_ms"`
	TimeoutBehavior string         `json:"timeout_behavior"`
}
```

Extend `ToolServerSpec`:
```go
type ToolServerSpec struct {
	Name          string             `json:"name"`
	URL           string             `json:"url"`
	Allowlist     []string           `json:"allowlist"`
	AuthToken     string             `json:"auth_token"`
	ApprovalRules []ToolApprovalRule `json:"approval_rules,omitempty"` // NEW
}
```

Extend `InvokeRequest`:
```go
type InvokeRequest struct {
	RunID        string                 `json:"run_id"`
	StepID       string                 `json:"step_id"`
	AgentName    string                 `json:"agent_name"`
	System       string                 `json:"system"`
	MaxTurns     int                    `json:"max_turns"`
	Model        ModelSpec              `json:"model"`
	Input        map[string]interface{} `json:"input"`
	ToolServers  []ToolServerSpec       `json:"tool_servers"`
	CallbackURL  string                 `json:"callback_url"`
	OutputSchema map[string]any         `json:"output_schema,omitempty"`
	Messages          []Message   `json:"messages,omitempty"`           // NEW: resume context
	ApprovedToolCalls []string    `json:"approved_tool_calls,omitempty"` // NEW: pre-approved IDs
	IsResume          bool        `json:"is_resume,omitempty"`           // NEW: signals cumulative metrics
}
```

Extend `CallbackPayload`:
```go
type CallbackPayload struct {
	RunID     string                 `json:"run_id"`
	StepID    string                 `json:"step_id"`
	Status    string                 `json:"status"`
	Output    map[string]interface{} `json:"output,omitempty"`
	Error     string                 `json:"error,omitempty"`
	RawOutput string                 `json:"raw_output,omitempty"`
	Metrics   Metrics                `json:"metrics"`
	Messages        []Message        `json:"messages,omitempty"`          // NEW: always set
	PendingApproval *PendingApproval `json:"pending_approval,omitempty"`  // NEW: non-nil when pending
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```
go test ./internal/runtime/agent/... -run "TestInvokeRequest|TestCallbackPayload" -v
```

Expected: PASS

- [ ] **Step 5: Confirm full package still compiles**

```
go build ./internal/runtime/agent/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/agent/types.go internal/runtime/agent/types_test.go
git commit -m "feat(runtime): add ToolApprovalRule, PendingApproval; extend InvokeRequest and CallbackPayload"
```

---

## Task 4 — Orchestrator dispatcher: pass ApprovalRules in InvokeRequest

**Files:**
- Modify: `internal/orchestrator/server.go`

The `runtimeDispatcher.Dispatch()` currently builds `agent.ToolServerSpec` with only `Allowlist []string`. It needs to also build `ApprovalRules []ToolApprovalRule` from `agentCfg.Servers[].Access.Allowlist` entries that have `RequireApproval` set.

- [ ] **Step 1: Write the failing test**

In `internal/orchestrator/server_test.go` (create if it doesn't exist), add:

```go
package orchestrator_test

// NOTE: runtimeDispatcher is unexported. Test via integration of the ToolServerSpec
// produced when an agent config has require_approval entries.
// We use a test agent config file for this.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
)

// TestDispatcher_ApprovalRulesPassedToRuntime sets up a fake runtime that captures
// the InvokeRequest and verifies ApprovalRules are populated.
func TestDispatcher_ApprovalRulesPassedToRuntime(t *testing.T) {
	var captured agent.InvokeRequest

	// Fake runtime that captures the request body.
	fakeRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
	}))
	defer fakeRuntime.Close()

	// Fake callback server (orchestrator side) — never called in this test.
	fakeOrch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer fakeOrch.Close()

	// Write a temp agent config with require_approval.
	dir := t.TempDir()
	agentYAML := `
name: test-agent
model: standard
system: you are helpful
max_turns: 1
servers:
  - name: my-server
    path: my-server.server.yaml
    access:
      allowlist:
        - "safe-tool"
        - name: "delete-*"
          require_approval:
            on_reject: fail
            timeout: 30m
            timeout_behavior: reject
output:
  schema:
    type: object
    properties:
      result:
        type: string
`
	serverYAML := `
name: my-server
description: test
url: http://tool-server:9100
`
	writeFile(t, dir+"/agents/test-agent.agent.yaml", agentYAML)
	writeFile(t, dir+"/servers/my-server.server.yaml", serverYAML)

	d := newTestDispatcher(t, fakeRuntime.URL, fakeOrch.URL, dir)

	// Dispatch with a minimal step pointing to the agent.
	step := &config.PipelineStep{Agent: "agents/test-agent.agent.yaml"}
	go d.Dispatch(context.Background(), "run_1", "step_1", step, map[string]any{})

	// Give the goroutine a moment to POST.
	time.Sleep(50 * time.Millisecond)

	if len(captured.ToolServers) == 0 {
		t.Fatal("no tool servers in InvokeRequest")
	}
	srv := captured.ToolServers[0]
	if len(srv.ApprovalRules) != 1 {
		t.Fatalf("want 1 approval rule, got %d", len(srv.ApprovalRules))
	}
	rule := srv.ApprovalRules[0]
	if rule.Pattern != "delete-*" {
		t.Errorf("want delete-*, got %s", rule.Pattern)
	}
	if rule.OnReject != "fail" {
		t.Errorf("want fail, got %s", rule.OnReject)
	}
	if rule.TimeoutMS != int64(30*time.Minute/time.Millisecond) {
		t.Errorf("unexpected timeout_ms: %d", rule.TimeoutMS)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
```

Note: `newTestDispatcher` is a helper to construct the unexported `runtimeDispatcher`; you can either expose it via a test hook or test this behavior with an integration test. If it's too hard to unit test the private type directly, skip to Step 3 and validate the output manually.

- [ ] **Step 2: Run to confirm it fails**

```
go test ./internal/orchestrator/... -run TestDispatcher_ApprovalRules -v
```

Expected: compile or runtime failure.

- [ ] **Step 3: Update `runtimeDispatcher.Dispatch()` in `internal/orchestrator/server.go`**

In the loop that builds `toolServers`, find the block that constructs `agent.ToolServerSpec`:

```go
toolServers = append(toolServers, agent.ToolServerSpec{
    Name:      serverCfg.Name,
    URL:       serverCfg.URL,
    Allowlist: srv.Access.Allowlist,
    AuthToken: authToken,
})
```

The `srv.Access.Allowlist` is now `[]config.ToolAccess`. The `Allowlist` field on `ToolServerSpec` is still `[]string` (for tool discovery). Build both:

```go
var allowlist []string
var approvalRules []agent.ToolApprovalRule
for _, ta := range srv.Access.Allowlist {
    allowlist = append(allowlist, ta.Name)
    if ta.RequireApproval != nil {
        approvalRules = append(approvalRules, agent.ToolApprovalRule{
            Pattern:         ta.Name,
            OnReject:        ta.RequireApproval.OnReject,
            TimeoutMS:       ta.RequireApproval.Timeout.Milliseconds(),
            TimeoutBehavior: ta.RequireApproval.TimeoutBehavior,
        })
    }
}
toolServers = append(toolServers, agent.ToolServerSpec{
    Name:          serverCfg.Name,
    URL:           serverCfg.URL,
    Allowlist:     allowlist,
    AuthToken:     authToken,
    ApprovalRules: approvalRules,
})
```

- [ ] **Step 4: Build to confirm no compile errors**

```
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/server.go
git commit -m "feat(orchestrator): pass ApprovalRules from agent config in InvokeRequest"
```

---

## Task 5 — Loop helpers: matchesPattern + approval rule lookup

**Files:**
- Modify: `internal/runtime/agent/loop.go`
- Test: `internal/runtime/agent/loop_test.go`

- [ ] **Step 1: Write the failing tests**

Create (or extend) `internal/runtime/agent/loop_test.go`:

```go
package agent

import "testing"

func TestMatchesPattern(t *testing.T) {
	cases := []struct {
		toolName string
		pattern  string
		want     bool
	}{
		{"delete-records", "delete-*", true},
		{"delete-records", "delete-records", true},
		{"read-data", "delete-*", false},
		{"anything", "*", true},
		{"read-records", "read-*", true},
		{"read", "read-*", false}, // no suffix after prefix
	}
	for _, tc := range cases {
		got := matchesPattern(tc.toolName, tc.pattern)
		if got != tc.want {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", tc.toolName, tc.pattern, got, tc.want)
		}
	}
}

func TestFindApprovalRule_Match(t *testing.T) {
	rules := []ToolApprovalRule{
		{Pattern: "safe-*", OnReject: "recover"},
		{Pattern: "delete-*", OnReject: "fail", TimeoutMS: 1800000, TimeoutBehavior: "reject"},
	}
	rule := findApprovalRule("delete-records", rules)
	if rule == nil {
		t.Fatal("expected match, got nil")
	}
	if rule.OnReject != "fail" {
		t.Errorf("want fail, got %s", rule.OnReject)
	}
}

func TestFindApprovalRule_NoMatch(t *testing.T) {
	rules := []ToolApprovalRule{
		{Pattern: "delete-*", OnReject: "fail"},
	}
	if rule := findApprovalRule("read-data", rules); rule != nil {
		t.Errorf("expected nil, got %+v", rule)
	}
}
```

Note: these tests reference unexported functions `matchesPattern` and `findApprovalRule`. Since this test file is in `package agent` (not `package agent_test`), it has access to unexported symbols.

- [ ] **Step 2: Run to confirm they fail**

```
go test ./internal/runtime/agent/... -run "TestMatchesPattern|TestFindApprovalRule" -v
```

Expected: compile error — functions not defined.

- [ ] **Step 3: Add helpers to `internal/runtime/agent/loop.go`**

Add after the existing `stripCodeFence` function (at the bottom of the file):

```go
// matchesPattern reports whether toolName matches the given allowlist/approval pattern.
// Supported patterns: exact name, "prefix-*" (tool name must have more chars after prefix), "*".
func matchesPattern(toolName, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "-*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(toolName, prefix) && len(toolName) > len(prefix)
	}
	return toolName == pattern
}

// findApprovalRule returns the first ToolApprovalRule whose pattern matches toolName,
// or nil if no rule matches.
func findApprovalRule(toolName string, rules []ToolApprovalRule) *ToolApprovalRule {
	for i := range rules {
		if matchesPattern(toolName, rules[i].Pattern) {
			return &rules[i]
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```
go test ./internal/runtime/agent/... -run "TestMatchesPattern|TestFindApprovalRule" -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/agent/loop.go internal/runtime/agent/loop_test.go
git commit -m "feat(runtime): add matchesPattern and findApprovalRule helpers"
```

---

## Task 6 — Loop: always include Messages in callback

**Files:**
- Modify: `internal/runtime/agent/loop.go`
- Test: `internal/runtime/agent/loop_test.go`

- [ ] **Step 1: Write the failing test**

Add to `loop_test.go`:

```go
func TestLoop_CallbackAlwaysHasMessages(t *testing.T) {
	// Fake gateway that returns a simple JSON response (no tool calls).
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"content":        `{"answer": "42"}`,
			"model_resolved": "test-model",
			"tokens_in":      10,
			"tokens_out":     5,
			"cost_usd":       0.001,
		})
	}))
	defer gateway.Close()

	loop := NewLoop(gateway.URL, mcp.New(http.DefaultClient))
	req := InvokeRequest{
		RunID:        "run_1",
		StepID:       "step_1",
		System:       "you are helpful",
		MaxTurns:     1,
		Model:        ModelSpec{Group: "standard", MaxTokens: 100},
		Input:        map[string]any{"task": "compute"},
		OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}},
	}
	payload := loop.Run(context.Background(), req)
	if len(payload.Messages) == 0 {
		t.Error("expected Messages to be populated in callback payload")
	}
}
```

Add necessary imports: `"net/http/httptest"`, `"context"`, `"encoding/json"`, `"github.com/kimitsu-ai/ktsu/internal/runtime/agent/mcp"`.

- [ ] **Step 2: Run to confirm it fails**

```
go test ./internal/runtime/agent/... -run TestLoop_CallbackAlwaysHasMessages -v
```

Expected: FAIL — `payload.Messages` is nil.

- [ ] **Step 3: Update `Loop.Run()` in `loop.go` to include Messages**

The `run()` method currently returns `(map[string]any, string, Metrics, error)`. To return messages too, we need to also thread them out.

Change `run()` signature to return messages:

```go
func (l *Loop) run(ctx context.Context, req InvokeRequest) (map[string]any, string, []Message, Metrics, error) {
```

Update the `Run()` method:

```go
func (l *Loop) Run(ctx context.Context, req InvokeRequest) CallbackPayload {
	start := time.Now()
	payload := CallbackPayload{
		RunID:  req.RunID,
		StepID: req.StepID,
	}

	output, rawOutput, messages, metrics, err := l.run(ctx, req)
	metrics.DurationMS = time.Since(start).Milliseconds()
	payload.Metrics = metrics
	payload.Messages = messages // always set

	if err != nil {
		payload.Status = "failed"
		payload.Error = err.Error()
		payload.RawOutput = rawOutput
	} else {
		payload.Status = "ok"
		payload.Output = output
	}
	return payload
}
```

In `run()`, thread `messages` through to the return. At every `return` statement, include the current `messages` slice. Example changes:

```go
// In run(), change return statements to include messages:
return nil, "", messages, metrics, fmt.Errorf("discover tools from %s: %w", srv.Name, err)
// ... and:
return nil, "", messages, metrics, err   // (on callGateway error)
// ... and the success return:
return output, "", messages, metrics, nil
// ... and the max_turns_exceeded case:
return nil, lastInvalidContent, messages, metrics, fmt.Errorf("max_turns_exceeded")
```

Also update `callGateway` to not change — it stays as is. Just ensure every early return in `run()` passes the current `messages` slice.

- [ ] **Step 4: Run the test to confirm it passes**

```
go test ./internal/runtime/agent/... -run TestLoop_CallbackAlwaysHasMessages -v
```

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

```
go test ./internal/runtime/... -v
```

Expected: all PASS (fix any compile errors from the signature change).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/agent/loop.go internal/runtime/agent/loop_test.go
git commit -m "feat(runtime): always include full message context in callback payload"
```

---

## Task 7 — Loop: approval check + checkpoint

**Files:**
- Modify: `internal/runtime/agent/loop.go`
- Test: `internal/runtime/agent/loop_test.go`

- [ ] **Step 1: Write the failing test**

Add to `loop_test.go`:

```go
func TestLoop_ApprovalCheckpoint(t *testing.T) {
	callCount := 0
	// Gateway returns a tool call on turn 1.
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]any{
			"content":        "",
			"model_resolved": "test-model",
			"tokens_in":      10,
			"tokens_out":     5,
			"cost_usd":       0.001,
			"tool_calls": []map[string]any{
				{
					"id":        "toolu_001",
					"name":      "delete-records",
					"arguments": map[string]any{"table": "users"},
				},
			},
		})
	}))
	defer gateway.Close()

	loop := NewLoop(gateway.URL, mcp.New(http.DefaultClient))
	req := InvokeRequest{
		RunID:   "run_1",
		StepID:  "step_1",
		System:  "be helpful",
		MaxTurns: 5,
		Model:   ModelSpec{Group: "standard", MaxTokens: 100},
		Input:   map[string]any{"task": "delete"},
		OutputSchema: map[string]any{"type": "object"},
		ToolServers: []ToolServerSpec{
			{
				Name:      "my-server",
				URL:       "http://unused",
				Allowlist: []string{"delete-records"},
				ApprovalRules: []ToolApprovalRule{
					{
						Pattern:         "delete-*",
						OnReject:        "fail",
						TimeoutMS:       1800000,
						TimeoutBehavior: "reject",
					},
				},
			},
		},
	}

	payload := loop.Run(context.Background(), req)

	if payload.Status != "pending_approval" {
		t.Fatalf("want pending_approval, got %s (error: %s)", payload.Status, payload.Error)
	}
	if payload.PendingApproval == nil {
		t.Fatal("PendingApproval is nil")
	}
	if payload.PendingApproval.ToolName != "delete-records" {
		t.Errorf("want delete-records, got %s", payload.PendingApproval.ToolName)
	}
	if payload.PendingApproval.ToolUseID != "toolu_001" {
		t.Errorf("want toolu_001, got %s", payload.PendingApproval.ToolUseID)
	}
	if len(payload.Messages) == 0 {
		t.Error("Messages should be set at checkpoint")
	}
	// The LLM was called once (for the tool call decision).
	if callCount != 1 {
		t.Errorf("want 1 LLM call, got %d", callCount)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

```
go test ./internal/runtime/agent/... -run TestLoop_ApprovalCheckpoint -v
```

Expected: FAIL — `payload.Status` is `"failed"` (tool_not_permitted or similar, since the tool server URL is unreachable).

- [ ] **Step 3: Implement approval check in the tool call loop in `loop.go`**

In the `run()` function, inside the `for _, tc := range gwResp.ToolCalls` loop, after the existing `toolByName` lookup and before the `mcpClient.CallTool` call:

```go
for _, tc := range gwResp.ToolCalls {
    sref, ok := toolByName[tc.Name]
    if !ok {
        return nil, "", messages, metrics, fmt.Errorf("tool_not_permitted: %s", tc.Name)
    }

    // --- Approval check (NEW) ---
    if rule := findApprovalRule(tc.Name, sref.approvalRules); rule != nil {
        // Check if pre-approved.
        preApproved := false
        for _, id := range req.ApprovedToolCalls {
            if id == tc.ID {
                preApproved = true
                break
            }
        }
        if !preApproved {
            // Build checkpoint: include the pending tool_use in messages.
            toolUseContent, _ := json.Marshal([]map[string]any{{
                "type":  "tool_use",
                "id":    tc.ID,
                "name":  tc.Name,
                "input": tc.Arguments,
            }})
            checkpointMessages := append(append([]Message{}, messages...), Message{
                Role:    "assistant",
                Content: string(toolUseContent),
            })
            return nil, "", checkpointMessages, metrics, &errPendingApproval{
                PendingApproval: PendingApproval{
                    ToolName:        tc.Name,
                    ToolUseID:       tc.ID,
                    Arguments:       tc.Arguments,
                    OnReject:        rule.OnReject,
                    TimeoutMS:       rule.TimeoutMS,
                    TimeoutBehavior: rule.TimeoutBehavior,
                },
            }
        }
    }
    // --- end approval check ---

    // Existing tool call execution continues...
    argsJSON, _ := json.Marshal(tc.Arguments)
    log.Printf(...)
    result, err := l.mcpClient.CallTool(...)
    ...
}
```

Define `errPendingApproval` near the top of `loop.go`:

```go
// errPendingApproval is returned by run() when a tool requires approval.
// Run() detects this and sets status "pending_approval" in the CallbackPayload.
type errPendingApproval struct {
	PendingApproval PendingApproval
}

func (e *errPendingApproval) Error() string { return "pending_approval" }
```

Update `Run()` to detect and handle `errPendingApproval`:

```go
func (l *Loop) Run(ctx context.Context, req InvokeRequest) CallbackPayload {
	start := time.Now()
	payload := CallbackPayload{
		RunID:  req.RunID,
		StepID: req.StepID,
	}

	output, rawOutput, messages, metrics, err := l.run(ctx, req)
	metrics.DurationMS = time.Since(start).Milliseconds()
	payload.Metrics = metrics
	payload.Messages = messages

	var paErr *errPendingApproval
	if errors.As(err, &paErr) {
		payload.Status = "pending_approval"
		payload.PendingApproval = &paErr.PendingApproval
		return payload
	}

	if err != nil {
		payload.Status = "failed"
		payload.Error = err.Error()
		payload.RawOutput = rawOutput
	} else {
		payload.Status = "ok"
		payload.Output = output
	}
	return payload
}
```

Also update `serverRef` (the internal struct in `run()`) to carry approval rules:

```go
type serverRef struct {
    url           string
    authToken     string
    approvalRules []ToolApprovalRule // NEW
}
```

And populate it during tool discovery:

```go
for _, srv := range req.ToolServers {
    discovered, err := l.mcpClient.DiscoverTools(ctx, srv.URL, srv.AuthToken, srv.Allowlist)
    if err != nil {
        return nil, "", messages, metrics, fmt.Errorf("discover tools from %s: %w", srv.Name, err)
    }
    for _, t := range discovered {
        toolByName[t.Name] = serverRef{
            url:           srv.URL,
            authToken:     srv.AuthToken,
            approvalRules: srv.ApprovalRules, // NEW
        }
        ...
    }
}
```

Add `"errors"` to the imports in `loop.go`.

- [ ] **Step 4: Run the test to confirm it passes**

```
go test ./internal/runtime/agent/... -run TestLoop_ApprovalCheckpoint -v
```

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

```
go test ./internal/runtime/... -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/agent/loop.go internal/runtime/agent/loop_test.go
git commit -m "feat(runtime): pause at approval-required tool calls and return pending_approval checkpoint"
```

---

## Task 8 — Loop: resume from context

**Files:**
- Modify: `internal/runtime/agent/loop.go`
- Test: `internal/runtime/agent/loop_test.go`

When `req.Messages != nil`, skip initial message construction and resume from the stored context. If `req.ApprovedToolCalls` is non-empty, execute the pending tool call before re-entering the main loop.

- [ ] **Step 1: Write the failing test**

Add to `loop_test.go`:

```go
func TestLoop_ResumeFromContext_Approved(t *testing.T) {
	toolCalled := false
	// Fake MCP server that confirms the tool was called.
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		if method == "tools/call" {
			toolCalled = true
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "deleted 5 rows"}},
				},
			})
			return
		}
		// tools/list
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"tools": []map[string]any{
					{"name": "delete-records", "description": "delete", "inputSchema": map[string]any{}},
				},
			},
		})
	}))
	defer mcpServer.Close()

	turnCount := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turnCount++
		// After resume, produce a final JSON answer.
		json.NewEncoder(w).Encode(map[string]any{
			"content":        `{"result": "done"}`,
			"model_resolved": "test-model",
			"tokens_in":      5,
			"tokens_out":     3,
		})
	}))
	defer gateway.Close()

	// Build resume context: system + user + assistant tool_use (the checkpoint state).
	toolUseContent, _ := json.Marshal([]map[string]any{{
		"type":  "tool_use",
		"id":    "toolu_001",
		"name":  "delete-records",
		"input": map[string]any{"table": "users"},
	}})
	resumeMessages := []Message{
		{Role: "system", Content: "be helpful"},
		{Role: "user", Content: `{"task":"delete"}`},
		{Role: "assistant", Content: string(toolUseContent)},
	}

	loop := NewLoop(gateway.URL, mcp.New(http.DefaultClient))
	req := InvokeRequest{
		RunID:             "run_1",
		StepID:            "step_1",
		IsResume:          true,
		Messages:          resumeMessages,
		ApprovedToolCalls: []string{"toolu_001"},
		MaxTurns:          5,
		Model:             ModelSpec{Group: "standard", MaxTokens: 100},
		OutputSchema:      map[string]any{"type": "object"},
		ToolServers: []ToolServerSpec{
			{
				Name:      "my-server",
				URL:       mcpServer.URL,
				Allowlist: []string{"delete-records"},
			},
		},
	}

	payload := loop.Run(context.Background(), req)

	if payload.Status != "ok" {
		t.Fatalf("want ok, got %s (error: %s)", payload.Status, payload.Error)
	}
	if !toolCalled {
		t.Error("expected tool to be called on resume")
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

```
go test ./internal/runtime/agent/... -run TestLoop_ResumeFromContext_Approved -v
```

Expected: FAIL.

- [ ] **Step 3: Implement resume logic at the start of `run()` in `loop.go`**

After tool discovery and before initial message construction, add the resume branch:

```go
var messages []Message

if req.Messages != nil {
    // Resume mode: restore context from checkpoint.
    messages = req.Messages

    // If there are pre-approved tool calls, execute the pending one now.
    // The last message in the checkpoint is the assistant tool_use block.
    if len(req.ApprovedToolCalls) > 0 && len(messages) > 0 {
        lastMsg := messages[len(messages)-1]
        if lastMsg.Role == "assistant" {
            var toolUseBlocks []map[string]any
            if err := json.Unmarshal([]byte(lastMsg.Content), &toolUseBlocks); err == nil {
                for _, block := range toolUseBlocks {
                    toolID, _ := block["id"].(string)
                    toolName, _ := block["name"].(string)
                    toolInput, _ := block["input"].(map[string]any)

                    for _, approvedID := range req.ApprovedToolCalls {
                        if approvedID != toolID {
                            continue
                        }
                        sref, ok := toolByName[toolName]
                        if !ok {
                            return nil, "", messages, metrics, fmt.Errorf("tool_not_permitted on resume: %s", toolName)
                        }
                        result, err := l.mcpClient.CallTool(ctx, sref.url, sref.authToken, toolName, toolInput)
                        if err != nil {
                            return nil, "", messages, metrics, fmt.Errorf("tool call %s on resume: %w", toolName, err)
                        }
                        metrics.ToolCalls++
                        resultText := ""
                        if len(result.Content) > 0 {
                            resultText = result.Content[0].Text
                        }
                        toolResultContent, _ := json.Marshal([]map[string]any{{
                            "type":        "tool_result",
                            "tool_use_id": toolID,
                            "content":     resultText,
                        }})
                        messages = append(messages, Message{Role: "user", Content: string(toolResultContent)})
                        break
                    }
                }
            }
        }
    }
} else {
    // Fresh start: build initial messages from system prompt and input.
    inputJSON, err := json.Marshal(req.Input)
    if err != nil {
        return nil, "", messages, metrics, fmt.Errorf("marshal input: %w", err)
    }
    systemPrompt := req.System
    if len(req.OutputSchema) > 0 {
        schemaJSON, err := json.MarshalIndent(req.OutputSchema, "", "  ")
        if err == nil {
            systemPrompt += "\n\nYour output MUST conform to this JSON schema:\n" + string(schemaJSON)
        }
    }
    messages = []Message{
        {Role: "system", Content: systemPrompt},
        {Role: "user", Content: string(inputJSON)},
    }
}
```

Remove the original initial-message construction block that previously came after tool discovery (it's now inside the `else` branch above).

- [ ] **Step 4: Run the test to confirm it passes**

```
go test ./internal/runtime/agent/... -run TestLoop_ResumeFromContext_Approved -v
```

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

```
go test ./internal/runtime/... -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/agent/loop.go internal/runtime/agent/loop_test.go
git commit -m "feat(runtime): resume agent loop from stored message context on re-invoke"
```

---

## Task 9 — Orchestrator state types: Approval, Step.Messages

**Files:**
- Modify: `pkg/types/run.go`
- Test: `pkg/types/run_test.go`

- [ ] **Step 1: Write the failing test**

Create (or extend) `pkg/types/run_test.go`:

```go
package types_test

import (
	"encoding/json"
	"testing"
	"time"
	"github.com/kimitsu-ai/ktsu/pkg/types"
)

func TestApproval_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	a := types.Approval{
		ID:              "appr_abc",
		RunID:           "run_1",
		StepID:          "step_1",
		ToolName:        "delete-records",
		ToolUseID:       "toolu_001",
		Arguments:       map[string]any{"table": "users"},
		Status:          types.ApprovalStatusPending,
		OriginalRequest: json.RawMessage(`{"run_id":"run_1"}`),
		CreatedAt:       now,
	}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var got types.Approval
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "appr_abc" {
		t.Errorf("ID mismatch: %s", got.ID)
	}
	if got.Status != types.ApprovalStatusPending {
		t.Errorf("Status mismatch: %s", got.Status)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

```
go test ./pkg/types/... -run TestApproval -v
```

Expected: compile error — `Approval` and `ApprovalStatus` not defined.

- [ ] **Step 3: Add types to `pkg/types/run.go`**

Add to the existing `run.go` file:

```go
// Add these status constants alongside the existing RunStatus/StepStatus consts:
const (
    StepStatusPendingApproval StepStatus = "pending_approval" // NEW
)

// ApprovalStatus represents the state of an approval request.
type ApprovalStatus string

const (
    ApprovalStatusPending  ApprovalStatus = "pending"
    ApprovalStatusApproved ApprovalStatus = "approved"
    ApprovalStatusRejected ApprovalStatus = "rejected"
)

// Approval records a pending or resolved tool call approval.
type Approval struct {
    ID              string          `json:"id"`
    RunID           string          `json:"run_id"`
    StepID          string          `json:"step_id"`
    ToolName        string          `json:"tool_name"`
    ToolUseID       string          `json:"tool_use_id"`
    Arguments       map[string]any  `json:"arguments"`
    Status          ApprovalStatus  `json:"status"`
    Decision        string          `json:"decision,omitempty"`   // "approve" | "reject"
    Reason          string          `json:"reason,omitempty"`
    OnReject        string          `json:"on_reject"`             // from policy
    TimeoutMS       int64           `json:"timeout_ms"`
    TimeoutBehavior string          `json:"timeout_behavior"`
    OriginalRequest json.RawMessage `json:"original_request"`      // serialized InvokeRequest
    CreatedAt       time.Time       `json:"created_at"`
    ResolvedAt      *time.Time      `json:"resolved_at,omitempty"`
}
```

Add `"encoding/json"` to the imports in `run.go` if not present.

Also add `Messages json.RawMessage` to `Step`:

```go
type Step struct {
    ID        string                 `json:"id"`
    RunID     string                 `json:"run_id"`
    Name      string                 `json:"name"`
    Type      StepType               `json:"type"`
    Status    StepStatus             `json:"status"`
    StartedAt *time.Time             `json:"started_at,omitempty"`
    EndedAt   *time.Time             `json:"ended_at,omitempty"`
    Error     string                 `json:"error,omitempty"`
    Output    map[string]interface{} `json:"output,omitempty"`
    Metrics   StepMetrics            `json:"metrics,omitempty"`
    Messages  json.RawMessage        `json:"messages,omitempty"` // NEW: full conversation context
}
```

- [ ] **Step 4: Run the test to confirm it passes**

```
go test ./pkg/types/... -run TestApproval -v
```

Expected: PASS

- [ ] **Step 5: Run full build**

```
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add pkg/types/run.go pkg/types/run_test.go
git commit -m "feat(types): add Approval record and Step.Messages for approval state"
```

---

## Task 10 — State store: Approval CRUD in Store interface and MemStore

**Files:**
- Modify: `internal/orchestrator/state/store.go`
- Modify: `internal/orchestrator/state/mem_store.go`
- Test: `internal/orchestrator/state/mem_store_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/orchestrator/state/mem_store_test.go` (or extend if it exists):

```go
package state_test

import (
	"context"
	"testing"
	"time"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
	"github.com/kimitsu-ai/ktsu/pkg/types"
)

func TestMemStore_Approval_CreateAndGet(t *testing.T) {
	store := state.NewMemStore()
	ctx := context.Background()

	approval := &types.Approval{
		ID:        "appr_1",
		RunID:     "run_1",
		StepID:    "step_1",
		ToolName:  "delete-records",
		ToolUseID: "toolu_001",
		Status:    types.ApprovalStatusPending,
		CreatedAt: time.Now(),
	}

	if err := store.CreateApproval(ctx, approval); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	got, err := store.GetApproval(ctx, "appr_1")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.ToolName != "delete-records" {
		t.Errorf("ToolName mismatch: %s", got.ToolName)
	}
}

func TestMemStore_Approval_Update(t *testing.T) {
	store := state.NewMemStore()
	ctx := context.Background()

	approval := &types.Approval{
		ID:     "appr_2",
		RunID:  "run_1",
		StepID: "step_1",
		Status: types.ApprovalStatusPending,
	}
	store.CreateApproval(ctx, approval)

	approval.Status = types.ApprovalStatusApproved
	approval.Decision = "approve"
	now := time.Now()
	approval.ResolvedAt = &now

	if err := store.UpdateApproval(ctx, approval); err != nil {
		t.Fatalf("UpdateApproval: %v", err)
	}

	got, _ := store.GetApproval(ctx, "appr_2")
	if got.Status != types.ApprovalStatusApproved {
		t.Errorf("Status not updated: %s", got.Status)
	}
}

func TestMemStore_Approval_ListPending(t *testing.T) {
	store := state.NewMemStore()
	ctx := context.Background()

	store.CreateApproval(ctx, &types.Approval{ID: "a1", Status: types.ApprovalStatusPending})
	store.CreateApproval(ctx, &types.Approval{ID: "a2", Status: types.ApprovalStatusApproved})
	store.CreateApproval(ctx, &types.Approval{ID: "a3", Status: types.ApprovalStatusPending})

	pending, err := store.ListApprovals(ctx, string(types.ApprovalStatusPending))
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Errorf("want 2 pending, got %d", len(pending))
	}
}
```

- [ ] **Step 2: Run to confirm they fail**

```
go test ./internal/orchestrator/state/... -run TestMemStore_Approval -v
```

Expected: compile errors — methods not defined.

- [ ] **Step 3: Add Approval methods to `Store` interface in `store.go`**

```go
type Store interface {
    // ... existing methods ...
    CreateApproval(ctx context.Context, approval *types.Approval) error
    UpdateApproval(ctx context.Context, approval *types.Approval) error
    GetApproval(ctx context.Context, approvalID string) (*types.Approval, error)
    ListApprovals(ctx context.Context, status string) ([]*types.Approval, error)
}
```

- [ ] **Step 4: Add Approval CRUD to `MemStore` in `mem_store.go`**

Add `approvals map[string]*types.Approval` to the `MemStore` struct and initialize in `NewMemStore`:

```go
type MemStore struct {
    mu        sync.RWMutex
    runs      map[string]*types.Run
    steps     map[string]map[string]*types.Step
    approvals map[string]*types.Approval // NEW
}

func NewMemStore() *MemStore {
    return &MemStore{
        runs:      make(map[string]*types.Run),
        steps:     make(map[string]map[string]*types.Step),
        approvals: make(map[string]*types.Approval), // NEW
    }
}
```

Implement the new methods:

```go
func (m *MemStore) CreateApproval(_ context.Context, approval *types.Approval) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if _, exists := m.approvals[approval.ID]; exists {
        return errors.New("approval already exists")
    }
    cp := *approval
    m.approvals[cp.ID] = &cp
    return nil
}

func (m *MemStore) UpdateApproval(_ context.Context, approval *types.Approval) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if _, exists := m.approvals[approval.ID]; !exists {
        return errors.New("approval not found")
    }
    cp := *approval
    m.approvals[cp.ID] = &cp
    return nil
}

func (m *MemStore) GetApproval(_ context.Context, approvalID string) (*types.Approval, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    a, ok := m.approvals[approvalID]
    if !ok {
        return nil, errors.New("approval not found")
    }
    cp := *a
    return &cp, nil
}

func (m *MemStore) ListApprovals(_ context.Context, status string) ([]*types.Approval, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    var result []*types.Approval
    for _, a := range m.approvals {
        if status == "" || string(a.Status) == status {
            cp := *a
            result = append(result, &cp)
        }
    }
    return result, nil
}
```

Also update `copyStep` helper in `mem_store.go` to copy the new `Messages` field:

```go
func copyStep(s *types.Step) *types.Step {
    cp := *s
    if s.Output != nil {
        cp.Output = make(map[string]interface{}, len(s.Output))
        for k, v := range s.Output {
            cp.Output[k] = v
        }
    }
    if s.Messages != nil {
        cp.Messages = append(json.RawMessage{}, s.Messages...)
    }
    cp.Metrics = s.Metrics
    return &cp
}
```

Also add stub implementations to `SQLiteStore` (all return `ErrNotImplemented`):

```go
func (s *SQLiteStore) CreateApproval(_ context.Context, _ *types.Approval) error { return ErrNotImplemented }
func (s *SQLiteStore) UpdateApproval(_ context.Context, _ *types.Approval) error { return ErrNotImplemented }
func (s *SQLiteStore) GetApproval(_ context.Context, _ string) (*types.Approval, error) { return nil, ErrNotImplemented }
func (s *SQLiteStore) ListApprovals(_ context.Context, _ string) ([]*types.Approval, error) { return nil, ErrNotImplemented }
```

Check `store.go` for how `ErrNotImplemented` is defined — it may be the `storeError("not implemented")` constant or similar. Use the same pattern.

- [ ] **Step 5: Run tests to confirm they pass**

```
go test ./internal/orchestrator/state/... -run TestMemStore_Approval -v
```

Expected: PASS

- [ ] **Step 6: Build to confirm no errors**

```
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/orchestrator/state/store.go internal/orchestrator/state/mem_store.go internal/orchestrator/state/mem_store_test.go
git commit -m "feat(state): add Approval CRUD to Store interface and MemStore"
```

---

## Task 11 — Orchestrator callback handler: pending_approval branch

**Files:**
- Modify: `internal/orchestrator/server.go`
- Test: `internal/orchestrator/server_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/orchestrator/server_test.go`:

```go
func TestHandleStepComplete_PendingApproval_StoresApproval(t *testing.T) {
	store := state.NewMemStore()
	ctx := context.Background()

	// Pre-create the run.
	store.CreateRun(ctx, &types.Run{
		ID:           "run_1",
		WorkflowName: "test-wf",
		Status:       types.RunStatusRunning,
	})
	store.CreateStep(ctx, &types.Step{
		ID:     "step_1",
		RunID:  "run_1",
		Status: types.StepStatusRunning,
	})

	// Build an orchestrator server with the MemStore.
	srv := newTestServer(t, store)

	// Original InvokeRequest (as JSON).
	origReq, _ := json.Marshal(agent.InvokeRequest{RunID: "run_1", StepID: "step_1"})

	payload := agent.CallbackPayload{
		RunID:  "run_1",
		StepID: "step_1",
		Status: "pending_approval",
		Messages: []agent.Message{
			{Role: "system", Content: "be helpful"},
		},
		Metrics: agent.Metrics{TokensIn: 10, CostUSD: 0.001},
		PendingApproval: &agent.PendingApproval{
			ToolName:        "delete-records",
			ToolUseID:       "toolu_001",
			Arguments:       map[string]any{"table": "users"},
			OnReject:        "fail",
			TimeoutMS:       0,
			TimeoutBehavior: "reject",
		},
	}

	b, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/runs/run_1/steps/step_1/complete", bytes.NewReader(b))
	req.SetPathValue("run_id", "run_1")
	req.SetPathValue("step_id", "step_1")
	w := httptest.NewRecorder()

	srv.handleStepComplete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	// Approval should be stored.
	approvals, err := store.ListApprovals(ctx, "pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 {
		t.Fatalf("want 1 approval, got %d", len(approvals))
	}
	if approvals[0].ToolName != "delete-records" {
		t.Errorf("wrong tool: %s", approvals[0].ToolName)
	}

	// Step should have messages and partial metrics stored.
	step, _ := store.GetStep(ctx, "run_1", "step_1")
	if len(step.Messages) == 0 {
		t.Error("Step.Messages not populated")
	}
	if step.Metrics.TokensIn != 10 {
		t.Errorf("Metrics.TokensIn not stored: %d", step.Metrics.TokensIn)
	}

	// The dispatcher channel should NOT have been signaled (no pending waiter = OK here).
}
```

Note: `newTestServer` is a helper that constructs the unexported `server` struct for testing. You'll need to add an exported constructor or use the public `Orchestrator.Start` path in integration. For a pure unit test, expose `newServer` via a test file or add a `NewServerForTest` function.

- [ ] **Step 2: Run to confirm it fails**

```
go test ./internal/orchestrator/... -run TestHandleStepComplete_PendingApproval -v
```

Expected: compile error or failure.

- [ ] **Step 3: Update `handleStepComplete` in `server.go`**

Add a new branch at the start of `handleStepComplete` for `pending_approval`, before the reserved-fields logic:

```go
func (s *server) handleStepComplete(w http.ResponseWriter, r *http.Request) {
    runID := r.PathValue("run_id")
    stepID := r.PathValue("step_id")

    var payload agent.CallbackPayload
    if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
        http.Error(w, "invalid payload", http.StatusBadRequest)
        return
    }

    // --- NEW: pending_approval branch ---
    if payload.Status == "pending_approval" {
        s.handlePendingApproval(r.Context(), runID, stepID, payload)
        w.WriteHeader(http.StatusOK)
        return
    }
    // --- end pending_approval branch ---

    // Existing: process reserved fields ...
    // (unchanged from current code)
    ...
}

// handlePendingApproval stores the approval record and partial metrics.
// It does NOT signal the pending dispatcher channel — the step remains suspended.
func (s *server) handlePendingApproval(ctx context.Context, runID, stepID string, payload agent.CallbackPayload) {
    pa := payload.PendingApproval
    if pa == nil {
        log.Printf("handlePendingApproval: missing PendingApproval in payload for %s/%s", runID, stepID)
        return
    }

    // Serialize message context and store on the step.
    messagesJSON, _ := json.Marshal(payload.Messages)
    step, err := s.store.GetStep(ctx, runID, stepID)
    if err != nil {
        log.Printf("handlePendingApproval: get step %s/%s: %v", runID, stepID, err)
        return
    }
    step.Messages = messagesJSON
    step.Status = types.StepStatusPendingApproval
    step.Metrics = types.StepMetrics{
        TokensIn:  payload.Metrics.TokensIn,
        TokensOut: payload.Metrics.TokensOut,
        CostUSD:   payload.Metrics.CostUSD,
        DurationMS: payload.Metrics.DurationMS,
        LLMCalls:  payload.Metrics.LLMCalls,
        ToolCalls: payload.Metrics.ToolCalls,
    }
    if err := s.store.UpdateStep(ctx, step); err != nil {
        log.Printf("handlePendingApproval: update step: %v", err)
    }

    // Generate approval ID.
    b := make([]byte, 8)
    rand.Read(b)
    approvalID := "appr_" + hex.EncodeToString(b)

    // Store original InvokeRequest for later re-dispatch.
    // We don't have it here yet — it will be set by the decide endpoint.
    // For now, store what we know. The decide endpoint fetches the step messages.
    approval := &types.Approval{
        ID:              approvalID,
        RunID:           runID,
        StepID:          stepID,
        ToolName:        pa.ToolName,
        ToolUseID:       pa.ToolUseID,
        Arguments:       pa.Arguments,
        Status:          types.ApprovalStatusPending,
        OnReject:        pa.OnReject,
        TimeoutMS:       pa.TimeoutMS,
        TimeoutBehavior: pa.TimeoutBehavior,
        CreatedAt:       time.Now(),
    }
    if err := s.store.CreateApproval(ctx, approval); err != nil {
        log.Printf("handlePendingApproval: create approval: %v", err)
        return
    }

    log.Printf("approval %s created for %s/%s tool=%s", approvalID, runID, stepID, pa.ToolName)

    // Start timeout goroutine if configured.
    if pa.TimeoutMS > 0 {
        go s.runApprovalTimeout(approvalID, time.Duration(pa.TimeoutMS)*time.Millisecond)
    }

    // Fire on:approval webhook steps (fire-and-forget).
    go s.fireApprovalWebhooks(context.Background(), runID, stepID, approval)
}
```

Also add the `runApprovalTimeout` stub (used in a later task):

```go
func (s *server) runApprovalTimeout(approvalID string, d time.Duration) {
    time.Sleep(d)
    // Implemented fully in Task 12.
    log.Printf("approval timeout fired for %s (not yet implemented)", approvalID)
}
```

And the `fireApprovalWebhooks` stub (implemented in Task 14):

```go
func (s *server) fireApprovalWebhooks(ctx context.Context, runID, stepID string, approval *types.Approval) {
    // Implemented in Task 14.
}
```

- [ ] **Step 4: Store also needs to handle `StepStatusPendingApproval` — verify it compiles**

`pkg/types/run.go` already has `StepStatusPendingApproval = "pending_approval"` from Task 9. If the `StepMetrics` conversion above references fields that differ from `agent.Metrics`, align them (field names should match as per tasks 3 and 9).

- [ ] **Step 5: Run tests**

```
go test ./internal/orchestrator/... -run TestHandleStepComplete_PendingApproval -v
```

- [ ] **Step 6: Build**

```
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/orchestrator/server.go
git commit -m "feat(orchestrator): handle pending_approval callback — store approval and partial metrics"
```

---

## Task 12 — Orchestrator: decide endpoints + approval resolution

**Files:**
- Modify: `internal/orchestrator/server.go`

The decide endpoint resolves an approval (approve or reject) and re-dispatches the step to the runtime with the appropriate context.

- [ ] **Step 1: Write the failing test**

Add to `server_test.go`:

```go
func TestHandleApprovalDecide_Approve_ReDispatchesToRuntime(t *testing.T) {
    reInvokeCalled := false
    // Fake runtime that accepts the re-invoke and returns a successful callback later.
    fakeRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var req agent.InvokeRequest
        json.NewDecoder(r.Body).Decode(&req)
        if req.IsResume && len(req.ApprovedToolCalls) > 0 {
            reInvokeCalled = true
        }
        w.WriteHeader(http.StatusAccepted)
        json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
    }))
    defer fakeRuntime.Close()

    store := state.NewMemStore()
    ctx := context.Background()

    // Pre-populate state.
    messagesJSON, _ := json.Marshal([]agent.Message{
        {Role: "system", Content: "be helpful"},
        {Role: "assistant", Content: `[{"type":"tool_use","id":"toolu_001","name":"delete-records","input":{"table":"users"}}]`},
    })
    origReq, _ := json.Marshal(agent.InvokeRequest{
        RunID: "run_1", StepID: "step_1",
        System: "be helpful", MaxTurns: 5,
        Model: agent.ModelSpec{Group: "standard", MaxTokens: 1024},
        CallbackURL: "http://orch/runs/run_1/steps/step_1/complete",
        ToolServers: []agent.ToolServerSpec{{Name: "s", URL: "http://s", Allowlist: []string{"delete-records"}}},
        OutputSchema: map[string]any{"type": "object"},
    })
    store.CreateRun(ctx, &types.Run{ID: "run_1", WorkflowName: "wf", Status: types.RunStatusRunning})
    store.CreateStep(ctx, &types.Step{
        ID: "step_1", RunID: "run_1",
        Status: types.StepStatusPendingApproval,
        Messages: messagesJSON,
    })
    store.CreateApproval(ctx, &types.Approval{
        ID: "appr_1", RunID: "run_1", StepID: "step_1",
        ToolName: "delete-records", ToolUseID: "toolu_001",
        Status: types.ApprovalStatusPending,
        OnReject: "fail",
        OriginalRequest: origReq,
    })

    srv := newTestServerWithRuntime(t, store, fakeRuntime.URL)

    body := strings.NewReader(`{"decision":"approve","reason":"looks fine"}`)
    req := httptest.NewRequest("POST", "/runs/run_1/steps/step_1/approval/decide", body)
    req.SetPathValue("run_id", "run_1")
    req.SetPathValue("step_id", "step_1")
    w := httptest.NewRecorder()

    srv.handleApprovalDecide(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
    }
    if !reInvokeCalled {
        t.Error("expected runtime to be re-invoked with IsResume=true and ApprovedToolCalls set")
    }

    // Approval should be updated to approved.
    appr, _ := store.GetApproval(ctx, "appr_1")
    if appr.Status != types.ApprovalStatusApproved {
        t.Errorf("approval not marked approved: %s", appr.Status)
    }
}
```

- [ ] **Step 2: Run to confirm it fails**

```
go test ./internal/orchestrator/... -run TestHandleApprovalDecide -v
```

- [ ] **Step 3: Add decide endpoints to `routes()` in `server.go`**

```go
func (s *server) routes() {
    // ... existing routes ...
    s.mux.HandleFunc("GET /runs/{run_id}/steps/{step_id}/approval", s.handleGetApproval)
    s.mux.HandleFunc("POST /runs/{run_id}/steps/{step_id}/approval/decide", s.handleApprovalDecide)
    s.mux.HandleFunc("GET /approvals", s.handleListApprovals)
}
```

- [ ] **Step 4: Implement `handleGetApproval`, `handleListApprovals`, and `handleApprovalDecide`**

```go
func (s *server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
    runID := r.PathValue("run_id")
    stepID := r.PathValue("step_id")

    approvals, err := s.store.ListApprovals(r.Context(), "")
    if err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    for _, a := range approvals {
        if a.RunID == runID && a.StepID == stepID {
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(a)
            return
        }
    }
    http.Error(w, "no approval for this step", http.StatusNotFound)
}

func (s *server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
    status := r.URL.Query().Get("status")
    approvals, err := s.store.ListApprovals(r.Context(), status)
    if err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(approvals)
}

func (s *server) handleApprovalDecide(w http.ResponseWriter, r *http.Request) {
    runID := r.PathValue("run_id")
    stepID := r.PathValue("step_id")

    var body struct {
        Decision string `json:"decision"` // "approve" | "reject"
        Reason   string `json:"reason"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Decision != "approve" && body.Decision != "reject") {
        http.Error(w, `decision must be "approve" or "reject"`, http.StatusBadRequest)
        return
    }

    // Find the pending approval for this step.
    approvals, err := s.store.ListApprovals(r.Context(), string(types.ApprovalStatusPending))
    if err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    var approval *types.Approval
    for _, a := range approvals {
        if a.RunID == runID && a.StepID == stepID {
            approval = a
            break
        }
    }
    if approval == nil {
        http.Error(w, "no pending approval for this step", http.StatusNotFound)
        return
    }

    now := time.Now()
    approval.Decision = body.Decision
    approval.Reason = body.Reason
    approval.ResolvedAt = &now

    switch body.Decision {
    case "approve":
        approval.Status = types.ApprovalStatusApproved
        if err := s.store.UpdateApproval(r.Context(), approval); err != nil {
            http.Error(w, "internal error", http.StatusInternalServerError)
            return
        }
        go s.redispatchApproved(context.Background(), runID, stepID, approval)

    case "reject":
        approval.Status = types.ApprovalStatusRejected
        if err := s.store.UpdateApproval(r.Context(), approval); err != nil {
            http.Error(w, "internal error", http.StatusInternalServerError)
            return
        }
        if approval.OnReject == "fail" {
            // Signal the dispatcher channel with a failed payload.
            s.signalStep(runID, stepID, agent.CallbackPayload{
                RunID:  runID,
                StepID: stepID,
                Status: "failed",
                Error:  fmt.Sprintf("tool_call_rejected: %s: %s", approval.ToolName, body.Reason),
            })
        } else {
            // Recover: inject rejection tool result and re-dispatch.
            go s.redispatchRejected(context.Background(), runID, stepID, approval, body.Reason)
        }
    }

    w.WriteHeader(http.StatusOK)
}

// signalStep sends a payload to the dispatcher channel (used for reject+fail and timeout).
func (s *server) signalStep(runID, stepID string, payload agent.CallbackPayload) {
    key := stepCallbackKey{runID, stepID}
    s.pendingMu.Lock()
    ch, ok := s.pendingCallbacks[key]
    s.pendingMu.Unlock()
    if ok {
        ch <- payload
    }
}

// redispatchApproved re-invokes the runtime with the checkpoint context + approved tool call.
func (s *server) redispatchApproved(ctx context.Context, runID, stepID string, approval *types.Approval) {
    step, err := s.store.GetStep(ctx, runID, stepID)
    if err != nil {
        log.Printf("redispatchApproved: get step: %v", err)
        return
    }

    var origReq agent.InvokeRequest
    if err := json.Unmarshal(approval.OriginalRequest, &origReq); err != nil {
        log.Printf("redispatchApproved: unmarshal original request: %v", err)
        return
    }

    var messages []agent.Message
    if err := json.Unmarshal(step.Messages, &messages); err != nil {
        log.Printf("redispatchApproved: unmarshal messages: %v", err)
        return
    }

    resumeReq := origReq
    resumeReq.Messages = messages
    resumeReq.ApprovedToolCalls = []string{approval.ToolUseID}
    resumeReq.IsResume = true
    resumeReq.CallbackURL = s.o.cfg.OwnURL + "/runs/" + runID + "/steps/" + stepID + "/complete"

    s.postToRuntime(ctx, resumeReq)
}

// redispatchRejected injects a rejection tool result and re-invokes the runtime.
func (s *server) redispatchRejected(ctx context.Context, runID, stepID string, approval *types.Approval, reason string) {
    step, err := s.store.GetStep(ctx, runID, stepID)
    if err != nil {
        log.Printf("redispatchRejected: get step: %v", err)
        return
    }

    var origReq agent.InvokeRequest
    if err := json.Unmarshal(approval.OriginalRequest, &origReq); err != nil {
        log.Printf("redispatchRejected: unmarshal original request: %v", err)
        return
    }

    var messages []agent.Message
    if err := json.Unmarshal(step.Messages, &messages); err != nil {
        log.Printf("redispatchRejected: unmarshal messages: %v", err)
        return
    }

    // Inject synthetic rejection tool result.
    rejectionContent, _ := json.Marshal([]map[string]any{{
        "type":        "tool_result",
        "tool_use_id": approval.ToolUseID,
        "content":     fmt.Sprintf("Tool call rejected by operator: %s", reason),
    }})
    messages = append(messages, agent.Message{Role: "user", Content: string(rejectionContent)})

    resumeReq := origReq
    resumeReq.Messages = messages
    resumeReq.IsResume = true
    resumeReq.CallbackURL = s.o.cfg.OwnURL + "/runs/" + runID + "/steps/" + stepID + "/complete"

    s.postToRuntime(ctx, resumeReq)
}

// postToRuntime POSTs an InvokeRequest to the runtime /invoke endpoint.
func (s *server) postToRuntime(ctx context.Context, req agent.InvokeRequest) {
    body, err := json.Marshal(req)
    if err != nil {
        log.Printf("postToRuntime: marshal: %v", err)
        return
    }
    httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.o.cfg.RuntimeURL+"/invoke", bytes.NewReader(body))
    if err != nil {
        log.Printf("postToRuntime: create request: %v", err)
        return
    }
    httpReq.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(httpReq)
    if err != nil {
        log.Printf("postToRuntime: %v", err)
        return
    }
    resp.Body.Close()
}
```

Also store the `OriginalRequest` in the approval: the `handlePendingApproval` function doesn't currently have the original InvokeRequest. The dispatcher needs to store it. The cleanest place: the dispatcher stores the serialized InvokeRequest in the `Approval` record just before POSTing to the runtime.

Update the flow: the `runtimeDispatcher` stores the InvokeRequest in the Approval record AFTER the `pending_approval` callback is handled. However, since the dispatcher blocks, this can't happen inside Dispatch(). Instead, store the original request in a side-channel.

Simplest fix: the `handlePendingApproval` doesn't need the original request — the `redispatchApproved` can reconstruct the InvokeRequest from what's needed (step messages, stored tool server configs). But that requires re-loading the agent config.

Alternative: store the serialized InvokeRequest in the `Approval` record from within `handleStepComplete` by passing the original request there. This requires the dispatcher to include the serialized InvokeRequest in the `CallbackPayload`.

**Add `OriginalRequest json.RawMessage` to `CallbackPayload`** (update `internal/runtime/agent/types.go`):

```go
type CallbackPayload struct {
    // ... existing fields ...
    OriginalRequest json.RawMessage `json:"original_request,omitempty"` // NEW: set by runtime on pending_approval
}
```

And in the loop's `Run()` method, when returning a `pending_approval` payload, include the serialized original request:

```go
var paErr *errPendingApproval
if errors.As(err, &paErr) {
    origReqJSON, _ := json.Marshal(req)
    payload.Status = "pending_approval"
    payload.PendingApproval = &paErr.PendingApproval
    payload.OriginalRequest = origReqJSON
    return payload
}
```

Then in `handlePendingApproval`, store `payload.OriginalRequest` in the `Approval.OriginalRequest` field.

Update `handlePendingApproval` to include this:

```go
approval := &types.Approval{
    ...
    OriginalRequest: payload.OriginalRequest, // from the callback
    ...
}
```

- [ ] **Step 5: Run tests**

```
go test ./internal/orchestrator/... -run "TestHandleApproval" -v
```

- [ ] **Step 6: Build**

```
go build ./...
```

- [ ] **Step 7: Implement `runApprovalTimeout` (replace the stub)**

```go
func (s *server) runApprovalTimeout(approvalID string, d time.Duration) {
    time.Sleep(d)
    ctx := context.Background()
    approval, err := s.store.GetApproval(ctx, approvalID)
    if err != nil || approval.Status != types.ApprovalStatusPending {
        return // already resolved
    }
    log.Printf("approval %s timed out", approvalID)
    now := time.Now()
    approval.ResolvedAt = &now

    switch approval.TimeoutBehavior {
    case "reject":
        approval.Status = types.ApprovalStatusRejected
        approval.Decision = "reject"
        approval.Reason = "approval timeout"
        s.store.UpdateApproval(ctx, approval)
        if approval.OnReject == "fail" {
            s.signalStep(approval.RunID, approval.StepID, agent.CallbackPayload{
                RunID:  approval.RunID,
                StepID: approval.StepID,
                Status: "failed",
                Error:  fmt.Sprintf("tool_call_rejected: %s: approval timeout", approval.ToolName),
            })
        } else {
            go s.redispatchRejected(ctx, approval.RunID, approval.StepID, approval, "approval timeout")
        }
    default: // "fail"
        approval.Status = types.ApprovalStatusRejected
        approval.Decision = "reject"
        approval.Reason = "approval timeout"
        s.store.UpdateApproval(ctx, approval)
        s.signalStep(approval.RunID, approval.StepID, agent.CallbackPayload{
            RunID:  approval.RunID,
            StepID: approval.StepID,
            Status: "failed",
            Error:  fmt.Sprintf("tool_call_rejected: %s: approval timeout", approval.ToolName),
        })
    }
}
```

- [ ] **Step 8: Commit**

```bash
git add internal/orchestrator/server.go internal/runtime/agent/types.go internal/runtime/agent/loop.go
git commit -m "feat(orchestrator): add approval decide endpoints, re-dispatch logic, and timeout handling"
```

---

## Task 13 — Orchestrator: cumulative metrics on resume

**Files:**
- Modify: `internal/orchestrator/server.go`
- Test: `internal/orchestrator/server_test.go`

When a `pending_approval` step is resumed and the final `ok`/`failed` callback arrives with `IsResume: true`, add the new metrics to the existing partial metrics stored on the step rather than overwriting.

- [ ] **Step 1: Write the failing test**

Add to `server_test.go`:

```go
func TestHandleStepComplete_ResumeMergesMetrics(t *testing.T) {
    store := state.NewMemStore()
    ctx := context.Background()

    // Pre-create step with existing partial metrics (from leg 1).
    store.CreateRun(ctx, &types.Run{ID: "run_1", Status: types.RunStatusRunning})
    store.CreateStep(ctx, &types.Step{
        ID: "step_1", RunID: "run_1",
        Status: types.StepStatusPendingApproval,
        Metrics: types.StepMetrics{TokensIn: 50, TokensOut: 20, CostUSD: 0.005, LLMCalls: 2, ToolCalls: 1, DurationMS: 500},
    })

    // Register a pending channel so the callback can signal it.
    pendingCallbacks := make(map[stepCallbackKey]chan agent.CallbackPayload)
    ch := make(chan agent.CallbackPayload, 1)
    pendingCallbacks[stepCallbackKey{"run_1", "step_1"}] = ch

    srv := newTestServerWithCallbacks(t, store, pendingCallbacks)

    // Leg 2 callback: ok, IsResume=true, new metrics.
    payload := agent.CallbackPayload{
        RunID:  "run_1",
        StepID: "step_1",
        Status: "ok",
        Output: map[string]any{"result": "done"},
        Metrics: agent.Metrics{TokensIn: 30, TokensOut: 10, CostUSD: 0.003, LLMCalls: 1, ToolCalls: 1, DurationMS: 300},
        IsResume: true,
    }

    b, _ := json.Marshal(payload)
    req := httptest.NewRequest("POST", "/runs/run_1/steps/step_1/complete", bytes.NewReader(b))
    req.SetPathValue("run_id", "run_1")
    req.SetPathValue("step_id", "step_1")
    w := httptest.NewRecorder()

    srv.handleStepComplete(w, req)

    // Read from channel.
    received := <-ch

    if received.Metrics.TokensIn != 80 { // 50 + 30
        t.Errorf("TokensIn: want 80, got %d", received.Metrics.TokensIn)
    }
    if received.Metrics.CostUSD != 0.008 { // 0.005 + 0.003
        t.Errorf("CostUSD: want 0.008, got %f", received.Metrics.CostUSD)
    }
    if received.Metrics.LLMCalls != 3 { // 2 + 1
        t.Errorf("LLMCalls: want 3, got %d", received.Metrics.LLMCalls)
    }
}
```

- [ ] **Step 2: Run to confirm it fails**

```
go test ./internal/orchestrator/... -run TestHandleStepComplete_ResumeMergesMetrics -v
```

- [ ] **Step 3: Add `IsResume` to `CallbackPayload` and update `handleStepComplete`**

`CallbackPayload` already has `IsResume` added in Task 12 (via the loop adding it). If not yet added, add:

```go
// In internal/runtime/agent/types.go, CallbackPayload:
IsResume bool `json:"is_resume,omitempty"`
```

And in the loop's `Run()`, when `req.IsResume == true`, pass it through to the callback:

```go
payload.IsResume = req.IsResume
```

In `handleStepComplete` (in `server.go`), after the reserved-fields processing block, before signaling the channel, check for resume and merge:

```go
// Merge metrics if this is a resume leg.
if payload.IsResume {
    existing, err := s.store.GetStep(r.Context(), runID, stepID)
    if err == nil && existing.Status == types.StepStatusPendingApproval {
        payload.Metrics.TokensIn += existing.Metrics.TokensIn
        payload.Metrics.TokensOut += existing.Metrics.TokensOut
        payload.Metrics.CostUSD += existing.Metrics.CostUSD
        payload.Metrics.DurationMS += existing.Metrics.DurationMS
        payload.Metrics.LLMCalls += existing.Metrics.LLMCalls
        payload.Metrics.ToolCalls += existing.Metrics.ToolCalls
    }
}

// Signal the waiting runner goroutine.
key := stepCallbackKey{runID, stepID}
...
```

- [ ] **Step 4: Run tests**

```
go test ./internal/orchestrator/... -run TestHandleStepComplete_ResumeMergesMetrics -v
```

Expected: PASS

- [ ] **Step 5: Run full test suite**

```
go test ./... -v 2>&1 | tail -40
```

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/server.go internal/runtime/agent/types.go internal/runtime/agent/loop.go
git commit -m "feat(orchestrator): accumulate metrics across resume legs for approval-gated steps"
```

---

## Task 14 — on:approval webhook step

**Files:**
- Modify: `internal/config/types.go` — add `On string` to `PipelineStep`
- Modify: `internal/orchestrator/server.go` — implement `fireApprovalWebhooks`
- Test: `internal/orchestrator/server_test.go`

- [ ] **Step 1: Add `On` to `PipelineStep` in `internal/config/types.go`**

Find the `PipelineStep` struct. Add one field:

```go
type PipelineStep struct {
    ID                  string              `yaml:"id"`
    Agent               string              `yaml:"agent,omitempty"`
    Transform           *TransformSpec      `yaml:"transform,omitempty"`
    Webhook             *WebhookSpec        `yaml:"webhook,omitempty"`
    ForEach             *ForEachSpec        `yaml:"for_each,omitempty"`
    DependsOn           []string            `yaml:"depends_on,omitempty"`
    Condition           string              `yaml:"condition,omitempty"`
    ConfidenceThreshold float64             `yaml:"confidence_threshold,omitempty"`
    Params              map[string]any      `yaml:"params,omitempty"`
    Model               *ModelSpec          `yaml:"model,omitempty"`
    Consolidation       string              `yaml:"consolidation,omitempty"`
    Output              map[string]any      `yaml:"output,omitempty"`
    On                  string              `yaml:"on,omitempty"` // NEW: "approval" for approval-triggered webhooks
}
```

- [ ] **Step 2: Write the failing test for webhook firing**

Add to `server_test.go`:

```go
func TestFireApprovalWebhooks_CallsConfiguredWebhook(t *testing.T) {
    var received map[string]any
    webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        json.NewDecoder(r.Body).Decode(&received)
        w.WriteHeader(http.StatusOK)
    }))
    defer webhookServer.Close()

    // Write a workflow YAML with an on:approval webhook step.
    dir := t.TempDir()
    wfYAML := fmt.Sprintf(`
name: test-wf
input:
  schema:
    type: object
pipeline:
  - id: process-data
    agent: agents/test.agent.yaml
  - id: notify-approver
    type: webhook
    on: approval
    depends_on: [process-data]
    webhook:
      url: %s
      method: POST
      body:
        run_id: "{{ run_id }}"
        tool_name: "{{ pending_approval.tool_name }}"
`, webhookServer.URL)
    writeFile(t, dir+"/workflows/test-wf.workflow.yaml", wfYAML)

    store := state.NewMemStore()
    store.CreateRun(context.Background(), &types.Run{
        ID: "run_1", WorkflowName: "test-wf", Status: types.RunStatusRunning,
    })

    approval := &types.Approval{
        ID: "appr_1", RunID: "run_1", StepID: "process-data",
        ToolName: "delete-records", ToolUseID: "toolu_001",
    }

    srv := newTestServerWithWorkflowDir(t, store, dir)
    srv.fireApprovalWebhooks(context.Background(), "run_1", "process-data", approval)

    // Give the goroutine time to fire.
    time.Sleep(50 * time.Millisecond)

    if received["tool_name"] != "delete-records" {
        t.Errorf("webhook body missing tool_name: %v", received)
    }
}
```

- [ ] **Step 3: Run to confirm it fails**

```
go test ./internal/orchestrator/... -run TestFireApprovalWebhooks -v
```

- [ ] **Step 4: Implement `fireApprovalWebhooks` in `server.go`**

Replace the stub with the full implementation:

```go
func (s *server) fireApprovalWebhooks(ctx context.Context, runID, stepID string, approval *types.Approval) {
    run, err := s.store.GetRun(ctx, runID)
    if err != nil {
        log.Printf("fireApprovalWebhooks: get run: %v", err)
        return
    }

    wfPath := filepath.Join(s.o.cfg.WorkflowDir, run.WorkflowName+".workflow.yaml")
    wf, err := config.LoadWorkflow(wfPath)
    if err != nil {
        log.Printf("fireApprovalWebhooks: load workflow: %v", err)
        return
    }

    approveURL := s.o.cfg.OwnURL + "/runs/" + runID + "/steps/" + stepID + "/approval/decide"

    for _, step := range wf.Pipeline {
        if step.On != "approval" || step.Webhook == nil {
            continue
        }
        // Check depends_on includes the suspended step.
        dependsOnSuspended := false
        for _, dep := range step.DependsOn {
            if dep == stepID {
                dependsOnSuspended = true
                break
            }
        }
        if !dependsOnSuspended {
            continue
        }

        // Build webhook body by evaluating the body map.
        // Replace template variables: {{ run_id }}, {{ pending_approval.tool_name }}, etc.
        body := buildApprovalWebhookBody(step.Webhook.Body, runID, stepID, approveURL, approval)

        go s.sendApprovalWebhook(ctx, step.Webhook.URL, body)
    }
}

// buildApprovalWebhookBody substitutes approval template variables in the body map.
func buildApprovalWebhookBody(template map[string]any, runID, stepID, approveURL string, approval *types.Approval) map[string]any {
    vars := map[string]string{
        "{{ run_id }}":                      runID,
        "{{ step_id }}":                     stepID,
        "{{ approval_url }}":                approveURL,
        "{{ rejection_url }}":               approveURL,
        "{{ pending_approval.tool_name }}":  approval.ToolName,
        "{{ pending_approval.tool_use_id }}": approval.ToolUseID,
    }
    result := make(map[string]any, len(template))
    for k, v := range template {
        if s, ok := v.(string); ok {
            for tmpl, val := range vars {
                s = strings.ReplaceAll(s, tmpl, val)
            }
            result[k] = s
        } else {
            result[k] = v
        }
    }
    return result
}

// sendApprovalWebhook POSTs the body to the given URL (fire-and-forget).
func (s *server) sendApprovalWebhook(ctx context.Context, url string, body map[string]any) {
    b, err := json.Marshal(body)
    if err != nil {
        log.Printf("sendApprovalWebhook: marshal: %v", err)
        return
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
    if err != nil {
        log.Printf("sendApprovalWebhook: create request: %v", err)
        return
    }
    req.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        log.Printf("sendApprovalWebhook: POST %s: %v", url, err)
        return
    }
    resp.Body.Close()
    log.Printf("approval webhook fired: %s status=%d", url, resp.StatusCode)
}
```

Add `"strings"` to imports if not already present.

- [ ] **Step 5: Run tests**

```
go test ./internal/orchestrator/... -run TestFireApprovalWebhooks -v
go test ./internal/config/... -v
```

- [ ] **Step 6: Run all tests**

```
go test ./... 2>&1 | tail -30
```

- [ ] **Step 7: Commit**

```bash
git add internal/config/types.go internal/orchestrator/server.go
git commit -m "feat: add on:approval webhook step type and fire approval webhooks on pending_approval"
```

---

## Task 15 — Documentation

**Files:**
- Modify: `docs/kimitsu-tool-servers.md`
- Modify: `docs/kimitsu-configuration.md`
- Modify: `docs/kimitsu-runtime.md`

- [ ] **Step 1: Update `docs/kimitsu-tool-servers.md`**

Add a section after the existing allowlist documentation:

```markdown
## Requiring Approval for Dangerous Tool Calls

Any tool in the allowlist can be marked as requiring approval before it executes.
Use the object form for that entry and add a `require_approval` block:

```yaml
access:
  allowlist:
    - "read-*"               # no approval needed
    - name: "delete-*"
      require_approval:
        on_reject: fail      # fail | recover
        timeout: 30m         # optional; omit for no timeout
        timeout_behavior: reject  # fail | reject
```

**`on_reject` values:**
- `fail` — step and run are marked failed on rejection
- `recover` — the agent sees a "rejected" tool result and can reason about alternatives

**`timeout` / `timeout_behavior`:** If the approval is not resolved within `timeout`, the
orchestrator applies `timeout_behavior`: treat as a rejection (`reject`) or hard failure (`fail`).
Omit `timeout` for no automatic expiry.
```

- [ ] **Step 2: Update `docs/kimitsu-configuration.md`**

Add to the boot validation section:

```markdown
### Approval Policy Validation

For each server ref with `require_approval` entries:
- `on_reject` must be `"fail"` or `"recover"`
- If `timeout` is set, `timeout_behavior` must be `"fail"` or `"reject"`
```

- [ ] **Step 3: Update `docs/kimitsu-runtime.md`**

Add a section explaining the suspend/resume flow:

```markdown
## Manual Approval Checkpoints

When an agent attempts to call a tool that has `require_approval` configured, the runtime
suspends the step rather than calling the tool. It sends a `pending_approval` callback to
the orchestrator with the full message context and pending tool call details.

The runtime is stateless — the step can be resumed by any runtime instance after an operator
approves or rejects the call via:

```
POST /runs/{run_id}/steps/{step_id}/approval/decide
{"decision": "approve", "reason": "optional"}
```

**Full message context** is always included in every callback payload (`messages` field),
enabling debugging of any completed step regardless of approvals.
```

- [ ] **Step 4: Commit**

```bash
git add docs/kimitsu-tool-servers.md docs/kimitsu-configuration.md docs/kimitsu-runtime.md
git commit -m "docs: document manual approval configuration and suspend/resume flow"
```

---

## Self-Review Checklist

- [x] **Spec coverage:** All spec sections covered — config types (Task 1–2), runtime types (Task 3), dispatcher (Task 4), pattern matching (Task 5), message capture (Task 6), checkpoint (Task 7), resume (Task 8), state (Tasks 9–10), callback handler (Task 11), decide endpoints (Task 12), cumulative metrics (Task 13), approval webhook (Task 14), docs (Task 15).
- [x] **Placeholder scan:** No TBD or TODO in code blocks.
- [x] **Type consistency:** `ToolApprovalRule` defined in Task 3, used in Tasks 4, 5, 7. `PendingApproval` defined in Task 3, used in Tasks 7, 11, 12. `Approval` defined in Task 9, used in Tasks 10, 11, 12. `StepStatusPendingApproval` defined in Task 9, used in Task 11. `signalStep` defined in Task 12, used in Tasks 12 and 13 (timeout). `matchesPattern`/`findApprovalRule` defined in Task 5, used in Task 7. `errPendingApproval` defined in Task 7, used in `Run()`. `IsResume` in `InvokeRequest` (Task 3), set by loop in Task 8, used for metrics in Task 13. `OriginalRequest` in `CallbackPayload` added in Task 12, set by loop in Task 12 via `Run()` update.
