package kv

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

func TestKV_setThenGetReturnsValue(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "kv_set", mustMarshal(map[string]string{"key": "foo", "value": "bar"}))
	if err != nil {
		t.Fatalf("kv_set failed: %v", err)
	}

	out, err := s.Call(ctx, "kv_get", mustMarshal(map[string]string{"key": "foo"}))
	if err != nil {
		t.Fatalf("kv_get failed: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("could not decode kv_get output: %v", err)
	}
	if result["value"] != "bar" {
		t.Errorf("expected value %q, got %q", "bar", result["value"])
	}
}

func TestKV_getMissingKeyReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "kv_get", mustMarshal(map[string]string{"key": "missing"}))
	if err == nil {
		t.Error("expected error for missing key, got nil")
	}
}

func TestKV_deleteRemovesKey(t *testing.T) {
	s := New()
	s.Call(ctx, "kv_set", mustMarshal(map[string]string{"key": "x", "value": "1"}))
	s.Call(ctx, "kv_delete", mustMarshal(map[string]string{"key": "x"}))

	_, err := s.Call(ctx, "kv_get", mustMarshal(map[string]string{"key": "x"}))
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestKV_setOverwritesExistingValue(t *testing.T) {
	s := New()
	s.Call(ctx, "kv_set", mustMarshal(map[string]string{"key": "k", "value": "v1"}))
	s.Call(ctx, "kv_set", mustMarshal(map[string]string{"key": "k", "value": "v2"}))

	out, err := s.Call(ctx, "kv_get", mustMarshal(map[string]string{"key": "k"}))
	if err != nil {
		t.Fatalf("kv_get failed: %v", err)
	}
	var result map[string]string
	json.Unmarshal(out, &result)
	if result["value"] != "v2" {
		t.Errorf("expected overwritten value %q, got %q", "v2", result["value"])
	}
}

func TestKV_unknownToolReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "kv_unknown", nil)
	if err == nil {
		t.Error("expected error for unknown tool, got nil")
	}
}
