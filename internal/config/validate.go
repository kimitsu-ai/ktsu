package config

import "fmt"

// ValidateApprovalPolicies checks that all require_approval blocks within the
// given server refs have valid on_reject and timeout_behavior values.
func ValidateApprovalPolicies(servers []ServerRef) error {
	for _, srv := range servers {
		for _, ta := range srv.Access.Allowlist {
			if ta.RequireApproval == nil {
				continue
			}
			p := ta.RequireApproval
			if p.OnReject != "fail" && p.OnReject != "recover" {
				return fmt.Errorf(
					"server %q tool %q: require_approval.on_reject must be \"fail\" or \"recover\", got %q",
					srv.Name, ta.Name, p.OnReject,
				)
			}
			if p.Timeout > 0 {
				if p.TimeoutBehavior != "fail" && p.TimeoutBehavior != "reject" {
					return fmt.Errorf(
						"server %q tool %q: require_approval.timeout_behavior must be \"fail\" or \"reject\", got %q",
						srv.Name, ta.Name, p.TimeoutBehavior,
					)
				}
			}
		}
	}
	return nil
}
