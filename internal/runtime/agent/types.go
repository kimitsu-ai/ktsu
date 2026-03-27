package agent

// InvokeRequest is the payload the orchestrator sends to POST /invoke.
type InvokeRequest struct {
	RunID        string           `json:"run_id"`
	StepID       string           `json:"step_id"`
	AgentName    string           `json:"agent_name"`
	System       string           `json:"system"`
	MaxTurns     int              `json:"max_turns"`
	Model        ModelSpec        `json:"model"`
	Input        map[string]any   `json:"input"`
	ToolServers  []ToolServerSpec `json:"tool_servers"`
	CallbackURL  string           `json:"callback_url"`
	OutputSchema map[string]any   `json:"output_schema,omitempty"`
}

// ModelSpec identifies which LLM model group to use.
type ModelSpec struct {
	Group     string `json:"group"`
	MaxTokens int    `json:"max_tokens"`
}

// ToolServerSpec declares a tool server the agent may call.
type ToolServerSpec struct {
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Allowlist []string `json:"allowlist"`
	AuthToken string   `json:"auth_token,omitempty"` // resolved bearer token
}

// CallbackPayload is the result POSTed to callback_url when the loop finishes.
type CallbackPayload struct {
	RunID   string         `json:"run_id"`
	StepID  string         `json:"step_id"`
	Status  string         `json:"status"` // "ok" | "failed"
	Output  map[string]any `json:"output"`
	Error   string         `json:"error"`
	Metrics Metrics        `json:"metrics"`
}

// Metrics holds accumulated execution statistics for a completed invocation.
type Metrics struct {
	ModelResolved string  `json:"model_resolved"`
	TokensIn      int     `json:"tokens_in"`
	TokensOut     int     `json:"tokens_out"`
	CostUSD       float64 `json:"cost_usd"`
	DurationMS    int64   `json:"duration_ms"`
	LLMCalls      int     `json:"llm_calls"`
	ToolCalls     int     `json:"tool_calls"`
}
