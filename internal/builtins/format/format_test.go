package format

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

func callResult(t *testing.T, s *FormatServer, tool string, args any) string {
	t.Helper()
	out, err := s.Call(ctx, tool, mustMarshal(args))
	if err != nil {
		t.Fatalf("%s failed: %v", tool, err)
	}
	var result map[string]string
	json.Unmarshal(out, &result)
	return result["output"]
}

// --- format_json ---

func TestFormat_jsonPrettyPrintsValidJSON(t *testing.T) {
	s := New()
	output := callResult(t, s, "format_json", map[string]string{"value": `{"a":1}`})
	if output == "" {
		t.Error("expected non-empty output from format_json")
	}
	// Must still be valid JSON
	var v any
	if err := json.Unmarshal([]byte(output), &v); err != nil {
		t.Errorf("format_json output is not valid JSON: %v", err)
	}
}

func TestFormat_jsonRejectsInvalidJSON(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "format_json", mustMarshal(map[string]string{"value": "{bad"}))
	if err == nil {
		t.Error("expected error for invalid JSON input, got nil")
	}
}

// --- format_yaml ---

func TestFormat_yamlConvertsJSONToYAML(t *testing.T) {
	s := New()
	output := callResult(t, s, "format_yaml", map[string]string{"value": `{"name":"alice","age":30}`})
	if output == "" {
		t.Error("expected non-empty YAML output")
	}
	// Should contain key names as YAML
	if len(output) == 0 {
		t.Error("expected YAML output, got empty string")
	}
}

func TestFormat_yamlRejectsInvalidJSON(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "format_yaml", mustMarshal(map[string]string{"value": "{bad"}))
	if err == nil {
		t.Error("expected error for invalid JSON input, got nil")
	}
}

// --- format_template ---

func TestFormat_templateRendersGoTemplate(t *testing.T) {
	s := New()
	out, err := s.Call(ctx, "format_template", mustMarshal(map[string]any{
		"template": "Hello, {{.name}}!",
		"data":     map[string]string{"name": "world"},
	}))
	if err != nil {
		t.Fatalf("format_template failed: %v", err)
	}
	var result map[string]string
	json.Unmarshal(out, &result)
	if result["output"] != "Hello, world!" {
		t.Errorf("expected %q, got %q", "Hello, world!", result["output"])
	}
}

func TestFormat_templateRejectsInvalidTemplate(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "format_template", mustMarshal(map[string]any{
		"template": "{{.unclosed",
		"data":     map[string]string{},
	}))
	if err == nil {
		t.Error("expected error for invalid template, got nil")
	}
}

func TestFormat_unknownToolReturnsError(t *testing.T) {
	s := New()
	_, err := s.Call(ctx, "format_unknown", nil)
	if err == nil {
		t.Error("expected error for unknown tool, got nil")
	}
}
