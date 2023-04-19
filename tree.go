// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Tree implements a Concurrent Radix Tree that supports lock-free reads while allowing concurrent writes.
// The caller is responsible for ensuring that all writes are run serially.
//
// IMPORTANT:
// Each tree as its own sync.Mutex that may be used to serialize write. Since the router tree may be swapped at any
// given time, you MUST always copy the pointer locally to avoid inadvertently causing a deadlock by locking/unlocking
// the wrong Tree.
//
// Good:
// t := r.Tree()
// t.Lock()
// defer t.Unlock()
//
// Dramatically bad, may cause deadlock
// r.Tree().Lock()
// defer r.Tree().Unlock()
type Tree struct {
	ctx   sync.Pool
	nodes atomic.Pointer[[]*node]
	mws   []middleware
	sync.Mutex
	maxParams atomic.Uint32
	maxDepth  atomic.Uint32
}

// Handle registers a new handler for the given method and path. This function return an error if the route
// is already registered or conflict with another. It's perfectly safe to add a new handler while the tree is in use
// for serving requests. However, this function is NOT thread-safe and should be run serially, along with all other
// Tree APIs that perform write operations. To override an existing route, use Update.
func (t *Tree) Handle(method, path string, handler HandlerFunc) error {
	p, catchAllKey, n, err := parseRoute(path)
	if err != nil {
		return err
	}

	return t.insert(method, p, catchAllKey, uint32(n), applyMiddleware(RouteHandlers, t.mws, handler))
}

// Update override an existing handler for the given method and path. If the route does not exist,
// the function return an ErrRouteNotFound. It's perfectly safe to update a handler while the tree is in use for
// serving requests. However, this function is NOT thread-safe and should be run serially, along with all other
// Tree APIs that perform write operations. To add new handler, use Handle method.
func (t *Tree) Update(method, path string, handler HandlerFunc) error {
	p, catchAllKey, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	return t.update(method, p, catchAllKey, applyMiddleware(RouteHandlers, t.mws, handler))
}

// Remove delete an existing handler for the given method and path. If the route does not exist, the function
// return an ErrRouteNotFound. It's perfectly safe to remove a handler while the tree is in use for serving requests.
// However, this function is NOT thread-safe and should be run serially, along with all other Tree APIs that perform
// write operations.
func (t *Tree) Remove(method, path string) error {
	path, _, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	if !t.remove(method, path) {
		return fmt.Errorf("%w: route [%s] %s is not registered", ErrRouteNotFound, method, path)
	}

	return nil
}

// Methods returns a sorted slice of HTTP methods that are currently in use to route requests.
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Methods() []string {
	var methods []string
	nds := *t.nodes.Load()
	for i := range nds {
		if len(nds[i].children) > 0 {
			if methods == nil {
				methods = make([]string, 0)
			}
			methods = append(methods, nds[i].key)
		}
	}
	sort.Strings(methods)
	return methods
}

// Lookup allow to do manual lookup of a route for the given request and return the matched HandlerFunc along with a
// ContextCloser and trailing slash redirect recommendation. You should always close the ContextCloser if NOT nil by
// calling cc.Close(). Note that the returned ContextCloser does not have a router attached (use the SetFox method).
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Lookup(w http.ResponseWriter, r *http.Request) (handler HandlerFunc, cc ContextCloser, tsr bool) {
	nds := *t.nodes.Load()
	index := findRootNode(r.Method, nds)
	if index < 0 {
		return
	}

	path := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		path = r.URL.RawPath
	}

	c := t.ctx.Get().(*context)
	c.Reset(nil, w, r)
	n, tsr := t.lookup(nds[index], path, c.params, c.skipNds, false)
	if n != nil {
		c.path = n.path
		return n.handler, c, tsr
	}
	return nil, c, tsr
}

// LookupPath allow to do manual lookup of a route for the given method and path and return the matched HandlerFunc
// along with a ContextCloser and trailing slash redirect recommendation. If lazy is set to true, wildcard parameter are
// not parsed. You should always close the ContextCloser if NOT nil by calling cc.Close(). Note that the returned
// ContextCloser does not have a router, request and response writer attached (use the Reset method).
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) LookupPath(method, path string, lazy bool) (handler HandlerFunc, cc ContextCloser, tsr bool) {
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return
	}

	c := t.ctx.Get().(*context)
	c.resetNil()
	n, tsr := t.lookup(nds[index], path, c.params, c.skipNds, lazy)
	if n != nil {
		c.path = n.path
		return n.handler, c, tsr
	}
	return nil, c, tsr
}

// Has allows to check if the given method and path exactly match a registered route. This function is safe for
// concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Has(method, path string) bool {
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return false
	}

	c := t.ctx.Get().(*context)
	c.resetNil()
	n, _ := t.lookup(nds[index], path, c.params, c.skipNds, true)
	c.Close()
	return n != nil && n.path == path
}

// Insert is not safe for concurrent use. The path must start by '/' and it's not validated. Use
// parseRoute before.
func (t *Tree) insert(method, path, catchAllKey string, paramsN uint32, handler HandlerFunc) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	if method == "" {
		return fmt.Errorf("%w: http method is missing", ErrInvalidRoute)
	}

	var rootNode *node
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		rootNode = &node{key: method}
		t.addRoot(rootNode)
	} else {
		rootNode = nds[index]
	}

	isCatchAll := catchAllKey != ""

	result := t.search(rootNode, path)
	switch result.classify() {
	case exactMatch:
		// e.g. matched exactly "te" node when inserting "te" key.
		// te
		// ├── st
		// └── am
		// Create a new node from "st" reference and update the "te" (parent) reference to "st" node.
		if result.matched.isLeaf() {
			if result.matched.isCatchAll() && isCatchAll {
				return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
			}
			return fmt.Errorf("%w: new route [%s] %s conflict with %s", ErrRouteExist, method, appendCatchAll(path, catchAllKey), result.matched.path)
		}

		// The matched node can only be the result of a previous split and therefore has children.
		if isCatchAll && result.matched.paramChildIndex >= 0 {
			return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
		}
		// We are updating an existing node. We only need to create a new node from
		// the matched one with the updated/added value (handler and wildcard).
		n := newNodeFromRef(result.matched.key, handler, result.matched.children, result.matched.childKeys, catchAllKey, result.matched.paramChildIndex, path)

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

		keyCharsFromStartOfNodeFound := path[result.charsMatched-result.charsMatchedInNodeFound:]
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.key)
		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)

		if strings.HasPrefix(suffixFromExistingEdge, "{") && isCatchAll {
			return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
		}

		child := newNodeFromRef(
			suffixFromExistingEdge,
			result.matched.handler,
			result.matched.children,
			result.matched.childKeys,
			result.matched.catchAllKey,
			result.matched.paramChildIndex,
			result.matched.path,
		)

		parent := newNode(
			cPrefix,
			handler,
			[]*node{child},
			catchAllKey,
			path,
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

		if strings.HasPrefix(keySuffix, "{") && result.matched.isCatchAll() {
			return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
		}

		// No children, so no paramChild
		child := newNode(keySuffix, handler, nil, catchAllKey, path)
		edges := result.matched.getEdgesShallowCopy()
		edges = append(edges, child)
		n := newNode(
			result.matched.key,
			result.matched.handler,
			edges,
			result.matched.catchAllKey,
			result.matched.path,
		)

		t.updateMaxDepth(result.depth + 1)
		t.updateMaxParams(paramsN)

		if result.matched == rootNode {
			n.key = method
			n.paramChildIndex = -1
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

		// Rule: a node with {param} has no child or has a separator before the end of the key
		for i := len(cPrefix) - 1; i >= 0; i-- {
			if cPrefix[i] == '/' {
				break
			}
			if cPrefix[i] == '{' {
				return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
			}
		}

		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)
		keySuffix := path[result.charsMatched:]

		// No children, so no paramChild
		n1 := newNodeFromRef(keySuffix, handler, nil, nil, catchAllKey, -1, path) // inserted node
		n2 := newNodeFromRef(
			suffixFromExistingEdge,
			result.matched.handler,
			result.matched.children,
			result.matched.childKeys,
			result.matched.catchAllKey,
			result.matched.paramChildIndex,
			result.matched.path,
		) // previous matched node

		// n3 children never start with a param
		n3 := newNode(cPrefix, nil, []*node{n1, n2}, "", "") // intermediary node

		t.updateMaxDepth(result.depth + 1)
		t.updateMaxParams(paramsN)
		result.p.updateEdge(n3)
	default:
		// safeguard against introducing a new result type
		panic("internal error: unexpected result type")
	}
	return nil
}

// update is not safe for concurrent use.
func (t *Tree) update(method string, path, catchAllKey string, handler HandlerFunc) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return fmt.Errorf("%w: route [%s] %s is not registered", ErrRouteNotFound, method, path)
	}

	result := t.search(nds[index], path)
	if !result.isExactMatch() || !result.matched.isLeaf() {
		return fmt.Errorf("%w: route [%s] %s is not registered", ErrRouteNotFound, method, path)
	}

	if catchAllKey != result.matched.catchAllKey {
		err := newConflictErr(method, path, catchAllKey, []string{result.matched.path})
		err.isUpdate = true
		return err
	}

	// We are updating an existing node (could be a leaf or not). We only need to create a new node from
	// the matched one with the updated/added value (handler and wildcard).
	n := newNodeFromRef(
		result.matched.key,
		handler,
		result.matched.children,
		result.matched.childKeys,
		catchAllKey,
		result.matched.paramChildIndex,
		path,
	)
	result.p.updateEdge(n)
	return nil
}

// remove is not safe for concurrent use.
func (t *Tree) remove(method, path string) bool {
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return false
	}

	result := t.search(nds[index], path)
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
			"",
			result.matched.paramChildIndex,
			"",
		)
		result.p.updateEdge(n)
		return true
	}

	if len(result.matched.children) == 1 {
		child := result.matched.get(0)
		mergedPath := fmt.Sprintf("%s%s", result.matched.key, child.key)
		n := newNodeFromRef(
			mergedPath,
			child.handler,
			child.children,
			child.childKeys,
			child.catchAllKey,
			child.paramChildIndex,
			child.path,
		)
		result.p.updateEdge(n)
		return true
	}

	// recreate the parent edges without the removed node
	parentEdges := make([]*node, len(result.p.children)-1)
	added := 0
	for i := 0; i < len(result.p.children); i++ {
		n := result.p.get(i)
		if n != result.matched {
			parentEdges[added] = n
			added++
		}
	}

	parentIsRoot := result.p == nds[index]
	var parent *node
	if len(parentEdges) == 1 && !result.p.isLeaf() && !parentIsRoot {
		child := parentEdges[0]
		mergedPath := fmt.Sprintf("%s%s", result.p.key, child.key)
		parent = newNodeFromRef(
			mergedPath,
			child.handler,
			child.children,
			child.childKeys,
			child.catchAllKey,
			child.paramChildIndex,
			child.path,
		)
	} else {
		parent = newNode(
			result.p.key,
			result.p.handler,
			parentEdges,
			result.p.catchAllKey,
			result.p.path,
		)
	}

	if parentIsRoot {
		if len(parent.children) == 0 && isRemovable(method) {
			return t.removeRoot(method)
		}
		parent.key = method
		parent.paramChildIndex = -1
		t.updateRoot(parent)
		return true
	}

	result.pp.updateEdge(parent)
	return true
}

const (
	slashDelim   = '/'
	bracketDelim = '{'
)

func (t *Tree) lookup(rootNode *node, path string, params *Params, skipNds *skippedNodes, lazy bool) (n *node, tsr bool) {
	if len(rootNode.children) == 0 {
		return nil, false
	}

	var (
		charsMatched            int
		charsMatchedInNodeFound int
		paramCnt                uint32
	)

	current := rootNode.children[0].Load()
	*skipNds = (*skipNds)[:0]

Walk:
	for charsMatched < len(path) {
		charsMatchedInNodeFound = 0
		for i := 0; charsMatched < len(path); i++ {
			if i >= len(current.key) {
				break
			}

			if current.key[i] != path[charsMatched] || path[charsMatched] == bracketDelim {
				if current.key[i] == '{' {
					startPath := charsMatched
					idx := strings.IndexByte(path[charsMatched:], slashDelim)
					if idx > 0 {
						// There is another path segment (e.g. /foo/{bar}/baz)
						charsMatched += idx
					} else if idx < 0 {
						// This is the end of the path (e.g. /foo/{bar})
						charsMatched += len(path[charsMatched:])
					} else {
						// segment is empty
						break Walk
					}

					startKey := charsMatchedInNodeFound
					idx = strings.IndexByte(current.key[startKey:], slashDelim)
					if idx >= 0 {
						// -1 since on the next incrementation, if any, 'i' are going to be incremented
						i += idx - 1
						charsMatchedInNodeFound += idx
					} else {
						// -1 since on the next incrementation, if any, 'i' are going to be incremented
						i += len(current.key[charsMatchedInNodeFound:]) - 1
						charsMatchedInNodeFound += len(current.key[charsMatchedInNodeFound:])
					}

					if !lazy {
						paramCnt++
						*params = append(*params, Param{Key: current.key[startKey+1 : charsMatchedInNodeFound-1], Value: path[startPath:charsMatched]})
					}

					continue
				}

				break Walk
			}

			charsMatched++
			charsMatchedInNodeFound++
		}

		if charsMatched < len(path) {
			// linear search
			idx := -1
			for i := 0; i < len(current.childKeys); i++ {
				if current.childKeys[i] == path[charsMatched] {
					idx = i
					break
				}
			}

			if idx < 0 {
				if current.paramChildIndex < 0 {
					break
				}
				// child param: go deeper and since the child param is evaluated
				// now, no need to backtrack later.
				idx = current.paramChildIndex
				current = current.children[idx].Load()
				continue
			}

			if current.paramChildIndex >= 0 || current.isCatchAll() {
				*skipNds = append(*skipNds, skippedNode{current, charsMatched})
				paramCnt = 0
			}
			current = current.children[idx].Load()
		}
	}

	hasSkpNds := len(*skipNds) > 0

	if !current.isLeaf() {
		if hasSkpNds {
			goto Backtrack
		}

		return nil, false
	}

	if charsMatched == len(path) {
		if charsMatchedInNodeFound == len(current.key) {
			// Exact match, note that if we match a catch all node
			if !lazy && current.isCatchAll() {
				*params = append(*params, Param{Key: current.catchAllKey, Value: path[charsMatched:]})
				return current, false
			}

			return current, false
		}
		if charsMatchedInNodeFound < len(current.key) {
			// Key end mid-edge
			if !tsr {
				if strings.HasSuffix(path, "/") {
					// Tsr recommendation: remove the extra trailing slash (got an exact match)
					remainingPrefix := current.key[:charsMatchedInNodeFound]
					tsr = len(remainingPrefix) == 1 && remainingPrefix[0] == slashDelim
				} else {
					// Tsr recommendation: add an extra trailing slash (got an exact match)
					remainingSuffix := current.key[charsMatchedInNodeFound:]
					tsr = len(remainingSuffix) == 1 && remainingSuffix[0] == slashDelim
				}
			}

			if hasSkpNds {
				goto Backtrack
			}

			return nil, tsr
		}
	}

	// Incomplete match to end of edge
	if charsMatched < len(path) && charsMatchedInNodeFound == len(current.key) {
		if current.isCatchAll() {
			if !lazy {
				*params = append(*params, Param{Key: current.catchAllKey, Value: path[charsMatched:]})
				return current, false
			}
			// Same as exact match, no tsr recommendation
			return current, false
		}

		// Tsr recommendation: remove the extra trailing slash (got an exact match)
		if !tsr {
			remainingKeySuffix := path[charsMatched:]
			tsr = len(remainingKeySuffix) == 1 && remainingKeySuffix[0] == slashDelim
		}

		if hasSkpNds {
			goto Backtrack
		}

		return nil, tsr
	}

	// Finally incomplete match to middle of edge
Backtrack:
	if hasSkpNds {
		skipped := skipNds.pop()
		if skipped.node.paramChildIndex < 0 {
			// skipped is catch all
			current = skipped.node
			*params = (*params)[:len(*params)-int(paramCnt)]

			if !lazy {
				*params = append(*params, Param{Key: current.catchAllKey, Value: path[skipped.pathIndex:]})

				return current, false
			}

			return current, false
		}

		current = skipped.node.children[skipped.node.paramChildIndex].Load()

		*params = (*params)[:len(*params)-int(paramCnt)]
		charsMatched = skipped.pathIndex
		paramCnt = 0
		goto Walk
	}

	return nil, tsr
}

func (t *Tree) search(rootNode *node, path string) searchResult {
	current := rootNode

	var (
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
		depth:                   depth,
	}
}

func (t *Tree) allocateContext() *context {
	params := make(Params, 0, t.maxParams.Load())
	skipNds := make(skippedNodes, 0, t.maxDepth.Load())
	return &context{
		params:  &params,
		skipNds: &skipNds,
		// This is a read only value, no reset, it's always the
		// owner of the pool.
		tree: t,
	}
}

// addRoot append a new root node to the tree.
// Note: This function should be guarded by mutex.
func (t *Tree) addRoot(n *node) {
	nds := *t.nodes.Load()
	newNds := make([]*node, 0, len(nds)+1)
	newNds = append(newNds, nds...)
	newNds = append(newNds, n)
	t.nodes.Store(&newNds)
}

// updateRoot replaces a root node in the tree.
// Due to performance optimization, the tree uses atomic.Pointer[[]*node] instead of
// atomic.Pointer[atomic.Pointer[*node]]. As a result, the root node cannot be replaced
// directly by swapping the pointer. Instead, a new list of nodes is created with the
// updated root node, and the entire list is swapped afterwards.
// Note: This function should be guarded by mutex.
func (t *Tree) updateRoot(n *node) bool {
	nds := *t.nodes.Load()
	// for root node, the key contains the HTTP verb.
	index := findRootNode(n.key, nds)
	if index < 0 {
		return false
	}
	newNds := make([]*node, 0, len(nds))
	newNds = append(newNds, nds[:index]...)
	newNds = append(newNds, n)
	newNds = append(newNds, nds[index+1:]...)
	t.nodes.Store(&newNds)
	return true
}

// removeRoot remove a root nod from the tree.
// Note: This function should be guarded by mutex.
func (t *Tree) removeRoot(method string) bool {
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return false
	}
	newNds := make([]*node, 0, len(nds)-1)
	newNds = append(newNds, nds[:index]...)
	newNds = append(newNds, nds[index+1:]...)
	t.nodes.Store(&newNds)
	return true
}

// updateMaxParams perform an update only if max is greater than the current
// max params. This function should be guarded by mutex.
func (t *Tree) updateMaxParams(max uint32) {
	if max > t.maxParams.Load() {
		t.maxParams.Store(max)
	}
}

// updateMaxDepth perform an update only if max is greater than the current
// max depth. This function should be guarded my mutex.
func (t *Tree) updateMaxDepth(max uint32) {
	if max > t.maxDepth.Load() {
		t.maxDepth.Store(max)
	}
}
