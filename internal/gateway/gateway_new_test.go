package gateway_test

import (
	"os"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/config"
	gw "github.com/kimitsu-ai/ktsu/internal/gateway"
)

func ptr(s string) *string { return &s }

func gatewayConfigWithEnv() *config.GatewayConfig {
	return &config.GatewayConfig{
		Env: []config.EnvVarDecl{
			{Name: "ANTHROPIC_API_KEY", Secret: true},
		},
		Providers: []config.ProviderConfig{
			{
				Name: "anthropic",
				Type: "anthropic",
				Config: map[string]string{
					"api_key": "{{ env.ANTHROPIC_API_KEY }}",
				},
			},
		},
		ModelGroups: []config.ModelGroupConfig{
			{Name: "standard", Models: []string{"anthropic/claude-haiku-4-5-20251001"}, Strategy: "round_robin"},
		},
	}
}

func TestNew_envResolution_substitutesTemplates(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key")
	cfg := gw.Config{GatewayConfig: gatewayConfigWithEnv()}
	_, err := gw.New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_envResolution_failsOnMissingVar(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	cfg := gw.Config{GatewayConfig: gatewayConfigWithEnv()}
	_, err := gw.New(cfg)
	if err == nil {
		t.Fatal("expected error for missing env var, got nil")
	}
}

func TestNew_envResolution_usesDefault(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	gcfg := &config.GatewayConfig{
		Env: []config.EnvVarDecl{
			{Name: "ANTHROPIC_API_KEY", Secret: true, Default: ptr("sk-default")},
		},
		Providers: []config.ProviderConfig{
			{
				Name:   "anthropic",
				Type:   "anthropic",
				Config: map[string]string{"api_key": "{{ env.ANTHROPIC_API_KEY }}"},
			},
		},
		ModelGroups: []config.ModelGroupConfig{
			{Name: "standard", Models: []string{"anthropic/claude-haiku-4-5-20251001"}, Strategy: "round_robin"},
		},
	}
	_, err := gw.New(gw.Config{GatewayConfig: gcfg})
	if err != nil {
		t.Fatalf("unexpected error with default: %v", err)
	}
}

func TestNew_envResolution_failsOnUndeclaredRef(t *testing.T) {
	gcfg := &config.GatewayConfig{
		Env: []config.EnvVarDecl{}, // UNDECLARED_KEY not declared
		Providers: []config.ProviderConfig{
			{
				Name:   "anthropic",
				Type:   "anthropic",
				Config: map[string]string{"api_key": "{{ env.UNDECLARED_KEY }}"},
			},
		},
		ModelGroups: []config.ModelGroupConfig{
			{Name: "standard", Models: []string{"anthropic/claude-haiku-4-5-20251001"}, Strategy: "round_robin"},
		},
	}
	_, err := gw.New(gw.Config{GatewayConfig: gcfg})
	if err == nil {
		t.Fatal("expected error for undeclared env ref, got nil")
	}
}
