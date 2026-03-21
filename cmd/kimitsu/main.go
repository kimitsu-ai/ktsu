package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/kimitsu-ai/ktsu/internal/builtins"
	blobpkg "github.com/kimitsu-ai/ktsu/internal/builtins/blob"
	clipkg "github.com/kimitsu-ai/ktsu/internal/builtins/cli"
	envelopepkg "github.com/kimitsu-ai/ktsu/internal/builtins/envelope"
	formatpkg "github.com/kimitsu-ai/ktsu/internal/builtins/format"
	kvpkg "github.com/kimitsu-ai/ktsu/internal/builtins/kv"
	logpkg "github.com/kimitsu-ai/ktsu/internal/builtins/log"
	memorypkg "github.com/kimitsu-ai/ktsu/internal/builtins/memory"
	transformpkg "github.com/kimitsu-ai/ktsu/internal/builtins/transform"
	validatepkg "github.com/kimitsu-ai/ktsu/internal/builtins/validate"
	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/gateway"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator"
	"github.com/kimitsu-ai/ktsu/internal/runtime"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "kimitsu",
		Short: "Kimitsu — agentic pipeline framework",
	}
	root.AddCommand(startCmd())
	root.AddCommand(validateCmd())
	root.AddCommand(lockCmd())
	return root
}

func signalCtx() context.Context {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx
}

// envOr returns the value of envKey if set and non-empty, otherwise defaultVal.
func envOr(envKey, defaultVal string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return defaultVal
}

// envIntOr returns the integer value of envKey if set and parseable, otherwise defaultVal.
func envIntOr(envKey string, defaultVal int) int {
	if v := os.Getenv(envKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func startCmd() *cobra.Command {
	start := &cobra.Command{
		Use:   "start",
		Short: "Start a Kimitsu service",
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
	var envPath, workflowDir, projectDir, host string
	var port int
	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Start the Kimitsu orchestrator",
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
				EnvPath:     envPath,
				Env:         envCfg,
				WorkflowDir: workflowDir,
				ProjectDir:  projectDir,
				Host:        host,
				Port:        port,
			})
			log.Printf("starting %s", o)
			return o.Start(signalCtx())
		},
	}
	cmd.Flags().StringVar(&envPath, "env", "", "path to environment config (e.g. environments/dev.env.yaml)")
	cmd.Flags().StringVar(&workflowDir, "workflow-dir", "./workflows", "path to workflow directory")
	cmd.Flags().StringVar(&projectDir, "project-dir", ".", "project root for resolving inlet/outlet paths")
	cmd.Flags().StringVar(&host, "host", envOr("KTSU_ORCHESTRATOR_HOST", ""), "host interface to bind (env: KTSU_ORCHESTRATOR_HOST)")
	cmd.Flags().IntVar(&port, "port", envIntOr("KTSU_ORCHESTRATOR_PORT", 8080), "port to listen on (env: KTSU_ORCHESTRATOR_PORT)")
	return cmd
}

func startRuntimeCmd() *cobra.Command {
	var orchestratorURL, gatewayURL, host string
	var port int
	cmd := &cobra.Command{
		Use:   "runtime",
		Short: "Start the Kimitsu agent runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			r := runtime.New(runtime.Config{
				OrchestratorURL: orchestratorURL,
				LLMGatewayURL:   gatewayURL,
				Host:            host,
				Port:            port,
			})
			log.Printf("starting %s", r)
			return r.Start(signalCtx())
		},
	}
	cmd.Flags().StringVar(&orchestratorURL, "orchestrator",
		envOr("KTSU_ORCHESTRATOR_URL", "http://localhost:8080"),
		"orchestrator URL (env: KTSU_ORCHESTRATOR_URL)")
	cmd.Flags().StringVar(&gatewayURL, "gateway",
		envOr("KTSU_GATEWAY_URL", "http://localhost:8081"),
		"LLM gateway URL (env: KTSU_GATEWAY_URL)")
	cmd.Flags().StringVar(&host, "host", envOr("KTSU_RUNTIME_HOST", ""), "host interface to bind (env: KTSU_RUNTIME_HOST)")
	cmd.Flags().IntVar(&port, "port", envIntOr("KTSU_RUNTIME_PORT", 8082), "port to listen on (env: KTSU_RUNTIME_PORT)")
	return cmd
}

func startGatewayCmd() *cobra.Command {
	var configPath, host string
	var port int
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Start the Kimitsu LLM gateway",
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
				Host:          host,
				Port:          port,
			})
			log.Printf("starting %s", g)
			return g.Start(signalCtx())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "gateway.yaml", "path to gateway config")
	cmd.Flags().StringVar(&host, "host", envOr("KTSU_GATEWAY_HOST", ""), "host interface to bind (env: KTSU_GATEWAY_HOST)")
	cmd.Flags().IntVar(&port, "port", envIntOr("KTSU_GATEWAY_PORT", 8081), "port to listen on (env: KTSU_GATEWAY_PORT)")
	return cmd
}

func startBuiltinCmd(name string, defaultPort int, newFn func() builtins.BuiltinServer, hasOrchestrator bool) *cobra.Command {
	var host string
	var port int
	var orchestratorURL string
	cmd := &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Start the ktsu/%s built-in tool server", name),
		RunE: func(cmd *cobra.Command, args []string) error {
			b := newFn()
			return builtins.StartBuiltin(b, host, port, orchestratorURL)
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "host interface to bind")
	cmd.Flags().IntVar(&port, "port", defaultPort, "port to listen on")
	if hasOrchestrator {
		cmd.Flags().StringVar(&orchestratorURL, "orchestrator",
			envOr("KTSU_ORCHESTRATOR_URL", "http://localhost:8080"),
			"orchestrator URL for registration (env: KTSU_ORCHESTRATOR_URL)")
	}
	return cmd
}

func validateCmd() *cobra.Command {
	var envPath string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate Kimitsu configuration files",
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
		Short: "Generate kimitsu.lock.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("lock: not implemented")
			return nil
		},
	}
}
