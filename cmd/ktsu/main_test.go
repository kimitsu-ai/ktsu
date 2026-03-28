package main

import (
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

func writeFile(path, content string) error {
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
	if err := writeFile(dir+"/agents/test.agent.yaml", "name: test\nmodel: default"); err != nil {
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
servers:
  - name: search
    path: ../servers.yaml
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
