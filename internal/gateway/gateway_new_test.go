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
	t.Setenv("ANTHROPIC_API_KEY", "") // registers cleanup to restore original value
	os.Unsetenv("ANTHROPIC_API_KEY")  // unset within this test
	cfg := gw.Config{GatewayConfig: gatewayConfigWithEnv()}
	_, err := gw.New(cfg)
	if err == nil {
		t.Fatal("expected error for missing env var, got nil")
	}
}

func TestNew_envResolution_usesDefault(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "") // registers cleanup to restore original value
	os.Unsetenv("ANTHROPIC_API_KEY")  // unset within this test
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

func TestNew_envResolution_emptyEnvVarDoesNotUseDefault(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "") // explicitly set to empty string (not unset)
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
	// var is set (to ""), so default should NOT apply; buildProvider should then fail on empty api_key
	_, err := gw.New(gw.Config{GatewayConfig: gcfg})
	if err == nil {
		t.Fatal("expected error because explicitly-empty env var should not use default")
	}
}

func TestNew_doesNotMutateCallerConfig(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-real-key")
	gcfg := &config.GatewayConfig{
		Env: []config.EnvVarDecl{
			{Name: "ANTHROPIC_API_KEY", Secret: true},
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
	original := gcfg.Providers[0].Config["api_key"]
	_, err := gw.New(gw.Config{GatewayConfig: gcfg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gcfg.Providers[0].Config["api_key"]; got != original {
		t.Errorf("New() mutated caller config: want %q, got %q", original, got)
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
