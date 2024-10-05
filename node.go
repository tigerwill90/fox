// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
)

type node struct {
	// The registered route matching the full path. Nil if the node is not a leaf.
	// Once assigned, route is immutable.
	route *Route

	// key represent a segment of a route which share a common prefix with it parent.
	key string

	// Catch all key registered to retrieve this node parameter.
	// Once assigned, catchAllKey is immutable.
	catchAllKey string

	// First char of each outgoing edges from this node sorted in ascending order.
	// Once assigned, this is a read only slice. It allows to lazily traverse the
	// tree without the extra cost of atomic load operation.
	childKeys []byte

	// Child nodes representing outgoing edges from this node sorted in ascending order.
	// Once assigned, this is mostly a read only slice with the exception than we can update atomically
	// each pointer reference to a new child node starting with the same character.
	children []atomic.Pointer[node]

	params []param

	// The index of a paramChild if any, -1 if none (per rules, only one paramChildren is allowed).
	paramChildIndex int
}

func newNode(key string, route *Route, children []*node, catchAllKey string) *node {
	slices.SortFunc(children, func(a, b *node) int {
		return cmp.Compare(a.key, b.key)
	})
	nds := make([]atomic.Pointer[node], len(children))
	childKeys := make([]byte, len(children))
	childIndex := -1
	for i := range children {
		assertNotNil(children[i])
		childKeys[i] = children[i].key[0]
		nds[i].Store(children[i])
		if strings.HasPrefix(children[i].key, "{") {
			childIndex = i
		}
	}

	return newNodeFromRef(key, route, nds, childKeys, catchAllKey, childIndex)
}

func newNodeFromRef(key string, route *Route, children []atomic.Pointer[node], childKeys []byte, catchAllKey string, childIndex int) *node {
	return &node{
		key:             key,
		childKeys:       childKeys,
		children:        children,
		route:           route,
		catchAllKey:     catchAllKey,
		paramChildIndex: childIndex,
		params:          parseWildcard(key),
	}
}

func (n *node) isLeaf() bool {
	return n.route != nil
}

func (n *node) isCatchAll() bool {
	return n.catchAllKey != ""
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
		return n.children[id].Load()
	}
	id := binarySearch(n.childKeys, s)
	if id < 0 {
		return nil
	}
	return n.children[id].Load()
}

func (n *node) updateEdge(node *node) {
	if len(n.children) <= 50 {
		id := linearSearch(n.childKeys, node.key[0])
		if id < 0 {
			panic("internal error: cannot update the edge with this node")
		}
		n.children[id].Store(node)
		return
	}
	id := binarySearch(n.childKeys, node.key[0])
	if id < 0 {
		panic("internal error: cannot update the edge with this node")
	}
	n.children[id].Store(node)
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

func (n *node) get(i int) *node {
	return n.children[i].Load()
}

func (n *node) getEdgesShallowCopy() []*node {
	nodes := make([]*node, len(n.children))
	for i := range n.children {
		nodes[i] = n.get(i)
	}
	return nodes
}

// assertNotNil is a safeguard against creating unsafe.Pointer(nil).
func assertNotNil(n *node) {
	if n == nil {
		panic("internal error: a node cannot be nil")
	}
}

func (n *node) String() string {
	return n.string(0)
}

func (n *node) string(space int) string {
	sb := strings.Builder{}
	sb.WriteString(strings.Repeat(" ", space))
	sb.WriteString("path: ")
	sb.WriteString(n.key)

	if n.paramChildIndex >= 0 {
		sb.WriteString(" [paramIdx=")
		sb.WriteString(strconv.Itoa(n.paramChildIndex))
		sb.WriteByte(']')
		if n.hasWildcard() {
			sb.WriteString(" [")
			for i, param := range n.params {
				if i > 0 {
					sb.WriteByte(',')
				}
				sb.WriteString(param.key)
				sb.WriteString(" (")
				sb.WriteString(strconv.Itoa(param.end))
				sb.WriteString(")")
			}
			sb.WriteString("]")
		}

	}

	if n.isCatchAll() {
		sb.WriteString(" [catchAll]")
	}
	if n.isLeaf() {
		sb.WriteString(" [leaf=")
		sb.WriteString(n.route.path)
		sb.WriteString("]")
	}
	if n.hasWildcard() {
		sb.WriteString(" [")
		for i, param := range n.params {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(param.key)
			sb.WriteString(" (")
			sb.WriteString(strconv.Itoa(param.end))
			sb.WriteString(")")
		}
		sb.WriteByte(']')
	}

	sb.WriteByte('\n')
	children := n.getEdgesShallowCopy()
	for _, child := range children {
		sb.WriteString("  ")
		sb.WriteString(child.string(space + 4))
	}
	return sb.String()
}

type skippedNodes []skippedNode

func (n *skippedNodes) pop() skippedNode {
	skipped := (*n)[len(*n)-1]
	*n = (*n)[:len(*n)-1]
	return skipped
}

type skippedNode struct {
	n         *node
	pathIndex int
	paramCnt  uint32
	seen      bool
}

func appendCatchAll(path, catchAllKey string) string {
	if catchAllKey != "" {
		suffix := "*{" + catchAllKey + "}"
		if !strings.HasSuffix(path, suffix) {
			return path + suffix
		}
	}
	return path
}

// param represents a parsed parameter and its end position in the path.
type param struct {
	key string
	end int // -1 if end with {a}, else pos of the next char
	// catchAll bool
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
			//case stateCatchAll:
			//if segment[i] == '}' {
			//	end := -1
			//	if len(segment[i+1:]) > 0 {
			//		end = i + 1
			//	}
			//	params = append(params, param{
			//		key:      segment[start:i],
			//		end:      end,
			//		catchAll: true,
			//	})
			//	start = 0
			//	state = stateDefault
			//}
			//i++
		default:
			//	if segment[i] == '*' {
			//	state = stateCatchAll
			//	i += 2
			//	start = i
			//	continue
			//}

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
