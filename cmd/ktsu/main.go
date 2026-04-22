package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kimitsu-ai/ktsu/internal/builtins"
	envelopepkg "github.com/kimitsu-ai/ktsu/internal/builtins/envelope"
	"github.com/kimitsu-ai/ktsu/internal/config"
	configbuiltins "github.com/kimitsu-ai/ktsu/internal/config/builtins"
	"github.com/kimitsu-ai/ktsu/internal/gateway"
	"github.com/kimitsu-ai/ktsu/internal/hub"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/dag"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
	"github.com/kimitsu-ai/ktsu/internal/runtime"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func hubEnabled() bool {
	return os.Getenv("KTSU_HUB_ENABLED") == "true"
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "ktsu",
		Short:        "Kimitsu — agentic pipeline framework",
		SilenceUsage: true,
	}
	root.AddCommand(startCmd())
	root.AddCommand(validateCmd())
	root.AddCommand(invokeCmd())
	root.AddCommand(lockCmd())
	root.AddCommand(newCmd())
	root.AddCommand(runsGroupCmd())
	root.AddCommand(workflowGroupCmd())
	if hubEnabled() {
		root.AddCommand(hubCmd())
	}
	return root
}

func hubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hub",
		Short: "Interact with the ktsuhub workflow registry",
	}
	cmd.AddCommand(hubLoginCmd())
	cmd.AddCommand(hubInstallCmd())
	cmd.AddCommand(hubUpdateCmd())
	cmd.AddCommand(hubPublishCmd())
	cmd.AddCommand(hubSearchCmd())
	return cmd
}

func hubLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate with GitHub",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("hub login: not yet implemented")
		},
	}
}

func hubInstallCmd() *cobra.Command {
	var cacheDir string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "install <target>[@ref]",
		Short: "Install a workflow from ktsuhub or a git repo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := args[0]
			// Only split on @ for non-URL targets. URLs (https://...) may contain @
			// as part of the authority (e.g. user@host), not as a ref separator.
			var target, ref string
			if strings.Contains(raw, "://") {
				target = raw
			} else {
				target, ref, _ = strings.Cut(raw, "@")
			}
			cd := cacheDir
			if strings.HasPrefix(cd, "~/") {
				if home, err := os.UserHomeDir(); err == nil {
					cd = filepath.Join(home, cd[2:])
				}
			}
			return hub.Install(hub.InstallOpts{
				Target:   target,
				Ref:      ref,
				CacheDir: cd,
				LockPath: "ktsuhub.lock.yaml",
				DryRun:   dryRun,
			})
		},
	}
	cmd.Flags().StringVar(&cacheDir, "cache-dir",
		envOr("KTSU_CACHE_DIR", "~/.ktsu/cache"),
		"local cache directory (env: KTSU_CACHE_DIR)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without making changes")
	return cmd
}

func hubUpdateCmd() *cobra.Command {
	var latest, dryRun bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Re-resolve all entries in ktsuhub.lock.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return hub.Update(hub.UpdateOpts{
				LockPath: "ktsuhub.lock.yaml",
				Latest:   latest,
				DryRun:   dryRun,
			})
		},
	}
	cmd.Flags().BoolVar(&latest, "latest", false, "also update pinned version entries")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview changes without writing")
	return cmd
}

func hubPublishCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "publish",
		Short: "Publish workflows to ktsuhub",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("hub publish: not yet implemented")
		},
	}
}

func hubSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search ktsuhub from the CLI",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("hub search: not yet implemented")
		},
	}
	cmd.Flags().String("tag", "", "filter by tag")
	cmd.Flags().Int("limit", 10, "number of results to return")
	return cmd
}

func runsGroupCmd() *cobra.Command {
	var orchestratorURL, workflow, status string
	var limit int

	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List and inspect workflow runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			u := orchestratorURL + "/runs"
			params := url.Values{}
			if workflow != "" {
				params.Set("workflow", workflow)
			}
			if status != "" {
				params.Set("status", status)
			}
			if limit > 0 {
				params.Set("limit", strconv.Itoa(limit))
			}
			if len(params) > 0 {
				u += "?" + params.Encode()
			}

			resp, err := doRequest(cmd.Context(), "GET", u, nil)
			if err != nil {
				return fmt.Errorf("runs: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				var errResult map[string]interface{}
				json.NewDecoder(resp.Body).Decode(&errResult)
				if msg, ok := errResult["error"].(string); ok {
					return fmt.Errorf("runs: %s (status %d)", msg, resp.StatusCode)
				}
				return fmt.Errorf("runs: orchestrator returned status %d", resp.StatusCode)
			}

			var result struct {
				Runs []struct {
					ID           string    `json:"id"`
					WorkflowName string    `json:"workflow_name"`
					Status       string    `json:"status"`
					CreatedAt    time.Time `json:"created_at"`
					UpdatedAt    time.Time `json:"updated_at"`
					Error        string    `json:"error,omitempty"`
				} `json:"runs"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("runs: decode: %w", err)
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "RUN ID\tWORKFLOW\tSTATUS\tSTARTED\tDURATION")
			for _, r := range result.Runs {
				duration := "-"
				if r.Status == "complete" || r.Status == "failed" {
					d := r.UpdatedAt.Sub(r.CreatedAt).Round(time.Second)
					duration = d.String()
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					r.ID, r.WorkflowName, r.Status,
					r.CreatedAt.Local().Format("2006-01-02 15:04:05"),
					duration)
			}
			tw.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&orchestratorURL, "orchestrator",
		envOr("KTSU_ORCHESTRATOR_URL", "http://localhost:5050"),
		"orchestrator URL (env: KTSU_ORCHESTRATOR_URL)")
	cmd.Flags().StringVar(&workflow, "workflow", "", "filter by workflow name")
	cmd.Flags().StringVar(&status, "status", "", "filter by status (pending, running, complete, failed)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results (default 50)")

	cmd.AddCommand(runsGetCmd())
	return cmd
}

func runsGetCmd() *cobra.Command {
	var orchestratorURL string
	cmd := &cobra.Command{
		Use:   "get <run_id>",
		Short: "Print the envelope for a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			resp, err := doRequest(cmd.Context(), "GET", orchestratorURL+"/runs/"+runID+"/envelope", nil)
			if err != nil {
				return fmt.Errorf("runs get: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				var result map[string]interface{}
				json.NewDecoder(resp.Body).Decode(&result)
				if msg, ok := result["error"].(string); ok {
					return fmt.Errorf("runs get: %s (status %d)", msg, resp.StatusCode)
				}
				return fmt.Errorf("runs get: orchestrator returned status %d", resp.StatusCode)
			}

			var envelope interface{}
			if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
				return fmt.Errorf("runs get: decode: %w", err)
			}
			data, _ := json.MarshalIndent(envelope, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	cmd.Flags().StringVar(&orchestratorURL, "orchestrator",
		envOr("KTSU_ORCHESTRATOR_URL", "http://localhost:5050"),
		"orchestrator URL (env: KTSU_ORCHESTRATOR_URL)")
	return cmd
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

func doRequest(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if method == "POST" || method == "PUT" {
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

func startCmd() *cobra.Command {
	var (
		all bool
		// orchestrator flags
		envPath, workflowDir, ownURL, projectDir string
		storeType, dbPath                        string
		orchHost                                 string
		orchPort                                 int
		// gateway flags
		gatewayConfigPath string
		gwHost            string
		gwPort            int
		// runtime flags
		rtHost string
		rtPort int
	)

	start := &cobra.Command{
		Use:   "start",
		Short: "Start a Kimitsu service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all {
				return cmd.Help()
			}

			var envCfg *config.EnvConfig
			if envPath != "" {
				var err error
				envCfg, err = config.LoadEnv(envPath)
				if err != nil {
					return fmt.Errorf("load env: %w", err)
				}
			}

			gatewayCfg, err := config.LoadGateway(gatewayConfigPath)
			if err != nil {
				return fmt.Errorf("load gateway config: %w", err)
			}

			orchLogger := log.New(os.Stderr, "[orchestrator] ", log.LstdFlags)
			gwLogger := log.New(os.Stderr, "[gateway]      ", log.LstdFlags)
			rtLogger := log.New(os.Stderr, "[runtime]      ", log.LstdFlags)

			orchURL := fmt.Sprintf("http://localhost:%d", orchPort)
			gwURL := fmt.Sprintf("http://localhost:%d", gwPort)
			rtURL := fmt.Sprintf("http://localhost:%d", rtPort)
			log.Printf("starting all services...")

			g, err := gateway.New(gateway.Config{
				ConfigPath:    gatewayConfigPath,
				GatewayConfig: gatewayCfg,
				Host:          gwHost,
				Port:          gwPort,
				Logger:        gwLogger,
			})
			if err != nil {
				return fmt.Errorf("gateway init: %w", err)
			}

			orchOwnURL := ownURL
			if orchOwnURL == "" {
				orchOwnURL = orchURL
			}

			o := orchestrator.New(orchestrator.Config{
				EnvPath:     envPath,
				Env:         envCfg,
				WorkflowDir: workflowDir,
				Host:        orchHost,
				Port:        orchPort,
				RuntimeURL:  rtURL,
				GatewayURL:  gwURL,
				OwnURL:      orchOwnURL,
				ProjectDir:  projectDir,
				StoreType:   state.StoreType(storeType),
				StoreDSN:    dbPath,
				Logger:      orchLogger,
			})

			r := runtime.New(runtime.Config{
				OrchestratorURL: orchURL,
				LLMGatewayURL:   gwURL,
				Host:            rtHost,
				Port:            rtPort,
				Logger:          rtLogger,
			})

			sigCtx := signalCtx()
			ctx, cancel := context.WithCancel(sigCtx)
			defer cancel()

			errc := make(chan error, 3)
			go func() {
				if err := g.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errc <- fmt.Errorf("gateway: %w", err)
					cancel()
				}
			}()
			go func() {
				if err := o.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errc <- fmt.Errorf("orchestrator: %w", err)
					cancel()
				}
			}()
			go func() {
				if err := r.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errc <- fmt.Errorf("runtime: %w", err)
					cancel()
				}
			}()

			select {
			case err := <-errc:
				return err
			case <-sigCtx.Done():
				return nil
			}
		},
	}

	start.Flags().BoolVar(&all, "all", false, "Start orchestrator, gateway, and runtime in a single process")
	start.Flags().StringVar(&envPath, "env", "", "path to environment config (e.g. environments/dev.env.yaml)")
	start.Flags().StringVar(&workflowDir, "workflow-dir", "./workflows", "path to workflow directory")
	start.Flags().StringVar(&ownURL, "own-url", envOr("KTSU_OWN_URL", ""), "orchestrator's own URL for callbacks (env: KTSU_OWN_URL)")
	start.Flags().StringVar(&projectDir, "project-dir", envOr("KTSU_PROJECT_DIR", "."), "project root for resolving agent/server paths (env: KTSU_PROJECT_DIR)")
	start.Flags().StringVar(&orchHost, "orchestrator-host", envOr("KTSU_ORCHESTRATOR_HOST", ""), "orchestrator bind host (env: KTSU_ORCHESTRATOR_HOST)")
	start.Flags().IntVar(&orchPort, "orchestrator-port", envIntOr("KTSU_ORCHESTRATOR_PORT", 5050), "orchestrator port (env: KTSU_ORCHESTRATOR_PORT)")
	start.Flags().StringVar(&storeType, "store-type", envOr("KTSU_STORE_TYPE", "memory"), "orchestrator store type: memory, sqlite (env: KTSU_STORE_TYPE)")
	start.Flags().StringVar(&dbPath, "db-path", envOr("KTSU_DB_PATH", "ktsu.db"), "orchestrator database path for sqlite (env: KTSU_DB_PATH)")
	start.Flags().StringVar(&gatewayConfigPath, "gateway-config", "gateway.yaml", "path to gateway config")
	start.Flags().StringVar(&gwHost, "gateway-host", envOr("KTSU_GATEWAY_HOST", ""), "gateway bind host (env: KTSU_GATEWAY_HOST)")
	start.Flags().IntVar(&gwPort, "gateway-port", envIntOr("KTSU_GATEWAY_PORT", 5052), "gateway port (env: KTSU_GATEWAY_PORT)")
	start.Flags().StringVar(&rtHost, "runtime-host", envOr("KTSU_RUNTIME_HOST", ""), "runtime bind host (env: KTSU_RUNTIME_HOST)")
	start.Flags().IntVar(&rtPort, "runtime-port", envIntOr("KTSU_RUNTIME_PORT", 5051), "runtime port (env: KTSU_RUNTIME_PORT)")

	start.AddCommand(startOrchestratorCmd())
	start.AddCommand(startRuntimeCmd())
	start.AddCommand(startGatewayCmd())
	start.AddCommand(startBuiltinCmd("envelope", 9104, func() builtins.BuiltinServer { return envelopepkg.New() }, true))
	return start
}

func startOrchestratorCmd() *cobra.Command {
	var envPath, workflowDir, host, runtimeURL, gatewayURL, ownURL, projectDir string
	var storeType, dbPath string
	var port int
	var workspaces []string
	var noHubLock bool
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
			orchOwnURL := ownURL
			if orchOwnURL == "" {
				h := host
				if h == "" {
					h = "localhost"
				}
				orchOwnURL = fmt.Sprintf("http://%s:%d", h, port)
			}
			var extraWorkspaces []orchestrator.Workspace
			for _, ws := range workspaces {
				if strings.HasPrefix(ws, "~/") {
					if home, err := os.UserHomeDir(); err == nil {
						ws = filepath.Join(home, ws[2:])
					}
				}
				extraWorkspaces = append(extraWorkspaces, orchestrator.Workspace{ProjectDir: ws})
			}

			o := orchestrator.New(orchestrator.Config{
				EnvPath:     envPath,
				Env:         envCfg,
				WorkflowDir: workflowDir,
				Host:        host,
				Port:        port,
				RuntimeURL:  runtimeURL,
				GatewayURL:  gatewayURL,
				OwnURL:      orchOwnURL,
				ProjectDir:  projectDir,
				StoreType:   state.StoreType(storeType),
				StoreDSN:    dbPath,
				Workspaces:  extraWorkspaces,
				NoHubLock:   noHubLock,
			})
			log.Printf("starting %s", o)
			return o.Start(signalCtx())
		},
	}
	cmd.Flags().StringVar(&envPath, "env", "", "path to environment config (e.g. environments/dev.env.yaml)")
	cmd.Flags().StringVar(&workflowDir, "workflow-dir", "./workflows", "path to workflow directory")
	cmd.Flags().StringVar(&host, "host", envOr("KTSU_ORCHESTRATOR_HOST", ""), "host interface to bind (env: KTSU_ORCHESTRATOR_HOST)")
	cmd.Flags().IntVar(&port, "port", envIntOr("KTSU_ORCHESTRATOR_PORT", 5050), "port to listen on (env: KTSU_ORCHESTRATOR_PORT)")
	cmd.Flags().StringVar(&runtimeURL, "runtime-url",
		envOr("KTSU_RUNTIME_URL", ""),
		"agent runtime URL (env: KTSU_RUNTIME_URL)")
	cmd.Flags().StringVar(&gatewayURL, "gateway-url",
		envOr("KTSU_GATEWAY_URL", ""),
		"LLM gateway URL (env: KTSU_GATEWAY_URL)")
	cmd.Flags().StringVar(&ownURL, "own-url",
		envOr("KTSU_OWN_URL", ""),
		"orchestrator's own URL for callbacks (env: KTSU_OWN_URL)")
	cmd.Flags().StringVar(&projectDir, "project-dir",
		envOr("KTSU_PROJECT_DIR", "."),
		"project root for resolving agent/server paths (env: KTSU_PROJECT_DIR)")
	cmd.Flags().StringVar(&storeType, "store-type",
		envOr("KTSU_STORE_TYPE", "memory"),
		"orchestrator store type: memory, sqlite (env: KTSU_STORE_TYPE)")
	cmd.Flags().StringVar(&dbPath, "db-path",
		envOr("KTSU_DB_PATH", "ktsu.db"),
		"orchestrator database path for sqlite (env: KTSU_DB_PATH)")
	cmd.Flags().StringArrayVar(&workspaces, "workspace", nil, "additional workspace root (repeatable)")
	cmd.Flags().BoolVar(&noHubLock, "no-hub-lock", false, "ignore ktsuhub.lock.yaml even if present")
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
		envOr("KTSU_ORCHESTRATOR_URL", "http://localhost:5050"),
		"orchestrator URL (env: KTSU_ORCHESTRATOR_URL)")
	cmd.Flags().StringVar(&gatewayURL, "gateway",
		envOr("KTSU_GATEWAY_URL", "http://localhost:5052"),
		"LLM gateway URL (env: KTSU_GATEWAY_URL)")
	cmd.Flags().StringVar(&host, "host", envOr("KTSU_RUNTIME_HOST", ""), "host interface to bind (env: KTSU_RUNTIME_HOST)")
	cmd.Flags().IntVar(&port, "port", envIntOr("KTSU_RUNTIME_PORT", 5051), "port to listen on (env: KTSU_RUNTIME_PORT)")
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
			g, err := gateway.New(gateway.Config{
				ConfigPath:    configPath,
				GatewayConfig: gatewayCfg,
				Host:          host,
				Port:          port,
			})
			if err != nil {
				return fmt.Errorf("gateway init: %w", err)
			}
			log.Printf("starting %s", g)
			return g.Start(signalCtx())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "gateway.yaml", "path to gateway config")
	cmd.Flags().StringVar(&host, "host", envOr("KTSU_GATEWAY_HOST", ""), "host interface to bind (env: KTSU_GATEWAY_HOST)")
	cmd.Flags().IntVar(&port, "port", envIntOr("KTSU_GATEWAY_PORT", 5052), "port to listen on (env: KTSU_GATEWAY_PORT)")
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
			envOr("KTSU_ORCHESTRATOR_URL", "http://localhost:5050"),
			"orchestrator URL for registration (env: KTSU_ORCHESTRATOR_URL)")
	}
	return cmd
}

func invokeCmd() *cobra.Command {
	var orchestratorURL, inputJSON string
	var wait bool
	cmd := &cobra.Command{
		Use:   "invoke <workflow>",
		Short: "Invoke a workflow on the orchestrator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflow := args[0]
			resp, err := doRequest(cmd.Context(), "POST", orchestratorURL+"/invoke/"+workflow, strings.NewReader(inputJSON))
			if err != nil {
				return fmt.Errorf("invoke: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
				var result map[string]interface{}
				json.NewDecoder(resp.Body).Decode(&result)
				if msg, ok := result["error"].(string); ok {
					return fmt.Errorf("invoke: %s (status %d)", msg, resp.StatusCode)
				}
				return fmt.Errorf("invoke: orchestrator returned status %d", resp.StatusCode)
			}

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("invoke: decode response: %w", err)
			}

			runID, _ := result["run_id"].(string)
			if runID == "" {
				return fmt.Errorf("invoke: orchestrator did not return a run_id")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "run_id: %s\n", runID)
			if !wait {
				return nil
			}

			// Poll GET /runs/{run_id} until the run reaches a terminal status.
			for {
				time.Sleep(1 * time.Second)
				r, err := doRequest(cmd.Context(), "GET", orchestratorURL+"/runs/"+runID, nil)
				if err != nil {
					return fmt.Errorf("poll: %w", err)
				}
				if r.StatusCode != http.StatusOK {
					r.Body.Close()
					return fmt.Errorf("poll: status %d", r.StatusCode)
				}
				var status map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&status); err != nil {
					r.Body.Close()
					return fmt.Errorf("poll: decode: %w", err)
				}
				r.Body.Close()
				run, _ := status["run"].(map[string]interface{})
				s, _ := run["status"].(string)
				if s != "running" && s != "pending" && s != "" {
					data, _ := json.MarshalIndent(status, "", "  ")
					fmt.Fprintln(cmd.OutOrStdout(), string(data))
					return nil
				}
			}
		},
	}
	cmd.Flags().StringVar(&inputJSON, "input", "{}", "JSON input for the workflow")
	cmd.Flags().BoolVar(&wait, "wait", false, "poll until the run completes and print result")
	cmd.Flags().StringVar(&orchestratorURL, "orchestrator",
		envOr("KTSU_ORCHESTRATOR_URL", "http://localhost:5050"),
		"orchestrator URL (env: KTSU_ORCHESTRATOR_URL)")
	return cmd
}

func validateCmd() *cobra.Command {
	var envPath, workflowDir, projectDir string
	var workspaces []string
	var noHubLock bool
	cmd := &cobra.Command{
		Use:   "validate [project-dir]",
		Short: "Validate Kimitsu configuration files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Positional arg overrides --project-dir flag (backward compat).
			if len(args) > 0 {
				projectDir = args[0]
			}

			var showGraph bool
			if g, err := cmd.Flags().GetBool("graph"); err == nil {
				showGraph = g
			}

			if envPath != "" {
				if _, err := config.LoadEnv(envPath); err != nil {
					return fmt.Errorf("invalid env config: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "env config OK: %s\n", envPath)
			}

			if workflowDir == "" {
				workflowDir = filepath.Join(projectDir, "workflows")
			}

			hasErrors := false
			var allErrs []string
			checkErrors := func(list []fileStatus) {
				for _, f := range list {
					if len(f.errors) > 0 {
						hasErrors = true
						allErrs = append(allErrs, fmt.Sprintf("%s: %v", f.path, f.errors))
					}
				}
			}

			if workflowDir != "" {
				results, err := validateWorkflows(cmd, workflowDir, projectDir)

				if showGraph {
					// Only output the Mermaid graphs
					for _, res := range results {
						fmt.Fprintf(cmd.OutOrStdout(), "%%%% Mermaid Graph for %s\n", res.File)
						fmt.Fprintln(cmd.OutOrStdout(), generateMermaidGraph(res, projectDir))
					}
				} else {
					// Collect all external refs and add project-wide configs
					summary := buildProjectSummary(projectDir, results)

					// Print grouped summary
					printProjectSummary(cmd.OutOrStdout(), projectDir, summary)

					checkErrors(summary.workflows)
					checkErrors(summary.agents)
					checkErrors(summary.servers)
					checkErrors(summary.systems)
				}

				if err != nil {
					return err
				}
			}

			// Auto-load workspaces from ktsuhub.lock.yaml unless suppressed.
			if !noHubLock {
				lockPath := filepath.Join(projectDir, "ktsuhub.lock.yaml")
				lock, lockErr := config.LoadHubLock(lockPath)
				if lockErr != nil {
					if !errors.Is(lockErr, os.ErrNotExist) {
						fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: ktsuhub.lock.yaml parse error: %v\n", lockErr)
					}
				} else {
					for _, entry := range lock.Entries {
						ws := entry.Cache
						if strings.HasPrefix(ws, "~/") {
							if home, err := os.UserHomeDir(); err == nil {
								ws = filepath.Join(home, ws[2:])
							}
						}
						workspaces = append(workspaces, ws)
					}
				}
			}

			// Validate additional workspaces. In graph mode, errors are not enforced
			// (matching the primary workspace's graph-mode behavior — output only).
			for _, ws := range workspaces {
				if strings.HasPrefix(ws, "~/") {
					if home, err := os.UserHomeDir(); err == nil {
						ws = filepath.Join(home, ws[2:])
					}
				}
				wsWorkflowDir := filepath.Join(ws, "workflows")
				if _, statErr := os.Stat(wsWorkflowDir); statErr != nil {
					continue
				}
				results, err := validateWorkflows(cmd, wsWorkflowDir, ws)
				if err != nil {
					return err
				}
				if !showGraph {
					summary := buildProjectSummary(ws, results)
					printProjectSummary(cmd.OutOrStdout(), ws, summary)
					checkErrors(summary.workflows)
					checkErrors(summary.agents)
					checkErrors(summary.servers)
					checkErrors(summary.systems)
				}
			}

			if hasErrors {
				return errors.New("validation failed")
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&envPath, "env", "", "path to environment config")
	cmd.Flags().StringVar(&workflowDir, "workflow-dir", "", "directory of *.workflow.yaml files to validate")
	cmd.Flags().Bool("graph", false, "output Mermaid graph of workflows")
	cmd.Flags().StringVar(&projectDir, "project-dir",
		envOr("KTSU_PROJECT_DIR", "."),
		"project root for resolving agent/server paths (env: KTSU_PROJECT_DIR)")
	cmd.Flags().StringArrayVar(&workspaces, "workspace", nil, "additional workspace root to validate (repeatable)")
	cmd.Flags().BoolVar(&noHubLock, "no-hub-lock", false, "ignore ktsuhub.lock.yaml even if present")
	return cmd
}

// ValidationResult holds the outcome of validating a single workflow file.
type ValidationResult struct {
	File         string
	Errors       []string
	Workflow     *config.WorkflowConfig
	ExternalRefs map[string]ExternalRef
}

type ExternalRef struct {
	Path   string
	Kind   string   // "agent", "server"
	Errors []string // Errors specifically for this file
	Deps   []string // paths of dependencies
}

// validateWorkflows loads every *.workflow.yaml in dir and runs DAG cycle detection and
// depends_on reference checks. Returns a slice of results and an error if validation fails.
func validateWorkflows(cmd *cobra.Command, dir, projectDir string) ([]ValidationResult, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.workflow.yaml"))
	if err != nil {
		return nil, fmt.Errorf("glob workflows: %w", err)
	}
	if len(files) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "no workflow files found in %s\n", dir)
		return nil, nil
	}

	var results []ValidationResult
	var allErrs []string
	for _, file := range files {
		wf, err := config.LoadWorkflow(file)
		if err != nil {
			errStr := fmt.Sprintf("%s: load error: %v", file, err)
			allErrs = append(allErrs, errStr)
			results = append(results, ValidationResult{File: file, Errors: []string{errStr}})
			continue
		}
		res := checkWorkflow(file, wf, projectDir)
		if len(res.Errors) != 0 {
			allErrs = append(allErrs, res.Errors...)
		}
		results = append(results, res)
	}

	if len(allErrs) > 0 {
		return results, errors.New(strings.Join(allErrs, "\n"))
	}
	return results, nil
}

// checkWorkflow validates a single workflow: depends_on references and DAG cycle detection.
func checkWorkflow(file string, wf *config.WorkflowConfig, projectDir string) ValidationResult {
	res := ValidationResult{
		File:         file,
		Workflow:     wf,
		ExternalRefs: make(map[string]ExternalRef),
	}

	// Build step ID set
	stepIDs := make(map[string]bool, len(wf.Pipeline))
	for _, step := range wf.Pipeline {
		stepIDs[step.ID] = true
	}

	// Check depends_on references
	for _, step := range wf.Pipeline {
		for _, dep := range step.DependsOn {
			if !stepIDs[dep] {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: step %q depends on unknown step %q", file, step.ID, dep))
			}
		}

		// Check external agent references
		if step.Agent != "" {
			agentPath := config.StripVersion(step.Agent)
			// Resolve relative to project root, matching orchestrator behavior.
			absPath := filepath.Join(projectDir, agentPath)
			validateExternalRef(&res, absPath, "agent", projectDir)
		}

		// Check sub-workflow references — resolve relative to the workflow file's directory.
		if step.Workflow != "" {
			wfDir := filepath.Dir(file)
			subWF, resolveErr := configbuiltins.ResolveWorkflowRef(step.Workflow, wfDir)
			if resolveErr != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: step %q: cannot resolve workflow ref %q: %v", file, step.ID, step.Workflow, resolveErr))
			} else {
				// Webhook conflict: parent step opts in but sub-workflow doesn't
				if step.WorkflowWebhooks == "execute" && subWF.Webhooks != "execute" {
					res.Errors = append(res.Errors, fmt.Sprintf(
						"%s: step %q: webhooks: execute declared on step but sub-workflow %q declares webhooks: %q (must also be \"execute\" for webhooks to fire)",
						file, step.ID, step.Workflow, subWF.Webhooks,
					))
				}

				// Missing required params
				declaredParams, parseErr := config.ParseParamsSchema(subWF.Params.Schema)
				if parseErr != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: step %q: sub-workflow params schema: %v", file, step.ID, parseErr))
				} else {
					for name, decl := range declaredParams {
						if decl.Default == nil {
							if _, provided := step.Params[name]; !provided {
								res.Errors = append(res.Errors, fmt.Sprintf(
									"%s: step %q: missing required param %q for sub-workflow %q",
									file, step.ID, name, step.Workflow,
								))
							}
						}
					}
				}

				// Env scoping for local (non-ktsu/) sub-workflows
				if !strings.HasPrefix(step.Workflow, "ktsu/") {
					wfPath := step.Workflow
					if !filepath.IsAbs(wfPath) {
						wfPath = filepath.Join(wfDir, wfPath)
					}
					checkEnvScoping(wfPath, "sub-workflow", &res.Errors)
				}
			}
		}
	}

	// Cross-workflow cycle detection — use the workflow file as the DFS root.
	if cycleMsg := checkWorkflowCycles(file, make(map[string]bool), make(map[string]bool)); cycleMsg != "" {
		res.Errors = append(res.Errors, cycleMsg)
	}

	// DAG cycle check
	nodes := make([]dag.Node, len(wf.Pipeline))
	for i, step := range wf.Pipeline {
		nodes[i] = dag.Node{ID: step.ID, Depends: step.DependsOn}
	}
	if _, err := dag.Resolve(nodes); err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", file, err))
	}

	return res
}

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

func useColor(out io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if f, ok := out.(*os.File); ok {
		stat, err := f.Stat()
		if err != nil {
			return false
		}
		return (stat.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// envRefLocations recursively finds all "env:*" string values in a YAML-decoded structure.
// Returns descriptive location strings like "auth: \"env:MY_VAR\"".
func envRefLocations(v interface{}, path string) []string {
	var found []string
	switch val := v.(type) {
	case string:
		if strings.HasPrefix(val, "env:") {
			if path != "" {
				found = append(found, fmt.Sprintf("%s: %q", path, val))
			} else {
				found = append(found, fmt.Sprintf("%q", val))
			}
		}
	case map[string]interface{}:
		for k, child := range val {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			found = append(found, envRefLocations(child, childPath)...)
		}
	case []interface{}:
		for i, child := range val {
			found = append(found, envRefLocations(child, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	return found
}

// checkWorkflowCycles performs DFS across workflow step references starting from rootRef.
// rootRef must be an absolute file path or a ktsu/ name.
// visited tracks refs we've fully processed; inStack tracks the current DFS path.
// Returns a non-empty error message if a cycle is detected.
func checkWorkflowCycles(rootRef string, visited, inStack map[string]bool) string {
	if inStack[rootRef] {
		return fmt.Sprintf("workflow cycle detected: %q is referenced transitively by itself", rootRef)
	}
	if visited[rootRef] {
		return ""
	}
	visited[rootRef] = true
	inStack[rootRef] = true
	defer func() { inStack[rootRef] = false }()

	// Resolve relative to the root ref's own directory.
	rootDir := filepath.Dir(rootRef)
	wf, err := configbuiltins.ResolveWorkflowRef(rootRef, rootDir)
	if err != nil {
		// Unresolvable refs are reported elsewhere; skip cycle check for them.
		return ""
	}
	for _, step := range wf.Pipeline {
		if step.Workflow == "" {
			continue
		}
		dep := step.Workflow
		if strings.HasPrefix(dep, "ktsu/") {
			continue // shipped workflows can't form cycles
		}
		if !filepath.IsAbs(dep) {
			dep = filepath.Join(rootDir, dep)
		}
		if msg := checkWorkflowCycles(dep, visited, inStack); msg != "" {
			return msg
		}
	}
	return ""
}

// checkEnvScoping reads filePath, scans for env: references, and appends errors to errs.
// fileKind is a human-readable label like "Agent" or "Server file".
func checkEnvScoping(filePath, fileKind string, errs *[]string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return // file not found is already reported elsewhere
	}
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return
	}
	for _, ref := range envRefLocations(raw, "") {
		*errs = append(*errs, fmt.Sprintf(
			"env: reference outside root workflow context in %s file %s — %s — use param: instead",
			fileKind, filePath, ref,
		))
	}
}

func validateExternalRef(res *ValidationResult, path, kind string, projectDir string) {
	if _, ok := res.ExternalRefs[path]; ok {
		return
	}

	ext := ExternalRef{Path: path, Kind: kind}
	switch kind {
	case "agent":
		agentCfg, err := config.LoadAgent(path)
		if err != nil {
			ext.Errors = append(ext.Errors, err.Error())
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", path, err))
		} else {
			checkEnvScoping(path, "agent", &res.Errors)

			// Check for output schema
			if agentCfg.Output == nil || len(agentCfg.Output.Schema) == 0 {
				ext.Errors = append(ext.Errors, "agent has no output schema defined")
				res.Errors = append(res.Errors, fmt.Sprintf("%s: agent has no output schema defined", path))
			}

			// Validate reflect field.
			if strings.TrimSpace(agentCfg.Prompt.Reflect) != "" {
				// Valid reflect — warn if max_turns is 1.
				if agentCfg.MaxTurns == 1 {
					fmt.Fprintf(os.Stderr, "WARNING: %s: reflect declared with max_turns: 1 — reflect will operate on single-turn output\n", path)
				}
			} else if agentCfg.Prompt.Reflect != "" {
				// Set but whitespace-only.
				ext.Errors = append(ext.Errors, "reflect prompt is empty or whitespace")
				res.Errors = append(res.Errors, fmt.Sprintf("%s: reflect prompt is empty or whitespace", path))
			}

			// Check for servers
			for _, srv := range agentCfg.Servers {
				if srv.Path != "" {
					serverPath := srv.Path
					if !filepath.IsAbs(srv.Path) {
						// Relative to agent file
						serverPath = filepath.Join(filepath.Dir(path), srv.Path)
					} else {
						// If absolute but doesn't exist, try project-relative
						if _, err := os.Stat(srv.Path); os.IsNotExist(err) {
							serverPath = filepath.Join(projectDir, srv.Path)
						}
					}
					ext.Deps = append(ext.Deps, serverPath)
					validateExternalRef(res, serverPath, "server", projectDir)
				}
			}
			// Check for sub-agents
			for _, sub := range agentCfg.SubAgents {
				subPath := config.StripVersion(sub)
				if !filepath.IsAbs(subPath) {
					subPath = filepath.Join(filepath.Dir(path), subPath)
				} else {
					if _, err := os.Stat(subPath); os.IsNotExist(err) {
						subPath = filepath.Join(projectDir, subPath)
					}
				}
				ext.Deps = append(ext.Deps, subPath)
				validateExternalRef(res, subPath, "agent", projectDir)
			}
		}
	case "server":
		_, err := config.LoadToolServer(path)
		if err != nil {
			// Try as manifest
			_, err2 := config.LoadServerManifest(path)
			if err2 != nil {
				ext.Errors = append(ext.Errors, err.Error())
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", path, err))
			} else {
				checkEnvScoping(path, "server", &res.Errors)
			}
		} else {
			checkEnvScoping(path, "server", &res.Errors)
		}
	}
	res.ExternalRefs[path] = ext
}

type fileStatus struct {
	path   string
	errors []string
}

type projectSummary struct {
	workflows []fileStatus
	agents    []fileStatus
	servers   []fileStatus
	systems   []fileStatus
}

func buildProjectSummary(projectDir string, workflowResults []ValidationResult) projectSummary {
	var s projectSummary
	uniqueAgents := make(map[string][]string)
	uniqueServers := make(map[string][]string)

	// dummy result for collecting orphan refs
	orphanRes := ValidationResult{ExternalRefs: make(map[string]ExternalRef)}

	// Scan for orphan agents
	if files, err := filepath.Glob(filepath.Join(projectDir, "agents", "*.agent.yaml")); err == nil {
		for _, f := range files {
			validateExternalRef(&orphanRes, f, "agent", projectDir)
		}
	}
	// Scan for orphan servers
	if files, err := filepath.Glob(filepath.Join(projectDir, "servers", "*.server.yaml")); err == nil {
		for _, f := range files {
			validateExternalRef(&orphanRes, f, "server", projectDir)
		}
	}

	for _, res := range workflowResults {
		s.workflows = append(s.workflows, fileStatus{path: res.File, errors: res.Errors})
		for path, ext := range res.ExternalRefs {
			if ext.Kind == "agent" {
				uniqueAgents[path] = ext.Errors
			} else if ext.Kind == "server" {
				uniqueServers[path] = ext.Errors
			}
		}
	}

	// Merge orphan refs
	for path, ext := range orphanRes.ExternalRefs {
		if ext.Kind == "agent" {
			uniqueAgents[path] = ext.Errors
		} else if ext.Kind == "server" {
			uniqueServers[path] = ext.Errors
		}
	}

	for path, errs := range uniqueAgents {
		s.agents = append(s.agents, fileStatus{path: path, errors: errs})
	}
	for path, errs := range uniqueServers {
		s.servers = append(s.servers, fileStatus{path: path, errors: errs})
	}

	// Explicitly check for project-wide configs
	checkSystemFile := func(name string, loadFn func(string) (any, error)) {
		path := filepath.Join(projectDir, name)
		if _, err := os.Stat(path); err == nil {
			var errs []string
			if _, err := loadFn(path); err != nil {
				errs = append(errs, err.Error())
			}
			s.systems = append(s.systems, fileStatus{path: path, errors: errs})
		}
	}

	checkSystemFile("gateway.yaml", func(p string) (any, error) { return config.LoadGateway(p) })
	checkSystemFile("servers.yaml", func(p string) (any, error) { return config.LoadServerManifest(p) })

	// Sort for consistent output
	sortStatus := func(list []fileStatus) {
		sort.Slice(list, func(i, j int) bool { return list[i].path < list[j].path })
	}
	sortStatus(s.workflows)
	sortStatus(s.agents)
	sortStatus(s.servers)
	sortStatus(s.systems)

	return s
}

func printProjectSummary(out io.Writer, projectDir string, s projectSummary) {
	useCol := useColor(out)
	colorize := func(color, text string) string {
		if !useCol {
			return text
		}
		return color + text + colorReset
	}

	fmt.Fprintf(out, "\n%s\n", colorize(colorBold+colorCyan, "Project Validation ("+projectDir+"):"))

	total, valid := 0, 0
	printGroup := func(name string, list []fileStatus) {
		if len(list) == 0 {
			return
		}
		fmt.Fprintf(out, "\n%s\n", colorize(colorBold, name+":"))
		for _, f := range list {
			total++
			rel, err := filepath.Rel(projectDir, f.path)
			if err != nil {
				rel = f.path
			}
			if len(f.errors) == 0 {
				fmt.Fprintf(out, "  %s %s\n", colorize(colorGreen, "OKAY"), rel)
				valid++
			} else {
				fmt.Fprintf(out, "  %s %s\n", colorize(colorRed, "FAIL"), rel)
				for _, e := range f.errors {
					fmt.Fprintf(out, "    - %s\n", colorize(colorYellow, e))
				}
			}
		}
	}

	printGroup("Workflows", s.workflows)
	printGroup("Agents", s.agents)
	printGroup("Servers", s.servers)
	printGroup("Systems", s.systems)

	summaryText := fmt.Sprintf("\nSummary: %d total, %d valid, %d invalid\n", total, valid, total-valid)
	fmt.Fprint(out, colorize(colorBold, summaryText))
}

func generateMermaidGraph(res ValidationResult, projectDir string) string {
	if res.Workflow == nil {
		return "%% (workflow failed to load, cannot generate graph)"
	}

	var sb strings.Builder
	sb.WriteString("graph TD\n")

	// Helper for safe Mermaid IDs
	safeID := func(p string) string {
		// Replace common path separators and dots with underscores
		id := strings.ReplaceAll(p, "/", "_")
		id = strings.ReplaceAll(id, "\\", "_")
		id = strings.ReplaceAll(id, ".", "_")
		id = strings.ReplaceAll(id, "-", "_")
		id = strings.ReplaceAll(id, ":", "_")
		return id
	}

	// Find steps with errors
	errorSteps := make(map[string]bool)
	for _, errStr := range res.Errors {
		for _, step := range res.Workflow.Pipeline {
			if strings.Contains(errStr, fmt.Sprintf("%q", step.ID)) || strings.Contains(errStr, "node "+step.ID) {
				errorSteps[step.ID] = true
			}
		}
	}

	// workflow steps
	for _, step := range res.Workflow.Pipeline {
		kind := "unknown"
		if step.Agent != "" {
			kind = "agent"
		} else if step.Transform != nil {
			kind = "transform"
		} else if step.Webhook != nil {
			kind = "webhook"
		}

		label := fmt.Sprintf("%s [%s]", step.ID, kind)
		if errorSteps[step.ID] {
			sb.WriteString(fmt.Sprintf("  %s[\"%s\"]:::failed\n", step.ID, label))
		} else {
			sb.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", step.ID, label))
		}
		for _, dep := range step.DependsOn {
			sb.WriteString(fmt.Sprintf("  %s --> %s\n", dep, step.ID))
		}

		if step.Agent != "" {
			agentPath := config.StripVersion(step.Agent)
			absPath := filepath.Join(projectDir, agentPath)
			sb.WriteString(fmt.Sprintf("  %s --> %s\n", step.ID, safeID(absPath)))
		}
	}

	// External files
	for path, ext := range res.ExternalRefs {
		id := safeID(path)
		// Display relative to workspace root if possible, or just the basename
		// For simplicity, let's use the path as provided in the config if it's relative.
		// Wait, we used absPath in validateExternalRef. Let's try to make it prettier.
		// Actually, let's just use the path as the user defined it if we can find it.
		// For now, let's use the full relative path from the current directory.
		cwd, _ := os.Getwd()
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			rel = path
		}

		label := fmt.Sprintf("%s [%s-file]", rel, ext.Kind)
		if len(ext.Errors) > 0 {
			sb.WriteString(fmt.Sprintf("  %s[\"%s\"]:::failed\n", id, label))
		} else {
			sb.WriteString(fmt.Sprintf("  %s[\"%s\"]:::file\n", id, label))
		}

		for _, dep := range ext.Deps {
			sb.WriteString(fmt.Sprintf("  %s --> %s\n", id, safeID(dep)))
		}
	}

	sb.WriteString("\n  classDef failed fill:#f96,stroke:#333,stroke-width:2px;\n")
	sb.WriteString("  classDef file fill:#e1f5fe,stroke:#01579b,stroke-width:2px;\n")
	return sb.String()
}

func lockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: "Generate ktsu.lock.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("lock: not implemented")
			return nil
		},
	}
}

func newCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Scaffold new Kimitsu resources",
	}
	cmd.AddCommand(newProjectCmd())
	return cmd
}

func newProjectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "project <name>",
		Short: "Bootstrap a new Kimitsu project scaffold",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if _, err := os.Stat(name); err == nil {
				return fmt.Errorf("project %q already exists", name)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("stat %s: %w", name, err)
			}

			type fileSpec struct {
				path    string
				tmplSrc string
			}

			workflowTmpl := `kind: workflow
name: {{.Name}}
version: "1.0.0"
description: ""

pipeline:
  - id: step1
    # Choose one: agent, transform, or webhook
    agent: agents/placeholder.agent.yaml
`
			agentTmpl := `name: placeholder
description: ""
model: default
system: ""
max_turns: 5
`
			envTmpl := `name: dev
variables: {}
providers: []
state:
  driver: sqlite
  dsn: ktsu.db
`
			gatewayTmpl := `providers: []
model_groups: []
`
			serversTmpl := `servers: []
`
			ktsuhubTmpl := `workflows: []
# Uncomment and fill in to publish this project to ktsuhub:
# workflows:
#   - name: {{.Name}}/my-workflow
#     version: "1.0.0"
#     description: ""
#     tags: []
#     entrypoint: workflows/{{.Name}}.workflow.yaml
`

			files := []fileSpec{
				{path: filepath.Join(name, "workflows", name+".workflow.yaml"), tmplSrc: workflowTmpl},
				{path: filepath.Join(name, "agents", "placeholder.agent.yaml"), tmplSrc: agentTmpl},
				{path: filepath.Join(name, "environments", "dev.env.yaml"), tmplSrc: envTmpl},
				{path: filepath.Join(name, "gateway.yaml"), tmplSrc: gatewayTmpl},
				{path: filepath.Join(name, "servers.yaml"), tmplSrc: serversTmpl},
				{path: filepath.Join(name, "ktsuhub.yaml"), tmplSrc: ktsuhubTmpl},
			}

			data := struct{ Name string }{Name: name}

			type parsedFile struct {
				path string
				tmpl *template.Template
			}
			parsed := make([]parsedFile, 0, len(files))
			for _, f := range files {
				tmpl, err := template.New(f.path).Parse(f.tmplSrc)
				if err != nil {
					return fmt.Errorf("parse template for %s: %w", f.path, err)
				}
				parsed = append(parsed, parsedFile{path: f.path, tmpl: tmpl})
			}

			for _, f := range parsed {
				var buf bytes.Buffer
				if err := f.tmpl.Execute(&buf, data); err != nil {
					return fmt.Errorf("render template for %s: %w", f.path, err)
				}
				if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
					os.RemoveAll(name)
					return fmt.Errorf("mkdir %s: %w", filepath.Dir(f.path), err)
				}
				if err := os.WriteFile(f.path, buf.Bytes(), 0o644); err != nil {
					os.RemoveAll(name)
					return fmt.Errorf("write %s: %w", f.path, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "created: %s\n", f.path)
			}
			return nil
		},
	}
}

func workflowGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Workflow utilities",
	}
	cmd.AddCommand(workflowTreeCmd())
	return cmd
}

// treeNode represents one item in the dependency tree.
type treeNode struct {
	Path     string     `json:"path"`
	Kind     string     `json:"kind"` // "workflow", "agent", "server"
	Children []treeNode `json:"children,omitempty"`
}

func workflowTreeCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "tree <workflow-file>",
		Short: "Print the full dependency tree of a workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wfFile := args[0]
			if !filepath.IsAbs(wfFile) {
				abs, err := filepath.Abs(wfFile)
				if err != nil {
					return fmt.Errorf("resolve path: %w", err)
				}
				wfFile = abs
			}
			tree, err := buildWorkflowTree(wfFile, make(map[string]bool))
			if err != nil {
				return err
			}
			if jsonOut {
				out, _ := json.MarshalIndent(tree, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			printTreeNode(cmd.OutOrStdout(), tree, "", true)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

// buildWorkflowTree builds a treeNode for the given workflow file (absolute path).
// seen prevents infinite recursion on shared sub-workflows (not cycles — those are boot-validated).
func buildWorkflowTree(absPath string, seen map[string]bool) (treeNode, error) {
	node := treeNode{Path: absPath, Kind: "workflow"}
	if seen[absPath] {
		return node, nil
	}
	seen[absPath] = true

	wf, err := config.LoadWorkflow(absPath)
	if err != nil {
		return node, fmt.Errorf("load %s: %w", absPath, err)
	}

	wfDir := filepath.Dir(absPath)
	for _, step := range wf.Pipeline {
		if step.Workflow != "" && !strings.HasPrefix(step.Workflow, "ktsu/") {
			dep := step.Workflow
			if !filepath.IsAbs(dep) {
				dep = filepath.Join(wfDir, dep)
			}
			child, err := buildWorkflowTree(dep, seen)
			if err == nil {
				node.Children = append(node.Children, child)
			} else {
				node.Children = append(node.Children, treeNode{Path: dep, Kind: "workflow"})
			}
		} else if step.Workflow != "" {
			// Shipped ktsu/ workflow
			node.Children = append(node.Children, treeNode{Path: step.Workflow, Kind: "workflow"})
		}

		if step.Agent != "" {
			agentPath := config.StripVersion(step.Agent)
			if !filepath.IsAbs(agentPath) {
				agentPath = filepath.Join(wfDir, agentPath)
			}
			agentNode := treeNode{Path: agentPath, Kind: "agent"}
			if !seen[agentPath] {
				seen[agentPath] = true
				agentCfg, err := config.LoadAgent(agentPath)
				if err == nil {
					for _, srv := range agentCfg.Servers {
						srvPath := srv.Path
						if !filepath.IsAbs(srvPath) {
							srvPath = filepath.Join(filepath.Dir(agentPath), srvPath)
						}
						agentNode.Children = append(agentNode.Children, treeNode{Path: srvPath, Kind: "server"})
					}
				}
			}
			node.Children = append(node.Children, agentNode)
		}
	}
	return node, nil
}

func printTreeNode(w io.Writer, node treeNode, prefix string, isLast bool) {
	connector := "├── "
	childPrefix := prefix + "│   "
	if isLast {
		connector = "└── "
		childPrefix = prefix + "    "
	}
	label := filepath.Base(node.Path)
	if strings.HasPrefix(node.Path, "ktsu/") {
		label = node.Path
	}
	fmt.Fprintf(w, "%s%s%s (%s)\n", prefix, connector, label, node.Kind)
	for i, child := range node.Children {
		printTreeNode(w, child, childPrefix, i == len(node.Children)-1)
	}
}

func printWorkflowTreeRoot(w io.Writer, node treeNode) {
	fmt.Fprintln(w, node.Path)
	for i, child := range node.Children {
		printTreeNode(w, child, "", i == len(node.Children)-1)
	}
}
