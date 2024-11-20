// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"github.com/tigerwill90/fox/internal/simplelru"
	"strings"
	"sync"
)

const defaultModifiedCache = 4096

// iTree implements an immutable Radix Tree. The immutability means that it is safe to concurrently read from a Tree
// without any coordination.
type iTree struct {
	ctx       sync.Pool
	root      roots
	fox       *Router
	maxParams uint32
	maxDepth  uint32
}

func (t *iTree) txn() *tXn {
	return &tXn{
		tree:      t,
		root:      t.root,
		maxParams: t.maxParams,
		maxDepth:  t.maxDepth,
	}
}

// lookup  returns the node matching the host and/or path. If lazy is false, it parses and record into c, path segment according to
// the route definition. In case of indirect match, tsr is true and n != nil.
func (t *iTree) lookup(method, hostPort, path string, c *cTx, lazy bool) (n *node, tsr bool) {
	return t.root.lookup(t, method, hostPort, path, c, lazy)
}

// tXn is a transaction on the tree. This transaction is applied
// atomically and returns a new tree when committed. A transaction
// is not thread safe, and should only be used by a single goroutine.
type tXn struct {
	tree      *iTree
	writable  *simplelru.LRU[*node, any]
	root      roots
	maxParams uint32
	maxDepth  uint32
}

func (t *tXn) commit() *iTree {
	nt := &iTree{
		root:      t.root,
		fox:       t.tree.fox,
		maxParams: t.maxParams,
		maxDepth:  t.maxDepth,
	}
	nt.ctx = sync.Pool{
		New: func() any {
			return nt.allocateContext()
		},
	}
	t.writable = nil
	return nt
}

// clone capture a point-in-time clone of the transaction. The cloned transaction will contain
// any uncommited writes in the original transaction but further mutations to either will be independent and result
// in different tree on commit.
func (t *tXn) clone() *tXn {
	// reset the writable node cache to avoid leaking future writes into the clone
	t.writable = nil
	tx := &tXn{
		tree:      t.tree,
		root:      t.root,
		maxParams: t.maxParams,
		maxDepth:  t.maxDepth,
	}
	return tx
}

// snapshot capture a point-in-time snapshot of the roots tree. Further mutation to txn
// will not be reflected on the snapshot.
func (t *tXn) snapshot() roots {
	t.writable = nil
	return t.root
}

// copyOnWriteSearch find the node that match the provided path. It clones from the root
// every node visited for the first time, except the matching one.
func (t *tXn) copyOnWriteSearch(rootNode *node, path string) searchResult {
	if t.writable == nil {
		lru, err := simplelru.NewLRU[*node, any](defaultModifiedCache, nil)
		if err != nil {
			panic(err)
		}
		t.writable = lru
	}

	current := rootNode

	var (
		ppp                     *node
		pp                      *node
		p                       *node
		charsMatched            int
		charsMatchedInNodeFound int
		depth                   uint32
	)

STOP:
	for charsMatched < len(path) {
		next := current.getEdge(path[charsMatched])
		if next == nil {
			break STOP
		}

		depth++
		ppp = pp
		pp = p
		p = current
		if p != nil {
			if _, ok := t.writable.Get(p); !ok {
				cp := p.clone()
				t.writable.Add(cp, nil)
				if pp == nil {
					t.updateRoot(cp)
				} else {
					pp.updateEdge(cp)
				}
				p = cp
			}
		}
		current = next
		charsMatchedInNodeFound = 0
		for i := 0; charsMatched < len(path); i++ {
			if i >= len(current.key) {
				break
			}

			if current.key[i] != path[charsMatched] {
				break STOP
			}

			charsMatched++
			charsMatchedInNodeFound++
		}
	}

	return searchResult{
		path:                    path,
		matched:                 current,
		charsMatched:            charsMatched,
		charsMatchedInNodeFound: charsMatchedInNodeFound,
		p:                       p,
		pp:                      pp,
		ppp:                     ppp,
		depth:                   depth,
	}
}

// insert is not safe for concurrent use
func (t *tXn) insert(method string, route *Route, paramsN uint32) error {
	var rootNode *node
	index := t.root.methodIndex(method)
	if index < 0 {
		rootNode = &node{
			key:                method,
			paramChildIndex:    -1,
			wildcardChildIndex: -1,
		}
		t.addRoot(rootNode)
	} else {
		rootNode = t.root[index]
	}

	path := route.pattern

	result := t.copyOnWriteSearch(rootNode, path)
	switch result.classify() {
	case exactMatch:
		// e.g. matched exactly "te" node when inserting "te" key.
		// te
		// ├── st
		// └── am
		// Create a new node from "st" reference and update the "te" (parent) reference to "st" node.
		if result.matched.isLeaf() {
			return fmt.Errorf("%w: new route %s %s conflict with %s", ErrRouteExist, method, route.pattern, result.matched.route.pattern)
		}

		// We are updating an existing node. We only need to create a new node from
		// the matched one with the updated/added value (handler and wildcard).
		n := newNodeFromRef(
			result.matched.key,
			route,
			result.matched.children,
			result.matched.childKeys,
			result.matched.paramChildIndex,
			result.matched.wildcardChildIndex,
		)

		t.updateMaxParams(paramsN)
		result.p.updateEdge(n)
	case keyEndMidEdge:
		// e.g. matched until "s" for "st" node when inserting "tes" key.
		// te
		// ├── st
		// └── am
		//
		// After patching
		// te
		// ├── am
		// └── s
		//     └── t
		// It requires to split "st" node.
		// 1. Create a "t" node from "st" reference.
		// 2. Create a new "s" node for "tes" key and link it to the child "t" node.
		// 3. Update the "te" (parent) reference to the new "s" node (we are swapping old "st" to new "s" node, first
		//    char remain the same).
		// Note that for key end-mid-edge, we never have to deal with hostname/path split, as hostname
		// always end with / per validation, so it end up on incomplete match case.

		keyCharsFromStartOfNodeFound := path[result.charsMatched-result.charsMatchedInNodeFound:]
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.key)
		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)

		child := newNodeFromRef(
			suffixFromExistingEdge,
			result.matched.route,
			result.matched.children,
			result.matched.childKeys,
			result.matched.paramChildIndex,
			result.matched.wildcardChildIndex,
		)

		parent := newNode(
			cPrefix,
			route,
			[]*node{child},
		)

		t.updateMaxParams(paramsN)
		t.updateMaxDepth(result.depth + 1)
		result.p.updateEdge(parent)
	case incompleteMatchToEndOfEdge:
		// e.g. matched until "st" for "st" node but still have remaining char (ify) when inserting "testify" key.
		// te
		// ├── st
		// └── am
		//
		// After patching
		// te
		// ├── am
		// └── st
		//     └── ify
		// 1. Create a new "ify" child node.
		// 2. Recreate the "st" node and link it to it's existing children and the new "ify" node.
		// 3. Update the "te" (parent) node to the new "st" node.

		keySuffix := path[result.charsMatched:]
		addDepth := uint32(1)

		// For hostname route, we always insert the path in a dedicated sub-child.
		// This allows to perform lookup optimization for route with hostname name.
		var child *node
		if route.hostSplit > 0 && result.charsMatched < route.hostSplit {
			host, p := keySuffix[:route.hostSplit-result.charsMatched], keySuffix[route.hostSplit-result.charsMatched:]
			pathChild := newNode(p, route, nil)
			child = newNode(host, nil, []*node{pathChild})
			addDepth++
		} else {
			// No children, so no paramChild
			child = newNode(keySuffix, route, nil)
		}

		edges := result.matched.getEdges()
		// new edges slices, so it can be reordered by slices.SortFunc in newNode()
		edges = append(edges, child)
		n := newNode(
			result.matched.key,
			result.matched.route,
			edges,
		)

		t.updateMaxDepth(result.depth + addDepth)
		t.updateMaxParams(paramsN)

		if result.matched == rootNode {
			n.key = method
			t.writable.Add(n, nil)
			t.updateRoot(n)
			break
		}
		result.p.updateEdge(n)
	case incompleteMatchToMiddleOfEdge:
		// e.g. matched until "s" for "st" node but still have remaining char ("s") which does not match anything
		// when inserting "tess" key.
		// te
		// ├── st
		// └── am
		//
		// After patching
		// te
		// ├── am
		// └── s
		//     ├── s
		//     └── t
		// It requires to split "st" node.
		// 1. Create a new "s" child node for "tess" key.
		// 2. Create a new "t" node from "st" reference (link "st" children to new "t" node).
		// 3. Create a new "s" node and link it to "s" and "t" node.
		// 4. Update the "te" (parent) node to the new "s" node (we are swapping old "st" to new "s" node, first
		//    char remain the same).

		keyCharsFromStartOfNodeFound := path[result.charsMatched-result.charsMatchedInNodeFound:]
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.key)
		isHostname := result.charsMatched <= route.hostSplit
		// Rule: a node with {param} or *{wildcard} has no child or has a separator before the end of the key
		if !isHostname {
			for i := len(cPrefix) - 1; i >= 0; i-- {
				if cPrefix[i] == '/' {
					break
				}

				if cPrefix[i] == '{' || cPrefix[i] == '*' {
					return newConflictErr(method, path, getRouteConflict(result.matched))
				}
			}
		} else if !strings.HasSuffix(cPrefix, "}") {
			// e.g. a.{b} is valid
			for i := len(cPrefix) - 1; i >= 0; i-- {
				if cPrefix[i] == '.' {
					break
				}

				if cPrefix[i] == '{' {
					return newConflictErr(method, path, getRouteConflict(result.matched))
				}
			}
		}

		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)
		keySuffix := path[result.charsMatched:]

		addDepth := uint32(1)
		// For domain route, we always insert the path in a dedicated sub-child.
		// This allows to perform lookup optimization for domain name.
		var n1 *node
		if route.hostSplit > 0 && result.charsMatched < route.hostSplit {
			host, p := keySuffix[:route.hostSplit-result.charsMatched], keySuffix[route.hostSplit-result.charsMatched:]
			pathChild := newNodeFromRef(p, route, nil, nil, -1, -1)
			n1 = newNode(host, nil, []*node{pathChild})
			addDepth++
		} else {
			// No children, so no paramChild or wildcardChild
			n1 = newNodeFromRef(keySuffix, route, nil, nil, -1, -1) // inserted node
		}

		n2 := newNodeFromRef(
			suffixFromExistingEdge,
			result.matched.route,
			result.matched.children,
			result.matched.childKeys,
			result.matched.paramChildIndex,
			result.matched.wildcardChildIndex,
		) // previous matched node

		// n3 children never start with a param
		n3 := newNode(cPrefix, nil, []*node{n1, n2}) // intermediary node

		t.updateMaxDepth(result.depth + addDepth)
		t.updateMaxParams(paramsN)
		result.p.updateEdge(n3)
	default:
		// safeguard against introducing a new result type
		panic("internal error: unexpected result type")
	}
	return nil
}

// update is not safe for concurrent use
func (t *tXn) update(method string, route *Route) error {
	path := route.pattern
	index := t.root.methodIndex(method)
	if index < 0 {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, path)
	}

	result := t.copyOnWriteSearch(t.root[index], path)
	if !result.isExactMatch() || !result.matched.isLeaf() {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, path)
	}

	// We are updating an existing node (could be a leaf or not). We only need to create a new node from
	// the matched one with the updated/added value (handler and wildcard).
	n := newNodeFromRef(
		result.matched.key,
		route,
		result.matched.children,
		result.matched.childKeys,
		result.matched.paramChildIndex,
		result.matched.wildcardChildIndex,
	)

	result.p.updateEdge(n)
	return nil
}

// remove is not safe for concurrent use.
func (t *tXn) remove(method, path string) bool {
	index := t.root.methodIndex(method)
	if index < 0 {
		return false
	}

	result := t.copyOnWriteSearch(t.root[index], path)
	if !result.isExactMatch() || !result.matched.isLeaf() {
		// Not and exact match or this node was created after a split (KEY_END_MID_EGGE operation),
		// therefore we cannot delete it.
		return false
	}

	if len(result.matched.children) > 1 {
		n := newNodeFromRef(
			result.matched.key,
			nil,
			result.matched.children,
			result.matched.childKeys,
			result.matched.paramChildIndex,
			result.matched.wildcardChildIndex,
		)

		result.p.updateEdge(n)
		return true
	}

	if len(result.matched.children) == 1 {
		child := result.matched.children[0]
		mergedPath := fmt.Sprintf("%s%s", result.matched.key, child.key)
		n := newNodeFromRef(
			mergedPath,
			child.route,
			child.children,
			child.childKeys,
			child.paramChildIndex,
			child.wildcardChildIndex,
		)

		result.p.updateEdge(n)
		return true
	}

	// recreate the parent edges without the removed node
	parentEdges := recreateParentEdge(result.p, result.matched)
	parentIsRoot := result.p == t.root[index]

	// The parent was the result of a previous hostname/path split, so we have at least depth 3,
	// where p can not be root, but pp and ppp may.
	if len(parentEdges) == 0 && !result.p.isLeaf() && !parentIsRoot {
		parentEdges = recreateParentEdge(result.pp, result.p)
		var parent *node
		parentParentIsRoot := result.pp == t.root[index]
		if len(parentEdges) == 1 && !result.pp.isLeaf() && !strings.HasPrefix(parentEdges[0].key, "/") && !parentParentIsRoot {
			// Note that !strings.HasPrefix(parentEdges[0].key, "/") ensure that we do not merge back a hostname
			// its path.
			// 		DELETE a.b.c{d}/foo/bar
			//		path: GET
			//		      path: a.b
			//		          path: .c{d}
			//		              path: /foo/bar
			//		          path: /
			//
			//		AFTER
			//		path: GET
			//		      path: a.b/ => bad
			child := parentEdges[0]
			mergedPath := fmt.Sprintf("%s%s", result.pp.key, child.key)
			parent = newNodeFromRef(
				mergedPath,
				child.route,
				child.children,
				child.childKeys,
				child.paramChildIndex,
				child.wildcardChildIndex,
			)
		} else {
			parent = newNode(result.pp.key, result.pp.route, parentEdges)
		}

		if parentParentIsRoot {
			if len(parent.children) == 0 && isRemovable(method) {
				return t.removeRoot(method)
			}
			parent.key = method
			t.writable.Add(parent, nil)
			t.updateRoot(parent)
			return true
		}

		result.ppp.updateEdge(parent)
		return true
	}

	var parent *node
	if len(parentEdges) == 1 && !result.p.isLeaf() && !parentIsRoot {
		child := parentEdges[0]
		mergedPath := fmt.Sprintf("%s%s", result.p.key, child.key)
		parent = newNodeFromRef(
			mergedPath,
			child.route,
			child.children,
			child.childKeys,
			child.paramChildIndex,
			child.wildcardChildIndex,
		)
	} else {
		parent = newNode(
			result.p.key,
			result.p.route,
			parentEdges,
		)
	}

	if parentIsRoot {
		if len(parent.children) == 0 && isRemovable(method) {
			return t.removeRoot(method)
		}
		parent.key = method
		t.writable.Add(parent, nil)
		t.updateRoot(parent)
		return true
	}

	result.pp.updateEdge(parent)
	return true
}

// addRoot append a new root node to the tree.
func (t *tXn) addRoot(n *node) {
	nr := make([]*node, 0, len(t.root)+1)
	nr = append(nr, t.root...)
	nr = append(nr, n)
	t.root = nr
}

// updateRoot replaces a root node in the tree.
func (t *tXn) updateRoot(n *node) bool {
	// for root node, the key contains the HTTP verb.
	index := t.root.methodIndex(n.key)
	if index < 0 {
		return false
	}
	nr := make([]*node, 0, len(t.root))
	nr = append(nr, t.root[:index]...)
	nr = append(nr, n)
	nr = append(nr, t.root[index+1:]...)
	t.root = nr
	return true
}

// removeRoot remove a root node from the tree.
func (t *tXn) removeRoot(method string) bool {
	index := t.root.methodIndex(method)
	if index < 0 {
		return false
	}
	nr := make([]*node, 0, len(t.root)-1)
	nr = append(nr, t.root[:index]...)
	nr = append(nr, t.root[index+1:]...)
	t.root = nr
	return true
}

// truncate truncates the tree for the provided methods. If not methods are provided,
// all methods are truncated.
func (t *tXn) truncate(methods []string) {
	if len(methods) == 0 {
		// Pre instantiate nodes for common http verb
		nr := make(roots, len(commonVerbs))
		for i := range commonVerbs {
			nr[i] = new(node)
			nr[i].key = commonVerbs[i]
			nr[i].paramChildIndex = -1
			nr[i].wildcardChildIndex = -1
		}
		t.root = nr
		return
	}

	oldlen := len(t.root)
	nr := make(roots, len(t.root))
	copy(nr, t.root)

	for _, method := range methods {
		idx := nr.methodIndex(method)
		if idx < 0 {
			continue
		}
		if !isRemovable(method) {
			nr[idx] = new(node)
			nr[idx].key = commonVerbs[idx]
			nr[idx].paramChildIndex = -1
			nr[idx].wildcardChildIndex = -1
			continue
		}

		nr = append(nr[:idx], nr[idx+1:]...)
	}

	clear(nr[len(nr):oldlen]) // zero/nil out the obsolete elements, for GC

	t.root = nr
}

// updateMaxParams perform an update only if max is greater than the current
func (t *tXn) updateMaxParams(max uint32) {
	if max > t.maxParams {
		t.maxParams = max
	}
}

// updateMaxDepth perform an update only if max is greater than the current
func (t *tXn) updateMaxDepth(max uint32) {
	if max > t.maxDepth {
		t.maxDepth = max
	}
}

func commonPrefix(k1, k2 string) string {
	minLength := min(len(k1), len(k2))
	for i := 0; i < minLength; i++ {
		if k1[i] != k2[i] {
			return k1[:i]
		}
	}
	return k1[:minLength]
}

// recreateParentEdge returns a copy of parent children, minus the matched node.
func recreateParentEdge(parent, matched *node) []*node {
	parentEdges := make([]*node, len(parent.children)-1)
	added := 0
	for i := 0; i < len(parent.children); i++ {
		n := parent.children[i]
		if n != matched {
			parentEdges[added] = n
			added++
		}
	}
	return parentEdges
}

func getRouteConflict(n *node) []string {
	routes := make([]string, 0)
	it := newRawIterator(n)
	for it.hasNext() {
		routes = append(routes, it.current.route.pattern)
	}
	return routes
}

func isRemovable(method string) bool {
	for _, verb := range commonVerbs {
		if verb == method {
			return false
		}
	}
	return true
}

func (t *iTree) allocateContext() *cTx {
	params := make(Params, 0, t.maxParams)
	tsrParams := make(Params, 0, t.maxParams)
	skipNds := make(skippedNodes, 0, t.maxDepth)
	return &cTx{
		params:    &params,
		skipNds:   &skipNds,
		tsrParams: &tsrParams,
		// This is a read only value, no reset. It's always the
		// owner of the pool.
		tree: t,
		// This is a read only value, no reset
		fox: t.fox,
	}
}

type resultType int

const (
	exactMatch resultType = iota
	incompleteMatchToEndOfEdge
	incompleteMatchToMiddleOfEdge
	keyEndMidEdge
)

type searchResult struct {
	matched                 *node
	p                       *node
	pp                      *node
	ppp                     *node
	path                    string
	visited                 []*node
	charsMatched            int
	charsMatchedInNodeFound int
	depth                   uint32
}

func (r searchResult) classify() resultType {
	if r.charsMatched == len(r.path) {
		if r.charsMatchedInNodeFound == len(r.matched.key) {
			return exactMatch
		}
		if r.charsMatchedInNodeFound < len(r.matched.key) {
			return keyEndMidEdge
		}
	} else if r.charsMatched < len(r.path) {
		// When the node matched is a root node, charsMatched & charsMatchedInNodeFound are both equals to 0, but the value of
		// the key is the http verb instead of a segment of the path and therefore len(r.matched.key) > 0 instead of empty (0).
		if r.charsMatchedInNodeFound == len(r.matched.key) || r.p == nil {
			return incompleteMatchToEndOfEdge
		}
		if r.charsMatchedInNodeFound < len(r.matched.key) {
			return incompleteMatchToMiddleOfEdge
		}
	}
	panic("internal error: cannot classify the result")
}

func (r searchResult) isExactMatch() bool {
	return r.charsMatched == len(r.path) && r.charsMatchedInNodeFound == len(r.matched.key)
}
