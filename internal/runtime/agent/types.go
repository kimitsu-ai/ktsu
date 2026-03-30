package agent

import "encoding/json"

// Message is a single conversation turn exchanged with the LLM gateway.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

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
	// Resume fields — populated only when re-invoking after an approval decision.
	Messages          []Message `json:"messages,omitempty"`            // full message context from checkpoint
	ApprovedToolCalls []string  `json:"approved_tool_calls,omitempty"` // tool_use IDs pre-approved by orchestrator
	IsResume          bool      `json:"is_resume,omitempty"`           // signals orchestrator to accumulate metrics
}

// ModelSpec identifies which LLM model group to use.
type ModelSpec struct {
	Group     string `json:"group"`
	MaxTokens int    `json:"max_tokens"`
}

// ToolApprovalRule describes approval requirements for tools matching a pattern
// on a specific server. Populated by the orchestrator from agent config.
type ToolApprovalRule struct {
	Pattern         string `json:"pattern"`         // exact, "prefix-*", or "*"
	OnReject        string `json:"on_reject"`        // "fail" | "recover"
	TimeoutMS       int64  `json:"timeout_ms"`       // 0 = no timeout
	TimeoutBehavior string `json:"timeout_behavior"` // "fail" | "reject"
}

// ToolServerSpec declares a tool server the agent may call.
type ToolServerSpec struct {
	Name          string             `json:"name"`
	URL           string             `json:"url"`
	Allowlist     []string           `json:"allowlist"`
	AuthToken     string             `json:"auth_token,omitempty"` // resolved bearer token
	ApprovalRules []ToolApprovalRule `json:"approval_rules,omitempty"`
}

// PendingApproval is included in CallbackPayload when status == "pending_approval".
type PendingApproval struct {
	ToolName        string         `json:"tool_name"`
	ToolUseID       string         `json:"tool_use_id"`
	Arguments       map[string]any `json:"arguments"`
	OnReject        string         `json:"on_reject"`
	TimeoutMS       int64          `json:"timeout_ms"`
	TimeoutBehavior string         `json:"timeout_behavior"`
}

// CallbackPayload is the result POSTed to callback_url when the loop finishes.
type CallbackPayload struct {
	RunID     string         `json:"run_id"`
	StepID    string         `json:"step_id"`
	Status    string         `json:"status"` // "ok" | "failed" | "pending_approval"
	Output    map[string]any `json:"output"`
	Error     string         `json:"error"`
	RawOutput string         `json:"raw_output,omitempty"` // last LLM content when output validation failed
	Metrics   Metrics        `json:"metrics"`
	IsResume  bool           `json:"is_resume,omitempty"`
	// Always set — full conversation context for debugging and approval resume.
	Messages        []Message        `json:"messages,omitempty"`
	PendingApproval *PendingApproval `json:"pending_approval,omitempty"`
	// Serialized InvokeRequest — set by runtime on pending_approval for re-dispatch.
	OriginalRequest json.RawMessage `json:"original_request,omitempty"`
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
