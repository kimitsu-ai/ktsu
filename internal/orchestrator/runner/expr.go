package runner

import (
	"fmt"
	"regexp"
	"strings"

	jmespath "github.com/jmespath/go-jmespath"
)

// hyphenIdentRe matches bare hyphenated JMESPath identifiers (invalid without quoting).
// Alternation order matters: quoted strings and backtick literals are matched first so
// they are consumed verbatim; the hyphenated-identifier branch is last.
//
// JMESPath treats '-' as arithmetic subtraction, so step IDs like "search-hn" must be
// written as `"search-hn"` in expressions. sanitizeExpr applies this transformation
// transparently so users can write natural hyphenated step IDs in workflow YAML.
var hyphenIdentRe = regexp.MustCompile(
	`("(?:[^"\\]|\\.)*"` + // already-quoted string literal → skip
		"|`[^`]*`" + // backtick literal (numeric/bool constant) → skip
		`|([a-zA-Z][a-zA-Z0-9]*(?:-[a-zA-Z0-9]+)+))`, // bare hyphenated identifier → quote
)

// templateRe matches {{ expr }} placeholders for inline string interpolation.
var templateRe = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)

// sanitizeExpr rewrites bare hyphenated identifiers in a JMESPath expression to their
// quoted form so that e.g. "search-hn.repos" becomes `"search-hn".repos`.
// Already-quoted identifiers and backtick literals are left unchanged.
func sanitizeExpr(expr string) string {
	if expr == "" {
		return expr
	}
	return hyphenIdentRe.ReplaceAllStringFunc(expr, func(match string) string {
		if match[0] == '"' || match[0] == '`' {
			return match // already quoted or backtick literal — pass through
		}
		return `"` + match + `"`
	})
}

// stripTemplate strips a standalone {{ expr }} wrapper and returns the bare expression.
// If s is not a template, it is returned unchanged. Use this when the entire value is
// a template expression and you want the typed JMESPath result.
func stripTemplate(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{{") && strings.HasSuffix(s, "}}") {
		return strings.TrimSpace(s[2 : len(s)-2])
	}
	return s
}

// interpolateTemplates replaces all {{ expr }} occurrences within s with their
// string-formatted JMESPath values evaluated against ctx. Used for URLs and other
// strings where templates appear inline alongside literal text.
func interpolateTemplates(s string, ctx map[string]interface{}) (string, error) {
	var lastErr error
	result := templateRe.ReplaceAllStringFunc(s, func(match string) string {
		sub := templateRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		val, err := jmespath.Search(sanitizeExpr(sub[1]), ctx)
		if err != nil {
			lastErr = err
			return ""
		}
		if val == nil {
			return ""
		}
		return fmt.Sprintf("%v", val)
	})
	return result, lastErr
}

// buildExprContext constructs the JMESPath evaluation context for expressions in
// conditions, params, body mappings, and output.map. The context includes:
//   - "step": all step outputs keyed by step ID (new canonical namespace)
//   - flat step IDs at the top level for backward compatibility
//   - "params": richParams (typed values from {{ }} param resolution)
//   - "env": envVars (root workflow only; pass nil for sub-workflows)
func buildExprContext(
	stepOutputs map[string]map[string]interface{},
	richParams map[string]interface{},
	envVars map[string]string,
) map[string]interface{} {
	ctx := make(map[string]interface{}, len(stepOutputs)+3)

	// Flat step IDs for backward compatibility (e.g. "receive.field").
	for id, out := range stepOutputs {
		ctx[id] = out
	}

	// "step" namespace for new-style expressions (e.g. "step.receive.field").
	stepNS := make(map[string]interface{}, len(stepOutputs))
	for id, out := range stepOutputs {
		stepNS[id] = out
	}
	ctx["step"] = stepNS

	if richParams != nil {
		ctx["params"] = richParams
	}

	if envVars != nil {
		envNS := make(map[string]interface{}, len(envVars))
		for k, v := range envVars {
			envNS[k] = v
		}
		ctx["env"] = envNS
	}

	return ctx
}
