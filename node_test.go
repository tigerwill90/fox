package fox

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestEmptyCatchAll(t *testing.T) {

	cases := []struct {
		name   string
		routes []string
		path   string
	}{
		{
			name:   "infix wildcard",
			routes: []string{"/foo/*{args}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard regexp",
			routes: []string{"/foo/*{args:$^}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with children",
			routes: []string{"/foo/*{args}/bar", "/foo/*{args}/caz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with children regexp",
			routes: []string{"/foo/*{args:$^}/bar", "/foo/*{args:$^}/caz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with static edge",
			routes: []string{"/foo/*{args}/bar", "/foo/baz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with static edge regexp",
			routes: []string{"/foo/*{args:$^}/bar", "/foo/baz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard and suffix wildcard",
			routes: []string{"/foo/*{args}/bar", "/foo/*{args}"},
			path:   "/foo/",
		},
		{
			name:   "infix wildcard and suffix wildcard regexp",
			routes: []string{"/foo/*{args:$^}/bar", "/foo/*{args:$^}"},
			path:   "/foo/",
		},
		{
			name:   "infix inflight wildcard",
			routes: []string{"/foo/abc*{args}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard regexp",
			routes: []string{"/foo/abc*{args:$^}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with children",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc*{args}/caz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with children regexp",
			routes: []string{"/foo/abc*{args:$^}/bar", "/foo/abc*{args:$^}/caz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with static edge",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc/baz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with static edge regexp",
			routes: []string{"/foo/abc*{args:$^}/bar", "/foo/abc/baz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard and suffix wildcard",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc*{args}"},
			path:   "/foo/abc",
		},
		{
			name:   "infix inflight wildcard and suffix wildcard regexp",
			routes: []string{"/foo/abc*{args:$^}/bar", "/foo/abc*{args:$^}"},
			path:   "/foo/abc",
		},
		{
			name:   "suffix wildcard wildcard with param edge",
			routes: []string{"/foo/*{args}", "/foo/{param}"},
			path:   "/foo/",
		},
		{
			name:   "suffix wildcard wildcard with param edge regexp",
			routes: []string{"/foo/*{args:$^}", "/foo/{param:$^}"},
			path:   "/foo/",
		},
		{
			name:   "suffix inflight wildcard wildcard with param edge",
			routes: []string{"/foo/abc*{args}", "/foo/abc{param}"},
			path:   "/foo/abc",
		},
		{
			name:   "suffix inflight wildcard wildcard with param edge regexp",
			routes: []string{"/foo/abc*{args:$^}", "/foo/abc{param:$^}"},
			path:   "/foo/abc",
		},
		{
			name:   "infix wildcard wildcard with param edge",
			routes: []string{"/foo/*{args}/bar", "/foo/{param}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard wildcard with param edge regexp",
			routes: []string{"/foo/*{args:$^}/bar", "/foo/{param:$^}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix inflight wildcard wildcard with param edge",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc{param}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard wildcard with param edge regexp",
			routes: []string{"/foo/abc*{args:$^}/bar", "/foo/abc{param:$^}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix wildcard wildcard with trailing slash",
			routes: []string{"/foo/*{args}/"},
			path:   "/foo//",
		},
		{
			name:   "infix wildcard wildcard with trailing slash regexp",
			routes: []string{"/foo/*{args:$^}/"},
			path:   "/foo//",
		},
		{
			name:   "infix inflight wildcard wildcard with trailing slash",
			routes: []string{"/foo/abc*{args}/"},
			path:   "/foo/abc/",
		},
		{
			name:   "infix inflight wildcard wildcard with trailing slash regexp",
			routes: []string{"/foo/abc*{args:$^}/"},
			path:   "/foo/abc/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(AllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(MethodGet, rte, emptyHandler)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			idx, n := lookupByPath(tree.patterns, http.MethodGet, tc.path, c, false, 0)
			require.False(t, c.tsr)
			require.Nil(t, n)
			assert.Equal(t, 0, idx)
		})
	}
}

func TestRouteWithParams(t *testing.T) {
	f, _ := New()
	routes := [...]string{
		"/",
		"/cmd/{tool}/{sub}",
		"/cmd/{tool}/",
		"/src/*{filepath}",
		"/search/",
		"/search/{query}",
		"/user_{name}",
		"/user_{name}/about",
		"/files/{dir}/*{filepath}",
		"/doc/",
		"/doc/go_faq.html",
		"/doc/go1.html",
		"/info/{user}/public",
		"/info/{user}/project/{project}",
		"/info/{user}/filepath/+{any}",
	}
	for _, rte := range routes {
		require.NoError(t, onlyError(f.Handle(MethodGet, rte, emptyHandler)))
	}

	tree := f.getTree()
	for _, rte := range routes {
		c := newTestContext(f)
		idx, n := lookupByPath(tree.patterns, http.MethodGet, rte, c, false, 0)
		require.NotNilf(t, n, "route: %s", rte)
		require.NotNilf(t, n.routes[idx], "route: %s", rte)
		assert.False(t, c.tsr)
		assert.Equal(t, rte, n.routes[idx].pattern)
	}
}

func TestRouteParamEmptySegment(t *testing.T) {
	f, _ := New(AllowRegexpParam(true))
	cases := []struct {
		name  string
		route string
		path  string
	}{
		{
			name:  "empty segment",
			route: "/cmd/{tool}/{sub}",
			path:  "/cmd//sub",
		},
		{
			name:  "empty segment regexp",
			route: "/cmd/{tool:bar}/{sub}",
			path:  "/cmd//sub",
		},
		{
			name:  "empty inflight end of route",
			route: "/command/exec:{tool}",
			path:  "/command/exec:",
		},
		{
			name:  "empty inflight end of route regexp",
			route: "/command/exec:{tool:bar}",
			path:  "/command/exec:",
		},
		{
			name:  "empty inflight segment",
			route: "/command/exec:{tool}/id",
			path:  "/command/exec:/id",
		},
		{
			name:  "empty inflight segment regexp",
			route: "/command/exec:{tool:bar}/id",
			path:  "/command/exec:/id",
		},
	}

	for _, tc := range cases {
		require.NoError(t, onlyError(f.Handle(MethodGet, tc.route, emptyHandler)))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := f.getTree()
			c := newTestContext(f)
			idx, n := lookupByPath(tree.patterns, http.MethodGet, tc.path, c, false, 0)
			assert.Nil(t, n)
			assert.Equal(t, 0, idx)
			assert.Empty(t, slices.Collect(c.Params()))
			assert.False(t, c.tsr)
		})
	}
}

func TestOverlappingRoute(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		routes     []string
		wantMatch  string
		wantParams Params
	}{
		{
			name: "basic test most specific",
			path: "/products/new",
			routes: []string{
				"/products/{id}",
				"/products/new",
			},
			wantMatch: "/products/new",
		},
		{
			name: "basic test most specific with regexp param",
			path: "/products/new",
			routes: []string{
				"/products/{id:[0-9]+}",
				"/products/new",
			},
			wantMatch: "/products/new",
		},
		{
			name: "basic test less specific",
			path: "/products/123",
			routes: []string{
				"/products/{id}",
				"/products/new",
			},
			wantMatch:  "/products/{id}",
			wantParams: Params{{Key: "id", Value: "123"}},
		},
		{
			name: "basic test less specific with regexp priority",
			path: "/products/123",
			routes: []string{
				"/products/{name}",
				"/products/{id:[0-9]+}",
				"/products/new",
			},
			wantMatch:  "/products/{id:[0-9]+}",
			wantParams: Params{{Key: "id", Value: "123"}},
		},
		{
			name: "basic test less specific with regexp and less specific",
			path: "/products/abc",
			routes: []string{
				"/products/{name}",
				"/products/{id:[0-9]+}",
				"/products/new",
			},
			wantMatch:  "/products/{name}",
			wantParams: Params{{Key: "name", Value: "abc"}},
		},
		{
			name: "ieof+backtrack to {id} wildcard while deleting {a}",
			path: "/base/val1/123/new/barr",
			routes: []string{
				"/{base}/val1/{id}",
				"/{base}/val1/123/{a}/bar",
				"/{base}/val1/{id}/new/{name}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id}/new/{name}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "id",
					Value: "123",
				},
				{
					Key:   "name",
					Value: "barr",
				},
			},
		},
		{
			name: "backtrack to {id} wildcard while deleting {a} with regexp constraint",
			path: "/base/val1/123/new/barr",
			routes: []string{
				"/{base}/val1/{id:[0-9]+}",
				"/{base}/val1/123/{a:[A-z]+}/{id:[0-9]+}",
				"/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "id",
					Value: "123",
				},
				{
					Key:   "name",
					Value: "barr",
				},
			},
		},
		{
			name: "kme+backtrack to {id} wildcard while deleting {a}",
			path: "/base/val1/123/new/ba",
			routes: []string{
				"/{base}/val1/{id}",
				"/{base}/val1/123/{a}/bar",
				"/{base}/val1/{id}/new/{name}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id}/new/{name}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "id",
					Value: "123",
				},
				{
					Key:   "name",
					Value: "ba",
				},
			},
		},
		{
			name: "kme+backtrack to {id} wildcard while deleting {a} with regexp constraint",
			path: "/base/val1/123/new/ba",
			routes: []string{
				"/{base}/val1/{id:[0-9]+}",
				"/{base}/val1/123/{a:[A-z]+}/bar",
				"/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "id",
					Value: "123",
				},
				{
					Key:   "name",
					Value: "ba",
				},
			},
		},
		{
			name: "ime+backtrack to {id} wildcard while deleting {a}",
			path: "/base/val1/123/new/bx",
			routes: []string{
				"/{base}/val1/{id}",
				"/{base}/val1/123/{a}/bar",
				"/{base}/val1/{id}/new/{name}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id}/new/{name}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "id",
					Value: "123",
				},
				{
					Key:   "name",
					Value: "bx",
				},
			},
		},
		{
			name: "ime+backtrack to {id} wildcard while deleting {a} with regex constraint",
			path: "/base/val1/123/new/bx",
			routes: []string{
				"/{base}/val1/{id:[0-9]+}",
				"/{base}/val1/123/{a:[A-z]+}/bar",
				"/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "id",
					Value: "123",
				},
				{
					Key:   "name",
					Value: "bx",
				},
			},
		},
		{
			name: "backtrack to catch while deleting {a}, {id} and {name}",
			path: "/base/val1/123/new/bar/",
			routes: []string{
				"/{base}/val1/{id}",
				"/{base}/val1/123/{a}/bar",
				"/{base}/val1/{id}/new/{name}",
				"/{base}/val*{all}",
			},
			wantMatch: "/{base}/val*{all}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "all",
					Value: "1/123/new/bar/",
				},
			},
		},
		{
			name: "backtrack to catch while deleting {a}, {id} and {name} with regexp constraint",
			path: "/base/val1/123/new/bar/",
			routes: []string{
				"/{base}/val1/{id:[0-9]+}",
				"/{base}/val1/123/{a:[A-z]+}/bar",
				"/{base}/val1/{id:[0-9]+}/new/{name:ba}",
				"/{base}/val*{all}",
			},
			wantMatch: "/{base}/val*{all}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "all",
					Value: "1/123/new/bar/",
				},
			},
		},
		{
			name: "notleaf+backtrack to catch while deleting {a}, {id}",
			path: "/base/val1/123/new",
			routes: []string{
				"/{base}/val1/123/{a}/baz",
				"/{base}/val1/123/{a}/bar",
				"/{base}/val1/{id}/new/{name}",
				"/{base}/val*{all}",
			},
			wantMatch: "/{base}/val*{all}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "all",
					Value: "1/123/new",
				},
			},
		},
		{
			name: "multi node most specific",
			path: "/foo/1/2/3/bar",
			routes: []string{
				"/foo/{ab}",
				"/foo/{ab}/{bc}",
				"/foo/{ab}/{bc}/{de}",
				"/foo/{ab}/{bc}/{de}/bar",
				"/foo/{ab}/{bc}/{de}/{fg}",
			},
			wantMatch: "/foo/{ab}/{bc}/{de}/bar",
			wantParams: Params{
				{
					Key:   "ab",
					Value: "1",
				},
				{
					Key:   "bc",
					Value: "2",
				},
				{
					Key:   "de",
					Value: "3",
				},
			},
		},
		{
			name: "multi node most specific with regexp",
			path: "/foo/1/2/3/bar",
			routes: []string{
				"/foo/{ab:[0-9]+}",
				"/foo/{ab:[0-9]}/{bc:[0-9]+}",
				"/foo/{ab:.*}/{bc:[0-9]}/{de:[0-9]+}",
				"/foo/{ab:1}/{bc:2}/{de:3}/bar",
				"/foo/{ab}/{bc}/{de}/{fg}",
			},
			wantMatch: "/foo/{ab:1}/{bc:2}/{de:3}/bar",
			wantParams: Params{
				{
					Key:   "ab",
					Value: "1",
				},
				{
					Key:   "bc",
					Value: "2",
				},
				{
					Key:   "de",
					Value: "3",
				},
			},
		},
		{
			name: "multi node most specific with multi name",
			path: "/foo/1/2/3/bar",
			routes: []string{
				"/foo/{aa}",
				"/foo/{bb}/{cc}",
				"/foo/{dd}/{ee}/{ff}",
				"/foo/{gg}/{hh}/{ii}/bar",
				"/foo/{jj}/{kk}/{ll}/{mm}",
			},
			wantMatch: "/foo/{gg}/{hh}/{ii}/bar",
			wantParams: Params{
				{
					Key:   "gg",
					Value: "1",
				},
				{
					Key:   "hh",
					Value: "2",
				},
				{
					Key:   "ii",
					Value: "3",
				},
			},
		},
		{
			name: "multi node most specific with regexp and multi name",
			path: "/foo/1/2/3/bar",
			routes: []string{
				"/foo/{aa:[0-9]+}",
				"/foo/{bb:[0-9]}/{cc:[0-9]+}",
				"/foo/{dd:.*}/{ee:[0-9]}/{ff:[0-9]+}",
				"/foo/{gg:1}/{hh:2}/{ii:3}/bar",
				"/foo/{jj}/{kk}/{ll}/{mm}",
			},
			wantMatch: "/foo/{gg:1}/{hh:2}/{ii:3}/bar",
			wantParams: Params{
				{
					Key:   "gg",
					Value: "1",
				},
				{
					Key:   "hh",
					Value: "2",
				},
				{
					Key:   "ii",
					Value: "3",
				},
			},
		},
		{
			name: "multi node less specific",
			path: "/foo/1/2/3/john",
			routes: []string{
				"/foo/{ab}",
				"/foo/{ab}/{bc}",
				"/foo/{ab}/{bc}/{de}",
				"/foo/{ab}/{bc}/{de}/bar",
				"/foo/{ab}/{bc}/{de}/{fg}",
			},
			wantMatch: "/foo/{ab}/{bc}/{de}/{fg}",
			wantParams: Params{
				{
					Key:   "ab",
					Value: "1",
				},
				{
					Key:   "bc",
					Value: "2",
				},
				{
					Key:   "de",
					Value: "3",
				},
				{
					Key:   "fg",
					Value: "john",
				},
			},
		},
		{
			name: "multi node less specific with regexp",
			path: "/foo/1/2/3/john",
			routes: []string{
				"/foo/{ab:[0-9]+}",
				"/foo/{ab:[0-9]}/{bc:[0-9]+}",
				"/foo/{ab:.*}/{bc:[0-9]}/{de:[0-9]+}",
				"/foo/{ab:1}/{bc:2}/{de:3}/bar",
				"/foo/{ab}/{bc}/{de}/{fg}",
			},
			wantMatch: "/foo/{ab}/{bc}/{de}/{fg}",
			wantParams: Params{
				{
					Key:   "ab",
					Value: "1",
				},
				{
					Key:   "bc",
					Value: "2",
				},
				{
					Key:   "de",
					Value: "3",
				},
				{
					Key:   "fg",
					Value: "john",
				},
			},
		},
		{
			name: "multi node less specific with multi name",
			path: "/foo/1/2/3/john",
			routes: []string{
				"/foo/{aa}",
				"/foo/{bb}/{cc}",
				"/foo/{dd}/{ee}/{ff}",
				"/foo/{gg}/{hh}/{ii}/bar",
				"/foo/{jj}/{kk}/{ll}/{mm}",
			},
			wantMatch: "/foo/{jj}/{kk}/{ll}/{mm}",
			wantParams: Params{
				{
					Key:   "jj",
					Value: "1",
				},
				{
					Key:   "kk",
					Value: "2",
				},
				{
					Key:   "ll",
					Value: "3",
				},
				{
					Key:   "mm",
					Value: "john",
				},
			},
		},
		{
			name: "backtrack on empty mid key parameter",
			path: "/foo/abc/bar",
			routes: []string{
				"/foo/abc{id}/bar",
				"/foo/{name}/bar",
			},
			wantMatch: "/foo/{name}/bar",
			wantParams: Params{
				{
					Key:   "name",
					Value: "abc",
				},
			},
		},
		{
			name: "backtrack on empty mid key parameter with regexp",
			path: "/foo/abc/bar",
			routes: []string{
				"/foo/abc{id:.*}/bar",
				"/foo/{name:.*}/bar",
			},
			wantMatch: "/foo/{name:.*}/bar",
			wantParams: Params{
				{
					Key:   "name",
					Value: "abc",
				},
			},
		},
		{
			name: "most specific param between catch all",
			path: "/foo/123",
			routes: []string{
				"/foo/{id}",
				"/foo/a*{args}",
				"/foo*{args}",
			},
			wantMatch: "/foo/{id}",
			wantParams: Params{
				{
					Key:   "id",
					Value: "123",
				},
			},
		},
		{
			name: "most specific param between catch all with regexp",
			path: "/foo/123",
			routes: []string{
				"/foo/{id:.*}",
				"/foo/a*{args:.*}",
				"/foo*{args:.*}",
			},
			wantMatch: "/foo/{id:.*}",
			wantParams: Params{
				{
					Key:   "id",
					Value: "123",
				},
			},
		},
		{
			name: "most specific catch all with param",
			path: "/foo/abc",
			routes: []string{
				"/foo/{id}",
				"/foo/a*{args}",
				"/foo*{args}",
			},
			wantMatch: "/foo/a*{args}",
			wantParams: Params{
				{
					Key:   "args",
					Value: "bc",
				},
			},
		},
		{
			name: "most specific catch all with param and regexp",
			path: "/foo/abc",
			routes: []string{
				"/foo/{id:.*}",
				"/foo/a*{args:.*}",
				"/foo*{args:.*}",
			},
			wantMatch: "/foo/a*{args:.*}",
			wantParams: Params{
				{
					Key:   "args",
					Value: "bc",
				},
			},
		},
		{
			name: "named parameter priority over catch-all",
			path: "/foo/abc",
			routes: []string{
				"/foo/{id}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id}",
			wantParams: Params{
				{
					Key:   "id",
					Value: "abc",
				},
			},
		},
		{
			name: "static priority over named parameter and catch-all",
			path: "/foo/abc",
			routes: []string{
				"/foo/abc",
				"/foo/{id}",
				"/foo/*{args}",
			},
			wantMatch:  "/foo/abc",
			wantParams: Params{},
		},
		{
			name: "no match static with named parameter fallback",
			path: "/foo/abd",
			routes: []string{
				"/foo/abc",
				"/foo/{id}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id}",
			wantParams: Params{
				{
					Key:   "id",
					Value: "abd",
				},
			},
		},
		{
			name: "no match static with catch all fallback",
			path: "/foo/abc/foo",
			routes: []string{
				"/foo/abc",
				"/foo/{id}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/*{args}",
			wantParams: Params{
				{
					Key:   "args",
					Value: "abc/foo",
				},
			},
		},
		{
			name: "most specific catch all with static",
			path: "/foo/bar/abd",
			routes: []string{
				"/foo/{id}/abc",
				"/foo/{id}/*{args}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id}/*{args}",
			wantParams: Params{
				{
					Key:   "id",
					Value: "bar",
				},
				{
					Key:   "args",
					Value: "abd",
				},
			},
		},
		{
			name: "most specific catch all with static and named parameter",
			path: "/foo/bar/abc/def",
			routes: []string{
				"/foo/{id_1}/abc",
				"/foo/{id_1}/{id_2}",
				"/foo/{id_1}/*{args}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id_1}/*{args}",
			wantParams: Params{
				{
					Key:   "id_1",
					Value: "bar",
				},
				{
					Key:   "args",
					Value: "abc/def",
				},
			},
		},
		{
			name: "backtrack to most specific named parameter with 2 skipped catch all",
			path: "/foo/bar/def",
			routes: []string{
				"/foo/{id_1}/abc",
				"/foo/{id_1}/*{args}",
				"/foo/{id_1}/{id_2}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id_1}/{id_2}",
			wantParams: Params{
				{
					Key:   "id_1",
					Value: "bar",
				},
				{
					Key:   "id_2",
					Value: "def",
				},
			},
		},
		{
			name: "backtrack to most specific catch-all with an exact match",
			path: "/foo/bar/x/y/z",
			routes: []string{
				"/foo/{id_1}/abc",
				"/foo/{id_1}/*{args}",
				"/foo/{id_1}/{id_2}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id_1}/*{args}",
			wantParams: Params{
				{
					Key:   "id_1",
					Value: "bar",
				},
				{
					Key:   "args",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "backtrack to most specific catch-all with an exact match",
			path: "/foo/bar/",
			routes: []string{
				"/foo/{id_1}/abc",
				"/foo/{id_1}/*{args}",
				"/foo/{id_1}/{id_2}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/*{args}",
			wantParams: Params{
				{
					Key:   "args",
					Value: "bar/",
				},
			},
		},
		{
			name: "param at index 1 with 2 nodes",
			path: "/foo/[barr]",
			routes: []string{
				"/foo/{bar}",
				"/foo/[bar]",
			},
			wantMatch: "/foo/{bar}",
			wantParams: Params{
				{
					Key:   "bar",
					Value: "[barr]",
				},
			},
		},
		{
			name: "param at index 1 with 3 nodes",
			path: "/foo/|barr|",
			routes: []string{
				"/foo/{bar}",
				"/foo/[bar]",
				"/foo/|bar|",
			},
			wantMatch: "/foo/{bar}",
			wantParams: Params{
				{
					Key:   "bar",
					Value: "|barr|",
				},
			},
		},
		{
			name: "param at index 0 with 3 nodes",
			path: "/foo/~barr~",
			routes: []string{
				"/foo/{bar}",
				"/foo/~bar~",
				"/foo/|bar|",
			},
			wantMatch: "/foo/{bar}",
			wantParams: Params{
				{
					Key:   "bar",
					Value: "~barr~",
				},
			},
		},
		{
			name: "regexp param priority in register order",
			path: "/foo/123",
			routes: []string{
				"/foo/{fallback}",
				"/foo/{a:[0-9]+}",
				"/foo/{b:[0-9-A-z]+}",
				"/foo/{c:[0-9-A-Z]+}",
			},
			wantMatch: "/foo/{a:[0-9]+}",
			wantParams: Params{
				{
					Key:   "a",
					Value: "123",
				},
			},
		},
		{
			name: "regexp param priority in register order, with last match",
			path: "/foo/abc",
			routes: []string{
				"/foo/{fallback}",
				"/foo/{a:[0-9]+}",
				"/foo/{b:[0-9-A-Z]+}",
				"/foo/{c:[0-9-A-z]+}",
			},
			wantMatch: "/foo/{c:[0-9-A-z]+}",
			wantParams: Params{
				{
					Key:   "c",
					Value: "abc",
				},
			},
		},
		{
			name: "regexp param priority with fallback",
			path: "/foo/*",
			routes: []string{
				"/foo/{fallback}",
				"/foo/{a:[0-9]+}",
				"/foo/{b:[0-9-A-Z]+}",
				"/foo/{c:[0-9-A-z]+}",
			},
			wantMatch: "/foo/{fallback}",
			wantParams: Params{
				{
					Key:   "fallback",
					Value: "*",
				},
			},
		},
		{
			name: "regexp wildcard priority in register order",
			path: "/foo/123",
			routes: []string{
				"/foo/*{fallback}",
				"/foo/*{a:[0-9]+}",
				"/foo/*{b:[0-9-A-z]+}",
				"/foo/*{c:[0-9-A-Z]+}",
			},
			wantMatch: "/foo/*{a:[0-9]+}",
			wantParams: Params{
				{
					Key:   "a",
					Value: "123",
				},
			},
		},
		{
			name: "regexp wildcard priority in register order, with last match",
			path: "/foo/abc",
			routes: []string{
				"/foo/*{fallback}",
				"/foo/*{a:[0-9]+}",
				"/foo/*{b:[0-9-A-Z]+}",
				"/foo/*{c:[0-9-A-z]+}",
			},
			wantMatch: "/foo/*{c:[0-9-A-z]+}",
			wantParams: Params{
				{
					Key:   "c",
					Value: "abc",
				},
			},
		},
		{
			name: "regexp wildcard priority with fallback",
			path: "/foo/*",
			routes: []string{
				"/foo/*{fallback}",
				"/foo/*{a:[0-9]+}",
				"/foo/*{b:[0-9-A-Z]+}",
				"/foo/*{c:[0-9-A-z]+}",
			},
			wantMatch: "/foo/*{fallback}",
			wantParams: Params{
				{
					Key:   "fallback",
					Value: "*",
				},
			},
		},
		{
			name: "regexp infix wildcard priority with fallback",
			path: "/foo/a/b/c/bar",
			routes: []string{
				"/foo/*{fallback}/bar",
				"/foo/*{a:[0-9]+}/bar",
				"/foo/*{b:[0-9-A-Z]+}/bar",
				"/foo/*{c:[0-9-A-z]+}/bar",
			},
			wantMatch: "/foo/*{fallback}/bar",
			wantParams: Params{
				{
					Key:   "fallback",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "mixing all together 1",
			path: "/foo/1/2/3/bar/foo",
			routes: []string{
				"/foo/*{fallback}/bar/*{ab:[0-9]+}",
				"/foo/*{a:.*}/bar/{b:[A-z]+}",
				"/foo/*{b}/bar/{foo}",
				"/foo/*{c:[0-9/]+}/bar/foo",
			},
			wantMatch: "/foo/*{a:.*}/bar/{b:[A-z]+}",
			wantParams: Params{
				{
					Key:   "a",
					Value: "1/2/3",
				},
				{
					Key:   "b",
					Value: "foo",
				},
			},
		},
		{
			name: "mixing all together 2",
			path: "/foo/1/2/3/bar/foo123",
			routes: []string{
				"/foo/*{fallback}/bar/*{ab:[0-9]+}",
				"/foo/*{a:.*}/bar/{b:[A-z]+}",
				"/foo/*{b}/bar/{foo}",
				"/foo/*{c:[0-9/]+}/bar/foo",
			},
			wantMatch: "/foo/*{b}/bar/{foo}",
			wantParams: Params{
				{
					Key:   "b",
					Value: "1/2/3",
				},
				{
					Key:   "foo",
					Value: "foo123",
				},
			},
		},
		{
			name: "mixing all together 3",
			path: "/foo/1/2/3/bar/foo",
			routes: []string{
				"/foo/*{fallback}/bar/*{ab:[0-9]+}",
				"/foo/*{a:.*}/bar/{b:[A-z]+}",
				"/foo/*{b}/bar/{foo}",
				"/foo/*{c:[0-9/]+}/bar/foo",
			},
			wantMatch: "/foo/*{a:.*}/bar/{b:[A-z]+}",
			wantParams: Params{
				{
					Key:   "a",
					Value: "1/2/3",
				},
				{
					Key:   "b",
					Value: "foo",
				},
			},
		},
		{
			name: "mixing all together 4",
			path: "/foo/1/2/3/bar/foo/1/2/3",
			routes: []string{
				"/foo/*{fallback}/bar/*{ab:[A-z0-9/]+}",
				"/foo/*{a:.*}/bar/{b:[A-z]+}",
				"/foo/*{b}/bar/{foo}",
				"/foo/*{c:[0-9/]+}/bar/foo",
			},
			wantMatch: "/foo/*{fallback}/bar/*{ab:[A-z0-9/]+}",
			wantParams: Params{
				{
					Key:   "fallback",
					Value: "1/2/3",
				},
				{
					Key:   "ab",
					Value: "foo/1/2/3",
				},
			},
		},
		{
			name: "exhausting infix with suffix fallback at first position",
			path: "/aa/b/c",
			routes: []string{
				"/*{args:.*}",
				"/*{a:a}/b/c",
				"/*{b:b}/b/c",
			},
			wantMatch: "/*{args:.*}",
			wantParams: Params{
				{
					Key:   "args",
					Value: "aa/b/c",
				},
			},
		},
		{
			name: "exhausting infix with suffix fallback at last position",
			path: "/aa/b/c",
			routes: []string{
				"/*{a:a}/b/c",
				"/*{b:b}/b/c",
				"/*{args}",
			},
			wantMatch: "/*{args}",
			wantParams: Params{
				{
					Key:   "args",
					Value: "aa/b/c",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(AllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(MethodGet, rte, emptyHandler)))
			}

			tree := f.getTree()

			c := newTestContext(f)
			idx, n := lookupByPath(tree.patterns, http.MethodGet, tc.path, c, false, 0)
			require.NotNil(t, n)
			require.NotNil(t, n.routes[idx])
			assert.False(t, c.tsr)
			assert.Equal(t, tc.wantMatch, n.routes[idx].pattern)
			c.route = n.routes[idx]
			*c.paramsKeys = c.route.params
			if len(tc.wantParams) == 0 {
				assert.Empty(t, slices.Collect(c.Params()))
			} else {
				var params Params = slices.Collect(c.Params())
				assert.Equal(t, tc.wantParams, params)
			}

			// Test with lazy
			c = newTestContext(f)
			idx, n = lookupByPath(tree.patterns, http.MethodGet, tc.path, c, true, 0)
			require.NotNil(t, n)
			require.NotNil(t, n.routes[idx])
			assert.False(t, c.tsr)
			c.route = n.routes[idx]
			assert.Empty(t, slices.Collect(c.Params()))
			assert.Equal(t, tc.wantMatch, n.routes[idx].pattern)
		})
	}
}

func TestInfixWildcard(t *testing.T) {
	cases := []struct {
		name       string
		routes     []string
		path       string
		wantPath   string
		wantTsr    bool
		wantParams []Param
	}{
		{
			name:     "simple infix wildcard",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo/a/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix wildcard with regexp",
			routes:   []string{"/foo/*{args:a}/bar"},
			path:     "/foo/a/bar",
			wantPath: "/foo/*{args:a}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix wildcard capture slash",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "simple infix wildcard capture slash with regexp",
			routes:   []string{"/foo/*{args:/}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/*{args:/}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "simple infix wildcard capture anything not empty",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo//a//bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/a/",
				},
			},
		},
		{
			name:     "simple infix wildcard capture anything not empty with regexp",
			routes:   []string{"/foo/*{args:/a/}/bar"},
			path:     "/foo//a//bar",
			wantPath: "/foo/*{args:/a/}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/a/",
				},
			},
		},
		{
			name:     "static with infix wildcard child",
			routes:   []string{"/foo/", "/foo/*{args}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/*{args}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "bar",
				},
			},
		},
		{
			name:     "static with infix wildcard regexp child",
			routes:   []string{"/foo/", "/foo/*{args:[A-z]+}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/*{args:[A-z]+}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "bar",
				},
			},
		},
		{
			name:     "static with 2 infix wildcard and regexp child",
			routes:   []string{"/foo/", "/foo/*{args}/baz", "/foo/*{args:[A-z]+}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/*{args:[A-z]+}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "bar",
				},
			},
		},
		{
			name:     "static with infix wildcard child capture slash",
			routes:   []string{"/foo/", "/foo/*{args}/baz"},
			path:     "/foo///baz",
			wantPath: "/foo/*{args}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "static with infix wildcard regexp child capture slash after fallback",
			routes:   []string{"/foo/", "/foo/*{args}/baz", "/foo/*{args:a}/baz"},
			path:     "/foo///baz",
			wantPath: "/foo/*{args}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "simple infix wildcard with route char",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo/*{args}/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args}",
				},
			},
		},
		{
			name:     "simple infix wildcard regexp with route char",
			routes:   []string{"/foo/*{args:.*}/bar"},
			path:     "/foo/*{args:.*}/bar",
			wantPath: "/foo/*{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args:.*}",
				},
			},
		},
		{
			name:     "simple infix wildcard with multi segment and route char",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo/*{args}/b/c/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args}/b/c",
				},
			},
		},
		{
			name:     "simple infix wildcard regexp with multi segment and route char",
			routes:   []string{"/foo/*{args:.*}/bar"},
			path:     "/foo/*{args:.*}/b/c/bar",
			wantPath: "/foo/*{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args:.*}/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/za/bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix regexp inflight wildcard",
			routes:   []string{"/foo/z*{args:a}/bar"},
			path:     "/foo/za/bar",
			wantPath: "/foo/z*{args:a}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard capture slash",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/z//bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "simple infix regexp inflight wildcard capture slash",
			routes:   []string{"/foo/z*{args:/}/bar"},
			path:     "/foo/z//bar",
			wantPath: "/foo/z*{args:/}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with route char",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/z*{args}/bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args}",
				},
			},
		},
		{
			name:     "simple infix regexp inflight wildcard with route char",
			routes:   []string{"/foo/z*{args:.*}/bar"},
			path:     "/foo/z*{args:.*}/bar",
			wantPath: "/foo/z*{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args:.*}",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with multi segment",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/za/b/c/bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard regexp with multi segment",
			routes:   []string{"/foo/z*{args:[A-z/]+}/bar"},
			path:     "/foo/za/b/c/bar",
			wantPath: "/foo/z*{args:[A-z/]+}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard regexp with multi segment",
			routes:   []string{"/foo/z*{args:a/b/c}/bar"},
			path:     "/foo/za/b/c/bar",
			wantPath: "/foo/z*{args:a/b/c}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with multi slash",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/z////bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "///",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard regexp with multi slash",
			routes:   []string{"/foo/z*{args:///}/bar"},
			path:     "/foo/z////bar",
			wantPath: "/foo/z*{args:///}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "///",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with multi segment and route char",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/z*{args}/b/c/bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args}/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard regexp with multi segment and route char",
			routes:   []string{"/foo/z*{args:.*}/bar"},
			path:     "/foo/z*{args}/b/c/bar",
			wantPath: "/foo/z*{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args}/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard long",
			routes:   []string{"/foo/xyz*{args}/bar"},
			path:     "/foo/xyza/bar",
			wantPath: "/foo/xyz*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with multi segment long",
			routes:   []string{"/foo/xyz*{args}/bar"},
			path:     "/foo/xyza/b/c/bar",
			wantPath: "/foo/xyz*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "overlapping infix and suffix wildcard match infix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar"},
			path:     "/foo/a/b/c/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "overlapping infix and suffix regexp wildcard match infix",
			routes:   []string{"/foo/*{args:.*}", "/foo/*{args:.*}/bar"},
			path:     "/foo/a/b/c/bar",
			wantPath: "/foo/*{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "overlapping infix and suffix wildcard match infix with slash",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "overlapping infix and suffix regexp wildcard match infix with slash",
			routes:   []string{"/foo/*{args:/}", "/foo/*{args:/}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/*{args:/}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "overlapping infix and suffix wildcard match suffix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar"},
			path:     "/foo/a/b/c/baz",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c/baz",
				},
			},
		},
		{
			name:     "overlapping infix and suffix regexp wildcard match suffix",
			routes:   []string{"/foo/*{args:.*}", "/foo/*{args:.*}/bar"},
			path:     "/foo/a/b/c/baz",
			wantPath: "/foo/*{args:.*}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c/baz",
				},
			},
		},
		{
			name:     "overlapping infix and suffix wildcard match suffix with empty slash",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar"},
			path:     "/foo///baz",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "//baz",
				},
			},
		},
		{
			name:     "overlapping infix and suffix regexp wildcard match suffix with empty slash",
			routes:   []string{"/foo/*{args:.*}", "/foo/*{args:.*}/bar"},
			path:     "/foo///baz",
			wantPath: "/foo/*{args:.*}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "//baz",
				},
			},
		},
		{
			name:     "overlapping infix and suffix wildcard match suffix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar"},
			path:     "/foo/a/b/c/barito",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c/barito",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match infix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo/a/b/c/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match infix with empty slash",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match suffix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo/a/b/c/bili",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c/bili",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match infix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}"},
			path:     "/foo/a/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match param",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo/a/bar",
			wantPath: "/foo/{ps}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match suffix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}"},
			path:     "/foo/a/bili",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/bili",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match param",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}"},
			path:     "/foo/a",
			wantPath: "/foo/{ps}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match param with ts",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/"},
			path:     "/foo/a/",
			wantPath: "/foo/{ps}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match suffix without ts",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/"},
			path:     "/foo/a",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match suffix without ts",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}"},
			path:     "/foo/a/",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/",
				},
			},
		},
		{
			name:     "overlapping infix inflight suffix wildcard and param match param",
			routes:   []string{"/foo/123*{args}", "/foo/123*{args}/bar", "/foo/123{ps}/bar"},
			path:     "/foo/123a/bar",
			wantPath: "/foo/123{ps}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix inflight suffix wildcard and param match suffix",
			routes:   []string{"/foo/123*{args}", "/foo/123*{args}/bar", "/foo/123{ps}"},
			path:     "/foo/123a/bili",
			wantPath: "/foo/123*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/bili",
				},
			},
		},
		{
			name:     "overlapping infix inflight suffix wildcard and param match param",
			routes:   []string{"/foo/123*{args}", "/foo/123*{args}/bar", "/foo/123{ps}"},
			path:     "/foo/123a",
			wantPath: "/foo/123{ps}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name:     "infix segment followed by param",
			routes:   []string{"/foo/*{a}/{b}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/*{a}/{b}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a/b/c",
				},
				{
					Key:   "b",
					Value: "d",
				},
			},
		},
		{
			name:     "infix segment followed by two params",
			routes:   []string{"/foo/*{a}/{b}/{c}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/*{a}/{b}/{c}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a/b",
				},
				{
					Key:   "b",
					Value: "c",
				},
				{
					Key:   "c",
					Value: "d",
				},
			},
		},
		{
			name:     "infix segment followed by one param and one wildcard",
			routes:   []string{"/foo/*{a}/{b}/*{c}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/*{a}/{b}/*{c}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
				{
					Key:   "b",
					Value: "b",
				},
				{
					Key:   "c",
					Value: "c/d",
				},
			},
		},
		{
			name:     "param followed by suffix wildcard",
			routes:   []string{"/foo/{a}/*{b}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/{a}/*{b}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
				{
					Key:   "b",
					Value: "b/c/d",
				},
			},
		},
		{
			name:     "infix inflight segment followed by param",
			routes:   []string{"/foo/123*{a}/{b}"},
			path:     "/foo/123a/b/c/d",
			wantPath: "/foo/123*{a}/{b}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a/b/c",
				},
				{
					Key:   "b",
					Value: "d",
				},
			},
		},
		{
			name:     "inflight param followed by suffix wildcard",
			routes:   []string{"/foo/123{a}/*{b}"},
			path:     "/foo/123a/b/c/d",
			wantPath: "/foo/123{a}/*{b}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
				{
					Key:   "b",
					Value: "b/c/d",
				},
			},
		},
		{
			name:     "multi infix segment simple",
			routes:   []string{"/foo/*{$1}/bar/*{$2}/baz"},
			path:     "/foo/a/bar/b/c/d/baz",
			wantPath: "/foo/*{$1}/bar/*{$2}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "a",
				},
				{
					Key:   "$2",
					Value: "b/c/d",
				},
			},
		},
		{
			name:     "multi inflight segment simple",
			routes:   []string{"/foo/123*{$1}/bar/456*{$2}/baz"},
			path:     "/foo/123a/bar/456b/c/d/baz",
			wantPath: "/foo/123*{$1}/bar/456*{$2}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "a",
				},
				{
					Key:   "$2",
					Value: "b/c/d",
				},
			},
		},
		{
			name:     "static priority",
			routes:   []string{"/foo/bar/baz", "/foo/{ps}/baz", "/foo/*{any}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/bar/baz",
			wantTsr:  false,
		},
		{
			name:     "param priority",
			routes:   []string{"/foo/bar/baz", "/foo/{ps}/baz", "/foo/*{any}/baz"},
			path:     "/foo/buzz/baz",
			wantPath: "/foo/{ps}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "buzz",
				},
			},
		},
		{
			name:     "fallback catch all",
			routes:   []string{"/foo/bar/baz", "/foo/{ps}/baz", "/foo/*{any}/baz"},
			path:     "/foo/a/b/baz",
			wantPath: "/foo/*{any}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b",
				},
			},
		},
		{
			name: "complex overlapping route with static priority",
			routes: []string{
				"/foo/bar/baz/{$1}/jo",
				"/foo/*{any}/baz/{$1}/jo",
				"/foo/{ps}/baz/{$1}/jo",
			},
			path:     "/foo/bar/baz/1/jo",
			wantPath: "/foo/bar/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "complex overlapping route with param priority",
			routes: []string{
				"/foo/bar/baz/{$1}/jo",
				"/foo/*{any}/baz/{$1}/jo",
				"/foo/{ps}/baz/{$1}/jo",
			},
			path:     "/foo/bam/baz/1/jo",
			wantPath: "/foo/{ps}/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "bam",
				},
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "complex overlapping route with catch all fallback",
			routes: []string{
				"/foo/bar/baz/{$1}/jo",
				"/foo/*{any}/baz/{$1}/jo",
				"/foo/{ps}/baz/{$1}/jo",
			},
			path:     "/foo/a/b/c/baz/1/jo",
			wantPath: "/foo/*{any}/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "complex overlapping route with catch all fallback",
			routes: []string{
				"/foo/bar/baz/{$1}/jo",
				"/foo/*{any}/baz/{$1}/john",
				"/foo/{ps}/baz/{$1}/johnny",
			},
			path:     "/foo/a/baz/1/john",
			wantPath: "/foo/*{any}/baz/{$1}/john",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a",
				},
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "overlapping static and infix",
			routes: []string{
				"/foo/*{any}/baz",
				"/foo/a/b/baz",
			},
			path:     "/foo/a/b/baz",
			wantPath: "/foo/a/b/baz",
			wantTsr:  false,
		},
		{
			name: "overlapping static and infix with catch all fallback",
			routes: []string{
				"/foo/*{any}/baz",
				"/foo/a/b/baz",
			},
			path:     "/foo/a/b/c/baz",
			wantPath: "/foo/*{any}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash",
			routes: []string{
				"/foo/*{any}/",
			},
			path:     "/foo/a/b/c/",
			wantPath: "/foo/*{any}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "overlapping static and infix with most specific",
			routes: []string{
				"/foo/*{any}/{a}/ddd/",
				"/foo/*{any}/bbb/{d}",
			},
			path:     "/foo/a/b/c/bbb/ddd/",
			wantPath: "/foo/*{any}/{a}/ddd/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
				{
					Key:   "a",
					Value: "bbb",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash",
			routes: []string{
				"/foo/*{any}",
				"/foo/*{any}/b/",
				"/foo/*{any}/c/",
			},
			path:     "/foo/x/y/z/",
			wantPath: "/foo/*{any}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z/",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash most specific",
			routes: []string{
				"/foo/*{any}",
				"/foo/*{any}/",
			},
			path:     "/foo/x/y/z/",
			wantPath: "/foo/*{any}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "infix regexp wildcard with trailing slash most specific",
			routes: []string{
				"/foo/*{any:.*}",
				"/foo/*{any:.*}/",
			},
			path:     "/foo/x/y/z/",
			wantPath: "/foo/*{any:.*}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash most specific",
			routes: []string{
				"/foo/*{any}",
				"/foo/*{any}/",
			},
			path:     "/foo/x/y/z",
			wantPath: "/foo/*{any}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "infix regexp wildcard with trailing slash most specific",
			routes: []string{
				"/foo/*{any:.*}",
				"/foo/*{any:.*}/",
			},
			path:     "/foo/x/y/z",
			wantPath: "/foo/*{any:.*}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(AllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(MethodGet, rte, emptyHandler)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			idx, n := lookupByPath(tree.patterns, http.MethodGet, tc.path, c, false, 0)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.routes[idx].pattern)
			assert.Equal(t, tc.wantTsr, c.tsr)
			c.route = n.routes[idx]
			*c.paramsKeys = c.route.params
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}

}

func TestInfixWildcardTsr(t *testing.T) {
	cases := []struct {
		name       string
		routes     []string
		path       string
		wantPath   string
		wantTsr    bool
		wantParams []Param
	}{
		{
			name: "infix wildcard with trailing slash and tsr add",
			routes: []string{
				"/foo/*{any}/",
			},
			path:     "/foo/a/b/c",
			wantPath: "/foo/*{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash and tsr add and empty slash",
			routes: []string{
				"/foo/*{any}/",
			},
			path:     "/foo//a",
			wantPath: "/foo/*{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "/a",
				},
			},
		},
		{
			name: "infix wildcard with tsr but skipped node match",
			routes: []string{
				"/foo/*{any}/",
				"/{x}/a/b/c",
			},
			path:     "/foo/a/b/c",
			wantPath: "/{x}/a/b/c",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "x",
					Value: "foo",
				},
			},
		},
		{
			name: "infix wildcard with tsr but skipped node does not match",
			routes: []string{
				"/foo/*{any}/",
				"/{x}/a/b/x",
			},
			path:     "/foo/a/b/c",
			wantPath: "/foo/*{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash and tsr add",
			routes: []string{
				"/foo/*{any}/",
				"/foo/*{any}/abc",
				"/foo/*{any}/bcd",
			},
			path:     "/foo/a/b/c/abd",
			wantPath: "/foo/*{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c/abd",
				},
			},
		},
		{
			name: "infix wildcard with sub-node tsr add fallback",
			routes: []string{
				"/foo/*{any}/{a}/ddd/",
				"/foo/*{any}/bbb/{d}/foo",
			},
			path:     "/foo/a/b/c/bbb/ddd",
			wantPath: "/foo/*{any}/{a}/ddd/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
				{
					Key:   "a",
					Value: "bbb",
				},
			},
		},
		{
			name: "infix wildcard with sub-node tsr at depth 1 but direct match",
			routes: []string{
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbb",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/*{any}/bbb",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with sub-node tsr at depth 1 and 2 but direct match",
			routes: []string{
				"/foo/*{any}/b/c/bbb/",
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbb",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/*{any}/bbb",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with sub-node tsr at depth 1 and 2 but fallback first tsr",
			routes: []string{
				"/foo/*{any}/b/c/bbb/",
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/*{any}/b/c/bbb/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a",
				},
			},
		},
		{
			name: "infix wildcard with sub-node tsr at depth 1 and 2 but fallback first tsr",
			routes: []string{
				"/foo/*{any}/",
				"/foo/*{any}/b/c/bbb/",
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/*{any}/b/c/bbb/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a",
				},
			},
		},
		{
			name: "infix wildcard with depth 0 tsr and sub-node tsr at depth 1 fallback first tsr",
			routes: []string{
				"/foo/a/b/c/bbb/",
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/a/b/c/bbb/",
			wantTsr:  true,
		},
		{
			name: "infix wildcard with depth 0 tsr and sub-node tsr at depth 1 fallback first tsr",
			routes: []string{
				"/foo/{first}/b/c/bbb/",
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/{first}/b/c/bbb/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "first",
					Value: "a",
				},
			},
		},
		{
			name: "multi infix wildcard with sub-node tsr at depth 1 but direct match",
			routes: []string{
				"/foo/*{any1}/b/c/*{any2}/d/",
				"/foo/*{any1}/c/*{any2}/d",
			},
			path:     "/foo/a/b/c/x/y/z/d",
			wantPath: "/foo/*{any1}/c/*{any2}/d",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any1",
					Value: "a/b",
				},
				{
					Key:   "any2",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "multi infix wildcard with sub-node tsr at depth 1 and fallback first",
			routes: []string{
				"/foo/*{any1}/b/c/*{any2}/d/",
				"/foo/*{any1}/c/*{any2}/x",
			},
			path:     "/foo/a/b/c/x/y/z/d",
			wantPath: "/foo/*{any1}/b/c/*{any2}/d/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any1",
					Value: "a",
				},
				{
					Key:   "any2",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "multi infix wildcard with sub-node tsr and skipped nodes at depth 1 and fallback first",
			routes: []string{
				"/foo/*{any1}/b/c/*{any2}/{a}/",
				"/foo/*{any1}/b/c/*{any2}/d{a}/",
				"/foo/*{any1}/b/c/*{any2}/dd/",
				"/foo/*{any1}/c/*{any2}/x",
			},
			path:     "/foo/a/b/c/x/y/z/dd",
			wantPath: "/foo/*{any1}/b/c/*{any2}/dd/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any1",
					Value: "a",
				},
				{
					Key:   "any2",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "multi infix wildcard with sub-node tsr and skipped nodes at depth 1 and direct match",
			routes: []string{
				"/foo/*{any1}/b/c/*{any2}/{a}/",
				"/foo/*{any1}/b/c/*{any2}/d{a}/",
				"/foo/*{any1}/b/c/*{any2}/dd/",
				"/foo/*{any1}/c/*{any2}/x",
			},
			path:     "/foo/a/b/c/x/y/z/xd/",
			wantPath: "/foo/*{any1}/b/c/*{any2}/{a}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any1",
					Value: "a",
				},
				{
					Key:   "any2",
					Value: "x/y/z",
				},
				{
					Key:   "a",
					Value: "xd",
				},
			},
		},
		{
			name: "multi infix wildcard with sub-node tsr and skipped nodes at depth 1 with direct match depth 0",
			routes: []string{
				"/foo/*{any1}/b/c/*{any2}/{a}/",
				"/foo/*{any1}/b/c/*{any2}/d{a}/",
				"/foo/*{any1}/b/c/*{any2}/dd/",
				"/foo/*{any1}/c/*{any2}/x",
				"/{a}/*{any1}/c/x/y/z/dd",
			},
			path:     "/foo/a/b/c/x/y/z/dd",
			wantPath: "/{a}/*{any1}/c/x/y/z/dd",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "any1",
					Value: "a/b",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(MethodGet, rte, emptyHandler)))
			}

			tree := f.getTree()

			c := newTestContext(f)
			idx, n := lookupByPath(tree.patterns, http.MethodGet, tc.path, c, false, 0)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.routes[idx].pattern)
			assert.Equal(t, tc.wantTsr, c.tsr)
			c.route = n.routes[idx]
			*c.paramsKeys = c.route.params
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}
}

func TestTree_LookupTsr(t *testing.T) {
	cases := []struct {
		name     string
		paths    []string
		key      string
		want     bool
		wantPath string
	}{
		{
			name:     "match mid edge",
			paths:    []string{"/foo/bar/"},
			key:      "/foo/bar",
			want:     true,
			wantPath: "/foo/bar/",
		},
		{
			name:     "incomplete match end of edge",
			paths:    []string{"/foo/bar"},
			key:      "/foo/bar/",
			want:     true,
			wantPath: "/foo/bar",
		},
		{
			name:     "match mid edge with child node",
			paths:    []string{"/users/", "/users/{id}"},
			key:      "/users",
			want:     true,
			wantPath: "/users/",
		},
		{
			name:     "match mid edge in child node",
			paths:    []string{"/users", "/users/{id}"},
			key:      "/users/",
			want:     true,
			wantPath: "/users",
		},
		{
			name:  "match mid edge in child node with parent not leaf",
			paths: []string{"/test/x", "/tests/"},
			key:   "/test/",
		},
		{
			name:  "match mid edge in child node with invalid remaining prefix",
			paths: []string{"/users/{id}"},
			key:   "/users/",
		},
		{
			name:  "match mid edge with child node with invalid remaining suffix",
			paths: []string{"/users/{id}"},
			key:   "/users",
		},
		{
			name:  "match mid edge with ts and more char after",
			paths: []string{"/foo/bar/buzz"},
			key:   "/foo/bar",
		},
		{
			name:  "match mid edge with ts and more char before",
			paths: []string{"/foo/barr/"},
			key:   "/foo/bar",
		},
		{
			name:  "incomplete match end of edge with ts and more char after",
			paths: []string{"/foo/bar"},
			key:   "/foo/bar/buzz",
		},
		{
			name:  "incomplete match end of edge with ts and more char before",
			paths: []string{"/foo/bar"},
			key:   "/foo/barr/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			for _, path := range tc.paths {
				require.NoError(t, onlyError(f.Handle(MethodGet, path, emptyHandler)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			idx, n := lookupByPath(tree.patterns, http.MethodGet, tc.key, c, true, 0)
			assert.Equal(t, tc.want, c.tsr)
			if tc.want {
				require.NotNil(t, n)
				require.NotNil(t, n.routes[idx])
				assert.Equal(t, tc.wantPath, n.routes[idx].pattern)
			}
		})
	}
}

func TestNode_String(t *testing.T) {
	f, _ := New()
	require.NoError(t, onlyError(f.Handle(MethodGet, "/foo/{bar}/*{baz}", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(
		MethodAny, "/foo/bar",
		emptyHandler,
		WithQueryMatcher("a", "b"),
		WithHeaderMatcher("b", "c"),
	)))
	require.NoError(t, onlyError(f.Handle(
		[]string{http.MethodPost, http.MethodDelete}, "/foo/bar",
		emptyHandler,
		WithQueryMatcher("a", "b"),
	)))
	tree := f.getTree()

	want := `root:
      path: /foo/ [params: 1]
          path: bar
                => /foo/bar [methods: DELETE, POST] [matchers: fox.QueryMatcher] [priority: 1]
                => /foo/bar [matchers: fox.QueryMatcher, fox.HeaderMatcher] [priority: 2]
          path: ?
              path: / [wildcards: 1]
                  path: *
                        => /foo/{bar}/*{baz} [methods: GET] [priority: 0]`
	assert.Equal(t, want, strings.TrimSuffix(tree.patterns.String(), "\n"))

	fmt.Println(tree.patterns)
}
