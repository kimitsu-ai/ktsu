package config_test

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kimitsu-ai/ktsu/internal/config"
)

func TestToolAccessUnmarshal_PlainString(t *testing.T) {
	in := `
allowlist:
  - "read-*"
  - "write-data"
`
	var ac config.AccessConfig
	if err := yaml.Unmarshal([]byte(in), &ac); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ac.Allowlist) != 2 {
		t.Fatalf("want 2 entries, got %d", len(ac.Allowlist))
	}
	if ac.Allowlist[0].Name != "read-*" {
		t.Errorf("want read-*, got %s", ac.Allowlist[0].Name)
	}
	if ac.Allowlist[0].RequireApproval != nil {
		t.Error("want nil RequireApproval for plain string entry")
	}
}

func TestToolAccessUnmarshal_ObjectForm(t *testing.T) {
	in := `
allowlist:
  - "read-*"
  - name: "delete-*"
    require_approval:
      on_reject: fail
      timeout: 30m
      timeout_behavior: reject
`
	var ac config.AccessConfig
	if err := yaml.Unmarshal([]byte(in), &ac); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ac.Allowlist) != 2 {
		t.Fatalf("want 2 entries, got %d", len(ac.Allowlist))
	}
	entry := ac.Allowlist[1]
	if entry.Name != "delete-*" {
		t.Errorf("want delete-*, got %s", entry.Name)
	}
	if entry.RequireApproval == nil {
		t.Fatal("want non-nil RequireApproval")
	}
	if entry.RequireApproval.OnReject != "fail" {
		t.Errorf("want fail, got %s", entry.RequireApproval.OnReject)
	}
	if entry.RequireApproval.TimeoutBehavior != "reject" {
		t.Errorf("want reject, got %s", entry.RequireApproval.TimeoutBehavior)
	}
	if entry.RequireApproval.Timeout != 30*time.Minute {
		t.Errorf("want 30m, got %v", entry.RequireApproval.Timeout)
	}
}

func TestToolAccessUnmarshal_ObjectFormNoPolicy(t *testing.T) {
	in := `
allowlist:
  - name: "read-*"
`
	var ac config.AccessConfig
	if err := yaml.Unmarshal([]byte(in), &ac); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ac.Allowlist) != 1 {
		t.Fatalf("want 1 entry, got %d", len(ac.Allowlist))
	}
	if ac.Allowlist[0].Name != "read-*" {
		t.Errorf("want read-*, got %s", ac.Allowlist[0].Name)
	}
	if ac.Allowlist[0].RequireApproval != nil {
		t.Error("want nil RequireApproval for object form with no require_approval block")
	}
}
