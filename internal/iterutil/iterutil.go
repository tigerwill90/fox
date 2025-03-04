// The code in this package is derivative of https://github.com/jub0bs/iterutil (all credit to jub0bs).
// Mount of this source code is governed by a MIT License that can be found
// at https://github.com/jub0bs/iterutil/blob/main/LICENSE.

package iterutil

import (
	"bytes"
	"github.com/tigerwill90/fox/internal/constraints"
	"iter"
	"strings"
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

func Take[I constraints.Integer, E any](seq iter.Seq[E], count I) iter.Seq[E] {
	return func(yield func(E) bool) {
		for e := range seq {
			if count > 0 {
				if !yield(e) {
					return
				}
				count--
				continue
			}
			return
		}
	}
}

func At[I constraints.Integer, E any](seq iter.Seq[E], n I) (e E, ok bool) {
	if n < 0 {
		panic("cannot be negative")
	}
	for v := range seq {
		if 0 < n {
			n--
			continue
		}
		e = v
		ok = true
		return
	}
	return
}

func SplitBytesSeq(s, sep []byte) iter.Seq[[]byte] {
	return splitBytesSeq(s, sep)
}

func splitBytesSeq(s, sep []byte) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for {
			i := bytes.Index(s, sep)
			if i < 0 {
				break
			}
			frag := s[:i]
			if !yield(frag) {
				return
			}
			s = s[i+len(sep):]
		}
		yield(s)
	}
}

func SplitStringSeq(s, sep string) iter.Seq[string] {
	return splitStringSeq(s, sep)
}

func splitStringSeq(s, sep string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for {
			i := strings.Index(s, sep)
			if i < 0 {
				break
			}
			frag := s[:i]
			if !yield(frag) {
				return
			}
			s = s[i+len(sep):]
		}
		yield(s)
	}
}

func BackwardSplitStringSeq(s, sep string) iter.Seq[string] {
	return backwardSplitSeq(s, sep)
}

func backwardSplitSeq(s, sep string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for {
			i := strings.LastIndex(s, sep)
			if i < 0 {
				break
			}
			frag := s[i+len(sep):]
			if !yield(frag) {
				return
			}
			s = s[:i]
		}
		yield(s)
	}
}
