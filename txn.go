package fox

import (
	"fmt"
	"github.com/tigerwill90/fox/internal/simplelru"
	"strings"
	"sync/atomic"
)

const defaultModifiedCache = 8192

type Txn struct {
	root      atomic.Pointer[[]*node]
	tree      *Tree
	writable  *simplelru.LRU[*node, any]
	maxParams uint32
	maxDepth  uint32
}

func (t *Tree) Txn() *Txn {
	txn := &Txn{
		maxParams: t.maxParams.Load(),
		maxDepth:  t.maxDepth.Load(),
		tree:      t,
	}
	txn.root.Store(t.nodes.Load())
	return txn
}

// Handle registers a new handler for the given method and route pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrRouteConflict]: If the route conflicts with another.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to add a new handler while the tree is in use for serving requests. However, this function is NOT
// thread-safe and should be run serially, along with all other [Tree] APIs that perform write operations.
// To override an existing route, use [Tree.Update].
func (txn *Txn) Handle(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if matched := regEnLetter.MatchString(method); !matched {
		return nil, fmt.Errorf("%w: missing or invalid http method", ErrInvalidRoute)
	}

	n, err := parseRoute(pattern)
	if err != nil {
		return nil, err
	}

	rte := txn.tree.newRoute(pattern, handler, opts...)

	if err = txn.insert(method, rte, n); err != nil {
		return nil, err
	}
	return rte, nil
}

// Update override an existing handler for the given method and route pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to update a handler while the tree is in use for serving requests. However, this function is NOT thread-safe
// and should be run serially, along with all other [Tree] APIs that perform write operations. To add a new handler,
// use [Tree.Handle] method.
func (txn *Txn) Update(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if method == "" {
		return nil, fmt.Errorf("%w: missing http method", ErrInvalidRoute)
	}

	_, err := parseRoute(pattern)
	if err != nil {
		return nil, err
	}

	rte := txn.tree.newRoute(pattern, handler, opts...)
	if err = txn.update(method, rte); err != nil {
		return nil, err
	}

	return rte, nil
}

// Delete deletes an existing handler for the given method and router pattern. If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to delete a handler while the tree is in use for serving requests. However, this function is NOT
// thread-safe and should be run serially, along with all other [Tree] APIs that perform write operations.
func (txn *Txn) Delete(method, pattern string) error {
	if method == "" {
		return fmt.Errorf("%w: missing http method", ErrInvalidRoute)
	}

	_, err := parseRoute(pattern)
	if err != nil {
		return err
	}

	if !txn.remove(method, pattern) {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, pattern)
	}

	return nil
}

// TODO do not copy this
func (txn *Txn) search(rootNode *node, path string) searchResult {
	current := rootNode

	var (
		visited                 []*node
		ppp                     *node
		pp                      *node
		p                       *node
		charsMatched            int
		charsMatchedInNodeFound int
		depth                   uint32
	)

	visited = make([]*node, 0, min(15, txn.maxDepth))

STOP:
	for charsMatched < len(path) {
		next := current.getEdge(path[charsMatched])
		if next == nil {
			break STOP
		}

		depth++
		if ppp != nil {
			visited = append(visited, ppp)
		}
		ppp = pp
		pp = p
		p = current
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
		visited:                 visited,
		depth:                   depth,
	}
}

func (txn *Txn) insert(method string, route *Route, paramsN uint32) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	if !txn.tree.race.CompareAndSwap(0, 1) {
		panic(ErrConcurrentAccess)
	}
	defer txn.tree.race.Store(0)

	if txn.writable == nil {
		lru, err := simplelru.NewLRU[*node, any](defaultModifiedCache, nil)
		if err != nil {
			panic(err)
		}
		txn.writable = lru
	}

	var rootNode *node
	nds := *txn.root.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		rootNode = &node{
			key:                method,
			paramChildIndex:    -1,
			wildcardChildIndex: -1,
		}
		txn.addRoot(rootNode)
	} else {
		rootNode = nds[index]
	}

	path := route.pattern

	result := txn.search(rootNode, path)
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

		txn.updateMaxParams(paramsN)
		txn.updateToRoot(result.p, result.pp, result.ppp, result.visited, n)
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

		txn.updateMaxParams(paramsN)
		txn.updateMaxDepth(result.depth + 1)
		txn.updateToRoot(result.p, result.pp, result.ppp, result.visited, parent)
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
		idx := strings.IndexByte(path, '/')
		if idx > 0 && result.charsMatched < idx {
			host, p := keySuffix[:idx-result.charsMatched], keySuffix[idx-result.charsMatched:]
			pathChild := newNode(p, route, nil)
			child = newNode(host, nil, []*node{pathChild})
			addDepth++
		} else {
			// No children, so no paramChild
			child = newNode(keySuffix, route, nil)
		}

		edges := result.matched.getEdgesShallowCopy()
		edges = append(edges, child)
		n := newNode(
			result.matched.key,
			result.matched.route,
			edges,
		)

		txn.updateMaxDepth(result.depth + addDepth)
		txn.updateMaxParams(paramsN)

		if result.matched == rootNode {
			n.key = method
			txn.writable.Add(n, nil)
			txn.updateRoot(n)
			break
		}
		txn.updateToRoot(result.p, result.pp, result.ppp, result.visited, n)
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

		idx := strings.IndexByte(path, '/')
		keyCharsFromStartOfNodeFound := path[result.charsMatched-result.charsMatchedInNodeFound:]
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.key)
		isHostname := result.charsMatched <= idx
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
		if idx > 0 && result.charsMatched < idx {
			host, p := keySuffix[:idx-result.charsMatched], keySuffix[idx-result.charsMatched:]
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

		txn.updateMaxDepth(result.depth + addDepth)
		txn.updateMaxParams(paramsN)
		txn.updateToRoot(result.p, result.pp, result.ppp, result.visited, n3)
	default:
		// safeguard against introducing a new result type
		panic("internal error: unexpected result type")
	}
	return nil
}

// update is not safe for concurrent use.
func (txn *Txn) update(method string, route *Route) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	if !txn.tree.race.CompareAndSwap(0, 1) {
		panic(ErrConcurrentAccess)
	}
	defer txn.tree.race.Store(0)

	if txn.writable == nil {
		lru, err := simplelru.NewLRU[*node, any](defaultModifiedCache, nil)
		if err != nil {
			panic(err)
		}
		txn.writable = lru
	}

	path := route.pattern

	nds := *txn.root.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, path)
	}

	result := txn.search(nds[index], path)
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
	txn.updateToRoot(result.p, result.pp, result.ppp, result.visited, n)
	return nil
}

func (txn *Txn) remove(method, path string) bool {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	if !txn.tree.race.CompareAndSwap(0, 1) {
		panic(ErrConcurrentAccess)
	}
	defer txn.tree.race.Store(0)

	if txn.writable == nil {
		lru, err := simplelru.NewLRU[*node, any](defaultModifiedCache, nil)
		if err != nil {
			panic(err)
		}
		txn.writable = lru
	}

	nds := *txn.root.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return false
	}

	result := txn.search(nds[index], path)
	if result.classify() != exactMatch {
		return false
	}

	// This node was created after a split (KEY_END_MID_EGGE operation), therefore we cannot delete
	// this node.
	if !result.matched.isLeaf() {
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
		txn.updateToRoot(result.p, result.pp, result.ppp, result.visited, n)
		// result.p.updateEdge(n)
		return true
	}

	if len(result.matched.children) == 1 {
		child := result.matched.get(0)
		mergedPath := fmt.Sprintf("%s%s", result.matched.key, child.key)
		n := newNodeFromRef(
			mergedPath,
			child.route,
			child.children,
			child.childKeys,
			child.paramChildIndex,
			child.wildcardChildIndex,
		)
		txn.updateToRoot(result.p, result.pp, result.ppp, result.visited, n)
		// result.p.updateEdge(n)
		return true
	}

	// recreate the parent edges without the removed node
	parentEdges := recreateParentEdge(result.p, result.matched)
	parentIsRoot := result.p == nds[index]

	// The parent was the result of a previous hostname/path split, so we have at least depth 3,
	// where p can not be root, but pp and ppp may.
	if len(parentEdges) == 0 && !result.p.isLeaf() && !parentIsRoot {
		parentEdges = recreateParentEdge(result.pp, result.p)
		var parent *node
		parentParentIsRoot := result.pp == nds[index]
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
				return txn.removeRoot(method)
			}
			parent.key = method
			txn.writable.Add(parent, nil)
			txn.updateRoot(parent)
			return true
		}

		txn.updateToRoot(nil, nil, result.ppp, result.visited, parent)
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
			return txn.removeRoot(method)
		}
		parent.key = method
		txn.updateRoot(parent)
		return true
	}

	txn.updateToRoot(nil, result.pp, result.ppp, result.visited, parent)
	return true
}

func (txn *Txn) updateToRoot(p, pp, ppp *node, visited []*node, n *node) {
	nn := n
	if p != nil {
		np := p
		if _, ok := txn.writable.Get(np); !ok {
			np = np.clone()
			txn.writable.Add(np, nil)
			// np is root and has never been writen
			if pp == nil {
				np.updateEdge(n)
				txn.updateRoot(np)
				return
			}
		}

		// If it's a clone, it's not a root
		np.updateEdge(n)
		if pp == nil {
			return
		}
		nn = np
	}

	if pp != nil {
		npp := pp
		if _, ok := txn.writable.Get(npp); !ok {
			npp = npp.clone()
			txn.writable.Add(npp, nil)
			// npp is root and has never been writen
			if ppp == nil {
				npp.updateEdge(nn)
				txn.updateRoot(npp)
				return
			}
		}

		// If it's a clone, it's not a root
		npp.updateEdge(nn)
		if ppp == nil {
			return
		}
		nn = npp
	}

	nppp := ppp
	if _, ok := txn.writable.Get(nppp); !ok {
		nppp = nppp.clone()
		txn.writable.Add(nppp, nil)
		// nppp is root and has never been writen
		if len(visited) == 0 {
			nppp.updateEdge(nn)
			txn.updateRoot(nppp)
			return
		}
	}

	// If it's a clone, it's not a root
	nppp.updateEdge(nn)
	if len(visited) == 0 {
		return
	}

	// Process remaining visited nodes in reverse order
	current := nppp
	for i := len(visited) - 1; i >= 0; i-- {
		vNode := visited[i]

		if _, ok := txn.writable.Get(vNode); !ok {
			vNode = vNode.clone()
			txn.writable.Add(vNode, nil)
		}

		vNode.updateEdge(current)
		current = vNode
	}

	if current != visited[0] {
		// root is a clone
		txn.updateRoot(current)
	}
}

func (txn *Txn) commit() {
	txn.tree.maxParams.Store(txn.maxParams)
	txn.tree.maxDepth.Store(txn.maxDepth)
	txn.tree.nodes.Swap(txn.root.Load())
}

// addRoot append a new root node to the tree.
// Note: This function should be guarded by mutex.
func (txn *Txn) addRoot(n *node) {
	nds := *txn.root.Load()
	newNds := make([]*node, 0, len(nds)+1)
	newNds = append(newNds, nds...)
	newNds = append(newNds, n)
	txn.root.Store(&newNds)
}

func (txn *Txn) updateRoot(n *node) bool {
	nds := *txn.root.Load()
	// for root node, the key contains the HTTP verb.
	index := findRootNode(n.key, nds)
	if index < 0 {
		return false
	}
	newNds := make([]*node, 0, len(nds))
	newNds = append(newNds, nds[:index]...)
	newNds = append(newNds, n)
	newNds = append(newNds, nds[index+1:]...)
	txn.root.Store(&newNds)
	return true
}

// removeRoot remove a root nod from the tree.
// Note: This function should be guarded by mutex.
func (txn *Txn) removeRoot(method string) bool {
	nds := *txn.root.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return false
	}
	newNds := make([]*node, 0, len(nds)-1)
	newNds = append(newNds, nds[:index]...)
	newNds = append(newNds, nds[index+1:]...)
	txn.root.Store(&newNds)
	return true
}

// updateMaxParams perform an update only if max is greater than the current
// max params. This function should be guarded by mutex.
func (txn *Txn) updateMaxParams(max uint32) {
	if max > txn.maxParams {
		txn.maxParams = max
	}
}

// updateMaxDepth perform an update only if max is greater than the current
// max depth. This function should be guarded my mutex.
func (txn *Txn) updateMaxDepth(max uint32) {
	if max > txn.maxDepth {
		txn.maxDepth = max
	}
}
