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
			it.stack = append(it.stack, stack{edges: elem.getEdgesShallowCopy()})
		}

		it.current = elem

		if it.current.isLeaf() {
			it.path = elem.route.Path()
			return true
		}
	}

	it.current = nil
	it.path = ""
	return false
}

type Iter struct {
	t *Tree
}

// Methods returns a range iterator over all HTTP methods registered in the routing tree.
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func (it Iter) Methods() iter.Seq[string] {
	return func(yield func(string) bool) {
		nds := *it.t.nodes.Load()
		for i := range nds {
			if len(nds[i].children) > 0 {
				if !yield(nds[i].key) {
					return
				}
			}
		}
	}
}

// Routes returns a range iterator over all registered routes in the routing tree that exactly match the provided path
// for the given HTTP methods.
//
// This method performs a lookup for each method and the exact route associated with the provided `path`. It yields
// a tuple containing the HTTP method and the corresponding route if the route is registered for that method and path.
//
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func (it Iter) Routes(methods iter.Seq[string], path string) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		nds := *it.t.nodes.Load()
		c := it.t.ctx.Get().(*cTx)
		defer c.Close()
		for method := range methods {
			c.resetNil()
			index := findRootNode(method, nds)
			if index < 0 || len(nds[index].children) == 0 {
				continue
			}

			n, tsr := it.t.lookupByPath(nds[index].children[0].Load(), path, c, true)
			if n != nil && !tsr && n.route.path == path {
				if !yield(method, n.route) {
					return
				}
			}
		}
	}
}

// Reverse returns a range iterator over all routes registered in the routing tree that match the given path
// for the provided HTTP methods. It performs a reverse lookup for each method and path combination,
// yielding a tuple containing the HTTP method and the corresponding route.
// Unlike Routes, which matches an exact route path, Reverse is used to match a path (e.g., a path from an incoming
// request) to registered routes in the tree.
//
// When WithIgnoreTrailingSlash or WithRedirectTrailingSlash is enabled, Reverse will match routes regardless
// of whether a trailing slash is present.
//
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and may change in future releases.
func (it Iter) Reverse(methods iter.Seq[string], path string) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		nds := *it.t.nodes.Load()
		c := it.t.ctx.Get().(*cTx)
		defer c.Close()
		for method := range methods {
			c.resetNil()
			index := findRootNode(method, nds)
			if index < 0 || len(nds[index].children) == 0 {
				continue
			}

			n, tsr := it.t.lookupByPath(nds[index].children[0].Load(), path, c, true)
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
		nds := *it.t.nodes.Load()
		maxDepth := it.t.maxDepth.Load()
		var stacks []stack
		if maxDepth < stackSizeThreshold {
			stacks = make([]stack, 0, stackSizeThreshold) // stack allocation
		} else {
			stacks = make([]stack, 0, maxDepth) // heap allocation
		}

		for method := range methods {
			index := findRootNode(method, nds)
			if index < 0 || len(nds[index].children) == 0 {
				continue
			}

			result := it.t.search(nds[index], prefix)
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
					stacks = append(stacks, stack{edges: elem.getEdgesShallowCopy()})
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
		for method, route := range it.Prefix(it.Methods(), "/") {
			if !yield(method, route) {
				return
			}
		}
	}
}
