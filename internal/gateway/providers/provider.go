package providers

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type InvokeRequest struct {
	RunID     string    `json:"run_id"`
	StepID    string    `json:"step_id"`
	Group     string    `json:"group"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}

type InvokeResponse struct {
	Content       string  `json:"content"`
	ModelResolved string  `json:"model_resolved"`
	TokensIn      int     `json:"tokens_in"`
	TokensOut     int     `json:"tokens_out"`
	CostUSD       float64 `json:"cost_usd"`
}

type Provider interface {
	Name() string
	Invoke(ctx context.Context, req InvokeRequest) (InvokeResponse, error)
}
