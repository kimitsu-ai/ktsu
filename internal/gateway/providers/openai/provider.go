package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

// Provider implements providers.Provider for OpenAI-compatible APIs.
type Provider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// New creates an OpenAI-compatible provider.
// baseURL should be the root URL (e.g. "https://api.openai.com/v1").
func New(baseURL, apiKey string) *Provider {
	return &Provider{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

func (p *Provider) Name() string { return "openai" }

func (p *Provider) Invoke(ctx context.Context, req providers.InvokeRequest) (providers.InvokeResponse, error) {
	msgs := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, p.toOAIMessage(m))
	}

	body := map[string]any{
		"model":      req.Model,
		"messages":   msgs,
		"max_tokens": req.MaxTokens,
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if len(req.Tools) > 0 {
		body["tools"] = p.toOAITools(req.Tools)
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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

	var oaiResp struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"` // JSON-encoded string
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBytes, &oaiResp); err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if len(oaiResp.Choices) == 0 {
		return providers.InvokeResponse{}, fmt.Errorf("empty choices in response")
	}

	result := providers.InvokeResponse{
		Content:   oaiResp.Choices[0].Message.Content,
		TokensIn:  oaiResp.Usage.PromptTokens,
		TokensOut: oaiResp.Usage.CompletionTokens,
	}

	for _, tc := range oaiResp.Choices[0].Message.ToolCalls {
		var args map[string]any
		// OpenAI returns arguments as a JSON-encoded string; parse it.
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{"_raw": tc.Function.Arguments}
			}
		}
		result.ToolCalls = append(result.ToolCalls, providers.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	if result.Content == "" && len(result.ToolCalls) == 0 {
		return providers.InvokeResponse{}, fmt.Errorf("empty content and no tool calls in response")
	}

	return result, nil
}

// toOAIMessage converts a normalized Message to the OpenAI wire format.
func (p *Provider) toOAIMessage(m providers.Message) map[string]any {
	if m.Role == "tool" {
		return map[string]any{
			"role":         "tool",
			"content":      m.Content,
			"tool_call_id": m.ToolCallID,
		}
	}
	if len(m.ToolCalls) > 0 {
		oaiToolCalls := make([]map[string]any, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			oaiToolCalls[i] = map[string]any{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": string(argsJSON),
				},
			}
		}
		msg := map[string]any{
			"role":       m.Role,
			"tool_calls": oaiToolCalls,
		}
		if m.Content != "" {
			msg["content"] = m.Content
		}
		return msg
	}
	return map[string]any{"role": m.Role, "content": m.Content}
}

// toOAITools maps provider ToolDefinitions to the OpenAI function calling format.
func (p *Provider) toOAITools(tools []providers.ToolDefinition) []map[string]any {
	out := make([]map[string]any, len(tools))
	for i, t := range tools {
		out[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		}
	}
	return out
}

// parseError translates an upstream error response into a GatewayError.
func (p *Provider) parseError(statusCode int, body []byte) *providers.GatewayError {
	var errResp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(body, &errResp) //nolint:errcheck

	code := errResp.Error.Code
	msg := errResp.Error.Message
	if msg == "" {
		msg = fmt.Sprintf("upstream returned status %d", statusCode)
	}

	if code == "billing_hard_limit_reached" || code == "insufficient_quota" {
		return &providers.GatewayError{Type: "budget_exceeded", Message: msg, Retryable: false}
	}

	retryable := statusCode == 429 || statusCode >= 500
	return &providers.GatewayError{
		Type:      "provider_error",
		Message:   fmt.Sprintf("upstream returned %d: %s", statusCode, msg),
		Retryable: retryable,
	}
}
