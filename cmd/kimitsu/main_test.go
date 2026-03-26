package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestInvokeCmd_postsToOrchestrator verifies that kimitsu invoke sends a POST /invoke/{workflow}
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
