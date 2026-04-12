package agent

import (
	"testing"
)

func TestCheckFatalReservedFields(t *testing.T) {
	tests := []struct {
		name    string
		output  map[string]any
		wantErr string
	}{
		{
			name:    "injection attempt",
			output:  map[string]any{"ktsu_injection_attempt": true},
			wantErr: "injection attempt detected",
		},
		{
			name:    "untrusted content",
			output:  map[string]any{"ktsu_untrusted_content": true},
			wantErr: "untrusted content detected",
		},
		{
			name:    "low quality",
			output:  map[string]any{"ktsu_low_quality": true},
			wantErr: "low quality output",
		},
		{
			name:    "needs human",
			output:  map[string]any{"ktsu_needs_human": true},
			wantErr: "needs_human_review",
		},
		{
			name:    "all false — no error",
			output:  map[string]any{"ktsu_injection_attempt": false, "result": "ok"},
			wantErr: "",
		},
		{
			name:    "no reserved fields — no error",
			output:  map[string]any{"category": "billing"},
			wantErr: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkFatalReservedFields(tc.output)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("want nil error, got %v", err)
				}
			} else {
				if err == nil || err.Error() != tc.wantErr {
					t.Errorf("want error %q, got %v", tc.wantErr, err)
				}
			}
		})
	}
}

func TestShouldReflect(t *testing.T) {
	tests := []struct {
		name         string
		output       map[string]any
		outputSchema map[string]any
		threshold    float64
		want         bool
	}{
		{
			name:         "no confidence in schema — always reflect",
			output:       map[string]any{"category": "billing"},
			outputSchema: map[string]any{"properties": map[string]any{"category": map[string]any{"type": "string"}}},
			threshold:    0.8,
			want:         true,
		},
		{
			name:         "confidence in schema, no threshold — always reflect",
			output:       map[string]any{"ktsu_confidence": 0.95},
			outputSchema: map[string]any{"properties": map[string]any{"ktsu_confidence": map[string]any{"type": "number"}}},
			threshold:    0,
			want:         true,
		},
		{
			name:         "confidence below threshold — reflect",
			output:       map[string]any{"ktsu_confidence": 0.6},
			outputSchema: map[string]any{"properties": map[string]any{"ktsu_confidence": map[string]any{"type": "number"}}},
			threshold:    0.8,
			want:         true,
		},
		{
			name:         "confidence meets threshold — skip reflect",
			output:       map[string]any{"ktsu_confidence": 0.85},
			outputSchema: map[string]any{"properties": map[string]any{"ktsu_confidence": map[string]any{"type": "number"}}},
			threshold:    0.8,
			want:         false,
		},
		{
			name:         "confidence equals threshold exactly — skip reflect",
			output:       map[string]any{"ktsu_confidence": 0.8},
			outputSchema: map[string]any{"properties": map[string]any{"ktsu_confidence": map[string]any{"type": "number"}}},
			threshold:    0.8,
			want:         false,
		},
		{
			name:         "no output schema — always reflect",
			output:       map[string]any{"result": "ok"},
			outputSchema: nil,
			threshold:    0.8,
			want:         true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldReflect(tc.output, tc.outputSchema, tc.threshold)
			if got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}
