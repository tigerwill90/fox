package fox

import (
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/tigerwill90/fox/internal/simplelru"
)

type tXn2 struct {
	writable  *simplelru.LRU[*node2, struct{}]
	root      map[string]*node2
	written   bool
	size      int
	maxParams uint32
	depth     uint32
	method    string
	mode      insertMode
}

// insert performs a recursive copy-on-write insertion of a route into the tree.
// It uses path copying to create a new tree version: only nodes along the path
// from root to the insertion point are cloned, while unmodified subtrees are
// shared with the previous version. This enables lock-free concurrent reads
// against the old root while the new version is being constructed.
//
// The insertion proceeds in two phases:
// 1. Descend: traverse the tree (read-only) to find the insertion point
// 2. Ascend: clone and modify nodes along the path during stack unwinding
//
// Upon successful insertion, the transaction's root is updated to point to the
// new tree version.
func (t *tXn2) insert(method string, route *Route, mode insertMode) error {
	root := t.root[method]
	if root == nil {
		if t.mode == modeUpdate {
			return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, route.pattern)
		}
		root = &node2{
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
		if !t.written {
			t.root = maps.Clone(t.root)
			t.written = true
		}
		t.root[method] = newRoot
		t.depth = max(t.depth, t.computePathDepth(newRoot, route.tokens))
		t.size++
	}
	return nil
}

func (t *tXn2) insertTokens(n *node2, tokens []token, route *Route) (*node2, error) {

	// Base case: no tokens left, attach route
	if len(tokens) == 0 {
		if t.mode == modeInsert && n.isLeaf() {
			return nil, fmt.Errorf("%w: new route %s %s conflict with %s", ErrRouteExist, t.method, route.pattern, n.route.pattern)
		}
		if t.mode == modeUpdate && !n.isLeaf() {
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
		return t.insertStatic(n, tk.value, remaining, route)
	case nodeParam:
		return t.insertParam(n, tk, remaining, route)
	case nodeWildcard:
		return t.insertWildcard(n, tk, remaining, route)
	default:
		panic("internal error: unknown token type")
	}
}

func (t *tXn2) insertStatic(n *node2, search string, remaining []token, route *Route) (*node2, error) {

	if len(search) == 0 {
		return t.insertTokens(n, remaining, route)
	}

	idx, child := n.getStaticEdge(search[0])
	if child == nil {
		if t.mode == modeUpdate {
			return nil, fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, t.method, route.pattern)
		}

		newChild, err := t.insertTokens(
			&node2{
				label: search[0],
				key:   search,
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
		// TODO check if len(search) > 0 is probably a optimization
		remaining = append([]token{{typ: nodeStatic, value: search}}, remaining...)
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
	splitNode := &node2{
		label: search[0],
		key:   search[:commonPrefix],
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
		&node2{
			label: search[0],
			key:   search,
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

func (t *tXn2) insertParam(n *node2, tk token, remaining []token, route *Route) (*node2, error) {
	key := canonicalKey(tk)
	idx, child := n.getParamEdge(key)
	if child == nil {
		if t.mode == modeUpdate {
			return nil, fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, t.method, route.pattern)
		}

		newChild, err := t.insertTokens(
			&node2{
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

func (t *tXn2) insertWildcard(n *node2, tk token, remaining []token, route *Route) (*node2, error) {
	key := canonicalKey(tk)
	idx, child := n.getWildcardEdge(key)
	if child == nil {
		if t.mode == modeUpdate {
			return nil, fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, t.method, route.pattern)
		}

		newChild, err := t.insertTokens(
			&node2{
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

func (t *tXn2) writeNode(n *node2) *node2 {
	if t.writable == nil {
		lru, err := simplelru.NewLRU[*node2, struct{}](defaultModifiedCache, nil)
		if err != nil {
			panic(err)
		}
		t.writable = lru
	}

	if _, ok := t.writable.Get(n); ok {
		return n
	}

	nc := &node2{
		label:  n.label,
		key:    n.key,
		route:  n.route,
		regexp: n.regexp,
	}
	if len(n.statics) != 0 {
		nc.statics = make([]*node2, len(n.statics))
		copy(nc.statics, n.statics)
	}
	if len(n.params) != 0 {
		nc.params = make([]*node2, len(n.params))
		copy(nc.params, n.params)
	}
	if len(n.wildcards) != 0 {
		nc.wildcards = make([]*node2, len(n.wildcards))
		copy(nc.wildcards, n.wildcards)
	}

	t.writable.Add(nc, struct{}{})
	return nc
}

// delete performs a recursive copy-on-write deletion of a route from the tree.
// It uses path copying to create a new tree version: only nodes along the path
// from root to the deletion point are cloned, while unmodified subtrees are
// shared with the previous version. This enables lock-free concurrent reads
// against the old root while the new version is being constructed.
//
// The deletion proceeds in two phases:
//  1. Descend: traverse the tree (read-only) to find the target route
//  2. Ascend: clone and modify nodes along the path during stack unwinding,
//     pruning empty nodes and merging static nodes where appropriate
//
// Returns the deleted route and true if found, nil and false otherwise.
// Upon successful deletion, the transaction's root is updated to point to the
// new tree version.
func (t *tXn2) delete(method string, tokens []token) (*Route, bool) {
	root := t.root[method]
	if root == nil {
		return nil, false
	}

	newRoot, route := t.deleteTokens(root, root, tokens)
	if newRoot != nil {
		if !t.written {
			t.root = maps.Clone(t.root)
			t.written = true
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

func (t *tXn2) deleteTokens(root, n *node2, tokens []token) (*node2, *Route) {
	// Base case: no tokens left, delete route at this node
	if len(tokens) == 0 {
		if !n.isLeaf() {
			return nil, nil
		}

		oldRoute := n.route
		nc := t.writeNode(n)
		nc.route = nil

		// If this is not root, not wildcard and has exactly one static child, merge
		if n != root && len(nc.statics) == 1 && len(nc.params) == 0 && len(nc.wildcards) == 0 && nc.label != 0 {
			t.mergeChild(nc)
		}

		return nc, oldRoute
	}

	tk := tokens[0]
	remaining := tokens[1:]

	switch tk.typ {
	case nodeStatic:
		return t.deleteStatic(root, n, tk.value, remaining)
	case nodeParam:
		return t.deleteParam(root, n, canonicalKey(tk), remaining)
	case nodeWildcard:
		return t.deleteWildcard(root, n, canonicalKey(tk), remaining)
	default:
		panic("internal error: unknown token type")
	}
}

func (t *tXn2) deleteStatic(root, n *node2, search string, remaining []token) (*node2, *Route) {
	if len(search) == 0 {
		return t.deleteTokens(root, n, remaining)
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

	// Recurse into child
	newChild, deletedRoute := t.deleteTokens(root, child, remaining)
	if deletedRoute == nil {
		return nil, nil
	}

	nc := t.writeNode(n)

	// Check if child is now empty (no route, no children)
	if newChild.route == nil &&
		len(newChild.statics) == 0 &&
		len(newChild.params) == 0 &&
		len(newChild.wildcards) == 0 {
		// Remove the empty child
		nc.delStaticEdge(label)

		// If parent now has exactly one static child and no route, merge
		if n != root &&
			len(nc.statics) == 1 &&
			!nc.isLeaf() &&
			len(nc.params) == 0 &&
			len(nc.wildcards) == 0 {
			t.mergeChild(nc)
		}
	} else {
		// Update the child reference
		nc.statics[idx] = newChild
	}

	return nc, deletedRoute
}

func (t *tXn2) deleteParam(root, n *node2, key string, remaining []token) (*node2, *Route) {
	idx, child := n.getParamEdge(key)
	if child == nil {
		return nil, nil
	}

	// Recurse into param's children
	newChild, deletedRoute := t.deleteTokens(root, child, remaining)
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

func (t *tXn2) deleteWildcard(root, n *node2, key string, remaining []token) (*node2, *Route) {
	idx, child := n.getWildcardEdge(key)
	if child == nil {
		return nil, nil
	}

	// Recurse into wildcard's children
	newChild, deletedRoute := t.deleteTokens(root, child, remaining)
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

func (t *tXn2) computePathDepth(root *node2, tokens []token) uint32 {
	var depth uint32
	current := root

	for _, tk := range tokens {
		depth += uint32(len(current.params) + len(current.wildcards))

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

// mergeChild is called to collapse the given node with its child. This is only
// called when the given node is not a leaf and has a single edge.
func (t *tXn2) mergeChild(n *node2) {
	child := n.statics[0]

	// Merge nodes
	n.key = concat(n.key, child.key)
	n.route = child.route
	if len(child.statics) != 0 {
		n.statics = make([]*node2, len(child.statics))
		copy(n.statics, child.statics)
	} else {
		n.statics = nil
	}

	if len(child.params) != 0 {
		n.params = make([]*node2, len(child.params))
		copy(n.params, child.params)
	}

	if len(child.wildcards) != 0 {
		n.wildcards = make([]*node2, len(child.wildcards))
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
	typ    nodeType
	value  string
	regexp *regexp.Regexp
}
