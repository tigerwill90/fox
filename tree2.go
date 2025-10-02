package fox

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"

	"github.com/tigerwill90/fox/internal/simplelru"
)

const defaultModifiedCache2 = 4096

type iTree2 struct {
	pool      sync.Pool
	fox       *Router
	root      map[string]*node2
	size      int
	maxParams uint32
	maxDepth  uint32
}

func (t *iTree2) txn() *tXn2 {
	return &tXn2{
		tree:      t,
		root:      t.root,
		size:      t.size,
		maxParams: t.maxParams,
		maxDepth:  t.maxDepth,
	}
}

func (t *iTree2) allocateContext() *cTx {
	params := make([]string, 0, t.maxParams)
	tsrParams := make([]string, 0, t.maxParams)
	skipStack := make(skipStack, 0, t.maxDepth)
	return &cTx{
		params2:    &params,
		tsrParams2: &tsrParams,
		skipStack:  &skipStack,
		// This is a read only value, no reset. It's always the
		// owner of the pool.
		tree2: t,
		// This is a read only value, no reset
		fox: t.fox,
	}
}

func (t *iTree2) lookupByPath(root *node2, path string, c *cTx, lazy bool) (n *node2, tsr bool) {

	var (
		charsMatched     int
		skipStatic       bool
		childParamIdx    int
		childWildcardIdx int
	)

	current := root
	search := path
	*c.skipStack = (*c.skipStack)[:0]

Walk:
	for len(search) > 0 {
		if !skipStatic {
			label := search[0]
			x := string(label)
			_ = x
			if _, child := current.getStaticEdge(label); child != nil {
				keyLen := len(child.key)
				if keyLen <= len(search) && search[:keyLen] == child.key {
					x := search[:keyLen]
					y := child.key
					_ = x
					_ = y
					if len(current.params) > 0 || len(current.wildcards) > 0 {
						*c.skipStack = append(*c.skipStack, skipNode{
							node:      current,
							pathIndex: charsMatched,
							paramCnt:  len(*c.params2),
						})
					}

					current = child
					search = search[keyLen:]
					z := search
					_ = z
					charsMatched += keyLen
					continue
				}
			}
		}

		x := search
		_ = x
		// /foo/{bar}
		// /foo/b
		// search /foo/xyz
		skipStatic = false
		params := current.params[childParamIdx:]
		if len(params) > 0 {
			end := strings.IndexByte(search, slashDelim)
			if end == -1 {
				end = len(search)
			}

			if end == 0 {
				goto Backtrack //  // Empty segment
			}

			segment := search[:end]
			x := segment
			_ = x
			for i, paramNode := range params {
				if paramNode.regexp != nil {
					if !paramNode.regexp.MatchString(segment) {
						continue
					}
				}

				// Save other params/wildcards for backtracking (only params after current + all wildcards)
				nextChildIx := i + 1
				if nextChildIx < len(params) || len(current.wildcards) > 0 {
					*c.skipStack = append(*c.skipStack, skipNode{
						node:               current,
						pathIndex:          charsMatched,
						paramCnt:           len(*c.params2),
						childParamIndex:    nextChildIx + childParamIdx,
						childWildcardIndex: 0,
					})
				}

				if !lazy {
					*c.params2 = append(*c.params2, segment)
				}

				current = paramNode
				search = search[end:]
				x := search
				_ = x
				charsMatched += end
				childParamIdx = 0
				goto Walk
			}
		}

		wildcards := current.wildcards[childWildcardIdx:]
		if len(wildcards) > 0 {
			remaining := search
			subCtx := t.pool.Get().(*cTx)
			for _, wildcardNode := range wildcards {
				hasInfix := len(wildcardNode.statics) > 0
				if hasInfix {
					startCapture := charsMatched
					for offset := 0; offset <= len(remaining); offset++ {
						idx := strings.IndexByte(remaining[offset:], slashDelim)
						if idx < 0 {
							// No more slashes, wildcard would capture rest but no suffix match possible
							break
						}

						captureEnd := charsMatched + offset + idx
						captureValue := path[startCapture:captureEnd]

						// Validate regex constraint on captured value
						if wildcardNode.regexp != nil && !wildcardNode.regexp.MatchString(captureValue) {
							offset += idx + 1
							continue
						}

						// Empty segment validation
						if startCapture == captureEnd && offset > 0 {
							offset += idx + 1
							continue
						}

						suffixStart := captureEnd
						subSearch := path[suffixStart:]

						// Reset params
						*subCtx.params2 = (*subCtx.params2)[:0]

						subNode, subTsr := t.lookupByPath(wildcardNode, subSearch, subCtx, lazy)
						if subNode != nil {
							if subTsr {
								if !tsr {
									tsr = true
									n = subNode
									if !lazy {
										*c.tsrParams2 = (*c.tsrParams2)[:0]
										*c.tsrParams2 = append(*c.tsrParams2, *c.params2...)
										*c.tsrParams2 = append(*c.tsrParams2, captureValue)
										*c.tsrParams2 = append(*c.tsrParams2, *subCtx.params2...)
									}
								}
								offset += idx + 1
								continue
							}

							// Direct infix match
							if !lazy {
								*c.params2 = append(*c.params2, captureValue)
								*c.params2 = append(*c.params2, *subCtx.params2...)
							}

							t.pool.Put(subCtx)
							return subNode, false
						}

						offset += idx + 1
					}
				}
			}
			t.pool.Put(subCtx)

			for _, wildcardNode := range wildcards {
				if wildcardNode.isLeaf() {
					if wildcardNode.regexp != nil && !wildcardNode.regexp.MatchString(remaining) {
						continue
					}

					if !lazy {
						*c.params2 = append(*c.params2, remaining)
					}

					return wildcardNode, false
				}
			}
		}

		childParamIdx = 0
		childWildcardIdx = 0
		goto Backtrack
	}

	if current.route != nil {
		return current, false
	}

Backtrack:
	if len(*c.skipStack) == 0 {
		return n, tsr
	}

	skipped := c.skipStack.pop()

	if skipped.childParamIndex < len(skipped.node.params) {
		current = skipped.node
		*c.params2 = (*c.params2)[:skipped.paramCnt]
		search = path[skipped.pathIndex:]
		x := search
		_ = x
		charsMatched = skipped.pathIndex
		skipStatic = true
		childParamIdx = skipped.childParamIndex
		y := childParamIdx
		_ = y
		goto Walk
	}

	if skipped.childParamIndex < len(skipped.node.wildcards) {
		current = skipped.node
		*c.params2 = (*c.params2)[:skipped.paramCnt]
		search = path[skipped.pathIndex:]
		x := search
		_ = x
		charsMatched = skipped.pathIndex
		skipStatic = true
		childWildcardIdx = skipped.childWildcardIndex
		y := childWildcardIdx
		_ = y
		goto Walk
	}

	return n, tsr
}

type tXn2 struct {
	tree      *iTree2
	writable  *simplelru.LRU[*node2, struct{}]
	root      map[string]*node2
	method    string
	size      int
	maxParams uint32
	maxDepth  uint32
	forked    bool
	mode      insertMode
}

func (t *tXn2) commit() *iTree2 {
	tc := &iTree2{
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
	// t.forked = false // TODO verify
	return tc
}

// clone capture a point-in-time clone of the transaction. The cloned transaction will contain
// any uncommited writes in the original transaction but further mutations to either will be independent and result
// in different tree on commit.
func (t *tXn2) clone() *tXn2 {
	t.writable = nil
	t.forked = false
	tx := &tXn2{
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
func (t *tXn2) snapshot() map[string]*node2 {
	t.writable = nil
	t.forked = false
	return t.root
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
		if !t.forked {
			t.root = maps.Clone(t.root)
			t.forked = true
		}
		t.root[method] = newRoot
		t.maxDepth = max(t.maxDepth, t.computePathDepth(newRoot, route.tokens))
		t.maxParams = max(t.maxParams, route.psLen)
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

	// TODO tokenize in place
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

func (t *tXn2) insertStatic(n *node2, tk token, remaining []token, route *Route) (*node2, error) {
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
			&node2{
				label:  search[0],
				key:    search,
				hsplit: tk.hsplit,
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
	splitNode := &node2{
		label:  search[0],
		key:    search[:commonPrefix],
		hsplit: tk.hsplit,
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

func (t *tXn2) deleteTokens(root, n *node2, tokens []token) (*node2, *Route) {
	if len(tokens) == 0 {
		if !n.isLeaf() {
			return nil, nil
		}

		oldRoute := n.route
		nc := t.writeNode(n)
		nc.route = nil

		if n != root &&
			len(nc.statics) == 1 &&
			len(nc.params) == 0 &&
			len(nc.wildcards) == 0 &&
			nc.label != 0 {
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

	newChild, deletedRoute := t.deleteTokens(root, child, remaining)
	if deletedRoute == nil {
		return nil, nil
	}

	nc := t.writeNode(n)

	if newChild.route == nil &&
		len(newChild.statics) == 0 &&
		len(newChild.params) == 0 &&
		len(newChild.wildcards) == 0 {
		nc.delStaticEdge(label)

		// Clear hsplit if we deleted a '/' child. If this node was at a hostname/path
		// boundary, that boundary no longer exists. Otherwise, this is a no-op
		if label == slashDelim {
			nc.hsplit = false
		}

		if n != root &&
			!nc.isLeaf() &&
			len(nc.statics) == 1 &&
			len(nc.params) == 0 &&
			len(nc.wildcards) == 0 &&
			!nc.hsplit {
			t.mergeChild(nc)
		}
	} else {
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

// TODO add coverage for metrics
func (t *tXn2) truncate(methods []string) {
	if len(methods) == 0 {
		t.root = make(map[string]*node2)
		t.maxDepth = 0
		t.maxParams = 0
		t.size = 0
		t.forked = true
		return
	}

	if !t.forked {
		t.root = maps.Clone(t.root)
		t.forked = true
	}
	for _, method := range methods {
		delete(t.root, method)
	}
	t.slowMax()
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

func (t *tXn2) slowMax() {
	type stack struct {
		edges []*node2
		depth uint32
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
			edges: []*node2{root},
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

			alternative := len(elem.params) + len(elem.wildcards)
			totalDepth := last.depth + uint32(alternative)

			if len(elem.statics) > 0 {
				stacks = append(stacks, stack{edges: elem.statics, depth: totalDepth})
			}
			if len(elem.params) > 0 {
				stacks = append(stacks, stack{edges: elem.params, depth: totalDepth})
			}
			if len(elem.wildcards) > 0 {
				stacks = append(stacks, stack{edges: elem.wildcards, depth: totalDepth})
			}

			if elem.isLeaf() {
				t.size++
				t.maxParams = max(t.maxParams, elem.route.psLen)
				t.maxDepth = max(t.maxDepth, totalDepth)
			}
		}
	}
}

func (t *tXn2) writeNode(n *node2) *node2 {
	if t.writable == nil {
		lru, err := simplelru.NewLRU[*node2, struct{}](defaultModifiedCache2, nil)
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
		hsplit: n.hsplit,
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
	// Compiled regular expression constraint for params/wildcards, nil if none.
	regexp *regexp.Regexp
	// The literal string value of this token segment.
	value string
	// The type of this token: static, param, or wildcard.
	typ nodeType
	// True if this token ends at the hostname/path boundary.
	// Nodes created from tokens with hsplit=true cannot be merged
	// during deletion to preserve the boundary for lookupByPath optimization.
	// Only relevant for nodeStatic tokens since params and wildcards
	// are isolated in their own nodes and never merged.
	hsplit bool
}
