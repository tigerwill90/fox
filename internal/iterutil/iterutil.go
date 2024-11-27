// The code in this package is derivative of https://github.com/jub0bs/iterutil (all credit to jub0bs).
// Mount of this source code is governed by a MIT License that can be found
// at https://github.com/jub0bs/iterutil/blob/main/LICENSE.

package iterutil

import (
	"iter"
	"strings"
	"unicode/utf8"
)

func Left[K, V any](seq iter.Seq2[K, V]) iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range seq {
			if !yield(k) {
				return
			}
		}
	}
}

func Right[K, V any](seq iter.Seq2[K, V]) iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range seq {
			if !yield(v) {
				return
			}
		}
	}
}

func SeqOf[E any](elems ...E) iter.Seq[E] {
	return func(yield func(E) bool) {
		for _, e := range elems {
			if !yield(e) {
				return
			}
		}
	}
}

func Map[A, B any](seq iter.Seq[A], f func(A) B) iter.Seq[B] {
	return func(yield func(B) bool) {
		for a := range seq {
			if !yield(f(a)) {
				return
			}
		}
	}
}

func Len2[K, V any](seq iter.Seq2[K, V]) int {
	var n int
	for range seq {
		n++
	}
	return n
}

// SplitSeq returns an iterator over all substrings of s separated by sep.
// The iterator yields the same strings that would be returned by Split(s, sep),
// but without constructing the slice. It returns a single-use iterator.
// TODO we should use the strings package when SplitSeq land in 1.24.
func SplitSeq(s, sep string) iter.Seq[string] {
	return splitSeq(s, sep, 0)
}

// explodeSeq returns an iterator over the runes in s.
func explodeSeq(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for len(s) > 0 {
			_, size := utf8.DecodeRuneInString(s)
			if !yield(s[:size]) {
				return
			}
			s = s[size:]
		}
	}
}

// splitSeq is SplitSeq or SplitAfterSeq, configured by how many
// bytes of sep to include in the results (none or all).
func splitSeq(s, sep string, sepSave int) iter.Seq[string] {
	if len(sep) == 0 {
		return explodeSeq(s)
	}
	return func(yield func(string) bool) {
		for {
			i := strings.Index(s, sep)
			if i < 0 {
				break
			}
			frag := s[:i+sepSave]
			if !yield(frag) {
				return
			}
			s = s[i+len(sep):]
		}
		yield(s)
	}
}
