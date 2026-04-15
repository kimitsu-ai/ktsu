package types

import (
	"encoding/json"
	"time"
)

type StepType string

const (
	StepTypeTransform StepType = "transform"
	StepTypeAgent     StepType = "agent"
	StepTypeWebhook   StepType = "webhook"
	StepTypeWorkflow  StepType = "workflow"
)

type StepStatus string

const (
	StepStatusPending  StepStatus = "pending"
	StepStatusRunning  StepStatus = "running"
	StepStatusComplete StepStatus = "complete"
	StepStatusFailed   StepStatus = "failed"
	StepStatusSkipped          StepStatus = "skipped"
	StepStatusPendingApproval  StepStatus = "pending_approval"
)

type RunStatus string

const (
	RunStatusPending  RunStatus = "pending"
	RunStatusRunning  RunStatus = "running"
	RunStatusComplete RunStatus = "complete"
	RunStatusFailed   RunStatus = "failed"
)

type Run struct {
	ID           string            `json:"id"`
	WorkflowName string            `json:"workflow_name"`
	Status       RunStatus         `json:"status"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	CompletedAt  *time.Time        `json:"completed_at,omitempty"`
	Error        string            `json:"error,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Step struct {
	ID        string                 `json:"id"`
	RunID     string                 `json:"run_id"`
	Name      string                 `json:"name"`
	Type      StepType               `json:"type"`
	Status    StepStatus             `json:"status"`
	StartedAt *time.Time             `json:"started_at,omitempty"`
	EndedAt   *time.Time             `json:"ended_at,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Output    map[string]interface{} `json:"output,omitempty"`
	Metrics   StepMetrics            `json:"metrics,omitempty"`
	Reflected *bool                  `json:"reflected,omitempty"`
	Messages  json.RawMessage        `json:"messages,omitempty"` // full conversation context, set on pending_approval
}

// ApprovalStatus is the lifecycle state of a manual approval request.
type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusRejected ApprovalStatus = "rejected"
	ApprovalStatusTimeout  ApprovalStatus = "timeout"
)

// Approval records a pending or decided manual approval for a tool call.
type Approval struct {
	RunID           string          `json:"run_id"`
	StepID          string          `json:"step_id"`
	ToolName        string          `json:"tool_name"`
	ToolUseID       string          `json:"tool_use_id"`
	Arguments       map[string]any  `json:"arguments,omitempty"`
	OnReject        string          `json:"on_reject"`
	TimeoutMS       int64           `json:"timeout_ms"`
	TimeoutBehavior string          `json:"timeout_behavior"`
	Status          ApprovalStatus  `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
	DecidedAt       *time.Time      `json:"decided_at,omitempty"`
	// OriginalRequest is the serialized InvokeRequest for re-dispatch on approval.
	OriginalRequest json.RawMessage `json:"original_request,omitempty"`
	// PartialMetrics holds metrics from the pending_approval leg.
	PartialMetrics  StepMetrics     `json:"partial_metrics,omitempty"`
}
