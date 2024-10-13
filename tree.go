// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"net/http"
	"slices"
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
// t := fox.Tree()
// t.Lock()
// defer t.Unlock()
//
// Dramatically bad, may cause deadlock
// fox.Tree().Lock()
// defer fox.Tree().Unlock()
type Tree struct {
	ctx   sync.Pool
	nodes atomic.Pointer[[]*node]
	fox   *Router
	mws   []middleware
	sync.Mutex
	maxParams atomic.Uint32
	maxDepth  atomic.Uint32
}

// Handle registers a new handler for the given method and path. This function return an error if the route
// is already registered or conflict with another. It's perfectly safe to add a new handler while the tree is in use
// for serving requests. However, this function is NOT thread-safe and should be run serially, along with all other
// Tree APIs that perform write operations. To override an existing route, use Update.
func (t *Tree) Handle(method, path string, handler HandlerFunc, opts ...PathOption) error {
	if handler == nil {
		return fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if matched := regEnLetter.MatchString(method); !matched {
		return fmt.Errorf("%w: missing or invalid http method", ErrInvalidRoute)
	}

	p, catchAllKey, n, err := parseRoute(path)
	if err != nil {
		return err
	}

	// nolint:gosec
	return t.insert(method, p, catchAllKey, uint32(n), t.newRoute(path, handler, opts...))
}

// Update override an existing handler for the given method and path. If the route does not exist,
// the function return an ErrRouteNotFound. It's perfectly safe to update a handler while the tree is in use for
// serving requests. However, this function is NOT thread-safe and should be run serially, along with all other
// Tree APIs that perform write operations. To add a new handler, use Handle method.
func (t *Tree) Update(method, path string, handler HandlerFunc, opts ...PathOption) error {
	if handler == nil {
		return fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if method == "" {
		return fmt.Errorf("%w: missing http method", ErrInvalidRoute)
	}

	p, catchAllKey, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	return t.update(method, p, catchAllKey, t.newRoute(path, handler, opts...))
}

// Remove delete an existing handler for the given method and path. If the route does not exist, the function
// return an ErrRouteNotFound. It's perfectly safe to remove a handler while the tree is in use for serving requests.
// However, this function is NOT thread-safe and should be run serially, along with all other Tree APIs that perform
// write operations.
func (t *Tree) Remove(method, path string) error {
	if method == "" {
		return fmt.Errorf("%w: missing http method", ErrInvalidRoute)
	}

	path, _, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	if !t.remove(method, path) {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, path)
	}

	return nil
}

// Has allows to check if the given method and path exactly match a registered route. This function is safe for
// concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Has(method, path string) bool {
	return t.Route(method, path) != nil
}

// Route performs a lookup for a registered route matching the given method and path. It returns the route if a
// match is found or nil otherwise. This function is safe for concurrent use by multiple goroutine and while
// mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Route(method, path string) *Route {
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return nil
	}

	c := t.ctx.Get().(*cTx)
	c.resetNil()
	n, tsr := t.lookup(nds[index], path, c, true)
	c.Close()
	if n != nil && !tsr && n.route.path == path {
		return n.route
	}
	return nil
}

// Reverse perform a reverse lookup on the tree for the given method and path and return the matching registered route
// (if any) along with a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). This function is safe for concurrent use by multiple goroutine and while
// mutation on Tree are ongoing. See also Tree.Lookup as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Reverse(method, path string) (route *Route, tsr bool) {
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return nil, false
	}

	c := t.ctx.Get().(*cTx)
	c.resetNil()
	n, tsr := t.lookup(nds[index], path, c, true)
	c.Close()
	if n != nil {
		return n.route, tsr
	}
	return nil, false
}

// Lookup performs a manual route lookup for a given http.Request, returning the matched Route along with a
// ContextCloser, and a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). The ContextCloser should always be closed if non-nil. This method is primarily
// intended for integrating the fox router into custom routing solutions or middleware. This function is safe for concurrent
// use by multiple goroutine and while mutation on Tree are ongoing. If there is a direct match or a tsr is possible,
// Lookup always return a Route and a ContextCloser.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Lookup(w ResponseWriter, r *http.Request) (route *Route, cc ContextCloser, tsr bool) {
	nds := *t.nodes.Load()
	index := findRootNode(r.Method, nds)

	if index < 0 {
		return
	}

	c := t.ctx.Get().(*cTx)
	c.Reset(w, r)

	target := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		target = r.URL.RawPath
	}

	n, tsr := t.lookup(nds[index], target, c, false)
	if n != nil {
		c.route = n.route
		c.tsr = tsr
		return n.route, c, tsr
	}
	c.Close()
	return nil, nil, tsr
}

// Iter returns an iterator that provides access to a collection of iterators for traversing the routing tree.
// This function is safe for concurrent use by multiple goroutines and can operate while the Tree is being modified.
// This API is EXPERIMENTAL and may change in future releases.
func (t *Tree) Iter() Iter {
	return Iter{t: t}
}

// Insert is not safe for concurrent use. The path must start by '/' and it's not validated. Use
// parseRoute before.
func (t *Tree) insert(method, path, catchAllKey string, paramsN uint32, route *Route) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
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
			return fmt.Errorf("%w: new route %s %s conflict with %s", ErrRouteExist, method, route.path, result.matched.route.path)
		}

		// We are updating an existing node. We only need to create a new node from
		// the matched one with the updated/added value (handler and wildcard).
		n := newNodeFromRef(result.matched.key, route, result.matched.children, result.matched.childKeys, catchAllKey, result.matched.paramChildIndex)

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

		child := newNodeFromRef(
			suffixFromExistingEdge,
			result.matched.route,
			result.matched.children,
			result.matched.childKeys,
			result.matched.catchAllKey,
			result.matched.paramChildIndex,
		)

		parent := newNode(
			cPrefix,
			route,
			[]*node{child},
			catchAllKey,
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

		// No children, so no paramChild
		child := newNode(keySuffix, route, nil, catchAllKey)
		edges := result.matched.getEdgesShallowCopy()
		edges = append(edges, child)
		n := newNode(
			result.matched.key,
			result.matched.route,
			edges,
			result.matched.catchAllKey,
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
		n1 := newNodeFromRef(keySuffix, route, nil, nil, catchAllKey, -1) // inserted node
		n2 := newNodeFromRef(
			suffixFromExistingEdge,
			result.matched.route,
			result.matched.children,
			result.matched.childKeys,
			result.matched.catchAllKey,
			result.matched.paramChildIndex,
		) // previous matched node

		// n3 children never start with a param
		n3 := newNode(cPrefix, nil, []*node{n1, n2}, "") // intermediary node

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
func (t *Tree) update(method string, path, catchAllKey string, route *Route) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, path)
	}

	result := t.search(nds[index], path)
	if !result.isExactMatch() || !result.matched.isLeaf() {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, path)
	}

	if catchAllKey != result.matched.catchAllKey {
		err := newConflictErr(method, path, catchAllKey, []string{result.matched.route.path})
		err.isUpdate = true
		return err
	}

	// We are updating an existing node (could be a leaf or not). We only need to create a new node from
	// the matched one with the updated/added value (handler and wildcard).
	n := newNodeFromRef(
		result.matched.key,
		route,
		result.matched.children,
		result.matched.childKeys,
		catchAllKey,
		result.matched.paramChildIndex,
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
		)
		result.p.updateEdge(n)
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
			child.catchAllKey,
			child.paramChildIndex,
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
			child.route,
			child.children,
			child.childKeys,
			child.catchAllKey,
			child.paramChildIndex,
		)
	} else {
		parent = newNode(
			result.p.key,
			result.p.route,
			parentEdges,
			result.p.catchAllKey,
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

func (t *Tree) lookup(rootNode *node, path string, c *cTx, lazy bool) (n *node, tsr bool) {
	if len(rootNode.children) == 0 {
		return nil, false
	}

	var (
		charsMatched            int
		charsMatchedInNodeFound int
		paramCnt                uint32
		paramKeyCnt             uint32
		parent                  *node
	)

	current := rootNode.children[0].Load()
	*c.skipNds = (*c.skipNds)[:0]

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

					idx = current.params[paramKeyCnt].end - charsMatchedInNodeFound
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
						*c.params = append(*c.params, Param{Key: current.params[paramKeyCnt].key, Value: path[startPath:charsMatched]})
					}
					paramKeyCnt++
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

			// Only one node which is a child param, load it directly and go deeper
			if idx < 0 {
				if current.paramChildIndex < 0 {
					break
				}

				// The node is also a catch-all, save it as the last fallback.
				if current.catchAllKey != "" {
					*c.skipNds = append(*c.skipNds, skippedNode{current, charsMatched, paramCnt, true})
				}

				idx = current.paramChildIndex
				parent = current
				current = current.children[idx].Load()
				paramKeyCnt = 0
				continue
			}

			// Save the node if we need to evaluate the child param or catch-all later
			if current.paramChildIndex >= 0 || current.catchAllKey != "" {
				*c.skipNds = append(*c.skipNds, skippedNode{current, charsMatched, paramCnt, false})
			}
			parent = current
			current = current.children[idx].Load()
			paramKeyCnt = 0
		}
	}

	paramCnt = 0
	paramKeyCnt = 0
	hasSkpNds := len(*c.skipNds) > 0

	if !current.isLeaf() {

		if !tsr {
			// Tsr recommendation: remove the extra trailing slash (got an exact match)
			// If match the completely /foo/, we end up in an intermediary node which is not a leaf.
			// /foo [leaf=/foo]
			//	  /
			//		b/ [leaf=/foo/b/]
			//		x/ [leaf=/foo/x/]
			// But the parent (/foo) could be a leaf. This is only valid if we have an exact match with
			// the intermediary node (charsMatched == len(path)).
			if strings.HasSuffix(path, "/") && parent != nil && parent.isLeaf() && charsMatched == len(path) {
				tsr = true
				n = parent
				// Save also a copy of the matched params, it should not allocate anything in most case.
				if !lazy {
					if cap(*c.params) > cap(*c.tsrParams) {
						// Grow c.tsrParams to a least cap(c.params)
						*c.tsrParams = slices.Grow(*c.tsrParams, cap(*c.params))
					}
					// cap(c.tsrParams) >= cap(c.params)
					// now constraint into len(c.params) & cap(c.params)
					*c.tsrParams = (*c.tsrParams)[:len(*c.params):cap(*c.params)]
					copy(*c.tsrParams, *c.params)
				}
			}
		}

		if hasSkpNds {
			goto Backtrack
		}

		return n, tsr
	}

	// From here we are always in a leaf
	if charsMatched == len(path) {
		if charsMatchedInNodeFound == len(current.key) {
			// Exact match, note that if we match a catch-all node
			if !lazy && current.catchAllKey != "" {
				*c.params = append(*c.params, Param{Key: current.catchAllKey, Value: path[charsMatched:]})
				// Exact match, tsr is always false
				return current, false
			}
			// Exact match, tsr is always false
			return current, false
		}
		if charsMatchedInNodeFound < len(current.key) {
			// Key end mid-edge
			if !tsr {
				if strings.HasSuffix(path, "/") {
					// Tsr recommendation: remove the extra trailing slash (got an exact match)
					remainingPrefix := current.key[:charsMatchedInNodeFound]
					if len(remainingPrefix) == 1 && remainingPrefix[0] == slashDelim {
						tsr = true
						n = parent
						// Save also a copy of the matched params, it should not allocate anything in most case.
						if !lazy {
							if cap(*c.params) > cap(*c.tsrParams) {
								// Grow c.tsrParams to a least cap(c.params)
								*c.tsrParams = slices.Grow(*c.tsrParams, cap(*c.params))
							}
							// cap(c.tsrParams) >= cap(c.params)
							// now constraint into len(c.params) & cap(c.params)
							*c.tsrParams = (*c.tsrParams)[:len(*c.params):cap(*c.params)]
							copy(*c.tsrParams, *c.params)
						}
					}
				} else {
					// Tsr recommendation: add an extra trailing slash (got an exact match)
					remainingSuffix := current.key[charsMatchedInNodeFound:]
					if len(remainingSuffix) == 1 && remainingSuffix[0] == slashDelim {
						tsr = true
						n = current
						// Save also a copy of the matched params, it should not allocate anything in most case.
						if !lazy {
							if cap(*c.params) > cap(*c.tsrParams) {
								// Grow c.tsrParams to a least cap(c.params)
								*c.tsrParams = slices.Grow(*c.tsrParams, cap(*c.params))
							}
							// cap(c.tsrParams) >= cap(c.params)
							// now constraint into len(c.params) & cap(c.params)
							*c.tsrParams = (*c.tsrParams)[:len(*c.params):cap(*c.params)]
							copy(*c.tsrParams, *c.params)
						}
					}
				}
			}

			if hasSkpNds {
				goto Backtrack
			}

			return n, tsr
		}
	}

	// Incomplete match to end of edge
	if charsMatched < len(path) && charsMatchedInNodeFound == len(current.key) {
		if current.catchAllKey != "" {
			if !lazy {
				*c.params = append(*c.params, Param{Key: current.catchAllKey, Value: path[charsMatched:]})
				// Same as exact match, no tsr recommendation
				return current, false
			}
			// Same as exact match, no tsr recommendation
			return current, false
		}

		// Tsr recommendation: remove the extra trailing slash (got an exact match)
		if !tsr {
			remainingKeySuffix := path[charsMatched:]
			if len(remainingKeySuffix) == 1 && remainingKeySuffix[0] == slashDelim {
				tsr = true
				n = current
				// Save also a copy of the matched params, it should not allocate anything in most case.
				if !lazy {
					if cap(*c.params) > cap(*c.tsrParams) {
						// Grow c.tsrParams to a least cap(c.params)
						*c.tsrParams = slices.Grow(*c.tsrParams, cap(*c.params))
					}
					// cap(c.tsrParams) >= cap(c.params)
					// now constraint into len(c.params) & cap(c.params)
					*c.tsrParams = (*c.tsrParams)[:len(*c.params):cap(*c.params)]
					copy(*c.tsrParams, *c.params)
				}
			}
		}

		if hasSkpNds {
			goto Backtrack
		}

		return n, tsr
	}

	// Finally incomplete match to middle of edge
Backtrack:
	if hasSkpNds {
		skipped := c.skipNds.pop()
		if skipped.n.paramChildIndex < 0 || skipped.seen {
			// skipped is catch all
			current = skipped.n
			*c.params = (*c.params)[:skipped.paramCnt]

			if !lazy {
				*c.params = append(*c.params, Param{Key: current.catchAllKey, Value: path[skipped.pathIndex:]})
				// Same as exact match, no tsr recommendation
				return current, false
			}
			// Same as exact match, no tsr recommendation
			return current, false
		}

		// Could be a catch-all node with child param
		// /foo/*{any}
		// /foo/{bar}
		// In this case we evaluate first the child param node and fall back to the catch-all.
		if skipped.n.catchAllKey != "" {
			*c.skipNds = append(*c.skipNds, skippedNode{skipped.n, skipped.pathIndex, skipped.paramCnt, true})
		}

		parent = skipped.n
		current = skipped.n.children[skipped.n.paramChildIndex].Load()

		*c.params = (*c.params)[:skipped.paramCnt]
		charsMatched = skipped.pathIndex
		goto Walk
	}

	return n, tsr
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

func (t *Tree) allocateContext() *cTx {
	maxParams := t.maxParams.Load()
	params := make(Params, 0, maxParams)
	tsrParams := make(Params, 0, maxParams)
	skipNds := make(skippedNodes, 0, t.maxDepth.Load())
	return &cTx{
		params:    &params,
		skipNds:   &skipNds,
		tsrParams: &tsrParams,
		// This is a read only value, no reset, it's always the
		// owner of the pool.
		tree: t,
		// This is a read only value, no reset.
		fox: t.fox,
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

// newRoute create a new route, apply path options and apply middleware on the handler.
func (t *Tree) newRoute(path string, handler HandlerFunc, opts ...PathOption) *Route {
	rte := &Route{
		ipStrategy:            t.fox.ipStrategy,
		hbase:                 handler,
		path:                  path,
		mws:                   t.mws,
		redirectTrailingSlash: t.fox.redirectTrailingSlash,
		ignoreTrailingSlash:   t.fox.ignoreTrailingSlash,
	}

	for _, opt := range opts {
		opt.applyPath(rte)
	}
	rte.hself, rte.hall = applyRouteMiddleware(rte.mws, handler)

	return rte
}
