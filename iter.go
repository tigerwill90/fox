// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
	"iter"
)

func newRawIterator(n *node) *rawIterator {
	return &rawIterator{
		stack: []stack{{edges: []*node{n}}},
	}
}

type rawIterator struct {
	current *node
	path    string
	stack   []stack
}

const stackSizeThreshold = 25

type stack struct {
	edges []*node
}

func (it *rawIterator) hasNext() bool {
	for len(it.stack) > 0 {
		n := len(it.stack)
		last := it.stack[n-1]
		elem := last.edges[0]

		if len(last.edges) > 1 {
			it.stack[n-1].edges = last.edges[1:]
		} else {
			it.stack = it.stack[:n-1]
		}

		if len(elem.children) > 0 {
			it.stack = append(it.stack, stack{edges: elem.children})
		}

		it.current = elem

		if it.current.isLeaf() {
			it.path = elem.route.Pattern()
			return true
		}
	}

	it.current = nil
	it.path = ""
	return false
}

// Iter provide a set of range iterators for traversing registered methods and routes. Iter capture a point-in-time
// snapshot of the routing tree. Therefore, all iterators returned by Iter will not observe subsequent write on the
// router or on the transaction from which the Iter is created.
type Iter struct {
	tree     *iTree
	root     roots
	maxDepth uint32
}

// Methods returns a range iterator over all HTTP methods registered in the routing tree. The iterator reflect a snapshot
// of the routing tree at the time [Iter] is created. This function is safe for concurrent use by multiple goroutine and
// while mutation on routes are ongoing.
func (it Iter) Methods() iter.Seq[string] {
	return func(yield func(string) bool) {
		for i := range it.root {
			if len(it.root[i].children) > 0 {
				if !yield(it.root[i].key) {
					return
				}
			}
		}
	}
}

// Routes returns a range iterator over all registered routes in the routing tree that exactly match the provided route
// pattern for the given HTTP methods. The iterator reflect a snapshot of the routing tree at the time [Iter] is created.
// This function is safe for concurrent use by multiple goroutine and while mutation on routes are ongoing.
func (it Iter) Routes(methods iter.Seq[string], pattern string) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		c := it.tree.ctx.Get().(*cTx)
		defer c.Close()
		host, path := SplitHostPath(pattern)
		for method := range methods {
			c.resetNil()
			n, tsr := it.root.lookup(it.tree, method, host, path, c, true)
			if n != nil && !tsr && n.route.pattern == pattern {
				if !yield(method, n.route) {
					return
				}
			}
		}
	}
}

// Reverse returns a range iterator over all routes registered in the routing tree that match the given host and path
// for the provided HTTP methods. Unlike [Iter.Routes], which matches an exact route, Reverse is used to match an url
// (e.g., a path from an incoming request) to a registered routes in the tree. The iterator reflect a snapshot of the
// routing tree at the time [Iter] is created.
//
// If [WithHandleTrailingSlash] option is enabled on a route with the [RelaxedSlash] or [RedirectSlash] flag, Reverse will
// match it regardless of whether a trailing slash is present. If the path is empty, a default slash is automatically added.
//
// This function is safe for concurrent use by multiple goroutine and while mutation on routes are ongoing.
func (it Iter) Reverse(methods iter.Seq[string], host, path string) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		c := it.tree.ctx.Get().(*cTx)
		defer c.Close()
		for method := range methods {
			c.resetNil()
			n, tsr := it.root.lookup(it.tree, method, host, cmp.Or(path, "/"), c, true)
			if n != nil && (!tsr || n.route.handleSlash != StrictSlash) {
				if !yield(method, n.route) {
					return
				}
			}
		}
	}
}

// Prefix returns a range iterator over all routes in the routing tree that match a given prefix and HTTP methods.
// The iterator reflect a snapshot of the routing tree at the time [Iter] is created. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing.
func (it Iter) Prefix(methods iter.Seq[string], prefix string) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		var stacks []stack
		if it.maxDepth < stackSizeThreshold {
			stacks = make([]stack, 0, stackSizeThreshold) // stack allocation
		} else {
			stacks = make([]stack, 0, it.maxDepth) // heap allocation
		}

		for method := range methods {
			index := it.root.methodIndex(method)
			if index < 0 || len(it.root[index].children) == 0 {
				continue
			}

			matched := it.root.search(it.root[index], prefix)
			if matched == nil {
				continue
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

				if len(elem.children) > 0 {
					stacks = append(stacks, stack{edges: elem.children})
				}

				if elem.isLeaf() {
					if !yield(method, elem.route) {
						return
					}
				}
			}
		}
	}
}

// All returns a range iterator over all routes registered in the routing tree. The iterator reflect a snapshot
// of the routing tree at the time [Iter] is created. This function is safe for concurrent use by multiple goroutine
// and while mutation on routes are ongoing.
func (it Iter) All() iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		for method, route := range it.Prefix(it.Methods(), "") {
			if !yield(method, route) {
				return
			}
		}
	}
}
