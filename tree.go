package fox

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/tigerwill90/fox/internal/simplelru"
	"github.com/tigerwill90/fox/internal/slicesutil"
)

const defaultModifiedCache = 4096

type iTree struct {
	pool      sync.Pool
	fox       *Router
	patterns  *node
	names     *node
	methods   map[string]uint
	size      int
	maxParams int
	maxDepth  int
}

func (t *iTree) txn() *tXn {
	return &tXn{
		tree:      t,
		patterns:  t.patterns,
		names:     t.names,
		methods:   t.methods,
		size:      t.size,
		maxParams: t.maxParams,
		maxDepth:  t.maxDepth,
	}
}

func (t *iTree) lookup(method, hostPort, path string, c *Context, lazy bool) (int, *node) {
	return t.patterns.lookup(method, hostPort, path, c, lazy)
}

func (t *iTree) lookupByPath(method, path string, c *Context, lazy bool) (int, *node) {
	c.tsr = false
	*c.skipStack = (*c.skipStack)[:0]
	return lookupByPath(t.patterns, method, path, c, lazy, offsetZero)
}

func (t *iTree) allocateContext() *Context {
	params := make([]string, 0, t.maxParams)
	tsrParams := make([]string, 0, t.maxParams)
	keys := make([]string, 0, t.maxParams)
	stacks := make(skipStack, 0, t.maxDepth)
	return &Context{
		params:     &params,
		tsrParams:  &tsrParams,
		skipStack:  &stacks,
		paramsKeys: &keys,
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
	patterns  *node
	names     *node
	methods   map[string]uint
	size      int
	maxParams int
	maxDepth  int
	forked    bool
	mode      insertMode
}

func (t *tXn) commit() *iTree {
	tc := &iTree{
		patterns:  t.patterns,
		names:     t.names,
		methods:   t.methods,
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
		patterns:  t.patterns,
		names:     t.names,
		methods:   t.methods,
		size:      t.size,
		maxParams: t.maxParams,
		maxDepth:  t.maxDepth,
	}
	return tx
}

// snapshot capture a point-in-time snapshot of the roots tree. Further mutation to txn
// will not be reflected on the snapshot.
func (t *tXn) snapshot() (patterns, names *node, methods map[string]uint) {
	t.writable = nil
	t.forked = false
	return t.patterns, t.names, t.methods
}

func (t *tXn) insertName(route *Route, mode insertMode) error {
	newRoot, err := t.insertNameIn(t.names, route.name, route, mode)
	if err != nil {
		return err
	}
	if newRoot != nil {
		t.names = newRoot
	}
	return nil
}

func (t *tXn) insertNameIn(n *node, search string, route *Route, mode insertMode) (*node, error) {
	if len(search) == 0 {
		if n.isLeaf() && mode != modeUpdate {
			// Unlike the regular insert path, where we don't know whether a route exists for the given name,
			// update mode provides a stronger guarantee: the caller has already verified that old.name == new.name
			// before invoking insert. This precondition eliminates the need to check for update errors in cases
			// like node splitting, which the standard insert path must handle.
			return nil, &RouteNameConflictError{New: route, Conflict: n.routes[0]}
		}

		nc := t.writeNode(n)
		nc.routes = []*Route{route}
		return nc, nil
	}

	idx, child := n.getStaticEdge(search[0])
	if child == nil {
		newChild := &node{
			label:  search[0],
			key:    search,
			routes: []*Route{route},
		}

		nc := t.writeNode(n)
		nc.addStaticEdge(newChild)
		return nc, nil
	}

	commonPrefix := longestPrefix(search, child.key)
	if commonPrefix == len(child.key) {
		search = search[commonPrefix:]
		newChild, err := t.insertNameIn(child, search, route, mode)
		if err != nil {
			return nil, err
		}
		nc := t.writeNode(n)
		nc.statics[idx] = newChild
		return nc, nil
	}

	nc := t.writeNode(n)
	splitNode := &node{
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
		splitNode.routes = []*Route{route}
		return nc, nil
	}

	splitNode.addStaticEdge(&node{
		label:  search[0],
		key:    search,
		routes: []*Route{route},
	})
	return nc, nil
}

func (t *tXn) deleteName(route *Route) bool {
	newRoot := t.deleteNameIn(t.names, t.names, route.name)
	if newRoot != nil {
		t.names = newRoot
		return true
	}

	return false
}

func (t *tXn) deleteNameIn(root, n *node, search string) *node {
	if len(search) == 0 {
		if !n.isLeaf() {
			return nil
		}

		nc := t.writeNode(n)
		nc.routes = nil

		if n != root && len(nc.statics) == 1 {
			t.mergeChild(nc)
		}

		return nc
	}

	label := search[0]
	idx, child := n.getStaticEdge(label)
	if child == nil || !strings.HasPrefix(search, child.key) {
		return nil
	}

	// Consume the matched portion
	search = search[len(child.key):]

	newChild := t.deleteNameIn(root, child, search)
	if newChild == nil {
		return nil
	}

	nc := t.writeNode(n)

	if !newChild.isLeaf() && len(newChild.statics) == 0 {
		nc.delStaticEdge(label)
		if n != root && !nc.isLeaf() && len(nc.statics) == 1 {
			t.mergeChild(nc)
		}
	} else {
		nc.statics[idx] = newChild
	}

	return nc
}

// insert performs a recursive copy-on-write insertion.
func (t *tXn) insert(route *Route, mode insertMode) error {
	t.mode = mode

	newRoot, err := t.insertTokens(nil, t.patterns, route.tokens, route)
	if err != nil {
		return err
	}
	if newRoot != nil {
		t.patterns = newRoot
		t.maxDepth = max(t.maxDepth, t.computePathDepth(newRoot, route.tokens))
		t.maxParams = max(t.maxParams, len(route.params))
		t.size++
		if len(route.methods) > 0 && t.mode == modeInsert {
			if !t.forked {
				t.methods = maps.Clone(t.methods)
				t.forked = true
			}

			for _, method := range route.methods {
				t.methods[method]++
			}
		}
	}
	return nil
}

func (t *tXn) insertTokens(p, n *node, tokens []token, route *Route) (*node, error) {
	// Base case: no tokens left, attach route
	if len(tokens) == 0 {
		switch t.mode {
		case modeInsert:
			if n.isLeaf() {
				if idx := slices.IndexFunc(n.routes, func(r *Route) bool {
					return r.matchersEqual(route.matchers) && slicesutil.Overlap(r.methods, route.methods)
				}); idx >= 0 {
					return nil, &RouteConflictError{New: route, Conflicts: []*Route{n.routes[idx]}}
				}
			}

			// Catch-all with empty capture conflict detection.
			// Routes like /foo/ and /foo/+{any} conflict because a request to /foo/ always matches the exact route,
			// making /foo/+{any} unreachable. The correct semantic is to use /foo/*{any} which doesn't capture empty segments.
			//
			// We handle both insertion orders:
			//   1. /foo/ then /foo/+{any}: check parent for existing exact match
			//   2. /foo/+{any} then /foo/: check child wildcards with catchEmpty flag
			//
			// Conflicts are method-aware: GET /foo/ and POST /foo/+{any} don't conflict since they route to different method sets.
			// Conflicts are matcher-agnostic: /foo/ and /foo/+{any}?a=b still conflict because pattern matching precedes matcher evaluation.
			var conflicts []*Route
			if route.catchEmpty && p != nil {
				for _, r := range p.routes {
					if slicesutil.Overlap(r.methods, route.methods) {
						conflicts = append(conflicts, r)
					}
				}
			}
			if len(conflicts) > 0 {
				return nil, &RouteConflictError{New: route, Conflicts: conflicts}
			}

			for _, wildcard := range n.wildcards {
				for _, r := range wildcard.routes {
					if r.catchEmpty && slicesutil.Overlap(r.methods, route.methods) {
						conflicts = append(conflicts, r)
					}
				}
			}
			if len(conflicts) > 0 {
				return nil, &RouteConflictError{New: route, Conflicts: conflicts}
			}

			if route.name != "" {
				if err := t.insertName(route, modeInsert); err != nil {
					return nil, err
				}
			}

			nc := t.writeNode(n)
			nc.addRoute(route)
			return nc, nil
		case modeUpdate:
			idx := slices.IndexFunc(n.routes, func(r *Route) bool {
				return r.pattern == route.pattern && slices.Equal(r.methods, route.methods) && r.matchersEqual(route.matchers)
			})
			if idx == -1 {
				return nil, newRouteNotFoundError(route)
			}

			oldRoute := n.routes[idx]
			// Updating a route supports mutating the handler, route options, and route name. Name changes require
			// extra care. If the caller updates a route without providing a name, any previously registered name
			// is deleted. If the same name is provided, the route for that name is updated. The most complex case
			// is when the caller wants to change the name: we must delete the old name entry and register the new one.
			// The order of operations is critical here; if any step fails (e.g., the new name collides with an
			// existing one), we must not have cloned any nodes while traversing the names tree. Therefore, we always
			// attempt the name insertion first, then update the patterns tree only on success.
			if oldRoute.name != "" {
				// If the new route has no name, we simply need to delete the old name (and the new route will not have
				// any name registered)
				if route.name == "" {
					t.deleteName(n.routes[idx])
				} else if oldRoute.name == route.name {
					// If the new route name is equal to the old route name, we need to update the registered name with
					// the new route. Since we have the guarantee that the route exist at oldRoute.name, this cannot fail.
					if err := t.insertName(route, modeUpdate); err != nil {
						panic(fmt.Errorf("internal error: update name: %w", err))
					}
				} else {
					// If the new route name is different from the old route name, we first try to insert the new name and
					// then only on success, we can safely deregister the old name.
					if err := t.insertName(route, modeInsert); err != nil {
						return nil, err
					}
					t.deleteName(n.routes[idx])
				}

				nc := t.writeNode(n)
				nc.replaceRoute(route)
				return nc, nil
			}

			// Last but not least, the oldRoute may not have any name registered, and in this case this is a simple
			// insert for this new name.
			if route.name != "" {
				if err := t.insertName(route, modeInsert); err != nil {
					return nil, err
				}
			}

			nc := t.writeNode(n)
			nc.replaceRoute(route)
			return nc, nil
		default:
			panic("internal error: unexpected insert mode")
		}
	}

	tk := tokens[0]
	remaining := tokens[1:]

	switch tk.typ {
	case nodeStatic:
		return t.insertStatic(p, n, tk, remaining, route)
	case nodeParam:
		return t.insertParam(n, tk, remaining, route)
	case nodeWildcard:
		return t.insertWildcard(n, tk, remaining, route)
	default:
		panic("internal error: unknown token type")
	}
}

func (t *tXn) insertStatic(p, n *node, tk token, remaining []token, route *Route) (*node, error) {
	search := tk.value

	if len(search) == 0 {
		return t.insertTokens(p, n, remaining, route)
	}

	idx, child := n.getStaticEdge(search[0])
	if child == nil {
		if t.mode == modeUpdate {
			return nil, newRouteNotFoundError(route)
		}

		newChild, err := t.insertTokens(
			n,
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
		newChild, err := t.insertTokens(n, child, remaining, route)
		if err != nil {
			return nil, err
		}
		nc := t.writeNode(n)
		nc.statics[idx] = newChild
		return nc, nil
	}

	if t.mode == modeUpdate {
		return nil, newRouteNotFoundError(route)
	}

	// All following case require creating a split node.
	splitNode := &node{
		label: search[0],
		key:   search[:commonPrefix],
		host:  tk.hsplit,
	}

	search = search[commonPrefix:]
	if len(search) == 0 {
		// e.g. we have /foo and want to insert /fo,
		// we first split /foo into /fo, o and then fo <- get the new route
		if len(remaining) > 0 {
			newSplitNode, err := t.insertTokens(n, splitNode, remaining, route)
			if err != nil {
				return nil, err
			}

			nc := t.writeNode(n)
			nc.replaceStaticEdge(newSplitNode)

			// Restore the existing child node
			modChild := t.writeNode(child)
			modChild.label = modChild.key[commonPrefix]
			modChild.key = modChild.key[commonPrefix:]
			newSplitNode.addStaticEdge(modChild)

			return nc, nil
		}

		if route.name != "" {
			if err := t.insertName(route, modeInsert); err != nil {
				return nil, err
			}
		}

		nc := t.writeNode(n)
		nc.replaceStaticEdge(splitNode)

		modChild := t.writeNode(child)
		modChild.label = modChild.key[commonPrefix]
		modChild.key = modChild.key[commonPrefix:]
		splitNode.addStaticEdge(modChild)
		splitNode.routes = []*Route{route}
		return nc, nil
	}

	// e.g. we have /foo and want to insert /fob
	// we first have our splitNode /fo, with old child (modChild) equal o, and insert the edge b
	newChild, err := t.insertTokens(
		n,
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
	nc.replaceStaticEdge(splitNode)

	modChild := t.writeNode(child)
	modChild.label = modChild.key[commonPrefix]
	modChild.key = modChild.key[commonPrefix:]
	splitNode.addStaticEdge(modChild)
	splitNode.addStaticEdge(newChild)
	return nc, nil
}

func (t *tXn) insertParam(n *node, tk token, remaining []token, route *Route) (*node, error) {
	key := canonicalKey(tk)
	idx, child := n.getParamEdge(key)
	if child == nil {
		if t.mode == modeUpdate {
			return nil, newRouteNotFoundError(route)
		}

		newChild, err := t.insertTokens(
			n,
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

	newChild, err := t.insertTokens(n, child, remaining, route)
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
			return nil, newRouteNotFoundError(route)
		}

		newChild, err := t.insertTokens(
			n,
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

	newChild, err := t.insertTokens(n, child, remaining, route)
	if err != nil {
		return nil, err
	}
	nc := t.writeNode(n)
	nc.wildcards[idx] = newChild
	return nc, nil
}

// delete performs a recursive copy-on-write deletion.
func (t *tXn) delete(route *Route) (*Route, bool) {

	newRoot, oldRoute := t.deleteTokens(t.patterns, t.patterns, route.tokens, route)
	if newRoot != nil {
		t.patterns = newRoot
		if !t.forked && len(route.methods) > 0 {
			t.methods = maps.Clone(t.methods)
			t.forked = true
		}
	}

	if oldRoute != nil {
		t.size--
		for _, method := range route.methods {
			t.methods[method]--
			if n, ok := t.methods[method]; ok && n == 0 {
				delete(t.methods, method)
			}
		}
		return oldRoute, true
	}

	return nil, false
}

func (t *tXn) deleteTokens(root, n *node, tokens []token, route *Route) (*node, *Route) {
	if len(tokens) == 0 {
		if !n.isLeaf() {
			return nil, nil
		}

		idx := slices.IndexFunc(n.routes, func(r *Route) bool {
			return r.pattern == route.pattern && slices.Equal(r.methods, route.methods) && r.matchersEqual(route.matchers)
		})
		if idx == -1 {
			return nil, nil
		}

		oldRoute := n.routes[idx]
		nc := t.writeNode(n)
		nc.delRoute(idx)
		if oldRoute.name != "" {
			t.deleteName(oldRoute) // The root key always hold the http method.
		}

		if n != root &&
			!nc.isLeaf() &&
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
		return t.deleteStatic(root, n, tk.value, remaining, route)
	case nodeParam:
		return t.deleteParam(root, n, canonicalKey(tk), remaining, route)
	case nodeWildcard:
		return t.deleteWildcard(root, n, canonicalKey(tk), remaining, route)
	default:
		panic("internal error: unknown token type")
	}
}

func (t *tXn) deleteStatic(root, n *node, search string, remaining []token, route *Route) (*node, *Route) {
	if len(search) == 0 {
		return t.deleteTokens(root, n, remaining, route)
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

	newChild, deletedRoute := t.deleteTokens(root, child, remaining, route)
	if deletedRoute == nil {
		return nil, nil
	}

	nc := t.writeNode(n)

	if !newChild.isLeaf() &&
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

func (t *tXn) deleteParam(root, n *node, key string, remaining []token, route *Route) (*node, *Route) {
	idx, child := n.getParamEdge(key)
	if child == nil {
		return nil, nil
	}

	// Recurse into param's children
	newChild, deletedRoute := t.deleteTokens(root, child, remaining, route)
	if deletedRoute == nil {
		return nil, nil
	}

	nc := t.writeNode(n)

	// If param node is now empty, remove it
	if !newChild.isLeaf() &&
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

func (t *tXn) deleteWildcard(root, n *node, key string, remaining []token, route *Route) (*node, *Route) {
	idx, child := n.getWildcardEdge(key)
	if child == nil {
		return nil, nil
	}

	// Recurse into wildcard's children
	newChild, deletedRoute := t.deleteTokens(root, child, remaining, route)
	if deletedRoute == nil {
		return nil, nil
	}

	nc := t.writeNode(n)

	// If wildcard node is now empty, remove it
	if !newChild.isLeaf() &&
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

func (t *tXn) truncate() {
	t.patterns = new(node)
	t.names = new(node)
	t.methods = make(map[string]uint)
	t.maxDepth = 0
	t.maxParams = 0
	t.size = 0
	t.writable = nil
	t.forked = false
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
		regexp: n.regexp,
		host:   n.host,
	}

	if len(n.routes) != 0 {
		nc.routes = make([]*Route, len(n.routes))
		copy(nc.routes, n.routes)
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

	// A node that belong to a wildcard or param cannot be merged with a child.
	if n.label == 0x00 {
		return
	}
	// A node that belong to a host cannot be merged with a child key that start with a '/'.
	if n.host && strings.HasPrefix(child.key, "/") {
		return
	}

	// Merge nodes
	n.key = concat(n.key, child.key)

	if len(child.routes) != 0 {
		n.routes = make([]*Route, len(child.routes))
		copy(n.routes, child.routes)
	}

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
		expr := tk.regexp.String()
		return expr[1 : len(expr)-1]
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
