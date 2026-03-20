package types

import "time"

type StepType string

const (
	StepTypeInlet     StepType = "inlet"
	StepTypeTransform StepType = "transform"
	StepTypeAgent     StepType = "agent"
	StepTypeOutlet    StepType = "outlet"
)

type StepStatus string

const (
	StepStatusPending  StepStatus = "pending"
	StepStatusRunning  StepStatus = "running"
	StepStatusComplete StepStatus = "complete"
	StepStatusFailed   StepStatus = "failed"
	StepStatusSkipped  StepStatus = "skipped"
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
}
