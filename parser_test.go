package fox

import (
	"errors"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"testing"

	"github.com/fox-toolkit/fox/internal/iterutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parsePattern(t *testing.T) {
	f := MustRouter(WithAllowRegexpParam(true))

	staticToken := func(v string, hsplit bool) token {
		return token{
			value:  v,
			typ:    nodeStatic,
			hsplit: hsplit,
		}
	}

	paramToken := func(v, reg string) token {
		tk := token{
			value: v,
			typ:   nodeParam,
		}
		if reg != "" {
			tk.regexp = regexp.MustCompile("^(?:" + reg + ")$")
		}
		return tk
	}

	wildcardToken := func(v, reg string) token {
		tk := token{
			value: v,
			typ:   nodeWildcard,
		}
		if reg != "" {
			tk.regexp = regexp.MustCompile("^(?:" + reg + ")$")
		}
		return tk
	}

	nestedWildcardToken := func(v, reg string) token {
		tk := wildcardToken(v, reg)
		tk.nested = true
		return tk
	}

	hostParamToken := func(v, reg string) token {
		tk := token{
			value: v,
			typ:   nodeParam,
		}
		if reg != "" {
			tk.regexp = regexp.MustCompile("(?i)^(?:" + reg + ")$")
		}
		return tk
	}

	hostWildcardToken := func(v, reg string) token {
		tk := token{
			value: v,
			typ:   nodeWildcard,
		}
		if reg != "" {
			tk.regexp = regexp.MustCompile("(?i)^(?:" + reg + ")$")
		}
		return tk
	}

	cases := []struct {
		name             string
		path             string
		wantN            int
		wantTokens       []token
		optionalCatchAll bool
	}{
		{
			name:       "valid static route",
			path:       "/foo/bar",
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/foo/bar", false))),
		},
		{
			name:  "top level domain param",
			path:  "{tld}/foo/bar",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				paramToken("tld", ""),
				staticToken("/foo/bar", false),
			)),
		},
		{
			name:  "top level domain wildcard",
			path:  "+{tld}/foo/bar",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				wildcardToken("tld", ""),
				staticToken("/foo/bar", false),
			)),
		},
		{
			name:  "valid catch all route",
			path:  "/foo/bar/+{arg}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/", false),
				wildcardToken("arg", ""),
			)),
		},
		{
			name:  "valid param route",
			path:  "/foo/bar/{baz}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/", false),
				paramToken("baz", ""),
			)),
		},
		{
			name:  "valid multi params route",
			path:  "/foo/{bar}/{baz}",
			wantN: 2,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", ""),
				staticToken("/", false),
				paramToken("baz", ""),
			)),
		},
		{
			name:  "valid same params route",
			path:  "/foo/{bar}/{bar}",
			wantN: 2,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", ""),
				staticToken("/", false),
				paramToken("bar", ""),
			)),
		},
		{
			name:  "valid multi params and catch all route",
			path:  "/foo/{bar}/{baz}/+{arg}",
			wantN: 3,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", ""),
				staticToken("/", false),
				paramToken("baz", ""),
				staticToken("/", false),
				wildcardToken("arg", ""),
			)),
		},
		{
			name:  "valid inflight param",
			path:  "/foo/xyz:{bar}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/xyz:", false),
				paramToken("bar", ""),
			)),
		},
		{
			name:  "valid inflight catchall",
			path:  "/foo/xyz:+{bar}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/xyz:", false),
				wildcardToken("bar", ""),
			)),
		},
		{
			name:  "valid multi inflight param and catch all",
			path:  "/foo/xyz:{bar}/abc:{bar}/+{arg}",
			wantN: 3,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/xyz:", false),
				paramToken("bar", ""),
				staticToken("/abc:", false),
				paramToken("bar", ""),
				staticToken("/", false),
				wildcardToken("arg", ""),
			)),
		},
		{
			name:  "catch all with arg in the middle of the route",
			path:  "/foo/bar/+{bar}/baz",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/baz", false),
			)),
		},
		{
			name:  "multiple catch all suffix and inflight with arg in the middle of the route",
			path:  "/foo/bar/+{bar}/x+{args}/y/+{z}/{b}",
			wantN: 4,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/x", false),
				nestedWildcardToken("args", ""),
				staticToken("/y/", false),
				nestedWildcardToken("z", ""),
				staticToken("/", false),
				paramToken("b", ""),
			)),
		},
		{
			name:  "inflight catch all with arg in the middle of the route",
			path:  "/foo/bar/damn+{bar}/baz",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/damn", false),
				wildcardToken("bar", ""),
				staticToken("/baz", false),
			)),
		},
		{
			name:  "catch all with arg in the middle of the route and param after",
			path:  "/foo/bar/+{bar}/{baz}",
			wantN: 2,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/", false),
				paramToken("baz", ""),
			)),
		},
		{
			name:  "simple domain and path",
			path:  "foo/bar",
			wantN: 0,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("foo", true),
				staticToken("/bar", false),
			)),
		},
		{
			name:  "simple domain with trailing slash",
			path:  "foo/",
			wantN: 0,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("foo", true),
				staticToken("/", false),
			)),
		},
		{
			name:  "period in param path allowed",
			path:  "foo/{.bar}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("foo", true),
				staticToken("/", false),
				paramToken(".bar", ""),
			)),
		},
		{
			name: "missing trailing slash after hostname",
			path: "foo.com",
		},
		{
			name:  "empty parameter",
			path:  "/foo/bar{}",
			wantN: 0,
		},
		{
			name:  "encoded unreserved rejected",
			path:  "/a%61b",
			wantN: 0,
		},
		{
			name:  "lowercase hex rejected",
			path:  "/foo%2fbar/caf%c3%a9",
			wantN: 0,
		},
		{
			name:       "encoded reserved with uppercase hex kept as registered",
			path:       "/foo%2Fbar/caf%C3%A9",
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/foo%2Fbar/caf%C3%A9", false))),
		},
		{
			name:       "encoded structural characters stay literal",
			path:       "/a%7Bb%7D/%2A/%2B",
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/a%7Bb%7D/%2A/%2B", false))),
		},
		{
			name:  "encoded unreserved before param rejected",
			path:  "/x%61/{id}",
			wantN: 0,
		},
		{
			name:       "regex constraint not decoded",
			path:       "/foo/{bar:[%41]+}",
			wantN:      1,
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/foo/", false), paramToken("bar", "[%41]+"))),
		},
		{
			name:       "escape inside brace segment not validated",
			path:       "/foo/{bar:[%61%2f]+}",
			wantN:      1,
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/foo/", false), paramToken("bar", "[%61%2f]+"))),
		},
		{
			name:  "escape after brace segment rejected",
			path:  "/{id}/%2e",
			wantN: 0,
		},
		{
			name:  "unbalanced brace with escape",
			path:  "/foo/{bar%2",
			wantN: 0,
		},
		{
			name:  "encoded dot dot segment rejected",
			path:  "/foo/%2E%2E/bar",
			wantN: 0,
		},
		{
			name:  "encoded single dot segment rejected",
			path:  "/foo/%2e/bar",
			wantN: 0,
		},
		{
			name:  "trailing percent rejected",
			path:  "/100%",
			wantN: 0,
		},
		{
			name:  "truncated escape rejected",
			path:  "/foo%2",
			wantN: 0,
		},
		{
			name:  "invalid escape rejected",
			path:  "/%zz",
			wantN: 0,
		},
		{
			name:       "literal star as suffix",
			path:       "/foo/bar/*",
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/foo/bar/*", false))),
		},
		{
			name:  "missing arguments name after param",
			path:  "/foo/bar/{",
			wantN: 0,
		},
		{
			name:       "literal star in the middle of the route",
			path:       "/foo/bar/*/baz",
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/foo/bar/*/baz", false))),
		},
		{
			name:       "literal plus as suffix",
			path:       "/foo/bar/a+",
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/foo/bar/a+", false))),
		},
		{
			name:       "literal plus in the middle of the route",
			path:       "/emails/a+b/baz",
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/emails/a+b/baz", false))),
		},
		{
			name:       "literal plus before wildcard brace still wildcard",
			path:       "/foo/+{bar}",
			wantN:      1,
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/foo/", false), wildcardToken("bar", ""))),
		},
		{
			name:  "empty infix catch all",
			path:  "/foo/bar/+{}/baz",
			wantN: 0,
		},
		{
			name:  "empty ending catch all",
			path:  "/foo/bar/baz/+{}",
			wantN: 0,
		},
		{
			name:  "unexpected character in param",
			path:  "/foo/{{bar}",
			wantN: 0,
		},
		{
			name:  "unexpected character in param",
			path:  "/foo/{*bar}",
			wantN: 0,
		},
		{
			name:  "unexpected character in catch-all",
			path:  "/foo/+{/bar}",
			wantN: 0,
		},
		{
			name:  "catch all not supported in hostname",
			path:  "a.b.c*/",
			wantN: 0,
		},
		{
			name:  "illegal character in params hostname",
			path:  "a.b.c{/",
			wantN: 0,
		},
		{
			name:  "illegal character in hostname label",
			path:  "a.b.c}/",
			wantN: 0,
		},
		{
			name:  "unexpected character in param hostname",
			path:  "a.{.bar}.c/",
			wantN: 0,
		},
		{
			name:  "unexpected character in wildcard hostname",
			path:  "a.+{.bar}.c/",
			wantN: 0,
		},
		{
			name:  "unexpected character in param hostname",
			path:  "a.{/bar}.c/",
			wantN: 0,
		},
		{
			name:  "unexpected character in wildcard hostname",
			path:  "a.+{/bar}.c/",
			wantN: 0,
		},
		{
			name:  "in flight catch-all after param in one route segment",
			path:  "/foo/{bar}+{baz}",
			wantN: 0,
		},
		{
			name:  "multiple param in one route segment",
			path:  "/foo/{bar}{baz}",
			wantN: 0,
		},
		{
			name:  "in flight param after catch all",
			path:  "/foo/+{args}{param}",
			wantN: 0,
		},
		{
			name:  "consecutive catch all with no slash",
			path:  "/foo/+{args}+{param}",
			wantN: 0,
		},
		{
			name:  "consecutive catch all",
			path:  "/foo/+{args}/+{param}",
			wantN: 0,
		},
		{
			name:  "consecutive catch all with inflight",
			path:  "/foo/ab+{args}/+{param}",
			wantN: 0,
		},
		{
			name:  "unexpected char after inflight catch all",
			path:  "/foo/ab+{args}a",
			wantN: 0,
		},
		{
			name:  "unexpected char after catch all",
			path:  "/foo/+{args}a",
			wantN: 0,
		},
		{
			name:  "prefix catch-all in hostname",
			path:  "+{any}.com/foo",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				wildcardToken("any", ""),
				staticToken(".com", true),
				staticToken("/foo", false),
			)),
		},
		{
			name:  "infix catch-all in hostname",
			path:  "a.+{any}.com/foo",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("a.", true),
				wildcardToken("any", ""),
				staticToken(".com", true),
				staticToken("/foo", false),
			)),
		},
		{
			name:  "illegal catch-all in hostname",
			path:  "a.b.+{any}/foo",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("a.b.", true),
				wildcardToken("any", ""),
				staticToken("/foo", false),
			)),
		},
		{
			name:  "static hostname with catch-all path",
			path:  "a.b.com/+{any}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("a.b.com", true),
				staticToken("/", false),
				wildcardToken("any", ""),
			)),
		},
		{
			name:  "illegal control character in path",
			path:  "example.com/foo\x00",
			wantN: 0,
		},
		{
			name:  "illegal control character in path regex constraint",
			path:  "/x/{a:\x01}",
			wantN: 0,
		},
		{
			name:  "illegal control character in hostname regex constraint",
			path:  "{s:a\nb}.example.com/",
			wantN: 0,
		},
		{
			name:  "illegal leading hyphen in hostname",
			path:  "-a.com/",
			wantN: 0,
		},
		{
			name:  "illegal leading dot in hostname",
			path:  ".a.com/",
			wantN: 0,
		},
		{
			name:  "illegal trailing hyphen in hostname",
			path:  "a.com-/",
			wantN: 0,
		},
		{
			name:  "illegal trailing dot in hostname",
			path:  "a.com./",
			wantN: 0,
		},
		{
			name:  "illegal trailing dot in hostname after param",
			path:  "{tld}./foo/bar",
			wantN: 0,
		},
		{
			name:  "illegal single dot in hostname",
			path:  "./",
			wantN: 0,
		},
		{
			name:  "illegal hyphen before dot",
			path:  "a-.com/",
			wantN: 0,
		},
		{
			name:  "illegal hyphen after dot",
			path:  "a.-com/",
			wantN: 0,
		},
		{
			name:  "illegal double dot",
			path:  "a..com/",
			wantN: 0,
		},
		{
			name:  "illegal double dot with param state",
			path:  "{b}..com/",
			wantN: 0,
		},
		{
			name:  "illegal double dot with inflight param state",
			path:  "a{b}..com/",
			wantN: 0,
		},
		{
			name:  "param not finishing with delimiter in hostname",
			path:  "{a}b{b}.com/",
			wantN: 0,
		},
		{
			name:  "consecutive parameter in hostname",
			path:  "{a}{b}.com/",
			wantN: 0,
		},
		{
			name:  "leading hostname label exceed 63 characters",
			path:  "uj01dowf1x5lk6lysurbr0lgbdd1wfyw8sm8q17mnt0i9igk774vcwr5rly5dguu.b.com/",
			wantN: 0,
		},
		{
			name:  "middle hostname label exceed 63 characters",
			path:  "a.uj01dowf1x5lk6lysurbr0lgbdd1wfyw8sm8q17mnt0i9igk774vcwr5rly5dguu.com/",
			wantN: 0,
		},
		{
			name:  "trailing hostname label exceed 63 characters",
			path:  "a.b.uj01dowf1x5lk6lysurbr0lgbdd1wfyw8sm8q17mnt0i9igk774vcwr5rly5dguu/",
			wantN: 0,
		},
		{
			name:  "illegal character in domain",
			path:  "a.b!.com/",
			wantN: 0,
		},
		{
			name:  "invalid all-numeric label",
			path:  "123/",
			wantN: 0,
		},
		{
			name:  "all-numeric label with param",
			path:  "123.{a}.456/",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("123.", true),
				paramToken("a", ""),
				staticToken(".456", true),
				staticToken("/", false),
			)),
		},
		{
			name:  "all-numeric label with wildcard",
			path:  "123.+{a}.456/",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("123.", true),
				wildcardToken("a", ""),
				staticToken(".456", true),
				staticToken("/", false),
			)),
		},
		{
			name:  "all-numeric label with path wildcard",
			path:  "123.456/{abc}",
			wantN: 0,
		},
		{
			name:  "hostname exceed 255 character",
			path:  "a.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjx.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr/",
			wantN: 0,
		},
		{
			name:  "invalid all-numeric label",
			path:  "11.22.33/",
			wantN: 0,
		},
		{
			name:  "invalid uppercase label",
			path:  "ABC/",
			wantN: 0,
		},
		{
			name:  "2 regular params in domain",
			path:  "{a}.{b}.com/",
			wantN: 2,
			wantTokens: slices.Collect(iterutil.SeqOf(
				paramToken("a", ""),
				staticToken(".", true),
				paramToken("b", ""),
				staticToken(".com", true),
				staticToken("/", false),
			)),
		},
		{
			name:  "253 character with .",
			path:  "78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzj/",
			wantN: 0,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzj", true),
				staticToken("/", false),
			)),
		},
		{
			name:  "param does not count at character",
			path:  "{a}.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjx.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzj/",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				paramToken("a", ""),
				staticToken(".78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjx.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzj", true),
				staticToken("/", false),
			)),
		},
		{
			name:  "hostname variant with multiple catch all suffix and inflight with arg in the middle of the route",
			path:  "example.com/foo/bar/+{bar}/x+{args}/y/+{z}/{b}",
			wantN: 4,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("example.com", true),
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/x", false),
				nestedWildcardToken("args", ""),
				staticToken("/y/", false),
				nestedWildcardToken("z", ""),
				staticToken("/", false),
				paramToken("b", ""),
			)),
		},
		{
			name:  "hostname variant with inflight catch all with arg in the middle of the route",
			path:  "example.com/foo/bar/damn+{bar}/baz",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("example.com", true),
				staticToken("/foo/bar/damn", false),
				wildcardToken("bar", ""),
				staticToken("/baz", false),
			)),
		},
		{
			name:  "hostname variant catch all with arg in the middle of the route and param after",
			path:  "example.com/foo/bar/+{bar}/{baz}",
			wantN: 2,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("example.com", true),
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/", false),
				paramToken("baz", ""),
			)),
		},
		{
			name:  "complex domain and path",
			path:  "{ab}.{c}.de{f}.com/foo/bar/+{bar}/x+{args}/y/+{z}/{b}",
			wantN: 7,
			wantTokens: slices.Collect(iterutil.SeqOf(
				paramToken("ab", ""),
				staticToken(".", true),
				paramToken("c", ""),
				staticToken(".de", true),
				paramToken("f", ""),
				staticToken(".com", true),
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/x", false),
				nestedWildcardToken("args", ""),
				staticToken("/y/", false),
				nestedWildcardToken("z", ""),
				staticToken("/", false),
				paramToken("b", ""),
			)),
		},
		{
			name: "path with double slash",
			path: "/foo//bar",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo//bar", false),
			)),
		},
		{
			name: "path with > double slash",
			path: "/foo///bar",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo///bar", false),
			)),
		},
		{
			name: "hostname with double slash path",
			path: "example.com//foo",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("example.com", true),
				staticToken("//foo", false),
			)),
		},
		{
			name:  "param after empty segment",
			path:  "/foo//{bar}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo//", false),
				paramToken("bar", ""),
			)),
		},
		{
			name: "path with slash dot slash",
			path: "/foo/./bar",
		},
		{
			name: "path with slash dot slash",
			path: "/foo/././bar",
		},
		{
			name: "path with double dot parent reference",
			path: "/foo/../bar",
		},
		{
			name: "path with double dot parent reference",
			path: "/foo/../../bar",
		},
		{
			name: "path ending with slash dot",
			path: "/foo/.",
		},
		{
			name: "path ending with slash double dot",
			path: "/foo/..",
		},
		{
			name: "path ending with slash dot",
			path: "/.",
		},
		{
			name: "path ending with slash double dot",
			path: "/..",
		},
		// Allowed dot and slash combinations
		{
			name: "last path segment starting with slash dot and text",
			path: "/foo/.bar",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/.bar", false),
			)),
		},
		{
			name: "last path segment starting with slash dot and text",
			path: "/foo/..bar",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/..bar", false),
			)),
		},
		{
			name: "path segment starting with slash dot and text",
			path: "/foo/.bar/baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/.bar/baz", false),
			)),
		},
		{
			name:  "path segment starting with slash dot and param",
			path:  "/foo/.{foo}/baz",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/.", false),
				paramToken("foo", ""),
				staticToken("/baz", false),
			)),
		},
		{
			name: "path segment starting with slash dot and text",
			path: "/foo/..bar/baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/..bar/baz", false),
			)),
		},
		{
			name:  "path segment starting with slash dot and param",
			path:  "/foo/..{foo}/baz",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/..", false),
				paramToken("foo", ""),
				staticToken("/baz", false),
			)),
		},
		{
			name: "path segment ending with dot slash",
			path: "/foo/bar./baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar./baz", false),
			)),
		},
		{
			name: "path segment ending with double dot slash",
			path: "/foo/bar../baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar../baz", false),
			)),
		},
		{
			name: "path segment with > double dot",
			path: "/foo/.../baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/.../baz", false),
			)),
		},
		{
			name: "path segment ending with slash and > double dot",
			path: "/foo/...",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/...", false),
			)),
		},
		{
			name: "last path segment ending with dot",
			path: "/foo/bar.",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar.", false),
			)),
		},
		{
			name: "last path segment ending with double dot",
			path: "/foo/bar..",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar..", false),
			)),
		},
		{
			name: "path segment with dot",
			path: "/foo/a.b.c",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/a.b.c", false),
			)),
		},
		{
			name: "path segment with double dot",
			path: "/foo/a..b..c",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/a..b..c", false),
			)),
		},
		// Regexp
		{
			name: "simple ending param with regexp",
			path: "/foo/{bar:[A-z]+}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", "[A-z]+"),
			)),
			wantN: 1,
		},
		{
			name: "simple ending param with regexp",
			path: "/foo/+{bar:[A-z]+}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				wildcardToken("bar", "[A-z]+"),
			)),
			wantN: 1,
		},
		{
			name: "simple infix param with regexp",
			path: "/foo/{bar:[A-z]+}/baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", "[A-z]+"),
				staticToken("/baz", false),
			)),
			wantN: 1,
		},
		{
			name: "multi infix and ending param with regexp",
			path: "/foo/{bar:[A-z]+}/{baz:[0-9]+}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", "[A-z]+"),
				staticToken("/", false),
				paramToken("baz", "[0-9]+"),
			)),
			wantN: 2,
		},
		{
			name: "multi infix and ending wildcard with regexp",
			path: "/foo/+{bar:[A-z]+}/a+{baz:[0-9]+}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				wildcardToken("bar", "[A-z]+"),
				staticToken("/a", false),
				nestedWildcardToken("baz", "[0-9]+"),
			)),
			wantN: 2,
		},
		{
			name: "consecutive infix regexp wildcard and regexp param allowed",
			path: "/foo/+{bar:[A-z]+}/{baz:[0-9]+}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				wildcardToken("bar", "[A-z]+"),
				staticToken("/", false),
				paramToken("baz", "[0-9]+"),
			)),
			wantN: 2,
		},
		{
			name: "hostname starting with regexp",
			path: "{a:[A-z]+}.b.c/foo",
			wantTokens: slices.Collect(iterutil.SeqOf(
				hostParamToken("a", "[A-z]+"),
				staticToken(".b.c", true),
				staticToken("/foo", false),
			)),
			wantN: 1,
		},
		{
			name: "hostname with middle param regexp",
			path: "a.{b:[A-z]+}.c/foo",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("a.", true),
				hostParamToken("b", "[A-z]+"),
				staticToken(".c", true),
				staticToken("/foo", false),
			)),
			wantN: 1,
		},
		{
			name: "hostname ending with param regexp",
			path: "a.b.{c:[A-z]+}/foo",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("a.b.", true),
				hostParamToken("c", "[A-z]+"),
				staticToken("/foo", false),
			)),
			wantN: 1,
		},
		{
			name: "non capturing group allowed in regexp",
			path: "/foo/{bar:(?:foo|bar)}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", "(?:foo|bar)"),
			)),
			wantN: 1,
		},
		{
			name: "regexp wildcard at the beginning of the path",
			path: "/+{foo:[A-z]+}/bar",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/", false),
				wildcardToken("foo", "[A-z]+"),
				staticToken("/bar", false),
			)),
			wantN: 1,
		},
		{
			name: "regexp wildcard at the beginning of the host",
			path: "+{a:[A-z]+}.b.c/",
			wantTokens: slices.Collect(iterutil.SeqOf(
				hostWildcardToken("a", "[A-z]+"),
				staticToken(".b.c", true),
				staticToken("/", false),
			)),
			wantN: 1,
		},
		{
			name: "consecutive wildcard from hostname to path",
			path: "+{foo}/+{bar}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				wildcardToken("foo", ""),
				staticToken("/", false),
				wildcardToken("bar", ""),
			)),
			wantN: 2,
		},
		{
			name: "consecutive wildcard with empty catch all from hostname to path",
			path: "+{foo}/*{bar}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				wildcardToken("foo", ""),
				staticToken("/", false),
				wildcardToken("bar", ""),
			)),
			wantN:            2,
			optionalCatchAll: true,
		},
		{
			name: "param then wildcard regexp",
			path: "{a}.+{b:b}/",
			wantTokens: slices.Collect(iterutil.SeqOf(
				paramToken("a", ""),
				staticToken(".", true),
				hostWildcardToken("b", "b"),
				staticToken("/", false),
			)),
			wantN: 2,
		},
		{
			name: "param regexp then wildcard regexp",
			path: "{a:a}.+{b:b}/",
			wantTokens: slices.Collect(iterutil.SeqOf(
				hostParamToken("a", "a"),
				staticToken(".", true),
				hostWildcardToken("b", "b"),
				staticToken("/", false),
			)),
			wantN: 2,
		},
		{
			name: "catch all empty as suffix",
			path: "/foo/*{any}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				wildcardToken("any", ""),
			)),
			wantN:            1,
			optionalCatchAll: true,
		},
		{
			name: "consecutive infix wildcard at start with regexp not allowed",
			path: "/+{foo:[A-z]+}/+{baz:[0-9]+}",
		},
		{
			name: "consecutive wildcard with catch all empty not allowed",
			path: "/+{foo}/*{baz}",
		},
		{
			name: "consecutive infix wildcard with catch all empty at start with regexp not allowed",
			path: "/+{foo:[A-z]+}/*{baz:[0-9]+}",
		},
		{
			name: "hostname consecutive infix wildcard at start with regexp not allowed",
			path: "/{foo:[A-z]+}.+{baz:[0-9]+}/",
		},
		{
			name: "consecutive infix wildcard at start with and without regexp not allowed",
			path: "/+{foo:[A-z]+}/+{baz}",
		},
		{
			name: "hostname consecutive infix wildcard at start with and without regexp not allowed",
			path: "+{foo:[A-z]+}.+{baz}/",
		},
		{
			name: "consecutive infix wildcard at start with regexp not allowed",
			path: "/+{foo}/+{baz:[0-9]+}/",
		},
		{
			name: "hostname consecutive infix wildcard at start with regexp not allowed",
			path: "+{foo}.+{baz:[0-9]+}/",
		},
		{
			name: "consecutive infix wildcard with regexp not allowed",
			path: "/foo/+{bar:[A-z]+}/+{baz:[0-9]+}",
		},
		{
			name: "hostname consecutive infix wildcard with regexp not allowed",
			path: "foo.+{bar:[A-z]+}.+{baz:[0-9]+}/",
		},
		{
			name: "consecutive infix wildcard with first regexp not allowed",
			path: "/foo/+{bar:[A-z]+}/+{baz}",
		},
		{
			name: "hostname consecutive infix wildcard with first regexp not allowed",
			path: "foo.+{bar:[A-z]+}.+{baz}/",
		},
		{
			name: "consecutive infix wildcard with second regexp not allowed",
			path: "/foo/+{bar}/+{baz:[A-z]+}/",
		},
		{
			name: "hostname consecutive infix wildcard with second regexp not allowed",
			path: "foo.+{bar}.+{baz:[A-z]+}/",
		},
		{
			name: "non slash char after regexp param not allowed",
			path: "/foo/{bar:[A-z]+}a/",
		},
		{
			name: "non slash char after regexp wildcard not allowed",
			path: "/foo/+{bar:[A-z]+}a/",
		},
		{
			name: "regexp wildcard not allowed in hostname",
			path: "+{a.{b:[A-z]+}}.c/",
		},
		{
			name: "regexp wildcard not allowed in hostname",
			path: "+{a.b.{c:[A-z]+}/",
		},
		{
			name: "missing param name with regexp",
			path: "/foo/{:[A-z]+}",
		},
		{
			name: "missing wildcard name with regexp",
			path: "/foo/+{:[A-z]+}",
		},
		{
			name: "missing regular expression",
			path: "/foo/{a:}",
		},
		{
			name: "missing regular expression with only ':'",
			path: "/foo/{:}",
		},
		{
			name: "unsupported regexp in optional wildcard",
			path: "/foo/*{any:[A-z]+}",
		},
		{
			name: "unbalanced braces in param regexp",
			path: "/foo/{bar:[A-z]+",
		},
		{
			name: "unbalanced braces in wildcard regexp",
			path: "/foo/+{bar:[A-z]+",
		},
		{
			name: "balanced braces in param regexp with invalid char after",
			path: "/foo/{bar:{}}a",
		},
		{
			name: "balanced braces in wildcard regexp with invalid brace after",
			path: "/foo/{bar:{}}}",
		},
		{
			name: "unbalanced braces in regexp complex",
			path: "/foo/{bar:{{{{}}}}",
		},
		{
			name: "invalid regular expression",
			path: "/foo/{bar:a{5,2}}",
		},
		{
			name: "invalid regular expression",
			path: "/foo/{bar:\\k}",
		},
		{
			name: "capture group in regexp are not allowed",
			path: "/foo/{bar:(foo|bar)}",
		},
		{
			name:       "literal star without opening brace",
			path:       "/foo/*:bar",
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/foo/*:bar", false))),
		},
		{
			name: "no infix catch all empty",
			path: "/foo/*{any}/bar",
		},
		{
			name: "no infix inflight catch all empty",
			path: "/foo/uuid_*{any}/bar",
		},
		{
			name: "no suffix catch all empty in hostname",
			path: "a.b.*{any}/",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pat, paramCnt, err := f.parsePattern(tc.path)
			if err != nil {
				var patErr *PatternError
				require.ErrorAs(t, err, &patErr)
				return
			}
			assert.Equal(t, tc.wantN, paramCnt)
			assert.Equal(t, tc.wantTokens, pat.tokens)
			assert.Equal(t, tc.optionalCatchAll, pat.optionalCatchAll)
			if err == nil {
				assert.Equal(t, strings.IndexByte(tc.path, '/'), pat.endHost)
			}
		})
	}
}

func Test_parsePattern_CanonicalEncoding(t *testing.T) {
	f := MustRouter()

	t.Run("valid patterns are stored as registered", func(t *testing.T) {
		cases := []string{
			"/users/john",
			"/a%7Bb",
			"/a%2Fb/{id}",
			"example.com/caf%C3%A9",
		}
		for _, in := range cases {
			pat, _, err := f.parsePattern(in)
			require.NoError(t, err)
			assert.Equal(t, in, pat.str)
			assert.Equal(t, strings.IndexByte(in, '/'), pat.endHost)
		}
	})

	t.Run("non-canonical escapes are rejected", func(t *testing.T) {
		cases := []struct {
			name     string
			in       string
			wantHint string
		}{
			{"encoded unreserved", "/users/j%6Fhn", "non-canonical percent-encoding, encodes an unreserved character"},
			{"lowercase hex", "/a%2fb", "non-canonical percent-encoding, hex must be uppercase"},
			{"encoded tilde", "/%7E", "non-canonical percent-encoding, encodes an unreserved character"},
			{"with hostname", "example.com/%7e%61", "non-canonical percent-encoding, encodes an unreserved character"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, _, err := f.parsePattern(tc.in)
				var pe *PatternError
				require.ErrorAs(t, err, &pe)
				assert.Equal(t, "syntax", pe.Reason)
				assert.Equal(t, tc.in, pe.Pattern)
				assert.Contains(t, pe.Error(), tc.wantHint)
			})
		}
	})
}

func TestPatternError_PatternField(t *testing.T) {
	f := MustRouter()
	var pe *PatternError

	_, _, err := f.parsePattern("/foo/%zz")
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, "/foo/%zz", pe.Pattern)

	_, _, err = f.parsePattern("/foo/../bar")
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, "/foo/../bar", pe.Pattern)
}

func Test_parsePattern_ParamsConstraint(t *testing.T) {
	t.Run("param limit", func(t *testing.T) {
		f, _ := NewRouter(WithMaxRouteParams(3))
		_, _, err := f.parsePattern("/{1}/{2}/{3}")
		assert.NoError(t, err)
		_, _, err = f.parsePattern("/{1}/{2}/{3}/{4}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/ab{1}/{2}/cd/{3}/{4}/ef")
		assert.Error(t, err)
	})
	t.Run("param key limit", func(t *testing.T) {
		f, _ := NewRouter(WithMaxRouteParamKeyBytes(3))
		_, _, err := f.parsePattern("/{abc}/{abc}/{abc}")
		assert.NoError(t, err)
		_, _, err = f.parsePattern("/{abcd}/{abc}/{abc}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{abc}/{abcd}/{abc}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{abc}/{abc}/{abcd}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{abc}/+{abcd}/{abc}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{abc}/{abc}/+{abcdef}")
		assert.Error(t, err)
	})
	t.Run("param key limit with regexp", func(t *testing.T) {
		f, _ := NewRouter(WithMaxRouteParamKeyBytes(3), WithAllowRegexpParam(true))
		_, _, err := f.parsePattern("/{abc:a}/{abc:a}/{abc:a}")
		assert.NoError(t, err)
		_, _, err = f.parsePattern("/{abcd:a}/{abc:a}/{abc:a}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{abc:a}/{abcd:a}/{abc:a}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{abc:a}/{abc:a}/{abcd:a}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{abc:a}/+{abcd:a}/{abc:a}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{abc:a}/{abc:a}/+{abcdef:a}")
		assert.Error(t, err)
	})
	t.Run("disabled regexp support for param", func(t *testing.T) {
		f, _ := NewRouter()
		_, _, err := f.parsePattern("/{a}/{b}/{c}")
		assert.NoError(t, err)
		// path params
		_, _, err = f.parsePattern("/{a:a}/{b}/{c}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{a}/{b:b}/{c}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{a}/{b}/{c:c}")
		assert.Error(t, err)
		// hostname params
		_, _, err = f.parsePattern("{a:a}.{b}.{c}/")
		assert.Error(t, err)
		_, _, err = f.parsePattern("{a}.{b:b}.{c}/")
		assert.Error(t, err)
		_, _, err = f.parsePattern("{a}.{b}.{c:c}/")
		assert.Error(t, err)
	})
	t.Run("disabled regexp support for wildcard", func(t *testing.T) {
		f, _ := NewRouter()
		_, _, err := f.parsePattern("/{a}/{b}/{c}")
		assert.NoError(t, err)
		// wildcard
		_, _, err = f.parsePattern("/+{a:a}/{b}/{c}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{a}/+{b:b}/{c}")
		assert.Error(t, err)
		_, _, err = f.parsePattern("/{a}/{b}/+{c:c}")
		assert.Error(t, err)
	})
}

func TestPatternError_Position(t *testing.T) {
	cases := []struct {
		name       string
		pattern    string
		options    []GlobalOption
		wantType   string
		wantReason string
		wantStart  int
		wantEnd    int
		wantMsg    string
	}{
		{
			name:       "hostname missing trailing slash",
			pattern:    "foo.com",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  7,
			wantEnd:    7,
			wantMsg:    "missing trailing '/' after hostname",
		},
		{
			name:       "hostname label starts with dash",
			pattern:    "-a.com/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  0,
			wantEnd:    1,
			wantMsg:    "label starts with '-'",
		},
		{
			name:       "hostname label starts with dot",
			pattern:    ".a.com/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  0,
			wantEnd:    1,
			wantMsg:    "label starts with '.'",
		},
		{
			name:       "hostname dash after dot",
			pattern:    "a.-b.com/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  2,
			wantEnd:    3,
			wantMsg:    "illegal character after '.'",
		},
		{
			name:       "hostname consecutive dots",
			pattern:    "a..com/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  1,
			wantEnd:    3,
			wantMsg:    "illegal consecutive '.'",
		},
		{
			name:       "hostname label ends with dash",
			pattern:    "a-.com/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  1,
			wantEnd:    2,
			wantMsg:    "label ends with '-'",
		},
		{
			name:       "hostname label exceeds 63 chars at dot",
			pattern:    "uj01dowf1x5lk6lysurbr0lgbdd1wfyw8sm8q17mnt0i9igk774vcwr5rly5dguu.com/",
			wantType:   "hostname",
			wantReason: "constraint",
			wantStart:  0,
			wantEnd:    64,
			wantMsg:    "label exceeds 63 characters",
		},
		{
			name:       "hostname uppercase character",
			pattern:    "A.com/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  0,
			wantEnd:    1,
			wantMsg:    "uppercase character in label",
		},
		{
			name:       "hostname illegal character in label",
			pattern:    "a!.com/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  1,
			wantEnd:    2,
			wantMsg:    "illegal character in label",
		},
		{
			name:       "hostname trailing dash",
			pattern:    "a.com-/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  5,
			wantEnd:    6,
			wantMsg:    "illegal trailing '-'",
		},
		{
			name:       "hostname trailing dot",
			pattern:    "a.com./",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  5,
			wantEnd:    6,
			wantMsg:    "illegal trailing '.'",
		},
		{
			name:       "hostname all numeric",
			pattern:    "123/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  0,
			wantEnd:    3,
			wantMsg:    "all numeric",
		},
		{
			name:       "hostname trailing label exceeds 63 chars",
			pattern:    "a.b.uj01dowf1x5lk6lysurbr0lgbdd1wfyw8sm8q17mnt0i9igk774vcwr5rly5dguu/",
			wantType:   "hostname",
			wantReason: "constraint",
			wantStart:  4,
			wantEnd:    68,
			wantMsg:    "label exceeds 63 characters",
		},
		{
			name:       "hostname exceeds 253 characters",
			pattern:    "a.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjx.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr/",
			wantType:   "hostname",
			wantReason: "constraint",
			wantStart:  0,
			wantEnd:    256,
			wantMsg:    "exceeds 253 characters",
		},
		{
			name:       "hostname missing parameter after + delimiter",
			pattern:    "+a.com/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  0,
			wantEnd:    1,
			wantMsg:    "missing parameter after delimiter",
		},
		{
			name:       "hostname consecutive wildcard",
			pattern:    "+{a}.+{b}.com/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  5,
			wantEnd:    9,
			wantMsg:    "consecutive wildcard",
		},
		{
			name:       "hostname too many parameters",
			pattern:    "{a}.{b}.com/",
			options:    []GlobalOption{WithMaxRouteParams(1)},
			wantType:   "hostname",
			wantReason: "constraint",
			wantStart:  4,
			wantEnd:    7,
			wantMsg:    "too many parameters",
		},
		{
			name:       "hostname illegal character after parameter",
			pattern:    "{a}b.com/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  3,
			wantEnd:    4,
			wantMsg:    "illegal character after parameter",
		},
		{
			name:       "hostname optional wildcard not allowed",
			pattern:    "a.*{any}/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  2,
			wantEnd:    8,
			wantMsg:    "optional wildcard allowed only as suffix",
		},
		{
			name:       "hostname bare star illegal character",
			pattern:    "a.b*/",
			wantType:   "hostname",
			wantReason: "syntax",
			wantStart:  3,
			wantEnd:    4,
			wantMsg:    "illegal character in label",
		},
		{
			name:       "path consecutive wildcard",
			pattern:    "/+{a}/+{b}",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  6,
			wantEnd:    10,
			wantMsg:    "consecutive wildcard",
		},
		{
			name:       "path too many parameters",
			pattern:    "/foo/{a}/{b}",
			options:    []GlobalOption{WithMaxRouteParams(1)},
			wantType:   "path",
			wantReason: "constraint",
			wantStart:  9,
			wantEnd:    12,
			wantMsg:    "too many parameters",
		},
		{
			name:       "path optional wildcard not as suffix",
			pattern:    "/foo/*{any}/bar",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  5,
			wantEnd:    11,
			wantMsg:    "optional wildcard allowed only as suffix",
		},
		{
			name:       "path illegal character after parameter",
			pattern:    "/foo/{a}b",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  8,
			wantEnd:    9,
			wantMsg:    "illegal character after parameter",
		},
		{
			name:       "path illegal control character",
			pattern:    "/foo\x01bar",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  4,
			wantEnd:    5,
			wantMsg:    "illegal control character",
		},
		{
			name:       "path invalid percent encoding",
			pattern:    "/foo/%zzbar",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  5,
			wantEnd:    8,
			wantMsg:    "invalid percent-encoding",
		},
		{
			name:       "path truncated percent encoding",
			pattern:    "/foo%2",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  4,
			wantEnd:    6,
			wantMsg:    "invalid percent-encoding",
		},
		{
			name:       "path invalid percent encoding with hostname",
			pattern:    "example.com/%zz",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  12,
			wantEnd:    15,
			wantMsg:    "invalid percent-encoding",
		},
		{
			name:       "path encoded dot segment rejected as non-canonical",
			pattern:    "/foo/%2E%2E/bar",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  5,
			wantEnd:    8,
			wantMsg:    "non-canonical percent-encoding, encodes an unreserved character",
		},
		{
			name:       "path lowercase hex rejected as non-canonical",
			pattern:    "/a%2fb",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  2,
			wantEnd:    5,
			wantMsg:    "non-canonical percent-encoding, hex must be uppercase",
		},
		{
			name:       "path raw space requires encoding",
			pattern:    "/foo bar",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  4,
			wantEnd:    5,
			wantMsg:    "character requires percent-encoding",
		},
		{
			name:       "path raw non-ascii rune spans all bytes",
			pattern:    "/café",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  4,
			wantEnd:    6,
			wantMsg:    "character requires percent-encoding",
		},
		{
			name:       "path invalid utf8 byte spans one byte",
			pattern:    "/a\x80b",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  2,
			wantEnd:    3,
			wantMsg:    "character requires percent-encoding",
		},
		{
			name:       "path raw pipe requires encoding",
			pattern:    "/a|b",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  2,
			wantEnd:    3,
			wantMsg:    "character requires percent-encoding",
		},
		{
			name:       "path raw backslash requires encoding",
			pattern:    "/a\\b",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  2,
			wantEnd:    3,
			wantMsg:    "character requires percent-encoding",
		},
		{
			name:       "path del byte is a control character",
			pattern:    "/a\x7fb",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  2,
			wantEnd:    3,
			wantMsg:    "illegal control character",
		},
		{
			name:       "path query delimiter rejected",
			pattern:    "/search?q=1",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  7,
			wantEnd:    8,
			wantMsg:    "illegal query delimiter in patterns",
		},
		{
			name:       "path fragment delimiter rejected",
			pattern:    "/foo#frag",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  4,
			wantEnd:    5,
			wantMsg:    "illegal fragment delimiter in patterns",
		},
		{
			name:       "path stray closing brace rejected",
			pattern:    "/foo}bar",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  4,
			wantEnd:    5,
			wantMsg:    "unbalanced braces",
		},
		{
			name:       "path dot segment single dot mid",
			pattern:    "/foo/./bar",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  4,
			wantEnd:    7,
			wantMsg:    "unsafe dot segment",
		},
		{
			name:       "path dot segment single dot end",
			pattern:    "/foo/.",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  4,
			wantEnd:    6,
			wantMsg:    "unsafe dot segment",
		},
		{
			name:       "path dot segment double dot mid",
			pattern:    "/foo/../bar",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  4,
			wantEnd:    8,
			wantMsg:    "unsafe dot segment",
		},
		{
			name:       "path dot segment double dot end",
			pattern:    "/foo/..",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  4,
			wantEnd:    7,
			wantMsg:    "unsafe dot segment",
		},
		{
			name:       "path root single dot",
			pattern:    "/.",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  0,
			wantEnd:    2,
			wantMsg:    "unsafe dot segment",
		},
		{
			name:       "path root double dot",
			pattern:    "/..",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  0,
			wantEnd:    3,
			wantMsg:    "unsafe dot segment",
		},
		{
			name:       "unbalanced braces",
			pattern:    "/foo/{bar",
			wantType:   "path",
			wantReason: "syntax",
			wantStart:  5,
			wantEnd:    9,
			wantMsg:    "unbalanced braces",
		},
		{
			name:       "parameter key too large",
			pattern:    "/foo/{abcd}",
			options:    []GlobalOption{WithMaxRouteParamKeyBytes(3)},
			wantType:   "path",
			wantReason: "constraint",
			wantStart:  6,
			wantEnd:    10,
			wantMsg:    "key too large",
		},
		{
			name:       "missing parameter name",
			pattern:    "/foo/{}",
			wantType:   "path",
			wantReason: "parameter",
			wantStart:  5,
			wantEnd:    7,
			wantMsg:    "missing name",
		},
		{
			name:       "illegal character in parameter name",
			pattern:    "/foo/{*bar}",
			wantType:   "path",
			wantReason: "parameter",
			wantStart:  6,
			wantEnd:    7,
			wantMsg:    "illegal character in name",
		},
		{
			name:       "regexp not allowed in optional wildcard",
			pattern:    "/foo/*{any:[A-z]+}",
			wantType:   "path",
			wantReason: "regexp",
			wantStart:  5,
			wantEnd:    18,
			wantMsg:    "not allowed in optional wildcard",
		},
		{
			name:       "regexp feature not enabled",
			pattern:    "/foo/{a:[A-z]+}",
			wantType:   "path",
			wantReason: "regexp",
			wantStart:  8,
			wantEnd:    14,
			wantMsg:    "feature not enabled",
		},
		{
			name:       "regexp missing expression",
			pattern:    "/foo/{a:}",
			options:    []GlobalOption{WithAllowRegexpParam(true)},
			wantType:   "path",
			wantReason: "regexp",
			wantStart:  8,
			wantEnd:    8,
			wantMsg:    "missing expression",
		},
		{
			name:       "regexp compile error",
			pattern:    "/foo/{a:a{5,2}}",
			options:    []GlobalOption{WithAllowRegexpParam(true)},
			wantType:   "path",
			wantReason: "regexp",
			wantStart:  8,
			wantEnd:    14,
			wantMsg:    "error parsing regexp",
		},
		{
			name:       "regexp capture group not allowed",
			pattern:    "/foo/{a:(foo|bar)}",
			options:    []GlobalOption{WithAllowRegexpParam(true)},
			wantType:   "path",
			wantReason: "regexp",
			wantStart:  8,
			wantEnd:    17,
			wantMsg:    "capture group",
		},
		{
			name:       "regexp anchor escape rejected",
			pattern:    "/foo/{a:a)|(?:}",
			options:    []GlobalOption{WithAllowRegexpParam(true)},
			wantType:   "path",
			wantReason: "regexp",
			wantStart:  8,
			wantEnd:    14,
			wantMsg:    "error parsing regexp",
		},
		{
			name:       "regexp too large",
			pattern:    "/foo/{a:[a-z]{1,1000}}",
			options:    []GlobalOption{WithAllowRegexpParam(true)},
			wantType:   "path",
			wantReason: "regexp",
			wantStart:  8,
			wantEnd:    21,
			wantMsg:    "expression too large",
		},
		{
			name:       "regexp exceeds max size",
			pattern:    "/foo/{a:[0-9]+}",
			options:    []GlobalOption{WithAllowRegexpParam(true), WithMaxRegexpSize(3)},
			wantType:   "path",
			wantReason: "regexp",
			wantStart:  8,
			wantEnd:    14,
			wantMsg:    "expression too large",
		},
		{
			name:       "hostname label exceeds 63 chars before dot with param",
			pattern:    strings.Repeat("a", 64) + "{b}.com/",
			wantType:   "hostname",
			wantReason: "constraint",
			wantStart:  0,
			wantEnd:    67,
			wantMsg:    "label exceeds 63 characters",
		},
		{
			name:       "hostname trailing label exceeds 63 chars with param",
			pattern:    strings.Repeat("a", 64) + "{b}/",
			wantType:   "hostname",
			wantReason: "constraint",
			wantStart:  0,
			wantEnd:    67,
			wantMsg:    "label exceeds 63 characters",
		},
		{
			name:       "hostname middle label exceeds 63 chars with param",
			pattern:    "a.b." + strings.Repeat("a", 64) + "{b}.com/",
			wantType:   "hostname",
			wantReason: "constraint",
			wantStart:  4,
			wantEnd:    71,
			wantMsg:    "label exceeds 63 characters",
		},
		{
			name:       "path control character in parameter name",
			pattern:    "/foo/{a\x00b}",
			wantType:   "path",
			wantReason: "parameter",
			wantStart:  7,
			wantEnd:    8,
			wantMsg:    "illegal control character in name",
		},
		{
			name:       "hostname control character in parameter name",
			pattern:    "{a\x7fb}.com/",
			wantType:   "hostname",
			wantReason: "parameter",
			wantStart:  2,
			wantEnd:    3,
			wantMsg:    "illegal control character in name",
		},
		{
			name:       "path control character in regex constraint",
			pattern:    "/x/{a:a\nb}",
			options:    []GlobalOption{WithAllowRegexpParam(true)},
			wantType:   "path",
			wantReason: "regexp",
			wantStart:  7,
			wantEnd:    8,
			wantMsg:    "illegal control character",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := MustRouter(tc.options...)
			_, _, err := f.parsePattern(tc.pattern)
			require.Error(t, err)
			var pe *PatternError
			require.ErrorAs(t, err, &pe)
			assert.Equal(t, tc.wantType, pe.Type)
			assert.Equal(t, tc.wantReason, pe.Reason)
			assert.Equal(t, tc.wantStart, pe.Start)
			assert.Equal(t, tc.wantEnd, pe.End)
			assert.Contains(t, pe.Error(), tc.wantMsg)
		})
	}
}

func TestPatternError_PointerLine(t *testing.T) {
	t.Run("multi-byte rune gets a single caret", func(t *testing.T) {
		f := MustRouter()
		_, _, err := f.parsePattern("/café")
		require.Error(t, err)
		var pe *PatternError
		require.ErrorAs(t, err, &pe)
		lines := strings.Split(pe.Error(), "\n")
		require.Len(t, lines, 3)
		assert.Equal(t, "      /café", lines[1])
		assert.Equal(t, "          ^", lines[2])
	})

	t.Run("ascii span keeps one caret per character", func(t *testing.T) {
		f := MustRouter()
		_, _, err := f.parsePattern("/a/./b")
		require.Error(t, err)
		var pe *PatternError
		require.ErrorAs(t, err, &pe)
		lines := strings.Split(pe.Error(), "\n")
		require.Len(t, lines, 3)
		assert.Equal(t, "      /a/./b", lines[1])
		assert.Equal(t, "        ^^^", lines[2])
	})

	t.Run("multi-byte prefix keeps the caret aligned", func(t *testing.T) {
		pe := &PatternError{Pattern: "/héllo wörld", Reason: "syntax", Start: 7, End: 8}
		lines := strings.Split(pe.Error(), "\n")
		require.Len(t, lines, 3)
		assert.Equal(t, "      /héllo wörld", lines[1])
		assert.Equal(t, "            ^", lines[2])
	})

	t.Run("out of range offsets clamp without panic", func(t *testing.T) {
		pe := &PatternError{Pattern: "/a", Reason: "syntax", Start: -1, End: 10}
		lines := strings.Split(pe.Error(), "\n")
		require.Len(t, lines, 3)
		assert.Equal(t, "      ^^", lines[2])
	})
}

func TestPatternError_Unwrap(t *testing.T) {
	t.Run("regexp compile error wraps underlying error", func(t *testing.T) {
		f := MustRouter(WithAllowRegexpParam(true))
		_, _, err := f.parsePattern("/foo/{a:a{5,2}}")
		require.Error(t, err)
		var pe *PatternError
		require.ErrorAs(t, err, &pe)
		var syntaxErr *syntax.Error
		assert.ErrorAs(t, pe, &syntaxErr)
	})
	t.Run("regexp too large wraps ErrLarge", func(t *testing.T) {
		f := MustRouter(WithAllowRegexpParam(true))
		_, _, err := f.parsePattern("/foo/{a:[a-z]{1,1000}}")
		require.Error(t, err)
		var syntaxErr *syntax.Error
		require.ErrorAs(t, err, &syntaxErr)
		assert.Equal(t, syntax.ErrLarge, syntaxErr.Code)
	})
	t.Run("non-regexp error returns nil on unwrap", func(t *testing.T) {
		f := MustRouter()
		_, _, err := f.parsePattern("/foo/./bar")
		require.Error(t, err)
		var pe *PatternError
		require.ErrorAs(t, err, &pe)
		assert.Nil(t, pe.Unwrap())
	})
	t.Run("empty pattern returns ErrInvalidRoute", func(t *testing.T) {
		f := MustRouter()
		_, _, err := f.parsePattern("")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidRoute)
		var pe *PatternError
		assert.False(t, errors.As(err, &pe))
	})
}

func Test_minRegexpSize(t *testing.T) {
	cases := []struct {
		expr  string
		limit int
		want  int
	}{
		{expr: `a`, limit: 1000, want: 1},
		{expr: `abc`, limit: 1000, want: 3},
		{expr: `[a-z]+`, limit: 1000, want: 1},
		{expr: `a{3}`, limit: 1000, want: 3},
		{expr: `a{2,5}`, limit: 1000, want: 5},
		{expr: `a{2,}`, limit: 1000, want: 2},
		{expr: `a{0}`, limit: 1000, want: 1},
		{expr: `a*`, limit: 1000, want: 1},
		{expr: `(?:)`, limit: 1000, want: 1},
		{expr: `(?:a|bc)`, limit: 1000, want: 3},
		{expr: `\b`, limit: 1000, want: 1},
		{expr: `(?i)k`, limit: 1000, want: 1},
		{expr: `[^\x00-\x{10FFFF}]`, limit: 1000, want: 1},
		{expr: `(?:a{0,3}){2,}`, limit: 1000, want: 6},
		{expr: `(?:(?:)*){10}`, limit: 1000, want: 10},
		{expr: `(?:a{10}){100}`, limit: 1000, want: 1000},
		{expr: `(?:a{10}){100}`, limit: 999, want: 1000},
		{expr: strings.Repeat("a{1000}", 3000), limit: 1000, want: 1001},
	}

	for _, tc := range cases {
		t.Run(tc.expr[:min(len(tc.expr), 32)], func(t *testing.T) {
			re, err := syntax.Parse(tc.expr, syntax.Perl)
			require.NoError(t, err)
			got := minRegexpSize(re, tc.limit)
			assert.Equal(t, tc.want, got)
			if got <= tc.limit {
				prog, err := syntax.Compile(re.Simplify())
				require.NoError(t, err)
				assert.LessOrEqual(t, got, len(prog.Inst))
			}
		})
	}
}
