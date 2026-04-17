package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// TestRunsGetCmd_printsEnvelopeJSON verifies that ktsu runs get sends GET /runs/{run_id}/envelope
// and pretty-prints the JSON response.
func TestRunsGetCmd_printsEnvelopeJSON(t *testing.T) {
	envelope := map[string]interface{}{
		"run_id":   "run-abc123",
		"workflow": "my-workflow",
		"status":   "complete",
		"steps":    []interface{}{},
		"totals":   map[string]interface{}{"duration_ms": float64(1200)},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/runs/run-abc123/envelope" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(envelope)
	}))
	defer srv.Close()

	var buf strings.Builder
	cmd := runsGetCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().Set("orchestrator", srv.URL)
	cmd.SetOut(&buf)

	if err := cmd.RunE(cmd, []string{"run-abc123"}); err != nil {
		t.Fatalf("runsGetCmd.RunE: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(buf.String()), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", err, buf.String())
	}
	if got["run_id"] != "run-abc123" {
		t.Errorf("want run_id=run-abc123, got %v", got["run_id"])
	}
}

// TestRunsGetCmd_notFound verifies that a 404 from the orchestrator
// surfaces as an error containing the server's error message.
func TestRunsGetCmd_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "run not found"})
	}))
	defer srv.Close()

	cmd := runsGetCmd()
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

// TestRunsGroupCmd_hasGetSubcommand verifies the runs command group registers the get subcommand.
func TestRunsGroupCmd_hasGetSubcommand(t *testing.T) {
	cmd := runsGroupCmd()
	for _, sub := range cmd.Commands() {
		if sub.Use == "get <run_id>" {
			return
		}
	}
	t.Error("want 'get' subcommand on runs command group")
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestNewProjectCmd_happyPath verifies that all 6 scaffold files are created and
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
		"myproject/ktsuhub.yaml",
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

func TestNewProjectCmd_createsKtsuhubYaml(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	cmd := newProjectCmd()
	cmd.SetOut(io.Discard)
	if err := cmd.RunE(cmd, []string{"myapp"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hubYaml := filepath.Join(dir, "myapp", "ktsuhub.yaml")
	if _, err := os.Stat(hubYaml); os.IsNotExist(err) {
		t.Error("expected ktsuhub.yaml to be created by new project scaffold")
	}
	data, err := os.ReadFile(hubYaml)
	if err != nil {
		t.Fatalf("read ktsuhub.yaml: %v", err)
	}
	if !strings.Contains(string(data), "workflows:") {
		t.Errorf("expected ktsuhub.yaml to contain 'workflows:', got:\n%s", data)
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

func TestValidateCmd_reflect_whitespaceOnlyErrors(t *testing.T) {
	dir := t.TempDir()

	agentContent := `
name: my-agent
model: standard
prompt:
  system: "You are helpful."
reflect: "   "
output:
  schema:
    type: object
    properties:
      result: { type: string }
`
	if err := writeFile(filepath.Join(dir, "agents/my.agent.yaml"), agentContent); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	if err := writeFile(filepath.Join(dir, "workflows/wf.workflow.yaml"), `
name: wf
pipeline:
  - id: step1
    agent: agents/my.agent.yaml
`); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", filepath.Join(dir, "workflows"))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.RunE(cmd, []string{dir}); err == nil {
		t.Error("want error for whitespace-only reflect, got nil")
	}
	output := buf.String()
	if !strings.Contains(output, "reflect prompt is empty or whitespace") {
		t.Errorf("missing reflect error in output: %s", output)
	}
}

func TestValidateCmd_reflect_validPromptPasses(t *testing.T) {
	dir := t.TempDir()

	agentContent := `
name: my-agent
model: standard
prompt:
  system: "You are helpful."
reflect: |
  Review your answer. Is it correct?
output:
  schema:
    type: object
    properties:
      result: { type: string }
`
	if err := writeFile(filepath.Join(dir, "agents/my.agent.yaml"), agentContent); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	if err := writeFile(filepath.Join(dir, "workflows/wf.workflow.yaml"), `
name: wf
pipeline:
  - id: step1
    agent: agents/my.agent.yaml
`); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", filepath.Join(dir, "workflows"))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.RunE(cmd, []string{dir}); err != nil {
		t.Errorf("want no error for valid reflect, got: %v (output: %s)", err, buf.String())
	}
}

func TestValidateCmd_reflect_maxTurns1Warns(t *testing.T) {
	dir := t.TempDir()

	agentContent := `
name: my-agent
model: standard
max_turns: 1
prompt:
  system: "You are helpful."
reflect: |
  Review your answer.
output:
  schema:
    type: object
    properties:
      result: { type: string }
`
	if err := writeFile(filepath.Join(dir, "agents/my.agent.yaml"), agentContent); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	if err := writeFile(filepath.Join(dir, "workflows/wf.workflow.yaml"), `
name: wf
pipeline:
  - id: step1
    agent: agents/my.agent.yaml
`); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", filepath.Join(dir, "workflows"))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Should pass validation (not an error).
	if err := cmd.RunE(cmd, []string{dir}); err != nil {
		t.Errorf("want no error for reflect+max_turns:1, got: %v", err)
	}
}

// TestValidateCmd_workflowStep_webhookConflict verifies that validate catches a parent step
// declaring webhooks: execute when the sub-workflow declares webhooks: suppress.
func TestValidateCmd_workflowStep_webhookConflict(t *testing.T) {
	dir := t.TempDir()

	subWF := `kind: workflow
name: sub
version: "1.0.0"
visibility: sub-workflow
webhooks: suppress
pipeline:
  - id: noop
    transform:
      inputs:
        - from: input
      ops:
        - map:
            expr: "{ok: true}"
`
	if err := writeFile(filepath.Join(dir, "workflows/sub.workflow.yaml"), subWF); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	parentWF := `kind: workflow
name: parent
version: "1.0.0"
pipeline:
  - id: call
    workflow: ./sub.workflow.yaml
    webhooks: execute
`
	if err := writeFile(filepath.Join(dir, "workflows/parent.workflow.yaml"), parentWF); err != nil {
		t.Fatalf("write parent workflow: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", filepath.Join(dir, "workflows"))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.RunE(cmd, []string{dir})
	if err == nil {
		t.Fatal("expected validation error for webhook conflict, got none")
	}
	if !strings.Contains(buf.String(), "webhooks: execute") {
		t.Errorf("expected webhook conflict message, got: %s", buf.String())
	}
}

// TestValidateCmd_workflowStep_missingRequiredParam verifies that validate catches a
// workflow step that doesn't provide a required param declared by the sub-workflow.
func TestValidateCmd_workflowStep_missingRequiredParam(t *testing.T) {
	dir := t.TempDir()

	subWF := `kind: workflow
name: sub
version: "1.0.0"
visibility: sub-workflow
params:
  schema:
    type: object
    required: [webhook_url]
    properties:
      webhook_url: {type: string}
pipeline:
  - id: noop
    transform:
      inputs:
        - from: input
      ops:
        - map:
            expr: "{ok: true}"
`
	if err := writeFile(filepath.Join(dir, "workflows/sub.workflow.yaml"), subWF); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	parentWF := `kind: workflow
name: parent
version: "1.0.0"
pipeline:
  - id: call
    workflow: ./sub.workflow.yaml
    # webhook_url intentionally omitted
`
	if err := writeFile(filepath.Join(dir, "workflows/parent.workflow.yaml"), parentWF); err != nil {
		t.Fatalf("write parent workflow: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", filepath.Join(dir, "workflows"))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.RunE(cmd, []string{dir})
	if err == nil {
		t.Fatal("expected validation error for missing required param, got none")
	}
	if !strings.Contains(buf.String(), "webhook_url") {
		t.Errorf("expected 'webhook_url' in error output, got: %s", buf.String())
	}
}

// TestValidateCmd_workflowStep_crossWorkflowCycle verifies that validate catches cycles
// across workflow step references (A → B → A).
func TestValidateCmd_workflowStep_crossWorkflowCycle(t *testing.T) {
	dir := t.TempDir()

	// A references B
	wfA := `kind: workflow
name: wf-a
version: "1.0.0"
pipeline:
  - id: call-b
    workflow: ./wf-b.workflow.yaml
`
	// B references A
	wfB := `kind: workflow
name: wf-b
version: "1.0.0"
pipeline:
  - id: call-a
    workflow: ./wf-a.workflow.yaml
`
	if err := writeFile(filepath.Join(dir, "workflows/wf-a.workflow.yaml"), wfA); err != nil {
		t.Fatalf("write wf-a: %v", err)
	}
	if err := writeFile(filepath.Join(dir, "workflows/wf-b.workflow.yaml"), wfB); err != nil {
		t.Fatalf("write wf-b: %v", err)
	}

	var buf strings.Builder
	cmd := validateCmd()
	cmd.Flags().Set("workflow-dir", filepath.Join(dir, "workflows"))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.RunE(cmd, []string{dir})
	if err == nil {
		t.Fatal("expected validation error for cross-workflow cycle, got none")
	}
	if !strings.Contains(buf.String(), "cycle") {
		t.Errorf("expected 'cycle' in error output, got: %s", buf.String())
	}
}

// TestWorkflowTreeCmd_basicOutput verifies that `ktsu workflow tree` prints the workflow file
// and its sub-workflow dependency.
func TestWorkflowTreeCmd_basicOutput(t *testing.T) {
	dir := t.TempDir()

	subWF := `kind: workflow
name: sub
version: "1.0.0"
pipeline:
  - id: noop
    transform:
      inputs: [{from: input}]
      ops:
        - map:
            expr: "{ok: true}"
`
	parentWF := `kind: workflow
name: parent
version: "1.0.0"
pipeline:
  - id: call
    workflow: ./sub.workflow.yaml
`
	if err := writeFile(filepath.Join(dir, "sub.workflow.yaml"), subWF); err != nil {
		t.Fatalf("write sub: %v", err)
	}
	parentPath := filepath.Join(dir, "parent.workflow.yaml")
	if err := writeFile(parentPath, parentWF); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	var buf strings.Builder
	root := rootCmd()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"workflow", "tree", parentPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("workflow tree: %v\nOutput: %s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "parent.workflow.yaml") {
		t.Errorf("expected parent.workflow.yaml in output, got: %s", out)
	}
	if !strings.Contains(out, "sub.workflow.yaml") {
		t.Errorf("expected sub.workflow.yaml in output, got: %s", out)
	}
}

// TestWorkflowTreeCmd_jsonOutput verifies --json flag produces valid JSON with path and kind fields.
func TestWorkflowTreeCmd_jsonOutput(t *testing.T) {
	dir := t.TempDir()
	parentWF := `kind: workflow
name: parent
version: "1.0.0"
pipeline:
  - id: noop
    transform:
      inputs: [{from: input}]
      ops:
        - map:
            expr: "{ok: true}"
`
	parentPath := filepath.Join(dir, "parent.workflow.yaml")
	if err := writeFile(parentPath, parentWF); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	var buf strings.Builder
	root := rootCmd()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"workflow", "tree", "--json", parentPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("workflow tree --json: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nOutput: %s", err, buf.String())
	}
	if result["kind"] != "workflow" {
		t.Errorf("expected kind=workflow, got: %v", result["kind"])
	}
	if !strings.Contains(result["path"].(string), "parent.workflow.yaml") {
		t.Errorf("expected path to contain parent.workflow.yaml, got: %v", result["path"])
	}
}

func TestValidateCmd_multipleWorkspaces(t *testing.T) {
	ws1 := t.TempDir()
	ws2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws1, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws2, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}

	wf1 := `kind: workflow
name: flow1
version: "1.0.0"
pipeline: []
`
	wf2 := `kind: workflow
name: flow2
version: "1.0.0"
pipeline: []
`
	os.WriteFile(filepath.Join(ws1, "workflows", "flow1.workflow.yaml"), []byte(wf1), 0o644)
	os.WriteFile(filepath.Join(ws2, "workflows", "flow2.workflow.yaml"), []byte(wf2), 0o644)

	cmd := validateCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("project-dir", ws1); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("workspace", ws2); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCmd_lockAutoLoad(t *testing.T) {
	primary := t.TempDir()
	hubWs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(primary, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hubWs, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf := `kind: workflow
name: hub-flow
version: "1.0.0"
pipeline: []
`
	os.WriteFile(filepath.Join(hubWs, "workflows", "hub-flow.workflow.yaml"), []byte(wf), 0o644)

	lockContent := fmt.Sprintf(`entries:
  - name: testorg/hub-flow
    source: github.com/testorg/hub-flow
    sha: abc123
    cache: %s
    mutable: false
`, hubWs)
	os.WriteFile(filepath.Join(primary, "ktsuhub.lock.yaml"), []byte(lockContent), 0o644)

	cmd := validateCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("project-dir", primary); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error with lock auto-load: %v", err)
	}
}

func TestWorkflowTreeCmd_outputsFiles(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "workflows"), 0o755)
	os.MkdirAll(filepath.Join(dir, "agents"), 0o755)
	os.MkdirAll(filepath.Join(dir, "servers"), 0o755)

	agentYAML := `name: greeter
model: default
max_turns: 1
servers:
  - name: wiki
    path: ../servers/wiki.server.yaml
`
	serverYAML := `name: wiki
description: wiki search
`
	wfYAML := `kind: workflow
name: hello
version: "1.0.0"
pipeline:
  - id: s1
    agent: ../agents/greeter.agent.yaml
`
	os.WriteFile(filepath.Join(dir, "workflows", "hello.workflow.yaml"), []byte(wfYAML), 0o644)
	os.WriteFile(filepath.Join(dir, "agents", "greeter.agent.yaml"), []byte(agentYAML), 0o644)
	os.WriteFile(filepath.Join(dir, "servers", "wiki.server.yaml"), []byte(serverYAML), 0o644)

	wfPath := filepath.Join(dir, "workflows", "hello.workflow.yaml")
	var buf strings.Builder
	root := rootCmd()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"workflow", "tree", wfPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "hello.workflow.yaml") {
		t.Errorf("expected workflow file in output, got:\n%s", output)
	}
	if !strings.Contains(output, "greeter.agent.yaml") {
		t.Errorf("expected agent in output, got:\n%s", output)
	}
	if !strings.Contains(output, "wiki.server.yaml") {
		t.Errorf("expected server in output, got:\n%s", output)
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
