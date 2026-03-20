package config

// WorkflowConfig represents a workflow.yaml file
type WorkflowConfig struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Trigger     TriggerConfig     `yaml:"trigger"`
	Steps       []StepConfig      `yaml:"steps"`
	Env         map[string]string `yaml:"env"`
}

type TriggerConfig struct {
	Type   string                 `yaml:"type"` // http, schedule, event
	Config map[string]interface{} `yaml:"config"`
}

type StepConfig struct {
	ID      string                 `yaml:"id"`
	Type    string                 `yaml:"type"` // inlet, transform, agent, outlet
	Depends []string               `yaml:"depends"`
	Config  map[string]interface{} `yaml:"config"`
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

// InletConfig represents an inlet step config
type InletConfig struct {
	Schema   map[string]interface{} `yaml:"schema"`
	Mappings map[string]string      `yaml:"mappings"` // JMESPath mappings
}

// OutletConfig represents an outlet step config
type OutletConfig struct {
	Condition string            `yaml:"condition"` // JMESPath condition
	Mappings  map[string]string `yaml:"mappings"`
}

// TransformConfig represents a transform step config
type TransformConfig struct {
	Filter   string            `yaml:"filter"` // JMESPath filter
	Mappings map[string]string `yaml:"mappings"`
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
	Name     string   `yaml:"name"`
	Models   []string `yaml:"models"`
	Strategy string   `yaml:"strategy"` // round_robin, cost_optimized, etc.
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
