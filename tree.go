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
// given time (see [Router.Swap]), you MUST always copy the pointer locally to avoid inadvertently causing a deadlock
// by locking/unlocking the wrong Tree.
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
	sync.Mutex
	maxParams atomic.Uint32
	maxDepth  atomic.Uint32
	race      atomic.Uint32
}

// Handle registers a new handler for the given method and path. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrRouteConflict]: If the route conflicts with another.
//   - [ErrInvalidRoute]: If the provided method or path is invalid.
//
// It's safe to add a new handler while the tree is in use for serving requests. However, this function is NOT
// thread-safe and should be run serially, along with all other [Tree] APIs that perform write operations.
// To override an existing route, use [Tree.Update].
func (t *Tree) Handle(method, path string, handler HandlerFunc, opts ...PathOption) (*Route, error) {
	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if matched := regEnLetter.MatchString(method); !matched {
		return nil, fmt.Errorf("%w: missing or invalid http method", ErrInvalidRoute)
	}

	n, err := parseRoute(path)
	if err != nil {
		return nil, err
	}

	rte := t.newRoute(path, handler, opts...)

	if err = t.insert(method, rte, n); err != nil {
		return nil, err
	}
	return rte, nil
}

// Update override an existing handler for the given method and path. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
// - [ErrRouteNotFound]: if the route does not exist.
// - [ErrInvalidRoute]: If the provided method or path is invalid.
//
// It's safe to update a handler while the tree is in use for serving requests. However, this function is NOT thread-safe
// and should be run serially, along with all other [Tree] APIs that perform write operations. To add a new handler,
// use [Tree.Handle] method.
func (t *Tree) Update(method, path string, handler HandlerFunc, opts ...PathOption) (*Route, error) {
	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if method == "" {
		return nil, fmt.Errorf("%w: missing http method", ErrInvalidRoute)
	}

	_, err := parseRoute(path)
	if err != nil {
		return nil, err
	}

	rte := t.newRoute(path, handler, opts...)
	if err = t.update(method, rte); err != nil {
		return nil, err
	}

	return rte, nil
}

// Delete deletes an existing handler for the given method and path. If an error occurs, it returns one of the following:
// - [ErrRouteNotFound]: if the route does not exist.
// - [ErrInvalidRoute]: If the provided method or path is invalid.
// It's safe to delete a handler while the tree is in use for serving requests. However, this function is NOT
// thread-safe and should be run serially, along with all other [Tree] APIs that perform write operations.
func (t *Tree) Delete(method, path string) error {
	if method == "" {
		return fmt.Errorf("%w: missing http method", ErrInvalidRoute)
	}

	_, err := parseRoute(path)
	if err != nil {
		return err
	}

	if !t.remove(method, path) {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, path)
	}

	return nil
}

// Has allows to check if the given method and path exactly match a registered route. This function is safe for
// concurrent use by multiple goroutine and while mutation on [Tree] are ongoing. See also [Tree.Route] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Has(method, path string) bool {
	return t.Route(method, path) != nil
}

// Route performs a lookup for a registered route matching the given method and path. It returns the [Route] if a
// match is found or nil otherwise. This function is safe for concurrent use by multiple goroutine and while
// mutation on [Tree] are ongoing. See also [Tree.Has] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Route(method, path string) *Route {
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 || len(nds[index].children) == 0 {
		return nil
	}

	c := t.ctx.Get().(*cTx)
	c.resetNil()
	n, tsr := t.lookup(nds[index].children[0].Load(), path, c, true)
	c.Close()
	if n != nil && !tsr && n.route.path == path {
		return n.route
	}
	return nil
}

// Reverse perform a reverse lookup on the tree for the given method and path and return the matching registered [Route]
// (if any) along with a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). This function is safe for concurrent use by multiple goroutine and while
// mutation on [Tree] are ongoing. See also [Tree.Lookup] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Reverse(method, path string) (route *Route, tsr bool) {
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 || len(nds[index].children) == 0 {
		return nil, false
	}

	c := t.ctx.Get().(*cTx)
	c.resetNil()
	n, tsr := t.lookup(nds[index].children[0].Load(), path, c, true)
	c.Close()
	if n != nil {
		return n.route, tsr
	}
	return nil, false
}

// Lookup performs a manual route lookup for a given [http.Request], returning the matched [Route] along with a
// [ContextCloser], and a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). If there is a direct match or a tsr is possible, Lookup always return a
// [Route] and a [ContextCloser]. The [ContextCloser] should always be closed if non-nil. This function is safe for
// concurrent use by multiple goroutine and while mutation on [Tree] are ongoing. See also [Tree.Reverse] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Lookup(w ResponseWriter, r *http.Request) (route *Route, cc ContextCloser, tsr bool) {
	nds := *t.nodes.Load()
	index := findRootNode(r.Method, nds)
	if index < 0 || len(nds[index].children) == 0 {
		return
	}

	c := t.ctx.Get().(*cTx)
	c.resetWithWriter(w, r)

	target := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		target = r.URL.RawPath
	}

	n, tsr := t.lookup(nds[index].children[0].Load(), target, c, false)
	if n != nil {
		c.route = n.route
		c.tsr = tsr
		return n.route, c, tsr
	}
	c.Close()
	return nil, nil, tsr
}

// Iter returns an iterator that provides access to a collection of iterators for traversing the routing tree.
// This function is safe for concurrent use by multiple goroutine and while mutation on [Tree] are ongoing.
// This API is EXPERIMENTAL and may change in future releases.
func (t *Tree) Iter() Iter {
	return Iter{t: t}
}

// Insert is not safe for concurrent use. The path must start by '/' and it's not validated. Use
// parseRoute before.
func (t *Tree) insert(method string, route *Route, paramsN uint32) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	if !t.race.CompareAndSwap(0, 1) {
		panic(ErrConcurrentAccess)
	}
	defer t.race.Store(0)

	var rootNode *node
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		rootNode = &node{key: method}
		t.addRoot(rootNode)
	} else {
		rootNode = nds[index]
	}

	path := route.path

	result := t.search(rootNode, path)
	switch result.classify() {
	case exactMatch:
		// e.g. matched exactly "te" node when inserting "te" key.
		// te
		// ├── st
		// └── am
		// Create a new node from "st" reference and update the "te" (parent) reference to "st" node.
		if result.matched.isLeaf() {
			return fmt.Errorf("%w: new route %s %s conflict with %s", ErrRouteExist, method, route.path, result.matched.route.path)
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

		// No children, so no paramChild
		child := newNode(keySuffix, route, nil)
		edges := result.matched.getEdgesShallowCopy()
		edges = append(edges, child)
		n := newNode(
			result.matched.key,
			result.matched.route,
			edges,
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

		// Rule: a node with {param} or *{wildcard} has no child or has a separator before the end of the key
		for i := len(cPrefix) - 1; i >= 0; i-- {
			if cPrefix[i] == '/' {
				break
			}
			if cPrefix[i] == '{' || cPrefix[i] == '*' {
				return newConflictErr(method, path, getRouteConflict(result.matched))
			}
		}

		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)
		keySuffix := path[result.charsMatched:]

		// No children, so no paramChild or wildcardChild
		n1 := newNodeFromRef(keySuffix, route, nil, nil, -1, -1) // inserted node
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
func (t *Tree) update(method string, route *Route) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	if !t.race.CompareAndSwap(0, 1) {
		panic(ErrConcurrentAccess)
	}
	defer t.race.Store(0)

	path := route.path

	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, path)
	}

	result := t.search(nds[index], path)
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
func (t *Tree) remove(method, path string) bool {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	if !t.race.CompareAndSwap(0, 1) {
		panic(ErrConcurrentAccess)
	}
	defer t.race.Store(0)

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
			result.matched.paramChildIndex,
			result.matched.wildcardChildIndex,
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
			child.paramChildIndex,
			child.wildcardChildIndex,
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
	starDelim    = '*'
)

func (t *Tree) lookup(target *node, path string, c *cTx, lazy bool) (n *node, tsr bool) {
	var (
		charsMatched            int
		charsMatchedInNodeFound int
		paramCnt                uint32
		paramKeyCnt             uint32
		parent                  *node
	)

	current := target
	*c.skipNds = (*c.skipNds)[:0]

Walk:
	for charsMatched < len(path) {
		charsMatchedInNodeFound = 0
		for i := 0; charsMatched < len(path); i++ {
			if i >= len(current.key) {
				break
			}

			if current.key[i] != path[charsMatched] || path[charsMatched] == bracketDelim || path[charsMatched] == starDelim {
				if current.key[i] == bracketDelim {
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

				if current.key[i] == starDelim {
					//                | current.params[paramKeyCnt].end (10)
					// key: foo/*{bar}/                                      => 10 - 5 = 5 => i+=idx set i to '/'
					//          | charsMatchedInNodeFound (5)
					idx := current.params[paramKeyCnt].end - charsMatchedInNodeFound
					var interNode *node
					if idx >= 0 {
						// Unfortunately, we cannot use object pooling here because we need to keep a reference to this
						// interNode object until the lookup function returns, especially when TSR (Trailing Slash Redirect)
						// is enabled. The interNode may be referenced by subNode and 'n'.
						interNode = &node{
							route:     current.route,
							key:       current.key[current.params[paramKeyCnt].end:],
							childKeys: current.childKeys,
							children:  current.children,
							// len(current.params)-1 is safe since we have at least the current infix wildcard in params
							params:             make([]param, 0, len(current.params)-1),
							paramChildIndex:    current.paramChildIndex,
							wildcardChildIndex: current.wildcardChildIndex,
						}
						for _, ps := range current.params[paramKeyCnt+1:] { // paramKeyCnt+1 is safe since we have at least the current infix wildcard in params
							interNode.params = append(interNode.params, param{
								key: ps.key,
								// end is relative to the original key, so we need to adjust the position relative to
								// the new intermediary node.
								end:      ps.end - current.params[paramKeyCnt].end,
								catchAll: ps.catchAll,
							})
						}

						charsMatchedInNodeFound += idx

					} else if len(current.children) > 0 {
						interNode = current.get(0)
						charsMatchedInNodeFound += len(current.key[charsMatchedInNodeFound:])
					} else {
						// We are in an ending catch all node with no child, so it's a direct match
						if !lazy {
							*c.params = append(*c.params, Param{Key: current.params[paramKeyCnt].key, Value: path[charsMatched:]})
						}
						return current, false
					}

					subCtx := t.ctx.Get().(*cTx)
					startPath := charsMatched
					for {
						idx := strings.IndexByte(path[charsMatched:], slashDelim)
						// idx >= 0, we have a next segment with at least one char
						if idx > 0 {
							*subCtx.params = (*subCtx.params)[:0]
							charsMatched += idx
							subNode, subTsr := t.lookup(interNode, path[charsMatched:], subCtx, false)
							if subNode == nil {
								// Try with next segment
								charsMatched++
								continue
							}

							// We have a tsr opportunity
							if subTsr {
								// Only if no previous tsr
								if !tsr {
									tsr = true
									n = subNode
									if !lazy {
										*c.tsrParams = (*c.tsrParams)[:0]
										*c.tsrParams = append(*c.tsrParams, *c.params...)
										*c.tsrParams = append(*c.tsrParams, Param{Key: current.params[paramKeyCnt].key, Value: path[startPath:charsMatched]})
										*c.tsrParams = append(*c.tsrParams, *subCtx.tsrParams...)
									}
								}

								// Try with next segment
								charsMatched++
								continue
							}

							if !lazy {
								*c.params = append(*c.params, Param{Key: current.params[paramKeyCnt].key, Value: path[startPath:charsMatched]})
								*c.params = append(*c.params, *subCtx.params...)
							}

							t.ctx.Put(subCtx)
							return subNode, subTsr
						}

						t.ctx.Put(subCtx)

						// We can record params here because it may be either an ending catch-all node (leaf=/foo/*{args}) with
						// children, or we may have a tsr opportunity (leaf=/foo/*{args}/ with /foo/x/y/z path). Note that if
						// there is no tsr opportunity, and skipped nodes > 0, we will truncate the params anyway.
						if !lazy {
							*c.params = append(*c.params, Param{Key: current.params[paramKeyCnt].key, Value: path[startPath:]})
						}

						// We are also in an ending catch all, and this is the most specific path
						if current.params[paramKeyCnt].end == -1 {
							return current, false
						}

						charsMatched += len(path[charsMatched:])

						break Walk
					}
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

			// No next static segment found, but maybe some params or wildcard child
			if idx < 0 {
				// We have at least a param child which is has higher priority that catch-all
				if current.paramChildIndex >= 0 {
					// We have also a wildcard child, save it for later evaluation
					if current.wildcardChildIndex >= 0 {
						*c.skipNds = append(*c.skipNds, skippedNode{current, charsMatched, paramCnt, current.wildcardChildIndex})
					}

					// Go deeper
					idx = current.paramChildIndex
					parent = current
					current = current.children[idx].Load()
					paramKeyCnt = 0
					continue
				}
				if current.wildcardChildIndex >= 0 {
					// We have a wildcard child, go deeper
					idx = current.wildcardChildIndex
					parent = current
					current = current.children[idx].Load()
					paramKeyCnt = 0
					continue
				}

				// We have nothing more to evaluate
				break
			}

			// Here we have a next static segment and possibly wildcard children, so we save them for later evaluation if needed.
			if current.wildcardChildIndex >= 0 {
				*c.skipNds = append(*c.skipNds, skippedNode{current, charsMatched, paramCnt, current.wildcardChildIndex})
			}
			if current.paramChildIndex >= 0 {
				*c.skipNds = append(*c.skipNds, skippedNode{current, charsMatched, paramCnt, current.paramChildIndex})
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

		parent = skipped.n
		current = skipped.n.children[skipped.childIndex].Load()

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
		mws:                   t.fox.mws,
		redirectTrailingSlash: t.fox.redirectTrailingSlash,
		ignoreTrailingSlash:   t.fox.ignoreTrailingSlash,
	}

	for _, opt := range opts {
		opt.applyPath(rte)
	}
	rte.hself, rte.hall = applyRouteMiddleware(rte.mws, handler)

	return rte
}

func copyParams(src, dst *Params) {
	if cap(*src) > cap(*dst) {
		// Grow dst to a least cap(src)
		*dst = slices.Grow(*dst, cap(*src))
	}
	// cap(dst) >= cap(src)
	// now constraint into len(src) & cap(src)
	*dst = (*dst)[:len(*src):cap(*src)]
	copy(*dst, *src)
}
