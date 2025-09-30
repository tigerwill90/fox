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

func (t *tXn2) Insert(key string, route *Route) {
	segments, _ := tokenizePath(key)
	newRoot := t.insert(t.root, key, segments, route)
	if newRoot != nil {
		t.root = newRoot
	}
}

func (t *tXn2) insert(n *node2, search string, segments []segment, route *Route) *node2 {

	if len(search) == 0 {
		nc := t.writeNode(n)
		nc.route = route
		return nc
	}

	idx, child := n.getStaticEdge(search[0])
	if child == nil {
		leaf := &node2{
			label: search[0],
			key:   search,
			route: route,
		}
		nc := t.writeNode(n)
		nc.addStaticEdge(leaf)
		return nc
	}

	commonPrefix := longestPrefix(search, child.key)
	if commonPrefix == len(child.key) {
		search = search[commonPrefix:]
		newChild := t.insert(child, search, segments, route)
		if newChild != nil {
			nc := t.writeNode(n)
			nc.statics[idx] = newChild
			return nc
		}
	}

	nc := t.writeNode(n)
	splitNode := &node2{
		label: search[0],
		key:   search[:commonPrefix],
	}
	nc.replaceStaticEdge(splitNode)

	modChild := t.writeNode(child)
	splitNode.addStaticEdge(modChild)
	modChild.label = modChild.key[commonPrefix]
	modChild.key = modChild.key[commonPrefix:]

	search = search[commonPrefix:]
	if len(search) == 0 {
		splitNode.route = route
		return nc
	}

	splitNode.addStaticEdge(&node2{
		label: search[0],
		key:   search,
		route: route,
	})
	return nc
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

// longestPrefix finds the length of the shared prefix
// of two strings
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

type segmentType int

const (
	segmentStatic segmentType = iota
	segmentParam
	segmentCatchAll
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
					typ:   segmentCatchAll,
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
