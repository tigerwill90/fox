package fox

import (
	"sort"
)

type Iterator struct {
	r       *Router
	method  string
	current *node
	stacks  []stack
	valid   bool
	started bool
}

// NewIterator returns an Iterator that traverses all registered routes in lexicographic order.
// An Iterator is safe to use when the router is serving request, when routing updates are ongoing or
// concurrently with other Iterator. Note that changes that happen while iterating over routes may not be reflected
// by the Iterator. This api is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) NewIterator() *Iterator {
	return &Iterator{
		r: fox,
	}
}

func (it *Iterator) methods() map[string]*node {
	nds := *it.r.trees.Load()
	m := make(map[string]*node, len(nds))
	for i := range nds {
		if len(nds[i].children) > 0 {
			m[nds[i].key] = nds[i]
		}
	}
	return m
}

// SeekPrefix reset the iterator cursor to the first route starting with key.
// It does not keep tracking of previous seek.
func (it *Iterator) SeekPrefix(key string) {
	nds := it.methods()
	keys := make([]string, 0, len(nds))
	for method, n := range nds {
		result := it.r.search(n, key)
		if result.isExactMatch() || result.isKeyMidEdge() {
			nds[method] = result.matched
			keys = append(keys, method)
		}
	}

	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	stacks := make([]stack, 0, len(keys))
	for _, key := range keys {
		stacks = append(stacks, stack{
			edges:  []*node{nds[key]},
			method: key,
		})
	}

	it.stacks = stacks
}

// SeekMethod reset the iterator cursor to the first route for the given method.
// It does not keep tracking of previous seek.
func (it *Iterator) SeekMethod(method string) {
	nds := it.methods()
	stacks := make([]stack, 0, 1)
	n, ok := nds[method]
	if ok {
		stacks = append(stacks, stack{
			edges:  []*node{n},
			method: method,
		})
	}

	it.stacks = stacks
}

// SeekMethodPrefix reset the iterator cursor to the first route starting with key for the given method.
// It does not keep tracking of previous seek.
func (it *Iterator) SeekMethodPrefix(method, key string) {
	nds := it.methods()
	stacks := make([]stack, 0, 1)
	n, ok := nds[method]
	if ok {
		result := it.r.search(n, key)
		if result.isExactMatch() || result.isKeyMidEdge() {
			stacks = append(stacks, stack{
				edges:  []*node{result.matched},
				method: method,
			})
		}
	}

	it.stacks = stacks
}

// Rewind reset the iterator cursor all the way to zero-th position which is the first method and route.
// It does not keep track of whether the cursor started with SeekPrefix, SeekMethod or SeekMethodPrefix.
func (it *Iterator) Rewind() {
	nds := it.methods()
	methods := make([]string, 0, len(nds))
	for method := range nds {
		methods = append(methods, method)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(methods)))

	stacks := make([]stack, 0, len(methods))
	for _, method := range methods {
		stacks = append(stacks, stack{
			edges:  []*node{nds[method]},
			method: method,
		})
	}

	it.stacks = stacks
}

// Valid returns false when iteration is done.
func (it *Iterator) Valid() bool {
	if !it.started {
		it.started = true
		it.Next()
		return it.valid
	}
	return it.valid
}

// Next advance the iterator to the next route. Always check it.Valid() after a it.Next().
func (it *Iterator) Next() {
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
			it.stacks = append(it.stacks, stack{edges: elem.getEdgesShallowCopy()})
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
func (it *Iterator) Path() string {
	if it.current != nil {
		return it.current.path
	}
	return ""
}

// Method returns the http method for the current route.
func (it *Iterator) Method() string {
	return it.method
}

// Handler return the registered handler for the current route.
func (it *Iterator) Handler() Handler {
	if it.current != nil {
		return it.current.handler
	}
	return nil
}

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

type stack struct {
	edges  []*node
	method string
}

func (it *rawIterator) fullPath() string {
	return it.path
}

func (it *rawIterator) node() *node {
	return it.current
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
			it.path = elem.path
			return true
		}
	}

	it.current = nil
	it.path = ""
	return false
}
