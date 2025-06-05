// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
	"github.com/tigerwill90/fox/internal/netutil"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

type roots []*node

func (r roots) methodIndex(method string) int {
	// Nodes for common http method are pre instantiated.
	switch method {
	case http.MethodGet:
		return 0
	case http.MethodPost:
		return 1
	case http.MethodPut:
		return 2
	case http.MethodDelete:
		return 3
	}

	for i, nd := range r[verb:] {
		if nd.key == method {
			return i + verb
		}
	}
	return -1
}

func (r roots) search(rootNode *node, path string) (matched *node) {
	current := rootNode

	var (
		charsMatched            int
		charsMatchedInNodeFound int
	)

STOP:
	for charsMatched < len(path) {
		next := current.getEdge(path[charsMatched])
		if next == nil {
			break STOP
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

	if charsMatched == len(path) {
		// Exact match
		if charsMatchedInNodeFound == len(current.key) {
			return current
		}
		// Key end mid-edge
		if charsMatchedInNodeFound < len(current.key) {
			return current
		}
	}
	return nil
}

// lookup  returns the node matching the host and/or path. If lazy is false, it parses and record into c, path segment according to
// the route definition. In case of indirect match, tsr is true and n != nil.
func (r roots) lookup(t *iTree, method, hostPort, path string, c *cTx, lazy bool) (n *node, tsr bool) {
	index := r.methodIndex(method)
	if index < 0 || len(r[index].children) == 0 {
		return nil, false
	}

	// The tree for this method only have path registered
	if len(r[index].children) == 1 && r[index].childKeys[0] == '/' {
		return lookupByPath(t, r[index].children[0], path, c, lazy)
	}

	host := netutil.StripHostPort(hostPort)
	if host != "" {
		// Try first by domain
		n, tsr = lookupByDomain(t, r[index], host, path, c, lazy)
		if n != nil {
			return n, tsr
		}
	}

	// Fallback by path
	idx := linearSearch(r[index].childKeys, '/')
	if idx < 0 {
		return nil, false
	}

	// Reset any recorded params and tsrParams
	*c.params = (*c.params)[:0]
	c.tsr = false

	return lookupByPath(t, r[index].children[idx], path, c, lazy)
}

// lookupByDomain is like lookupByPath, but for target with hostname.
func lookupByDomain(tree *iTree, target *node, host, path string, c *cTx, lazy bool) (n *node, tsr bool) {
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
			current = target.children[idx]
		} else {
			return
		}
	} else {
		// Here we have a next static segment and possibly wildcard children, so we save them for later evaluation if needed.
		if target.paramChildIndex >= 0 {
			*c.skipNds = append(*c.skipNds, skippedNode{target, charsMatched, paramCnt, target.paramChildIndex})
		}
		current = target.children[idx]
	}

	subCtx := tree.ctx.Get().(*cTx)
	defer tree.ctx.Put(subCtx)

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
					current = current.children[idx]
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

			current = current.children[idx]
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
		subNode, subTsr := lookupByPath(tree, current.children[idx], path, subCtx, lazy)
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
		if !lazy /*&& len(*subCtx.params) > 0*/ {
			*c.params = append(*c.params, *subCtx.params...)
		}

		return subNode, subTsr
	}

Backtrack:
	if hasSkpNds {
		skipped := c.skipNds.pop()

		current = skipped.n.children[skipped.childIndex]

		*c.params = (*c.params)[:skipped.paramCnt]
		charsMatched = skipped.pathIndex
		goto Walk
	}

	return n, tsr
}

// lookupByPath returns the node matching the path. If lazy is false, it parses and record into c, path segment according to
// the route definition. In case of indirect match, tsr is true and n != nil.
func lookupByPath(tree *iTree, target *node, path string, c *cTx, lazy bool) (n *node, tsr bool) {
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
						// key end with *{foo}, so we are sure to have only one children staring by '/'
						// e.g. /*{foo} and /*{foo}/bar
						inode = current.children[0]
						charsMatchedInNodeFound += len(current.key[charsMatchedInNodeFound:])
					} else {
						// We are in an ending catch all node with no child, so it's a direct match
						if !lazy {
							*c.params = append(*c.params, Param{Key: current.params[paramKeyCnt].key, Value: path[charsMatched:]})
						}
						return current, false
					}

					subCtx := tree.ctx.Get().(*cTx)
					startPath := charsMatched
					for {
						idx = strings.IndexByte(path[charsMatched:], slashDelim)
						// idx >= 0, we have a next segment with at least one char
						if idx > 0 {
							*subCtx.params = (*subCtx.params)[:0]
							charsMatched += idx
							subNode, subTsr := lookupByPath(tree, inode, path[charsMatched:], subCtx, false)
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

							tree.ctx.Put(subCtx)
							return subNode, subTsr
						}

						tree.ctx.Put(subCtx)

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
					current = current.children[idx]
					paramKeyCnt = 0
					continue
				}
				if current.wildcardChildIndex >= 0 {
					// We have a wildcard child, go deeper
					idx = current.wildcardChildIndex
					parent = current
					current = current.children[idx]
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
			current = current.children[idx]
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
					if parent != nil && parent.isLeaf() && len(remainingPrefix) == 1 && remainingPrefix[0] == slashDelim {
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
		current = skipped.n.children[skipped.childIndex]

		*c.params = (*c.params)[:skipped.paramCnt]
		charsMatched = skipped.pathIndex
		goto Walk
	}

	return n, tsr
}

type node struct {
	// The registered route matching the full path. Nil if the node is not a leaf.
	// Once assigned, route is immutable.
	route *Route

	// Precomputed inode for infix wildcard lookup, so we don't have to create it during the lookup phase.
	// Inode is this node, but with the key split after the first infix wildcard (e.g. /foo/*{bar}/baz => /baz).
	// Once assigned, inode is immutable.
	inode *node

	// key represent a segment of a route which share a common prefix with it parent.
	// Once assigned, key is immutable.
	key string

	// First char of each outgoing edges from this node sorted in ascending order.
	// Once assigned, this is a read only slice. It allows to lazily traverse the
	// tree without the extra cost of atomic load operation.
	childKeys []byte

	// Child nodes representing outgoing edges from this node sorted in ascending order.
	// Once assigned, this is mostly a read only slice with the exception than we can update
	// each pointer reference to a new child node starting with the same character.
	children []*node

	params []param

	// The index of a paramChild if any, -1 if none (per rules, only one paramChildren is allowed).
	// Once assigned, paramChildIndex is immutable.
	paramChildIndex int
	// The index of a wildcardChild if any, -1 if none (per rules, only one wildcardChild is allowed).
	// Once assigned, wildcardChildIndex is immutable.
	wildcardChildIndex int
}

// newNode create a new node. Note that is sort in place children, so it should NEVER be a slice from reference.
func newNode(key string, route *Route, children []*node) *node {
	slices.SortFunc(children, func(a, b *node) int {
		return cmp.Compare(a.key, b.key)
	})
	childKeys := make([]byte, len(children))
	paramChildIndex := -1
	wildcardChildIndex := -1
	for i := range children {
		childKeys[i] = children[i].key[0]
		if strings.HasPrefix(children[i].key, "{") {
			paramChildIndex = i
			continue
		}
		if strings.HasPrefix(children[i].key, "*") {
			wildcardChildIndex = i
		}
	}
	return newNodeFromRef(key, route, children, childKeys, paramChildIndex, wildcardChildIndex)
}

func newNodeFromRef(key string, route *Route, children []*node, childKeys []byte, paramChildIndex, wildcardChildIndex int) *node {

	var next *node
	params := parseWildcard(key)
	for _, p := range params {
		if p.catchAll && p.end >= 0 {
			next = newNodeFromRef(key[p.end:], route, children, childKeys, paramChildIndex, wildcardChildIndex)
			break
		}
	}

	return &node{
		key:                key,
		childKeys:          childKeys,
		children:           children,
		route:              route,
		inode:              next,
		paramChildIndex:    paramChildIndex,
		wildcardChildIndex: wildcardChildIndex,
		params:             params,
	}
}

func (n *node) isLeaf() bool {
	return n.route != nil
}

func (n *node) hasWildcard() bool {
	return len(n.params) > 0
}

func (n *node) getEdge(s byte) *node {
	if len(n.children) <= 50 {
		id := linearSearch(n.childKeys, s)
		if id < 0 {
			return nil
		}
		return n.children[id]
	}
	id := binarySearch(n.childKeys, s)
	if id < 0 {
		return nil
	}
	return n.children[id]
}

func (n *node) updateEdge(node *node) {
	if len(n.children) <= 50 {
		id := linearSearch(n.childKeys, node.key[0])
		if id < 0 {
			panic("internal error: cannot update the edge with this node")
		}
		n.children[id] = node
		return
	}
	id := binarySearch(n.childKeys, node.key[0])
	if id < 0 {
		panic("internal error: cannot update the edge with this node")
	}
	n.children[id] = node
}

// clone returns a copy of the nodes.
func (n *node) clone() *node {
	children := make([]*node, len(n.children))
	copy(children, n.children)
	// We need to recalculate inode.
	return newNodeFromRef(n.key, n.route, children, n.childKeys, n.paramChildIndex, n.wildcardChildIndex)
}

// getEdges returns a copy of children.
func (n *node) getEdges() []*node {
	children := make([]*node, len(n.children))
	copy(children, n.children)
	return children
}

func (n *node) String() string {
	return n.string(0, false)
}

func (n *node) Debug() string {
	return n.string(0, true)
}

func (n *node) string(space int, inode bool) string {
	sb := strings.Builder{}
	sb.WriteString(strings.Repeat(" ", space))
	sb.WriteString("path: ")
	sb.WriteString(n.key)

	if n.paramChildIndex >= 0 {
		sb.WriteString(" [paramIdx=")
		sb.WriteString(strconv.Itoa(n.paramChildIndex))
		sb.WriteByte(']')
	}

	if n.wildcardChildIndex >= 0 {
		sb.WriteString(" [wildcardIdx=")
		sb.WriteString(strconv.Itoa(n.wildcardChildIndex))
		sb.WriteByte(']')
	}

	if n.isLeaf() {
		sb.WriteString(" [leaf=")
		sb.WriteString(n.route.pattern)
		sb.WriteString("]")
	}
	if n.hasWildcard() {
		sb.WriteString(" [")
		for i, param := range n.params {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(param.key)
			sb.WriteString(" (")
			sb.WriteString(strconv.Itoa(param.end))
			sb.WriteString(")")
		}
		sb.WriteByte(']')
	}

	sb.WriteByte('\n')

	if inode {
		next := n.inode
		addSpace := space + 8
		for next != nil {
			sb.WriteString(strings.Repeat(" ", addSpace))
			sb.WriteString(" inode: ")
			sb.WriteString(next.key)
			if len(next.children) > 0 {
				sb.WriteString(" [child=")
				sb.WriteString(strconv.Itoa(len(next.children)))
				sb.WriteByte(']')
			}
			sb.WriteByte('\n')
			//children := next.getEdges()
			//for _, child := range children {
			//	sb.WriteString("  ")
			//	sb.WriteString(child.string(addSpace+4, false))
			//}
			addSpace += 8
			next = next.inode
		}
	}

	for _, child := range n.children {
		sb.WriteString("  ")
		sb.WriteString(child.string(space+4, inode))
	}
	return sb.String()
}

// linearSearch return the index of s in keys or -1, using a simple loop.
// Although binary search is a more efficient search algorithm,
// the small size of the child keys array means that the
// constant factor will dominate (cf Adaptive Radix Tree algorithm).
func linearSearch(keys []byte, s byte) int {
	for i := 0; i < len(keys); i++ {
		if keys[i] == s {
			return i
		}
	}
	return -1
}

// binarySearch return the index of s in keys or -1.
func binarySearch(keys []byte, s byte) int {
	low, high := 0, len(keys)-1
	for low <= high {
		// nolint:gosec
		mid := int(uint(low+high) >> 1) // avoid overflow
		cmp := compare(keys[mid], s)
		if cmp < 0 {
			low = mid + 1
		} else if cmp > 0 {
			high = mid - 1
		} else {
			return mid
		}
	}
	return -(low + 1)
}

func compare(a, b byte) int {
	if a == b {
		return 0
	}
	if a < b {
		return -1
	}
	return +1
}

type skippedNodes []skippedNode

func (n *skippedNodes) pop() skippedNode {
	skipped := (*n)[len(*n)-1]
	*n = (*n)[:len(*n)-1]
	return skipped
}

type skippedNode struct {
	n          *node
	pathIndex  int
	paramCnt   uint32
	childIndex int
}

// param represents a parsed parameter and its end position in the path.
type param struct {
	key      string
	end      int // -1 if end with {a}, else pos of the next char.
	catchAll bool
}

func parseWildcard(segment string) []param {
	var params []param

	state := stateDefault
	start := 0
	i := 0
	for i < len(segment) {
		switch state {
		case stateParam:
			if segment[i] == '}' {
				end := -1
				if len(segment[i+1:]) > 0 {
					end = i + 1
				}
				params = append(params, param{
					key: segment[start:i],
					end: end,
				})
				start = 0
				state = stateDefault
			}
			i++
		case stateCatchAll:
			if segment[i] == '}' {
				end := -1
				if len(segment[i+1:]) > 0 {
					end = i + 1
				}
				params = append(params, param{
					key:      segment[start:i],
					end:      end,
					catchAll: true,
				})
				start = 0
				state = stateDefault
			}
			i++
		default:
			if segment[i] == '*' {
				state = stateCatchAll
				i += 2
				start = i
				continue
			}

			if segment[i] == '{' {
				state = stateParam
				i++
				start = i
				continue
			}
			i++
		}
	}

	return params
}
