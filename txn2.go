package fox

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tigerwill90/fox/internal/simplelru"
)

type tXn2 struct {
	writable  *simplelru.LRU[*node2, struct{}]
	root      *node2
	size      int
	maxParams uint32
	depth     uint32
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
func (t *tXn2) insert(key string, route *Route) {
	tokens, _ := tokenizeKey(key)
	newRoot := t.insertTokens(t.root, tokens, route)
	if newRoot != nil {
		t.root = newRoot
		t.depth = max(t.depth, t.computePathDepth(tokens))
	}
	t.size++
}

func (t *tXn2) insertTokens(n *node2, tokens []token, route *Route) *node2 {

	// Base case: no tokens left, attach route
	if len(tokens) == 0 {
		nc := t.writeNode(n)
		nc.route = route
		return nc
	}

	seg := tokens[0]
	remaining := tokens[1:]

	switch seg.typ {
	case nodeStatic:
		return t.insertStatic(n, seg.value, remaining, route)
	case nodeParam:
		return t.insertParam(n, seg.value, remaining, route)
	case nodeCatchAll:
		return t.insertWildcard(n, seg.value, remaining, route)
	default:
		panic("internal error: unknown token type")
	}

	return nil
}

func (t *tXn2) insertStatic(n *node2, search string, remaining []token, route *Route) *node2 {

	if len(search) == 0 {
		return t.insertTokens(n, remaining, route)
	}

	idx, child := n.getStaticEdge(search[0])
	if child == nil {
		newChild := t.insertTokens(&node2{
			label: search[0],
			key:   search,
		}, remaining, route)
		if newChild != nil {
			nc := t.writeNode(n)
			nc.addStaticEdge(newChild)
			return nc
		}
		return nil
	}

	commonPrefix := longestPrefix(search, child.key)
	if commonPrefix == len(child.key) {
		search = search[commonPrefix:]
		// TODO check if len(search) > 0 is probably a optimization
		remaining = append([]token{{typ: nodeStatic, value: search}}, remaining...)
		// e.g. child /foo and want insert /fooo, insert "o"
		newChild := t.insertTokens(child, remaining, route)
		if newChild != nil {
			nc := t.writeNode(n)
			nc.statics[idx] = newChild
			return nc
		}
		return nil
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
			newSplitNode := t.insertTokens(splitNode, remaining, route)
			if newSplitNode != nil {
				nc.replaceStaticEdge(newSplitNode)
				return nc
			}
			return nil
		}
		splitNode.route = route
		return nc
	}
	// e.g. we have /foo and want to insert /fob
	// we first have our splitNode /fo, with old child (modChild) equal o, and insert the edge b

	newChild := t.insertTokens(&node2{
		label: search[0],
		key:   search,
	}, remaining, route)
	if newChild != nil {
		splitNode.addStaticEdge(newChild)
		return nc
	}

	return nil
}

func (t *tXn2) insertParam(n *node2, key string, remaining []token, route *Route) *node2 {
	idx, child := n.getParamEdge(key)
	if child == nil {
		newChild := t.insertTokens(
			&node2{
				key: key,
			},
			remaining,
			route,
		)
		nc := t.writeNode(n)
		nc.addParamEdge(newChild)
		return nc
	}

	newChild := t.insertTokens(child, remaining, route)
	if newChild != nil {
		nc := t.writeNode(n)
		nc.params[idx] = newChild
		return nc
	}
	return nil
}

func (t *tXn2) insertWildcard(n *node2, key string, remaining []token, route *Route) *node2 {
	idx, child := n.getWildcardEdge(key)
	if child == nil {
		newChild := t.insertTokens(
			&node2{
				key: key,
			},
			remaining,
			route,
		)
		nc := t.writeNode(n)
		nc.addWildcardEdge(newChild)
		return nc
	}

	newChild := t.insertTokens(child, remaining, route)
	if newChild != nil {
		nc := t.writeNode(n)
		nc.wildcards[idx] = newChild
		return nc
	}
	return nil
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
		label: n.label,
		key:   n.key,
		route: n.route,
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
func (t *tXn2) delete(key string) (*Route, bool) {
	tokens, err := tokenizeKey(key)
	if err != nil {
		return nil, false
	}

	newRoot, route := t.deleteTokens(t.root, tokens)
	if newRoot != nil {
		t.root = newRoot
	}

	if route != nil {
		t.size--
		return route, true
	}

	return nil, false
}

func (t *tXn2) deleteTokens(n *node2, tokens []token) (*node2, *Route) {
	// Base case: no tokens left, delete route at this node
	if len(tokens) == 0 {
		if !n.isLeaf() {
			return nil, nil
		}

		oldRoute := n.route
		nc := t.writeNode(n)
		nc.route = nil

		// If this is not root, not wildcard and has exactly one static child, merge
		if n != t.root && len(nc.statics) == 1 && len(nc.params) == 0 && len(nc.wildcards) == 0 && nc.label != 0 {
			t.mergeChild(nc)
		}

		return nc, oldRoute
	}

	seg := tokens[0]
	remaining := tokens[1:]

	switch seg.typ {
	case nodeStatic:
		return t.deleteStatic(n, seg.value, remaining)
	case nodeParam:
		return t.deleteParam(n, seg.value, remaining)
	case nodeCatchAll:
		return t.deleteWildcard(n, seg.value, remaining)
	default:
		panic("internal error: unknown token type")
	}
}

func (t *tXn2) deleteStatic(n *node2, search string, remaining []token) (*node2, *Route) {
	if len(search) == 0 {
		return t.deleteTokens(n, remaining)
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
	newChild, deletedRoute := t.deleteTokens(child, remaining)
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
		if n != t.root &&
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

func (t *tXn2) deleteParam(n *node2, key string, remaining []token) (*node2, *Route) {
	idx, child := n.getParamEdge(key)
	if child == nil {
		return nil, nil
	}

	// Recurse into param's children
	newChild, deletedRoute := t.deleteTokens(child, remaining)
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

		if n != t.root &&
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

func (t *tXn2) deleteWildcard(n *node2, key string, remaining []token) (*node2, *Route) {
	idx, child := n.getWildcardEdge(key)
	if child == nil {
		return nil, nil
	}

	// Recurse into wildcard's children
	newChild, deletedRoute := t.deleteTokens(child, remaining)
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

		if n != t.root &&
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

func (t *tXn2) computePathDepth(tokens []token) uint32 {
	var depth uint32
	current := t.root

	for _, seg := range tokens {
		depth += uint32(len(current.params) + len(current.wildcards))

		switch seg.typ {
		case nodeStatic:
			search := seg.value
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
			_, current = current.getParamEdge(seg.value)
		case nodeCatchAll:
			_, current = current.getWildcardEdge(seg.value)
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

type nodeType int

const (
	nodeStatic nodeType = iota
	nodeParam
	nodeCatchAll
)

type token struct {
	typ   nodeType
	value string
	regex *regexp.Regexp
}

// tokenizeKey splits a path into tokens, separating static portions from
// dynamic parameters ({placeholder}) and catch-all parameters ({placeholder...}).
//
// Examples:
//
//	/a/b/c/{bar}/baz       → ["a/b/c/", "{bar}", "/baz"]
//	/a/{foo}/b/{bar...}    → ["a/", "{foo}", "/b/", "{bar...}"]
//	/{id}/track            → ["", "{id}", "/track"]
//	/static                → ["static"]
func tokenizeKey(path string) ([]token, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("empty path")
	}

	var tokens []token
	var staticBuf strings.Builder
	i := 0

	for i < len(path) {
		if path[i] == '{' {
			// Flush any accumulated static content
			if staticBuf.Len() > 0 {
				tokens = append(tokens, token{
					typ:   nodeStatic,
					value: staticBuf.String(),
				})
				staticBuf.Reset()
			}

			// Find closing brace
			start := i
			i++
			for i < len(path) && path[i] != '}' {
				i++
			}

			if i >= len(path) {
				return nil, fmt.Errorf("unclosed parameter at position %d", start)
			}

			// Extract parameter content (including braces)
			param := path[start : i+1]
			i++ // Move past closing brace

			// Check if it's a catch-all (ends with ...)
			if strings.HasSuffix(param, "...}") {
				tokens = append(tokens, token{
					typ:   nodeCatchAll,
					value: param,
				})
			} else {
				tokens = append(tokens, token{
					typ:   nodeParam,
					value: param,
				})
			}
		} else {
			// Accumulate static content
			staticBuf.WriteByte(path[i])
			i++
		}
	}

	// Flush any remaining static content
	if staticBuf.Len() > 0 {
		tokens = append(tokens, token{
			typ:   nodeStatic,
			value: staticBuf.String(),
		})
	}

	return tokens, nil
}
