package config

// WorkflowConfig represents a workflow.yaml file (kind: workflow)
type WorkflowConfig struct {
	Kind        string         `yaml:"kind"`
	Name        string         `yaml:"name"`
	Version     string         `yaml:"version"`
	Description string         `yaml:"description"`
	Pipeline    []PipelineStep `yaml:"pipeline"`
	ModelPolicy *ModelPolicy   `yaml:"model_policy,omitempty"`
}

// PipelineStep is one entry in pipeline[]. Exactly one of Inlets/Inlet/Agent/Transform/Outlet is set.
type PipelineStep struct {
	ID                  string                 `yaml:"id"`
	Inlets              []string               `yaml:"inlets,omitempty"`
	Inlet               string                 `yaml:"inlet,omitempty"`
	Agent               string                 `yaml:"agent,omitempty"`
	Transform           *TransformSpec         `yaml:"transform,omitempty"`
	Outlet              string                 `yaml:"outlet,omitempty"`
	DependsOn           []string               `yaml:"depends_on,omitempty"`
	Condition           string                 `yaml:"condition,omitempty"`
	ConfidenceThreshold float64                `yaml:"confidence_threshold,omitempty"`
	Params              map[string]interface{} `yaml:"params,omitempty"`
	Model               *ModelSpec             `yaml:"model,omitempty"`
	Consolidation       string                 `yaml:"consolidation,omitempty"`
	Output              *OutputSpec            `yaml:"output,omitempty"`
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
}

type OutputSpec struct {
	Schema map[string]interface{} `yaml:"schema"`
}

// InletConfig represents an inlet YAML file (kind: inlet)
type InletConfig struct {
	Kind        string       `yaml:"kind"`
	Name        string       `yaml:"name"`
	Version     string       `yaml:"version"`
	Description string       `yaml:"description"`
	Trigger     TriggerSpec  `yaml:"trigger"`
	Mapping     InletMapping `yaml:"mapping"`
	Output      OutputSpec   `yaml:"output"`
}

type TriggerSpec struct {
	Type string `yaml:"type"` // webhook, schedule, email, workflow
	Path string `yaml:"path,omitempty"`
	Cron string `yaml:"cron,omitempty"`
}

type InletMapping struct {
	Envelope map[string]string `yaml:"envelope"`
	Output   map[string]string `yaml:"output"`
}

// OutletConfig represents an outlet YAML file (kind: outlet)
type OutletConfig struct {
	Kind        string        `yaml:"kind"`
	Name        string        `yaml:"name"`
	Version     string        `yaml:"version"`
	Description string        `yaml:"description"`
	Inputs      []OutletInput `yaml:"inputs"`
	Mapping     OutletMapping `yaml:"mapping"`
	Output      OutputSpec    `yaml:"output"`
}

type OutletInput struct {
	From     string `yaml:"from"`
	Optional bool   `yaml:"optional"`
}

type OutletMapping struct {
	Action OutletAction `yaml:"action"`
}

type OutletAction struct {
	Type string                 `yaml:"type"` // http_post, http_put, email_reply, noop
	URL  string                 `yaml:"url,omitempty"`
	Body map[string]interface{} `yaml:"body,omitempty"`
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
	Image       string            `yaml:"image"`
	Env         map[string]string `yaml:"env"`
}
