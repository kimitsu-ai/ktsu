package runner

import "testing"

func TestSanitizeExpr(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// basic hyphenated identifier
		{"search-hn.repos", `"search-hn".repos`},
		// already quoted — must not double-quote
		{`"search-hn".repos`, `"search-hn".repos`},
		// no hyphens — unchanged
		{"input.items", "input.items"},
		{"repos", "repos"},
		// multi-segment hyphenated
		{"repo-github.stars", `"repo-github".stars`},
		{"a-b-c.d", `"a-b-c".d`},
		// backtick literal — unchanged
		{"count > `10`", "count > `10`"},
		// mix: hyphenated ident + backtick literal
		{"search-hn.count > `10`", `"search-hn".count > ` + "`10`"},
		// already quoted + backtick literal — both unchanged
		{`"search-hn".count > ` + "`10`", `"search-hn".count > ` + "`10`"},
		// single segment with hyphen (no dot)
		{"search-hn", `"search-hn"`},
		// hyphenated in a filter-style expression
		{"repo-github.active == `true`", `"repo-github".active == ` + "`true`"},
		// multiple hyphenated identifiers in one expression
		{"search-hn.count > repo-github.stars", `"search-hn".count > "repo-github".stars`},
		// empty string
		{"", ""},
		// no special chars
		{"status", "status"},
		// underscore (not a hyphen) — unchanged
		{"search_hn.repos", "search_hn.repos"},
	}
	for _, tc := range cases {
		got := sanitizeExpr(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeExpr(%q)\n  got  %q\n  want %q", tc.in, got, tc.want)
		}
	}
}
