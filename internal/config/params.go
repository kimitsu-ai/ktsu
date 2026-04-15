package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

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

// ValidatePromptRefs returns an error if prompt.system references any {{key}}
// not declared in params. Call this at boot to catch misconfigured agents.
func ValidatePromptRefs(system string, params map[string]ParamDecl) error {
	matches := placeholderRe.FindAllStringSubmatch(system, -1)
	for _, m := range matches {
		key := m[1]
		if _, ok := params[key]; !ok {
			return fmt.Errorf("prompt references undeclared param %q", key)
		}
	}
	return nil
}

// InterpolatePrompt replaces {{key}} placeholders in system with values from resolved.
// Returns an error if any placeholder key is missing from resolved.
func InterpolatePrompt(system string, resolved map[string]string) (string, error) {
	var replaceErr error
	result := placeholderRe.ReplaceAllStringFunc(system, func(match string) string {
		key := placeholderRe.FindStringSubmatch(match)[1]
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
// Resolution order: ParamDecl.Default < stepAgentParams (last wins).
// Returns an error if any required param (nil Default) has no value in stepAgentParams.
// Resolves env:VAR_NAME in both defaults and step-provided values.
func ResolveAgentParams(declared map[string]ParamDecl, stepAgentParams map[string]any) (map[string]string, error) {
	result := make(map[string]string, len(declared))
	for name, decl := range declared {
		if decl.Default != nil {
			val, err := lookupEnvValue(*decl.Default)
			if err != nil {
				return nil, fmt.Errorf("agent param %q default: %w", name, err)
			}
			result[name] = val
		}
		if v, ok := stepAgentParams[name]; ok {
			if s, ok := v.(string); ok {
				val, err := lookupEnvValue(s)
				if err != nil {
					return nil, fmt.Errorf("agent param %q: %w", name, err)
				}
				result[name] = val
			} else {
				return nil, fmt.Errorf("agent param %q: value must be a string, got %T", name, v)
			}
		}
		if _, ok := result[name]; !ok {
			return nil, fmt.Errorf("required agent param %q has no value", name)
		}
	}
	return result, nil
}

// ResolveServerParams resolves final string values for all declared server params.
// Resolution order: ParamDecl.Default < agentRefParams < stepServerParams (last wins).
// Returns an error if any required param (nil Default) has no value anywhere.
// Resolves env:VAR_NAME at each level.
func ResolveServerParams(declared map[string]ParamDecl, agentRefParams map[string]string, stepServerParams map[string]any) (map[string]string, error) {
	result := make(map[string]string, len(declared))
	for name, decl := range declared {
		if decl.Default != nil {
			val, err := lookupEnvValue(*decl.Default)
			if err != nil {
				return nil, fmt.Errorf("server param %q default: %w", name, err)
			}
			result[name] = val
		}
		if v, ok := agentRefParams[name]; ok {
			val, err := lookupEnvValue(v)
			if err != nil {
				return nil, fmt.Errorf("server param %q agent ref: %w", name, err)
			}
			result[name] = val
		}
		if v, ok := stepServerParams[name]; ok {
			if s, ok := v.(string); ok {
				val, err := lookupEnvValue(s)
				if err != nil {
					return nil, fmt.Errorf("server param %q: %w", name, err)
				}
				result[name] = val
			} else {
				return nil, fmt.Errorf("server param %q: value must be a string, got %T", name, v)
			}
		}
		if _, ok := result[name]; !ok {
			return nil, fmt.Errorf("required server param %q has no value", name)
		}
	}
	return result, nil
}
