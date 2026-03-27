package airlock

import (
	"fmt"
	"strings"

	"github.com/kimitsu-ai/ktsu/pkg/types"
)

// ValidateInput checks that the workflow input satisfies the declared schema.
// Only required field presence is checked.
func ValidateInput(input map[string]interface{}, schema map[string]interface{}) error {
	if schema == nil {
		return nil
	}
	if required, ok := schema["required"].([]interface{}); ok {
		for _, r := range required {
			field, _ := r.(string)
			if _, exists := input[field]; !exists {
				return fmt.Errorf("input missing required field %q", field)
			}
		}
	}
	return nil
}

// Validate checks that output conforms to the declared schema and
// does not contain reserved ktsu_ fields that were not set by the orchestrator.
func Validate(output map[string]interface{}, schema map[string]interface{}, reserved *types.ReservedFields) error {
	for key := range output {
		if strings.HasPrefix(key, types.ReservedPrefix) {
			return fmt.Errorf("output contains reserved field %q", key)
		}
	}

	if schema != nil {
		if required, ok := schema["required"].([]interface{}); ok {
			for _, r := range required {
				field, _ := r.(string)
				if _, exists := output[field]; !exists {
					return fmt.Errorf("output missing required field %q", field)
				}
			}
		}
	}

	return nil
}
