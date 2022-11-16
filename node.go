package fox

import (
	"sort"
	"strings"
	"sync/atomic"
)

type node struct {
	// key represent a segment of a route which share a common prefix with it parent.
	key string

	// First char of each outgoing edges from this node sorted in ascending order.
	// Once assigned, this is a read only slice. It allows to lazily traverse the
	// tree without the extra cost of atomic load operation.
	childKeys []byte

	// Catch all key registered to retrieve this node parameter.
	// Once assigned, catchAllKey is immutable.
	catchAllKey string

	// Child nodes representing outgoing edges from this node sorted in ascending order.
	// Once assigned, this is mostly a read only slice with the exception than we can update atomically
	// each pointer reference to a new child node starting with the same character.
	children []atomic.Pointer[node]

	// The registered handler matching the full path. Nil if the node is not a leaf.
	// Once assigned, handler is immutable.
	handler Handler

	// The full path when it's a leaf node
	path string

	// Indicate whether its child node is a param node type. If true, len(children) == 1.
	// Once assigned, paramChild is immutable.
	paramChild bool
}

func newNode(key string, handler Handler, children []*node, catchAllKey string, path string) *node {
	sort.Slice(children, func(i, j int) bool {
		return children[i].key < children[j].key
	})
	nds := make([]atomic.Pointer[node], len(children))
	childKeys := make([]byte, len(children))
	for i := range children {
		assertNotNil(children[i])
		childKeys[i] = children[i].key[0]
		nds[i].Store(children[i])
	}

	return newNodeFromRef(key, handler, nds, childKeys, catchAllKey, path)
}

func newNodeFromRef(key string, handler Handler, children []atomic.Pointer[node], childKeys []byte, catchAllKey string, path string) *node {
	n := &node{
		key:         key,
		childKeys:   childKeys,
		children:    children,
		handler:     handler,
		catchAllKey: catchAllKey,
		path:        path,
	}
	if catchAllKey != "" {
		n.path += "*" + catchAllKey
	}
	return n
}

func (n *node) isLeaf() bool {
	return n.handler != nil
}

func (n *node) isCatchAll() bool {
	return n.catchAllKey != ""
}

func (n *node) getEdge(s byte) *node {
	if len(n.children) <= 4 {
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

// linearSearch return the index of s in keys or -1, using a simple loop.
// Although binary search is a more efficient search algorithm,
// the small size of the child keys array (<= 4) means that the
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

func (n *node) updateEdge(node *node) {
	id := binarySearch(n.childKeys, node.key[0])
	if id < 0 {
		panic("internal error: cannot update the edge with this node")
	}
	n.children[id].Store(node)
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
	if n.paramChild {
		sb.WriteString(" [paramChild]")
	}

	if n.isCatchAll() {
		sb.WriteString(" [static]")
	}
	if n.isLeaf() {
		sb.WriteString(" [leaf=")
		sb.WriteString(n.path)
		sb.WriteString("]")
	}

	sb.WriteByte('\n')
	children := n.getEdgesShallowCopy()
	for _, child := range children {
		sb.WriteString("  ")
		sb.WriteString(child.string(space + 2))
	}
	return sb.String()
}
