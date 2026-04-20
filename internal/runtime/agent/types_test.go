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
	if len(got.Messages) != 1 {
		t.Errorf("want 1 message, got %d", len(got.Messages))
	}
}

func TestInvokeRequest_UserMessage_replacesInput(t *testing.T) {
	req := agent.InvokeRequest{
		RunID:       "r1",
		UserMessage: "Name: Kyle",
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["user_message"]; !ok {
		t.Error("expected user_message field in JSON")
	}
	if _, ok := m["input"]; ok {
		t.Error("did not expect input field in JSON after removal")
	}
}

func TestToolServerSpec_SecretKeys_JSONRoundTrip(t *testing.T) {
	spec := agent.ToolServerSpec{
		Name:       "my-server",
		URL:        "http://localhost:8080",
		SecretKeys: []string{"API_KEY", "DB_PASSWORD"},
	}
	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	// Verify the field is marshaled as "secret_keys"
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["secret_keys"]; !ok {
		t.Error("expected secret_keys field in JSON")
	}
	// Verify round-trip
	var got agent.ToolServerSpec
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.SecretKeys) != 2 {
		t.Fatalf("want 2 secret keys, got %d", len(got.SecretKeys))
	}
	if got.SecretKeys[0] != "API_KEY" || got.SecretKeys[1] != "DB_PASSWORD" {
		t.Errorf("SecretKeys not preserved: %v", got.SecretKeys)
	}
}

func TestToolApprovalRule_JSONRoundTrip(t *testing.T) {
	rule := agent.ToolApprovalRule{
		Pattern:         "delete-*",
		OnReject:        "fail",
		TimeoutMS:       1800000,
		TimeoutBehavior: "reject",
	}
	b, _ := json.Marshal(rule)
	var got agent.ToolApprovalRule
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Pattern != "delete-*" {
		t.Errorf("Pattern mismatch: %s", got.Pattern)
	}
	if got.TimeoutMS != 1800000 {
		t.Errorf("TimeoutMS mismatch: %d", got.TimeoutMS)
	}
}
