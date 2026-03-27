package airlock

import (
	"testing"

	"github.com/kimitsu-ai/ktsu/pkg/types"
)

func TestValidate_allowsNilOutput(t *testing.T) {
	err := Validate(nil, nil, &types.ReservedFields{})
	if err != nil {
		t.Errorf("expected no error for nil output, got %v", err)
	}
}

func TestValidate_allowsEmptyOutput(t *testing.T) {
	err := Validate(map[string]interface{}{}, nil, &types.ReservedFields{})
	if err != nil {
		t.Errorf("expected no error for empty output, got %v", err)
	}
}

func TestValidate_allowsNonReservedOutput(t *testing.T) {
	output := map[string]interface{}{
		"result": "ok",
		"score":  0.9,
	}
	err := Validate(output, nil, &types.ReservedFields{})
	if err != nil {
		t.Errorf("expected no error for clean output, got %v", err)
	}
}

func TestValidate_rejectsReservedKeyInOutput(t *testing.T) {
	output := map[string]interface{}{
		"result":                 "ok",
		"ktsu_injection_attempt": true,
	}
	err := Validate(output, nil, &types.ReservedFields{})
	if err == nil {
		t.Error("expected error when output contains reserved ktsu_ key, got nil")
	}
}

// Each documented reserved field must be rejected from user pipeline output.
// These tests name the exact fields so violations surface clearly in test output.

func TestValidate_rejectsInjectionAttemptField(t *testing.T) {
	// ktsu_injection_attempt: signals hijack attempt; orchestrator fails the entire run
	output := map[string]interface{}{"ktsu_injection_attempt": true}
	if err := Validate(output, nil, &types.ReservedFields{}); err == nil {
		t.Error("expected error for ktsu_injection_attempt in user output")
	}
}

func TestValidate_rejectsUntrustedContentField(t *testing.T) {
	// ktsu_untrusted_content: signals suspicious content; orchestrator fails the step
	output := map[string]interface{}{"ktsu_untrusted_content": true}
	if err := Validate(output, nil, &types.ReservedFields{}); err == nil {
		t.Error("expected error for ktsu_untrusted_content in user output")
	}
}

func TestValidate_rejectsConfidenceField(t *testing.T) {
	// ktsu_confidence: self-assessed confidence (0.0–1.0); orchestrator fails step if below threshold
	output := map[string]interface{}{"ktsu_confidence": 0.5}
	if err := Validate(output, nil, &types.ReservedFields{}); err == nil {
		t.Error("expected error for ktsu_confidence in user output")
	}
}

func TestValidate_rejectsLowQualityField(t *testing.T) {
	// ktsu_low_quality: signals unreliable output; orchestrator fails the step
	output := map[string]interface{}{"ktsu_low_quality": true}
	if err := Validate(output, nil, &types.ReservedFields{}); err == nil {
		t.Error("expected error for ktsu_low_quality in user output")
	}
}

func TestValidate_rejectsSkipReasonField(t *testing.T) {
	// ktsu_skip_reason: signals nothing to do; orchestrator marks step skipped
	output := map[string]interface{}{"ktsu_skip_reason": "no data"}
	if err := Validate(output, nil, &types.ReservedFields{}); err == nil {
		t.Error("expected error for ktsu_skip_reason in user output")
	}
}

func TestValidate_rejectsNeedsHumanField(t *testing.T) {
	// ktsu_needs_human: signals beyond agent authorization; orchestrator fails run with needs_human_review
	output := map[string]interface{}{"ktsu_needs_human": true}
	if err := Validate(output, nil, &types.ReservedFields{}); err == nil {
		t.Error("expected error for ktsu_needs_human in user output")
	}
}

func TestValidate_rejectsFlagsField(t *testing.T) {
	// ktsu_flags: observability labels; no pipeline effect, but still reserved
	output := map[string]interface{}{"ktsu_flags": []string{"suspicious"}}
	if err := Validate(output, nil, &types.ReservedFields{}); err == nil {
		t.Error("expected error for ktsu_flags in user output")
	}
}

func TestValidate_rejectsRationaleField(t *testing.T) {
	// ktsu_rationale: agent reasoning explanation; no pipeline effect, but still reserved
	output := map[string]interface{}{"ktsu_rationale": "I chose X because Y"}
	if err := Validate(output, nil, &types.ReservedFields{}); err == nil {
		t.Error("expected error for ktsu_rationale in user output")
	}
}

func TestValidate_nilSchemaSkipsSchemaValidation(t *testing.T) {
	output := map[string]interface{}{"result": "ok"}
	if err := Validate(output, nil, &types.ReservedFields{}); err != nil {
		t.Errorf("expected no error when schema is nil, got %v", err)
	}
}

func TestValidate_rejectsMissingRequiredField(t *testing.T) {
	schema := map[string]interface{}{
		"required": []interface{}{"result", "score"},
	}
	output := map[string]interface{}{
		"result": "ok",
		// "score" is missing
	}
	if err := Validate(output, schema, &types.ReservedFields{}); err == nil {
		t.Error("expected error when required field is missing from output")
	}
}

func TestValidate_allowsOutputWithAllRequiredFields(t *testing.T) {
	schema := map[string]interface{}{
		"required": []interface{}{"result", "score"},
	}
	output := map[string]interface{}{
		"result": "ok",
		"score":  0.95,
	}
	if err := Validate(output, schema, &types.ReservedFields{}); err != nil {
		t.Errorf("expected no error when all required fields present, got %v", err)
	}
}

func TestValidate_rejectsUnknownKtsuPrefixedField(t *testing.T) {
	// Any future ktsu_* key must also be rejected, not just the currently known ones
	output := map[string]interface{}{"ktsu_future_field": "value"}
	if err := Validate(output, nil, &types.ReservedFields{}); err == nil {
		t.Error("expected error for unknown ktsu_ prefixed field in user output")
	}
}

// ValidateInput tests

func TestValidateInput_nilSchema(t *testing.T) {
	err := ValidateInput(map[string]interface{}{"x": 1}, nil)
	if err != nil {
		t.Errorf("expected no error for nil schema, got %v", err)
	}
}

func TestValidateInput_noRequiredField(t *testing.T) {
	schema := map[string]interface{}{"type": "object"}
	err := ValidateInput(map[string]interface{}{}, schema)
	if err != nil {
		t.Errorf("expected no error when schema has no required, got %v", err)
	}
}

func TestValidateInput_allRequiredPresent(t *testing.T) {
	schema := map[string]interface{}{
		"required": []interface{}{"message", "user_id"},
	}
	input := map[string]interface{}{"message": "hello", "user_id": "u1"}
	if err := ValidateInput(input, schema); err != nil {
		t.Errorf("expected no error when all required fields present, got %v", err)
	}
}

func TestValidateInput_missingRequiredField(t *testing.T) {
	schema := map[string]interface{}{
		"required": []interface{}{"message", "user_id"},
	}
	input := map[string]interface{}{"message": "hello"}
	if err := ValidateInput(input, schema); err == nil {
		t.Error("expected error when required field missing, got nil")
	}
}

func TestValidateInput_emptyInput(t *testing.T) {
	schema := map[string]interface{}{
		"required": []interface{}{"message"},
	}
	if err := ValidateInput(map[string]interface{}{}, schema); err == nil {
		t.Error("expected error for empty input when required field declared")
	}
}
