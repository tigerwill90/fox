package fox

import (
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainLookup(t *testing.T) {
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
				"exemple.com/foo/bar/baz/{$1}/jo",
				"exemple.com/foo/*{any}/baz/{$1}/jo",
				"exemple.com/foo/{ps}/baz/{$1}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "exemple.com/foo/bar/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "static hostname with complex overlapping route with static priority and regexp",
			routes: []string{
				"exemple.com/foo/bar/baz/{$1:[0-9]}/jo",
				"exemple.com/foo/*{any:.*}/baz/{$1:.*}/jo",
				"exemple.com/foo/{ps:.*}/baz/{$1:.*}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "exemple.com/foo/bar/baz/{$1:[0-9]}/jo",
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
				"exemple.com/foo/bar/baz/{$1}/jo",
				"exemple.com/foo/*{any}/baz/{$1}/jo",
				"exemple.com/foo/{ps}/baz/{$1}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bam/baz/1/jo",
			wantPath: "exemple.com/foo/{ps}/baz/{$1}/jo",
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
				"exemple.com/foo/bar/baz/{$1:[0-9]}/jo",
				"exemple.com/foo/*{any:.*}/baz/{$1:.*}/jo",
				"exemple.com/foo/{ps:.*}/baz/{$1:.*}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bam/baz/1/jo",
			wantPath: "exemple.com/foo/{ps:.*}/baz/{$1:.*}/jo",
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
				"exemple.com/foo/bar/baz/{$1}/jo",
				"{any}.com/foo/*{any}/baz/{$1}/jo",
				"exemple.{tld}/foo/{ps}/baz/{$1}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "exemple.com/foo/bar/baz/{$1}/jo",
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
				"exemple.com/foo/bar/baz/{$1}/jo",
				"{any:.*}.com/foo/*{any}/baz/{$1}/jo",
				"exemple.{tld}/foo/{ps}/baz/{$1}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "exemple.com/foo/bar/baz/{$1}/jo",
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
				"exemple.com/foo/bar/baz/{$1}/jo",
				"{any}.com/foo/*{any}/baz/{$1}/jo",
				"exemple.{tld}/foo/{ps}/baz/{$1}/jo",
			},
			host:     "EXEMPLE.COM",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "exemple.com/foo/bar/baz/{$1}/jo",
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
				"exemple.{tld}/foo/*{any}/baz/{$1}/jo",
				"exemple.com/foo/{ps}/baz/{$1}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bam/baz/1/jo",
			wantPath: "exemple.com/foo/{ps}/baz/{$1}/jo",
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
			name: "make hostname case sensitive with regexp",
			routes: []string{
				"{a:[a-z]+}.b.c/foo",
				"{a:[A-Z]+}.{b:[A-Z]+}.{c:[A-Z]+}/foo",
				"{a:[A-Z]+}.{b:[a-z]+}.c/foo",
			},
			host:     "FOO.B.C",
			path:     "/foo",
			wantPath: "{a:[A-Z]+}.{b:[A-Z]+}.{c:[A-Z]+}/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "FOO",
				},
				{
					Key:   "b",
					Value: "B",
				},
				{
					Key:   "c",
					Value: "C",
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
			name: "regexp priority with tsr but backtrack to most specific",
			routes: []string{
				"{a:.*}.{b}.{c}/foo/",
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
			name: "path priority with tsr hostname",
			routes: []string{
				"{a}.{b}.{c}/{d}",
				"{a}.{b}.c/{d}",
				"{a}.b.c/{path}/bar/",
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
				"{a:[A-z]+}.b.c/{path}/bar/",
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
				"*{any}/bar",
			},
			host:     "foo.com",
			path:     "/bar",
			wantPath: "*{any}/bar",
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
				"*{any:[A-Z.]+}/bar",
			},
			host:     "FOO.COM",
			path:     "/bar",
			wantPath: "*{any:[A-Z.]+}/bar",
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
				"*{any}.com/bar",
			},
			host:     "a.b.com",
			path:     "/bar",
			wantPath: "*{any}.com/bar",
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
				"*{any}.com/bar",
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
				"*{any}.com/bar",
			},
			host:     "a.b.com",
			path:     "/bar",
			wantPath: "*{any}.com/bar",
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
				"*{any:[A-Z.]+}.com/bar",
			},
			host:     "A.B.com",
			path:     "/bar",
			wantPath: "*{any:[A-Z.]+}.com/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "A.B",
				},
			},
		},
		{
			name: "simple infix wildcard",
			routes: []string{
				"example.*{any}.com/bar",
			},
			host:     "example.foo.bar.com",
			path:     "/bar",
			wantPath: "example.*{any}.com/bar",
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
				"example.*{any:[A-Z.]+}.com/bar",
			},
			host:     "example.FOO.BAR.com",
			path:     "/bar",
			wantPath: "example.*{any:[A-Z.]+}.com/bar",
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
				"*{any}.{tld}/bar",
			},
			host:     "a.b.com",
			path:     "/bar",
			wantPath: "*{any}.{tld}/bar",
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
				"{first}.*{any}.{tld}/bar",
			},
			host:     "foo.s1.s2.s3.com",
			path:     "/bar",
			wantPath: "{first}.*{any}.{tld}/bar",
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
				"{first}.{second}.*{any}/bar",
			},
			host:     "first.second.third.com",
			path:     "/bar",
			wantPath: "{first}.{second}.*{any}/bar",
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
				"*{any}.b.com/bar",
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
			name: "eval param with wildcard fallback",
			routes: []string{
				"*{any}.b.com/bar",
				"{ps}.b.com/bar",
			},
			host:     "foo.b.b.com",
			path:     "/bar",
			wantPath: "*{any}.b.com/bar",
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
				"a.*{any}.com/bar",
				"a.*{any}/bar",
			},
			host:     "a.bar.baz.com",
			path:     "/bar",
			wantPath: "a.*{any}.com/bar",
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
				"a.*{any}.com/bar",
				"a.*{any}/bar",
			},
			host:     "a.bar.baz.ch",
			path:     "/bar",
			wantPath: "a.*{any}/bar",
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
				"a.*{3}.com/bar",
				"a.*{1:[A-z.]+}.com/bar",
				"a.*{2:[0-9.]+}.com/bar",
			},
			host:     "a.b.c.com",
			path:     "/bar",
			wantPath: "a.*{1:[A-z.]+}.com/bar",
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
				"a.*{3}.com/bar",
				"a.*{1:[A-z.]+}.com/bar",
				"a.*{2:[0-9.]+}.com/bar",
			},
			host:     "a.1.2.com",
			path:     "/bar",
			wantPath: "a.*{2:[0-9.]+}.com/bar",
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
				"a.*{3}.com/bar",
				"a.*{1:[A-z.]+}.com/bar",
				"a.*{2:[0-9.]+}.com/bar",
			},
			host:     "a.b.2.com",
			path:     "/bar",
			wantPath: "a.*{3}.com/bar",
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
				"*{a}.{b}.{c}/{d}",
				"*{a}.{b}.c/{d}",
				"*{a}.b.c/{path}/bar/",
				"/{a}/barr",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "*{a}.b.c/{path}/bar/",
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
				"*{a}.{b}.{c}/{d}",
				"*{a}.{b}.c/{d}",
				"*{a}.b.c/{path}/bar/",
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
				"{a:.*}.{b:.*}.*{c:nomatch}/john/bar",
				"*{a:nomatch}.{b}.c/{d}",
				"*{a:[A-z]+}.b.c/{path}/bar/",
				"/{a:^$}/bar",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "*{a:[A-z]+}.b.c/{path}/bar/",
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
				"{a:.*}.{b:.*}.*{c:nomatch}/john/bar",
				"*{a:foo}.{b}.c/{d}/bar",
				"*{a:[A-z]+}.b.c/{path}/bar/",
				"/{a:^$}/bar",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "*{a:foo}.{b}.c/{d}/bar",
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
				"{a:.*}.{b:.*}.*{c:.*}/john/bar",
				"*{a:foo}.{b}.c/{d}/bar",
				"*{a:[A-z]+}.b.c/{path}/bar/",
				"/{a:^$}/bar",
			},
			host:     "foo.b.c.com",
			path:     "/john/bar",
			wantPath: "{a:.*}.{b:.*}.*{c:.*}/john/bar",
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(AllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			idx, n := tree.lookup(http.MethodGet, tc.host, tc.path, c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.routes[idx].pattern)
			assert.Equal(t, tc.wantTsr, c.tsr)
			c.route = n.routes[idx]
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}
}
