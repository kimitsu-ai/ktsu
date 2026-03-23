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
	var msgs []providers.Message
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemPrompt = m.Content
		} else {
			msgs = append(msgs, m)
		}
	}

	body := map[string]interface{}{
		"model":      req.Model,
		"messages":   msgs,
		"max_tokens": req.MaxTokens,
	}
	if systemPrompt != "" {
		body["system"] = systemPrompt
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
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
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBytes, &antResp); err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("decode response: %w", err)
	}

	var content string
	for _, block := range antResp.Content {
		if block.Type == "text" {
			content = block.Text
			break
		}
	}

	return providers.InvokeResponse{
		Content:   content,
		TokensIn:  antResp.Usage.InputTokens,
		TokensOut: antResp.Usage.OutputTokens,
	}, nil
}

func (p *Provider) parseError(statusCode int, body []byte) *providers.GatewayError {
	var errResp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	// Ignore parse failure; fallback msg path below handles non-JSON bodies.
	json.Unmarshal(body, &errResp) //nolint:errcheck

	msg := errResp.Error.Message
	if msg == "" {
		msg = fmt.Sprintf("upstream returned status %d", statusCode)
	}

	// Detect credit exhaustion by message content
	if strings.Contains(strings.ToLower(msg), "credit balance") ||
		strings.Contains(strings.ToLower(msg), "credit limit") {
		return &providers.GatewayError{Type: "budget_exceeded", Message: msg, Retryable: false}
	}

	// 429, 529, and 5xx are retryable
	retryable := statusCode == 429 || statusCode == 529 || statusCode >= 500
	return &providers.GatewayError{
		Type:      "provider_error",
		Message:   fmt.Sprintf("upstream returned %d: %s", statusCode, msg),
		Retryable: retryable,
	}
}
