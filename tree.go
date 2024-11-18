// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"github.com/tigerwill90/fox/internal/netutil"
	"github.com/tigerwill90/fox/internal/simplelru"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// Tree implements a Concurrent Radix Tree that supports lock-free reads while allowing concurrent writes.
// The caller is responsible for ensuring that all writes are run serially.
type Tree struct {
	ctx       sync.Pool // immutable once assigned
	root      atomic.Pointer[[]*node]
	fox       *Router                    // immutable once assigned
	writable  *simplelru.LRU[*node, any] // must only be accessible by writer thread.
	maxParams atomic.Uint32
	maxDepth  atomic.Uint32
	race      atomic.Uint32
	txn       bool // immutable once assigned
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
func (t *Tree) Handle(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if matched := regEnLetter.MatchString(method); !matched {
		return nil, fmt.Errorf("%w: missing or invalid http method", ErrInvalidRoute)
	}

	rte, n, err := t.newRoute(pattern, handler, opts...)
	if err != nil {
		return nil, err
	}

	if err = t.insert(method, rte, n); err != nil {
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
func (t *Tree) Update(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if method == "" {
		return nil, fmt.Errorf("%w: missing http method", ErrInvalidRoute)
	}

	rte, _, err := t.newRoute(pattern, handler, opts...)
	if err != nil {
		return nil, err
	}

	if err = t.update(method, rte); err != nil {
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
func (t *Tree) Delete(method, pattern string) error {
	if method == "" {
		return fmt.Errorf("%w: missing http method", ErrInvalidRoute)
	}

	_, _, err := parseRoute(pattern)
	if err != nil {
		return err
	}

	if !t.remove(method, pattern) {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, pattern)
	}

	return nil
}

// Has allows to check if the given method and route pattern exactly match a registered route. This function is safe for
// concurrent use by multiple goroutine and while mutation on [Tree] are ongoing. See also [Tree.Route] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Has(method, pattern string) bool {
	return t.Route(method, pattern) != nil
}

// Route performs a lookup for a registered route matching the given method and route pattern. It returns the [Route] if a
// match is found or nil otherwise. This function is safe for concurrent use by multiple goroutine and while
// mutation on [Tree] are ongoing. See also [Tree.Has] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Route(method, pattern string) *Route {
	nds := *t.root.Load()
	index := findRootNode(method, nds)
	if index < 0 || len(nds[index].children) == 0 {
		return nil
	}

	c := t.ctx.Get().(*cTx)
	c.resetNil()

	host, path := SplitHostPath(pattern)
	n, tsr := t.lookup(nds[index], host, path, c, true)
	c.Close()
	if n != nil && !tsr && n.route.pattern == pattern {
		return n.route
	}
	return nil
}

// Reverse perform a reverse lookup on the tree for the given method, host and path and return the matching registered [Route]
// (if any) along with a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). This function is safe for concurrent use by multiple goroutine and while
// mutation on [Tree] are ongoing. See also [Tree.Lookup] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (t *Tree) Reverse(method, host, path string) (route *Route, tsr bool) {
	nds := *t.root.Load()
	index := findRootNode(method, nds)
	if index < 0 || len(nds[index].children) == 0 {
		return nil, false
	}

	c := t.ctx.Get().(*cTx)
	c.resetNil()
	n, tsr := t.lookup(nds[index], host, path, c, true)
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
	nds := *t.root.Load()
	index := findRootNode(r.Method, nds)
	if index < 0 || len(nds[index].children) == 0 {
		return
	}

	c := t.ctx.Get().(*cTx)
	c.resetWithWriter(w, r)

	path := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		path = r.URL.RawPath
	}

	n, tsr := t.lookup(nds[index], r.Host, path, c, false)
	if n != nil {
		c.route = n.route
		c.tsr = tsr
		return n.route, c, tsr
	}
	c.Close()
	return nil, nil, tsr
}

// Iter returns a collection of iterators for traversing the routing tree.
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

	if t.txn && t.writable == nil {
		lru, err := simplelru.NewLRU[*node, any](defaultModifiedCache, nil)
		if err != nil {
			panic(err)
		}
		t.writable = lru
	}

	var rootNode *node
	nds := *t.root.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		rootNode = &node{
			key:                method,
			paramChildIndex:    -1,
			wildcardChildIndex: -1,
		}
		t.addRoot(rootNode)
	} else {
		rootNode = nds[index]
	}

	path := route.pattern

	result := t.search(rootNode, path)
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

		if t.txn {
			t.updateToRoot(result.p, result.pp, result.ppp, result.visited, n)
			break
		}
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
		if t.txn {
			t.updateToRoot(result.p, result.pp, result.ppp, result.visited, parent)
			break
		}
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

		edges := result.matched.getEdgesShallowCopy()
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
			if t.txn {
				t.writable.Add(n, nil)
			}
			t.updateRoot(n)
			break
		}
		if t.txn {
			t.updateToRoot(result.p, result.pp, result.ppp, result.visited, n)
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

		if t.txn {
			t.updateToRoot(result.p, result.pp, result.ppp, result.visited, n3)
			break
		}

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

	if t.txn && t.writable == nil {
		lru, err := simplelru.NewLRU[*node, any](defaultModifiedCache, nil)
		if err != nil {
			panic(err)
		}
		t.writable = lru
	}

	path := route.pattern

	nds := *t.root.Load()
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

	if t.txn {
		t.updateToRoot(result.p, result.pp, result.ppp, result.visited, n)
		return nil
	}

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

	if t.txn && t.writable == nil {
		lru, err := simplelru.NewLRU[*node, any](defaultModifiedCache, nil)
		if err != nil {
			panic(err)
		}
		t.writable = lru
	}

	nds := *t.root.Load()
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

		if t.txn {
			t.updateToRoot(result.p, result.pp, result.ppp, result.visited, n)
			return true
		}

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

		if t.txn {
			t.updateToRoot(result.p, result.pp, result.ppp, result.visited, n)
			return true
		}

		result.p.updateEdge(n)
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
				return t.removeRoot(method)
			}
			parent.key = method
			if t.txn {
				t.writable.Add(parent, nil)
			}
			t.updateRoot(parent)
			return true
		}

		if t.txn {
			t.updateToRoot(nil, nil, result.ppp, result.visited, parent)
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
		if t.txn {
			t.writable.Add(parent, nil)
		}
		t.updateRoot(parent)
		return true
	}

	if t.txn {
		t.updateToRoot(nil, result.pp, result.ppp, result.visited, parent)
		return true
	}

	result.pp.updateEdge(parent)
	return true
}

// truncate truncates the tree for the provided methods.
// This function should be guarded by mutex.
func (t *Tree) truncate(methods []string) {
	if !t.race.CompareAndSwap(0, 1) {
		panic(ErrConcurrentAccess)
	}
	defer t.race.Store(0)

	nds := *t.root.Load()
	if len(methods) == 0 {
		// Pre instantiate nodes for common http verb
		newNds := make([]*node, len(commonVerbs))
		for i := range commonVerbs {
			newNds[i] = new(node)
			newNds[i].key = commonVerbs[i]
			newNds[i].paramChildIndex = -1
			newNds[i].wildcardChildIndex = -1
		}
		t.root.Store(&newNds)
		return
	}

	oldlen := len(nds)
	newNds := make([]*node, len(nds))
	copy(newNds, nds)

	for _, method := range methods {
		idx := findRootNode(method, newNds)
		if idx < 0 {
			continue
		}
		if !isRemovable(method) {
			newNds[idx] = new(node)
			newNds[idx].key = commonVerbs[idx]
			newNds[idx].paramChildIndex = -1
			newNds[idx].wildcardChildIndex = -1
			continue
		}

		newNds = append(newNds[:idx], newNds[idx+1:]...)
	}

	clear(newNds[len(newNds):oldlen]) // zero/nil out the obsolete elements, for GC

	t.root.Store(&newNds)
}

const (
	slashDelim   byte = '/'
	dotDelim     byte = '.'
	bracketDelim byte = '{'
	starDelim    byte = '*'
)

// lookup  returns the node matching the host and/or path. If lazy is false, it parses and record into c, path segment according to
// the route definition. In case of indirect match, tsr is true and n != nil.
func (t *Tree) lookup(root *node, hostPort, path string, c *cTx, lazy bool) (n *node, tsr bool) {
	// The tree for this method only have path registered
	if len(root.children) == 1 && root.childKeys[0] == '/' {
		return t.lookupByPath(root.children[0].Load(), path, c, lazy)
	}

	host := netutil.StripHostPort(hostPort)
	if host != "" {
		// Try first by domain
		n, tsr = t.lookupByDomain(root, host, path, c, lazy)
		if n != nil {
			return n, tsr
		}
	}

	// Fallback by path
	idx := linearSearch(root.childKeys, '/')
	if idx < 0 {
		return nil, false
	}

	// Reset any recorded params and tsrParams
	*c.params = (*c.params)[:0]
	c.tsr = false

	return t.lookupByPath(root.children[idx].Load(), path, c, lazy)
}

// lookupByDomain is like lookupByPath, but for target with hostname.
func (t *Tree) lookupByDomain(target *node, host, path string, c *cTx, lazy bool) (n *node, tsr bool) {
	var (
		charsMatched            int
		charsMatchedInNodeFound int
		paramCnt                uint32
		paramKeyCnt             uint32
		current                 *node
	)

	*c.skipNds = (*c.skipNds)[:0]

	idx := -1
	for i := 0; i < len(target.childKeys); i++ {
		if target.childKeys[i] == host[0] {
			idx = i
			break
		}
	}
	if idx < 0 {
		if target.paramChildIndex >= 0 {
			// We start with a param child, let's go deeper directly
			idx = target.paramChildIndex
			current = target.children[idx].Load()
		} else {
			return
		}
	} else {
		// Here we have a next static segment and possibly wildcard children, so we save them for later evaluation if needed.
		if target.paramChildIndex >= 0 {
			*c.skipNds = append(*c.skipNds, skippedNode{target, charsMatched, paramCnt, target.paramChildIndex})
		}
		current = target.children[idx].Load()
	}

	subCtx := t.ctx.Get().(*cTx)
	defer t.ctx.Put(subCtx)

Walk:
	for charsMatched < len(host) {
		charsMatchedInNodeFound = 0
		for i := 0; charsMatched < len(host); i++ {
			if i >= len(current.key) {
				break
			}

			if current.key[i] != host[charsMatched] || host[charsMatched] == bracketDelim {
				if current.key[i] == bracketDelim {
					startPath := charsMatched
					idx = strings.IndexByte(host[charsMatched:], dotDelim)
					if idx > 0 {
						// There is another path segment (e.g. foo.{bar}.baz)
						charsMatched += idx
					} else if idx < 0 {
						//
						// This is the end of the path (e.g. foo.{bar})
						charsMatched += len(host[charsMatched:])
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
						*c.params = append(*c.params, Param{Key: current.params[paramKeyCnt].key, Value: host[startPath:charsMatched]})
					}
					paramKeyCnt++
					continue
				}

				break Walk
			}
			charsMatched++
			charsMatchedInNodeFound++
		}

		if charsMatched < len(host) {
			// linear search
			idx = -1
			for i := 0; i < len(current.childKeys); i++ {
				if current.childKeys[i] == host[charsMatched] {
					idx = i
					break
				}
			}

			// No next static segment found, but maybe some params
			if idx < 0 {
				// We have a param child
				if current.paramChildIndex >= 0 {
					// Go deeper
					idx = current.paramChildIndex
					current = current.children[idx].Load()
					paramKeyCnt = 0
					continue
				}

				// We have nothing more to evaluate
				break
			}

			// Here we have a next static segment and possibly wildcard children, so we save them for later evaluation if needed.
			if current.paramChildIndex >= 0 {
				*c.skipNds = append(*c.skipNds, skippedNode{current, charsMatched, paramCnt, current.paramChildIndex})
			}

			current = current.children[idx].Load()
			paramKeyCnt = 0
		}
	}

	paramCnt = 0
	paramKeyCnt = 0
	hasSkpNds := len(*c.skipNds) > 0

	if charsMatchedInNodeFound == len(current.key) {
		// linear search
		idx = -1
		for i := 0; i < len(current.childKeys); i++ {
			if current.childKeys[i] == slashDelim {
				idx = i
				break
			}
		}
		if idx < 0 {
			goto Backtrack
		}

		*subCtx.params = (*subCtx.params)[:0]
		subNode, subTsr := t.lookupByPath(current.get(idx), path, subCtx, lazy)
		if subNode == nil {
			goto Backtrack
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
					*c.tsrParams = append(*c.tsrParams, *subCtx.tsrParams...)
				}
			}

			goto Backtrack
		}

		// Direct match
		if !lazy {
			*c.params = append(*c.params, *subCtx.params...)
		}

		return subNode, subTsr
	}

Backtrack:
	if hasSkpNds {
		skipped := c.skipNds.pop()

		current = skipped.n.children[skipped.childIndex].Load()

		*c.params = (*c.params)[:skipped.paramCnt]
		charsMatched = skipped.pathIndex
		goto Walk
	}

	return n, tsr
}

// lookupByPath returns the node matching the path. If lazy is false, it parses and record into c, path segment according to
// the route definition. In case of indirect match, tsr is true and n != nil.
func (t *Tree) lookupByPath(target *node, path string, c *cTx, lazy bool) (n *node, tsr bool) {
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
					var inode *node
					if idx >= 0 {
						inode = current.inode
						charsMatchedInNodeFound += idx
					} else if len(current.children) > 0 {
						inode = current.get(0)
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
						idx = strings.IndexByte(path[charsMatched:], slashDelim)
						// idx >= 0, we have a next segment with at least one char
						if idx > 0 {
							*subCtx.params = (*subCtx.params)[:0]
							charsMatched += idx
							subNode, subTsr := t.lookupByPath(inode, path[charsMatched:], subCtx, false)
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
					copyWithResize(c.tsrParams, c.params)
				}
			}
		}

		goto Backtrack
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
							copyWithResize(c.tsrParams, c.params)
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
							copyWithResize(c.tsrParams, c.params)
						}
					}
				}
			}

			goto Backtrack
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
					copyWithResize(c.tsrParams, c.params)
				}
			}
		}

		goto Backtrack
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
		visited                 []*node
		ppp                     *node
		pp                      *node
		p                       *node
		charsMatched            int
		charsMatchedInNodeFound int
		depth                   uint32
	)

	if t.txn {
		visited = make([]*node, 0, min(15, t.maxDepth.Load()))
	}

STOP:
	for charsMatched < len(path) {
		next := current.getEdge(path[charsMatched])
		if next == nil {
			break STOP
		}

		depth++
		if t.txn && ppp != nil {
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

func (t *Tree) allocateContext() *cTx {
	maxParams := t.maxParams.Load()
	params := make(Params, 0, maxParams)
	tsrParams := make(Params, 0, maxParams)
	skipNds := make(skippedNodes, 0, t.maxDepth.Load())
	return &cTx{
		params:    &params,
		skipNds:   &skipNds,
		tsrParams: &tsrParams,
		// This is a read only value, no reset
		fox: t.fox,
	}
}

// snapshot create a point in time snapshot of Tree. Each write on the new Tree are fully isolated. However, a lock must
// be held locked until the snapshot is commited or aborted.
func (t *Tree) snapshot() *Tree {
	tree := new(Tree)
	tree.fox = t.fox // immutable once assigned
	tree.txn = true  // immutable once assigned
	tree.root.Store(t.root.Load())
	tree.ctx = sync.Pool{
		New: func() any {
			return tree.allocateContext()
		},
	}
	tree.maxDepth.Store(t.maxDepth.Load())
	tree.maxParams.Store(t.maxParams.Load())
	return tree
}

// addRoot append a new root node to the tree.
// Note: This function should be guarded by mutex.
func (t *Tree) addRoot(n *node) {
	nds := *t.root.Load()
	newNds := make([]*node, 0, len(nds)+1)
	newNds = append(newNds, nds...)
	newNds = append(newNds, n)
	t.root.Store(&newNds)
}

// updateRoot replaces a root node in the tree.
// Due to performance optimization, the tree uses atomic.Pointer[[]*node] instead of
// atomic.Pointer[atomic.Pointer[*node]]. As a result, the root node cannot be replaced
// directly by swapping the pointer. Instead, a new list of nodes is created with the
// updated root node, and the entire list is swapped afterwards.
// Note: This function should be guarded by mutex.
func (t *Tree) updateRoot(n *node) bool {
	nds := *t.root.Load()
	// for root node, the key contains the HTTP verb.
	index := findRootNode(n.key, nds)
	if index < 0 {
		return false
	}
	newNds := make([]*node, 0, len(nds))
	newNds = append(newNds, nds[:index]...)
	newNds = append(newNds, n)
	newNds = append(newNds, nds[index+1:]...)
	t.root.Store(&newNds)
	return true
}

// removeRoot remove a root nod from the tree.
// Note: This function should be guarded by mutex.
func (t *Tree) removeRoot(method string) bool {
	nds := *t.root.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return false
	}
	newNds := make([]*node, 0, len(nds)-1)
	newNds = append(newNds, nds[:index]...)
	newNds = append(newNds, nds[index+1:]...)
	t.root.Store(&newNds)
	return true
}

// updateToRoot propagate update to the root by cloning any visited node that have not been cloned previously.
// This effectively allow to create a fully isolated snapshot of the tree.
// Note: This function should be guarded by mutex.
func (t *Tree) updateToRoot(p, pp, ppp *node, visited []*node, n *node) {
	nn := n
	if p != nil {
		np := p
		if _, ok := t.writable.Get(np); !ok {
			np = np.clone()
			t.writable.Add(np, nil)
			// np is root and has never been writen
			if pp == nil {
				np.updateEdge(n)
				t.updateRoot(np)
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
		if _, ok := t.writable.Get(npp); !ok {
			npp = npp.clone()
			t.writable.Add(npp, nil)
			// npp is root and has never been writen
			if ppp == nil {
				npp.updateEdge(nn)
				t.updateRoot(npp)
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
	if _, ok := t.writable.Get(nppp); !ok {
		nppp = nppp.clone()
		t.writable.Add(nppp, nil)
		// nppp is root and has never been writen
		if len(visited) == 0 {
			nppp.updateEdge(nn)
			t.updateRoot(nppp)
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

		if _, ok := t.writable.Get(vNode); !ok {
			vNode = vNode.clone()
			t.writable.Add(vNode, nil)
		}

		vNode.updateEdge(current)
		current = vNode
	}

	if current != visited[0] {
		// root is a clone
		t.updateRoot(current)
	}
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

// newRoute create a new route, apply route options and apply middleware on the handler.
func (t *Tree) newRoute(pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, uint32, error) {
	n, endHost, err := parseRoute(pattern)
	if err != nil {
		return nil, 0, err
	}

	rte := &Route{
		ipStrategy:            t.fox.ipStrategy,
		hbase:                 handler,
		pattern:               pattern,
		mws:                   t.fox.mws,
		redirectTrailingSlash: t.fox.redirectTrailingSlash,
		ignoreTrailingSlash:   t.fox.ignoreTrailingSlash,
		hostSplit:             endHost, // 0 if no host
	}

	for _, opt := range opts {
		opt.applyRoute(rte)
	}
	rte.hself, rte.hall = applyRouteMiddleware(rte.mws, handler)

	return rte, n, nil
}

func copyWithResize[S ~[]T, T any](dst, src *S) {
	if len(*src) > len(*dst) {
		// Grow dst cap to a least len(src)
		*dst = slices.Grow(*dst, len(*src)-len(*dst))
	}
	// cap(dst) >= len(src)
	// now constraint into len(src) & cap(dst)
	*dst = (*dst)[:len(*src):cap(*dst)]
	copy(*dst, *src)
}

func recreateParentEdge(parent, matched *node) []*node {
	parentEdges := make([]*node, len(parent.children)-1)
	added := 0
	for i := 0; i < len(parent.children); i++ {
		n := parent.get(i)
		if n != matched {
			parentEdges[added] = n
			added++
		}
	}
	return parentEdges
}

type resultType int

const (
	exactMatch resultType = iota
	incompleteMatchToEndOfEdge
	incompleteMatchToMiddleOfEdge
	keyEndMidEdge
)

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

func (r searchResult) isKeyMidEdge() bool {
	return r.charsMatched == len(r.path) && r.charsMatchedInNodeFound < len(r.matched.key)
}

func (c resultType) String() string {
	return [...]string{"EXACT_MATCH", "INCOMPLETE_MATCH_TO_END_OF_EDGE", "INCOMPLETE_MATCH_TO_MIDDLE_OF_EDGE", "KEY_END_MID_EDGE"}[c]
}

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

func commonPrefix(k1, k2 string) string {
	minLength := min(len(k1), len(k2))
	for i := 0; i < minLength; i++ {
		if k1[i] != k2[i] {
			return k1[:i]
		}
	}
	return k1[:minLength]
}
