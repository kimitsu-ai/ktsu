package blob

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

func TestBlob_putThenGetReturnsData(t *testing.T) {
	s := New()
	s.Call(ctx, "blob_put", mustMarshal(map[string]string{"key": "img/a", "data": "abc123"}))

	out, err := s.Call(ctx, "blob_get", mustMarshal(map[string]string{"key": "img/a"}))
	if err != nil {
		t.Fatalf("blob_get failed: %v", err)
	}
	var result map[string]string
	json.Unmarshal(out, &result)
	if result["data"] != "abc123" {
		t.Errorf("expected data %q, got %q", "abc123", result["data"])
	}
}

func TestBlob_getMissingKeyReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "blob_get", mustMarshal(map[string]string{"key": "missing"}))
	if err == nil {
		t.Error("expected error for missing key, got nil")
	}
}

func TestBlob_deleteRemovesKey(t *testing.T) {
	s := New()
	s.Call(ctx, "blob_put", mustMarshal(map[string]string{"key": "k", "data": "v"}))
	s.Call(ctx, "blob_delete", mustMarshal(map[string]string{"key": "k"}))

	_, err := s.Call(ctx, "blob_get", mustMarshal(map[string]string{"key": "k"}))
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestBlob_listReturnsKeysMatchingPrefix(t *testing.T) {
	s := New()
	s.Call(ctx, "blob_put", mustMarshal(map[string]string{"key": "img/a", "data": "1"}))
	s.Call(ctx, "blob_put", mustMarshal(map[string]string{"key": "img/b", "data": "2"}))
	s.Call(ctx, "blob_put", mustMarshal(map[string]string{"key": "doc/c", "data": "3"}))

	out, err := s.Call(ctx, "blob_list", mustMarshal(map[string]string{"prefix": "img/"}))
	if err != nil {
		t.Fatalf("blob_list failed: %v", err)
	}
	var result map[string][]string
	json.Unmarshal(out, &result)
	if len(result["keys"]) != 2 {
		t.Errorf("expected 2 keys with prefix img/, got %v", result["keys"])
	}
}

func TestBlob_listEmptyPrefixReturnsAll(t *testing.T) {
	s := New()
	s.Call(ctx, "blob_put", mustMarshal(map[string]string{"key": "a", "data": "1"}))
	s.Call(ctx, "blob_put", mustMarshal(map[string]string{"key": "b", "data": "2"}))

	out, err := s.Call(ctx, "blob_list", mustMarshal(map[string]string{"prefix": ""}))
	if err != nil {
		t.Fatalf("blob_list failed: %v", err)
	}
	var result map[string][]string
	json.Unmarshal(out, &result)
	if len(result["keys"]) != 2 {
		t.Errorf("expected 2 keys, got %v", result["keys"])
	}
}

func TestBlob_unknownToolReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "blob_unknown", nil)
	if err == nil {
		t.Error("expected error for unknown tool, got nil")
	}
}
