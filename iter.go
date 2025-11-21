// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"iter"
	"net/http"
	"slices"
	"strings"
)

const stackSizeThreshold = 25

type stack struct {
	edges []*node
}

// Iter provide a set of range iterators for traversing registered methods and routes. Iter capture a point-in-time
// snapshot of the routing tree. Therefore, all iterators returned by Iter will not observe subsequent write on the
// router or on the transaction from which the Iter is created.
type Iter struct {
	tree     *iTree
	patterns root
	names    root
	maxDepth int // tree or txn maxDepth
}

// Methods returns a range iterator over all HTTP methods registered in the routing tree. The iterator reflect a snapshot
// of the routing tree at the time [Iter] is created. This function is safe for concurrent use by multiple goroutine and
// while mutation on routes are ongoing.
func (it Iter) Methods() iter.Seq[string] {
	return func(yield func(string) bool) {
		for k := range it.patterns {
			if !yield(k) {
				return
			}
		}
	}
}

// Routes returns a range iterator over all registered routes in the routing tree that exactly match the provided route
// pattern for the given HTTP methods. The iterator reflect a snapshot of the routing tree at the time [Iter] is created.
// This function is safe for concurrent use by multiple goroutine and while mutation on routes are ongoing.
func (it Iter) Routes(methods iter.Seq[string], pattern string) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {

		for method := range methods {
			root := it.patterns[method]
			if root == nil {
				continue
			}

			matched := root.searchPattern(pattern)
			if matched == nil || !matched.isLeaf() {
				continue
			}

			for _, route := range matched.routes {
				if route.pattern == pattern {
					if !yield(method, route) {
						return
					}
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
// match it regardless of whether a trailing slash is present.
//
// This function is safe for concurrent use by multiple goroutine and while mutation on routes are ongoing.
func (it Iter) Reverse(methods iter.Seq[string], r *http.Request) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		c := it.tree.pool.Get().(*cTx)
		defer c.Close()
		for method := range methods {
			c.resetWithRequest(r)

			path := r.URL.Path
			if len(r.URL.RawPath) > 0 {
				// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
				path = r.URL.RawPath
			}

			idx, n := it.tree.lookup(method, r.Host, path, c, true)
			if n != nil && (!c.tsr || n.routes[idx].handleSlash != StrictSlash) {
				if !yield(method, n.routes[idx]) {
					return
				}
			}
		}
	}
}

// NamePrefix returns a range iterator over all routes in the routing tree that match a given name prefix and HTTP methods.
// The iterator reflect a snapshot of the routing tree at the time [Iter] is created. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing.
func (it Iter) NamePrefix(methods iter.Seq[string], prefix string) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		var stacks []stack
		if it.maxDepth < stackSizeThreshold {
			stacks = make([]stack, 0, stackSizeThreshold) // stack allocation
		} else {
			stacks = make([]stack, 0, it.maxDepth) // heap allocation TODO this inaccruate now (this is currently the max skipStack)
		}

		for method := range methods {
			root := it.names[method]
			if root == nil {
				continue
			}

			matched := root.searchName(prefix)
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

				if len(elem.statics) > 0 {
					stacks = append(stacks, stack{edges: elem.statics})
				}

				if elem.isLeaf() {
					if !yield(method, elem.routes[0]) {
						return
					}
				}
			}
		}
	}
}

// PatternPrefix returns a range iterator over all routes in the routing tree that match a given pattern prefix and HTTP methods.
// The iterator reflect a snapshot of the routing tree at the time [Iter] is created. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing.
// Note: Partial parameter syntax (e.g., /users/{name:) is not supported and will not match any routes.
func (it Iter) PatternPrefix(methods iter.Seq[string], prefix string) iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		var stacks []stack
		if it.maxDepth < stackSizeThreshold {
			stacks = make([]stack, 0, stackSizeThreshold) // stack allocation
		} else {
			stacks = make([]stack, 0, it.maxDepth) // heap allocation TODO this inaccruate now (this is currently the max skipStack)
		}

		for method := range methods {
			root := it.patterns[method]
			if root == nil {
				continue
			}

			matched := root.searchPattern(prefix)
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
						if len(route.params) > 0 && !strings.HasPrefix(route.pattern, prefix) {
							continue
						}

						if !yield(method, route) {
							return
						}
					}
				}
			}
		}
	}
}

// All returns a range iterator over all routes registered in the routing tree. The iterator reflect a snapshot
// of the routing tree at the time [Iter] is created. This function is safe for concurrent use by multiple goroutine
// and while mutation on routes are ongoing. See also [Iter.PatternPrefix] as an alternative.
func (it Iter) All() iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		methods := make([]string, 0, len(it.patterns))
		for k := range it.patterns {
			methods = append(methods, k)
		}
		slices.Sort(methods)
		for method, route := range it.PatternPrefix(slices.Values(methods), "") {
			if !yield(method, route) {
				return
			}
		}
	}
}

// Names returns a range iterator over all routes registered in the routing tree with a name. The iterator reflect a snapshot
// of the routing tree at the time [Iter] is created. This function is safe for concurrent use by multiple goroutine
// and while mutation on routes are ongoing. See also [Iter.NamePrefix] as an alternative.
func (it Iter) Names() iter.Seq2[string, *Route] {
	return func(yield func(string, *Route) bool) {
		methods := make([]string, 0, len(it.names))
		for k := range it.names {
			methods = append(methods, k)
		}
		slices.Sort(methods)
		for method, route := range it.NamePrefix(slices.Values(methods), "") {
			if !yield(method, route) {
				return
			}
		}
	}
}
