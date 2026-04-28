package config

import (
	"time"

	"gopkg.in/yaml.v3"
)

// ParamDecl declares a named parameter on an agent or server.
// Default nil means the param is required.
type ParamDecl struct {
	Description string  `yaml:"description"`
	Default     *string `yaml:"default,omitempty"`
	Secret      bool    `yaml:"secret,omitempty"`
}

// ParamsSchemaDecl wraps a JSON Schema declaration for workflow or agent params.
// YAML: params: { schema: { type: object, required: [...], properties: { name: { type, description, default? } } } }
type ParamsSchemaDecl struct {
	Schema map[string]interface{} `yaml:"schema,omitempty"`
}

// PromptConfig holds the LLM-facing prompt configuration for an agent.
type PromptConfig struct {
	System  string `yaml:"system"`
	User    string `yaml:"user,omitempty"`
	Reflect string `yaml:"reflect,omitempty"`
}

// InvokeAuthConfig declares how the orchestrator authenticates incoming /invoke requests
// for this workflow. If absent, the endpoint is unauthenticated.
type InvokeAuthConfig struct {
	Header string `yaml:"header"` // HTTP header name to read the token from
	Scheme string `yaml:"scheme"` // "bearer" (strip "Bearer " prefix) or "raw" (compare as-is)
	Secret string `yaml:"secret"` // value expression: env:VAR, param:NAME, or backtick literal
}

// InvokeConfig holds the invoke-time configuration for a workflow.
type InvokeConfig struct {
	Auth *InvokeAuthConfig `yaml:"auth,omitempty"`
}

// ServerAuthConfig declares how the MCP client authenticates outbound requests to a tool server.
// If absent on ToolServerConfig, no auth header is sent.
type ServerAuthConfig struct {
	Header string `yaml:"header"` // HTTP header name; defaults to "Authorization" at resolution time
	Scheme string `yaml:"scheme"` // "bearer" (prepend "Bearer ") or "raw" (value as-is); defaults to "bearer"
	Secret string `yaml:"secret"` // value expression: env:VAR, param:NAME, or backtick literal
}

// WorkflowConfig represents a workflow.yaml file (kind: workflow)
type WorkflowConfig struct {
	Kind        string           `yaml:"kind"`
	Name        string           `yaml:"name"`
	Version     string           `yaml:"version"`
	Description string           `yaml:"description"`
	Visibility  string           `yaml:"visibility,omitempty"`  // "root" | "sub-workflow"
	Webhooks    string           `yaml:"webhooks,omitempty"`    // "execute" | "suppress"
	Env         []EnvVarDecl     `yaml:"env,omitempty"`
	Params      ParamsSchemaDecl `yaml:"params,omitempty"`      // JSON Schema params declaration
	Pipeline    []PipelineStep   `yaml:"pipeline"`
	Output      *OutputSpec      `yaml:"output,omitempty"`
	ModelPolicy *ModelPolicy     `yaml:"model_policy,omitempty"`
	Invoke      InvokeConfig     `yaml:"invoke,omitempty"`
}

// PipelineStep is one entry in pipeline[]. Exactly one of Agent/Transform/Webhook/Workflow is set.
type PipelineStep struct {
	ID                  string                 `yaml:"id"`
	On                  string                 `yaml:"on,omitempty"` // "approval" — fires when a depends_on step enters pending_approval
	Agent               string                 `yaml:"agent,omitempty"`
	Transform           *TransformSpec         `yaml:"transform,omitempty"`
	Webhook             *WebhookSpec           `yaml:"webhook,omitempty"`
	Workflow            string                 `yaml:"workflow,omitempty"`
	WorkflowInput       map[string]interface{} `yaml:"input,omitempty"`
	WorkflowWebhooks    string                 `yaml:"webhooks,omitempty"`
	// For agent steps: flat key→value pairs; the reserved key "server" holds server ref params.
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

// AgentParams returns all top-level params excluding the "server" key.
// YAML: params: { name: val, message: val, server: { ... } }
func (s *PipelineStep) AgentParams() map[string]any {
	if s.Params == nil {
		return nil
	}
	result := make(map[string]any, len(s.Params))
	for k, v := range s.Params {
		if k == "server" {
			continue
		}
		result[k] = v
	}
	if len(result) == 0 {
		return nil
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
	Map    map[string]string      `yaml:"map,omitempty"`
	From   string                 `yaml:"from,omitempty"`
}

// EnvVarDecl declares an environment variable required by a workflow.
type EnvVarDecl struct {
	Name        string  `yaml:"name"`
	Secret      bool    `yaml:"secret,omitempty"`
	Default     *string `yaml:"default,omitempty"`
	Description string  `yaml:"description,omitempty"`
}

// AgentConfig represents an agent config block.
// prompt.system replaces the former top-level system field.
type AgentConfig struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Model       string           `yaml:"model"`
	Params      ParamsSchemaDecl `yaml:"params,omitempty"`
	Prompt      PromptConfig     `yaml:"prompt"`
	Servers     []ServerRef      `yaml:"servers"`
	MaxTurns    int              `yaml:"max_turns"`
	Output      *OutputSpec      `yaml:"output,omitempty"`
}

type ServerRef struct {
	Name   string            `yaml:"name"`
	Path   string            `yaml:"path"`
	Params map[string]string `yaml:"params,omitempty"`
	Access AccessConfig      `yaml:"access"`
}

// AccessConfig controls which tools an agent may call on a server.
type AccessConfig struct {
	Allowlist []ToolAccess `yaml:"allowlist"`
}

// ToolAccess is a single allowlist entry. It unmarshals from either a plain
// YAML string ("tool-name") or an object with optional require_approval policy.
type ToolAccess struct {
	Name            string          `yaml:"name"`
	RequireApproval *ApprovalPolicy `yaml:"require_approval,omitempty"`
}

// UnmarshalYAML implements yaml.Unmarshaler so that a plain scalar like
// "delete-*" is treated as ToolAccess{Name: "delete-*"}.
func (t *ToolAccess) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		t.Name = value.Value
		return nil
	}
	type toolAccessAlias ToolAccess
	var alias toolAccessAlias
	if err := value.Decode(&alias); err != nil {
		return err
	}
	*t = ToolAccess(alias)
	return nil
}

// ApprovalPolicy declares how the orchestrator should handle a required approval.
type ApprovalPolicy struct {
	OnReject        string        `yaml:"on_reject"`         // "fail" | "recover"
	Timeout         time.Duration `yaml:"timeout,omitempty"` // 0 = no timeout
	TimeoutBehavior string        `yaml:"timeout_behavior"`  // "fail" | "reject"
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
	Env         []EnvVarDecl       `yaml:"env,omitempty"`
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
	Auth        *ServerAuthConfig    `yaml:"auth,omitempty"`
	Params      map[string]ParamDecl `yaml:"params,omitempty"`
}

// HubLockFile represents ktsuhub.lock.yaml — tracks installed workflow packages.
type HubLockFile struct {
	Entries []HubLockEntry `yaml:"entries"`
}

// HubLockEntry is one installed workflow package.
type HubLockEntry struct {
	Name    string `yaml:"name"`              // e.g. "kyle/support-triage"
	Version string `yaml:"version,omitempty"` // semver if known
	Source  string `yaml:"source"`            // "ktsuhub", "github.com/owner/repo", "https://..."
	Ref     string `yaml:"ref,omitempty"`     // tag, branch, or SHA used at install time
	SHA     string `yaml:"sha,omitempty"`     // resolved commit SHA
	Cache   string `yaml:"cache"`             // absolute path to local cache dir
	Mutable bool   `yaml:"mutable"`           // true if installed from a branch (not pinned)
}

// HubManifest represents ktsuhub.yaml — declares which workflows a repo publishes.
type HubManifest struct {
	Workflows []HubManifestEntry `yaml:"workflows"`
}

// HubManifestEntry declares a single publishable workflow.
type HubManifestEntry struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Entrypoint  string   `yaml:"entrypoint"`
}
