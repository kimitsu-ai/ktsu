package builtins

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

// ServeHTTP starts an HTTP server for the given BuiltinServer on the given address.
func ServeHTTP(addr string, b BuiltinServer) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "server": b.Name()})
	})

	mux.HandleFunc("POST /tools/list", func(w http.ResponseWriter, r *http.Request) {
		var req mcp.ListToolsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result := mcp.ListToolsResult{Tools: b.Tools()}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	mux.HandleFunc("POST /tools/call", func(w http.ResponseWriter, r *http.Request) {
		var req mcp.CallToolRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		output, err := b.Call(r.Context(), req.Name, req.Arguments)
		if err != nil {
			result := mcp.CallToolResult{
				IsError: true,
				Content: []mcp.ToolContent{{Type: "text", Text: err.Error()}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(result)
			return
		}
		result := mcp.CallToolResult{
			Content: []mcp.ToolContent{{Type: "text", Text: string(output)}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	log.Printf("builtin server %s listening on %s", b.Name(), addr)
	return http.ListenAndServe(addr, mux)
}

// StartBuiltin is a helper to start a builtin server and format the address
func StartBuiltin(b BuiltinServer, port int, orchestratorURL string) error {
	addr := fmt.Sprintf(":%d", port)
	if orchestratorURL != "" {
		log.Printf("builtin %s registered with orchestrator at %s", b.Name(), orchestratorURL)
	}
	return ServeHTTP(addr, b)
}
