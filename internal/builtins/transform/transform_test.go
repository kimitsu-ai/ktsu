package transform

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

// --- transform_jmespath ---

func TestTransform_jmespathExtractsField(t *testing.T) {
	s := New()
	out, err := s.Call(ctx, "transform_jmespath", mustMarshal(map[string]any{
		"expression": "name",
		"value":      map[string]any{"name": "alice", "age": 30},
	}))
	if err != nil {
		t.Fatalf("transform_jmespath failed: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	if result["result"] != "alice" {
		t.Errorf("expected result %q, got %v", "alice", result["result"])
	}
}

func TestTransform_jmespathInvalidExpressionReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "transform_jmespath", mustMarshal(map[string]any{
		"expression": "[[invalid",
		"value":      map[string]any{},
	}))
	if err == nil {
		t.Error("expected error for invalid JMESPath expression, got nil")
	}
}

// --- transform_map ---

func TestTransform_mapProducesNewObjectFromMappings(t *testing.T) {
	s := New()
	out, err := s.Call(ctx, "transform_map", mustMarshal(map[string]any{
		"mappings": map[string]string{"full_name": "name", "years": "age"},
		"value":    map[string]any{"name": "bob", "age": 25},
	}))
	if err != nil {
		t.Fatalf("transform_map failed: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	mapped, _ := result["result"].(map[string]any)
	if mapped["full_name"] != "bob" || mapped["years"] != float64(25) {
		t.Errorf("unexpected mapped result: %v", mapped)
	}
}

// --- transform_filter ---

func TestTransform_filterKeepsMatchingItems(t *testing.T) {
	s := New()
	out, err := s.Call(ctx, "transform_filter", mustMarshal(map[string]any{
		"condition": "age > `18`",
		"items": []map[string]any{
			{"name": "alice", "age": 30},
			{"name": "bob", "age": 15},
			{"name": "carol", "age": 22},
		},
	}))
	if err != nil {
		t.Fatalf("transform_filter failed: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	items, _ := result["result"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 items after filter, got %d: %v", len(items), items)
	}
}

func TestTransform_unknownToolReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "transform_unknown", nil)
	if err == nil {
		t.Error("expected error for unknown tool, got nil")
	}
}
