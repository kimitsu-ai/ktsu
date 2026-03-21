package gateway

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
)

type server struct {
	g   *Gateway
	mux *http.ServeMux
}

func newServer(g *Gateway) *server {
	s := &server{g: g, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /invoke", s.handleInvoke)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
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
		srv.Shutdown(context.Background())
	}()
	return srv.Serve(ln)
}
