package airlock

import (
	"encoding/json"
	"fmt"

	"github.com/your-org/sdd-services/pkg/types"
)

// Validate checks that output conforms to the declared schema and
// does not contain reserved rss_ fields that were not set by the orchestrator.
func Validate(output map[string]interface{}, schema map[string]interface{}, reserved *types.ReservedFields) error {
	// stub
	_ = json.Marshal
	_ = fmt.Sprintf
	return nil
}
