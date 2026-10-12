// Copyright 2026 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

package nfa

import (
	"math/rand/v2"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

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
	for e := start + 1; e <= len(s); e++ {
		if (e == len(s) || s[e] == delim) && re.MatchString(s[start:e]) {
			ends = append(ends, e)
		}
	}
	return ends
}

func wantStarts(re *regexp.Regexp, s string) []int {
	var starts []int
	for i := range len(s) {
		if (i == 0 || s[i-1] < utf8.RuneSelf) && re.MatchString(s[i:]) {
			starts = append(starts, i)
		}
	}
	return starts
}

func gotStarts(r *Regexp, sc *Scratch, s string) []int {
	bits := make([]uint64, (len(s)+63)/64)
	r.Starts(sc, s, bits)
	var starts []int
	for i := range len(s) {
		if (i == 0 || s[i-1] < utf8.RuneSelf) && bits[i/64]&(1<<(i%64)) != 0 {
			starts = append(starts, i)
		}
	}
	return starts
}

func delimsOf(s string, delim byte) []int {
	var delims []int
	for i := range len(s) {
		if s[i] == delim {
			delims = append(delims, i)
		}
	}
	return delims
}

func TestEnds(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, expr := range exprs {
		t.Run(expr, func(t *testing.T) {
			re, r := compileTest(t, expr)
			sc := new(Scratch)
			for range 5000 {
				s := randString(rng)
				for start := 0; start <= len(s); start++ {
					assert.Equal(t, wantEnds(re, s, start, '/'), r.Ends(sc, s, start, '/', nil, nil))
				}
			}
		})
	}
}

func TestStarts(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for _, expr := range exprs {
		t.Run(expr, func(t *testing.T) {
			re, r := compileTest(t, expr)
			sc := new(Scratch)
			for range 5000 {
				s := randString(rng)
				assert.Equal(t, wantStarts(re, s), gotStarts(r, sc, s))
			}
		})
	}
}

func TestEndsSeen(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	for _, expr := range exprs {
		t.Run(expr, func(t *testing.T) {
			re, r := compileTest(t, expr)
			sc := new(Scratch)
			var seen Seen
			for range 2000 {
				s := randString(rng)
				seen.Reset(r.Len(), delimsOf(s, '/'))
				reported := make(map[int]bool)
				for range 4 {
					start := rng.IntN(len(s) + 1)
					got := r.Ends(sc, s, start, '/', &seen, nil)
					assert.True(t, slices.IsSorted(got))
					for _, e := range wantEnds(re, s, start, '/') {
						assert.True(t, reported[e] || slices.Contains(got, e))
					}
					for _, e := range got {
						assert.True(t, re.MatchString(s[start:e]))
						reported[e] = true
					}
				}
			}
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
		sc := new(Scratch)
		assert.Equal(t, wantEnds(re, s, start, '/'), r.Ends(sc, s, start, '/', nil, nil))
		assert.Equal(t, wantStarts(re, s), gotStarts(r, sc, s))
	})
}
