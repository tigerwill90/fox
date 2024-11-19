// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
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

const stackSizeThreshold = 15

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
			// TODO probably elem.children is now OKAY for read only
			it.stack = append(it.stack, stack{edges: elem.getEdges()})
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

type Iter struct {
	tree     *iTree
	root     root
	maxDepth uint32
}

// Methods returns a range iterator over all HTTP methods registered in the routing tree.
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
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
// pattern for the given HTTP methods.
//
// This method performs a lookup for each method and the exact route associated with the provided pattern. It yields
// a tuple containing the HTTP method and the corresponding route if the route is registered for that method and pattern.
//
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func (it Iter) Routes(methods iter.Seq[string], pattern string) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		c := it.tree.ctx.Get().(*cTx)
		defer c.Close()
		for method := range methods {
			c.resetNil()
			host, path := SplitHostPath(pattern)
			n, tsr := it.root.lookup(it.tree, method, host, path, c, true)
			if n != nil && !tsr && n.route.pattern == path {
				if !yield(method, n.route) {
					return
				}
			}
		}
	}
}

// Reverse returns a range iterator over all routes registered in the routing tree that match the given host and path
// for the provided HTTP methods. It performs a reverse lookup for each method and path combination,
// yielding a tuple containing the HTTP method and the corresponding route.
// Unlike Routes, which matches an exact route, Reverse is used to match an url (e.g., a path from an incoming
// request) to a registered routes in the tree.
//
// When WithIgnoreTrailingSlash or WithRedirectTrailingSlash is enabled, Reverse will match routes regardless
// of whether a trailing slash is present.
//
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and may change in future releases.
func (it Iter) Reverse(methods iter.Seq[string], host, path string) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		c := it.tree.ctx.Get().(*cTx)
		defer c.Close()
		for method := range methods {
			c.resetNil()
			n, tsr := it.root.lookup(it.tree, method, host, path, c, true)
			if n != nil && (!tsr || n.route.redirectTrailingSlash || n.route.ignoreTrailingSlash) {
				if !yield(method, n.route) {
					return
				}
			}
		}
	}
}

// Prefix returns a range iterator over all routes in the routing tree that match a given prefix and HTTP methods.
//
// This iterator traverses the routing tree for each method provided, starting from nodes that match the given prefix.
// For each method, it yields a tuple containing the HTTP method and the corresponding route found under that prefix.
// If no routes match the prefix, the method will not yield any results.
//
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and may change in future releases.
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

			result := it.root.search(it.root[index], prefix, false, it.maxDepth)
			if !result.isExactMatch() && !result.isKeyMidEdge() {
				continue
			}

			stacks = append(stacks, stack{
				edges: []*node{result.matched},
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
					stacks = append(stacks, stack{edges: elem.getEdges()})
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

// All returns a range iterator over all routes registered in the routing tree for all HTTP methods.
// The result is an iterator that yields a tuple containing the HTTP method and the corresponding route.
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and may change in future releases.
func (it Iter) All() iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		for method, route := range it.Prefix(it.Methods(), "") {
			if !yield(method, route) {
				return
			}
		}
	}
}
