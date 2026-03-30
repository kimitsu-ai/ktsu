package config_test

import (
	"testing"
	"time"

	"github.com/kimitsu-ai/ktsu/internal/config"
)

func TestValidateApprovalPolicies_Valid(t *testing.T) {
	servers := []config.ServerRef{
		{
			Name: "my-server",
			Access: config.AccessConfig{
				Allowlist: []config.ToolAccess{
					{Name: "read-data"},
					{
						Name: "delete-*",
						RequireApproval: &config.ApprovalPolicy{
							OnReject:        "fail",
							Timeout:         30 * time.Minute,
							TimeoutBehavior: "reject",
						},
					},
				},
			},
		},
	}
	if err := config.ValidateApprovalPolicies(servers); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateApprovalPolicies_InvalidOnReject(t *testing.T) {
	servers := []config.ServerRef{
		{
			Name: "my-server",
			Access: config.AccessConfig{
				Allowlist: []config.ToolAccess{
					{
						Name: "delete-*",
						RequireApproval: &config.ApprovalPolicy{
							OnReject:        "maybe",
							TimeoutBehavior: "reject",
						},
					},
				},
			},
		},
	}
	err := config.ValidateApprovalPolicies(servers)
	if err == nil {
		t.Fatal("expected error for invalid on_reject, got nil")
	}
}

func TestValidateApprovalPolicies_InvalidTimeoutBehavior(t *testing.T) {
	servers := []config.ServerRef{
		{
			Name: "my-server",
			Access: config.AccessConfig{
				Allowlist: []config.ToolAccess{
					{
						Name: "delete-*",
						RequireApproval: &config.ApprovalPolicy{
							OnReject:        "fail",
							Timeout:         1 * time.Minute,
							TimeoutBehavior: "ignore",
						},
					},
				},
			},
		},
	}
	err := config.ValidateApprovalPolicies(servers)
	if err == nil {
		t.Fatal("expected error for invalid timeout_behavior, got nil")
	}
}

func TestValidateApprovalPolicies_NoTimeoutSkipsTimeoutBehaviorCheck(t *testing.T) {
	servers := []config.ServerRef{
		{
			Name: "my-server",
			Access: config.AccessConfig{
				Allowlist: []config.ToolAccess{
					{
						Name: "delete-*",
						RequireApproval: &config.ApprovalPolicy{
							OnReject:        "fail",
							Timeout:         0,
							TimeoutBehavior: "",
						},
					},
				},
			},
		},
	}
	if err := config.ValidateApprovalPolicies(servers); err != nil {
		t.Fatalf("unexpected error when no timeout: %v", err)
	}
}
