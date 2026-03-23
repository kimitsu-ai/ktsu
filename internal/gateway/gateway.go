package gateway

import (
	"context"
	"fmt"
	"net/http"
	"os"

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
}

// Gateway is the LLM proxy service.
type Gateway struct {
	cfg        Config
	dispatcher Dispatchable
}

// New creates a Gateway from config, registering providers from GatewayConfig.
// Returns an error if any required provider cannot be initialized.
func New(cfg Config) (*Gateway, error) {
	if cfg.GatewayConfig == nil {
		return nil, fmt.Errorf("GatewayConfig is required")
	}
	provs, err := buildProviders(cfg.GatewayConfig)
	if err != nil {
		return nil, fmt.Errorf("build providers: %w", err)
	}
	d := NewDispatcher(cfg.GatewayConfig, provs)
	return &Gateway{cfg: cfg, dispatcher: d}, nil
}

// NewWithDispatcher creates a Gateway with a custom dispatcher (for testing).
func NewWithDispatcher(cfg Config, d Dispatchable) *Gateway {
	return &Gateway{cfg: cfg, dispatcher: d}
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

// buildProviders instantiates providers from the gateway config.
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
		apiKey := pc.Config["api_key_env"]
		if apiKey == "" {
			return nil, fmt.Errorf("missing api_key_env")
		}
		key := os.Getenv(apiKey)
		if key == "" {
			return nil, fmt.Errorf("env var %q is not set", apiKey)
		}
		return openai.New(baseURL, key), nil
	case "anthropic":
		apiKeyEnv := pc.Config["api_key_env"]
		if apiKeyEnv == "" {
			return nil, fmt.Errorf("missing api_key_env")
		}
		key := os.Getenv(apiKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("env var %q is not set", apiKeyEnv)
		}
		return anthropic.New("", key), nil
	default:
		return nil, fmt.Errorf("unknown provider type %q", pc.Type)
	}
}
