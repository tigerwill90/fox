package fox

import (
	"fmt"
	"strings"

	"github.com/tigerwill90/fox/internal/simplelru"
)

type tXn2 struct {
	writable  *simplelru.LRU[*node2, struct{}]
	root      *node2
	size      int
	maxParams uint32
	depth     uint32
}

// insert perform a recursive insertion of the route in the tree.
func (t *tXn2) insert(key string, route *Route) {
	segments, _ := tokenizePath(key)
	fmt.Println(segments)
	newRoot := t.insertSegments(t.root, segments, route)
	if newRoot != nil {
		t.root = newRoot
	}
	t.size++
}

func (t *tXn2) insertSegments(n *node2, segments []segment, route *Route) *node2 {

	// Base case: no segments left, attach route
	if len(segments) == 0 {
		nc := t.writeNode(n)
		nc.route = route
		return nc
	}

	seg := segments[0]
	remaining := segments[1:]

	switch seg.typ {
	case segmentStatic:
		return t.insertStatic(n, seg.value, remaining, route)
	case segmentParam:
		return t.insertParam(n, seg.value, remaining, route)
	case segmentWildcard:
		return t.insertWildcard(n, seg.value, remaining, route)
	default:
		panic("unknown segment type")
	}

	return nil
}

func (t *tXn2) insertStatic(n *node2, search string, remaining []segment, route *Route) *node2 {

	if len(search) == 0 {
		return t.insertSegments(n, remaining, route)
	}

	idx, child := n.getStaticEdge(search[0])
	if child == nil {
		newChild := t.insertSegments(&node2{
			label: search[0],
			key:   search,
		}, remaining, route)
		// TODO maybe check nil
		if newChild != nil {
			nc := t.writeNode(n)
			nc.addStaticEdge(newChild)
			return nc
		}
		return nil
	}

	commonPrefix := longestPrefix(search, child.key)
	if commonPrefix == len(child.key) {
		search = search[commonPrefix:]
		remaining = append([]segment{{segmentStatic, search}}, remaining...)
		// e.g. child /foo and want insert /fooo, insert "o"
		newChild := t.insertSegments(child, remaining, route)
		if newChild != nil {
			nc := t.writeNode(n)
			nc.statics[idx] = newChild
			return nc
		}
		return nil
	}

	nc := t.writeNode(n)
	splitNode := &node2{
		label: search[0],
		key:   search[:commonPrefix],
	}
	nc.replaceStaticEdge(splitNode)

	modChild := t.writeNode(child)
	modChild.label = modChild.key[commonPrefix]
	modChild.key = modChild.key[commonPrefix:]
	splitNode.addStaticEdge(modChild)

	search = search[commonPrefix:]
	if len(search) == 0 {
		// e.g. we have /foo and want to insert /fo,
		// we first split /foo into /fo, o and then fo <- get the new route
		// splitNode.route = route // SHOULD not need this
		if len(remaining) > 0 {
			newSplitNode := t.insertSegments(splitNode, remaining, route)
			if newSplitNode != nil {
				nc.replaceStaticEdge(newSplitNode)
				return nc
			}
			return nil
		}
		splitNode.route = route
		return nc
	}
	// e.g. we have /foo and want to insert /fob
	// we first have our splitNode /fo, with old child (modChild) equal o, and insert the edge b

	newChild := t.insertSegments(&node2{
		label: search[0],
		key:   search,
	}, remaining, route)
	if newChild != nil {
		splitNode.addStaticEdge(newChild)
		return nc
	}

	return nil
}

func (t *tXn2) insertParam(n *node2, key string, remaining []segment, route *Route) *node2 {
	idx, child := n.getParamEdge(key)
	if child == nil {
		newChild := t.insertSegments(
			&node2{
				key: key,
			},
			remaining,
			route,
		)
		nc := t.writeNode(n)
		nc.addParamEdge(newChild)
		return nc
	}

	newChild := t.insertSegments(child, remaining, route)
	if newChild != nil {
		nc := t.writeNode(n)
		nc.params[idx] = newChild
		return nc
	}
	return nil
}

func (t *tXn2) insertWildcard(n *node2, key string, remaining []segment, route *Route) *node2 {
	idx, child := n.getWildcardEdge(key)
	if child == nil {
		newChild := t.insertSegments(
			&node2{
				key: key,
			},
			remaining,
			route,
		)
		nc := t.writeNode(n)
		nc.addWildcardEdge(newChild)
		return nc
	}

	newChild := t.insertSegments(child, remaining, route)
	if newChild != nil {
		nc := t.writeNode(n)
		nc.wildcards[idx] = newChild
		return nc
	}
	return nil
}

func (t *tXn2) writeNode(n *node2) *node2 {
	if t.writable == nil {
		lru, err := simplelru.NewLRU[*node2, struct{}](defaultModifiedCache, nil)
		if err != nil {
			panic(err)
		}
		t.writable = lru
	}

	if _, ok := t.writable.Get(n); ok {
		return n
	}

	nc := &node2{
		label: n.label,
		key:   n.key,
		route: n.route,
	}
	if len(n.statics) != 0 {
		nc.statics = make([]*node2, len(n.statics))
		copy(nc.statics, n.statics)
	}
	if len(n.params) != 0 {
		nc.params = make([]*node2, len(n.params))
		copy(nc.params, n.params)
	}
	if len(n.wildcards) != 0 {
		nc.wildcards = make([]*node2, len(n.wildcards))
		copy(nc.wildcards, n.wildcards)
	}

	t.writable.Add(nc, struct{}{})
	return nc
}

func (t *tXn2) delete(n *node2, search string) (*node2, *Route) {
	if len(search) == 0 {
		if !n.isLeaf() {
			return nil, nil
		}

		oldRoute := n.route

		nc := t.writeNode(n)
		nc.route = nil
		if n != t.root && len(nc.statics) == 1 {
			t.mergeChild(nc)
		}
		return nc, oldRoute
	}

	// Look for an edge
	label := search[0]
	idx, child := n.getStaticEdge(label)
	if child == nil || !strings.HasPrefix(search, child.key) {
		return nil, nil
	}

	search = search[len(child.key):]
	newChild, route := t.delete(child, search)
	if newChild == nil {
		return nil, nil
	}

	nc := t.writeNode(n)
	if newChild.route == nil && len(newChild.statics) == 0 {
		nc.delStaticEdge(label)
		if n != t.root && len(nc.statics) == 1 && !nc.isLeaf() {
			t.mergeChild(nc)
		}
	} else {
		nc.statics[idx] = newChild
	}
	return nc, route
}

// mergeChild is called to collapse the given node with its child. This is only
// called when the given node is not a leaf and has a single edge.
func (t *tXn2) mergeChild(n *node2) {
	child := n.statics[0]

	// Merge nodes
	n.key = concat(n.key, child.key)
	n.route = child.route
	if len(child.statics) != 0 {
		n.statics = make([]*node2, len(child.statics))
		copy(n.statics, child.statics)
	} else {
		n.statics = nil
	}
}

// longestPrefix finds the length of the shared prefix of two strings
func longestPrefix(k1, k2 string) int {
	max := len(k1)
	if l := len(k2); l < max {
		max = l
	}
	var i int
	for i = 0; i < max; i++ {
		if k1[i] != k2[i] {
			break
		}
	}
	return i
}

// concat two string
func concat(a, b string) string {
	return a + b
}

type segmentType int

const (
	segmentStatic segmentType = iota
	segmentParam
	segmentWildcard
)

type segment struct {
	typ   segmentType
	value string
}

// tokenizePath splits a path into segments, separating static portions from
// dynamic parameters ({placeholder}) and catch-all parameters ({placeholder...}).
//
// Examples:
//
//	/a/b/c/{bar}/baz       → ["a/b/c/", "{bar}", "/baz"]
//	/a/{foo}/b/{bar...}    → ["a/", "{foo}", "/b/", "{bar...}"]
//	/{id}/track            → ["", "{id}", "/track"]
//	/static                → ["static"]
func tokenizePath(path string) ([]segment, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("empty path")
	}

	var segments []segment
	var staticBuf strings.Builder
	i := 0

	for i < len(path) {
		if path[i] == '{' {
			// Flush any accumulated static content
			if staticBuf.Len() > 0 {
				segments = append(segments, segment{
					typ:   segmentStatic,
					value: staticBuf.String(),
				})
				staticBuf.Reset()
			}

			// Find closing brace
			start := i
			i++
			for i < len(path) && path[i] != '}' {
				i++
			}

			if i >= len(path) {
				return nil, fmt.Errorf("unclosed parameter at position %d", start)
			}

			// Extract parameter content (including braces)
			param := path[start : i+1]
			i++ // Move past closing brace

			// Check if it's a catch-all (ends with ...)
			if strings.HasSuffix(param, "...}") {
				segments = append(segments, segment{
					typ:   segmentWildcard,
					value: param,
				})
			} else {
				segments = append(segments, segment{
					typ:   segmentParam,
					value: param,
				})
			}
		} else {
			// Accumulate static content
			staticBuf.WriteByte(path[i])
			i++
		}
	}

	// Flush any remaining static content
	if staticBuf.Len() > 0 {
		segments = append(segments, segment{
			typ:   segmentStatic,
			value: staticBuf.String(),
		})
	}

	return segments, nil
}
