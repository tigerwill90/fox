package fox

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// Tree implements a Concurrent Radix Tree that supports lock-free reads while allowing concurrent writes.
// Each tree as its own sync.Mutex and sync.Pool that may be used to serialize write and reduce memory allocation.
//
// IMPORTANT: Since the router tree may be swapped at any given time, you MUST always copy the pointer locally
// to avoid inadvertently releasing Params to the wrong pool or worst, causing a deadlock by locking/unlocking the
// wrong Tree.
//
// Good:
// t := r.Tree()
// t.Lock()
// defer t.Unlock()
//
// Dramatically bad, may cause deadlock
// r.Tree().Lock()
// defer r.Tree().Unlock()
//
// This principle also applies to the Lookup function, which requires releasing the Params slice by calling params.Free(tree).
// Always ensure that the Tree pointer passed as a parameter to params.Free is the same as the one passed to the Lookup function.
type Tree struct {
	p     sync.Pool
	nodes atomic.Pointer[[]*node]
	sync.Mutex
	maxParams atomic.Uint32
	saveRoute bool
}

// Handler registers a new handler for the given method and path. This function return an error if the route
// is already registered or conflict with another. It's perfectly safe to add a new handler while the tree is in use
// for serving requests. However, this function is NOT thread safe and should be run serially, along with
// all other Tree's APIs. To override an existing route, use Update.
func (t *Tree) Handler(method, path string, handler Handler) error {
	p, catchAllKey, n, err := parseRoute(path)
	if err != nil {
		return err
	}

	if t.saveRoute {
		n += 1
	}

	return t.insert(method, p, catchAllKey, uint32(n), handler)
}

// Update override an existing handler for the given method and path. If the route does not exist,
// the function return an ErrRouteNotFound. It's perfectly safe to update a handler while the tree is in use for
// serving requests. However, this function is NOT thread safe and should be run serially, along with
// all other Tree's APIs. To add new handler, use Handler method.
func (t *Tree) Update(method, path string, handler Handler) error {
	p, catchAllKey, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	return t.update(method, p, catchAllKey, handler)
}

// Remove delete an existing handler for the given method and path. If the route does not exist, the function
// return an ErrRouteNotFound. It's perfectly safe to remove a handler while the tree is in use for serving requests.
// However, this function is NOT thread safe and should be run serially, along with all other Tree's APIs.
func (t *Tree) Remove(method, path string) error {
	path, _, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	if !t.remove(method, path) {
		return ErrRouteNotFound
	}

	return nil
}

// insert is not safe for concurrent use.
func (t *Tree) insert(method, path, catchAllKey string, paramsN uint32, handler Handler) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	if method == "" {
		return fmt.Errorf("http method is missing: %w", ErrInvalidRoute)
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
			return fmt.Errorf("route [%s] %s conflict: %w", method, path, ErrRouteExist)
		}

		// The matched node can only be the result of a previous split and therefore has children.
		if isCatchAll {
			return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
		}
		// We are updating an existing node. We only need to create a new node from
		// the matched one with the updated/added value (handler and wildcard).
		n := newNodeFromRef(result.matched.key, handler, result.matched.children, result.matched.childKeys, catchAllKey, result.matched.paramChild, path)

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

		if isCatchAll {
			return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
		}

		keyCharsFromStartOfNodeFound := path[result.charsMatched-result.charsMatchedInNodeFound:]
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.key)
		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)
		// Rule: a node with :param has no child or has a separator before the end of the key or its child
		// start with a separator
		if !strings.HasPrefix(suffixFromExistingEdge, "/") {
			for i := len(cPrefix) - 1; i >= 0; i-- {
				if cPrefix[i] == '/' {
					break
				}
				if cPrefix[i] == ':' {
					return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
				}
			}
		}

		child := newNodeFromRef(
			suffixFromExistingEdge,
			result.matched.handler,
			result.matched.children,
			result.matched.childKeys,
			result.matched.catchAllKey,
			result.matched.paramChild,
			result.matched.path,
		)

		parent := newNode(
			cPrefix,
			handler,
			[]*node{child},
			catchAllKey,
			// e.g. tree encode /tes/:t and insert /tes/
			// /tes/ (paramChild)
			// ├── :t
			// since /tes/xyz will match until /tes/ and when looking for next child, 'x' will match nothing
			// if paramChild == true {
			// 	next = current.get(0)
			// }
			strings.HasPrefix(suffixFromExistingEdge, ":"),
			path,
		)

		t.updateMaxParams(paramsN)
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

		if result.matched.isCatchAll() {
			return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
		}

		keySuffix := path[result.charsMatched:]
		// Rule: a node with :param has no child or has a separator before the end of the key
		// make sure than and existing params :x is not extended to :xy
		// :x/:y is of course valid
		if !strings.HasPrefix(keySuffix, "/") {
			for i := len(result.matched.key) - 1; i >= 0; i-- {
				if result.matched.key[i] == '/' {
					break
				}
				if result.matched.key[i] == ':' {
					return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
				}
			}
		}

		// No children, so no paramChild
		child := newNode(keySuffix, handler, nil, catchAllKey, false, path)
		edges := result.matched.getEdgesShallowCopy()
		edges = append(edges, child)
		n := newNode(
			result.matched.key,
			result.matched.handler,
			edges,
			result.matched.catchAllKey,
			// e.g. tree encode /tes/ and insert /tes/:t
			// /tes/ (paramChild)
			// ├── :t
			// since /tes/xyz will match until /tes/ and when looking for next child, 'x' will match nothing
			// if paramChild == true {
			// 	next = current.get(0)
			// }
			strings.HasPrefix(keySuffix, ":"),
			result.matched.path,
		)

		t.updateMaxParams(paramsN)
		if result.matched == rootNode {
			n.key = method
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

		// Rule: a node with :param has no child or has a separator before the end of the key
		for i := len(cPrefix) - 1; i >= 0; i-- {
			if cPrefix[i] == '/' {
				break
			}
			if cPrefix[i] == ':' {
				return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
			}
		}

		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)
		// Rule: parent's of a node with :param have only one node or are prefixed by a char (e.g /:param)
		if strings.HasPrefix(suffixFromExistingEdge, ":") {
			return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
		}

		keySuffix := path[result.charsMatched:]
		// Rule: parent's of a node with :param have only one node or are prefixed by a char (e.g /:param)
		if strings.HasPrefix(keySuffix, ":") {
			return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched))
		}

		// No children, so no paramChild
		n1 := newNodeFromRef(keySuffix, handler, nil, nil, catchAllKey, false, path) // inserted node
		n2 := newNodeFromRef(
			suffixFromExistingEdge,
			result.matched.handler,
			result.matched.children,
			result.matched.childKeys,
			result.matched.catchAllKey,
			result.matched.paramChild,
			result.matched.path,
		) // previous matched node

		// n3 children never start with a param
		n3 := newNode(cPrefix, nil, []*node{n1, n2}, "", false, "") // intermediary node

		t.updateMaxParams(paramsN)
		result.p.updateEdge(n3)
	default:
		// safeguard against introducing a new result type
		panic("internal error: unexpected result type")
	}
	return nil
}

// update is not safe for concurrent use.
func (t *Tree) update(method string, path, catchAllKey string, handler Handler) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	nds := *t.nodes.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return fmt.Errorf("route [%s] %s is not registered: %w", method, path, ErrRouteNotFound)
	}

	result := t.search(nds[index], path)
	if !result.isExactMatch() || !result.matched.isLeaf() {
		return fmt.Errorf("route [%s] %s is not registered: %w", method, path, ErrRouteNotFound)
	}

	if catchAllKey != "" && len(result.matched.children) > 0 {
		return newConflictErr(method, path, catchAllKey, getRouteConflict(result.matched)[1:])
	}

	// We are updating an existing node (could be a leaf or not). We only need to create a new node from
	// the matched one with the updated/added value (handler and wildcard).
	n := newNodeFromRef(
		result.matched.key,
		handler,
		result.matched.children,
		result.matched.childKeys,
		catchAllKey,
		result.matched.paramChild,
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
			result.matched.paramChild,
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
			child.paramChild,
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
			child.paramChild,
			child.path,
		)
	} else {
		parent = newNode(
			result.p.key,
			result.p.handler,
			parentEdges,
			result.p.catchAllKey,
			result.p.paramChild,
			result.p.path,
		)
	}

	if parentIsRoot {
		if len(parent.children) == 0 && isRemovable(method) {
			return t.removeRoot(method)
		}
		parent.key = method
		t.updateRoot(parent)
		return true
	}

	result.pp.updateEdge(parent)
	return true
}

func (t *Tree) lookup(rootNode *node, path string, lazy bool) (n *node, params *Params, tsr bool) {
	var (
		charsMatched            int
		charsMatchedInNodeFound int
	)

	current := rootNode
STOP:
	for charsMatched < len(path) {
		idx := linearSearch(current.childKeys, path[charsMatched])
		if idx < 0 {
			if !current.paramChild {
				break STOP
			}
			idx = 0
		}

		current = current.get(idx)
		charsMatchedInNodeFound = 0
		for i := 0; charsMatched < len(path); i++ {
			if i >= len(current.key) {
				break
			}

			if current.key[i] != path[charsMatched] || path[charsMatched] == ':' {
				if current.key[i] == ':' {
					startPath := charsMatched
					idx := strings.Index(path[charsMatched:], "/")
					if idx >= 0 {
						charsMatched += idx
					} else {
						charsMatched += len(path[charsMatched:])
					}
					startKey := charsMatchedInNodeFound
					idx = strings.Index(current.key[startKey:], "/")
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
						if params == nil {
							params = t.newParams()
						}
						// :n where n > 0
						*params = append(*params, Param{Key: current.key[startKey+1 : charsMatchedInNodeFound], Value: path[startPath:charsMatched]})
					}
					continue
				}
				break STOP
			}

			charsMatched++
			charsMatchedInNodeFound++
		}
	}

	if !current.isLeaf() {
		return nil, params, false
	}

	if charsMatched == len(path) {
		if charsMatchedInNodeFound == len(current.key) {
			// Exact match, note that if we match a wildcard node, the param value is always '/'
			if !lazy && (t.saveRoute || current.isCatchAll()) {
				if params == nil {
					params = t.newParams()
				}

				if t.saveRoute {
					*params = append(*params, Param{Key: RouteKey, Value: current.path})
				}

				if current.isCatchAll() {
					*params = append(*params, Param{Key: current.catchAllKey, Value: path[charsMatched-1:]})
				}

				return current, params, false
			}
			return current, params, false
		} else if charsMatchedInNodeFound < len(current.key) {
			// Key end mid-edge
			// Tsr recommendation: add an extra trailing slash (got an exact match)
			remainingSuffix := current.key[charsMatchedInNodeFound:]
			return nil, nil, len(remainingSuffix) == 1 && remainingSuffix[0] == '/'
		}
	}

	// Incomplete match to end of edge
	if charsMatched < len(path) && charsMatchedInNodeFound == len(current.key) {
		if current.isCatchAll() {
			if !lazy {
				if params == nil {
					params = t.newParams()
				}
				*params = append(*params, Param{Key: current.catchAllKey, Value: path[charsMatched-1:]})
				if t.saveRoute {
					*params = append(*params, Param{Key: RouteKey, Value: current.path})
				}
				return current, params, false
			}
			// Same as exact match, no tsr recommendation
			return current, params, false
		}
		// Tsr recommendation: remove the extra trailing slash (got an exact match)
		remainingKeySuffix := path[charsMatched:]
		return nil, nil, len(remainingKeySuffix) == 1 && remainingKeySuffix[0] == '/'
	}

	return nil, nil, false
}

func (t *Tree) search(rootNode *node, path string) searchResult {
	current := rootNode

	var (
		pp                      *node
		p                       *node
		charsMatched            int
		charsMatchedInNodeFound int
	)

STOP:
	for charsMatched < len(path) {
		next := current.getEdge(path[charsMatched])
		if next == nil {
			break STOP
		}

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
	}
}

// addRoot is not safe for concurrent use.
func (t *Tree) addRoot(n *node) {
	nds := *t.nodes.Load()
	newNds := make([]*node, 0, len(nds)+1)
	newNds = append(newNds, nds...)
	newNds = append(newNds, n)
	t.nodes.Store(&newNds)
}

// updateRoot is not safe for concurrent use.
func (t *Tree) updateRoot(n *node) bool {
	nds := *t.nodes.Load()
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

// removeRoot is not safe for concurrent use.
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

func (t *Tree) load() []*node {
	return *t.nodes.Load()
}

func (t *Tree) newParams() *Params {
	return t.p.Get().(*Params)
}
