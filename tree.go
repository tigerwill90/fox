package fox

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"

	"github.com/tigerwill90/fox/internal/simplelru"
)

const defaultModifiedCache = 4096

type iTree struct {
	pool      sync.Pool
	fox       *Router
	root      root
	size      int
	maxParams int
	maxDepth  int
}

func (t *iTree) txn() *tXn {
	return &tXn{
		tree:      t,
		root:      t.root,
		size:      t.size,
		maxParams: t.maxParams,
		maxDepth:  t.maxDepth,
	}
}

func (t *iTree) lookup(method, hostPort, path string, c *cTx, lazy bool) *node {
	return t.root.lookup(method, hostPort, path, c, lazy)
}

func (t *iTree) allocateContext() *cTx {
	params := make([]string, 0, t.maxParams)
	tsrParams := make([]string, 0, t.maxParams)
	stacks := make(skipStack, 0, t.maxDepth)
	return &cTx{
		params:    &params,
		tsrParams: &tsrParams,
		skipStack: &stacks,
		// This is a read only value, no reset. It's always the
		// owner of the pool.
		tree: t,
		// This is a read only value, no reset
		fox: t.fox,
	}
}

type tXn struct {
	tree      *iTree
	writable  *simplelru.LRU[*node, struct{}]
	root      root
	method    string
	size      int
	maxParams int
	maxDepth  int
	forked    bool
	mode      insertMode
}

func (t *tXn) commit() *iTree {
	tc := &iTree{
		root:      t.root,
		fox:       t.tree.fox,
		size:      t.size,
		maxParams: t.maxParams,
		maxDepth:  t.maxDepth,
	}
	tc.pool = sync.Pool{
		New: func() any {
			return tc.allocateContext()
		},
	}
	t.writable = nil
	t.forked = false
	return tc
}

// clone capture a point-in-time clone of the transaction. The cloned transaction will contain
// any uncommited writes in the original transaction but further mutations to either will be independent and result
// in different tree on commit.
func (t *tXn) clone() *tXn {
	t.writable = nil
	t.forked = false
	tx := &tXn{
		tree:      t.tree,
		root:      t.root,
		size:      t.size,
		maxParams: t.maxParams,
		maxDepth:  t.maxDepth,
	}
	return tx
}

// snapshot capture a point-in-time snapshot of the roots tree. Further mutation to txn
// will not be reflected on the snapshot.
func (t *tXn) snapshot() root {
	t.writable = nil
	t.forked = false
	return t.root
}

// insert performs a recursive copy-on-write insertion.
func (t *tXn) insert(method string, route *Route, mode insertMode) error {
	root := t.root[method]
	if root == nil {
		if t.mode == modeUpdate {
			return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, route.pattern)
		}
		root = &node{
			key: method,
		}
	}

	t.mode = mode
	t.method = method

	newRoot, err := t.insertTokens(root, route.tokens, route)
	if err != nil {
		return err
	}
	if newRoot != nil {
		if !t.forked {
			t.root = maps.Clone(t.root)
			t.forked = true
		}
		t.root[method] = newRoot
		t.maxDepth = max(t.maxDepth, t.computePathDepth(newRoot, route.tokens))
		t.maxParams = max(t.maxParams, len(route.params))
		t.size++
	}
	return nil
}

func (t *tXn) insertTokens(n *node, tokens []token, route *Route) (*node, error) {

	// Base case: no tokens left, attach route
	if len(tokens) == 0 {
		if t.mode == modeInsert && n.isLeaf() {
			return nil, &RouteConflictError{Method: t.method, New: route, Existing: n.route}
		}
		if t.mode == modeUpdate && (!n.isLeaf() || n.route.pattern != route.pattern) {
			return nil, fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, t.method, route.pattern)
		}

		nc := t.writeNode(n)
		nc.route = route
		return nc, nil
	}

	tk := tokens[0]
	remaining := tokens[1:]

	switch tk.typ {
	case nodeStatic:
		return t.insertStatic(n, tk, remaining, route)
	case nodeParam:
		return t.insertParam(n, tk, remaining, route)
	case nodeWildcard:
		return t.insertWildcard(n, tk, remaining, route)
	default:
		panic("internal error: unknown token type")
	}
}

func (t *tXn) insertStatic(n *node, tk token, remaining []token, route *Route) (*node, error) {
	search := tk.value

	if len(search) == 0 {
		return t.insertTokens(n, remaining, route)
	}

	idx, child := n.getStaticEdge(search[0])
	if child == nil {
		if t.mode == modeUpdate {
			return nil, fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, t.method, route.pattern)
		}

		newChild, err := t.insertTokens(
			&node{
				label: search[0],
				key:   search,
				host:  tk.hsplit,
			},
			remaining,
			route,
		)
		if err != nil {
			return nil, err
		}
		nc := t.writeNode(n)
		nc.addStaticEdge(newChild)
		return nc, nil
	}

	commonPrefix := longestPrefix(search, child.key)
	if commonPrefix == len(child.key) {
		search = search[commonPrefix:]
		remaining = append([]token{{typ: nodeStatic, value: search, hsplit: tk.hsplit}}, remaining...)
		// e.g. child /foo and want insert /fooo, insert "o"
		newChild, err := t.insertTokens(child, remaining, route)
		if err != nil {
			return nil, err
		}
		nc := t.writeNode(n)
		nc.statics[idx] = newChild
		return nc, nil
	}

	if t.mode == modeUpdate {
		return nil, fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, t.method, route.pattern)
	}

	nc := t.writeNode(n)
	splitNode := &node{
		label: search[0],
		key:   search[:commonPrefix],
		host:  tk.hsplit,
	}
	nc.replaceStaticEdge(splitNode)

	modChild := t.writeNode(child)
	modChild.label = modChild.key[commonPrefix]
	modChild.key = modChild.key[commonPrefix:]
	splitNode.addStaticEdge(modChild)

	search = search[commonPrefix:]
	if len(search) == 0 {
		// e.g. we have /foo and want to insert /fo,
		// we first split /foo into /fo, o and then fo <- get the new route
		// splitNode.route = route // SHOULD not need this
		if len(remaining) > 0 {
			newSplitNode, err := t.insertTokens(splitNode, remaining, route)
			if err != nil {
				return nil, err
			}
			nc.replaceStaticEdge(newSplitNode)
			return nc, nil
		}
		splitNode.route = route
		return nc, nil
	}
	// e.g. we have /foo and want to insert /fob
	// we first have our splitNode /fo, with old child (modChild) equal o, and insert the edge b

	newChild, err := t.insertTokens(
		&node{
			label: search[0],
			key:   search,
			host:  tk.hsplit,
		},
		remaining,
		route,
	)
	if err != nil {
		return nil, err
	}
	splitNode.addStaticEdge(newChild)
	return nc, nil
}

func (t *tXn) insertParam(n *node, tk token, remaining []token, route *Route) (*node, error) {
	key := canonicalKey(tk)
	idx, child := n.getParamEdge(key)
	if child == nil {
		if t.mode == modeUpdate {
			return nil, fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, t.method, route.pattern)
		}

		newChild, err := t.insertTokens(
			&node{
				key:    key,
				regexp: tk.regexp,
			},
			remaining,
			route,
		)
		if err != nil {
			return nil, err
		}

		nc := t.writeNode(n)
		nc.addParamEdge(newChild)
		return nc, nil
	}

	newChild, err := t.insertTokens(child, remaining, route)
	if err != nil {
		return nil, err
	}

	nc := t.writeNode(n)
	nc.params[idx] = newChild
	return nc, nil
}

func (t *tXn) insertWildcard(n *node, tk token, remaining []token, route *Route) (*node, error) {
	key := canonicalKey(tk)
	idx, child := n.getWildcardEdge(key)
	if child == nil {
		if t.mode == modeUpdate {
			return nil, fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, t.method, route.pattern)
		}

		newChild, err := t.insertTokens(
			&node{
				key:    key,
				regexp: tk.regexp,
			},
			remaining,
			route,
		)
		if err != nil {
			return nil, err
		}
		nc := t.writeNode(n)
		nc.addWildcardEdge(newChild)
		return nc, nil
	}

	newChild, err := t.insertTokens(child, remaining, route)
	if err != nil {
		return nil, err
	}
	nc := t.writeNode(n)
	nc.wildcards[idx] = newChild
	return nc, nil
}

// delete performs a recursive copy-on-write deletion.
func (t *tXn) delete(method string, tokens []token, pattern string) (*Route, bool) {
	root := t.root[method]
	if root == nil {
		return nil, false
	}

	newRoot, route := t.deleteTokens(root, root, tokens, pattern)
	if newRoot != nil {
		if !t.forked {
			t.root = maps.Clone(t.root)
			t.forked = true
		}
		t.root[method] = newRoot
		if len(newRoot.wildcards) == 0 && len(newRoot.params) == 0 && len(newRoot.statics) == 0 {
			delete(t.root, method)
		}
	}

	if route != nil {
		t.size--
		return route, true
	}

	return nil, false
}

func (t *tXn) deleteTokens(root, n *node, tokens []token, pattern string) (*node, *Route) {
	if len(tokens) == 0 {
		if !n.isLeaf() {
			return nil, nil
		}

		if n.route.pattern != pattern {
			return nil, nil
		}

		oldRoute := n.route
		nc := t.writeNode(n)
		nc.route = nil

		if n != root &&
			len(nc.statics) == 1 &&
			len(nc.params) == 0 &&
			len(nc.wildcards) == 0 {
			t.mergeChild(nc)
		}

		return nc, oldRoute
	}

	tk := tokens[0]
	remaining := tokens[1:]

	switch tk.typ {
	case nodeStatic:
		return t.deleteStatic(root, n, tk.value, remaining, pattern)
	case nodeParam:
		return t.deleteParam(root, n, canonicalKey(tk), remaining, pattern)
	case nodeWildcard:
		return t.deleteWildcard(root, n, canonicalKey(tk), remaining, pattern)
	default:
		panic("internal error: unknown token type")
	}
}

func (t *tXn) deleteStatic(root, n *node, search string, remaining []token, pattern string) (*node, *Route) {
	if len(search) == 0 {
		return t.deleteTokens(root, n, remaining, pattern)
	}

	label := search[0]
	idx, child := n.getStaticEdge(label)
	if child == nil || !strings.HasPrefix(search, child.key) {
		return nil, nil
	}

	// Consume the matched portion
	search = search[len(child.key):]

	// Prepend remaining static portion if any
	if len(search) > 0 {
		remaining = append([]token{{typ: nodeStatic, value: search}}, remaining...)
	}

	newChild, deletedRoute := t.deleteTokens(root, child, remaining, pattern)
	if deletedRoute == nil {
		return nil, nil
	}

	nc := t.writeNode(n)

	if newChild.route == nil &&
		len(newChild.statics) == 0 &&
		len(newChild.params) == 0 &&
		len(newChild.wildcards) == 0 {
		nc.delStaticEdge(label)

		if n != root &&
			!nc.isLeaf() &&
			len(nc.statics) == 1 &&
			len(nc.params) == 0 &&
			len(nc.wildcards) == 0 {
			t.mergeChild(nc)
		}
	} else {
		nc.statics[idx] = newChild
	}

	return nc, deletedRoute
}

func (t *tXn) deleteParam(root, n *node, key string, remaining []token, pattern string) (*node, *Route) {
	idx, child := n.getParamEdge(key)
	if child == nil {
		return nil, nil
	}

	// Recurse into param's children
	newChild, deletedRoute := t.deleteTokens(root, child, remaining, pattern)
	if deletedRoute == nil {
		return nil, nil
	}

	nc := t.writeNode(n)

	// If param node is now empty, remove it
	if newChild.route == nil &&
		len(newChild.statics) == 0 &&
		len(newChild.params) == 0 &&
		len(newChild.wildcards) == 0 {
		nc.delParamEdge(key)

		if n != root &&
			len(nc.statics) == 1 &&
			!nc.isLeaf() &&
			len(nc.params) == 0 &&
			len(nc.wildcards) == 0 {
			t.mergeChild(nc)
		}

	} else {
		nc.params[idx] = newChild
	}

	return nc, deletedRoute
}

func (t *tXn) deleteWildcard(root, n *node, key string, remaining []token, pattern string) (*node, *Route) {
	idx, child := n.getWildcardEdge(key)
	if child == nil {
		return nil, nil
	}

	// Recurse into wildcard's children
	newChild, deletedRoute := t.deleteTokens(root, child, remaining, pattern)
	if deletedRoute == nil {
		return nil, nil
	}

	nc := t.writeNode(n)

	// If wildcard node is now empty, remove it
	if newChild.route == nil &&
		len(newChild.statics) == 0 &&
		len(newChild.params) == 0 &&
		len(newChild.wildcards) == 0 {
		nc.delWildcardEdge(key)

		if n != root &&
			len(nc.statics) == 1 &&
			!nc.isLeaf() &&
			len(nc.params) == 0 &&
			len(nc.wildcards) == 0 {
			t.mergeChild(nc)
		}
	} else {
		nc.wildcards[idx] = newChild
	}

	return nc, deletedRoute
}

func (t *tXn) truncate(methods []string) {
	if len(methods) == 0 {
		t.root = make(map[string]*node)
		t.maxDepth = 0
		t.maxParams = 0
		t.size = 0
		t.forked = true
		return
	}

	updated := false
	for _, method := range methods {
		if _, ok := t.root[method]; ok {
			// Only fork the root if we have something to delete
			if !t.forked {
				t.root = maps.Clone(t.root)
				t.forked = true
			}
			delete(t.root, method)
			updated = true
		}

	}
	if updated {
		t.slowMax()
	}
}

func (t *tXn) computePathDepth(root *node, tokens []token) int {
	var depth int
	current := root

	if len(current.params) > 0 || len(current.wildcards) > 0 {
		depth++
	}

	for _, tk := range tokens {
		switch tk.typ {
		case nodeStatic:
			search := tk.value
			for len(search) > 0 {
				_, child := current.getStaticEdge(search[0])
				if child == nil || !strings.HasPrefix(search, child.key) {
					current = nil
					break
				}
				search = search[len(child.key):]
				current = child
				if len(current.params) > 0 || len(current.wildcards) > 0 {
					depth++
				}
			}
		case nodeParam:
			_, current = current.getParamEdge(canonicalKey(tk))
		case nodeWildcard:
			_, current = current.getWildcardEdge(canonicalKey(tk))
		}

		if current == nil {
			break
		}
	}

	return depth
}

func (t *tXn) slowMax() {
	type stack struct {
		edges []*node
		depth int
	}

	var stacks []stack
	if t.maxDepth < stackSizeThreshold {
		stacks = make([]stack, 0, stackSizeThreshold) // stack allocation
	} else {
		stacks = make([]stack, 0, t.maxDepth) // heap allocation
	}

	t.size = 0
	t.maxDepth = 0
	t.maxParams = 0

	for _, root := range t.root {
		stacks = append(stacks, stack{
			edges: []*node{root},
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

			depth := last.depth
			if len(elem.params) > 0 || len(elem.wildcards) > 0 {
				depth = depth + 1
			}

			if len(elem.statics) > 0 {
				stacks = append(stacks, stack{edges: elem.statics, depth: depth})
			}
			if len(elem.params) > 0 {
				stacks = append(stacks, stack{edges: elem.params, depth: depth})
			}
			if len(elem.wildcards) > 0 {
				stacks = append(stacks, stack{edges: elem.wildcards, depth: depth})
			}

			if elem.isLeaf() {
				t.size++
				t.maxParams = max(t.maxParams, len(elem.route.params))
				t.maxDepth = max(t.maxDepth, depth)
			}
		}
	}
}

func (t *tXn) writeNode(n *node) *node {
	if t.writable == nil {
		lru, err := simplelru.NewLRU[*node, struct{}](defaultModifiedCache, nil)
		if err != nil {
			panic(err)
		}
		t.writable = lru
	}

	if _, ok := t.writable.Get(n); ok {
		return n
	}

	nc := &node{
		label:  n.label,
		key:    n.key,
		route:  n.route,
		regexp: n.regexp,
		host:   n.host,
	}
	if len(n.statics) != 0 {
		nc.statics = make([]*node, len(n.statics))
		copy(nc.statics, n.statics)
	}
	if len(n.params) != 0 {
		nc.params = make([]*node, len(n.params))
		copy(nc.params, n.params)
	}
	if len(n.wildcards) != 0 {
		nc.wildcards = make([]*node, len(n.wildcards))
		copy(nc.wildcards, n.wildcards)
	}

	t.writable.Add(nc, struct{}{})
	return nc
}

// mergeChild is called to collapse the given node with its child. This is only
// called when the given node is not a leaf and has a single edge.
func (t *tXn) mergeChild(n *node) {
	child := n.statics[0]

	// A node that belong to a wildcar or param cannot be merged with a child.
	if n.label == 0x00 {
		return
	}
	// A node that belong to a host cannot be merged with a child key that start with a '/'.
	if n.host && strings.HasPrefix(child.key, "/") {
		return
	}

	// Merge nodes
	n.key = concat(n.key, child.key)
	n.route = child.route
	if len(child.statics) != 0 {
		n.statics = make([]*node, len(child.statics))
		copy(n.statics, child.statics)
	} else {
		n.statics = nil
	}

	if len(child.params) != 0 {
		n.params = make([]*node, len(child.params))
		copy(n.params, child.params)
	}

	if len(child.wildcards) != 0 {
		n.wildcards = make([]*node, len(child.wildcards))
		copy(n.wildcards, child.wildcards)
	}
}

// longestPrefix finds the length of the shared prefix of two strings
func longestPrefix(k1, k2 string) int {
	max := len(k1)
	if l := len(k2); l < max {
		max = l
	}
	var i int
	for i = 0; i < max; i++ {
		if k1[i] != k2[i] {
			break
		}
	}
	return i
}

// concat two string
func concat(a, b string) string {
	return a + b
}

// canonicalKey returns the internal key representation for a token.
// Returns the regexp pattern if present, otherwise returns a normalized
// placeholder ("?" for params, "*" for catch-alls).
func canonicalKey(tk token) string {
	if tk.regexp != nil {
		return tk.regexp.String()
	}
	switch tk.typ {
	case nodeParam:
		return "?"
	case nodeWildcard:
		return "*"
	default:
		panic("internal error: unknown token type")
	}
}

type insertMode uint8

const (
	modeInsert insertMode = iota
	modeUpdate
)

type nodeType uint8

const (
	nodeStatic nodeType = iota
	nodeParam
	nodeWildcard
)

type token struct {
	// Compiled regular expression constraint for params/wildcards, nil if none.
	regexp *regexp.Regexp
	// The literal string value of this token segment.
	value string
	// The type of this token: static, param, or wildcard.
	typ nodeType
	// True if this token is part of the hostname portion of the route.
	// Nodes created from tokens with hsplit=true cannot be merged
	// during deletion to preserve the hostname/path boundary for lookupByPath optimization.
	// Only relevant for nodeStatic tokens since params and wildcards
	// are isolated in their own nodes and never merged.
	hsplit bool
}
