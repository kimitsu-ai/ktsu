package log

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

type logEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	RunID   string `json:"run_id"`
}

func TestLog_writeThenReadByRunID(t *testing.T) {
	s := New()
	s.Call(ctx, "log_write", mustMarshal(map[string]string{"run_id": "run1", "level": "info", "message": "hello"}))
	s.Call(ctx, "log_write", mustMarshal(map[string]string{"run_id": "run2", "level": "info", "message": "other"}))

	out, err := s.Call(ctx, "log_read", mustMarshal(map[string]string{"run_id": "run1"}))
	if err != nil {
		t.Fatalf("log_read failed: %v", err)
	}
	var result map[string][]logEntry
	json.Unmarshal(out, &result)
	if len(result["entries"]) != 1 || result["entries"][0].Message != "hello" {
		t.Errorf("expected 1 entry with message 'hello', got %v", result["entries"])
	}
}

func TestLog_readReturnsEmptyForUnknownRunID(t *testing.T) {
	s := New()
	out, err := s.Call(ctx, "log_read", mustMarshal(map[string]string{"run_id": "none"}))
	if err != nil {
		t.Fatalf("log_read failed: %v", err)
	}
	var result map[string][]logEntry
	json.Unmarshal(out, &result)
	if len(result["entries"]) != 0 {
		t.Errorf("expected 0 entries, got %v", result["entries"])
	}
}

func TestLog_tailReturnsLastNEntries(t *testing.T) {
	s := New()
	s.Call(ctx, "log_write", mustMarshal(map[string]string{"message": "a"}))
	s.Call(ctx, "log_write", mustMarshal(map[string]string{"message": "b"}))
	s.Call(ctx, "log_write", mustMarshal(map[string]string{"message": "c"}))

	out, err := s.Call(ctx, "log_tail", mustMarshal(map[string]int{"n": 2}))
	if err != nil {
		t.Fatalf("log_tail failed: %v", err)
	}
	var result map[string][]logEntry
	json.Unmarshal(out, &result)
	if len(result["entries"]) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result["entries"]))
	}
	if result["entries"][0].Message != "b" || result["entries"][1].Message != "c" {
		t.Errorf("expected last 2 entries [b c], got %v", result["entries"])
	}
}

func TestLog_writeDefaultsLevelToInfo(t *testing.T) {
	s := New()
	s.Call(ctx, "log_write", mustMarshal(map[string]string{"run_id": "r", "message": "msg"}))

	out, _ := s.Call(ctx, "log_read", mustMarshal(map[string]string{"run_id": "r"}))
	var result map[string][]logEntry
	json.Unmarshal(out, &result)
	if result["entries"][0].Level != "info" {
		t.Errorf("expected default level 'info', got %q", result["entries"][0].Level)
	}
}

func TestLog_unknownToolReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "log_unknown", nil)
	if err == nil {
		t.Error("expected error for unknown tool, got nil")
	}
}
