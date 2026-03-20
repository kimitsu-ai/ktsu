package envelope

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

func TestEnvelope_setThenGetReturnsValue(t *testing.T) {
	s := New()
	s.Call(ctx, "envelope_set", mustMarshal(map[string]string{"path": "status", "value": "running"}))

	out, err := s.Call(ctx, "envelope_get", mustMarshal(map[string]string{"path": "status"}))
	if err != nil {
		t.Fatalf("envelope_get failed: %v", err)
	}
	var result map[string]string
	json.Unmarshal(out, &result)
	if result["value"] != "running" {
		t.Errorf("expected value %q, got %q", "running", result["value"])
	}
}

func TestEnvelope_getMissingPathReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "envelope_get", mustMarshal(map[string]string{"path": "missing"}))
	if err == nil {
		t.Error("expected error for missing path, got nil")
	}
}

func TestEnvelope_appendBuildsSlice(t *testing.T) {
	s := New()
	s.Call(ctx, "envelope_append", mustMarshal(map[string]string{"path": "tags", "value": "a"}))
	s.Call(ctx, "envelope_append", mustMarshal(map[string]string{"path": "tags", "value": "b"}))

	out, err := s.Call(ctx, "envelope_get", mustMarshal(map[string]string{"path": "tags"}))
	if err != nil {
		t.Fatalf("envelope_get failed: %v", err)
	}
	var result map[string][]string
	json.Unmarshal(out, &result)
	if len(result["value"]) != 2 || result["value"][0] != "a" || result["value"][1] != "b" {
		t.Errorf("expected [a b], got %v", result["value"])
	}
}

func TestEnvelope_setOverwritesPreviousValue(t *testing.T) {
	s := New()
	s.Call(ctx, "envelope_set", mustMarshal(map[string]string{"path": "k", "value": "v1"}))
	s.Call(ctx, "envelope_set", mustMarshal(map[string]string{"path": "k", "value": "v2"}))

	out, _ := s.Call(ctx, "envelope_get", mustMarshal(map[string]string{"path": "k"}))
	var result map[string]string
	json.Unmarshal(out, &result)
	if result["value"] != "v2" {
		t.Errorf("expected overwritten value %q, got %q", "v2", result["value"])
	}
}

func TestEnvelope_unknownToolReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "envelope_unknown", nil)
	if err == nil {
		t.Error("expected error for unknown tool, got nil")
	}
}
