package config

// ParamDecl declares a named parameter on an agent or server.
// Default nil means the param is required.
type ParamDecl struct {
	Description string  `yaml:"description"`
	Default     *string `yaml:"default,omitempty"`
}

// ParamsSchemaDecl wraps a JSON Schema declaration for workflow or agent params.
// YAML: params: { schema: { type: object, required: [...], properties: { name: { type, description, default? } } } }
type ParamsSchemaDecl struct {
	Schema map[string]interface{} `yaml:"schema,omitempty"`
}

// PromptConfig holds the LLM-facing prompt configuration for an agent.
type PromptConfig struct {
	System string `yaml:"system"`
}

// WorkflowConfig represents a workflow.yaml file (kind: workflow)
type WorkflowConfig struct {
	Kind        string           `yaml:"kind"`
	Name        string           `yaml:"name"`
	Version     string           `yaml:"version"`
	Description string           `yaml:"description"`
	Visibility  string           `yaml:"visibility,omitempty"`  // "root" | "sub-workflow"
	Webhooks    string           `yaml:"webhooks,omitempty"`    // "execute" | "suppress"
	Params      ParamsSchemaDecl `yaml:"params,omitempty"`      // JSON Schema params declaration
	Input       WorkflowInput    `yaml:"input,omitempty"`
	Pipeline    []PipelineStep   `yaml:"pipeline"`
	ModelPolicy *ModelPolicy     `yaml:"model_policy,omitempty"`
}

// WorkflowInput declares the expected input schema for a workflow.
// The orchestrator validates incoming invoke payloads against this schema.
type WorkflowInput struct {
	Schema map[string]interface{} `yaml:"schema,omitempty"`
}

// PipelineStep is one entry in pipeline[]. Exactly one of Agent/Transform/Webhook/Workflow is set.
type PipelineStep struct {
	ID                  string                 `yaml:"id"`
	Agent               string                 `yaml:"agent,omitempty"`
	Transform           *TransformSpec         `yaml:"transform,omitempty"`
	Webhook             *WebhookSpec           `yaml:"webhook,omitempty"`
	Workflow            string                 `yaml:"workflow,omitempty"`
	WorkflowInput       map[string]interface{} `yaml:"input,omitempty"`
	WorkflowWebhooks    string                 `yaml:"webhooks,omitempty"`
	// For agent steps: {"agent": {"key": val}, "server": {"name": {"key": val}}}
	// For workflow steps: {"param_name": "value_expr"}
	Params              map[string]interface{} `yaml:"params,omitempty"`
	ForEach             *ForEachSpec           `yaml:"for_each,omitempty"`
	DependsOn           []string               `yaml:"depends_on,omitempty"`
	Condition           string                 `yaml:"condition,omitempty"`
	ConfidenceThreshold float64                `yaml:"confidence_threshold,omitempty"`
	Model               *ModelSpec             `yaml:"model,omitempty"`
	Consolidation       string                 `yaml:"consolidation,omitempty"`
	Output              *OutputSpec            `yaml:"output,omitempty"`
}

// AgentParams extracts the agent sub-map from Params (for agent steps).
// YAML: params: { agent: { key: val } }
func (s *PipelineStep) AgentParams() map[string]any {
	if s.Params == nil {
		return nil
	}
	m, _ := s.Params["agent"].(map[string]interface{})
	return m
}

// ServerParams extracts the server sub-map from Params (for agent steps).
// YAML: params: { server: { servername: { key: val } } }
func (s *PipelineStep) ServerParams() map[string]map[string]any {
	if s.Params == nil {
		return nil
	}
	raw, _ := s.Params["server"].(map[string]interface{})
	if raw == nil {
		return nil
	}
	result := make(map[string]map[string]any, len(raw))
	for k, v := range raw {
		if m, ok := v.(map[string]interface{}); ok {
			result[k] = m
		}
	}
	return result
}

// ForEachSpec configures fanout iteration for an agent step.
type ForEachSpec struct {
	From        string `yaml:"from"`
	MaxItems    int    `yaml:"max_items,omitempty"`
	Concurrency int    `yaml:"concurrency,omitempty"`
	MaxFailures int    `yaml:"max_failures,omitempty"` // 0=fail-fast (default), -1=unlimited, N=tolerate up to N
}

// WebhookSpec declares an HTTP webhook call.
// The body values are JMESPath expressions evaluated against the accumulated step outputs. The URL may use env:VAR_NAME to resolve from the environment at runtime.
type WebhookSpec struct {
	URL      string                 `yaml:"url"`
	Method   string                 `yaml:"method,omitempty"`   // default: POST
	Body     map[string]interface{} `yaml:"body,omitempty"`
	TimeoutS int                    `yaml:"timeout_s,omitempty"` // default: 30
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

// AgentConfig represents an agent config block.
// prompt.system replaces the former top-level system field.
type AgentConfig struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Model       string           `yaml:"model"`
	Params      ParamsSchemaDecl `yaml:"params,omitempty"`
	Prompt      PromptConfig     `yaml:"prompt"`
	Reflect     string           `yaml:"reflect,omitempty"`
	Servers     []ServerRef      `yaml:"servers"`
	SubAgents   []string         `yaml:"sub_agents"`
	MaxTurns    int              `yaml:"max_turns"`
	Output      *OutputSpec      `yaml:"output,omitempty"`
}

type ServerRef struct {
	Name   string            `yaml:"name"`
	Path   string            `yaml:"path"`
	Params map[string]string `yaml:"params,omitempty"`
	Access AccessConfig      `yaml:"access"`
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

// ToolServerConfig represents a tool server definition.
type ToolServerConfig struct {
	Name        string               `yaml:"name"`
	Description string               `yaml:"description"`
	URL         string               `yaml:"url"`
	Auth        string               `yaml:"auth,omitempty"`
	Params      map[string]ParamDecl `yaml:"params,omitempty"`
}
