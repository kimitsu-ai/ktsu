package gateway

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers/anthropic"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers/openai"
)

// Config holds Gateway startup configuration.
type Config struct {
	ConfigPath    string
	GatewayConfig *config.GatewayConfig
	Host          string
	Port          int
	Logger        *log.Logger
}

// Gateway is the LLM proxy service.
type Gateway struct {
	cfg        Config
	dispatcher Dispatchable
	logger     *log.Logger
}

// envTemplateRe matches {{ env.VAR_NAME }} placeholders in config values.
var envTemplateRe = regexp.MustCompile(`\{\{\s*env\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// New creates a Gateway from config, registering providers from GatewayConfig.
// Env vars declared in GatewayConfig.Env are resolved from the process environment
// (with optional defaults) and substituted into all provider config string values.
// Returns an error if any required env var is unset or any provider cannot be initialized.
func New(cfg Config) (*Gateway, error) {
	if cfg.GatewayConfig == nil {
		return nil, fmt.Errorf("GatewayConfig is required")
	}
	envVars, err := resolveGatewayEnv(cfg.GatewayConfig.Env)
	if err != nil {
		return nil, fmt.Errorf("resolve env: %w", err)
	}
	for i := range cfg.GatewayConfig.Providers {
		if err := applyEnvSubstitution(cfg.GatewayConfig.Providers[i].Config, envVars); err != nil {
			return nil, fmt.Errorf("provider %q: %w", cfg.GatewayConfig.Providers[i].Name, err)
		}
	}
	provs, err := buildProviders(cfg.GatewayConfig)
	if err != nil {
		return nil, fmt.Errorf("build providers: %w", err)
	}
	d := NewDispatcher(cfg.GatewayConfig, provs)
	return &Gateway{cfg: cfg, dispatcher: d, logger: cfg.Logger}, nil
}

// NewWithDispatcher creates a Gateway with a custom dispatcher (for testing).
func NewWithDispatcher(cfg Config, d Dispatchable) *Gateway {
	return &Gateway{cfg: cfg, dispatcher: d, logger: cfg.Logger}
}

// Handler returns the HTTP handler (for testing without starting a listener).
func (g *Gateway) Handler() http.Handler {
	return newServer(g, g.dispatcher).Handler()
}

// Start begins serving requests.
func (g *Gateway) Start(ctx context.Context) error {
	srv := newServer(g, g.dispatcher)
	return srv.serve(ctx)
}

func (g *Gateway) String() string {
	return fmt.Sprintf("Gateway(config=%s)", g.cfg.ConfigPath)
}

// resolveGatewayEnv builds a map of env var name → value from declared env vars.
// For each declaration: reads os.Getenv, falls back to Default, fails if neither is set.
func resolveGatewayEnv(decls []config.EnvVarDecl) (map[string]string, error) {
	resolved := make(map[string]string, len(decls))
	for _, d := range decls {
		val := os.Getenv(d.Name)
		if val == "" && d.Default != nil {
			val = *d.Default
		}
		if val == "" {
			return nil, fmt.Errorf("env var %q is required but not set", d.Name)
		}
		resolved[d.Name] = val
	}
	return resolved, nil
}

// applyEnvSubstitution replaces {{ env.VAR }} templates in all string values of configMap.
// Returns an error if any template references a var not declared in envVars.
func applyEnvSubstitution(configMap map[string]string, envVars map[string]string) error {
	for k, v := range configMap {
		result, err := substituteEnvTemplates(v, envVars)
		if err != nil {
			return fmt.Errorf("config key %q: %w", k, err)
		}
		configMap[k] = result
	}
	return nil
}

// substituteEnvTemplates replaces all {{ env.VAR }} occurrences in s with resolved values.
func substituteEnvTemplates(s string, envVars map[string]string) (string, error) {
	var missing []string
	result := envTemplateRe.ReplaceAllStringFunc(s, func(match string) string {
		sub := envTemplateRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		name := sub[1]
		val, ok := envVars[name]
		if !ok {
			missing = append(missing, name)
			return ""
		}
		return val
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("env var(s) %v referenced but not declared in env: section", missing)
	}
	return result, nil
}

// buildProviders instantiates providers from the gateway config.
// All provider config values must already be fully resolved (no {{ env.VAR }} templates).
func buildProviders(cfg *config.GatewayConfig) (map[string]providers.Provider, error) {
	provs := make(map[string]providers.Provider, len(cfg.Providers))
	for _, pc := range cfg.Providers {
		p, err := buildProvider(pc)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", pc.Name, err)
		}
		provs[pc.Name] = p
	}
	return provs, nil
}

func buildProvider(pc config.ProviderConfig) (providers.Provider, error) {
	switch pc.Type {
	case "openai":
		baseURL := pc.Config["base_url"]
		if baseURL == "" {
			return nil, fmt.Errorf("missing base_url")
		}
		key := pc.Config["api_key"]
		if key == "" {
			return nil, fmt.Errorf("missing api_key")
		}
		return openai.New(baseURL, key), nil
	case "anthropic":
		key := pc.Config["api_key"]
		if key == "" {
			return nil, fmt.Errorf("missing api_key")
		}
		return anthropic.New("", key), nil
	default:
		return nil, fmt.Errorf("unknown provider type %q", pc.Type)
	}
}
