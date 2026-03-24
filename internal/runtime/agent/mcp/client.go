package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	http *http.Client
}

// New returns a Client backed by the given HTTP client.
func New(httpClient *http.Client) *Client {
	return &Client{http: httpClient}
}

// DiscoverTools calls tools/list on url and returns only tools whose name
// appears in allowlist. An empty allowlist returns an empty slice.
func (c *Client) DiscoverTools(ctx context.Context, url string, allowlist []string) ([]ToolDefinition, error) {
	if len(allowlist) == 0 {
		return nil, nil
	}

	permitted := make(map[string]struct{}, len(allowlist))
	for _, name := range allowlist {
		permitted[name] = struct{}{}
	}

	resp, err := c.rpc(ctx, url, "tools/list", nil)
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
func (c *Client) CallTool(ctx context.Context, url, name string, arguments map[string]any) (ToolCallResult, error) {
	params := map[string]any{
		"name":      name,
		"arguments": arguments,
	}
	resp, err := c.rpc(ctx, url, "tools/call", params)
	if err != nil {
		return ToolCallResult{}, err
	}

	var result ToolCallResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return ToolCallResult{}, fmt.Errorf("parse tools/call response: %w", err)
	}
	return result, nil
}

// rpc sends a JSON-RPC 2.0 request and returns the raw result bytes.
func (c *Client) rpc(ctx context.Context, url, method string, params any) (json.RawMessage, error) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc %s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("rpc %s: server returned %d", method, resp.StatusCode)
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode rpc response: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("rpc %s error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
	}
	return envelope.Result, nil
}
