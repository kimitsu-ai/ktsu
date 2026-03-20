package validate

import (
	"context"
	"encoding/json"
	"testing"
)

var ctx = context.Background()

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// --- validate_json ---

func TestValidate_validJSONReturnsValid(t *testing.T) {
	s := New()
	out, err := s.Call(ctx, "validate_json", mustMarshal(map[string]string{"value": `{"a":1}`}))
	if err != nil {
		t.Fatalf("validate_json failed: %v", err)
	}
	var result map[string]bool
	json.Unmarshal(out, &result)
	if !result["valid"] {
		t.Error("expected valid=true for valid JSON")
	}
}

func TestValidate_invalidJSONReturnsNotValid(t *testing.T) {
	s := New()
	out, err := s.Call(ctx, "validate_json", mustMarshal(map[string]string{"value": "{bad"}))
	if err != nil {
		t.Fatalf("validate_json failed: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	if result["valid"] == true {
		t.Error("expected valid=false for invalid JSON")
	}
}

// --- validate_schema ---

func TestValidate_schemaPassesWhenRequiredFieldsPresent(t *testing.T) {
	s := New()
	out, err := s.Call(ctx, "validate_schema", mustMarshal(map[string]any{
		"value":  map[string]any{"name": "alice", "age": 30},
		"schema": map[string]any{"required": []string{"name", "age"}},
	}))
	if err != nil {
		t.Fatalf("validate_schema failed: %v", err)
	}
	var result map[string]bool
	json.Unmarshal(out, &result)
	if !result["valid"] {
		t.Error("expected valid=true when all required fields present")
	}
}

func TestValidate_schemaFailsWhenRequiredFieldMissing(t *testing.T) {
	s := New()
	out, err := s.Call(ctx, "validate_schema", mustMarshal(map[string]any{
		"value":  map[string]any{"name": "alice"},
		"schema": map[string]any{"required": []string{"name", "age"}},
	}))
	if err != nil {
		t.Fatalf("validate_schema failed: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	if result["valid"] == true {
		t.Error("expected valid=false when required field missing")
	}
	if result["error"] == "" {
		t.Error("expected non-empty error message")
	}
}

func TestValidate_unknownToolReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "validate_unknown", nil)
	if err == nil {
		t.Error("expected error for unknown tool, got nil")
	}
}
