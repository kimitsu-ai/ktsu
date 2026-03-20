package gateway

import (
	"context"
	"fmt"

	"github.com/your-org/sdd-services/internal/config"
)

type Config struct {
	ConfigPath    string
	GatewayConfig *config.GatewayConfig
	Port          int
}

type Gateway struct {
	cfg Config
}

func New(cfg Config) *Gateway {
	return &Gateway{cfg: cfg}
}

func (g *Gateway) Start(ctx context.Context) error {
	srv := newServer(g)
	return srv.serve(ctx)
}

func (g *Gateway) String() string {
	return fmt.Sprintf("Gateway(config=%s)", g.cfg.ConfigPath)
}
