package fox

import (
	"regexp"
	"slices"
	"sort"
	"strings"
)

type node2 struct {
	route     *Route
	label     byte
	key       string
	statics   []*node2
	params    []*node2
	wildcards []*node2
	regexp    *regexp.Regexp
	// maybe we should add a reference in a parent
}

func (n *node2) addStaticEdge(child *node2) {
	num := len(n.statics)
	idx := sort.Search(num, func(i int) bool {
		return n.statics[i].label >= child.label
	})
	n.statics = append(n.statics, child)
	if idx != num {
		copy(n.statics[idx+1:], n.statics[idx:num])
		n.statics[idx] = child
	}
}

func (n *node2) addParamEdge(child *node2) {
	n.params = append(n.params, child)
}

func (n *node2) addWildcardEdge(child *node2) {
	n.wildcards = append(n.wildcards, child)
}

func (n *node2) replaceStaticEdge(child *node2) {
	num := len(n.statics)
	idx := sort.Search(num, func(i int) bool {
		return n.statics[i].label >= child.label
	})
	if idx < num && n.statics[idx].label == child.label {
		n.statics[idx] = child
		return
	}
	panic("internal error: replacing missing edge")
}

func (n *node2) getStaticEdge(label byte) (int, *node2) {
	num := len(n.statics)
	idx := sort.Search(num, func(i int) bool { return n.statics[i].label >= label })
	if idx < num && n.statics[idx].label == label {
		return idx, n.statics[idx]
	}
	return -1, nil
}

func (n *node2) getParamEdge(key string) (int, *node2) {
	for idx, child := range n.params {
		if child.key == key {
			return idx, child
		}
	}
	return -1, nil
}

func (n *node2) getWildcardEdge(key string) (int, *node2) {
	for idx, child := range n.wildcards {
		if child.key == key {
			return idx, child
		}
	}
	return -1, nil
}

func (n *node2) delStaticEdge(label byte) {
	num := len(n.statics)
	idx := sort.Search(num, func(i int) bool {
		return n.statics[i].label >= label
	})
	if idx < num && n.statics[idx].label == label {
		copy(n.statics[idx:], n.statics[idx+1:])
		n.statics[len(n.statics)-1] = nil
		n.statics = n.statics[:len(n.statics)-1]
	}
}

func (n *node2) delParamEdge(key string) {
	idx := slices.IndexFunc(n.params, func(p *node2) bool { return p.key == key })
	if idx >= 0 {
		copy(n.params[idx:], n.params[idx+1:])
		n.params[len(n.params)-1] = nil
		n.params = n.params[:len(n.params)-1]
	}
}

func (n *node2) delWildcardEdge(key string) {
	idx := slices.IndexFunc(n.wildcards, func(p *node2) bool { return p.key == key })
	if idx >= 0 {
		copy(n.wildcards[idx:], n.wildcards[idx+1:])
		n.wildcards[len(n.wildcards)-1] = nil
		n.wildcards = n.wildcards[:len(n.wildcards)-1]
	}
}

func (n *node2) search(key string) (matched *node2) {
	current := n
	search := key

	for len(search) > 0 {
		if search[0] == bracketDelim {
			end := strings.IndexByte(search, '}')
			if end == -1 {
				goto STATIC // TODO Would be an optimization to return nil, but in futur we may allow to escape special char such as *
			}
			paramName := search[:end+1]
			_, child := current.getParamEdge(paramName)
			if child == nil {
				return nil
			}
			current = child
			search = search[end+1:]
			continue
		}

		if search[0] == starDelim {
			end := strings.IndexByte(search, '}')
			if end == -1 {
				goto STATIC
			}
			paramName := search[:end+1]
			_, child := current.getWildcardEdge(paramName)
			if child == nil {
				return nil
			}
			current = child
			search = search[end+1:]
			continue
		}

	STATIC:
		label := search[0]
		_, child := current.getStaticEdge(label)
		if child == nil {
			return nil
		}
		keyLen := len(child.key)
		if keyLen > len(search) || search[:keyLen] != child.key {
			return nil
		}
		search = search[keyLen:]
		current = child
	}

	return current
}

func (n *node2) isLeaf() bool {
	return n.route != nil
}

func (n *node2) String() string {
	return n.string(0, false)
}

func (n *node2) string(space int, inode bool) string {
	sb := strings.Builder{}
	sb.WriteString(strings.Repeat(" ", space))
	sb.WriteString("path: ")
	if n.label == 0 {
		sb.WriteByte('{')
		sb.WriteString(n.key)
		sb.WriteByte('}')
	} else {
		sb.WriteString(n.key)
	}

	if n.isLeaf() {
		sb.WriteString(" [leaf=")
		sb.WriteString(n.route.pattern)
		sb.WriteString(", label=")
		sb.WriteByte(n.label)
		sb.WriteString("]")
	}

	sb.WriteByte('\n')

	for _, child := range n.statics {
		sb.WriteString("  ")
		sb.WriteString(child.string(space+4, inode))
	}
	for _, child := range n.params {
		sb.WriteString("  ")
		sb.WriteString(child.string(space+4, inode))
	}
	for _, child := range n.wildcards {
		sb.WriteString("  ")
		sb.WriteString(child.string(space+4, inode))
	}
	return sb.String()
}
