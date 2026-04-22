package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// ToolDefinition is a single tool exposed by an MCP server.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ContentBlock is one item in a tool call result's content array.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolCallResult is the response from a tools/call invocation.
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
}

// Client makes JSON-RPC 2.0 calls to MCP tool servers over HTTP.
type Client struct {
	http    *http.Client
	bridges sync.Map // map[string]string: baseURL -> bridgeURL
}

// New returns a Client backed by the given HTTP client.
func New(httpClient *http.Client) *Client {
	return &Client{http: httpClient}
}

// DiscoverTools calls tools/list on url and returns only tools whose name
// appears in allowlist. An empty allowlist returns an empty slice.
// authHeader and authValue, if non-empty, are sent as a single HTTP header.
func (c *Client) DiscoverTools(ctx context.Context, url, authHeader, authValue string, allowlist []string) ([]ToolDefinition, error) {
	if len(allowlist) == 0 {
		return nil, nil
	}

	permitted := make(map[string]struct{}, len(allowlist))
	for _, name := range allowlist {
		permitted[name] = struct{}{}
	}

	resp, err := c.rpc(ctx, url, authHeader, authValue, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Tools []ToolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list response: %w", err)
	}

	var pruned []ToolDefinition
	for _, t := range result.Tools {
		if _, ok := permitted[t.Name]; ok {
			pruned = append(pruned, t)
		}
	}
	return pruned, nil
}

// CallTool invokes the named tool on url with the given arguments.
// It does not enforce the allowlist — callers must check before calling.
// authHeader and authValue, if non-empty, are sent as a single HTTP header.
func (c *Client) CallTool(ctx context.Context, url, authHeader, authValue, name string, arguments map[string]any) (ToolCallResult, error) {
	params := map[string]any{
		"name":      name,
		"arguments": arguments,
	}
	resp, err := c.rpc(ctx, url, authHeader, authValue, "tools/call", params)
	if err != nil {
		return ToolCallResult{}, err
	}

	var result ToolCallResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return ToolCallResult{}, fmt.Errorf("parse tools/call response: %w", err)
	}
	return result, nil
}

// Initialize sends an MCP initialize request with optional config params.
// Use this before DiscoverTools when the server requires per-connection configuration.
// config is sent under the "config" key in the initialize params.
// authHeader and authValue, if non-empty, are sent as a single HTTP header.
func (c *Client) Initialize(ctx context.Context, url, authHeader, authValue string, config map[string]any) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "ktsu", "version": "1.0"},
		"config":          config,
	}
	_, err := c.rpc(ctx, url, authHeader, authValue, "initialize", params)
	return err
}

// rpc sends a JSON-RPC 2.0 request and returns the raw result bytes.
// authHeader and authValue, if non-empty, are set as a single HTTP header.
func (c *Client) rpc(ctx context.Context, url, authHeader, authValue, method string, params any) (json.RawMessage, error) {
	// Resolve the actual target URL (bridge) via SSE handshake if possible.
	targetURL := c.resolveBridge(ctx, url)

	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      1,
	}
	if params != nil {
		payload["params"] = params
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal rpc request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2024-11-05")
	if authHeader != "" {
		req.Header.Set(authHeader, authValue)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc %s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("rpc %s: server returned %d", method, resp.StatusCode)
	}

	// Some MCP servers (e.g. GitHub's) respond with SSE (text/event-stream) even
	// for simple request/response calls.  Extract the first "data:" line.
	var rawJSON []byte
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 512*1024), 4*1024*1024) // up to 4 MiB per line
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				rawJSON = []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				break
			}
		}
		if len(rawJSON) == 0 {
			return nil, fmt.Errorf("rpc %s: no data line in SSE response", method)
		}
	} else {
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(resp.Body); err != nil {
			return nil, fmt.Errorf("rpc %s: read response: %w", method, err)
		}
		rawJSON = buf.Bytes()
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rawJSON, &envelope); err != nil {
		return nil, fmt.Errorf("decode rpc response: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("rpc %s error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
	}
	return envelope.Result, nil
}

// resolveBridge attempts to discover the session bridge URL by hitting {url}/sse.
// Returns the bridge URL on success, or the original baseURL as a fallback.
func (c *Client) resolveBridge(ctx context.Context, baseURL string) string {
	if v, ok := c.bridges.Load(baseURL); ok {
		return v.(string)
	}

	// Discovery logic — use a per-URL lock to avoid concurrent handshakes for the same server.
	// For simplicity in this implementation, we allow concurrent handshakes but the last
	// one to finish will populate the cache. Using sync.Map.LoadOrStore would be cleaner
	// if we had a dedicated handshake object.

	sseURL := strings.TrimSuffix(baseURL, "/") + "/sse"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL, nil)
	if err != nil {
		return baseURL
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return baseURL
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return baseURL
	}

	// Scan SSE stream for the "endpoint" event.
	// Format:
	// event: endpoint
	// data: http://...
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: endpoint") {
			if scanner.Scan() {
				dataLine := scanner.Text()
				if strings.HasPrefix(dataLine, "data:") {
					bridgeURL := strings.TrimSpace(strings.TrimPrefix(dataLine, "data:"))
					if bridgeURL != "" {
						c.bridges.Store(baseURL, bridgeURL)
						return bridgeURL
					}
				}
			}
		}
		// Some servers might just send data: ... without the event: endpoint line.
		if strings.HasPrefix(line, "data:") {
			potentialURL := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if strings.HasPrefix(potentialURL, "http") {
				c.bridges.Store(baseURL, potentialURL)
				return potentialURL
			}
		}
	}

	return baseURL
}
