package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

// Provider implements providers.Provider for the Anthropic native API.
type Provider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// New creates an Anthropic provider. baseURL defaults to "https://api.anthropic.com" if empty.
func New(baseURL, apiKey string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &Provider{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

func (p *Provider) Name() string { return "anthropic" }

func (p *Provider) Invoke(ctx context.Context, req providers.InvokeRequest) (providers.InvokeResponse, error) {
	// Anthropic requires system prompt as a top-level field, not in messages.
	var systemPrompt string
	antMsgs := make([]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "system" {
			if systemPrompt != "" {
				return providers.InvokeResponse{}, fmt.Errorf("anthropic provider: multiple system messages not supported")
			}
			systemPrompt = m.Content
			continue
		}
		antMsgs = append(antMsgs, p.toAntMessage(m))
	}

	body := map[string]any{
		"model":      req.Model,
		"messages":   antMsgs,
		"max_tokens": req.MaxTokens,
	}
	if systemPrompt != "" {
		body["system"] = systemPrompt
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if len(req.Tools) > 0 {
		body["tools"] = p.toAntTools(req.Tools)
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return providers.InvokeResponse{}, &providers.GatewayError{
			Type:      "provider_error",
			Message:   fmt.Sprintf("http request failed: %v", err),
			Retryable: true,
		}
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return providers.InvokeResponse{}, p.parseError(resp.StatusCode, respBytes)
	}

	var antResp struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBytes, &antResp); err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("decode response: %w", err)
	}

	result := providers.InvokeResponse{
		TokensIn:  antResp.Usage.InputTokens,
		TokensOut: antResp.Usage.OutputTokens,
	}

	for _, block := range antResp.Content {
		switch block.Type {
		case "text":
			result.Content = block.Text
		case "tool_use":
			result.ToolCalls = append(result.ToolCalls, providers.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: block.Input,
			})
		}
	}

	if result.Content == "" && len(result.ToolCalls) == 0 {
		return providers.InvokeResponse{}, fmt.Errorf("no text or tool_use content block in response")
	}

	return result, nil
}

// toAntMessage converts a normalized Message to the Anthropic wire format.
// For tool_use turns (assistant) and tool_result turns (user), structured content is used.
func (p *Provider) toAntMessage(m providers.Message) map[string]any {
	if m.Role == "tool" {
		// Tool result — Anthropic uses role "user" with tool_result content block.
		return map[string]any{
			"role": "user",
			"content": []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     m.Content,
				},
			},
		}
	}

	if len(m.ToolCalls) > 0 {
		// Assistant turn with tool use — encode as content array.
		content := []map[string]any{}
		if m.Content != "" {
			content = append(content, map[string]any{"type": "text", "text": m.Content})
		}
		for _, tc := range m.ToolCalls {
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Name,
				"input": tc.Arguments,
			})
		}
		return map[string]any{"role": m.Role, "content": content}
	}

	return map[string]any{"role": m.Role, "content": m.Content}
}

// toAntTools maps provider ToolDefinitions to the Anthropic tools format.
func (p *Provider) toAntTools(tools []providers.ToolDefinition) []map[string]any {
	out := make([]map[string]any, len(tools))
	for i, t := range tools {
		out[i] = map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.InputSchema,
		}
	}
	return out
}

func (p *Provider) parseError(statusCode int, body []byte) *providers.GatewayError {
	var errResp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(body, &errResp) //nolint:errcheck

	msg := errResp.Error.Message
	if msg == "" {
		msg = fmt.Sprintf("upstream returned status %d", statusCode)
	}

	if strings.Contains(strings.ToLower(msg), "credit balance") ||
		strings.Contains(strings.ToLower(msg), "credit limit") {
		return &providers.GatewayError{Type: "budget_exceeded", Message: msg, Retryable: false}
	}

	retryable := statusCode == 429 || statusCode == 529 || statusCode >= 500
	return &providers.GatewayError{
		Type:      "provider_error",
		Message:   fmt.Sprintf("upstream returned %d: %s", statusCode, msg),
		Retryable: retryable,
	}
}
