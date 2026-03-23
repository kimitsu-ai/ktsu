package providers

import "context"

// Message is a single turn in a conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// InvokeRequest is the normalized request the dispatcher sends to a provider.
// Temperature is a pointer so nil means "use model group default" vs 0.0 explicitly.
type InvokeRequest struct {
	RunID       string    `json:"run_id"`
	StepID      string    `json:"step_id"`
	Group       string    `json:"group"`
	Model       string    `json:"model"` // resolved by dispatcher before calling provider
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature *float64  `json:"temperature,omitempty"` // nil = use group default
}

// InvokeResponse is the normalized response returned to the caller.
type InvokeResponse struct {
	Content       string  `json:"content"`
	ModelResolved string  `json:"model_resolved"`
	TokensIn      int     `json:"tokens_in"`
	TokensOut     int     `json:"tokens_out"`
	CostUSD       float64 `json:"cost_usd"`
}

// GatewayError is a typed error returned by providers and the dispatcher.
// The HTTP handler maps Type to the correct HTTP status code.
type GatewayError struct {
	Type      string `json:"type"`      // "provider_error", "budget_exceeded", "no_models_available", "unknown_group"
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (e *GatewayError) Error() string { return e.Message }

// Provider is the interface all LLM provider adapters must implement.
type Provider interface {
	Name() string
	Invoke(ctx context.Context, req InvokeRequest) (InvokeResponse, error)
}
