package builtins_test

import (
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/config/builtins"
)

func TestLoad_shippedSlackInput(t *testing.T) {
	wf, err := builtins.Load("ktsu/slack-input")
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

func TestLoad_shippedSlackReply(t *testing.T) {
	wf, err := builtins.Load("ktsu/slack-reply")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Webhooks != "execute" {
		t.Errorf("slack-reply should declare webhooks: execute, got %q", wf.Webhooks)
	}
}

func TestLoad_unknownShipped(t *testing.T) {
	_, err := builtins.Load("ktsu/does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown shipped workflow")
	}
}

func TestLoad_invalidPrefix(t *testing.T) {
	_, err := builtins.Load("author/some-workflow")
	if err == nil {
		t.Fatal("expected error for non-ktsu/ name")
	}
}

func TestList_returnsShippedNames(t *testing.T) {
	names := builtins.List()
	found := make(map[string]bool, len(names))
	for _, n := range names {
		found[n] = true
	}
	if !found["ktsu/slack-input"] {
		t.Error("expected ktsu/slack-input in List()")
	}
	if !found["ktsu/slack-reply"] {
		t.Error("expected ktsu/slack-reply in List()")
	}
}

func TestResolveWorkflowRef_shippedSlackInput(t *testing.T) {
	wf, err := builtins.ResolveWorkflowRef("ktsu/slack-input", ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "ktsu/slack-input" {
		t.Errorf("wrong name: %q", wf.Name)
	}
}

func TestResolveWorkflowRef_hubRef_unsupported(t *testing.T) {
	_, err := builtins.ResolveWorkflowRef("author/some-workflow", ".")
	if err == nil {
		t.Fatal("expected error for hub ref (not implemented)")
	}
}
