package fox

import (
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_iTree_lookup_Domain(t *testing.T) {
	cases := []struct {
		name       string
		routes     []string
		host       string
		path       string
		wantPath   string
		wantTsr    bool
		wantParams []Param
	}{
		{
			name: "static hostname with complex overlapping route with static priority",
			routes: []string{
				"example.com/foo/bar/baz/{$1}/jo",
				"example.com/foo/+{any}/baz/{$1}/jo",
				"example.com/foo/{ps}/baz/{$1}/jo",
			},
			host:     "example.com",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "example.com/foo/bar/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "infix hostname wildcard with consecutive capturing single dot",
			routes: []string{
				"a.+{any}.com/bar",
			},
			host:     "a...com",
			path:     "/bar",
			wantPath: "a.+{any}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: ".",
				},
			},
		},
		{
			name: "sufix hostname wildcard with hostname ending with single dot",
			routes: []string{
				"+{any}/bar",
			},
			host:     "a.com.",
			path:     "/bar",
			wantPath: "+{any}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a.com",
				},
			},
		},
		{
			name: "static hostname with complex overlapping route with static priority and regexp",
			routes: []string{
				"example.com/foo/bar/baz/{$1:[0-9]}/jo",
				"example.com/foo/+{any:.*}/baz/{$1:.*}/jo",
				"example.com/foo/{ps:.*}/baz/{$1:.*}/jo",
			},
			host:     "example.com",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "example.com/foo/bar/baz/{$1:[0-9]}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "static hostname with complex overlapping route with param priority",
			routes: []string{
				"example.com/foo/bar/baz/{$1}/jo",
				"example.com/foo/+{any}/baz/{$1}/jo",
				"example.com/foo/{ps}/baz/{$1}/jo",
			},
			host:     "example.com",
			path:     "/foo/bam/baz/1/jo",
			wantPath: "example.com/foo/{ps}/baz/{$1}/jo",
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
			name: "static hostname with complex overlapping route with param priority and regexp",
			routes: []string{
				"example.com/foo/bar/baz/{$1:[0-9]}/jo",
				"example.com/foo/+{any:.*}/baz/{$1:.*}/jo",
				"example.com/foo/{ps:.*}/baz/{$1:.*}/jo",
			},
			host:     "example.com",
			path:     "/foo/bam/baz/1/jo",
			wantPath: "example.com/foo/{ps:.*}/baz/{$1:.*}/jo",
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
			name: "wildcard hostname with complex overlapping route with static priority",
			routes: []string{
				"example.com/foo/bar/baz/{$1}/jo",
				"{any}.com/foo/+{any}/baz/{$1}/jo",
				"example.{tld}/foo/{ps}/baz/{$1}/jo",
			},
			host:     "example.com",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "example.com/foo/bar/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "wildcard hostname with complex overlapping route with static priority an regexp",
			routes: []string{
				"example.com/foo/bar/baz/{$1}/jo",
				"{any:.*}.com/foo/+{any}/baz/{$1}/jo",
				"example.{tld}/foo/{ps}/baz/{$1}/jo",
			},
			host:     "example.com",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "example.com/foo/bar/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "wildcard hostname with complex overlapping route with static priority (case-insensitive)",
			routes: []string{
				"example.com/foo/bar/baz/{$1}/jo",
				"{any}.com/foo/+{any}/baz/{$1}/jo",
				"example.{tld}/foo/{ps}/baz/{$1}/jo",
			},
			host:     "EXAMPLE.COM",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "example.com/foo/bar/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "wildcard hostname with complex overlapping route with param priority",
			routes: []string{
				"{sub}.com/foo/bar/baz/{$1}/jo",
				"example.{tld}/foo/+{any}/baz/{$1}/jo",
				"example.com/foo/{ps}/baz/{$1}/jo",
			},
			host:     "example.com",
			path:     "/foo/bam/baz/1/jo",
			wantPath: "example.com/foo/{ps}/baz/{$1}/jo",
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
			name: "hostname not matching fallback to param",
			routes: []string{
				"{a}/foo",
				"fooxyz/foo",
				"foobar/foo",
			},
			host:     "foo",
			path:     "/foo",
			wantPath: "{a}/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
			},
		},
		{
			name: "hostname not matching fallback to param with regexp",
			routes: []string{
				"{a:.*}/foo",
				"fooxyz/foo",
				"foobar/foo",
			},
			host:     "foo",
			path:     "/foo",
			wantPath: "{a:.*}/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
			},
		},
		{
			name: "static priority in hostname",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
			},
			host:     "foo.b.c",
			path:     "/foo",
			wantPath: "{a}.b.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
			},
		},
		{
			name: "static priority in hostname with regexp",
			routes: []string{
				"{a:foo}.{b}.{c}/foo",
				"{a:foo}.{b}.c/foo",
				"{a:foo}.b.c/foo",
			},
			host:     "foo.b.c",
			path:     "/foo",
			wantPath: "{a:foo}.b.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
			},
		},
		{
			name: "static priority in hostname (case-insensitive)",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
			},
			host:     "FOO.B.C",
			path:     "/foo",
			wantPath: "{a}.b.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "FOO",
				},
			},
		},
		{
			name: "static priority in hostname (case-insensitive) with regexp",
			routes: []string{
				"{a:[A-z]+}.{b}.{c}/foo",
				"{a:[A-z]+}.{b}.c/foo",
				"{a:[A-z]+}.b.c/foo",
			},
			host:     "FOO.B.C",
			path:     "/foo",
			wantPath: "{a:[A-z]+}.b.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "FOO",
				},
			},
		},
		{
			name: "regexp hostname params match all cases with static label priority",
			routes: []string{
				"{a:[a-z]+}.b.c/foo",
				"{a:[A-Z]+}.{b:[A-Z]+}.{c:[A-Z]+}/foo",
				"{a:[A-Z]+}.{b:[a-z]+}.c/foo",
			},
			host:     "FOO.B.C",
			path:     "/foo",
			wantPath: "{a:[a-z]+}.b.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "FOO",
				},
			},
		},
		{
			name: "static priority in hostname",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
			},
			host:     "foo.bar.c",
			path:     "/foo",
			wantPath: "{a}.{b}.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "b",
					Value: "bar",
				},
			},
		},
		{
			name: "static priority in hostname",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
			},
			host:     "foo.bar.baz",
			path:     "/foo",
			wantPath: "{a}.{b}.{c}/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "b",
					Value: "bar",
				},
				{
					Key:   "c",
					Value: "baz",
				},
			},
		},
		{
			name: "fallback to path only",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
				"/foo/bar",
			},
			host:       "foo.bar.baz",
			path:       "/foo/bar",
			wantPath:   "/foo/bar",
			wantTsr:    false,
			wantParams: Params(nil),
		},
		{
			name: "regexp priority",
			routes: []string{
				"{a:.*}.{b}.{c}/foo",
				"{a:[A-z]+}.{b}.c/foo",
				"{a:a}.b.c/foo",
				"/foo/bar",
			},
			host:     "a.b.c",
			path:     "/foo",
			wantPath: "{a:.*}.{b}.{c}/foo",
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
					Value: "c",
				},
			},
		},
		{
			name: "regexp priority but backtrack to most specific",
			routes: []string{
				"{a:.*}.{b}.{c}/foo/x",
				"{a:[A-z]+}.{b}.c/foo",
				"{a:a}.b.c/foo",
				"/foo/bar",
			},
			host:     "a.b.c",
			path:     "/foo",
			wantPath: "{a:[A-z]+}.{b}.c/foo",
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
			},
		},
		{
			name: "regexp priority with tsr",
			routes: []string{
				"{a:.*}.{b}.{c}/foo",
				"{a:[a-z]+}.{b}.c/foo",
				"{a:a}.b.c/foo",
				"/foo/bar",
			},
			host:     "A.b.c",
			path:     "/foo/",
			wantPath: "{a:.*}.{b}.{c}/foo",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "a",
					Value: "A",
				},
				{
					Key:   "b",
					Value: "b",
				},
				{
					Key:   "c",
					Value: "c",
				},
			},
		},
		{
			name: "regexp priority then next static",
			routes: []string{
				"{a:.*}.{b}.{c}/foo",
				"{a:.*}.{b}.c/foo",
				"{a:.*}.b.c/foo",
				"/foo/bar",
			},
			host:     "a.b.c",
			path:     "/foo",
			wantPath: "{a:.*}.b.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
			},
		},
		{
			name: "regexp priority then param then next static",
			routes: []string{
				"{a:.*}.{b}.{c}/foo",
				"{a:.*}.{b}.c/foo",
				"{a:.*}.b.c/foo",
				"/foo/bar",
			},
			host:     "a.x.c",
			path:     "/foo",
			wantPath: "{a:.*}.{b}.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
				{
					Key:   "b",
					Value: "x",
				},
			},
		},
		{
			name: "regexp priority with backtrack to most specific",
			routes: []string{
				"{a:.*}.{b}.{c}/foo",
				"{a:[A-z]+}.{b}.c/foo",
				"{a:a}.b.c/{bar}",
				"/foo/bar",
			},
			host:     "a.b.c",
			path:     "/bar",
			wantPath: "{a:a}.b.c/{bar}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
				{
					Key:   "bar",
					Value: "bar",
				},
			},
		},
		{
			name: "regexp priority with backtrack to path only",
			routes: []string{
				"{a:.*}.{b}.{c}/foo",
				"{a:[A-z]+}.{b}.c/foo",
				"{a:a}.b.c/{bar}",
				"/foo/bar",
			},
			host:       "a.b.c",
			path:       "/foo/bar",
			wantPath:   "/foo/bar",
			wantTsr:    false,
			wantParams: Params(nil),
		},
		{
			name: "fallback to path only (case-insenitive)",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
				"/foo/bar",
			},
			host:       "FOO.BAR.BAZ",
			path:       "/foo/bar",
			wantPath:   "/foo/bar",
			wantTsr:    false,
			wantParams: Params(nil),
		},
		{
			name: "fallback to path only with param",
			routes: []string{
				"{a}.{b}.{c}/{d}",
				"{a}.{b}.c/{d}",
				"{a}.b.c/{d}",
				"/{a}/bar",
			},
			host:     "foo.bar.baz",
			path:     "/foo/bar",
			wantPath: "/{a}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
			},
		},
		{
			name: "fallback to tsr with hostname priority",
			routes: []string{
				"{a}.{b}.{c}/{d}",
				"{a}.{b}.c/{d}",
				"{a}.b.c/{path}/bar/",
				"/{a}/bar/",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "{a}.b.c/{path}/bar/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "path",
					Value: "john",
				},
			},
		},
		{
			name: "path priority with no hostname match",
			routes: []string{
				"{a}.{b}.{c}/{d}",
				"{a}.{b}.c/{d}",
				"{a}.b.c/{path}/bar/x",
				"/{a}/bar",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "/{a}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "john",
				},
			},
		},
		{
			name: "fallback to must specific hostname with path param and regexp",
			routes: []string{
				"{a:.*}.{b:.*}.{c}/{d}",
				"{a:foo}.{b}.c/{d}",
				"{a:[A-z]+}.b.c/{path}/bar/",
				"/{a}/bar/",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "{a:[A-z]+}.b.c/{path}/bar/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "path",
					Value: "john",
				},
			},
		},
		{
			name: "fallback to must specific hostname with path param and regexp",
			routes: []string{
				"{a:.*}.{b:.*}.{c}/{d}",
				"{a:foo}.{b}.c/{d}",
				"{a:[A-z]+}.b.c/{path}/bar/",
				"/{a}/ba",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "{a:[A-z]+}.b.c/{path}/bar/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "path",
					Value: "john",
				},
			},
		},
		{
			name: "fallback to must specific path with path param and regexp",
			routes: []string{
				"{a:.*}.{b:.*}.{c}/{d}",
				"{a:foo}.{b}.c/{d}",
				"{a:[A-z]+}.b.c/{path}/bar/x",
				"/{a}/bar",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "/{a}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "john",
				},
			},
		},
		{
			name: "fallback to must specific hostname with path param and regexp",
			routes: []string{
				"{a:.*}.{b:.*}.{c}/{d}",
				"{a:foo}.{b}.c/{d}",
				"{a:[A-z]+}.b.c/{path}/bar/",
				"/{a:^$}/bar",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "{a:[A-z]+}.b.c/{path}/bar/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "path",
					Value: "john",
				},
			},
		},
		{
			name: "fallback to path only with param",
			routes: []string{
				"{a:.*}.{b:.*}.{c}/{d}",
				"{a:foo}.{b}.c/{d}",
				"{a:[A-z]+}.b.c/{path}/bar/joh",
				"/{a}/bar",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "/{a}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "john",
				},
			},
		},
		{
			name: "fallback to tsr with hostname priority (case-insensitive)",
			routes: []string{
				"{a}.{b}.{c}/{d}",
				"{a}.{b}.c/{d}",
				"{a}.b.c/{path}/bar/",
				"/{a}/barr",
			},
			host:     "FOO.B.C",
			path:     "/john/bar",
			wantPath: "{a}.b.c/{path}/bar/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "a",
					Value: "FOO",
				},
				{
					Key:   "path",
					Value: "john",
				},
			},
		},
		{
			name: "simple hostname suffix wildcard",
			routes: []string{
				"+{any}/bar",
			},
			host:     "foo.com",
			path:     "/bar",
			wantPath: "+{any}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "foo.com",
				},
			},
		},
		{
			name: "simple hostname suffix wildcard with regexp",
			routes: []string{
				"+{any:[A-Z.]+}/bar",
			},
			host:     "FOO.COM",
			path:     "/bar",
			wantPath: "+{any:[A-Z.]+}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "FOO.COM",
				},
			},
		},
		{
			name: "simple prefix wildcard",
			routes: []string{
				"+{any}.com/bar",
			},
			host:     "a.b.com",
			path:     "/bar",
			wantPath: "+{any}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a.b",
				},
			},
		},
		{
			name: "simple prefix wildcard overlap static",
			routes: []string{
				"a.b.com/bar",
				"+{any}.com/bar",
			},
			host:       "a.b.com",
			path:       "/bar",
			wantPath:   "a.b.com/bar",
			wantTsr:    false,
			wantParams: Params(nil),
		},
		{
			name: "simple prefix wildcard overlap static with fallback",
			routes: []string{
				"a.b.com/barr",
				"+{any}.com/bar",
			},
			host:     "a.b.com",
			path:     "/bar",
			wantPath: "+{any}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a.b",
				},
			},
		},
		{
			name: "simple prefix wildcard with regexp",
			routes: []string{
				"+{any:[A-Z.]+}.com/bar",
			},
			host:     "A.B.com",
			path:     "/bar",
			wantPath: "+{any:[A-Z.]+}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "A.B",
				},
			},
		},
		{
			name: "infix wildcard favors the hostname with the longest static tail",
			routes: []string{
				"+{h}.x.com/foo",
				"+{h}.com/foo",
			},
			host:     "a.x.com",
			path:     "/foo",
			wantPath: "+{h}.x.com/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "h",
					Value: "a",
				},
			},
		},
		{
			name: "simple infix wildcard",
			routes: []string{
				"example.+{any}.com/bar",
			},
			host:     "example.foo.bar.com",
			path:     "/bar",
			wantPath: "example.+{any}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "foo.bar",
				},
			},
		},
		{
			name: "simple infix wildcard with regexp",
			routes: []string{
				"example.+{any:[A-Z.]+}.com/bar",
			},
			host:     "example.FOO.BAR.com",
			path:     "/bar",
			wantPath: "example.+{any:[A-Z.]+}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "FOO.BAR",
				},
			},
		},
		{
			name: "prefix wildcard with params",
			routes: []string{
				"+{any}.{tld}/bar",
			},
			host:     "a.b.com",
			path:     "/bar",
			wantPath: "+{any}.{tld}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a.b",
				},
				{
					Key:   "tld",
					Value: "com",
				},
			},
		},
		{
			name: "infix wildcard with params",
			routes: []string{
				"{first}.+{any}.{tld}/bar",
			},
			host:     "foo.s1.s2.s3.com",
			path:     "/bar",
			wantPath: "{first}.+{any}.{tld}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "first",
					Value: "foo",
				},
				{
					Key:   "any",
					Value: "s1.s2.s3",
				},
				{
					Key:   "tld",
					Value: "com",
				},
			},
		},
		{
			name: "suffix wildcard with params",
			routes: []string{
				"{first}.{second}.+{any}/bar",
			},
			host:     "first.second.third.com",
			path:     "/bar",
			wantPath: "{first}.{second}.+{any}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "first",
					Value: "first",
				},
				{
					Key:   "second",
					Value: "second",
				},
				{
					Key:   "any",
					Value: "third.com",
				},
			},
		},
		{
			name: "priority to params",
			routes: []string{
				"+{any}.b.com/bar",
				"{ps}.b.com/bar",
			},
			host:     "a.b.com",
			path:     "/bar",
			wantPath: "{ps}.b.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name: "fallback to catch-all with leading dot",
			routes: []string{
				"+{any}/bar",
				"{ps}/bar",
			},
			host:     ".com",
			path:     "/bar",
			wantPath: "+{any}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: ".com",
				},
			},
		},
		{
			name: "eval param with wildcard fallback",
			routes: []string{
				"+{any}.b.com/bar",
				"{ps}.b.com/bar",
			},
			host:     "foo.b.b.com",
			path:     "/bar",
			wantPath: "+{any}.b.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "foo.b",
				},
			},
		},
		{
			name: "priority to infix wildcard",
			routes: []string{
				"a.+{any}.com/bar",
				"a.+{any}/bar",
			},
			host:     "a.bar.baz.com",
			path:     "/bar",
			wantPath: "a.+{any}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "bar.baz",
				},
			},
		},
		{
			name: "eval infix with suffix fallback",
			routes: []string{
				"a.+{any}.com/bar",
				"a.+{any}/bar",
			},
			host:     "a.bar.baz.ch",
			path:     "/bar",
			wantPath: "a.+{any}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "bar.baz.ch",
				},
			},
		},
		{
			name: "priority to regexp wildcard",
			routes: []string{
				"a.+{3}.com/bar",
				"a.+{1:[A-z.]+}.com/bar",
				"a.+{2:[0-9.]+}.com/bar",
			},
			host:     "a.b.c.com",
			path:     "/bar",
			wantPath: "a.+{1:[A-z.]+}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "1",
					Value: "b.c",
				},
			},
		},
		{
			name: "priority to next regexp wildcard",
			routes: []string{
				"a.+{3}.com/bar",
				"a.+{1:[A-z.]+}.com/bar",
				"a.+{2:[0-9.]+}.com/bar",
			},
			host:     "a.1.2.com",
			path:     "/bar",
			wantPath: "a.+{2:[0-9.]+}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "2",
					Value: "1.2",
				},
			},
		},
		{
			name: "fallback to non-regexp infix wildcard",
			routes: []string{
				"a.+{3}.com/bar",
				"a.+{1:[A-z.]+}.com/bar",
				"a.+{2:[0-9.]+}.com/bar",
			},
			host:     "a.b.2.com",
			path:     "/bar",
			wantPath: "a.+{3}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "3",
					Value: "b.2",
				},
			},
		},
		{
			name: "fallback to tsr with hostname priority and prefix wildcard",
			routes: []string{
				"+{a}.{b}.{c}/{d}",
				"+{a}.{b}.c/{d}",
				"+{a}.b.c/{path}/bar/",
				"/{a}/barr",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "+{a}.b.c/{path}/bar/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "path",
					Value: "john",
				},
			},
		},
		{
			name: "fallback to path priority with prefix wildcard",
			routes: []string{
				"+{a}.{b}.{c}/{d}",
				"+{a}.{b}.c/{d}",
				"+{a}.b.c/{path}/bar/x",
				"/{path}/bar",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "/{path}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "path",
					Value: "john",
				},
			},
		},
		{
			name: "fallback to must specific hostname with path param, wildcard and regexp",
			routes: []string{
				"{a:.*}.{b:.*}.+{c:nomatch}/john/bar",
				"+{a:nomatch}.{b}.c/{d}",
				"+{a:[A-z]+}.b.c/{path}/bar/",
				"/{a:^$}/bar",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "+{a:[A-z]+}.b.c/{path}/bar/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "path",
					Value: "john",
				},
			},
		},
		{
			name: "fallback to must specific hostname with wildcard and regexp priority",
			routes: []string{
				"{a:.*}.{b:.*}.+{c:nomatch}/john/bar",
				"+{a:foo}.{b}.c/{d}/bar",
				"+{a:[A-z]+}.b.c/{path}/bar/",
				"/{a:^$}/bar",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "+{a:foo}.{b}.c/{d}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "b",
					Value: "b",
				},
				{
					Key:   "d",
					Value: "john",
				},
			},
		},
		{
			name: "direct to must specific with wildcard and regexp",
			routes: []string{
				"{a:.*}.{b:.*}.+{c:.*}/john/bar",
				"+{a:foo}.{b}.c/{d}/bar",
				"+{a:[A-z]+}.b.c/{path}/bar/",
				"/{a:^$}/bar",
			},
			host:     "foo.b.c.com",
			path:     "/john/bar",
			wantPath: "{a:.*}.{b:.*}.+{c:.*}/john/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "b",
					Value: "b",
				},
				{
					Key:   "c",
					Value: "c.com",
				},
			},
		},
		{
			name: "fallback to plain param after regexp param subtree dead end",
			routes: []string{
				"{p:[a-z]+}.x.example.com/",
				"{p}.y.example.com/",
			},
			host:     "abc.y.example.com",
			path:     "/",
			wantPath: "{p}.y.example.com/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "p",
					Value: "abc",
				},
			},
		},
		{
			name: "fallback to param hostname when static hostname path does not match",
			routes: []string{
				"foo.example.com/y",
				"{p}.example.com/x",
			},
			host:     "foo.example.com",
			path:     "/x",
			wantPath: "{p}.example.com/x",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "p",
					Value: "foo",
				},
			},
		},
		{
			name: "suffix hostname wildcard does not capture path bytes on path fallback",
			routes: []string{
				"+{x}/foo",
			},
			host: "zzz",
			path: "/bar/foo",
		},
		{
			name: "suffix hostname wildcard does not capture path bytes with empty host",
			routes: []string{
				"+{x}/foo",
			},
			host: "",
			path: "/bar/foo",
		},
		{
			name: "boundary hostname wildcard does not capture path bytes",
			routes: []string{
				"example.com/p",
				"example.com+{y}/q",
			},
			host: "example.com",
			path: "/zzz/q",
		},
		{
			name: "root hostname param does not capture asterisk form target",
			routes: []string{
				"{tld}/",
			},
			host: "zzz",
			path: "*",
		},
		{
			name: "path fallback to path only route with hostname wildcard sibling",
			routes: []string{
				"+{x}/foo",
				"/bar",
			},
			host:     "zzz",
			path:     "/bar",
			wantPath: "/bar",
			wantTsr:  false,
		},
		{
			name: "host with a slash does not match the path of a hostname route",
			routes: []string{
				"a.api/x",
				"a.api/x/y",
				"/y",
			},
			host:     "a.api/x",
			path:     "/y",
			wantPath: "/y",
			wantTsr:  false,
		},
		{
			name: "host with a slash captured by a hostname param falls back to path only route",
			routes: []string{
				"{sub}.example.com/x",
				"/x",
			},
			host:     "a/b.example.com",
			path:     "/x",
			wantPath: "/x",
			wantTsr:  false,
		},
		{
			name: "boundary hostname static path with hostname wildcard sibling",
			routes: []string{
				"example.com/p",
				"example.com+{y}/q",
			},
			host:     "example.com",
			path:     "/p",
			wantPath: "example.com/p",
			wantTsr:  false,
		},
		{
			name: "suffix hostname wildcard extending static hostname",
			routes: []string{
				"example.com/p",
				"example.com+{y}/q",
			},
			host:     "example.comfoo",
			path:     "/q",
			wantPath: "example.com+{y}/q",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "y",
					Value: "foo",
				},
			},
		},
		{
			name: "fallback to next suffix hostname wildcard when path subtree does not match",
			routes: []string{
				"a.+{x:[b.]+}/foo",
				"a.+{y}/bar",
			},
			host:     "a.b.b",
			path:     "/bar",
			wantPath: "a.+{y}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "y",
					Value: "b.b",
				},
			},
		},
		{
			name: "regexp suffix hostname wildcard priority when path subtree matches",
			routes: []string{
				"a.+{x:[b.]+}/foo",
				"a.+{y}/foo",
			},
			host:     "a.b.b",
			path:     "/foo",
			wantPath: "a.+{x:[b.]+}/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "x",
					Value: "b.b",
				},
			},
		},
		{
			name: "regexp hostname param matches case-insensitively",
			routes: []string{
				"{a:[a-z]+}.example.com/",
			},
			host:     "FOO.example.com",
			path:     "/",
			wantPath: "{a:[a-z]+}.example.com/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "FOO",
				},
			},
		},
		{
			name: "regexp hostname wildcard matches case-insensitively",
			routes: []string{
				"+{sub:[a-z.]+}.example.com/",
			},
			host:     "A.B.example.com",
			path:     "/",
			wantPath: "+{sub:[a-z.]+}.example.com/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "sub",
					Value: "A.B",
				},
			},
		},
		{
			name: "no match when no suffix hostname wildcard path subtree matches",
			routes: []string{
				"a.+{x:[b.]+}/foo",
				"a.+{y}/bar",
			},
			host: "a.b.b",
			path: "/nope",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true), WithTrailingSlash(RelaxedSlash))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			idx, n, tsr := tree.lookup(http.MethodGet, tc.host, tc.path, c, false)
			if tc.wantPath == "" {
				require.Nil(t, n)
				return
			}
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.routes[idx].pattern.str)
			assert.Equal(t, tc.wantTsr, tsr)
			c.route = n.routes[idx]
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}
}

func Test_iTree_lookup_Matchers(t *testing.T) {

	type route struct {
		pattern  string
		matchers []Matcher
	}

	cases := []struct {
		name        string
		routes      []route
		host        string
		path        string
		wantPattern string
		wantTsr     bool
		wantParams  []Param
	}{
		{
			name: "tsr on hostname route after failing all query match",
			routes: []route{
				{pattern: "example.com/foo/bar/"},
				{pattern: "/foo/bar", matchers: []Matcher{QueryMatcher{"a", "b"}}},
			},
			host:        "example.com",
			path:        "/foo/bar",
			wantPattern: "example.com/foo/bar/",
			wantTsr:     true,
		},
		{
			name: "tsr on hostname+matcher route after failing all query match",
			routes: []route{
				{pattern: "example.com/foo/bar/", matchers: []Matcher{QueryMatcher{"c", "d"}}},
				{pattern: "/foo/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
			},
			host:        "example.com",
			path:        "/foo/bar?c=d",
			wantPattern: "example.com/foo/bar/",
			wantTsr:     true,
		},
		{
			name: "tsr on catch-all+matcher route after failing all query match",
			routes: []route{
				{pattern: "/foo/+{any}/", matchers: []Matcher{QueryMatcher{"c", "d"}}},
				{pattern: "/foo/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
			},
			path:        "/foo/bar?c=d",
			wantPattern: "/foo/+{any}/",
			wantTsr:     true,
			wantParams: []Param{
				{
					Key:   "any",
					Value: "bar",
				},
			},
		},
		{
			name: "fallback on catch-all+matcher route after failing all query match",
			routes: []route{
				{pattern: "/foo/+{any}", matchers: []Matcher{QueryMatcher{"c", "d"}}},
				{pattern: "/foo/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
			},
			path:        "/foo/bar?c=d",
			wantPattern: "/foo/+{any}",
			wantParams: []Param{
				{
					Key:   "any",
					Value: "bar",
				},
			},
		},
		{
			name: "fallback on catch-all+matcher route after failing all query match",
			routes: []route{
				{pattern: "/foo/{name}", matchers: []Matcher{QueryMatcher{"c", "d"}}},
				{pattern: "/foo/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
			},
			path:        "/foo/bar?c=d",
			wantPattern: "/foo/{name}",
			wantParams: []Param{
				{
					Key:   "name",
					Value: "bar",
				},
			},
		},
		{
			name: "fallback on catch-all+matchers route after failing all query match",
			routes: []route{
				{pattern: "/foo/{name}/baz", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
				{pattern: "/foo/{id}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
				{pattern: "/foo/{id}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"e", "f"}}},
				{pattern: "/foo/+{any}", matchers: []Matcher{QueryMatcher{"a", "b"}}},
			},
			path:        "/foo/bar/baz?a=b",
			wantPattern: "/foo/+{any}",
			wantParams: []Param{
				{
					Key:   "any",
					Value: "bar/baz",
				},
			},
		},
		{
			name: "tsr on hostname route after failing one query match",
			routes: []route{
				{pattern: "example.com/foo/bar/"},
				{pattern: "/foo/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
			},
			host:        "example.com",
			path:        "/foo/bar?a=b",
			wantPattern: "example.com/foo/bar/",
			wantTsr:     true,
		},
		{
			name: "no match with hostname but path with matchers",
			routes: []route{
				{pattern: "example.com/foo/bar/baz"},
				{pattern: "/foo/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
			},
			host:        "example.com",
			path:        "/foo/bar?a=b&c=d",
			wantPattern: "/foo/bar",
		},
		{
			name: "no match with hostname but path with param backtrack",
			routes: []route{
				{pattern: "example.com/{name}/bar/baz"},
				{pattern: "/foo/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
			},
			host:        "example.com",
			path:        "/foo/bar?a=b&c=d",
			wantPattern: "/foo/bar",
		},
		{
			name: "match with multiple same query matchers",
			routes: []route{
				{pattern: "example.com/{name}/bar/baz"},
				{pattern: "/foo/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"a", "b"}}},
			},
			host:        "example.com",
			path:        "/foo/bar?a=b",
			wantPattern: "/foo/bar",
		},
		{
			name: "match many query matchers",
			routes: []route{
				{pattern: "example.com/{name}/bar/baz"},
				{pattern: "/foo/bar", matchers: []Matcher{
					QueryMatcher{"a", "b"},
					QueryMatcher{"c", "d"},
					QueryMatcher{"e", "f"},
					QueryMatcher{"g", "h"},
					QueryMatcher{"i", "j"},
					QueryMatcher{"k", "l"},
				}},
			},
			host:        "example.com",
			path:        "/foo/bar?e=f&k=l&a=b&c=d&g=h&i=j",
			wantPattern: "/foo/bar",
		},
		{
			name: "fallback to tsr after failing matching multiple same level routes",
			routes: []route{
				{pattern: "example.com/{name}/bar/"},
				{pattern: "/{id}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}}},
				{pattern: "/{id}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
				{pattern: "/{id}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}, QueryMatcher{"e", "f"}}},
			},
			host:        "example.com",
			path:        "/foo/bar?a=a&c=d&e=f",
			wantPattern: "example.com/{name}/bar/",
			wantTsr:     true,
			wantParams: []Param{
				{
					Key:   "name",
					Value: "foo",
				},
			},
		},
		{
			name: "fallback to must specific hostname with wildcard, regexp priority and matchers",
			routes: []route{
				{pattern: "{a:.*}.{b:.*}.+{c:.*}/john/bar", matchers: []Matcher{QueryMatcher{"a", "b"}}},
				{pattern: "+{a:[A-z]+}.b.c/{path}/bar/", matchers: []Matcher{QueryMatcher{"d", "e"}}},
				{pattern: "+{a:foo}.{b}.c/{d}/bar"},
				{pattern: "/{a:^$}/bar"},
			},
			host:        "foo.b.c",
			path:        "/john/bar?b=c",
			wantPattern: "+{a:foo}.{b}.c/{d}/bar",
			wantTsr:     false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "b",
					Value: "b",
				},
				{
					Key:   "d",
					Value: "john",
				},
			},
		},
		{
			name: "match must specific hostname with wildcard, regexp priority and matchers",
			routes: []route{
				{pattern: "{a:.*}.{b:.*}.+{c:.*}/john/bar", matchers: []Matcher{QueryMatcher{"a", "b"}}},
				{pattern: "+{a:foo}.{b}.c/{d}/bar/baz"},
				{pattern: "+{a:[A-z]+}.b.c/{path}/bar", matchers: []Matcher{QueryMatcher{"b", "c"}}},
				{pattern: "/{a:^$}/bar"},
			},
			host:        "foo.b.c",
			path:        "/john/bar?b=c",
			wantPattern: "+{a:[A-z]+}.b.c/{path}/bar",
			wantTsr:     false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "path",
					Value: "john",
				},
			},
		},
		{
			name: "fallback must specific path with wildcard, regexp priority and matchers",
			routes: []route{
				{pattern: "{a:.*}.{b:.*}.+{c:.*}/john/bar", matchers: []Matcher{QueryMatcher{"a", "b"}}},
				{pattern: "+{a:foo}.{b}.c/{d}/bar/", matchers: []Matcher{QueryMatcher{"d", "f"}}},
				{pattern: "+{a:[A-z]+}.b.c/{path}/bar", matchers: []Matcher{QueryMatcher{"e", "f"}}},
				{pattern: "/{a:.*}/bar"},
			},
			host:        "foo.b.c",
			path:        "/john/bar?b=c",
			wantPattern: "/{a:.*}/bar",
			wantTsr:     false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "john",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true), WithTrailingSlash(RelaxedSlash))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte.pattern, emptyHandler, WithMatcher(rte.matchers...))))
			}
			tree := f.getTree()
			c := newTestContext(f)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Host = tc.host
			c.req = req
			idx, n, tsr := tree.lookup(http.MethodGet, tc.host, c.RoutingPath(), c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPattern, n.routes[idx].pattern.str)
			assert.Equal(t, tc.wantTsr, tsr)
			c.route = n.routes[idx]
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}

}

func Test_iTree_lookup_MatchersPriority(t *testing.T) {
	type route struct {
		pattern  string
		matchers []Matcher
		priority uint
	}

	cases := []struct {
		name        string
		routes      []route
		path        string
		wantPattern string
		wantMatcher []Matcher
		wantParams  []Param
	}{
		{
			name: "match must specific matchers",
			routes: []route{
				{pattern: "/{name}/bar"},
				{pattern: "/{name}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}}},
				{pattern: "/{id}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
				{pattern: "/{foo}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}, QueryMatcher{"e", "f"}}},
			},
			path:        "/john/bar?a=b&c=d&e=f",
			wantPattern: "/{foo}/bar",
			wantMatcher: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}, QueryMatcher{"e", "f"}},
			wantParams: Params{
				{
					Key:   "foo",
					Value: "john",
				},
			},
		},
		{
			name: "match second must specific matchers",
			routes: []route{
				{pattern: "/{name}/bar"},
				{pattern: "/{name}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}}},
				{pattern: "/{id}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
				{pattern: "/{foo}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}, QueryMatcher{"e", "f"}}},
			},
			path:        "/john/bar?a=b&c=d&e=g",
			wantPattern: "/{id}/bar",
			wantMatcher: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}},
			wantParams: Params{
				{
					Key:   "id",
					Value: "john",
				},
			},
		},
		{
			name: "match less specific route",
			routes: []route{
				{pattern: "/{four}/bar"},
				{pattern: "/{third}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}}},
				{pattern: "/{second}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
				{pattern: "/{first}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}, QueryMatcher{"e", "f"}}},
			},
			path:        "/john/bar?a=g&c=d&e=g",
			wantPattern: "/{four}/bar",
			wantParams: Params{
				{
					Key:   "four",
					Value: "john",
				},
			},
		},
		{
			name: "match most specific route with priority",
			routes: []route{
				{pattern: "/{four}/bar"},
				{pattern: "/{third}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}}, priority: 1000},
				{pattern: "/{second}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}},
				{pattern: "/{first}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}, QueryMatcher{"e", "f"}}},
			},
			path:        "/john/bar?a=b&c=d&e=f",
			wantPattern: "/{third}/bar",
			wantMatcher: []Matcher{QueryMatcher{"a", "b"}},
			wantParams: Params{
				{
					Key:   "third",
					Value: "john",
				},
			},
		},
		{
			name: "match second most specific after failing priority route",
			routes: []route{
				{pattern: "/{four}/bar"},
				{pattern: "/{third}/bar", matchers: []Matcher{QueryMatcher{"a", "f"}}, priority: 1000},
				{pattern: "/{second}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}}, priority: 500},
				{pattern: "/{first}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}, QueryMatcher{"e", "f"}}},
			},
			path:        "/john/bar?a=b&c=d&e=f",
			wantPattern: "/{second}/bar",
			wantMatcher: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}},
			wantParams: Params{
				{
					Key:   "second",
					Value: "john",
				},
			},
		},
		{
			name: "invert priority fail to less priority",
			routes: []route{
				{pattern: "/{four}/bar"},
				{pattern: "/{third}/bar", matchers: []Matcher{QueryMatcher{"a", "f"}}, priority: 1000},
				{pattern: "/{second}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "f"}}, priority: 500},
				{pattern: "/{first}/bar", matchers: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}, QueryMatcher{"e", "f"}}, priority: 250},
			},
			path:        "/john/bar?a=b&c=d&e=f",
			wantPattern: "/{first}/bar",
			wantMatcher: []Matcher{QueryMatcher{"a", "b"}, QueryMatcher{"c", "d"}, QueryMatcher{"e", "f"}},
			wantParams: Params{
				{
					Key:   "first",
					Value: "john",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte.pattern, emptyHandler, WithMatcher(rte.matchers...), WithMatchersPriority(rte.priority))))
			}
			tree := f.getTree()
			c := newTestContext(f)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			c.req = req
			idx, n, _ := tree.lookup(http.MethodGet, "", c.RoutingPath(), c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPattern, n.routes[idx].pattern.str)
			assert.Equal(t, tc.wantMatcher, n.routes[idx].matchers)
			c.route = n.routes[idx]
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}
}

func Test_iTree_lookup_EmptyCatchAll(t *testing.T) {

	cases := []struct {
		name   string
		routes []string
		path   string
	}{
		{
			name:   "infix wildcard",
			routes: []string{"/foo/+{args}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard regexp",
			routes: []string{"/foo/+{args:$^}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with children",
			routes: []string{"/foo/+{args}/bar", "/foo/+{args}/caz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with children regexp",
			routes: []string{"/foo/+{args:$^}/bar", "/foo/+{args:$^}/caz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with static edge",
			routes: []string{"/foo/+{args}/bar", "/foo/baz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with static edge regexp",
			routes: []string{"/foo/+{args:$^}/bar", "/foo/baz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard and suffix wildcard",
			routes: []string{"/foo/+{args}/bar", "/foo/+{args}"},
			path:   "/foo/",
		},
		{
			name:   "infix wildcard and suffix wildcard regexp",
			routes: []string{"/foo/+{args:$^}/bar", "/foo/+{args:$^}"},
			path:   "/foo/",
		},
		{
			name:   "infix inflight wildcard",
			routes: []string{"/foo/abc+{args}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard regexp",
			routes: []string{"/foo/abc+{args:$^}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with children",
			routes: []string{"/foo/abc+{args}/bar", "/foo/abc+{args}/caz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with children regexp",
			routes: []string{"/foo/abc+{args:$^}/bar", "/foo/abc+{args:$^}/caz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with static edge",
			routes: []string{"/foo/abc+{args}/bar", "/foo/abc/baz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with static edge regexp",
			routes: []string{"/foo/abc+{args:$^}/bar", "/foo/abc/baz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard and suffix wildcard",
			routes: []string{"/foo/abc+{args}/bar", "/foo/abc+{args}"},
			path:   "/foo/abc",
		},
		{
			name:   "infix inflight wildcard and suffix wildcard regexp",
			routes: []string{"/foo/abc+{args:$^}/bar", "/foo/abc+{args:$^}"},
			path:   "/foo/abc",
		},
		{
			name:   "suffix wildcard wildcard with param edge",
			routes: []string{"/foo/+{args}", "/foo/{param}"},
			path:   "/foo/",
		},
		{
			name:   "suffix wildcard wildcard with param edge regexp",
			routes: []string{"/foo/+{args:$^}", "/foo/{param:$^}"},
			path:   "/foo/",
		},
		{
			name:   "suffix inflight wildcard wildcard with param edge",
			routes: []string{"/foo/abc+{args}", "/foo/abc{param}"},
			path:   "/foo/abc",
		},
		{
			name:   "suffix inflight wildcard wildcard with param edge regexp",
			routes: []string{"/foo/abc+{args:$^}", "/foo/abc{param:$^}"},
			path:   "/foo/abc",
		},
		{
			name:   "infix wildcard wildcard with param edge",
			routes: []string{"/foo/+{args}/bar", "/foo/{param}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard wildcard with param edge regexp",
			routes: []string{"/foo/+{args:$^}/bar", "/foo/{param:$^}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix inflight wildcard wildcard with param edge",
			routes: []string{"/foo/abc+{args}/bar", "/foo/abc{param}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard wildcard with param edge regexp",
			routes: []string{"/foo/abc+{args:$^}/bar", "/foo/abc{param:$^}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix wildcard wildcard with trailing slash",
			routes: []string{"/foo/+{args}/"},
			path:   "/foo//",
		},
		{
			name:   "infix wildcard wildcard with trailing slash regexp",
			routes: []string{"/foo/+{args:$^}/"},
			path:   "/foo//",
		},
		{
			name:   "infix inflight wildcard wildcard with trailing slash",
			routes: []string{"/foo/abc+{args}/"},
			path:   "/foo/abc/",
		},
		{
			name:   "infix inflight wildcard wildcard with trailing slash regexp",
			routes: []string{"/foo/abc+{args:$^}/"},
			path:   "/foo/abc/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			idx, n, tsr := tree.lookup(http.MethodGet, "", tc.path, c, false)
			require.False(t, tsr)
			require.Nil(t, n)
			assert.Equal(t, 0, idx)
		})
	}
}

func Test_iTree_lookup_WithParams(t *testing.T) {
	f, _ := NewRouter()
	routes := [...]string{
		"/",
		"/cmd/{tool}/{sub}",
		"/cmd/{tool}/",
		"/src/+{filepath}",
		"/search/",
		"/search/{query}",
		"/user_{name}",
		"/user_{name}/about",
		"/files/{dir}/+{filepath}",
		"/doc/",
		"/doc/go_faq.html",
		"/doc/go1.html",
		"/info/{user}/public",
		"/info/{user}/project/{project}",
		"/info/{user}/filepath/*{any}",
	}
	for _, rte := range routes {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
	}

	tree := f.getTree()
	for _, rte := range routes {
		c := newTestContext(f)
		idx, n, tsr := tree.lookup(http.MethodGet, "", rte, c, false)
		require.NotNilf(t, n, "route: %s", rte)
		require.NotNilf(t, n.routes[idx], "route: %s", rte)
		assert.False(t, tsr)
		assert.Equal(t, rte, n.routes[idx].pattern.str)
	}
}

func Test_iTree_lookup_ParamEmptySegment(t *testing.T) {
	f, _ := NewRouter(WithAllowRegexpParam(true))
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
		require.NoError(t, onlyError(f.Add(MethodGet, tc.route, emptyHandler)))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := f.getTree()
			c := newTestContext(f)
			idx, n, tsr := tree.lookup(http.MethodGet, "", tc.path, c, false)
			assert.Nil(t, n)
			assert.Equal(t, 0, idx)
			assert.Empty(t, slices.Collect(c.Params()))
			assert.False(t, tsr)
		})
	}
}

func Test_iTree_lookup_Overlapping(t *testing.T) {
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
				"/{base}/val1/123/{a}/barr",
				"/{base}/val1/{id}/new/{name}/y",
				"/{base}/val+{all}",
			},
			wantMatch: "/{base}/val+{all}",
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
				"/{base}/val1/123/{a:[A-z]+}/barr",
				"/{base}/val1/{id:[0-9]+}/new/{name:ba}",
				"/{base}/val+{all}",
			},
			wantMatch: "/{base}/val+{all}",
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
				"/{base}/val+{all}",
			},
			wantMatch: "/{base}/val+{all}",
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
				"/foo/a+{args}",
				"/foo+{args}",
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
				"/foo/a+{args:.*}",
				"/foo+{args:.*}",
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
				"/foo/a+{args}",
				"/foo+{args}",
			},
			wantMatch: "/foo/a+{args}",
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
				"/foo/a+{args:.*}",
				"/foo+{args:.*}",
			},
			wantMatch: "/foo/a+{args:.*}",
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
				"/foo/+{args}",
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
				"/foo/+{args}",
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
				"/foo/+{args}",
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
				"/foo/+{args}",
			},
			wantMatch: "/foo/+{args}",
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
				"/foo/{id}/+{args}",
				"/foo/+{args}",
			},
			wantMatch: "/foo/{id}/+{args}",
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
				"/foo/{id_1}/+{args}",
				"/foo/+{args}",
			},
			wantMatch: "/foo/{id_1}/+{args}",
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
				"/foo/{id_1}/+{args}",
				"/foo/{id_1}/{id_2}",
				"/foo/+{args}",
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
				"/foo/{id_1}/+{args}",
				"/foo/{id_1}/{id_2}",
				"/foo/+{args}",
			},
			wantMatch: "/foo/{id_1}/+{args}",
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
				"/foo/{id_1}/+{args}",
				"/foo/{id_1}/{id_2}",
				"/foo/+{args}",
			},
			wantMatch: "/foo/+{args}",
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
			path: "/foo/]barr]",
			routes: []string{
				"/foo/{bar}",
				"/foo/[bar]",
				"/foo/]bar]",
			},
			wantMatch: "/foo/{bar}",
			wantParams: Params{
				{
					Key:   "bar",
					Value: "]barr]",
				},
			},
		},
		{
			name: "param at index 0 with 3 nodes",
			path: "/foo/~barr~",
			routes: []string{
				"/foo/{bar}",
				"/foo/~bar~",
				"/foo/]bar]",
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
				"/foo/+{fallback}",
				"/foo/+{a:[0-9]+}",
				"/foo/+{b:[0-9-A-z]+}",
				"/foo/+{c:[0-9-A-Z]+}",
			},
			wantMatch: "/foo/+{a:[0-9]+}",
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
				"/foo/+{fallback}",
				"/foo/+{a:[0-9]+}",
				"/foo/+{b:[0-9-A-Z]+}",
				"/foo/+{c:[0-9-A-z]+}",
			},
			wantMatch: "/foo/+{c:[0-9-A-z]+}",
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
				"/foo/+{fallback}",
				"/foo/+{a:[0-9]+}",
				"/foo/+{b:[0-9-A-Z]+}",
				"/foo/+{c:[0-9-A-z]+}",
			},
			wantMatch: "/foo/+{fallback}",
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
				"/foo/+{fallback}/bar",
				"/foo/+{a:[0-9]+}/bar",
				"/foo/+{b:[0-9-A-Z]+}/bar",
				"/foo/+{c:[0-9-A-z]+}/bar",
			},
			wantMatch: "/foo/+{fallback}/bar",
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
				"/foo/+{fallback}/bar/+{ab:[0-9]+}",
				"/foo/+{a:.*}/bar/{b:[A-z]+}",
				"/foo/+{b}/bar/{foo}",
				"/foo/+{c:[0-9/]+}/bar/foo",
			},
			wantMatch: "/foo/+{a:.*}/bar/{b:[A-z]+}",
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
				"/foo/+{fallback}/bar/+{ab:[0-9]+}",
				"/foo/+{a:.*}/bar/{b:[A-z]+}",
				"/foo/+{b}/bar/{foo}",
				"/foo/+{c:[0-9/]+}/bar/foo",
			},
			wantMatch: "/foo/+{b}/bar/{foo}",
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
				"/foo/+{fallback}/bar/+{ab:[0-9]+}",
				"/foo/+{a:.*}/bar/{b:[A-z]+}",
				"/foo/+{b}/bar/{foo}",
				"/foo/+{c:[0-9/]+}/bar/foo",
			},
			wantMatch: "/foo/+{a:.*}/bar/{b:[A-z]+}",
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
				"/foo/+{fallback}/bar/+{ab:[A-z0-9/]+}",
				"/foo/+{a:.*}/bar/{b:[A-z]+}",
				"/foo/+{b}/bar/{foo}",
				"/foo/+{c:[0-9/]+}/bar/foo",
			},
			wantMatch: "/foo/+{fallback}/bar/+{ab:[A-z0-9/]+}",
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
				"/+{args:.*}",
				"/+{a:a}/b/c",
				"/+{b:b}/b/c",
			},
			wantMatch: "/+{args:.*}",
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
				"/+{a:a}/b/c",
				"/+{b:b}/b/c",
				"/+{args}",
			},
			wantMatch: "/+{args}",
			wantParams: Params{
				{
					Key:   "args",
					Value: "aa/b/c",
				},
			},
		},
		{
			name: "suffix regex catch-all capture includes the trailing slash",
			path: "/abc/",
			routes: []string{
				"/+{w:[a-z/]+}",
			},
			wantMatch: "/+{w:[a-z/]+}",
			wantParams: Params{
				{
					Key:   "w",
					Value: "abc/",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
			}

			tree := f.getTree()

			c := newTestContext(f)
			idx, n, tsr := tree.lookup(http.MethodGet, "", tc.path, c, false)
			require.NotNil(t, n)
			require.NotNil(t, n.routes[idx])
			assert.False(t, tsr)
			assert.Equal(t, tc.wantMatch, n.routes[idx].pattern.str)
			c.route = n.routes[idx]
			if len(tc.wantParams) == 0 {
				assert.Empty(t, slices.Collect(c.Params()))
			} else {
				var params Params = slices.Collect(c.Params())
				assert.Equal(t, tc.wantParams, params)
			}

			// Test with lazy
			c = newTestContext(f)
			idx, n, tsr = tree.lookup(http.MethodGet, "", tc.path, c, true)
			require.NotNil(t, n)
			require.NotNil(t, n.routes[idx])
			assert.False(t, tsr)
			c.route = n.routes[idx]
			assert.Empty(t, slices.Collect(c.Params()))
			assert.Equal(t, tc.wantMatch, n.routes[idx].pattern.str)
		})
	}
}

func Test_iTree_lookup_InfixWildcard(t *testing.T) {
	cases := []struct {
		name       string
		routes     []string
		path       string
		wantPath   string
		wantTsr    bool
		wantParams []Param
	}{
		{
			name:     "infix wildcard favors the route with the longest static tail",
			routes:   []string{"/+{w}/x/y", "/+{w}/y"},
			path:     "/a/x/y",
			wantPath: "/+{w}/x/y",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "w",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix wildcard",
			routes:   []string{"/foo/+{args}/bar"},
			path:     "/foo/a/bar",
			wantPath: "/foo/+{args}/bar",
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
			routes:   []string{"/foo/+{args:a}/bar"},
			path:     "/foo/a/bar",
			wantPath: "/foo/+{args:a}/bar",
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
			routes:   []string{"/foo/+{args}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/+{args}/bar",
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
			routes:   []string{"/foo/+{args:/}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/+{args:/}/bar",
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
			routes:   []string{"/foo/+{args}/bar"},
			path:     "/foo//a//bar",
			wantPath: "/foo/+{args}/bar",
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
			routes:   []string{"/foo/+{args:/a/}/bar"},
			path:     "/foo//a//bar",
			wantPath: "/foo/+{args:/a/}/bar",
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
			routes:   []string{"/foo/", "/foo/+{args}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/+{args}/baz",
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
			routes:   []string{"/foo/", "/foo/+{args:[A-z]+}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/+{args:[A-z]+}/baz",
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
			routes:   []string{"/foo/", "/foo/+{args}/baz", "/foo/+{args:[A-z]+}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/+{args:[A-z]+}/baz",
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
			routes:   []string{"/foo/", "/foo/+{args}/baz"},
			path:     "/foo///baz",
			wantPath: "/foo/+{args}/baz",
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
			routes:   []string{"/foo/", "/foo/+{args}/baz", "/foo/+{args:a}/baz"},
			path:     "/foo///baz",
			wantPath: "/foo/+{args}/baz",
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
			routes:   []string{"/foo/+{args}/bar"},
			path:     "/foo/+{args}/bar",
			wantPath: "/foo/+{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "+{args}",
				},
			},
		},
		{
			name:     "simple infix wildcard regexp with route char",
			routes:   []string{"/foo/+{args:.*}/bar"},
			path:     "/foo/+{args:.*}/bar",
			wantPath: "/foo/+{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "+{args:.*}",
				},
			},
		},
		{
			name:     "simple infix wildcard with multi segment and route char",
			routes:   []string{"/foo/+{args}/bar"},
			path:     "/foo/+{args}/b/c/bar",
			wantPath: "/foo/+{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "+{args}/b/c",
				},
			},
		},
		{
			name:     "simple infix wildcard regexp with multi segment and route char",
			routes:   []string{"/foo/+{args:.*}/bar"},
			path:     "/foo/+{args:.*}/b/c/bar",
			wantPath: "/foo/+{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "+{args:.*}/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard",
			routes:   []string{"/foo/z+{args}/bar"},
			path:     "/foo/za/bar",
			wantPath: "/foo/z+{args}/bar",
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
			routes:   []string{"/foo/z+{args:a}/bar"},
			path:     "/foo/za/bar",
			wantPath: "/foo/z+{args:a}/bar",
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
			routes:   []string{"/foo/z+{args}/bar"},
			path:     "/foo/z//bar",
			wantPath: "/foo/z+{args}/bar",
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
			routes:   []string{"/foo/z+{args:/}/bar"},
			path:     "/foo/z//bar",
			wantPath: "/foo/z+{args:/}/bar",
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
			routes:   []string{"/foo/z+{args}/bar"},
			path:     "/foo/z+{args}/bar",
			wantPath: "/foo/z+{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "+{args}",
				},
			},
		},
		{
			name:     "simple infix regexp inflight wildcard with route char",
			routes:   []string{"/foo/z+{args:.*}/bar"},
			path:     "/foo/z+{args:.*}/bar",
			wantPath: "/foo/z+{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "+{args:.*}",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with multi segment",
			routes:   []string{"/foo/z+{args}/bar"},
			path:     "/foo/za/b/c/bar",
			wantPath: "/foo/z+{args}/bar",
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
			routes:   []string{"/foo/z+{args:[A-z/]+}/bar"},
			path:     "/foo/za/b/c/bar",
			wantPath: "/foo/z+{args:[A-z/]+}/bar",
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
			routes:   []string{"/foo/z+{args:a/b/c}/bar"},
			path:     "/foo/za/b/c/bar",
			wantPath: "/foo/z+{args:a/b/c}/bar",
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
			routes:   []string{"/foo/z+{args}/bar"},
			path:     "/foo/z////bar",
			wantPath: "/foo/z+{args}/bar",
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
			routes:   []string{"/foo/z+{args:///}/bar"},
			path:     "/foo/z////bar",
			wantPath: "/foo/z+{args:///}/bar",
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
			routes:   []string{"/foo/z+{args}/bar"},
			path:     "/foo/z+{args}/b/c/bar",
			wantPath: "/foo/z+{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "+{args}/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard regexp with multi segment and route char",
			routes:   []string{"/foo/z+{args:.*}/bar"},
			path:     "/foo/z+{args}/b/c/bar",
			wantPath: "/foo/z+{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "+{args}/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard long",
			routes:   []string{"/foo/xyz+{args}/bar"},
			path:     "/foo/xyza/bar",
			wantPath: "/foo/xyz+{args}/bar",
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
			routes:   []string{"/foo/xyz+{args}/bar"},
			path:     "/foo/xyza/b/c/bar",
			wantPath: "/foo/xyz+{args}/bar",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar"},
			path:     "/foo/a/b/c/bar",
			wantPath: "/foo/+{args}/bar",
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
			routes:   []string{"/foo/+{args:.*}", "/foo/+{args:.*}/bar"},
			path:     "/foo/a/b/c/bar",
			wantPath: "/foo/+{args:.*}/bar",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/+{args}/bar",
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
			routes:   []string{"/foo/+{args:/}", "/foo/+{args:/}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/+{args:/}/bar",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar"},
			path:     "/foo/a/b/c/baz",
			wantPath: "/foo/+{args}",
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
			routes:   []string{"/foo/+{args:.*}", "/foo/+{args:.*}/bar"},
			path:     "/foo/a/b/c/baz",
			wantPath: "/foo/+{args:.*}",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar"},
			path:     "/foo///baz",
			wantPath: "/foo/+{args}",
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
			routes:   []string{"/foo/+{args:.*}", "/foo/+{args:.*}/bar"},
			path:     "/foo///baz",
			wantPath: "/foo/+{args:.*}",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar"},
			path:     "/foo/a/b/c/barito",
			wantPath: "/foo/+{args}",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo/a/b/c/bar",
			wantPath: "/foo/+{args}/bar",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/+{args}/bar",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo/a/b/c/bili",
			wantPath: "/foo/+{args}",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar", "/foo/{ps}"},
			path:     "/foo/a/bar",
			wantPath: "/foo/+{args}/bar",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar", "/foo/{ps}/bar"},
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar", "/foo/{ps}"},
			path:     "/foo/a/bili",
			wantPath: "/foo/+{args}",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar", "/foo/{ps}"},
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar", "/foo/{ps}/"},
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
			name:     "overlapping infix suffix wildcard and param match suffix",
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar", "/foo/{ps}/y"},
			path:     "/foo/a",
			wantPath: "/foo/+{args}",
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
			routes:   []string{"/foo/+{args}", "/foo/+{args}/bar", "/foo/{ps}/y"},
			path:     "/foo/a/",
			wantPath: "/foo/+{args}",
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
			routes:   []string{"/foo/123+{args}", "/foo/123+{args}/bar", "/foo/123{ps}/bar"},
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
			routes:   []string{"/foo/123+{args}", "/foo/123+{args}/bar", "/foo/123{ps}"},
			path:     "/foo/123a/bili",
			wantPath: "/foo/123+{args}",
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
			routes:   []string{"/foo/123+{args}", "/foo/123+{args}/bar", "/foo/123{ps}"},
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
			routes:   []string{"/foo/+{a}/{b}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/+{a}/{b}",
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
			routes:   []string{"/foo/+{a}/{b}/{c}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/+{a}/{b}/{c}",
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
			routes:   []string{"/foo/+{a}/{b}/+{c}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/+{a}/{b}/+{c}",
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
			routes:   []string{"/foo/{a}/+{b}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/{a}/+{b}",
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
			routes:   []string{"/foo/123+{a}/{b}"},
			path:     "/foo/123a/b/c/d",
			wantPath: "/foo/123+{a}/{b}",
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
			routes:   []string{"/foo/123{a}/+{b}"},
			path:     "/foo/123a/b/c/d",
			wantPath: "/foo/123{a}/+{b}",
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
			routes:   []string{"/foo/+{$1}/bar/+{$2}/baz"},
			path:     "/foo/a/bar/b/c/d/baz",
			wantPath: "/foo/+{$1}/bar/+{$2}/baz",
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
			routes:   []string{"/foo/123+{$1}/bar/456+{$2}/baz"},
			path:     "/foo/123a/bar/456b/c/d/baz",
			wantPath: "/foo/123+{$1}/bar/456+{$2}/baz",
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
			routes:   []string{"/foo/bar/baz", "/foo/{ps}/baz", "/foo/+{any}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/bar/baz",
			wantTsr:  false,
		},
		{
			name:     "param priority",
			routes:   []string{"/foo/bar/baz", "/foo/{ps}/baz", "/foo/+{any}/baz"},
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
			routes:   []string{"/foo/bar/baz", "/foo/{ps}/baz", "/foo/+{any}/baz"},
			path:     "/foo/a/b/baz",
			wantPath: "/foo/+{any}/baz",
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
				"/foo/+{any}/baz/{$1}/jo",
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
				"/foo/+{any}/baz/{$1}/jo",
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
				"/foo/+{any}/baz/{$1}/jo",
				"/foo/{ps}/baz/{$1}/jo",
			},
			path:     "/foo/a/b/c/baz/1/jo",
			wantPath: "/foo/+{any}/baz/{$1}/jo",
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
				"/foo/+{any}/baz/{$1}/john",
				"/foo/{ps}/baz/{$1}/johnny",
			},
			path:     "/foo/a/baz/1/john",
			wantPath: "/foo/+{any}/baz/{$1}/john",
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
				"/foo/+{any}/baz",
				"/foo/a/b/baz",
			},
			path:     "/foo/a/b/baz",
			wantPath: "/foo/a/b/baz",
			wantTsr:  false,
		},
		{
			name: "overlapping static and infix with catch all fallback",
			routes: []string{
				"/foo/+{any}/baz",
				"/foo/a/b/baz",
			},
			path:     "/foo/a/b/c/baz",
			wantPath: "/foo/+{any}/baz",
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
				"/foo/+{any}/",
			},
			path:     "/foo/a/b/c/",
			wantPath: "/foo/+{any}/",
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
				"/foo/+{any}/{a}/ddd/",
				"/foo/+{any}/bbb/{d}/e",
			},
			path:     "/foo/a/b/c/bbb/ddd/",
			wantPath: "/foo/+{any}/{a}/ddd/",
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
				"/foo/+{any}",
				"/foo/+{any}/b/",
				"/foo/+{any}/c/",
			},
			path:     "/foo/x/y/z/",
			wantPath: "/foo/+{any}",
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
				"/foo/+{any}",
				"/foo/+{any}/",
			},
			path:     "/foo/x/y/z/",
			wantPath: "/foo/+{any}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "infix wildcard with trailing with direct match most specific",
			routes: []string{
				"/foo/+{any}",
				"/foo/+{any}/",
			},
			path:     "/foo/x/y/z",
			wantPath: "/foo/+{any}",
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
				"/foo/+{any:.*}",
				"/foo/+{any:.*}/",
			},
			path:     "/foo/x/y/z/",
			wantPath: "/foo/+{any:.*}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "infix regexp wildcard with direct match",
			routes: []string{
				"/foo/+{any:.*}",
				"/foo/+{any:.*}/",
			},
			path:     "/foo/x/y/z",
			wantPath: "/foo/+{any:.*}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "infix wildcard with fallback to suffix",
			routes: []string{
				"/foo/+{any}",
				"/foo/+{any}/x",
			},
			path:     "/foo/x/y/z",
			wantPath: "/foo/+{any}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z",
				},
			},
		},
		{
			// infix regexp wildcard with trailing slash most specific
			name: "infix regexp wildcard with fallback to suffix",
			routes: []string{
				"/foo/+{any:.*}",
				"/foo/+{any:[A-z]}/",
			},
			path:     "/foo/x/y/z",
			wantPath: "/foo/+{any:.*}",
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
			f, _ := NewRouter(WithAllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			idx, n, tsr := tree.lookup(http.MethodGet, "", tc.path, c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.routes[idx].pattern.str)
			assert.Equal(t, tc.wantTsr, tsr)
			c.route = n.routes[idx]
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}

}

func Test_iTree_lookup_InfixWildcardTsr(t *testing.T) {
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
				"/foo/+{any}/",
			},
			path:     "/foo/a/b/c",
			wantPath: "/foo/+{any}/",
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
				"/foo/+{any}/",
			},
			path:     "/foo//a",
			wantPath: "/foo/+{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "/a",
				},
			},
		},
		{
			name: "infix wildcard with tsr and skipped node match",
			routes: []string{
				"/foo/+{any}/",
				"/{x}/a/b/c",
			},
			path:     "/foo/a/b/c",
			wantPath: "/foo/+{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with tsr but skipped node does not match",
			routes: []string{
				"/foo/+{any}/",
				"/{x}/a/b/x",
			},
			path:     "/foo/a/b/c",
			wantPath: "/foo/+{any}/",
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
				"/foo/+{any}/",
				"/foo/+{any}/abc",
				"/foo/+{any}/bcd",
			},
			path:     "/foo/a/b/c/abd",
			wantPath: "/foo/+{any}/",
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
				"/foo/+{any}/{a}/ddd/",
				"/foo/+{any}/bbb/{d}/foo",
			},
			path:     "/foo/a/b/c/bbb/ddd",
			wantPath: "/foo/+{any}/{a}/ddd/",
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
			name: "infix wildcard with with more specific tsr",
			routes: []string{
				"/foo/+{any}/c/bbb/",
				"/foo/+{any}/bbb",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/+{any}/c/bbb/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b",
				},
			},
		},
		{
			name: "infix wildcard with with more specific tsr but regexp don't pass",
			routes: []string{
				"/foo/+{any:bar}/c/bbb/",
				"/foo/+{any}/bbb",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/+{any}/bbb",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with more specific tsr (multi depth)",
			routes: []string{
				"/foo/+{any}/b/c/bbb/",
				"/foo/+{any}/c/bbb/",
				"/foo/+{any}/bbb",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/+{any}/b/c/bbb/",
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
				"/foo/+{any}/b/c/bbb/",
				"/foo/+{any}/c/bbb/",
				"/foo/+{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/+{any}/b/c/bbb/",
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
				"/foo/+{any}/",
				"/foo/+{any}/b/c/bbb/",
				"/foo/+{any}/c/bbb/",
				"/foo/+{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/+{any}/b/c/bbb/",
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
				"/foo/+{any}/c/bbb/",
				"/foo/+{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/a/b/c/bbb/",
			wantTsr:  true,
		},
		{
			name: "infix wildcard with depth 0 tsr and sub-node tsr at depth 1 fallback first tsr",
			routes: []string{
				"/foo/{first}/b/c/bbb/",
				"/foo/+{any}/c/bbb/",
				"/foo/+{any}/bbx",
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
			name: "infix wildcard with trailing slash and suffix regexp catch-all",
			routes: []string{
				"/foo/+{any:[A-z]+}",
				"/foo/+{any}/",
			},
			path:     "/foo/a/b/c",
			wantPath: "/foo/+{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "multi infix with most specific and match",
			routes: []string{
				"/foo/+{any1}/b/c/+{any2}/d/",
				"/foo/+{any1}/c/+{any2}/d",
			},
			path:     "/foo/a/b/c/x/y/z/d",
			wantPath: "/foo/+{any1}/b/c/+{any2}/d/",
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
			name: "multi infix wildcard with sub-node tsr at depth 1 and fallback first",
			routes: []string{
				"/foo/+{any1}/b/c/+{any2}/d/",
				"/foo/+{any1}/c/+{any2}/x",
			},
			path:     "/foo/a/b/c/x/y/z/d",
			wantPath: "/foo/+{any1}/b/c/+{any2}/d/",
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
				"/foo/+{any1}/b/c/+{any2}/{a}/",
				"/foo/+{any1}/b/c/+{any2}/d{a}/",
				"/foo/+{any1}/b/c/+{any2}/dd/",
				"/foo/+{any1}/c/+{any2}/x",
			},
			path:     "/foo/a/b/c/x/y/z/dd",
			wantPath: "/foo/+{any1}/b/c/+{any2}/dd/",
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
				"/foo/+{any1}/b/c/+{any2}/{a}/",
				"/foo/+{any1}/b/c/+{any2}/d{a}/",
				"/foo/+{any1}/b/c/+{any2}/dd/",
				"/foo/+{any1}/c/+{any2}/x",
			},
			path:     "/foo/a/b/c/x/y/z/xd/",
			wantPath: "/foo/+{any1}/b/c/+{any2}/{a}/",
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
			name: "multi infix wildcard with most specific tsr match",
			routes: []string{
				"/foo/+{any1}/b/c/+{any2}/{a}/",
				"/foo/+{any1}/b/c/+{any2}/d{a}/",
				"/foo/+{any1}/b/c/+{any2}/dd/",
				"/foo/+{any1}/c/+{any2}/x",
				"/{a}/+{any1}/c/x/y/z/dd",
			},
			path:     "/foo/a/b/c/x/y/z/dd",
			wantPath: "/foo/+{any1}/b/c/+{any2}/dd/",
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true), WithTrailingSlash(RelaxedSlash))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
			}

			tree := f.getTree()

			c := newTestContext(f)
			idx, n, tsr := tree.lookup(http.MethodGet, "", tc.path, c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.routes[idx].pattern.str)
			assert.Equal(t, tc.wantTsr, tsr)
			c.route = n.routes[idx]
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}
}

func Test_iTree_lookup_InfixWildcardLinear(t *testing.T) {
	docker := `[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*)*`
	registry := []string{
		"/v2/+{name:" + docker + "}/manifests/{reference}",
		"/v2/+{name:" + docker + "}/blobs/{digest}",
		"/v2/+{name:" + docker + "}/tags/list",
	}
	cases := []struct {
		name      string
		routes    []string
		opts      []RouteOption
		request   func(n int) (host, path string)
		wantMatch bool
	}{
		{
			name:   "regexp infix followed by a param",
			routes: registry,
			request: func(n int) (string, string) {
				return "", "/v2/" + strings.Repeat("abcdefg/", n) + "img/manifests/latest"
			},
			wantMatch: true,
		},
		{
			name:   "regexp infix followed by a param near miss",
			routes: registry,
			request: func(n int) (string, string) {
				return "", "/v2/" + strings.Repeat("abcdefg/", n) + "img/manifests/latest/x"
			},
		},
		{
			name:    "multi infix near miss",
			routes:  []string{"/files/+{p}/thumb/+{q}/download"},
			request: func(n int) (string, string) { return "", "/files" + strings.Repeat("/thumb", n) },
		},
		{
			name:    "regexp infix near miss",
			routes:  []string{"/files/+{p:(?:x/)*x}/x"},
			request: func(n int) (string, string) { return "", "/files/" + strings.Repeat("x/", n) + "Z" },
		},
		{
			name:      "regexp infix rejecting the shorter captures",
			routes:    []string{"/files/+{p:(?:x/)*y}/x"},
			request:   func(n int) (string, string) { return "", "/files/" + strings.Repeat("x/", n) + "y/x" },
			wantMatch: true,
		},
		{
			name:    "regexp infix below an infix",
			routes:  []string{"/+{a}/x/+{b:(?:[a-z]+/)*[a-z]+}/y"},
			request: func(n int) (string, string) { return "", strings.Repeat("/a/x", n) + "/z" },
		},
		{
			name:    "regexp infix listed below its last capture below an infix",
			routes:  []string{"/+{a}/x/+{b:z[a-z/]*}/y"},
			request: func(n int) (string, string) { return "", strings.Repeat("/a/x", n) + "/y/y" },
		},
		{
			name:    "regexp infix scanned to the end after a listed scan below an infix",
			routes:  []string{"/+{a}/x/+{b:z[a-z/]*}/y"},
			request: func(n int) (string, string) { return "", "/a/x/q/y/y" + strings.Repeat("/a/x", n) + "/Z" },
		},
		{
			name:    "regexp infix with trailing slash listed below an infix",
			routes:  []string{"/+{a}/x/+{b:z[a-z]*}/"},
			opts:    []RouteOption{WithTrailingSlash(RelaxedSlash)},
			request: func(n int) (string, string) { return "", strings.Repeat("/a/x", n) + "/Z" },
		},
		{
			name:    "regexp infix with trailing slash below an infix",
			routes:  []string{"/+{a}/x/+{b:(?:[a-z]+/)*[a-z]+}/"},
			opts:    []RouteOption{WithTrailingSlash(RelaxedSlash)},
			request: func(n int) (string, string) { return "", strings.Repeat("/a/x", n) + "/Z" },
		},
		{
			name:    "regexp infix with a bounded repeat below an infix",
			routes:  []string{"/+{a}/x/+{b:.{1,255}}/y"},
			request: func(n int) (string, string) { return "", strings.Repeat("/a/x", n) + "/Z" },
		},
		{
			name:    "regexp infix with bounded segments below an infix",
			routes:  []string{"/+{a}/x/+{b:(?:[a-z]+/){1,255}}/y"},
			request: func(n int) (string, string) { return "", strings.Repeat("/a/x", n) + "/Z" },
		},
		{
			name:    "regexp infix with bounded repeated segments below an infix",
			routes:  []string{"/+{a}/x/+{b:(?:(?:[a-z]+/)+){1,255}}/y"},
			request: func(n int) (string, string) { return "", strings.Repeat("/a/x", n) + "/Z" },
		},
		{
			name:    "regexp infix below an infix in a non ascii path",
			routes:  []string{"/+{a}/x/+{b:[^/]+}/y"},
			request: func(n int) (string, string) { return "", strings.Repeat("/é/x", n) + "/Z" },
		},
		{
			name:    "regexp suffix below an infix",
			routes:  []string{"/+{a}/x/+{b:[a-z/]+}"},
			request: func(n int) (string, string) { return "", strings.Repeat("/a/x", n) + "/Z" },
		},
		{
			name:   "hostname infix followed by a path",
			routes: []string{"+{sub}.example.+{tld}/{a}/x"},
			request: func(n int) (string, string) {
				return strings.Repeat("a.example.", n) + "com", "/" + strings.Repeat("a", n) + "/y"
			},
		},
		{
			name:    "hostname regexp infix below an infix",
			routes:  []string{"+{a}.x.+{b:(?:[a-z]+\\.)*[a-z]+}.com/"},
			request: func(n int) (string, string) { return strings.Repeat("a.x.", n) + "org", "/" },
		},
		{
			name:    "hostname regexp suffix below an infix",
			routes:  []string{"+{a}.x.+{b:[a-z.]+}/"},
			request: func(n int) (string, string) { return strings.Repeat("a.x.", n) + "9", "/" },
		},
		{
			name:   "hostname with slashes falls back to path only route",
			routes: []string{"+{v1}.api/+{v2:.*x}", "/+{v2:.*x}"},
			request: func(n int) (string, string) {
				return strings.Repeat("/.api", n), "/" + strings.Repeat("a", n) + "x"
			},
			wantMatch: true,
		},
	}

	steps := func(f *Router, host, path string) (int, bool) {
		c := newTestContext(f)
		_, n, _ := f.getTree().lookup(http.MethodGet, host, path, c, false)
		steps := c.lookup.steps
		for _, w := range c.lookup.regexps {
			steps += w.ends.Steps()
		}
		return steps, n != nil
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler, tc.opts...)))
			}
			host, path := tc.request(500)
			small, matched := steps(f, host, path)
			assert.Equal(t, tc.wantMatch, matched)
			host, path = tc.request(2000)
			large, matched := steps(f, host, path)
			assert.Equal(t, tc.wantMatch, matched)
			assert.Less(t, large, 6*small)
		})
	}
}

func Test_iTree_lookup_InfixWildcardSingleSegment(t *testing.T) {
	cases := []struct {
		name  string
		route string
		path  string
		steps int
	}{
		{name: "rejected by the static key", route: "/+{v:[a-z]+}/x", path: strings.Repeat("/v", 1000), steps: 1},
		{name: "empty first capture", route: "/+{v:a}/api", path: strings.Repeat("/", 1000) + "Z", steps: 1},
		{name: "rejected by the regexp", route: "/+{v:[a-z]+}/x", path: "/1" + strings.Repeat("/x", 1000), steps: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true))
			require.NoError(t, onlyError(f.Add(MethodGet, tc.route, emptyHandler)))
			c := newTestContext(f)
			_, n, _ := f.getTree().lookup(http.MethodGet, "", tc.path, c, false)
			assert.Nil(t, n)
			assert.Equal(t, tc.steps, c.lookup.steps)
		})
	}
}

func Test_iTree_lookup_InfixWildcardDisjointScans(t *testing.T) {
	cases := []struct {
		name   string
		route  string
		host   string
		path   string
		params []string
	}{
		{
			name:   "path",
			route:  "/+{site}/archive/+{date:[0-9]{4}/[0-9]{2}/[0-9]{2}}/{slug}",
			path:   "/blog/archive/engineering/archive/2026/10/13/pike-vm",
			params: []string{"blog/archive/engineering", "2026/10/13", "pike-vm"},
		},
		{
			name:   "path with a single segment regexp",
			route:  "/+{site}/archive/+{year:[0-9]+}/{slug}",
			path:   "/blog/archive/engineering/archive/2026/pike-vm",
			params: []string{"blog/archive/engineering", "2026", "pike-vm"},
		},
		{
			name:   "hostname",
			route:  "+{bucket}.s3.+{region:[a-z]{2}-[a-z]+-[0-9]}.amazonaws.com/+{key}",
			host:   "logs.s3.backup.s3.eu-west-1.amazonaws.com",
			path:   "/2026/app.log",
			params: []string{"logs.s3.backup", "eu-west-1", "2026/app.log"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true))
			require.NoError(t, onlyError(f.Add(MethodGet, tc.route, emptyHandler)))
			c := newTestContext(f)
			idx, n, _ := f.getTree().lookup(http.MethodGet, tc.host, tc.path, c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.route, n.routes[idx].Pattern())
			assert.Equal(t, tc.params, *c.params)
			assert.Nil(t, c.lookup.pathPikeVM)
			assert.Nil(t, c.lookup.hostPikeVM)
		})
	}
}

func Test_iTree_lookup_PikeVM(t *testing.T) {
	cases := []struct {
		name    string
		routes  []string
		relaxed bool
		host    string
		path    string
		want    string
		tsr     bool
		params  []string
	}{
		{
			name:   "infix match",
			routes: []string{"/+{a}/x/+{b:[a-z/]+}/y"},
			path:   "/a/x/B/x/c/y",
			want:   "/+{a}/x/+{b:[a-z/]+}/y",
			params: []string{"a/x/B", "c"},
		},
		{
			name:   "param after infix",
			routes: []string{"/+{a}/x/+{b:[a-z/]+}/{p}/z"},
			path:   "/a/x/b/x/C/x/c/d/z",
			want:   "/+{a}/x/+{b:[a-z/]+}/{p}/z",
			params: []string{"a/x/b/x/C", "c", "d"},
		},
		{
			name:   "suffix without regexp after infix",
			routes: []string{"/+{a}/x/+{b:[a-z/]+}/y/+{c}"},
			path:   "/a/x/B/x/c/y/d",
			want:   "/+{a}/x/+{b:[a-z/]+}/y/+{c}",
			params: []string{"a/x/B", "c", "d"},
		},
		{
			name:   "suffix regexp sibling",
			routes: []string{"/+{a}/x/+{b:[a-z/]+}/y", "/+{a}/x/+{c:[a-z/]+}"},
			path:   "/a/x/B/x/c/d",
			want:   "/+{a}/x/+{c:[a-z/]+}",
			params: []string{"a/x/B", "c/d"},
		},
		{
			name:   "empty optional catch-all",
			routes: []string{"/+{a}/x/+{b:[a-z/]+}/y/*{c}"},
			path:   "/a/x/B/x/c/y/",
			want:   "/+{a}/x/+{b:[a-z/]+}/y/*{c}",
			params: []string{"a/x/B", "c", ""},
		},
		{
			name:    "tsr adding a slash to a static key",
			routes:  []string{"/+{a}/x/+{b:[a-z/]+}/y/"},
			relaxed: true,
			path:    "/a/x/B/x/c/y",
			want:    "/+{a}/x/+{b:[a-z/]+}/y/",
			tsr:     true,
			params:  []string{"a/x/B", "c"},
		},
		{
			name:    "tsr adding a slash node",
			routes:  []string{"/+{a}/x/+{b:[a-z/]+}/y/", "/+{a}/x/+{b:[a-z/]+}/yz"},
			relaxed: true,
			path:    "/a/x/B/x/c/y",
			want:    "/+{a}/x/+{b:[a-z/]+}/y/",
			tsr:     true,
			params:  []string{"a/x/B", "c"},
		},
		{
			name:    "tsr removing a slash node",
			routes:  []string{"/+{a}/x/+{b:[a-z/]+}/y", "/+{a}/x/+{b:[a-z/]+}/y/z", "/+{a}/x/+{b:[a-z/]+}/y/w"},
			relaxed: true,
			path:    "/a/x/B/x/c/y/",
			want:    "/+{a}/x/+{b:[a-z/]+}/y",
			tsr:     true,
			params:  []string{"a/x/B", "c"},
		},
		{
			name:    "tsr removing a slash before the next wildcard",
			routes:  []string{"/+{a}/x+{b:[a-z/]+}/y", "/+{a}/x", "/+{a}/x+{c:[/a-z]+}"},
			relaxed: true,
			path:    "/a/x/B/x/",
			want:    "/+{a}/x",
			tsr:     true,
			params:  []string{"a/x/B"},
		},
		{
			name:    "suffix before a tsr removing a slash",
			routes:  []string{"/+{a}/x+{c:[/a-z]+}", "/+{a}/x", "/+{a}/x+{b:[a-z/]+}/y"},
			relaxed: true,
			path:    "/a/x/B/x/",
			want:    "/+{a}/x+{c:[/a-z]+}",
			params:  []string{"a/x/B", "/"},
		},
		{
			name:    "infix tsr",
			routes:  []string{"/+{a}/x/+{b:[a-z/]+}/", "/+{a}/x/+{c:[0-9/]+}"},
			relaxed: true,
			path:    "/a/x/B/x/c",
			want:    "/+{a}/x/+{b:[a-z/]+}/",
			tsr:     true,
			params:  []string{"a/x/B", "c"},
		},
		{
			name:    "suffix before an infix tsr",
			routes:  []string{"/+{a}/x/+{b:[a-z/]+}/", "/+{a}/x/+{c:[a-z/]+}"},
			relaxed: true,
			path:    "/a/x/B/x/c",
			want:    "/+{a}/x/+{c:[a-z/]+}",
			params:  []string{"a/x/B", "c"},
		},
		{
			name:   "walk resumes below the vm",
			routes: []string{"/s/+{a}/x/+{b:[a-z/]+}/y", "/{p}/+{w}"},
			path:   "/s/a/x/B/x/c/Z",
			want:   "/{p}/+{w}",
			params: []string{"s", "a/x/B/x/c/Z"},
		},
		{
			name:    "tsr in place of a param",
			routes:  []string{"/+{a}/x/+{b:[a-z/]+}/y{p}", "/+{a}/x/+{b:[a-z/]+}/y"},
			relaxed: true,
			path:    "/a/x/B/x/c/y/",
			want:    "/+{a}/x/+{b:[a-z/]+}/y",
			tsr:     true,
			params:  []string{"a/x/B", "c"},
		},
		{
			name:    "tsr between wildcards",
			routes:  []string{"/+{a}/x/+{b:[a-z/]+}/y+{c}/z", "/+{a}/x/+{b:[a-z/]+}/y+{d}/w", "/+{a}/x/+{b:[a-z/]+}/y"},
			relaxed: true,
			path:    "/a/x/B/x/c/y/",
			want:    "/+{a}/x/+{b:[a-z/]+}/y",
			tsr:     true,
			params:  []string{"a/x/B", "c"},
		},
		{
			name:   "non ascii path",
			routes: []string{"/+{a}/x/+{b:[^/]+}/y"},
			path:   "/a/x/B/x/é\xff€/y",
			want:   "/+{a}/x/+{b:[^/]+}/y",
			params: []string{"a/x/B", "é\xff€"},
		},
		{
			name:   "invalid utf8 in a regexp capture",
			routes: []string{"/+{a}/x/+{b:[a-zé]+}/y", "/+{a}/x/{p}/y"},
			path:   "/a/x/B/x/é\xe9/y",
			want:   "/+{a}/x/{p}/y",
			params: []string{"a/x/B", "é\xe9"},
		},
		{
			name:   "outermost infix with a regexp",
			routes: []string{"/+{a:[a-zB/]+}/x/+{b:[a-z/]+}/y"},
			path:   "/a/x/B/x/c/y",
			want:   "/+{a:[a-zB/]+}/x/+{b:[a-z/]+}/y",
			params: []string{"a/x/B", "c"},
		},
		{
			name:   "regexp suffix next to the outermost infix",
			routes: []string{"/+{a}/x/+{b:[a-z/]+}/y", "/+{s:[a-zB/]+}"},
			path:   "/a/x/B/x/c/z",
			want:   "/+{s:[a-zB/]+}",
			params: []string{"a/x/B/x/c/z"},
		},
		{
			name:   "regexp with closures followed at each step",
			routes: []string{"/+{a}/x/+{b:(?:[a-z€/]?){20}}/y"},
			path:   "/a/x/B/x/c€d/y",
			want:   "/+{a}/x/+{b:(?:[a-z€/]?){20}}/y",
			params: []string{"a/x/B", "c€d"},
		},
		{
			name:   "no match",
			routes: []string{"/+{a}/x/+{b:[a-z/]+}/y", "/+{a}/x/{p}/z"},
			path:   "/a/x/B/x//z",
		},
		{
			name:   "hostname regexp with closures followed at each step",
			routes: []string{"+{a}.x.+{b:(?:[a-z.]?){18}}.com/"},
			host:   "a.x.b.x.c.9.x.b.c.com",
			path:   "/",
			want:   "+{a}.x.+{b:(?:[a-z.]?){18}}.com/",
			params: []string{"a.x.b.x.c.9", "b.c"},
		},
		{
			name:   "hostname infix",
			routes: []string{"+{a}.x.+{b:(?:[a-z]+\\.)*[a-z]+}.com/p"},
			host:   "A.X.b.X.c9.X.C.COM",
			path:   "/p",
			want:   "+{a}.x.+{b:(?:[a-z]+\\.)*[a-z]+}.com/p",
			params: []string{"A.X.b.X.c9", "C"},
		},
		{
			name:   "hostname non ascii",
			routes: []string{"+{a}.x.+{b:[a-zé.]+}.com/"},
			host:   "a.x.é.x.\xff.x.é.com",
			path:   "/",
			want:   "+{a}.x.+{b:[a-zé.]+}.com/",
			params: []string{"a.x.é.x.\xff", "é"},
		},
		{
			name:   "hostname suffix with a path vm",
			routes: []string{"+{a}.x.+{b:[a-z]+}/+{c}/x/+{d:[a-z/]+}/y"},
			host:   "a.x.9.x.b",
			path:   "/a/x/B/x/c/y",
			want:   "+{a}.x.+{b:[a-z]+}/+{c}/x/+{d:[a-z/]+}/y",
			params: []string{"a.x.9", "b", "a/x/B", "c"},
		},
		{
			name:   "hostname path lookup fails",
			routes: []string{"+{a}.x.+{b:[a-z]+}/foo", "/bar"},
			host:   "a.x.9.x.b",
			path:   "/bar",
			want:   "/bar",
			params: []string{},
		},
		{
			name:   "hostname path lookup matched again",
			routes: []string{"+{a}.x.+{b:c|x\\.x\\.c\\.y}.y.+{s}/"},
			host:   "q.x.x.x.Z.x.x.x.c.y.y.w",
			path:   "/",
			want:   "+{a}.x.+{b:c|x\\.x\\.c\\.y}.y.+{s}/",
			params: []string{"q.x.x.x.Z", "x.x.c.y", "w"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := []GlobalOption{WithAllowRegexpParam(true)}
			if tc.relaxed {
				opts = append(opts, WithTrailingSlash(RelaxedSlash))
			}
			f, _ := NewRouter(opts...)
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
			}
			tree := f.getTree()
			for _, lazy := range []bool{false, true} {
				c := newTestContext(f)
				idx, n, tsr := tree.lookup(http.MethodGet, tc.host, tc.path, c, lazy)
				if tc.want == "" {
					assert.Nil(t, n)
					continue
				}
				require.NotNil(t, n)
				assert.Equal(t, tc.want, n.routes[idx].Pattern())
				assert.Equal(t, tc.tsr, tsr)
				if lazy {
					assert.Empty(t, *c.params)
				} else {
					assert.Equal(t, tc.params, *c.params)
				}
			}
		})
	}
}

func Test_iTree_lookup_PikeVMMatcher(t *testing.T) {
	var calls int
	f, _ := NewRouter(WithAllowRegexpParam(true))
	require.NoError(t, onlyError(f.Add(MethodGet, "/+{a}/x/+{b:[a-z/]+}/y", emptyHandler, WithMatcher(countingMatcher{calls: &calls}))))
	require.NoError(t, onlyError(f.Add(MethodGet, "/+{a}/x/+{b:[a-z/]+}/{p}", emptyHandler)))
	c := newTestContext(f)
	idx, n, _ := f.getTree().lookup(http.MethodGet, "", "/a/x/b/x/C/x/c/y", c, false)
	require.NotNil(t, n)
	assert.Equal(t, "/+{a}/x/+{b:[a-z/]+}/{p}", n.routes[idx].Pattern())
	assert.Equal(t, []string{"a/x/b/x/C", "c", "y"}, *c.params)
	assert.NotNil(t, c.lookup.pathPikeVM)
	assert.Positive(t, calls)
}

func Test_iTree_lookup_PikeVMTxnWrites(t *testing.T) {
	f, _ := NewRouter(WithAllowRegexpParam(true))
	require.NoError(t, onlyError(f.Add(MethodGet, "/+{a}/x/+{b:[a-z/]+}/y", emptyHandler)))
	req := httptest.NewRequest(http.MethodGet, "/a/x/B/x/c/z", nil)
	route, _ := f.Match(http.MethodGet, req)
	assert.Nil(t, route)

	require.NoError(t, f.Updates(func(txn *Txn) error {
		require.NoError(t, onlyError(txn.Add(MethodGet, "/+{a}/x/+{b:[a-z/]+}/w", emptyHandler)))
		route, _ := txn.Match(http.MethodGet, req)
		assert.Nil(t, route)
		require.NoError(t, onlyError(txn.Add(MethodGet, "/+{a}/x/+{b:[a-z/]+}/z", emptyHandler)))
		route, _ = txn.Match(http.MethodGet, req)
		require.NotNil(t, route)
		assert.Equal(t, "/+{a}/x/+{b:[a-z/]+}/z", route.Pattern())
		return nil
	}))

	route, _ = f.Match(http.MethodGet, req)
	require.NotNil(t, route)
	assert.Equal(t, "/+{a}/x/+{b:[a-z/]+}/z", route.Pattern())
}

func Test_iTree_lookup_InfixWildcardMemory(t *testing.T) {
	path := func(n int) (string, string) { return "", strings.Repeat("/a/x", n) + "/Z" }
	cases := []struct {
		routes  []string
		request func(n int) (host, path string)
	}{
		{routes: []string{"/+{a}/x/+{b:[a-z/]+}/y"}, request: path},
		{routes: []string{"/+{a}/x/+{b:(?:[a-z]+/)*[a-z]+}/y"}, request: path},
		{routes: []string{"/+{a}/x/+{b:[a-z0-9-]{1,63}(?:/[a-z0-9-]{1,63})*}/y"}, request: path},
		{routes: []string{"/+{a}/x/+{b:.{1,255}}/y"}, request: path},
		{routes: []string{"/+{a}/x/+{b:(?:(?:[a-z]+/)+){1,255}}/y"}, request: path},
		{routes: []string{"/+{a}/x/+{b:(?:[a-z/]?){64}}/y"}, request: path},
		{routes: []string{"/+{a}/x/+{b:[a-z/]+}/y", "/+{a}/x/+{c:[0-9/]+}/z", "/+{a}/x/+{d:(?:[a-z]+/)*[a-z]+}/w"}, request: path},
		{
			routes:  []string{"/+{b:[a-z/]+}/y"},
			request: func(n int) (string, string) { return "", strings.Repeat("/y", 2*n) + "/Z" },
		},
		{
			routes:  []string{"+{a}.x.+{b:(?:[a-z]+\\.)*[a-z]+}.com/"},
			request: func(n int) (string, string) { return strings.Repeat("a.x.", n) + "org", "/" },
		},
	}

	for _, tc := range cases {
		t.Run(tc.routes[0], func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
			}
			tree := f.getTree()

			fresh := func(n int) uint64 {
				host, path := tc.request(n)
				least := uint64(math.MaxUint64)
				for range 3 {
					c := newTestContext(f)
					var before, after runtime.MemStats
					runtime.ReadMemStats(&before)
					_, nd, _ := tree.lookup(http.MethodGet, host, path, c, false)
					runtime.ReadMemStats(&after)
					assert.Nil(t, nd)
					least = min(least, after.TotalAlloc-before.TotalAlloc)

					allocs := testing.AllocsPerRun(3, func() {
						*c.params = (*c.params)[:0]
						tree.lookup(http.MethodGet, host, path, c, false)
					})
					assert.Zero(t, allocs)
				}
				return least
			}
			small := fresh(1 << 12)
			assert.NotZero(t, small)
			assert.Equal(t, small, fresh(1<<14))
		})
	}
}

type countingMatcher struct {
	calls *int
}

func (m countingMatcher) Match(RequestContext) bool {
	*m.calls++
	return false
}

func (m countingMatcher) Equal(o Matcher) bool {
	om, ok := o.(countingMatcher)
	return ok && om.calls == m.calls
}

func Test_iTree_lookup_MatcherEvaluatedOnce(t *testing.T) {
	var calls int
	f, _ := NewRouter()
	require.NoError(t, onlyError(f.Add(MethodGet, "/+{a}/x/+{b}", emptyHandler, WithMatcher(countingMatcher{calls: &calls}))))
	path := strings.Repeat("/a/x", 100)
	c := newTextContextOnly(f, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	_, n, _ := f.getTree().lookup(http.MethodGet, "", path, c, false)
	assert.Nil(t, n)
	assert.Equal(t, 1, calls)
	_, n, _ = f.getTree().lookup(http.MethodGet, "", path, c, false)
	assert.Nil(t, n)
	assert.Equal(t, 2, calls)
}

func Test_iTree_lookup_ReusedContext(t *testing.T) {
	f, _ := NewRouter(WithAllowRegexpParam(true), WithTrailingSlash(RelaxedSlash))
	for _, rte := range []string{
		"/+{w:a.*}/{p}/",
		"/files/+{p:[a-z/]+}/{name}",
		"/+{a}/x/+{b:(?:[a-z]+/)*[a-z]+}/y",
		"/+{a}/x/+{f:[a-z/]+}",
		"+{h:[a-z.]+}.x.+{i:(?:[a-z]+\\.)*[a-z]+}.com/",
		"+{sub}.example.+{tld}/{a}/x",
	} {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
	}

	requests := []struct{ host, path string }{
		{"", "/a/a/b//x/a.b"},
		{"", "/files/a/bb/ccc/dddd"},
		{"", "/files/x/y/zz"},
		{"", "/ab//b/x"},
		{"", "/a/x/a/x/b/c/y"},
		{"", "/a/x/a/x/b/C"},
		{"", "/a/x/b/c"},
		{"a.x.b.c.com", "/"},
		{"a.b.x.c.x.d.com", "/"},
		{"a.example.b.example.com", "/b/x"},
		{"a.example.b.example.com", "/b/y"},
		{"", "/a/a/b//x/a.b"},
	}

	tree := f.getTree()
	reused := newTestContext(f)
	for _, req := range requests {
		fresh := newTestContext(f)
		wantIdx, wantNode, wantTsr := tree.lookup(http.MethodGet, req.host, req.path, fresh, false)
		*reused.params = (*reused.params)[:0]
		idx, n, tsr := tree.lookup(http.MethodGet, req.host, req.path, reused, false)
		assert.Equal(t, wantNode, n)
		assert.Equal(t, wantIdx, idx)
		assert.Equal(t, wantTsr, tsr)
		assert.Equal(t, *fresh.params, *reused.params)
	}
}

func Test_iTree_lookup_Tsr(t *testing.T) {
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
		{
			name:  "no tsr on pattern ending with double slash",
			paths: []string{"/foo//"},
			key:   "/foo/",
		},
		{
			name:  "no tsr on pattern ending with triple slash",
			paths: []string{"/foo///"},
			key:   "/foo//",
		},
		{
			name:  "no tsr on pattern ending with double slash and sibling",
			paths: []string{"/foo//", "/foo/bar"},
			key:   "/foo/",
		},
		{
			name:  "no tsr on request ending with double slash and standalone slash node",
			paths: []string{"/foo/", "/foo//bar", "/foo//qux"},
			key:   "/foo//",
		},
		{
			name:  "no tsr on request ending with double slash",
			paths: []string{"/foo/"},
			key:   "/foo//",
		},
		{
			name:     "match with empty catch all behind a split static node",
			paths:    []string{"/foo/*{a}", "/foobar"},
			key:      "/foo",
			want:     true,
			wantPath: "/foo/*{a}",
		},
		{
			name:     "match mid edge on pattern with infix double slash",
			paths:    []string{"/foo//bar/"},
			key:      "/foo//bar",
			want:     true,
			wantPath: "/foo//bar/",
		},
		{
			name:     "incomplete match end of edge on pattern with infix double slash",
			paths:    []string{"/foo//bar"},
			key:      "/foo//bar/",
			want:     true,
			wantPath: "/foo//bar",
		},
		{
			name:     "tsr on static route wins over direct match on lower priority param route",
			paths:    []string{"/foo", "/{a}/"},
			key:      "/foo/",
			want:     true,
			wantPath: "/foo",
		},
		{
			name:     "tsr on static route wins over direct match on lower priority param route reversed",
			paths:    []string{"/foo/", "/{a}"},
			key:      "/foo",
			want:     true,
			wantPath: "/foo/",
		},
		{
			name:     "match infix wildcard with added trailing slash",
			paths:    []string{"/+{a}/"},
			key:      "/x/y",
			want:     true,
			wantPath: "/+{a}/",
		},
		{
			name:  "no tsr when a suffix catch-all direct matches",
			paths: []string{"/+{a}/", "/+{a}"},
			key:   "/x/y",
		},
		{
			name:     "match regex param with extra trailing slash",
			paths:    []string{"/{p:[a-z]+}"},
			key:      "/abc/",
			want:     true,
			wantPath: "/{p:[a-z]+}",
		},
		{
			name:  "no tsr for suffix regex catch-all rejecting the full capture",
			paths: []string{"/+{w:[a-z]+}"},
			key:   "/abc/",
		},
		{
			name:  "no tsr when a suffix regex catch-all direct matches",
			paths: []string{"/+{w:[a-z/]+}", "/+{w:[a-z/]+}/x"},
			key:   "/abc/",
		},
		{
			name:  "no tsr for suffix regex catch-all rejecting the full capture with sibling",
			paths: []string{"/+{w:[a-z]+}", "/+{w:[a-z]+}/x"},
			key:   "/abc/",
		},
		{
			name:  "no tsr on slash node when parent suffix regex catch-all direct matches",
			paths: []string{"/+{w:[a-z/]+}", "/+{w:[a-z/]+}/x", "/+{w:[a-z/]+}/y"},
			key:   "/abc/",
		},
		{
			name:  "no tsr on slash node when parent suffix catch-all direct matches",
			paths: []string{"/+{a}", "/+{a}/x", "/+{a}/y"},
			key:   "/abc/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithTrailingSlash(RelaxedSlash), WithAllowRegexpParam(true))
			for _, path := range tc.paths {
				require.NoError(t, onlyError(f.Add(MethodGet, path, emptyHandler)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			idx, n, tsr := tree.lookup(http.MethodGet, "", tc.key, c, true)
			assert.Equal(t, tc.want, tsr)
			if tc.want {
				require.NotNil(t, n)
				require.NotNil(t, n.routes[idx])
				assert.Equal(t, tc.wantPath, n.routes[idx].pattern.str)
			}
		})
	}
}

func Test_iTree_lookup_TsrMatchers(t *testing.T) {
	type rte struct {
		pattern string
		opts    []RouteOption
	}

	cases := []struct {
		name         string
		routes       []rte
		target       string
		wantPath     string
		wantTsr      bool
		wantMatchers int
	}{
		{
			name:         "matcher pass on tsr candidate",
			routes:       []rte{{"/foo/", []RouteOption{WithQueryMatcher("mode", "beta")}}},
			target:       "/foo?mode=beta",
			wantPath:     "/foo/",
			wantTsr:      true,
			wantMatchers: 1,
		},
		{
			name:   "matcher veto on tsr candidate",
			routes: []rte{{"/foo/", []RouteOption{WithQueryMatcher("mode", "beta")}}},
			target: "/foo",
		},
		{
			name: "matcher veto reopens lower priority direct match",
			routes: []rte{
				{"/foo/", []RouteOption{WithQueryMatcher("mode", "beta")}},
				{"/{a}", nil},
			},
			target:   "/foo",
			wantPath: "/{a}",
		},
		{
			name: "matcher pass keeps tsr priority over direct match",
			routes: []rte{
				{"/foo/", []RouteOption{WithQueryMatcher("mode", "beta")}},
				{"/{a}", nil},
			},
			target:       "/foo?mode=beta",
			wantPath:     "/foo/",
			wantTsr:      true,
			wantMatchers: 1,
		},
		{
			name: "matched route first on shared tsr candidate",
			routes: []rte{
				{"/foo/", []RouteOption{WithQueryMatcher("mode", "beta")}},
				{"/foo/", nil},
			},
			target:       "/foo?mode=beta",
			wantPath:     "/foo/",
			wantTsr:      true,
			wantMatchers: 1,
		},
		{
			name: "fallback route on shared tsr candidate",
			routes: []rte{
				{"/foo/", []RouteOption{WithQueryMatcher("mode", "beta")}},
				{"/foo/", nil},
			},
			target:   "/foo",
			wantPath: "/foo/",
			wantTsr:  true,
		},
		{
			name:         "matcher pass on empty catch-all candidate",
			routes:       []rte{{"/foo/*{x}", []RouteOption{WithQueryMatcher("mode", "beta")}}},
			target:       "/foo?mode=beta",
			wantPath:     "/foo/*{x}",
			wantTsr:      true,
			wantMatchers: 1,
		},
		{
			name:   "matcher veto on empty catch-all candidate",
			routes: []rte{{"/foo/*{x}", []RouteOption{WithQueryMatcher("mode", "beta")}}},
			target: "/foo",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithTrailingSlash(RelaxedSlash))
			for _, r := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, r.pattern, emptyHandler, r.opts...)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			c.resetWithRequest(req)
			idx, n, tsr := tree.lookup(http.MethodGet, "", req.URL.Path, c, true)
			if tc.wantPath == "" {
				assert.Nil(t, n)
				return
			}
			require.NotNil(t, n)
			assert.Equal(t, tc.wantTsr, tsr)
			assert.Equal(t, tc.wantPath, n.routes[idx].pattern.str)
			assert.Equal(t, tc.wantMatchers, len(n.routes[idx].matchers))
		})
	}
}

func Test_iTree_delete(t *testing.T) {
	f, _ := NewRouter()
	routes := make([]route, len(githubAPI))
	copy(routes, githubAPI)

	for _, rte := range routes {
		require.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler)))
	}

	rand.Shuffle(len(routes), func(i, j int) { routes[i], routes[j] = routes[j], routes[i] })

	for _, rte := range routes {
		deletedRoute, err := f.Delete([]string{rte.method}, rte.path)
		require.NoError(t, err)
		assert.Equal(t, rte.path, deletedRoute.Pattern())
	}

	it := f.Iter()
	cnt := len(slices.Collect(it.All()))

	tree := f.getTree()
	assert.Equal(t, 0, cnt)
	assert.Equal(t, 0, len(tree.patterns.statics))
	assert.Equal(t, 0, len(tree.patterns.params))
	assert.Equal(t, 0, len(tree.patterns.wildcards))
}

func Test_iTree_deleteTxn(t *testing.T) {
	f, _ := NewRouter()
	routes := make([]route, len(githubAPI))
	copy(routes, githubAPI)

	for _, rte := range routes {
		require.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler)))
	}

	rand.Shuffle(len(routes), func(i, j int) { routes[i], routes[j] = routes[j], routes[i] })

	require.NoError(t, f.Updates(func(txn *Txn) error {
		for _, rte := range routes {
			deletedRoute, err := txn.Delete([]string{rte.method}, rte.path)
			if err != nil {
				return err
			}
			assert.Equal(t, rte.path, deletedRoute.Pattern())
		}
		return nil
	}))

	it := f.Iter()
	cnt := len(slices.Collect(it.All()))

	tree := f.getTree()
	assert.Equal(t, 0, cnt)
	assert.Equal(t, 0, len(tree.patterns.statics))
	assert.Equal(t, 0, len(tree.patterns.params))
	assert.Equal(t, 0, len(tree.patterns.wildcards))
}

func Test_iTree_deleteRoot(t *testing.T) {
	f, _ := NewRouter()
	require.NoError(t, onlyError(f.Add(MethodOptions, "/foo/bar", emptyHandler)))
	deletedRoute, err := f.Delete(MethodOptions, "/foo/bar")
	require.NoError(t, err)
	assert.Equal(t, "/foo/bar", deletedRoute.Pattern())
	tree := f.getTree()
	assert.Equal(t, 0, len(tree.patterns.statics))
	assert.Equal(t, 0, len(tree.patterns.params))
	assert.Equal(t, 0, len(tree.patterns.wildcards))

	require.NoError(t, onlyError(f.Add(MethodOptions, "example.com/foo/bar", emptyHandler)))
	deletedRoute, err = f.Delete(MethodOptions, "example.com/foo/bar")
	require.NoError(t, err)
	assert.Equal(t, "example.com/foo/bar", deletedRoute.Pattern())
	tree = f.getTree()
	assert.Equal(t, 0, len(tree.patterns.statics))
	assert.Equal(t, 0, len(tree.patterns.params))
	assert.Equal(t, 0, len(tree.patterns.wildcards))
}

func Test_iTree_deleteWildcard(t *testing.T) {
	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/foo/+{args}", emptyHandler)
	deletedRoute, err := f.Delete(MethodGet, "/foo")
	assert.ErrorIs(t, err, ErrRouteNotFound)
	assert.Nil(t, deletedRoute)
	f.MustAdd(MethodGet, "/foo/{bar}", emptyHandler)
	deletedRoute, err = f.Delete(MethodGet, "/foo/{bar}")
	assert.NoError(t, err)
	assert.Equal(t, "/foo/{bar}", deletedRoute.Pattern())
	assert.True(t, f.Has(MethodGet, "/foo/+{args}"))
}

func Test_lookupState_regexp(t *testing.T) {
	re, err := compileAnchoredRegexp("[a-z/]+")
	require.NoError(t, err)
	n := &node{regexp: re, prog: compileProg(re)}
	s := new(lookupState)

	w := s.regexp(n)
	w.scanned, w.suffixed = true, true
	ends := s.ends(n, "a/b/c/d", 0, slashDelim)
	assert.Equal(t, 1, ends.Next(0))
	steps := ends.Steps()
	s.reset()

	got := s.regexp(n)
	assert.Same(t, w, got)
	assert.False(t, got.scanned)
	assert.False(t, got.suffixed)
	got.ends.Start(n.prog, strings.Clone("a/b/c/d"), 0, slashDelim)
	assert.Equal(t, 1, got.ends.Next(1))
	assert.Greater(t, got.ends.Steps(), steps)
}
