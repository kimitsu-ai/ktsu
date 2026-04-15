package config

import (
	"testing"
)

func TestResolveWorkflowRef_shippedSlackInput(t *testing.T) {
	wf, err := ResolveWorkflowRef("ktsu/slack-input", ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "ktsu/slack-input" {
		t.Errorf("wrong name: %q", wf.Name)
	}
	if wf.Visibility != "sub-workflow" {
		t.Errorf("shipped workflow should be sub-workflow, got %q", wf.Visibility)
	}
}

func TestResolveWorkflowRef_shippedSlackReply(t *testing.T) {
	wf, err := ResolveWorkflowRef("ktsu/slack-reply", ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "ktsu/slack-reply" {
		t.Errorf("wrong name: %q", wf.Name)
	}
	if wf.Visibility != "sub-workflow" {
		t.Errorf("shipped workflow should be sub-workflow, got %q", wf.Visibility)
	}
}

func TestResolveWorkflowRef_invalidShippedName(t *testing.T) {
	_, err := ResolveWorkflowRef("ktsu/nonexistent", ".")
	if err == nil {
		t.Fatal("expected error for nonexistent shipped workflow")
	}
}

func TestResolveWorkflowRef_hubInstallNotSupported(t *testing.T) {
	_, err := ResolveWorkflowRef("author/workflow", ".")
	if err == nil {
		t.Fatal("expected error for hub-installed workflow (not yet supported)")
	}
}
