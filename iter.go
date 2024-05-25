// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"sort"
)

type Iterator[T Context] struct {
	tree    *Tree[T]
	method  string
	current *node[T]
	stacks  []stack[T]
	valid   bool
	started bool
}

// NewIterator returns an Iterator that traverses all registered routes in lexicographic order.
// An Iterator is safe to use when the router is serving request, when routing updates are ongoing or
// in parallel with other Iterators. Note that changes that happen while iterating over routes may not be reflected
// by the Iterator. This api is EXPERIMENTAL and is likely to change in future release.
func NewIterator[T Context](t *Tree[T]) *Iterator[T] {
	return &Iterator[T]{
		tree: t,
	}
}

func (it *Iterator[T]) methods() map[string]*node[T] {
	nds := *it.tree.nodes.Load()
	m := make(map[string]*node[T], len(nds))
	for i := range nds {
		if len(nds[i].children) > 0 {
			m[nds[i].key] = nds[i]
		}
	}
	return m
}

// SeekPrefix reset the iterator cursor to the first route starting with key.
// It does not keep tracking of previous seek.
func (it *Iterator[T]) SeekPrefix(key string) {
	nds := it.methods()
	keys := make([]string, 0, len(nds))
	for method, n := range nds {
		result := it.tree.search(n, key)
		if result.isExactMatch() || result.isKeyMidEdge() {
			nds[method] = result.matched
			keys = append(keys, method)
		}
	}

	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	stacks := make([]stack[T], 0, len(keys))
	for _, key := range keys {
		stacks = append(stacks, stack[T]{
			edges:  []*node[T]{nds[key]},
			method: key,
		})
	}

	it.stacks = stacks
}

// SeekMethod reset the iterator cursor to the first route for the given method.
// It does not keep tracking of previous seek.
func (it *Iterator[T]) SeekMethod(method string) {
	nds := it.methods()
	stacks := make([]stack[T], 0, 1)
	n, ok := nds[method]
	if ok {
		stacks = append(stacks, stack[T]{
			edges:  []*node[T]{n},
			method: method,
		})
	}

	it.stacks = stacks
}

// SeekMethodPrefix reset the iterator cursor to the first route starting with key for the given method.
// It does not keep tracking of previous seek.
func (it *Iterator[T]) SeekMethodPrefix(method, key string) {
	nds := it.methods()
	stacks := make([]stack[T], 0, 1)
	n, ok := nds[method]
	if ok {
		result := it.tree.search(n, key)
		if result.isExactMatch() || result.isKeyMidEdge() {
			stacks = append(stacks, stack[T]{
				edges:  []*node[T]{result.matched},
				method: method,
			})
		}
	}

	it.stacks = stacks
}

// Rewind reset the iterator cursor all the way to zero-th position which is the first method and route.
// It does not keep track of whether the cursor started with SeekPrefix, SeekMethod or SeekMethodPrefix.
func (it *Iterator[T]) Rewind() {
	nds := it.methods()
	methods := make([]string, 0, len(nds))
	for method := range nds {
		methods = append(methods, method)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(methods)))

	stacks := make([]stack[T], 0, len(methods))
	for _, method := range methods {
		stacks = append(stacks, stack[T]{
			edges:  []*node[T]{nds[method]},
			method: method,
		})
	}

	it.stacks = stacks
}

// Valid returns false when iteration is done.
func (it *Iterator[T]) Valid() bool {
	if !it.started {
		it.started = true
		it.Next()
		return it.valid
	}
	return it.valid
}

// Next advance the iterator to the next route. Always check it.Valid() after a it.Next().
func (it *Iterator[T]) Next() {
	for len(it.stacks) > 0 {
		n := len(it.stacks)
		last := it.stacks[n-1]
		elem := last.edges[0]

		if len(last.edges) > 1 {
			it.stacks[n-1].edges = last.edges[1:]
		} else {
			it.stacks = it.stacks[:n-1]
		}

		if len(elem.children) > 0 {
			it.stacks = append(it.stacks, stack[T]{edges: elem.getEdgesShallowCopy()})
		}

		it.current = elem
		if last.method != "" {
			it.method = last.method
		}

		if it.current.isLeaf() {
			it.valid = true
			return
		}
	}

	it.current = nil
	it.method = ""
	it.valid = false
	it.started = false
}

// Path returns the registered path for the current route.
func (it *Iterator[T]) Path() string {
	if it.current != nil {
		return it.current.path
	}
	return ""
}

// Method returns the http method for the current route.
func (it *Iterator[T]) Method() string {
	return it.method
}

// Handler return the registered handler for the current route.
func (it *Iterator[T]) Handler() HandlerFunc[T] {
	if it.current != nil {
		return it.current.handler
	}
	return nil
}

type stack[T Context] struct {
	method string
	edges  []*node[T]
}

func newRawIterator[T Context](n *node[T]) *rawIterator[T] {
	return &rawIterator[T]{
		stack: []stack[T]{{edges: []*node[T]{n}}},
	}
}

type rawIterator[T Context] struct {
	current *node[T]
	path    string
	stack   []stack[T]
}

func (it *rawIterator[T]) hasNext() bool {
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
			it.stack = append(it.stack, stack[T]{edges: elem.getEdgesShallowCopy()})
		}

		it.current = elem

		if it.current.isLeaf() {
			it.path = elem.path
			return true
		}
	}

	it.current = nil
	it.path = ""
	return false
}
