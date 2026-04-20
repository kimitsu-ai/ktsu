package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var placeholderRe = regexp.MustCompile(`\{\{[^}]+\}\}`)

// ResolveEnvValue resolves an "env:VAR_NAME" reference to its environment value.
// Non-env: values are returned unchanged.
func ResolveEnvValue(v string) string {
	if strings.HasPrefix(v, "env:") {
		return os.Getenv(strings.TrimPrefix(v, "env:"))
	}
	return v
}

// lookupEnvValue is like ResolveEnvValue but returns an error when an env:VAR
// reference names a variable that is not set in the environment.
func lookupEnvValue(v string) (string, error) {
	if strings.HasPrefix(v, "env:") {
		varName := strings.TrimPrefix(v, "env:")
		val, ok := os.LookupEnv(varName)
		if !ok {
			return "", fmt.Errorf("env var %q is not set", varName)
		}
		return val, nil
	}
	return v, nil
}

// ResolveValue resolves a value string using env and invocation params contexts.
// Handles: "env:VAR_NAME" (root-only), "param:NAME" (from invocationParams),
// "`literal`" (JMESPath backtick literal), plain strings (returned as-is).
// allowEnv must be true for env: references to succeed; false models sub-workflow context.
func ResolveValue(v string, allowEnv bool, invocationParams map[string]string) (string, error) {
	switch {
	case strings.HasPrefix(v, "env:"):
		if !allowEnv {
			return "", fmt.Errorf("env: reference %q not permitted outside root workflow context", v)
		}
		varName := strings.TrimPrefix(v, "env:")
		val, ok := os.LookupEnv(varName)
		if !ok {
			return "", fmt.Errorf("env var %q is not set", varName)
		}
		return val, nil
	case strings.HasPrefix(v, "param:"):
		name := strings.TrimPrefix(v, "param:")
		if invocationParams == nil {
			return "", fmt.Errorf("param %q referenced but no params context is available", name)
		}
		val, ok := invocationParams[name]
		if !ok {
			return "", fmt.Errorf("param %q is not available in this invocation", name)
		}
		return val, nil
	case len(v) >= 2 && v[0] == '`' && v[len(v)-1] == '`':
		return v[1 : len(v)-1], nil
	default:
		return v, nil
	}
}

// ValidateSystemPromptStatic returns an error if the system prompt contains
// any {{ }} template expressions. System prompts must be static for prompt caching.
func ValidateSystemPromptStatic(system string) error {
	if placeholderRe.MatchString(system) {
		return fmt.Errorf("prompt.system must be static — remove {{ }} expressions and use prompt.user for dynamic content")
	}
	return nil
}

// InterpolatePrompt replaces {{ params.key }} or {{ key }} placeholders in tmpl with values from resolved.
// Returns an error if any placeholder key is missing from resolved.
func InterpolatePrompt(tmpl string, resolved map[string]string) (string, error) {
	var replaceErr error
	result := placeholderRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := strings.TrimSpace(match[2 : len(match)-2])
		key = strings.TrimPrefix(key, "params.")
		v, ok := resolved[key]
		if !ok && replaceErr == nil {
			replaceErr = fmt.Errorf("prompt references param %q which has no resolved value", key)
		}
		return v
	})
	if replaceErr != nil {
		return "", replaceErr
	}
	return result, nil
}

// ResolveAgentParams resolves final string values for all declared agent params.
// Returns the resolved values, a map of which params are secret, and any error.
// Secret params must use an env: source — literal strings are rejected.
func ResolveAgentParams(declared map[string]ParamDecl, stepAgentParams map[string]any) (map[string]string, map[string]bool, error) {
	result := make(map[string]string, len(declared))
	isSecret := make(map[string]bool, len(declared))
	for name, decl := range declared {
		if decl.Default != nil {
			val, err := lookupEnvValue(*decl.Default)
			if err != nil {
				return nil, nil, fmt.Errorf("agent param %q default: %w", name, err)
			}
			result[name] = val
		}
		if v, ok := stepAgentParams[name]; ok {
			s, ok := v.(string)
			if !ok {
				return nil, nil, fmt.Errorf("agent param %q: value must be a string, got %T", name, v)
			}
			if decl.Secret && !strings.HasPrefix(s, "env:") {
				return nil, nil, fmt.Errorf("agent param %q is secret and must use an env: source", name)
			}
			val, err := lookupEnvValue(s)
			if err != nil {
				return nil, nil, fmt.Errorf("agent param %q: %w", name, err)
			}
			result[name] = val
		}
		if _, ok := result[name]; !ok {
			return nil, nil, fmt.Errorf("required agent param %q has no value", name)
		}
		if decl.Secret {
			isSecret[name] = true
		}
	}
	return result, isSecret, nil
}

// ResolveServerParams resolves final values for all declared server params.
// serverRefParams are the values declared in the agent file's ServerRef.Params —
// these may be {{ params.KEY }} templates resolved against resolvedAgentParams.
// Returns resolved values, a map of which are secret, and any error.
// Secret server params must be fed from secret agent params.
func ResolveServerParams(
	declared map[string]ParamDecl,
	serverRefParams map[string]string,
	resolvedAgentParams map[string]string,
	agentIsSecret map[string]bool,
) (map[string]string, map[string]bool, error) {
	result := make(map[string]string, len(declared))
	isSecret := make(map[string]bool, len(declared))
	for name, decl := range declared {
		if decl.Default != nil {
			val, err := lookupEnvValue(*decl.Default)
			if err != nil {
				return nil, nil, fmt.Errorf("server param %q default: %w", name, err)
			}
			result[name] = val
		}
		if refVal, ok := serverRefParams[name]; ok {
			val, agentKey, err := resolveServerParamValue(name, refVal, resolvedAgentParams)
			if err != nil {
				return nil, nil, err
			}
			if decl.Secret && agentKey != "" && !agentIsSecret[agentKey] {
				return nil, nil, fmt.Errorf("server param %q is secret but agent param %q is not marked secret", name, agentKey)
			}
			result[name] = val
		}
		if _, ok := result[name]; !ok {
			return nil, nil, fmt.Errorf("required server param %q has no value", name)
		}
		if decl.Secret {
			isSecret[name] = true
		}
	}
	return result, isSecret, nil
}

// resolveServerParamValue resolves a single server ref param value.
// If the value is a {{ params.KEY }} template, it is resolved against agentParams.
// Returns the resolved value, the agent param key used (if any), and any error.
func resolveServerParamValue(paramName, refVal string, agentParams map[string]string) (string, string, error) {
	trimmed := strings.TrimSpace(refVal)
	if strings.HasPrefix(trimmed, "{{") && strings.HasSuffix(trimmed, "}}") {
		inner := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
		if !strings.HasPrefix(inner, "params.") {
			return "", "", fmt.Errorf("server param %q: unsupported template %q — only {{ params.KEY }} is supported", paramName, refVal)
		}
		agentKey := strings.TrimPrefix(inner, "params.")
		agentVal, ok := agentParams[agentKey]
		if !ok {
			return "", "", fmt.Errorf("server param %q: agent param %q not found", paramName, agentKey)
		}
		return agentVal, agentKey, nil
	}
	val, err := lookupEnvValue(refVal)
	if err != nil {
		return "", "", fmt.Errorf("server param %q: %w", paramName, err)
	}
	return val, "", nil
}

// BuildScrubSet returns the resolved values of all secret params.
// Use the returned slice to redact secret values from error messages and envelope writes.
func BuildScrubSet(resolved map[string]string, isSecret map[string]bool) []string {
	var out []string
	for k, v := range resolved {
		if isSecret[k] && v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ParseParamsSchema converts a JSON Schema params declaration to the internal
// map[string]ParamDecl used by resolution functions.
// Required params (listed in "required") have nil Default.
// Optional params without an explicit "default" get an empty string default (not required).
func ParseParamsSchema(schema map[string]interface{}) (map[string]ParamDecl, error) {
	if schema == nil {
		return nil, nil
	}
	props, _ := schema["properties"].(map[string]interface{})
	requiredRaw, _ := schema["required"].([]interface{})
	required := make(map[string]bool, len(requiredRaw))
	for _, r := range requiredRaw {
		if s, ok := r.(string); ok {
			required[s] = true
		}
	}
	result := make(map[string]ParamDecl, len(props))
	for name, propRaw := range props {
		prop, _ := propRaw.(map[string]interface{})
		pd := ParamDecl{}
		if desc, ok := prop["description"].(string); ok {
			pd.Description = desc
		}
		if secret, ok := prop["secret"].(bool); ok {
			pd.Secret = secret
		}
		if def, ok := prop["default"].(string); ok {
			pd.Default = &def
		} else if !required[name] {
			// Optional param with no explicit default: use empty string so it isn't treated as required.
			empty := ""
			pd.Default = &empty
		}
		// required[name] && pd.Default == nil → param is required (nil Default = required in ParamDecl)
		result[name] = pd
	}
	return result, nil
}
