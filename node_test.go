package fox

import (
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/assert"
)

func TestParseBraceSegment(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		wantEnd int
		wantKey string
	}{
		// Valid param patterns
		{
			name:    "simple param",
			pattern: "{name}",
			wantEnd: 5,
			wantKey: "?",
		},
		{
			name:    "param with regex",
			pattern: "{name:regex}",
			wantEnd: 11,
			wantKey: "regex",
		},
		{
			name:    "single char param",
			pattern: "{a}",
			wantEnd: 2,
			wantKey: "?",
		},
		{
			name:    "param with complex regex",
			pattern: "{id:[0-9]+}",
			wantEnd: 10,
			wantKey: "[0-9]+",
		},
		{
			name:    "param with nested braces in regex",
			pattern: "{id:[0-9]{1,3}}",
			wantEnd: 14,
			wantKey: "[0-9]{1,3}",
		},
		{
			name:    "param followed by static",
			pattern: "{name}/foo",
			wantEnd: 5,
			wantKey: "?",
		},

		// Valid wildcard patterns
		{
			name:    "simple wildcard",
			pattern: "*{path}",
			wantEnd: 6,
			wantKey: "*",
		},
		{
			name:    "wildcard with regex",
			pattern: "*{path:regex}",
			wantEnd: 12,
			wantKey: "regex",
		},
		{
			name:    "wildcard with complex regex",
			pattern: "*{file:[a-z]+\\.txt}",
			wantEnd: 18,
			wantKey: "[a-z]+\\.txt",
		},
		{
			name:    "wildcard followed by static",
			pattern: "*{path}/thumbnail",
			wantEnd: 6,
			wantKey: "*",
		},

		// Empty and minimal patterns
		{
			name:    "empty string",
			pattern: "",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "empty braces",
			pattern: "{}",
			wantEnd: 1,
			wantKey: "?",
		},
		{
			name:    "empty wildcard braces",
			pattern: "*{}",
			wantEnd: 2,
			wantKey: "*",
		},

		// Invalid patterns should return early
		{
			name:    "no braces",
			pattern: "static",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "just opening brace",
			pattern: "{",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "just closing brace",
			pattern: "}",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "just asterisk",
			pattern: "*",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "asterisk without brace",
			pattern: "*path",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "unclosed param",
			pattern: "{name",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "unclosed wildcard",
			pattern: "*{path",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "unclosed with content",
			pattern: "{name:regex",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "only colon in braces",
			pattern: "{:}",
			wantEnd: 2,
			wantKey: "",
		},
		{
			name:    "colon at start of param name",
			pattern: "{:regex}",
			wantEnd: 7,
			wantKey: "regex",
		},
		{
			name:    "pattern starting with colon then brace",
			pattern: ":foo{bar}",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "unbalanced nested braces",
			pattern: "{id:[0-9]{1,3}",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "slash only",
			pattern: "/",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "path without param",
			pattern: "/users/list",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "closing before opening",
			pattern: "}name{",
			wantEnd: 0,
			wantKey: "",
		},
		{
			name:    "double opening brace",
			pattern: "{{name}",
			wantEnd: 0, // braceIndice needs balanced braces at level 0, {{name} ends at level 1
			wantKey: "",
		},
		{
			name:    "double closing brace",
			pattern: "{name}}",
			wantEnd: 5,
			wantKey: "?",
		},
		{
			name:    "asterisk with space",
			pattern: "* {path}",
			wantEnd: 7, // doesn't start with "*{", so brace in middle is found
			wantKey: "?",
		},
		{
			name:    "brace in middle of static",
			pattern: "foo{bar}baz",
			wantEnd: 7,
			wantKey: "?",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			end, key := parseBraceSegment(tc.pattern)
			assert.Equal(t, tc.wantEnd, end, "unexpected end index")
			assert.Equal(t, tc.wantKey, key, "unexpected key")
		})
	}
}

func TestParseBraceSegmentFuzzNoPanic(t *testing.T) {
	unicodeRanges := fuzz.UnicodeRanges{
		{First: 0x00, Last: 0x7F},   // ASCII
		{First: 0x80, Last: 0x07FF}, // Extended
	}
	f := fuzz.New().NilChance(0).NumElements(10000, 20000).Funcs(unicodeRanges.CustomStringFuzzFunc())

	patterns := make(map[string]struct{})
	f.Fuzz(&patterns)

	for pattern := range patterns {
		assert.NotPanics(t, func() {
			parseBraceSegment(pattern)
		})
	}
}
