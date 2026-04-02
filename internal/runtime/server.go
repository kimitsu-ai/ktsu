package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
)

type server struct {
	r                 *Runtime
	loop              *agent.Loop
	activeInvocations sync.Map // key: "run_id/step_id", value: struct{}
	mux               *http.ServeMux
	logger            *log.Logger
}

func (s *server) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

func newServer(r *Runtime, loop *agent.Loop) *server {
	s := &server{r: r, loop: loop, mux: http.NewServeMux(), logger: r.logger}
	s.routes()
	return s
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /invoke", s.handleInvoke)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		s.logf("handleHealth: encode failed: %v", err)
	}
}

func (s *server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	var req agent.InvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_request","message":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.RunID == "" || req.StepID == "" || req.CallbackURL == "" {
		http.Error(w, `{"error":"invalid_request","message":"run_id, step_id, and callback_url are required"}`, http.StatusBadRequest)
		return
	}

	key := req.RunID + "/" + req.StepID
	s.activeInvocations.Store(key, struct{}{})

	// Run the loop in a goroutine; deliver result via callback.
	go func() {
		payload := s.loop.Run(context.Background(), req)
		s.activeInvocations.Delete(key)
		if err := s.postCallback(req.CallbackURL, payload); err != nil {
			s.logf("runtime: callback failed for %s/%s: %v", req.RunID, req.StepID, err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"run_id":  req.RunID,
		"step_id": req.StepID,
		"status":  "accepted",
	}); err != nil {
		s.logf("handleInvoke: encode failed: %v", err)
	}
}

func (s *server) postCallback(callbackURL string, payload agent.CallbackPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := http.Post(callbackURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (s *server) serve(ctx context.Context) error {
	port := s.r.cfg.Port
	if port == 0 {
		port = 5051
	}
	addr := net.JoinHostPort(s.r.cfg.Host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("runtime failed to bind to %s: %w", addr, err)
	}
	s.logf("runtime listening on %s", addr)
	srv := &http.Server{Handler: s.mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	return srv.Serve(ln)
}
