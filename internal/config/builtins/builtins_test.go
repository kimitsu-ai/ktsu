package builtins_test

import (
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/config/builtins"
)

func TestResolveWorkflowRef_hubRef_unsupported(t *testing.T) {
	_, err := builtins.ResolveWorkflowRef("author/some-workflow", ".")
	if err == nil {
		t.Fatal("expected error for hub ref (not implemented)")
	}
}
