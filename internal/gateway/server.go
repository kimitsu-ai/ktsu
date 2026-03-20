package gateway

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
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
	ln, err := net.Listen("tcp", ":8081")
	if err != nil {
		return err
	}
	log.Printf("gateway listening on :8081")
	srv := &http.Server{Handler: s.mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	return srv.Serve(ln)
}
