package types

import "time"

// Envelope is the accumulated state object passed between pipeline steps
type Envelope struct {
	RunID    string                 `json:"run_id"`
	Workflow string                 `json:"workflow"`
	Status   string                 `json:"status,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Payload  map[string]interface{} `json:"payload,omitempty"`
	Steps    []StepEntry            `json:"steps"`
	Totals   RunTotals              `json:"totals"`
}

// StepEntry is a single step result with its ID, preserving pipeline order.
type StepEntry struct {
	ID string `json:"id"`
	StepOutput
}

type StepOutput struct {
	Output    map[string]interface{} `json:"output"`
	Metrics   StepMetrics            `json:"metrics"`
	Timestamp time.Time              `json:"timestamp"`
	Status    string                 `json:"status,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// InletContext is the input provided to the inlet step
type InletContext struct {
	Trigger string                 `json:"trigger"`
	Payload map[string]interface{} `json:"payload"`
}

// StepMetrics tracks resource usage for a step
type StepMetrics struct {
	DurationMS int64   `json:"duration_ms"`
	TokensIn   int     `json:"tokens_in"`
	TokensOut  int     `json:"tokens_out"`
	CostUSD    float64 `json:"cost_usd"`
	LLMCalls    int     `json:"llm_calls"`
	ToolCalls   int     `json:"tool_calls"`
	Reflected   bool    `json:"reflected,omitempty"`
	ReflectCalls int   `json:"reflect_calls,omitempty"`
}

// RunTotals aggregates metrics across all steps
type RunTotals struct {
	DurationMS int64   `json:"duration_ms"`
	TokensIn   int     `json:"tokens_in"`
	TokensOut  int     `json:"tokens_out"`
	CostUSD    float64 `json:"cost_usd"`
	LLMCalls   int     `json:"llm_calls"`
	ToolCalls  int     `json:"tool_calls"`
}
