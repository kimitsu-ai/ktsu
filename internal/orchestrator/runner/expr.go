package runner

import "regexp"

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
