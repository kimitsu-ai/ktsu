package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/runner"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
)

type server struct {
	o      *Orchestrator
	store  state.Store
	runner *runner.Runner
	mux    *http.ServeMux
}

func newServer(o *Orchestrator) *server {
	store := state.NewMemStore()
	r := runner.New(store, o.cfg.ProjectDir)
	s := &server{o: o, store: store, runner: r, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /invoke/{workflow}/{run_id}", s.handleInvoke)
	s.mux.HandleFunc("GET /envelope/{run_id}", s.handleGetEnvelope)
	s.mux.HandleFunc("POST /heartbeat", s.handleHeartbeat)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	workflow := r.PathValue("workflow")
	runID := r.PathValue("run_id")

	// Decode trigger body
	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body)

	// Build headers map
	headers := make(map[string]string)
	for k := range r.Header {
		headers[k] = r.Header.Get(k)
	}

	trigger := runner.TriggerContext{
		Type:      "webhook",
		Body:      body,
		Headers:   headers,
		RemoteIP:  r.RemoteAddr,
		RequestID: r.Header.Get("X-Request-ID"),
	}

	// Load workflow
	wfPath := filepath.Join(s.o.cfg.WorkflowDir, workflow+".workflow.yaml")
	wf, err := config.LoadWorkflow(wfPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("workflow not found: %s", workflow), http.StatusNotFound)
		return
	}

	// Run asynchronously
	go func() {
		ctx := context.Background()
		if err := s.runner.Execute(ctx, workflow, runID, wf, trigger); err != nil {
			log.Printf("run %s failed: %v", runID, err)
		}
	}()

	// Return 202
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"run_id": runID, "status": "accepted"})
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

func (s *server) serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		return err
	}
	log.Printf("orchestrator listening on :8080")
	srv := &http.Server{Handler: s.mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	return srv.Serve(ln)
}
