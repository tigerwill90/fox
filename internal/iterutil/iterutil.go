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

func SplitSeqN(s, sep string, n uint) iter.Seq[string] {
	return splitSeqN(s, sep, 0, n)
}

func splitSeqN(s, sep string, sepSave int, n uint) iter.Seq[string] {
	if len(sep) == 0 {
		return explodeSeqN(s, n)
	}

	return func(yield func(string) bool) {
		if n == 0 {
			return
		}
		var j uint
		for j < n-1 {
			i := strings.Index(s, sep)
			if i < 0 {
				break
			}
			frag := s[:i+sepSave]
			if !yield(frag) {
				return
			}
			s = s[i+len(sep):]
			j++
		}
		yield(s)
	}
}

func explodeSeqN(s string, n uint) iter.Seq[string] {
	return func(yield func(string) bool) {
		l := uint(utf8.RuneCountInString(s))
		if n > l {
			n = l
		}

		ni := int(n)
		for i := 0; i < ni-1; i++ {
			_, size := utf8.DecodeRuneInString(s)
			if !yield(s[:size]) {
				return
			}
			s = s[size:]
		}
		if ni > 0 {
			yield(s)
		}
	}
}
