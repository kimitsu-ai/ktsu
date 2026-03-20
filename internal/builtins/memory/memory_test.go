package memory

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

func TestMemory_storeThenRetrieveByID(t *testing.T) {
	s := New()
	out, err := s.Call(ctx, "memory_store", mustMarshal(map[string]string{"content": "remember this"}))
	if err != nil {
		t.Fatalf("memory_store failed: %v", err)
	}
	var stored map[string]string
	json.Unmarshal(out, &stored)
	id := stored["id"]
	if id == "" {
		t.Fatal("expected non-empty id from memory_store")
	}

	out2, err := s.Call(ctx, "memory_retrieve", mustMarshal(map[string]string{"id": id}))
	if err != nil {
		t.Fatalf("memory_retrieve failed: %v", err)
	}
	var result map[string]string
	json.Unmarshal(out2, &result)
	if result["content"] != "remember this" {
		t.Errorf("expected content %q, got %q", "remember this", result["content"])
	}
}

func TestMemory_retrieveMissingIDReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "memory_retrieve", mustMarshal(map[string]string{"id": "nope"}))
	if err == nil {
		t.Error("expected error for missing id, got nil")
	}
}

func TestMemory_searchReturnsEntriesContainingQuery(t *testing.T) {
	s := New()
	s.Call(ctx, "memory_store", mustMarshal(map[string]string{"content": "the cat sat on the mat"}))
	s.Call(ctx, "memory_store", mustMarshal(map[string]string{"content": "dogs are great pets"}))

	out, err := s.Call(ctx, "memory_search", mustMarshal(map[string]string{"query": "cat"}))
	if err != nil {
		t.Fatalf("memory_search failed: %v", err)
	}
	var result map[string][]map[string]string
	json.Unmarshal(out, &result)
	if len(result["results"]) != 1 || result["results"][0]["content"] != "the cat sat on the mat" {
		t.Errorf("expected 1 result with cat content, got %v", result["results"])
	}
}

func TestMemory_forgetRemovesEntry(t *testing.T) {
	s := New()
	out, _ := s.Call(ctx, "memory_store", mustMarshal(map[string]string{"content": "forget me"}))
	var stored map[string]string
	json.Unmarshal(out, &stored)
	id := stored["id"]

	s.Call(ctx, "memory_forget", mustMarshal(map[string]string{"id": id}))

	_, err := s.Call(ctx, "memory_retrieve", mustMarshal(map[string]string{"id": id}))
	if err == nil {
		t.Error("expected error after forget, got nil")
	}
}

func TestMemory_unknownToolReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "memory_unknown", nil)
	if err == nil {
		t.Error("expected error for unknown tool, got nil")
	}
}
