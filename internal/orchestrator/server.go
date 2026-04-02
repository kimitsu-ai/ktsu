package orchestrator

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/airlock"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/runner"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
	"github.com/kimitsu-ai/ktsu/pkg/types"
)

// defaultProjectDir returns "." when projectDir is empty.
func defaultProjectDir(d string) string {
	if d == "" {
		return "."
	}
	return d
}

// stepCallbackKey is used as a map key for pending step callbacks.
type stepCallbackKey struct{ runID, stepID string }

type server struct {
	o                *Orchestrator
	store            state.Store
	runner           *runner.Runner
	mux              *http.ServeMux
	pendingMu        sync.Mutex
	pendingCallbacks map[stepCallbackKey]chan agent.CallbackPayload
	logger           *log.Logger
}

func (s *server) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

func newServer(o *Orchestrator) *server {
	store := state.NewMemStore()
	s := &server{
		o:                o,
		store:            store,
		mux:              http.NewServeMux(),
		pendingCallbacks: make(map[stepCallbackKey]chan agent.CallbackPayload),
		logger:           o.logger,
	}

	var r *runner.Runner
	if o.cfg.RuntimeURL != "" {
		r = runner.NewWithDispatcher(store, &runtimeDispatcher{
			runtimeURL:       o.cfg.RuntimeURL,
			ownURL:           o.cfg.OwnURL,
			projectDir:       defaultProjectDir(o.cfg.ProjectDir),
			pendingMu:        &s.pendingMu,
			pendingCallbacks: s.pendingCallbacks,
		})
	} else {
		r = runner.New(store)
	}
	s.runner = r
	s.routes()
	return s
}

func (s *server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.o.cfg.APIKey == "" {
			h(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+s.o.cfg.APIKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "unauthorized",
				"message": "Set Authorization: Bearer <key> header. See KTSU_API_KEY.",
			})
			return
		}
		h(w, r)
	}
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /invoke/{workflow}", s.requireAuth(s.handleInvoke))
	s.mux.HandleFunc("GET /runs/{run_id}", s.requireAuth(s.handleGetRun))
	s.mux.HandleFunc("GET /envelope/{run_id}", s.requireAuth(s.handleGetEnvelope))
	s.mux.HandleFunc("POST /heartbeat", s.handleHeartbeat)
	s.mux.HandleFunc("POST /runs/{run_id}/steps/{step_id}/complete", s.handleStepComplete)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	workflow := r.PathValue("workflow")

	input := map[string]interface{}{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.logf("handleInvoke: non-JSON body (treating as empty): %v", err)
		input = map[string]interface{}{}
	}

	wfPath := filepath.Join(s.o.cfg.WorkflowDir, workflow+".workflow.yaml")
	wf, err := config.LoadWorkflow(wfPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("workflow not found: %s", workflow), http.StatusNotFound)
		return
	}

	if err := airlock.ValidateInput(input, wf.Input.Schema); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	runID := generateRunID()

	go func() {
		ctx := context.Background()
		if err := s.runner.Execute(ctx, workflow, runID, wf, input); err != nil {
			s.logf("run %s failed: %v", runID, err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"run_id": runID, "status": "accepted"})
}

// generateRunID creates a random run identifier.
func generateRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "run_" + hex.EncodeToString(b)
}

func (s *server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	run, err := s.store.GetRun(r.Context(), runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	steps, _ := s.store.ListSteps(r.Context(), runID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"run":   run,
		"steps": steps,
	})
}

func (s *server) handleGetEnvelope(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	env, err := s.store.GetEnvelope(r.Context(), runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(env)
}

func (s *server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	s.logf("heartbeat: %v", payload)
	w.WriteHeader(http.StatusOK)
}

func (s *server) handleStepComplete(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	stepID := r.PathValue("step_id")

	var payload agent.CallbackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Process reserved fields if the step succeeded.
	if payload.Status == "ok" && payload.Output != nil {
		clean, reserved, skipReason, err := runner.ProcessReservedFields(payload.Output, 0)
		if err != nil {
			payload.Status = "failed"
			payload.Error = err.Error()
			payload.Output = nil
		} else if skipReason != "" {
			payload.Status = "skipped"
			payload.Output = clean
		} else {
			if err := airlock.Validate(clean, nil, reserved); err != nil {
				payload.Status = "failed"
				payload.Error = "air-lock: " + err.Error()
				payload.Output = nil
			} else {
				payload.Output = clean
			}
		}
	}

	// Signal the waiting runner goroutine.
	key := stepCallbackKey{runID, stepID}
	s.pendingMu.Lock()
	ch, ok := s.pendingCallbacks[key]
	s.pendingMu.Unlock()

	if ok {
		ch <- payload
	} else {
		s.logf("handleStepComplete: no pending waiter for %s/%s", runID, stepID)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *server) serve(ctx context.Context) error {
	port := s.o.cfg.Port
	if port == 0 {
		port = 5050
	}
	addr := net.JoinHostPort(s.o.cfg.Host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.logf("orchestrator listening on %s", addr)
	srv := &http.Server{Handler: s.mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	return srv.Serve(ln)
}

// runtimeDispatcher implements runner.AgentDispatcher by POSTing to the runtime.
type runtimeDispatcher struct {
	runtimeURL       string
	ownURL           string
	projectDir       string
	pendingMu        *sync.Mutex
	pendingCallbacks map[stepCallbackKey]chan agent.CallbackPayload
}

func (d *runtimeDispatcher) Dispatch(ctx context.Context, runID, stepID string, step *config.PipelineStep, input map[string]interface{}) (map[string]interface{}, types.StepMetrics, error) {
	callbackURL := d.ownURL + "/runs/" + runID + "/steps/" + stepID + "/complete"

	// Register a channel before sending to avoid a race with a fast callback.
	ch := make(chan agent.CallbackPayload, 1)
	key := stepCallbackKey{runID, stepID}
	d.pendingMu.Lock()
	d.pendingCallbacks[key] = ch
	d.pendingMu.Unlock()
	defer func() {
		d.pendingMu.Lock()
		delete(d.pendingCallbacks, key)
		d.pendingMu.Unlock()
	}()

	// Load agent config and resolve tool servers when an agent path is provided.
	var (
		agentName    string
		system       string
		maxTurns     = 10
		modelGroup   string
		maxTokens    = 1024
		toolServers  []agent.ToolServerSpec
		outputSchema map[string]any
	)

	var zero types.StepMetrics
	if step != nil && step.Agent != "" {
		agentPath := filepath.Join(d.projectDir, config.StripVersion(step.Agent))
		agentCfg, err := config.LoadAgent(agentPath)
		if err != nil {
			return nil, zero, fmt.Errorf("load agent %s: %w", step.Agent, err)
		}
		agentName = agentCfg.Name
		system = agentCfg.System
		modelGroup = agentCfg.Model
		if agentCfg.MaxTurns > 0 {
			maxTurns = agentCfg.MaxTurns
		}
		if agentCfg.Output == nil || len(agentCfg.Output.Schema) == 0 {
			return nil, zero, fmt.Errorf("agent %s has no output schema defined", agentCfg.Name)
		}
		outputSchema = agentCfg.Output.Schema
		for _, srv := range agentCfg.Servers {
			serverPath := srv.Path
			if srv.Path != "" && !filepath.IsAbs(srv.Path) {
				// Relative to agent file
				serverPath = filepath.Join(filepath.Dir(agentPath), srv.Path)
			} else if srv.Path != "" && filepath.IsAbs(srv.Path) {
				// If it's absolute but doesn't exist, try project-relative.
				// This handles "/servers/foo.yaml" in a container where project root is /.
				if _, err := os.Stat(srv.Path); os.IsNotExist(err) {
					serverPath = filepath.Join(d.projectDir, srv.Path)
				}
			}

			serverCfg, err := config.LoadToolServer(serverPath)
			if err != nil {
				return nil, zero, fmt.Errorf("load server %s: %w", srv.Path, err)
			}
			authToken := serverCfg.Auth
			if strings.HasPrefix(authToken, "env:") {
				authToken = os.Getenv(strings.TrimPrefix(authToken, "env:"))
			}
			toolServers = append(toolServers, agent.ToolServerSpec{
				Name:      serverCfg.Name,
				URL:       serverCfg.URL,
				Allowlist: srv.Access.Allowlist,
				AuthToken: authToken,
			})
		}
	}

	// Step-level model overrides agent default.
	if step != nil && step.Model != nil {
		if step.Model.Group != "" {
			modelGroup = step.Model.Group
		}
		if step.Model.MaxTokens > 0 {
			maxTokens = step.Model.MaxTokens
		}
	}

	req := agent.InvokeRequest{
		RunID:        runID,
		StepID:       stepID,
		AgentName:    agentName,
		System:       system,
		MaxTurns:     maxTurns,
		Model:        agent.ModelSpec{Group: modelGroup, MaxTokens: maxTokens},
		ToolServers:  toolServers,
		Input:        input,
		CallbackURL:  callbackURL,
		OutputSchema: outputSchema,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, zero, fmt.Errorf("marshal invoke request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.runtimeURL+"/invoke", bytes.NewReader(body))
	if err != nil {
		return nil, zero, fmt.Errorf("create runtime request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, zero, fmt.Errorf("dispatch to runtime: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return nil, zero, fmt.Errorf("runtime returned %d", resp.StatusCode)
	}

	// Wait for the callback.
	select {
	case payload := <-ch:
		m := payload.Metrics
		metrics := types.StepMetrics{
			DurationMS: m.DurationMS,
			TokensIn:   m.TokensIn,
			TokensOut:  m.TokensOut,
			CostUSD:    m.CostUSD,
			LLMCalls:   m.LLMCalls,
			ToolCalls:  m.ToolCalls,
		}
		if payload.Status == "failed" {
			return nil, metrics, fmt.Errorf("agent step failed: %s", payload.Error)
		}
		return payload.Output, metrics, nil
	case <-ctx.Done():
		return nil, zero, ctx.Err()
	}
}
