package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

// Dispatchable is the interface the server uses for dispatch — allows test doubles.
type Dispatchable interface {
	Dispatch(ctx context.Context, req DispatchRequest) (providers.InvokeResponse, error)
}

type server struct {
	g          *Gateway
	dispatcher Dispatchable
	mux        *http.ServeMux
}

func newServer(g *Gateway, d Dispatchable) *server {
	s := &server{g: g, dispatcher: d, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /invoke", s.handleInvoke)
}

func (s *server) Handler() http.Handler { return s.mux }

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		log.Printf("handleHealth: encode failed: %v", err)
	}
}

func (s *server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	var req DispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON: "+err.Error(), false)
		return
	}

	resp, err := s.dispatcher.Dispatch(r.Context(), req)
	if err != nil {
		var gwErr *providers.GatewayError
		if errors.As(err, &gwErr) {
			status := gatewayErrorStatus(gwErr.Type)
			writeError(w, status, gwErr.Type, gwErr.Message, gwErr.Retryable)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error(), false)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("handleInvoke: encode failed: %v", err)
	}
}

func gatewayErrorStatus(errType string) int {
	switch errType {
	case "unknown_group":
		return http.StatusBadRequest // 400
	case "budget_exceeded":
		return http.StatusPaymentRequired // 402
	case "no_models_available":
		return http.StatusServiceUnavailable // 503
	default:
		return http.StatusBadGateway // 502 for provider_error
	}
}

func writeError(w http.ResponseWriter, status int, errType, message string, retryable bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"error":     errType,
		"message":   message,
		"retryable": retryable,
	}); err != nil {
		log.Printf("writeError: encode failed: %v", err)
	}
}

func (s *server) serve(ctx context.Context) error {
	port := s.g.cfg.Port
	if port == 0 {
		port = 8081
	}
	addr := net.JoinHostPort(s.g.cfg.Host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("gateway listening on %s", addr)
	srv := &http.Server{Handler: s.mux}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}()
	return srv.Serve(ln)
}
