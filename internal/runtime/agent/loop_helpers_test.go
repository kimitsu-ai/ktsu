package agent

import (
	"testing"
)

func TestMatchesPattern_ExactMatch(t *testing.T) {
	if !matchesPattern("delete-file", "delete-file") {
		t.Error("exact match should match")
	}
}

func TestMatchesPattern_NoMatch(t *testing.T) {
	if matchesPattern("read-file", "delete-file") {
		t.Error("different names should not match")
	}
}

func TestMatchesPattern_WildcardSuffix(t *testing.T) {
	if !matchesPattern("delete-file", "delete-*") {
		t.Error("prefix wildcard should match")
	}
	if matchesPattern("read-file", "delete-*") {
		t.Error("prefix wildcard should not match different prefix")
	}
}

func TestMatchesPattern_WildcardAll(t *testing.T) {
	if !matchesPattern("anything", "*") {
		t.Error("* should match any tool name")
	}
	if !matchesPattern("", "*") {
		t.Error("* should match empty string")
	}
}

func TestFindApprovalRule_Found(t *testing.T) {
	rules := []ToolApprovalRule{
		{Pattern: "read-*", OnReject: "recover"},
		{Pattern: "delete-file", OnReject: "fail"},
	}
	rule := findApprovalRule("delete-file", rules)
	if rule == nil {
		t.Fatal("expected rule, got nil")
	}
	if rule.OnReject != "fail" {
		t.Errorf("expected on_reject=fail, got %s", rule.OnReject)
	}
}

func TestFindApprovalRule_WildcardMatch(t *testing.T) {
	rules := []ToolApprovalRule{
		{Pattern: "delete-*", OnReject: "fail", TimeoutMS: 30000},
	}
	rule := findApprovalRule("delete-user", rules)
	if rule == nil {
		t.Fatal("expected rule, got nil")
	}
	if rule.TimeoutMS != 30000 {
		t.Errorf("expected timeout_ms=30000, got %d", rule.TimeoutMS)
	}
}

func TestFindApprovalRule_NotFound(t *testing.T) {
	rules := []ToolApprovalRule{
		{Pattern: "delete-*", OnReject: "fail"},
	}
	rule := findApprovalRule("read-file", rules)
	if rule != nil {
		t.Errorf("expected nil rule, got %+v", rule)
	}
}

func TestFindApprovalRule_EmptyRules(t *testing.T) {
	rule := findApprovalRule("any-tool", nil)
	if rule != nil {
		t.Errorf("expected nil for empty rules, got %+v", rule)
	}
}
