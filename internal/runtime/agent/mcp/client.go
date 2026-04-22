package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
	http        *http.Client
	bridges     sync.Map     // map[string]string: baseURL -> bridgeURL
	connections sync.Map     // map[string]io.Closer: baseURL -> response body
	waiters     sync.Map     // map[int64]chan json.RawMessage: id -> response chan
	nextID      atomic.Int64 // counter for JSON-RPC IDs
}

// New returns a Client backed by the given HTTP client.
func New(httpClient *http.Client) *Client {
	return &Client{
		http: httpClient,
	}
}

// DiscoverTools calls tools/list on url and returns only tools whose name
// appears in allowlist. An empty allowlist returns an empty slice.
// authHeader and authValue, if non-empty, are sent as a single HTTP header.
func (c *Client) DiscoverTools(ctx context.Context, url, persistentID, authHeader, authValue string, allowlist []string) ([]ToolDefinition, error) {
	if len(allowlist) == 0 {
		return nil, nil
	}

	resp, err := c.rpc(ctx, url, persistentID, authHeader, authValue, "tools/list", nil)
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
		for _, pattern := range allowlist {
			matched, _ := path.Match(pattern, t.Name)
			if matched {
				pruned = append(pruned, t)
				break
			}
		}
	}
	return pruned, nil
}

// CallTool invokes the named tool on url with the given arguments.
// It does not enforce the allowlist — callers must check before calling.
// authHeader and authValue, if non-empty, are sent as a single HTTP header.
func (c *Client) CallTool(ctx context.Context, url, persistentID, authHeader, authValue, name string, arguments map[string]any) (ToolCallResult, error) {
	params := map[string]any{
		"name":      name,
		"arguments": arguments,
	}
	resp, err := c.rpc(ctx, url, persistentID, authHeader, authValue, "tools/call", params)
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
func (c *Client) Initialize(ctx context.Context, url, persistentID, authHeader, authValue string, config map[string]any) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "ktsu", "version": "1.0"},
		"config":          config,
	}
	_, err := c.rpc(ctx, url, persistentID, authHeader, authValue, "initialize", params)
	return err
}

// rpc sends a JSON-RPC 2.0 request and returns the raw result bytes.
// authHeader and authValue, if non-empty, are set as a single HTTP header.
func (c *Client) rpc(ctx context.Context, url, persistentID, authHeader, authValue, method string, params any) (json.RawMessage, error) {
	return c.rpcWithRetry(ctx, url, persistentID, authHeader, authValue, method, params, true)
}

func (c *Client) rpcWithRetry(ctx context.Context, url, persistentID, authHeader, authValue, method string, params any, canRetry bool) (json.RawMessage, error) {
	// Resolve the actual target URL (bridge) via SSE handshake if possible.
	targetURL := c.resolveBridge(ctx, url, persistentID)

	id := c.nextID.Add(1)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      id,
	}
	if params != nil {
		payload["params"] = params
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal rpc request: %w", err)
	}

	resChan := make(chan json.RawMessage, 1)
	c.waiters.Store(id, resChan)
	defer c.waiters.Delete(id)

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

	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		if canRetry {
			// Session might have expired or connection closed. Clear cache and retry once.
			c.bridges.Delete(url)
			if v, ok := c.connections.LoadAndDelete(url); ok {
				if closer, ok := v.(io.Closer); ok {
					closer.Close()
				}
			}
			return c.rpcWithRetry(ctx, url, persistentID, authHeader, authValue, method, params, false)
		}
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("rpc %s: server returned %d", method, resp.StatusCode)
	}

	var rawJSON []byte
	if resp.StatusCode == http.StatusAccepted {
		// Wait for the response to arrive via the established SSE stream.
		select {
		case rawJSON = <-resChan:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(30 * time.Second):
			return nil, fmt.Errorf("rpc %s: timeout waiting for async response", method)
		}
	} else {
		// Traditional request/response or SSE-wrapped response in the POST body.
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "text/event-stream") {
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 0, 512*1024), 4*1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "data:") {
					rawJSON = []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
					break
				}
			}
		} else {
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(resp.Body); err != nil {
				return nil, fmt.Errorf("rpc %s: read response: %w", method, err)
			}
			rawJSON = buf.Bytes()
		}

		// If the body was empty, it might still arrive via SSE even if not 202.
		if len(bytes.TrimSpace(rawJSON)) == 0 {
			select {
			case rawJSON = <-resChan:
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(30 * time.Second):
				return nil, fmt.Errorf("rpc %s: empty body and timeout waiting for async response", method)
			}
		}
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
func (c *Client) resolveBridge(ctx context.Context, baseURL, persistentID string) string {
	if v, ok := c.bridges.Load(baseURL); ok {
		return v.(string)
	}

	sseURL := strings.TrimSuffix(baseURL, "/") + "/sse"
	if persistentID != "" {
		u, err := url.Parse(sseURL)
		if err == nil {
			q := u.Query()
			q.Set("persistentId", persistentID)
			u.RawQuery = q.Encode()
			sseURL = u.String()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL, nil)
	if err != nil {
		return baseURL
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return baseURL
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return baseURL
	}

	bridgeChan := make(chan string, 1)
	c.connections.Store(baseURL, resp.Body)

	// Unified dispatcher: handles both bridge discovery and async response dispatching.
	go func(body io.ReadCloser) {
		defer body.Close()
		s := bufio.NewScanner(body)
		s.Buffer(make([]byte, 0, 512*1024), 4*1024*1024)
		var bridgeURL string
		for s.Scan() {
			line := s.Text()

			// 1. Handle Bridge Discovery (event: endpoint)
			if strings.HasPrefix(line, "event: endpoint") {
				if s.Scan() {
					dataLine := s.Text()
					if strings.HasPrefix(dataLine, "data:") {
						bridgeURL = strings.TrimSpace(strings.TrimPrefix(dataLine, "data:"))
						// Resolve relative bridge URL against baseURL
						if !strings.HasPrefix(bridgeURL, "http") {
							base, _ := url.Parse(baseURL)
							rel, _ := url.Parse(bridgeURL)
							if base != nil && rel != nil {
								bridgeURL = base.ResolveReference(rel).String()
							}
						}
						// Propagate persistentID to bridge URL
						if persistentID != "" {
							u, err := url.Parse(bridgeURL)
							if err == nil {
								q := u.Query()
								q.Set("persistentId", persistentID)
								u.RawQuery = q.Encode()
								bridgeURL = u.String()
							}
						}
						// Signal resolveBridge that we found the endpoint
						select {
						case bridgeChan <- bridgeURL:
						default:
						}
					}
				}
				continue
			}

			// 2. Handle Asynchronous Response Dispatching
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data == "" {
					continue
				}
				// Also check if this data line is actually the bridge URL (some servers omit event: endpoint)
				if bridgeURL == "" && strings.HasPrefix(data, "http") {
					bridgeURL = data
					select {
					case bridgeChan <- bridgeURL:
					default:
					}
					continue
				}

				var msg struct {
					ID int64 `json:"id"`
				}
				if err := json.Unmarshal([]byte(data), &msg); err == nil && msg.ID != 0 {
					if v, ok := c.waiters.Load(msg.ID); ok {
						if ch, ok := v.(chan json.RawMessage); ok {
							select {
							case ch <- json.RawMessage(data):
							default:
							}
						}
					}
				}
			}
		}
	}(resp.Body)

	// Wait for the bridge discovery to complete.
	select {
	case bridgeURL := <-bridgeChan:
		c.bridges.Store(baseURL, bridgeURL)
		return bridgeURL
	case <-time.After(10 * time.Second):
		// Fallback to baseURL if discovery fails
		return baseURL
	}
}

// Close terminates all active SSE connections.
func (c *Client) Close() error {
	c.connections.Range(func(key, value any) bool {
		if closer, ok := value.(io.Closer); ok {
			closer.Close()
		}
		return true
	})
	return nil
}
