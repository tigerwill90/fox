// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"iter"
	"strings"
)

const stackSizeThreshold = 25

type stack struct {
	edges []*node
}

// RouteMatch represents a route matched by a reverse lookup operation.
type RouteMatch struct {
	*Route
	// Tsr is true when the match required trailing slash adjustment.
	Tsr bool
}

// Iter provide a set of range iterators for traversing registered methods and routes. Iter capture a point-in-time
// snapshot of the routing tree. Therefore, all iterators returned by Iter will not observe subsequent write on the
// router or on the transaction from which the Iter is created.
type Iter struct {
	tree     *iTree
	patterns *node
	names    *node
	methods  map[string]uint
	maxDepth int // tree or txn maxDepth
}

// Methods returns a range iterator over all HTTP methods registered in the routing tree. The iterator reflect a snapshot
// of the routing tree at the time [Iter] is created. This function is safe for concurrent use by multiple goroutine and
// while mutation on routes are ongoing.
func (it Iter) Methods() iter.Seq[string] {
	return func(yield func(string) bool) {
		for k := range it.methods {
			if !yield(k) {
				return
			}
		}
	}
}

// Routes returns a range iterator over all registered routes in the routing tree that exactly match the provided route
// pattern. The iterator reflect a snapshot of the routing tree at the time [Iter] is created. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing.
func (it Iter) Routes(pattern string) iter.Seq[*Route] {
	return func(yield func(*Route) bool) {
		matched := it.patterns.searchPattern(pattern)
		if matched == nil || !matched.isLeaf() {
			return
		}

		for _, route := range matched.routes {
			if route.Pattern() == pattern {
				if !yield(route) {
					return
				}
			}
		}
	}
}

// NamePrefix returns a range iterator over all routes in the routing tree that match a given name prefix. The iterator
// reflect a snapshot of the routing tree at the time [Iter] is created. This function is safe for concurrent use by
// multiple goroutine and while mutation on routes are ongoing.
func (it Iter) NamePrefix(prefix string) iter.Seq[*Route] {
	return func(yield func(*Route) bool) {
		var stacks []stack
		if it.maxDepth < stackSizeThreshold {
			stacks = make([]stack, 0, stackSizeThreshold) // stack allocation
		} else {
			stacks = make([]stack, 0, it.maxDepth) // heap allocation TODO this inaccruate now (this is currently the max skipStack)
		}

		matched := it.names.searchName(prefix)
		if matched == nil {
			return
		}

		stacks = append(stacks, stack{
			edges: []*node{matched},
		})

		for len(stacks) > 0 {
			n := len(stacks)
			last := stacks[n-1]
			elem := last.edges[0]

			if len(last.edges) > 1 {
				stacks[n-1].edges = last.edges[1:]
			} else {
				stacks = stacks[:n-1]
			}

			if len(elem.statics) > 0 {
				stacks = append(stacks, stack{edges: elem.statics})
			}

			if elem.isLeaf() {
				if !yield(elem.routes[0]) {
					return
				}
			}
		}
	}
}

// PatternPrefix returns a range iterator over all routes in the routing tree that match a given pattern prefix.
// The iterator reflect a snapshot of the routing tree at the time [Iter] is created. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing.
// Note: Partial parameter syntax (e.g., /users/{name:) is not supported and will not match any routes.
func (it Iter) PatternPrefix(prefix string) iter.Seq[*Route] {
	return func(yield func(*Route) bool) {
		var stacks []stack
		if it.maxDepth < stackSizeThreshold {
			stacks = make([]stack, 0, stackSizeThreshold) // stack allocation
		} else {
			stacks = make([]stack, 0, it.maxDepth) // heap allocation TODO this inaccruate now (this is currently the max skipStack)
		}

		matched := it.patterns.searchPattern(prefix)
		if matched == nil {
			return
		}

		stacks = append(stacks, stack{
			edges: []*node{matched},
		})

		for len(stacks) > 0 {
			n := len(stacks)
			last := stacks[n-1]
			elem := last.edges[0]

			if len(last.edges) > 1 {
				stacks[n-1].edges = last.edges[1:]
			} else {
				stacks = stacks[:n-1]
			}

			if len(elem.statics) > 0 {
				stacks = append(stacks, stack{edges: elem.statics})
			}
			if len(elem.params) > 0 {
				stacks = append(stacks, stack{edges: elem.params})
			}
			if len(elem.wildcards) > 0 {
				stacks = append(stacks, stack{edges: elem.wildcards})
			}

			if elem.isLeaf() {
				for _, route := range elem.routes {
					if len(route.params) > 0 && !strings.HasPrefix(route.Pattern(), prefix) {
						continue
					}

					if !yield(route) {
						return
					}
				}
			}
		}
	}
}

// All returns a range iterator over all routes registered in the routing tree. The iterator reflect a snapshot
// of the routing tree at the time [Iter] is created. This function is safe for concurrent use by multiple goroutine
// and while mutation on routes are ongoing. See also [Iter.PatternPrefix] as an alternative.
func (it Iter) All() iter.Seq[*Route] {
	return func(yield func(*Route) bool) {
		for route := range it.PatternPrefix("") {
			if !yield(route) {
				return
			}
		}
	}
}

// Names returns a range iterator over all routes registered in the routing tree with a name. The iterator reflect a snapshot
// of the routing tree at the time [Iter] is created. This function is safe for concurrent use by multiple goroutine
// and while mutation on routes are ongoing. See also [Iter.NamePrefix] as an alternative.
func (it Iter) Names() iter.Seq[*Route] {
	return func(yield func(*Route) bool) {
		for route := range it.NamePrefix("") {
			if !yield(route) {
				return
			}
		}
	}
}
