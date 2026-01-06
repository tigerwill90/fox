package slicesutil

import "cmp"

// EqualUnsorted reports whether two slices contain the same elements,
// regardless of order. Duplicates are accounted for: [1, 1, 2] is not
// equal to [1, 2, 2]. Returns true if both slices are empty.
//
// Runs in O(n²) time, but the matched slice should be stack-allocated in
// most cases. A hash-based O(n) approach was considered, but for small
// slices the cost of populating a map outweighs the quadratic comparison
// cost. Additionally, maps with more than 8 elements are heap-allocated,
// which adds to the cost.
func EqualUnsorted[S ~[]E, E comparable](s1, s2 S) bool {
	if len(s1) != len(s2) {
		return false
	}

	matched := make([]bool, len(s2))

outer:
	for _, a := range s1 {
		for i, b := range s2 {
			if !matched[i] && a == b {
				matched[i] = true
				continue outer
			}
		}
		return false
	}
	return true
}

// Overlap reports whether two sorted slices have at least one element in common.
// Both slices must be sorted in ascending order. Returns true if both slices are empty.
func Overlap[S ~[]E, E cmp.Ordered](s1, s2 S) bool {
	if len(s1) == 0 && len(s2) == 0 {
		return true
	}

	i, j := 0, 0
	for i < len(s1) && j < len(s2) {
		switch cmp.Compare(s1[i], s2[j]) {
		case -1:
			i++
		case 1:
			j++
		default:
			return true
		}
	}
	return false
}
