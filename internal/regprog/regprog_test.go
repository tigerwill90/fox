// Copyright 2026 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

package regprog

import (
	"math/rand/v2"
	"regexp"
	"regexp/syntax"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var exprs = []string{
	"[a-z/]+",
	"[a-z0-9]+(?:(?:\\.|_|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:\\.|_|__|-+)[a-z0-9]+)*)*",
	"\\w+(?:/\\w+)*",
	"(?:[^/]+/)*[^/]+",
	"[a-z/]{1,6}",
	"(?i)[a-z/]+",
	".+\\.css",
	"(?s).+",
	"a*",
	"(?:a|b/)+b",
	"\\ba\\b.*",
	"(?:\\B.)+",
	"(?m)^a$",
	"[^\\x{FFFD}]+",
	"é+/?",
	"(?U)a+b",
	"(?:a?){20}b?",
	"(?:[a-z/]?){24}",
	"(?:\\b?a?/?){12}",
	"(?m)(?:^?a?$?){10}",
}

const alphabet = "ab/.A_-\n"

func randString(rng *rand.Rand) string {
	var sb strings.Builder
	for range rng.IntN(12) {
		switch rng.IntN(12) {
		case 0:
			sb.WriteString("é")
		case 1:
			sb.WriteByte(0xff)
		default:
			sb.WriteByte(alphabet[rng.IntN(len(alphabet))])
		}
	}
	return sb.String()
}

func compileTest(t testing.TB, expr string) (*regexp.Regexp, *Regexp) {
	re, err := regexp.Compile("^(?:" + expr + ")$")
	require.NoError(t, err)
	r, err := Compile(re)
	require.NoError(t, err)
	return re, r
}

func wantEnds(re *regexp.Regexp, s string, start int, delim byte) []int {
	var ends []int
	for e := start + 1; e < len(s); e++ {
		if s[e] == delim && re.MatchString(s[start:e]) {
			ends = append(ends, e)
		}
	}
	return ends
}

func gotEnds(r *Regexp, l *Ends, s string, start int) []int {
	l.Start(r, s, start, '/')
	var ends []int
	for e := l.Next(start); e >= 0; e = l.Next(e + 1) {
		ends = append(ends, e)
	}
	return ends
}

func firstEnd(ends []int, from int) int {
	for _, e := range ends {
		if e >= from {
			return e
		}
	}
	return -1
}

func TestEnds(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, expr := range exprs {
		t.Run(expr, func(t *testing.T) {
			re, r := compileTest(t, expr)
			l := new(Ends)
			for range 5000 {
				s := randString(rng)
				for start := 0; start <= len(s); start++ {
					assert.Equal(t, wantEnds(re, s, start, '/'), gotEnds(r, l, s, start))
				}
			}
		})
	}
}

func TestEndsNext(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for _, expr := range exprs {
		t.Run(expr, func(t *testing.T) {
			re, r := compileTest(t, expr)
			l := new(Ends)
			for range 2000 {
				s := randString(rng)
				start := rng.IntN(len(s) + 1)
				want := wantEnds(re, s, start, '/')
				l.Start(r, s, start, '/')
				reach := start
				for range 4 {
					from := start + rng.IntN(len(s)-start+1)
					end := l.Next(from)
					assert.Equal(t, firstEnd(want, from), end)
					reach = max(reach, from, end)
					assert.GreaterOrEqual(t, l.Reach(), reach)
				}
			}
		})
	}
}

func TestFollow(t *testing.T) {
	for _, tc := range []struct {
		expr    string
		expands bool
	}{
		{expr: "[a-z0-9]+(?:(?:\\.|_|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:\\.|_|__|-+)[a-z0-9]+)*)*", expands: false},
		{expr: "(?:[a-z]+/){1,255}", expands: false},
		{expr: "(?:a?){16}", expands: true},
		{expr: "(?:a?){1000}", expands: true},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			_, r := compileTest(t, tc.expr)
			assert.Equal(t, tc.expands, r.Expands())
			for _, f := range r.follow {
				assert.LessOrEqual(t, len(f.pcs), (maxClosure+2)*r.Len())
			}
		})
	}
}

func TestConsumes(t *testing.T) {
	for _, tc := range []struct {
		expr                   string
		slash, dot, underscore bool
	}{
		{expr: "[a-z]+", slash: false, dot: false, underscore: false},
		{expr: "[^/]+", slash: false, dot: true, underscore: true},
		{expr: "\\d+|a", slash: false, dot: false, underscore: false},
		{expr: "(?i)[a-z]+\\.", slash: false, dot: true, underscore: false},
		{expr: "[a-z/]+", slash: true, dot: false, underscore: false},
		{expr: ".+", slash: true, dot: true, underscore: true},
		{expr: "\\W", slash: true, dot: true, underscore: false},
		{expr: "a|(?:b/)+", slash: true, dot: false, underscore: false},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			_, r := compileTest(t, tc.expr)
			assert.Equal(t, tc.slash, r.Consumes('/'))
			assert.Equal(t, tc.dot, r.Consumes('.'))
			assert.Equal(t, tc.underscore, r.Consumes('_'))
		})
	}
}

func FuzzEnds(f *testing.F) {
	for _, expr := range exprs {
		f.Add(expr, "ab/a.b/é/", 0)
	}
	f.Fuzz(func(t *testing.T, expr, s string, start int) {
		if _, err := syntax.Parse(expr, syntax.Perl); err != nil || len(s) > 64 {
			return
		}
		re, err := regexp.Compile("^(?:" + expr + ")$")
		if err != nil {
			return
		}
		r, err := Compile(re)
		require.NoError(t, err)
		start = max(0, min(start, len(s)))
		assert.Equal(t, wantEnds(re, s, start, '/'), gotEnds(r, new(Ends), s, start))
	})
}
