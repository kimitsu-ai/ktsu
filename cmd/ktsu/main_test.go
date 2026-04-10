package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInvokeCmd_postsToOrchestrator verifies that ktsu invoke sends a POST /invoke/{workflow}
// with the provided JSON input and prints the returned run_id.
func TestInvokeCmd_postsToOrchestrator(t *testing.T) {
	var receivedPath string
	var receivedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"run_id": "test-run-001"})
	}))
	defer srv.Close()

	cmd := invokeCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().Set("orchestrator", srv.URL)
	cmd.Flags().Set("input", `{"name":"world"}`)

	if err := cmd.RunE(cmd, []string{"hello"}); err != nil {
		t.Fatalf("invokeCmd.RunE: %v", err)
	}

	if receivedPath != "/invoke/hello" {
		t.Errorf("want POST /invoke/hello, got %s", receivedPath)
	}
	if receivedBody["name"] != "world" {
		t.Errorf("want body.name=world, got %v", receivedBody["name"])
	}
}

// TestInvokeCmd_wait_pollsUntilDone verifies --wait polls GET /runs/{run_id} until status is done.
func TestInvokeCmd_wait_pollsUntilDone(t *testing.T) {
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			json.NewEncoder(w).Encode(map[string]interface{}{"run_id": "run-wait-001"})
			return
		}
		// GET /runs/{run_id}: first call returns "running", second returns "completed"
		callCount++
		status := "running"
		if callCount >= 2 {
			status = "completed"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"run": map[string]interface{}{"id": "run-wait-001", "status": status},
		})
	}))
	defer srv.Close()

	cmd := invokeCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().Set("orchestrator", srv.URL)
	cmd.Flags().Set("input", "{}")
	cmd.Flags().Set("wait", "true")

	if err := cmd.RunE(cmd, []string{"hello"}); err != nil {
		t.Fatalf("invokeCmd.RunE with --wait: %v", err)
	}

	if callCount < 2 {
		t.Errorf("want at least 2 poll calls, got %d", callCount)
	}
}

// TestValidateCmd_detectsCycle verifies that validate reports a cycle in workflow DAG.
func TestValidateCmd_detectsCycle(t *testing.T) {
	dir := t.TempDir()

	// Write a workflow with a cycle: step_a -> step_b -> step_a
	cycleWorkflow := `
name: cycle-test
pipeline:
  - id: step_a
    depends_on: [step_b]
    webhook:
      url: http://example.com
  - id: step_b
    depends_on: [step_a]
    webhook:
      url: http://example.com
`
	if err := writeFile(dir+"/cycle.workflow.yaml", cycleWorkflow); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", dir)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("want error for cyclic workflow, got nil")
	}
}

// TestValidateCmd_detectsMissingDependency verifies validate reports an unknown step reference.
func TestValidateCmd_detectsMissingDependency(t *testing.T) {
	dir := t.TempDir()

	badWorkflow := `
name: missing-dep
pipeline:
  - id: step_a
    depends_on: [no_such_step]
    webhook:
      url: http://example.com
`
	if err := writeFile(dir+"/bad.workflow.yaml", badWorkflow); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", dir)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("want error for missing dependency, got nil")
	}
}

// TestValidateCmd_passesForValidWorkflow verifies a well-formed workflow returns no error.
func TestValidateCmd_passesForValidWorkflow(t *testing.T) {
	dir := t.TempDir()

	goodWorkflow := `
name: good-workflow
pipeline:
  - id: step_a
    webhook:
      url: http://example.com
  - id: step_b
    depends_on: [step_a]
    webhook:
      url: http://example.com
`
	if err := writeFile(dir+"/good.workflow.yaml", goodWorkflow); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", dir)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("want no error for valid workflow, got: %v", err)
	}
}

// TestValidateCmd_detectsOrphanErrors verifies that validate reports errors in agent files
// even if they are not referenced by any workflow.
func TestValidateCmd_detectsOrphanErrors(t *testing.T) {
	dir := t.TempDir()

	// Write a valid workflow
	goodWorkflow := `
name: good-workflow
pipeline:
  - id: step_a
    webhook: { url: http://example.com }
`
	if err := writeFile(filepath.Join(dir, "workflows/good.workflow.yaml"), goodWorkflow); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	// Write a broken agent (missing output schema)
	brokenAgent := `
name: broken-agent
system: test
`
	if err := writeFile(filepath.Join(dir, "agents/broken.agent.yaml"), brokenAgent); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", filepath.Join(dir, "workflows"))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Run validation on the project directory
	if err := cmd.RunE(cmd, []string{dir}); err == nil {
		t.Error("want error for broken orphan agent, got nil")
	}

	output := buf.String()
	if !strings.Contains(output, "broken.agent.yaml") {
		t.Errorf("output missing broken agent filename: %s", output)
	}
	if !strings.Contains(output, "has no output schema") {
		t.Errorf("output missing expected error message: %s", output)
	}
}

// TestValidateCmd_resolvesRelativePathsCorrectly verifies that validate correctly resolves
// relative server paths within an agent file.
func TestValidateCmd_resolvesRelativePathsCorrectly(t *testing.T) {
	dir := t.TempDir()

	// agents/myagent.agent.yaml -> ../servers/myserver.server.yaml
	agentContent := `
name: myagent
model: standard
system: test
servers:
  - name: myserver
    path: ../servers/myserver.server.yaml
output:
  schema: { type: object }
`
	serverContent := `
name: myserver
url: http://myserver
`
	if err := writeFile(filepath.Join(dir, "agents/myagent.agent.yaml"), agentContent); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	if err := writeFile(filepath.Join(dir, "servers/myserver.server.yaml"), serverContent); err != nil {
		t.Fatalf("write server: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Should pass because ../servers/myserver.server.yaml is resolved relative to agents/myagent.agent.yaml
	if err := cmd.RunE(cmd, []string{dir}); err != nil {
		t.Fatalf("validation failed: %v\nOutput: %s", err, buf.String())
	}
}

// TestValidateCmd_detectsMissingServerViaWorkflow verifies that validate catches a missing
// server file that is referenced through a workflow → agent → server chain.
func TestValidateCmd_detectsMissingServerViaWorkflow(t *testing.T) {
	dir := t.TempDir()

	workflowContent := `
name: test-workflow
pipeline:
  - id: step1
    agent: agents/myagent.agent.yaml
`
	agentContent := `
name: myagent
model: default
system: test
servers:
  - name: myserver
    path: ../servers/missing.server.yaml
output:
  schema: { type: object }
`
	if err := writeFile(filepath.Join(dir, "workflows/test.workflow.yaml"), workflowContent); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	if err := writeFile(filepath.Join(dir, "agents/myagent.agent.yaml"), agentContent); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	// Intentionally no server file — validate must catch this.

	var buf strings.Builder
	cmd := validateCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.RunE(cmd, []string{dir}); err == nil {
		t.Errorf("want error for missing server, got nil\nOutput: %s", buf.String())
	}
}

// TestValidateCmd_projectDirFlag verifies that --project-dir (and KTSU_PROJECT_DIR) controls
// the base path used when resolving agent and server references — matching orchestrator behaviour.
func TestValidateCmd_projectDirFlag(t *testing.T) {
	dir := t.TempDir()

	workflowContent := `
name: test-workflow
pipeline:
  - id: step1
    agent: agents/myagent.agent.yaml
`
	agentContent := `
name: myagent
model: default
system: test
servers:
  - name: myserver
    path: ../servers/myserver.server.yaml
output:
  schema: { type: object }
`
	serverContent := `
name: myserver
url: http://myserver
`
	if err := writeFile(filepath.Join(dir, "workflows/test.workflow.yaml"), workflowContent); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	if err := writeFile(filepath.Join(dir, "agents/myagent.agent.yaml"), agentContent); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	if err := writeFile(filepath.Join(dir, "servers/myserver.server.yaml"), serverContent); err != nil {
		t.Fatalf("write server: %v", err)
	}

	// Verify KTSU_PROJECT_DIR env var is honoured.
	t.Setenv("KTSU_PROJECT_DIR", dir)

	var buf strings.Builder
	cmd := validateCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// No positional arg — relies solely on KTSU_PROJECT_DIR via the --project-dir default.
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("want no error when KTSU_PROJECT_DIR is set correctly, got: %v\nOutput: %s", err, buf.String())
	}
}

// TestOrchestratorEnvelopeCmd_printsEnvelopeJSON verifies that ktsu orchestrator envelope
// sends GET /envelope/{run_id} and pretty-prints the JSON response.
func TestOrchestratorEnvelopeCmd_printsEnvelopeJSON(t *testing.T) {
	envelope := map[string]interface{}{
		"run_id":   "run-abc123",
		"workflow": "my-workflow",
		"status":   "complete",
		"steps":    []interface{}{},
		"totals":   map[string]interface{}{"duration_ms": float64(1200)},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/envelope/run-abc123" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(envelope)
	}))
	defer srv.Close()

	var buf strings.Builder
	cmd := orchestratorEnvelopeCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().Set("orchestrator", srv.URL)
	cmd.SetOut(&buf)

	if err := cmd.RunE(cmd, []string{"run-abc123"}); err != nil {
		t.Fatalf("orchestratorEnvelopeCmd.RunE: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(buf.String()), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", err, buf.String())
	}
	if got["run_id"] != "run-abc123" {
		t.Errorf("want run_id=run-abc123, got %v", got["run_id"])
	}
}

// TestOrchestratorEnvelopeCmd_notFound verifies that a 404 from the orchestrator
// surfaces as an error containing the server's error message.
func TestOrchestratorEnvelopeCmd_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "run not found"})
	}))
	defer srv.Close()

	cmd := orchestratorEnvelopeCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().Set("orchestrator", srv.URL)

	err := cmd.RunE(cmd, []string{"run-missing"})
	if err == nil {
		t.Fatal("want error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "run not found") {
		t.Errorf("want error containing 'run not found', got: %v", err)
	}
}

// TestInvokeCmd_auth verifies that invoke sends the Bearer token when provided.
func TestInvokeCmd_auth(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"run_id": "run-auth-001"})
	}))
	defer srv.Close()

	cmd := invokeCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().Set("orchestrator", srv.URL)
	cmd.Flags().Set("api-key", "secret-token")

	if err := cmd.RunE(cmd, []string{"hello"}); err != nil {
		t.Fatalf("invokeCmd.RunE with auth: %v", err)
	}

	if authHeader != "Bearer secret-token" {
		t.Errorf("want Authorization: Bearer secret-token, got %q", authHeader)
	}

	// Verify env var fallback
	authHeader = ""
	t.Setenv("KTSU_API_KEY", "env-token")
	cmd2 := invokeCmd()
	cmd2.SetContext(context.Background())
	cmd2.Flags().Set("orchestrator", srv.URL)
	if err := cmd2.RunE(cmd2, []string{"hello"}); err != nil {
		t.Fatalf("invokeCmd with KTSU_API_KEY: %v", err)
	}
	if authHeader != "Bearer env-token" {
		t.Errorf("want Authorization: Bearer env-token, got %q", authHeader)
	}

	// Flag overrides env var
	authHeader = ""
	cmd3 := invokeCmd()
	cmd3.SetContext(context.Background())
	cmd3.Flags().Set("orchestrator", srv.URL)
	cmd3.Flags().Set("api-key", "flag-token")
	if err := cmd3.RunE(cmd3, []string{"hello"}); err != nil {
		t.Fatalf("invokeCmd with flag override: %v", err)
	}
	if authHeader != "Bearer flag-token" {
		t.Errorf("want Authorization: Bearer flag-token, got %q", authHeader)
	}
}

// TestOrchestratorEnvelopeCmd_auth verifies that envelope sends the Bearer token when provided.
func TestOrchestratorEnvelopeCmd_auth(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer srv.Close()

	cmd := orchestratorEnvelopeCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().Set("orchestrator", srv.URL)
	cmd.Flags().Set("api-key", "secret-token")

	if err := cmd.RunE(cmd, []string{"run-123"}); err != nil {
		t.Fatalf("envelope with auth: %v", err)
	}

	if authHeader != "Bearer secret-token" {
		t.Errorf("want Authorization: Bearer secret-token, got %q", authHeader)
	}
}

// TestOrchestratorGroupCmd_hasOrcAlias verifies the orchestrator command group
// registers "orc" as an alias.
func TestOrchestratorGroupCmd_hasOrcAlias(t *testing.T) {
	cmd := orchestratorGroupCmd()
	for _, a := range cmd.Aliases {
		if a == "orc" {
			return
		}
	}
	t.Error("want 'orc' alias on orchestrator command group")
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestNewProjectCmd_happyPath verifies that all 5 scaffold files are created and
// the workflow YAML has the project name substituted.
func TestNewProjectCmd_happyPath(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	var buf strings.Builder
	cmd := newProjectCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.RunE(cmd, []string{"myproject"}); err != nil {
		t.Fatalf("newProjectCmd.RunE: %v", err)
	}

	expectedFiles := []string{
		"myproject/workflows/myproject.workflow.yaml",
		"myproject/agents/placeholder.agent.yaml",
		"myproject/environments/dev.env.yaml",
		"myproject/gateway.yaml",
		"myproject/servers.yaml",
	}

	output := buf.String()
	for _, f := range expectedFiles {
		if !strings.Contains(output, "created: "+f) {
			t.Errorf("want output to contain %q, got:\n%s", "created: "+f, output)
		}
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected file %s to exist: %v", f, err)
		}
	}

	// Check workflow YAML has the project name substituted.
	workflowPath := filepath.Join(dir, "myproject/workflows/myproject.workflow.yaml")
	contents, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	if !strings.Contains(string(contents), "name: myproject") {
		t.Errorf("workflow YAML should contain 'name: myproject', got:\n%s", contents)
	}
	if strings.Contains(string(contents), "{{") {
		t.Errorf("workflow YAML should not contain unrendered template markers, got:\n%s", contents)
	}
}

// TestNewProjectCmd_alreadyExists verifies an error is returned when the target directory exists.
func TestNewProjectCmd_alreadyExists(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	// Pre-create the directory so the command should fail.
	if err := os.Mkdir("existing", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cmd := newProjectCmd()
	err = cmd.RunE(cmd, []string{"existing"})
	if err == nil {
		t.Fatal("want error when project directory already exists, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("want error containing 'already exists', got: %v", err)
	}
}

// TestNewProjectCmd_missingName verifies cobra returns an error when no name argument is given.
func TestNewProjectCmd_missingName(t *testing.T) {
	cmd := newCmd()
	cmd.SetArgs([]string{"project"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error for missing name argument, got nil")
	}
}

func TestValidateCmd_graphOutput(t *testing.T) {
	dir := t.TempDir()

	goodWorkflow := `
name: good-workflow
pipeline:
  - id: step_a
    agent: agents/test.agent.yaml
  - id: step_b
    depends_on: [step_a]
    webhook:
      url: http://example.com
`
	if err := writeFile(dir+"/good.workflow.yaml", goodWorkflow); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	if err := os.MkdirAll(dir+"/agents", 0755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	if err := writeFile(dir+"/agents/test.agent.yaml", "name: test\nmodel: default\noutput:\n  schema:\n    type: object"); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", dir)
	if err := cmd.Flags().Set("graph", "true"); err != nil {
		t.Fatalf("Expected --graph flag to be available: %v", err)
	}
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.RunE(cmd, []string{dir}); err != nil {
		t.Fatalf("want no error for valid workflow, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "graph TD") {
		t.Errorf("want output to contain 'graph TD', got:\n%s", output)
	}
	if !strings.Contains(output, "step_a[\"step_a [agent]\"]") {
		t.Errorf("want output to contain 'step_a [agent]', got:\n%s", output)
	}
	if !strings.Contains(output, "step_b[\"step_b [webhook]\"]") {
		t.Errorf("want output to contain 'step_b [webhook]', got:\n%s", output)
	}
	if !strings.Contains(output, "step_a --> step_b") {
		t.Errorf("want output to contain 'step_a --> step_b', got:\n%s", output)
	}
}

func TestValidateCmd_graphOutput_highlightsErrors(t *testing.T) {
	dir := t.TempDir()

	// Workflow with a missing dependency: step_b depends on step_c (missing)
	badWorkflow := `
name: bad-workflow
pipeline:
  - id: step_a
    webhook:
      url: http://example.com
  - id: step_b
    depends_on: [step_c]
    webhook:
      url: http://example.com
`
	if err := writeFile(dir+"/bad.workflow.yaml", badWorkflow); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", dir)
	cmd.Flags().Set("graph", "true")
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// RunE returns error for invalid workflow, which is expected.
	_ = cmd.RunE(cmd, nil)

	output := buf.String()
	// Check that step_b is marked as failed
	if !strings.Contains(output, "step_b[\"step_b [webhook]\"]:::failed") {
		t.Errorf("want step_b to be marked as failed in graph, got:\n%s", output)
	}
}
func TestValidateCmd_graphOutput_externalRefs(t *testing.T) {
	dir := t.TempDir()

	// 1. Write Agent
	agentContent := `
name: researcher
model: default
servers:
  - name: search
    path: ../servers.yaml
output:
  schema: { type: object }
`
	if err := os.MkdirAll(dir+"/agents", 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	if err := writeFile(dir+"/agents/researcher.agent.yaml", agentContent); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	// 2. Write Server Manifest
	serverContent := `
servers:
  - name: search
    url: http://search.local
`
	if err := writeFile(dir+"/servers.yaml", serverContent); err != nil {
		t.Fatalf("write servers: %v", err)
	}

	// 3. Write Workflow
	workflowContent := `
name: search-workflow
pipeline:
  - id: step_1
    agent: agents/researcher.agent.yaml
`
	if err := os.MkdirAll(dir+"/workflows", 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := writeFile(dir+"/workflows/search.workflow.yaml", workflowContent); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("graph", "true")
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.RunE(cmd, []string{dir}); err != nil {
		t.Fatalf("want no error, got: %v", err)
	}

	output := buf.String()
	// Check for Grouped Summary Headers (should NOT be present in graph mode)
	if strings.Contains(output, "Workflows:") || strings.Contains(output, "Agents:") || strings.Contains(output, "Servers:") {
		t.Errorf("summary headers should be suppressed in graph mode, got:\n%s", output)
	}

	// Check for file markers (should NOT contain OKAY/FAIL in graph mode)
	if strings.Contains(output, "OKAY") || strings.Contains(output, "FAIL") {
		t.Errorf("summary markers should be suppressed in graph mode, got:\n%s", output)
	}

	// Check for Mermaid Graph content
	if !strings.Contains(output, "[agent-file]") {
		t.Errorf("want agent file node in graph, got:\n%s", output)
	}
}

func TestHubCmd_disabledByDefault(t *testing.T) {
	t.Setenv("KTSU_HUB_ENABLED", "")
	root := rootCmd()
	cmd, _, _ := root.Find([]string{"hub"})
	if cmd != root {
		t.Fatalf("expected Find to return root (hub not registered), got %q", cmd.Use)
	}
}

func TestHubCmd_enabledWithFlag(t *testing.T) {
	t.Setenv("KTSU_HUB_ENABLED", "true")
	root := rootCmd()
	cmd, _, err := root.Find([]string{"hub"})
	if err != nil {
		t.Fatalf("expected hub command to be found: %v", err)
	}
	if cmd.Use != "hub" {
		t.Fatalf("expected hub command, got %q", cmd.Use)
	}
}
