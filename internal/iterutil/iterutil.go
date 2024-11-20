// The code in this package is derivative of https://github.com/jub0bs/iterutil (all credit to jub0bs).
// Mount of this source code is governed by a MIT License that can be found
// at https://github.com/jub0bs/iterutil/blob/main/LICENSE.

package iterutil

import "iter"

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
