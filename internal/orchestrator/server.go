package orchestrator

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
)

type server struct {
	o   *Orchestrator
	mux *http.ServeMux
}

func newServer(o *Orchestrator) *server {
	s := &server{o: o, mux: http.NewServeMux()}
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
	_ = workflow
	_ = runID
	// stub
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *server) handleGetEnvelope(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	_ = runID
	// stub
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (s *server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	// stub
	http.Error(w, "not implemented", http.StatusNotImplemented)
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
