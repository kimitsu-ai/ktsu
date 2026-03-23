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
	body := map[string]interface{}{
		"model":      req.Model,
		"messages":   req.Messages,
		"max_tokens": req.MaxTokens,
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
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
	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return providers.InvokeResponse{}, p.parseError(resp.StatusCode, respBytes)
	}

	var oaiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
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

	return providers.InvokeResponse{
		Content:   oaiResp.Choices[0].Message.Content,
		TokensIn:  oaiResp.Usage.PromptTokens,
		TokensOut: oaiResp.Usage.CompletionTokens,
	}, nil
}

// parseError translates an upstream error response into a GatewayError.
func (p *Provider) parseError(statusCode int, body []byte) *providers.GatewayError {
	var errResp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(body, &errResp)

	code := errResp.Error.Code
	msg := errResp.Error.Message
	if msg == "" {
		msg = fmt.Sprintf("upstream returned status %d", statusCode)
	}

	// Billing hard limits are not retryable
	if code == "billing_hard_limit_reached" || code == "insufficient_quota" {
		return &providers.GatewayError{Type: "budget_exceeded", Message: msg, Retryable: false}
	}

	// 429 rate limit and 5xx are retryable
	retryable := statusCode == 429 || statusCode >= 500
	return &providers.GatewayError{
		Type:      "provider_error",
		Message:   fmt.Sprintf("upstream returned %d: %s", statusCode, msg),
		Retryable: retryable,
	}
}
