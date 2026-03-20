package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/your-org/sdd-services/internal/builtins"
	blobpkg "github.com/your-org/sdd-services/internal/builtins/blob"
	clipkg "github.com/your-org/sdd-services/internal/builtins/cli"
	envelopepkg "github.com/your-org/sdd-services/internal/builtins/envelope"
	formatpkg "github.com/your-org/sdd-services/internal/builtins/format"
	kvpkg "github.com/your-org/sdd-services/internal/builtins/kv"
	logpkg "github.com/your-org/sdd-services/internal/builtins/log"
	memorypkg "github.com/your-org/sdd-services/internal/builtins/memory"
	transformpkg "github.com/your-org/sdd-services/internal/builtins/transform"
	validatepkg "github.com/your-org/sdd-services/internal/builtins/validate"
	"github.com/your-org/sdd-services/internal/config"
	"github.com/your-org/sdd-services/internal/gateway"
	"github.com/your-org/sdd-services/internal/orchestrator"
	"github.com/your-org/sdd-services/internal/runtime"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "rss",
		Short: "RSS — Reliable Service Stack",
	}
	root.AddCommand(startCmd())
	root.AddCommand(validateCmd())
	root.AddCommand(lockCmd())
	return root
}

func signalCtx() context.Context {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	_ = stop
	return ctx
}

func startCmd() *cobra.Command {
	start := &cobra.Command{
		Use:   "start",
		Short: "Start an RSS service",
	}
	start.AddCommand(startOrchestratorCmd())
	start.AddCommand(startRuntimeCmd())
	start.AddCommand(startGatewayCmd())
	start.AddCommand(startBuiltinCmd("kv", 9100, func() builtins.BuiltinServer { return kvpkg.New() }, true))
	start.AddCommand(startBuiltinCmd("blob", 9101, func() builtins.BuiltinServer { return blobpkg.New() }, true))
	start.AddCommand(startBuiltinCmd("log", 9102, func() builtins.BuiltinServer { return logpkg.New() }, true))
	start.AddCommand(startBuiltinCmd("memory", 9103, func() builtins.BuiltinServer { return memorypkg.New() }, true))
	start.AddCommand(startBuiltinCmd("envelope", 9104, func() builtins.BuiltinServer { return envelopepkg.New() }, true))
	start.AddCommand(startBuiltinCmd("format", 9105, func() builtins.BuiltinServer { return formatpkg.New() }, false))
	start.AddCommand(startBuiltinCmd("validate", 9106, func() builtins.BuiltinServer { return validatepkg.New() }, false))
	start.AddCommand(startBuiltinCmd("transform", 9107, func() builtins.BuiltinServer { return transformpkg.New() }, false))
	start.AddCommand(startBuiltinCmd("cli", 9108, func() builtins.BuiltinServer { return clipkg.New() }, false))
	return start
}

func startOrchestratorCmd() *cobra.Command {
	var envPath string
	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Start the RSS orchestrator",
		RunE: func(cmd *cobra.Command, args []string) error {
			var envCfg *config.EnvConfig
			if envPath != "" {
				var err error
				envCfg, err = config.LoadEnv(envPath)
				if err != nil {
					return fmt.Errorf("load env: %w", err)
				}
			}
			o := orchestrator.New(orchestrator.Config{
				EnvPath: envPath,
				Env:     envCfg,
			})
			log.Printf("starting %s", o)
			return o.Start(signalCtx())
		},
	}
	cmd.Flags().StringVar(&envPath, "env", "", "path to environment config (e.g. environments/dev.env.yaml)")
	return cmd
}

func startRuntimeCmd() *cobra.Command {
	var orchestratorURL, gatewayURL string
	cmd := &cobra.Command{
		Use:   "runtime",
		Short: "Start the RSS agent runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			r := runtime.New(runtime.Config{
				OrchestratorURL: orchestratorURL,
				LLMGatewayURL:   gatewayURL,
			})
			log.Printf("starting %s", r)
			return r.Start(signalCtx())
		},
	}
	cmd.Flags().StringVar(&orchestratorURL, "orchestrator", "http://localhost:8080", "orchestrator URL")
	cmd.Flags().StringVar(&gatewayURL, "gateway", "http://localhost:8081", "LLM gateway URL")
	return cmd
}

func startGatewayCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Start the RSS LLM gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			var gatewayCfg *config.GatewayConfig
			if configPath != "" {
				var err error
				gatewayCfg, err = config.LoadGateway(configPath)
				if err != nil {
					return fmt.Errorf("load gateway config: %w", err)
				}
			}
			g := gateway.New(gateway.Config{
				ConfigPath:    configPath,
				GatewayConfig: gatewayCfg,
			})
			log.Printf("starting %s", g)
			return g.Start(signalCtx())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "gateway.yaml", "path to gateway config")
	return cmd
}

func startBuiltinCmd(name string, defaultPort int, newFn func() builtins.BuiltinServer, hasOrchestrator bool) *cobra.Command {
	var port int
	var orchestratorURL string
	cmd := &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Start the rss/%s built-in tool server", name),
		RunE: func(cmd *cobra.Command, args []string) error {
			b := newFn()
			return builtins.StartBuiltin(b, port, orchestratorURL)
		},
	}
	cmd.Flags().IntVar(&port, "port", defaultPort, "port to listen on")
	if hasOrchestrator {
		cmd.Flags().StringVar(&orchestratorURL, "orchestrator", "http://localhost:8080", "orchestrator URL for registration")
	}
	return cmd
}

func validateCmd() *cobra.Command {
	var envPath string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate RSS configuration files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if envPath != "" {
				if _, err := config.LoadEnv(envPath); err != nil {
					return fmt.Errorf("invalid env config: %w", err)
				}
				fmt.Printf("env config OK: %s\n", envPath)
			}
			fmt.Println("validation complete")
			return nil
		},
	}
	cmd.Flags().StringVar(&envPath, "env", "", "path to environment config")
	return cmd
}

func lockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: "Generate rss.lock.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("lock: not implemented")
			return nil
		},
	}
}
