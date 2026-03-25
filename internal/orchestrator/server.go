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
	"path/filepath"
	"strconv"
	"sync"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/airlock"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/runner"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
)

// stepCallbackKey is used as a map key for pending step callbacks.
type stepCallbackKey struct{ runID, stepID string }

type server struct {
	o               *Orchestrator
	store           state.Store
	runner          *runner.Runner
	mux             *http.ServeMux
	pendingMu       sync.Mutex
	pendingCallbacks map[stepCallbackKey]chan agent.CallbackPayload
}

func newServer(o *Orchestrator) *server {
	store := state.NewMemStore()
	s := &server{
		o:               o,
		store:           store,
		mux:             http.NewServeMux(),
		pendingCallbacks: make(map[stepCallbackKey]chan agent.CallbackPayload),
	}

	var r *runner.Runner
	if o.cfg.RuntimeURL != "" {
		r = runner.NewWithDispatcher(store, &runtimeDispatcher{
			runtimeURL:      o.cfg.RuntimeURL,
			ownURL:          o.cfg.OwnURL,
			pendingMu:       &s.pendingMu,
			pendingCallbacks: s.pendingCallbacks,
		})
	} else {
		r = runner.New(store)
	}
	s.runner = r
	s.routes()
	return s
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /invoke/{workflow}", s.handleInvoke)
	s.mux.HandleFunc("GET /envelope/{run_id}", s.handleGetEnvelope)
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
		log.Printf("handleInvoke: non-JSON body (treating as empty): %v", err)
		input = map[string]interface{}{}
	}

	wfPath := filepath.Join(s.o.cfg.WorkflowDir, workflow+".workflow.yaml")
	wf, err := config.LoadWorkflow(wfPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("workflow not found: %s", workflow), http.StatusNotFound)
		return
	}

	runID := generateRunID()

	go func() {
		ctx := context.Background()
		if err := s.runner.Execute(ctx, workflow, runID, wf, input); err != nil {
			log.Printf("run %s failed: %v", runID, err)
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
	log.Printf("heartbeat: %v", payload)
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
		log.Printf("handleStepComplete: no pending waiter for %s/%s", runID, stepID)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *server) serve(ctx context.Context) error {
	port := s.o.cfg.Port
	if port == 0 {
		port = 8080
	}
	addr := net.JoinHostPort(s.o.cfg.Host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("orchestrator listening on %s", addr)
	srv := &http.Server{Handler: s.mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	return srv.Serve(ln)
}

// runtimeDispatcher implements runner.AgentDispatcher by POSTing to the runtime.
type runtimeDispatcher struct {
	runtimeURL      string
	ownURL          string
	pendingMu       *sync.Mutex
	pendingCallbacks map[stepCallbackKey]chan agent.CallbackPayload
}

func (d *runtimeDispatcher) Dispatch(ctx context.Context, runID, stepID string, input map[string]interface{}) (map[string]interface{}, error) {
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

	req := agent.InvokeRequest{
		RunID:       runID,
		StepID:      stepID,
		Input:       input,
		CallbackURL: callbackURL,
		MaxTurns:    10, // default; step-level override is a future concern
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal invoke request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.runtimeURL+"/invoke", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create runtime request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dispatch to runtime: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("runtime returned %d", resp.StatusCode)
	}

	// Wait for the callback.
	select {
	case payload := <-ch:
		if payload.Status == "failed" {
			return nil, fmt.Errorf("agent step failed: %s", payload.Error)
		}
		return payload.Output, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
