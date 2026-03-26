package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"

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
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/dag"
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
	root.AddCommand(invokeCmd())
	root.AddCommand(lockCmd())
	root.AddCommand(newCmd())
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
	var envPath, workflowDir, host string
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
				Host:        host,
				Port:        port,
			})
			log.Printf("starting %s", o)
			return o.Start(signalCtx())
		},
	}
	cmd.Flags().StringVar(&envPath, "env", "", "path to environment config (e.g. environments/dev.env.yaml)")
	cmd.Flags().StringVar(&workflowDir, "workflow-dir", "./workflows", "path to workflow directory")
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

func invokeCmd() *cobra.Command {
	var orchestratorURL, inputJSON string
	var wait bool
	cmd := &cobra.Command{
		Use:   "invoke <workflow>",
		Short: "Invoke a workflow on the orchestrator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflow := args[0]
			resp, err := http.Post(orchestratorURL+"/invoke/"+workflow, "application/json", strings.NewReader(inputJSON))
			if err != nil {
				return fmt.Errorf("invoke: %w", err)
			}
			defer resp.Body.Close()
			var result map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&result)
			runID, _ := result["run_id"].(string)
			fmt.Fprintf(cmd.OutOrStdout(), "run_id: %s\n", runID)
			if !wait || runID == "" {
				return nil
			}
			// Poll GET /runs/{run_id} until the run reaches a terminal status.
			for {
				time.Sleep(1 * time.Second)
				r, err := http.Get(orchestratorURL + "/runs/" + runID)
				if err != nil {
					return fmt.Errorf("poll: %w", err)
				}
				var status map[string]interface{}
				json.NewDecoder(r.Body).Decode(&status)
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
		envOr("KTSU_ORCHESTRATOR_URL", "http://localhost:8080"),
		"orchestrator URL (env: KTSU_ORCHESTRATOR_URL)")
	return cmd
}

func validateCmd() *cobra.Command {
	var envPath, workflowDir string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate Kimitsu configuration files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if envPath != "" {
				if _, err := config.LoadEnv(envPath); err != nil {
					return fmt.Errorf("invalid env config: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "env config OK: %s\n", envPath)
			}

			if workflowDir != "" {
				if err := validateWorkflows(cmd, workflowDir); err != nil {
					return err
				}
			}

			fmt.Fprintln(cmd.OutOrStdout(), "validation complete")
			return nil
		},
	}
	cmd.Flags().StringVar(&envPath, "env", "", "path to environment config")
	cmd.Flags().StringVar(&workflowDir, "workflow-dir", "", "directory of *.workflow.yaml files to validate")
	return cmd
}

// validateWorkflows loads every *.workflow.yaml in dir and runs DAG cycle detection and
// depends_on reference checks. Returns a combined error if any workflow is invalid.
func validateWorkflows(cmd *cobra.Command, dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.workflow.yaml"))
	if err != nil {
		return fmt.Errorf("glob workflows: %w", err)
	}
	if len(files) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "no workflow files found in %s\n", dir)
		return nil
	}

	var errs []string
	for _, file := range files {
		wf, err := config.LoadWorkflow(file)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: load error: %v", file, err))
			continue
		}
		fileErrs := checkWorkflow(file, wf)
		if len(fileErrs) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "OK: %s\n", file)
		}
		errs = append(errs, fileErrs...)
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

// checkWorkflow validates a single workflow: depends_on references and DAG cycle detection.
func checkWorkflow(file string, wf *config.WorkflowConfig) []string {
	var errs []string

	// Build step ID set
	stepIDs := make(map[string]bool, len(wf.Pipeline))
	for _, step := range wf.Pipeline {
		stepIDs[step.ID] = true
	}

	// Check depends_on references
	for _, step := range wf.Pipeline {
		for _, dep := range step.DependsOn {
			if !stepIDs[dep] {
				errs = append(errs, fmt.Sprintf("%s: step %q depends on unknown step %q", file, step.ID, dep))
			}
		}
	}

	// DAG cycle check
	nodes := make([]dag.Node, len(wf.Pipeline))
	for i, step := range wf.Pipeline {
		nodes[i] = dag.Node{ID: step.ID, Depends: step.DependsOn}
	}
	if _, err := dag.Resolve(nodes); err != nil {
		errs = append(errs, fmt.Sprintf("%s: %v", file, err))
	}

	return errs
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
  dsn: kimitsu.db
`
			gatewayTmpl := `providers: []
model_groups: []
`
			serversTmpl := `servers: []
`

			files := []fileSpec{
				{path: filepath.Join(name, "workflows", name+".workflow.yaml"), tmplSrc: workflowTmpl},
				{path: filepath.Join(name, "agents", "placeholder.agent.yaml"), tmplSrc: agentTmpl},
				{path: filepath.Join(name, "environments", "dev.env.yaml"), tmplSrc: envTmpl},
				{path: filepath.Join(name, "gateway.yaml"), tmplSrc: gatewayTmpl},
				{path: filepath.Join(name, "servers.yaml"), tmplSrc: serversTmpl},
			}

			data := struct{ Name string }{Name: name}

			for _, f := range files {
				tmpl, err := template.New("").Parse(f.tmplSrc)
				if err != nil {
					return fmt.Errorf("parse template for %s: %w", f.path, err)
				}
				var buf bytes.Buffer
				if err := tmpl.Execute(&buf, data); err != nil {
					return fmt.Errorf("render template for %s: %w", f.path, err)
				}
				if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
					return fmt.Errorf("mkdir %s: %w", filepath.Dir(f.path), err)
				}
				if err := os.WriteFile(f.path, buf.Bytes(), 0o644); err != nil {
					return fmt.Errorf("write %s: %w", f.path, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "created: %s\n", f.path)
			}
			return nil
		},
	}
}
