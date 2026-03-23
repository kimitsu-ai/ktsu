package providers_test

import (
	"errors"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

func TestGatewayError_implements_error(t *testing.T) {
	err := &providers.GatewayError{
		Type:      "provider_error",
		Message:   "upstream failed",
		Retryable: true,
	}
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatal("expected GatewayError to satisfy errors.As")
	}
	if gwErr.Error() != "upstream failed" {
		t.Fatalf("expected 'upstream failed', got %q", gwErr.Error())
	}
}
