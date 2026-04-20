package config

import (
	"os"
	"testing"
)

// --- ResolveEnvValue ---

func TestResolveEnvValue_envRef(t *testing.T) {
	os.Setenv("TEST_PARAM_VAR", "resolved-value")
	defer os.Unsetenv("TEST_PARAM_VAR")
	got := ResolveEnvValue("env:TEST_PARAM_VAR")
	if got != "resolved-value" {
		t.Errorf("got %q want %q", got, "resolved-value")
	}
}

func TestResolveEnvValue_literal(t *testing.T) {
	got := ResolveEnvValue("plain-value")
	if got != "plain-value" {
		t.Errorf("got %q want %q", got, "plain-value")
	}
}

func TestResolveEnvValue_unsetEnvReturnsEmpty(t *testing.T) {
	os.Unsetenv("DEFINITELY_NOT_SET_XYZ")
	got := ResolveEnvValue("env:DEFINITELY_NOT_SET_XYZ")
	if got != "" {
		t.Errorf("expected empty string for unset env var, got %q", got)
	}
}

// --- ValidateSystemPromptStatic ---

func TestValidateSystemPromptStatic_noTemplates_ok(t *testing.T) {
	err := ValidateSystemPromptStatic("You are a helpful assistant.")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSystemPromptStatic_emptyPrompt_ok(t *testing.T) {
	if err := ValidateSystemPromptStatic(""); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSystemPromptStatic_templateExpression_errors(t *testing.T) {
	err := ValidateSystemPromptStatic("You are greeting {{ params.name }}.")
	if err == nil {
		t.Error("expected error for {{ }} in system prompt")
	}
}

func TestValidateSystemPromptStatic_multipleTemplates_errors(t *testing.T) {
	err := ValidateSystemPromptStatic("Hello {{ params.name }}, your id is {{ params.id }}.")
	if err == nil {
		t.Error("expected error for {{ }} in system prompt")
	}
}

// --- InterpolatePrompt ---

func TestInterpolatePrompt_replacesAll(t *testing.T) {
	got, err := InterpolatePrompt("Hello {{name}}, domain is {{domain}}.", map[string]string{
		"name":   "Alice",
		"domain": "billing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Hello Alice, domain is billing."
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestInterpolatePrompt_noRefs(t *testing.T) {
	got, err := InterpolatePrompt("No placeholders here.", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "No placeholders here." {
		t.Errorf("got %q", got)
	}
}

func TestInterpolatePrompt_missingValueReturnsError(t *testing.T) {
	_, err := InterpolatePrompt("Hello {{name}}.", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing value, got nil")
	}
}

func TestInterpolatePrompt_repeatedRef(t *testing.T) {
	got, err := InterpolatePrompt("{{name}} and {{name}} again.", map[string]string{"name": "Bob"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Bob and Bob again."
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// --- ResolveAgentParams ---

func strPtr(s string) *string { return &s }

func TestResolveAgentParams_usesDefault(t *testing.T) {
	declared := map[string]ParamDecl{
		"persona": {Description: "...", Default: strPtr("helpful assistant")},
	}
	got, _, err := ResolveAgentParams(declared, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["persona"] != "helpful assistant" {
		t.Errorf("got %q want %q", got["persona"], "helpful assistant")
	}
}

func TestResolveAgentParams_stepOverridesDefault(t *testing.T) {
	declared := map[string]ParamDecl{
		"persona": {Description: "...", Default: strPtr("helpful assistant")},
	}
	got, _, err := ResolveAgentParams(declared, map[string]any{"persona": "support rep"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["persona"] != "support rep" {
		t.Errorf("got %q want %q", got["persona"], "support rep")
	}
}

func TestResolveAgentParams_missingRequiredReturnsError(t *testing.T) {
	declared := map[string]ParamDecl{
		"domain": {Description: "..."},
	}
	_, _, err := ResolveAgentParams(declared, nil)
	if err == nil {
		t.Fatal("expected error for missing required param, got nil")
	}
}

func TestResolveAgentParams_resolvesEnvDefault(t *testing.T) {
	os.Setenv("TEST_PERSONA", "admin")
	defer os.Unsetenv("TEST_PERSONA")
	declared := map[string]ParamDecl{
		"persona": {Description: "...", Default: strPtr("env:TEST_PERSONA")},
	}
	got, _, err := ResolveAgentParams(declared, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["persona"] != "admin" {
		t.Errorf("got %q want %q", got["persona"], "admin")
	}
}

func TestResolveAgentParams_resolvesEnvStepOverride(t *testing.T) {
	os.Setenv("TEST_DOMAIN", "finance")
	defer os.Unsetenv("TEST_DOMAIN")
	declared := map[string]ParamDecl{
		"domain": {Description: "..."},
	}
	got, _, err := ResolveAgentParams(declared, map[string]any{"domain": "env:TEST_DOMAIN"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["domain"] != "finance" {
		t.Errorf("got %q want %q", got["domain"], "finance")
	}
}

func TestResolveAgentParams_emptyDeclared(t *testing.T) {
	got, _, err := ResolveAgentParams(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

// --- ResolveServerParams ---

func TestResolveServerParams_usesServerDefault(t *testing.T) {
	declared := map[string]ParamDecl{
		"namespace": {Description: "...", Default: strPtr("global")},
	}
	got, err := ResolveServerParams(declared, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["namespace"] != "global" {
		t.Errorf("got %q want %q", got["namespace"], "global")
	}
}

func TestResolveServerParams_agentRefOverridesDefault(t *testing.T) {
	declared := map[string]ParamDecl{
		"namespace": {Description: "...", Default: strPtr("global")},
	}
	got, err := ResolveServerParams(declared, map[string]string{"namespace": "team-ns"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["namespace"] != "team-ns" {
		t.Errorf("got %q want %q", got["namespace"], "team-ns")
	}
}

func TestResolveServerParams_stepOverridesAgentRef(t *testing.T) {
	declared := map[string]ParamDecl{
		"namespace": {Description: "...", Default: strPtr("global")},
	}
	got, err := ResolveServerParams(
		declared,
		map[string]string{"namespace": "team-ns"},
		map[string]any{"namespace": "user-123"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["namespace"] != "user-123" {
		t.Errorf("got %q want %q", got["namespace"], "user-123")
	}
}

func TestResolveServerParams_missingRequiredReturnsError(t *testing.T) {
	declared := map[string]ParamDecl{
		"namespace": {Description: "..."},
	}
	_, err := ResolveServerParams(declared, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing required server param, got nil")
	}
}

func TestResolveAgentParams_unsetEnvVarReturnsError(t *testing.T) {
	os.Unsetenv("DEFINITELY_NOT_SET_PARAM")
	declared := map[string]ParamDecl{
		"key": {Description: "...", Default: strPtr("env:DEFINITELY_NOT_SET_PARAM")},
	}
	_, _, err := ResolveAgentParams(declared, nil)
	if err == nil {
		t.Fatal("expected error for unset env var in default, got nil")
	}
}

func TestResolveAgentParams_nonStringStepValueReturnsError(t *testing.T) {
	declared := map[string]ParamDecl{
		"count": {Description: "..."},
	}
	_, _, err := ResolveAgentParams(declared, map[string]any{"count": 42})
	if err == nil {
		t.Fatal("expected error for non-string step param value, got nil")
	}
}

func TestResolveAgentParams_tracksSecretParam(t *testing.T) {
	t.Setenv("MY_TOKEN", "secret-val")
	declared := map[string]ParamDecl{
		"token": {Secret: true},
		"name":  {},
	}
	resolved, isSecret, err := ResolveAgentParams(declared, map[string]any{
		"token": "env:MY_TOKEN",
		"name":  "Kyle",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["token"] != "secret-val" {
		t.Errorf("expected token=secret-val, got %q", resolved["token"])
	}
	if !isSecret["token"] {
		t.Error("expected token marked as secret")
	}
	if isSecret["name"] {
		t.Error("expected name not marked as secret")
	}
}

func TestResolveAgentParams_secretParamLiteralStringErrors(t *testing.T) {
	declared := map[string]ParamDecl{
		"token": {Secret: true},
	}
	_, _, err := ResolveAgentParams(declared, map[string]any{
		"token": "literal-value",
	})
	if err == nil {
		t.Error("expected error: secret param must use env: source")
	}
}

func TestResolveAgentParams_secretParamTemplateStringErrors(t *testing.T) {
	declared := map[string]ParamDecl{
		"token": {Secret: true},
	}
	_, _, err := ResolveAgentParams(declared, map[string]any{
		"token": "already-resolved-value",
	})
	if err == nil {
		t.Error("expected error: resolved non-env: value for secret param")
	}
}

func TestResolveServerParams_unsetEnvVarReturnsError(t *testing.T) {
	os.Unsetenv("DEFINITELY_NOT_SET_SERVER_PARAM")
	declared := map[string]ParamDecl{
		"ns": {Description: "...", Default: strPtr("env:DEFINITELY_NOT_SET_SERVER_PARAM")},
	}
	_, err := ResolveServerParams(declared, nil, nil)
	if err == nil {
		t.Fatal("expected error for unset env var in server param default, got nil")
	}
}

// --- ResolveValue ---

func TestResolveValue_envRef_allowed(t *testing.T) {
	os.Setenv("TEST_RESOLVE_VAR", "hello")
	defer os.Unsetenv("TEST_RESOLVE_VAR")
	got, err := ResolveValue("env:TEST_RESOLVE_VAR", true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q want %q", got, "hello")
	}
}

func TestResolveValue_envRef_forbidden(t *testing.T) {
	os.Setenv("TEST_RESOLVE_VAR", "hello")
	defer os.Unsetenv("TEST_RESOLVE_VAR")
	_, err := ResolveValue("env:TEST_RESOLVE_VAR", false, nil)
	if err == nil {
		t.Fatal("expected error when env: used in non-root context")
	}
}

func TestResolveValue_envRef_unset(t *testing.T) {
	os.Unsetenv("DEFINITELY_NOT_SET_XYZ2")
	_, err := ResolveValue("env:DEFINITELY_NOT_SET_XYZ2", true, nil)
	if err == nil {
		t.Fatal("expected error for unset env var")
	}
}

func TestResolveValue_paramRef_found(t *testing.T) {
	got, err := ResolveValue("param:webhook_url", false, map[string]string{"webhook_url": "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com" {
		t.Errorf("got %q want %q", got, "https://example.com")
	}
}

func TestResolveValue_paramRef_missing(t *testing.T) {
	_, err := ResolveValue("param:missing", false, map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing param")
	}
}

func TestResolveValue_paramRef_nilContext(t *testing.T) {
	_, err := ResolveValue("param:anything", false, nil)
	if err == nil {
		t.Fatal("expected error when invocationParams is nil")
	}
}

func TestResolveValue_backtickLiteral(t *testing.T) {
	got, err := ResolveValue("`support-bot`", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "support-bot" {
		t.Errorf("got %q want %q", got, "support-bot")
	}
}

func TestResolveValue_backtickSingle(t *testing.T) {
	got, err := ResolveValue("`x`", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "x" {
		t.Errorf("got %q want %q", got, "x")
	}
}

func TestResolveValue_plainString(t *testing.T) {
	got, err := ResolveValue("hello", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestResolveValue_emptyString(t *testing.T) {
	got, err := ResolveValue("", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q want empty", got)
	}
}

// --- ParseParamsSchema ---

func TestParseParamsSchema_requiredParam(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"webhook_url"},
		"properties": map[string]interface{}{
			"webhook_url": map[string]interface{}{
				"type":        "string",
				"description": "Slack webhook URL",
			},
		},
	}
	got, err := ParseParamsSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decl, ok := got["webhook_url"]
	if !ok {
		t.Fatal("expected webhook_url in result")
	}
	if decl.Default != nil {
		t.Error("required param should have nil Default")
	}
}

func TestParseParamsSchema_optionalWithDefault(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"username": map[string]interface{}{
				"type":    "string",
				"default": "kimitsu",
			},
		},
	}
	got, err := ParseParamsSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decl, ok := got["username"]
	if !ok {
		t.Fatal("expected username")
	}
	if decl.Default == nil || *decl.Default != "kimitsu" {
		t.Errorf("expected default 'kimitsu', got %v", decl.Default)
	}
}

func TestParseParamsSchema_optionalNoDefault(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tag": map[string]interface{}{"type": "string"},
		},
	}
	got, err := ParseParamsSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decl := got["tag"]
	// optional with no explicit default gets empty string default (not required)
	if decl.Default == nil {
		t.Error("optional param with no default should get empty string default")
	}
	if *decl.Default != "" {
		t.Errorf("expected empty string default, got %q", *decl.Default)
	}
}

func TestParseParamsSchema_nilSchema(t *testing.T) {
	got, err := ParseParamsSchema(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for nil schema")
	}
}

func TestParseParamsSchema_description(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"token": map[string]interface{}{
				"type":        "string",
				"description": "API token",
			},
		},
	}
	got, err := ParseParamsSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["token"].Description != "API token" {
		t.Errorf("expected description 'API token', got %q", got["token"].Description)
	}
}

func TestParseParamsSchema_multipleParams(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"token"},
		"properties": map[string]interface{}{
			"token": map[string]interface{}{
				"type": "string",
			},
			"username": map[string]interface{}{
				"type":    "string",
				"default": "bot",
			},
		},
	}
	got, err := ParseParamsSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 params, got %d", len(got))
	}
	if got["token"].Default != nil {
		t.Error("token should be required (nil Default)")
	}
	if got["username"].Default == nil || *got["username"].Default != "bot" {
		t.Error("username should have default 'bot'")
	}
}
