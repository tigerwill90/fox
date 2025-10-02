package fox

import (
	"regexp"
	"slices"
	"sort"
	"strings"
)

type node2 struct {
	// route holds the registered handler if this node is a leaf.
	route *Route

	// regexp is an optional compiled regular expression constraint for param and wildcard nodes.
	// When present, captured segments must match this pattern during lookup.
	regexp *regexp.Regexp

	// key identifies the node's content and varies by node type:
	//
	// Static nodes: Contains the literal path segment (e.g., "/users", "foo")
	//
	// Param nodes without regex: Contains "?" as a canonical placeholder.
	// This allows routes with different param names (/foo/{bar} and /foo/{fizz})
	// to share the same tree node while preventing duplicate route registration.
	// The actual placeholder names are stored in the route definition.
	//
	// Wildcard nodes without regex: Contains "*" as a canonical placeholder,
	// following the same sharing semantics as params.
	//
	// Param/wildcard nodes with regex: Contains the regex pattern string.
	// Multiple regex nodes can exist at the same level, each with a distinct pattern.
	// During lookup, patterns are evaluated in insertion order until one matches.
	key string

	// statics contains child nodes for static path segments, sorted by label byte.
	statics []*node2

	// params contains child nodes for parameters. Regex params are ordered first
	// (in insertion order), followed by at most one non-regex param node ("?").
	params []*node2

	// wildcards contains child nodes for catch-all segments. Regex wildcards are
	// ordered first (in insertion order), followed by at most one non-regex wildcard ("*").
	wildcards []*node2

	// label is the first byte of the key for static nodes, used for binary search.
	// Set to 0x00 for param and wildcard nodes.
	label byte

	// hsplit marks nodes at hostname/path boundaries. When true, this static node
	// represents the end of a hostname section, with a '/' child beginning the path.
	// This enables optimized lookups by treating the path portion as a substring.
	// Only set for static nodes; params and wildcards are already isolated.
	hsplit bool
}

// addStaticEdge inserts a static child node while maintaining sorted order by label byte.
// Uses binary search to find the insertion point.
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

// addParamEdge appends a param child node, maintaining evaluation order:
// regex params first (in insertion order), then the non-regex param ("?") last.
// This ordering ensures regex-constrained params are evaluated before the catch-all "?" param.
// Only one non-regex param is allowed per node; multiple regex params are permitted.
func (n *node2) addParamEdge(child *node2) {
	n.params = append(n.params, child)

	if child.key == "?" {
		return
	}

	idx := slices.IndexFunc(n.params, func(node *node2) bool {
		return node.key == "?"
	})

	if idx >= 0 && idx < len(n.params)-1 {
		lastIdx := len(n.params) - 1
		n.params[idx], n.params[lastIdx] = n.params[lastIdx], n.params[idx]
	}
}

// addWildcardEdge appends a wildcard child node, maintaining evaluation order:
// regex wildcards first (in insertion order), then the non-regex wildcard ("*") last.
// This ordering ensures regex-constrained wildcards are evaluated before the catch-all "*" wildcard.
// Only one non-regex wildcard is allowed per node; multiple regex wildcards are permitted.
func (n *node2) addWildcardEdge(child *node2) {
	n.wildcards = append(n.wildcards, child)

	if child.key == "*" {
		return
	}

	idx := slices.IndexFunc(n.wildcards, func(node *node2) bool {
		return node.key == "*"
	})

	if idx >= 0 && idx < len(n.wildcards)-1 {
		lastIdx := len(n.wildcards) - 1
		n.wildcards[idx], n.wildcards[lastIdx] = n.wildcards[lastIdx], n.wildcards[idx]
	}
}

// replaceStaticEdge updates an existing static child node in place.
// Uses binary search to locate the child by label, then replaces it.
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

// getStaticEdge retrieves a static child node by its label byte using binary search.
// Returns the child's index and pointer if found, or (-1, nil) if not found.
func (n *node2) getStaticEdge(label byte) (int, *node2) {
	num := len(n.statics)
	idx := sort.Search(num, func(i int) bool { return n.statics[i].label >= label })
	if idx < num && n.statics[idx].label == label {
		return idx, n.statics[idx]
	}
	return -1, nil
}

// getParamEdge retrieves a param child node by its key (either "?" or a regex pattern).
// Returns the child's index and pointer if found, or (-1, nil) if not found.
// Uses linear search since params are ordered by priority, not key.
func (n *node2) getParamEdge(key string) (int, *node2) {
	for idx, child := range n.params {
		if child.key == key {
			return idx, child
		}
	}
	return -1, nil
}

// getWildcardEdge retrieves a wildcard child node by its key (either "*" or a regex pattern).
// Returns the child's index and pointer if found, or (-1, nil) if not found.
// Uses linear search since wildcards are ordered by priority, not key.
func (n *node2) getWildcardEdge(key string) (int, *node2) {
	for idx, child := range n.wildcards {
		if child.key == key {
			return idx, child
		}
	}
	return -1, nil
}

// delStaticEdge removes a static child node by its label byte.
// Uses binary search to locate the child, then shifts remaining elements left.
// Sets the last element to nil before reslicing to allow garbage collection.
// No-op if no child with the given label exists.
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

// delParamEdge removes a param child node by its key (either "?" or a regex pattern).
// Shifts remaining elements left after removal and clears the last slot for GC.
// No-op if no param with the given key exists.
func (n *node2) delParamEdge(key string) {
	idx := slices.IndexFunc(n.params, func(p *node2) bool { return p.key == key })
	if idx >= 0 {
		copy(n.params[idx:], n.params[idx+1:])
		n.params[len(n.params)-1] = nil
		n.params = n.params[:len(n.params)-1]
	}
}

// delWildcardEdge removes a wildcard child node by its key (either "*" or a regex pattern).
// Shifts remaining elements left after removal and clears the last slot for GC.
// No-op if no wildcard with the given key exists.
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
	sb.WriteString(n.key)
	if n.label == 0 {
		sb.WriteString(" (param)")
	}
	if n.hsplit {
		sb.WriteString(" (boundary)")
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

type skipStack []skipNode

func (n *skipStack) pop() skipNode {
	skipped := (*n)[len(*n)-1]
	*n = (*n)[:len(*n)-1]
	return skipped
}

type skipNode struct {
	node               *node2
	pathIndex          int
	paramCnt           int
	childParamIndex    int
	childWildcardIndex int
}

// equalASCIIIgnoreCase performs case-insensitive comparison of two ASCII bytes.
// Only supports ASCII letters (A-Z, a-z), digits (0-9), and hyphen (-).
// Used for hostname matching where registered routes follow LDH standard.
func equalASCIIIgnoreCase2(s, t uint8) bool {
	// Easy case.
	if t == s {
		return true
	}

	// Make s < t to simplify what follows.
	if t < s {
		t, s = s, t
	}

	// ASCII only, s/t must be upper/lower case
	if 'A' <= s && s <= 'Z' && t == s+'a'-'A' {
		return true
	}

	return false
}
