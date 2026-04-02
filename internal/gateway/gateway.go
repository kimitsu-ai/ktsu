package gateway

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

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
		key, err := resolveAPIKey(pc)
		if err != nil {
			return nil, err
		}
		return openai.New(baseURL, key), nil
	case "anthropic":
		key, err := resolveAPIKey(pc)
		if err != nil {
			return nil, err
		}
		return anthropic.New("", key), nil
	default:
		return nil, fmt.Errorf("unknown provider type %q", pc.Type)
	}
}

// resolveAPIKey reads the api_key field from a provider config.
// Values prefixed with "env:" are resolved from environment variables.
func resolveAPIKey(pc config.ProviderConfig) (string, error) {
	raw := pc.Config["api_key"]
	if raw == "" {
		return "", fmt.Errorf("missing api_key")
	}
	if strings.HasPrefix(raw, "env:") {
		envVar := strings.TrimPrefix(raw, "env:")
		key := os.Getenv(envVar)
		if key == "" {
			return "", fmt.Errorf("env var %q is not set", envVar)
		}
		return key, nil
	}
	return raw, nil
}
