package config

// WorkflowConfig represents a workflow.yaml file (kind: workflow)
type WorkflowConfig struct {
	Kind        string         `yaml:"kind"`
	Name        string         `yaml:"name"`
	Version     string         `yaml:"version"`
	Description string         `yaml:"description"`
	Input       WorkflowInput  `yaml:"input,omitempty"`
	Pipeline    []PipelineStep `yaml:"pipeline"`
	ModelPolicy *ModelPolicy   `yaml:"model_policy,omitempty"`
}

// WorkflowInput declares the expected input schema for a workflow.
// The orchestrator validates incoming invoke payloads against this schema.
type WorkflowInput struct {
	Schema map[string]interface{} `yaml:"schema,omitempty"`
}

// PipelineStep is one entry in pipeline[]. Exactly one of Agent/Transform/Webhook is set.
type PipelineStep struct {
	ID                  string                 `yaml:"id"`
	Agent               string                 `yaml:"agent,omitempty"`
	Transform           *TransformSpec         `yaml:"transform,omitempty"`
	Webhook             *WebhookSpec           `yaml:"webhook,omitempty"`
	ForEach             *ForEachSpec           `yaml:"for_each,omitempty"`
	DependsOn           []string               `yaml:"depends_on,omitempty"`
	Condition           string                 `yaml:"condition,omitempty"`
	ConfidenceThreshold float64                `yaml:"confidence_threshold,omitempty"`
	Params              map[string]interface{} `yaml:"params,omitempty"`
	Model               *ModelSpec             `yaml:"model,omitempty"`
	Consolidation       string                 `yaml:"consolidation,omitempty"`
	Output              *OutputSpec            `yaml:"output,omitempty"`
}

// ForEachSpec configures fanout iteration for an agent step.
// The agent is invoked once per item in the array resolved from From.
type ForEachSpec struct {
	From        string `yaml:"from"`
	MaxItems    int    `yaml:"max_items,omitempty"`
	Concurrency int    `yaml:"concurrency,omitempty"`
}

// WebhookSpec declares an HTTP webhook call made by the orchestrator when this step runs.
// The body values are JMESPath expressions evaluated against the accumulated step outputs.
// The URL may use env:VAR_NAME to resolve from the environment at runtime.
type WebhookSpec struct {
	URL       string                 `yaml:"url"`
	Method    string                 `yaml:"method,omitempty"` // default: POST
	Body      map[string]interface{} `yaml:"body,omitempty"`
	TimeoutS  int                    `yaml:"timeout_s,omitempty"` // default: 30
}

type TransformSpec struct {
	Inputs []TransformInput         `yaml:"inputs"`
	Ops    []map[string]interface{} `yaml:"ops"`
}

type TransformInput struct {
	From string `yaml:"from"`
}

type ModelSpec struct {
	Group     string `yaml:"group"`
	MaxTokens int    `yaml:"max_tokens"`
}

type ModelPolicy struct {
	CostBudgetUSD float64           `yaml:"cost_budget_usd"`
	ForceGroup    string            `yaml:"force_group,omitempty"`
	GroupMap      map[string]string `yaml:"group_map,omitempty"`
	TimeoutS      int               `yaml:"timeout_s,omitempty"`
}

type OutputSpec struct {
	Schema map[string]interface{} `yaml:"schema"`
}

// AgentConfig represents an agent config block
type AgentConfig struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Model       string      `yaml:"model"`
	Servers     []ServerRef `yaml:"servers"`
	SubAgents   []string    `yaml:"sub_agents"`
	System      string      `yaml:"system"`
	MaxTurns    int         `yaml:"max_turns"`
	Output      *OutputSpec `yaml:"output,omitempty"`
}

type ServerRef struct {
	Name   string       `yaml:"name"`
	Path   string       `yaml:"path"` // for local servers
	Access AccessConfig `yaml:"access"`
}

type AccessConfig struct {
	Allowlist []string `yaml:"allowlist"`
}

// EnvConfig represents environments/*.env.yaml
type EnvConfig struct {
	Name      string            `yaml:"name"`
	Variables map[string]string `yaml:"variables"`
	Providers []ProviderConfig  `yaml:"providers"`
	State     StateConfig       `yaml:"state"`
}

type ProviderConfig struct {
	Name   string            `yaml:"name"`
	Type   string            `yaml:"type"` // anthropic, openai, etc.
	Config map[string]string `yaml:"config"`
}

type StateConfig struct {
	Driver string `yaml:"driver"` // sqlite, postgres
	DSN    string `yaml:"dsn"`
}

// GatewayConfig represents gateway.yaml
type GatewayConfig struct {
	Providers   []ProviderConfig   `yaml:"providers"`
	ModelGroups []ModelGroupConfig `yaml:"model_groups"`
}

type ModelGroupConfig struct {
	Name               string          `yaml:"name"`
	Models             []string        `yaml:"models"`
	Strategy           string          `yaml:"strategy"` // round_robin, cost_optimized
	DefaultTemperature float64         `yaml:"default_temperature,omitempty"`
	Pricing            []PricingConfig `yaml:"pricing,omitempty"`
}

type PricingConfig struct {
	Model            string  `yaml:"model"`
	InputPerMillion  float64 `yaml:"input_per_million"`
	OutputPerMillion float64 `yaml:"output_per_million"`
}

// ServerManifest represents servers.yaml (marketplace)
type ServerManifest struct {
	Servers []ToolServerConfig `yaml:"servers"`
}

// ToolServerConfig represents a tool server definition
type ToolServerConfig struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	URL         string            `yaml:"url"`
	Auth        string            `yaml:"auth,omitempty"` // bearer token or "env:VAR_NAME"
	Image       string            `yaml:"image"`
	Env         map[string]string `yaml:"env"`
}
