package providers

import "context"

// Message is a single turn in a conversation.
// For tool-use turns, ToolCalls carries the LLM's tool requests (role: assistant),
// and ToolCallID identifies the tool result (role: tool).
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolDefinition describes a tool the LLM can request to call.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ToolCall is a tool invocation requested by the LLM.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// InvokeRequest is the normalized request the dispatcher sends to a provider.
// Temperature is a pointer so nil means "use model group default" vs 0.0 explicitly.
type InvokeRequest struct {
	RunID       string           `json:"run_id"`
	StepID      string           `json:"step_id"`
	Group       string           `json:"group"`
	Model       string           `json:"model"` // resolved by dispatcher before calling provider
	Messages    []Message        `json:"messages"`
	MaxTokens   int              `json:"max_tokens"`
	Temperature *float64         `json:"temperature,omitempty"` // nil = use group default
	Tools       []ToolDefinition `json:"tools,omitempty"`
}

// InvokeResponse is the normalized response returned to the caller.
type InvokeResponse struct {
	Content       string     `json:"content"`
	ModelResolved string     `json:"model_resolved"`
	TokensIn      int        `json:"tokens_in"`
	TokensOut     int        `json:"tokens_out"`
	CostUSD       float64    `json:"cost_usd"`
	ToolCalls     []ToolCall `json:"tool_calls,omitempty"`
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
