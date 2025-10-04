package fox

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/tigerwill90/fox/internal/netutil"
	"github.com/tigerwill90/fox/internal/stringutil"
)

type root map[string]*node

func (rt root) lookup(tree *iTree, method, hostPort, path string, c *cTx, lazy bool) (n *node, tsr bool) {
	root := rt[method]
	if root == nil {
		return nil, false
	}

	// The tree for this method, we only have path registered
	if len(root.params) == 0 && len(root.statics) == 1 && root.statics[0].label == slashDelim {
		return lookupByPath(tree, root, path, c, lazy)
	}

	host := netutil.StripHostPort(hostPort)
	if host != "" {
		// Try first by domain
		n, tsr = lookupByHostname(tree, root, host, path, c, lazy)
		if n != nil {
			return n, tsr
		}
	}

	// Fallback by path and reset any recorded params and tsrParams
	*c.params = (*c.params)[:0]
	c.tsr = false

	return lookupByPath(tree, root, path, c, lazy)
}

func lookupByHostname(tree *iTree, root *node, host, path string, c *cTx, lazy bool) (n *node, tsr bool) {
	var (
		charsMatched  int
		skipStatic    bool
		childParamIdx int
	)

	matched := root
	search := host
	*c.skipStack = (*c.skipStack)[:0]

	subCtx := tree.pool.Get().(*cTx)
	defer tree.pool.Put(subCtx)

Walk:
	for len(search) > 0 {
		if !skipStatic {
			label := stringutil.ToLowerASCII(search[0])
			num := len(matched.statics)
			idx := sort.Search(num, func(i int) bool { return matched.statics[i].label >= label })
			if idx < num && matched.statics[idx].label == label {
				child := matched.statics[idx]
				keyLen := len(child.key)
				if keyLen <= len(search) && stringutil.EqualStringsASCIIIgnoreCase(search[:keyLen], child.key) {
					if len(matched.params) > 0 {
						*c.skipStack = append(*c.skipStack, skipNode{
							node:      matched,
							pathIndex: charsMatched,
							paramCnt:  len(*c.params),
						})
					}

					matched = child
					search = search[keyLen:]
					charsMatched += keyLen
					childParamIdx = 0
					continue
				}
			}
		}

		skipStatic = false
		params := matched.params[childParamIdx:]
		if len(params) > 0 {
			end := strings.IndexByte(search, dotDelim)
			if end == -1 {
				end = len(search)
			}

			if end == 0 {
				goto Backtrack
			}

			segment := search[:end]

			for i, paramNode := range params {
				if paramNode.regexp != nil {
					if !paramNode.regexp.MatchString(segment) {
						continue
					}
				}

				nextChildIx := i + 1
				if nextChildIx < len(params) {
					*c.skipStack = append(*c.skipStack, skipNode{
						node:          matched,
						pathIndex:     charsMatched,
						paramCnt:      len(*c.params),
						childParamIdx: nextChildIx + childParamIdx,
					})
				}

				if !lazy {
					*c.params = append(*c.params, segment)
				}

				matched = paramNode
				search = search[end:]
				charsMatched += end
				childParamIdx = 0
				goto Walk
			}
		}

		childParamIdx = 0
		goto Backtrack
	}

	if _, pathChild := matched.getStaticEdge(slashDelim); pathChild != nil {
		*subCtx.params = (*subCtx.params)[:0]
		subNode, subTsr := lookupByPath(tree, matched, path, subCtx, lazy)
		if subNode != nil {
			if subTsr {
				if !tsr {
					tsr = true
					n = subNode
					if !lazy {
						*c.tsrParams = (*c.tsrParams)[:0]
						*c.tsrParams = append(*c.tsrParams, *c.params...)
						*c.tsrParams = append(*c.tsrParams, *subCtx.tsrParams...)
					}
				}
			} else {
				if !lazy {
					*c.params = append(*c.params, *subCtx.params...)
				}
				return subNode, false
			}
		}
	}

Backtrack:
	if len(*c.skipStack) == 0 {
		return n, tsr
	}

	skipped := c.skipStack.pop()

	if skipped.childParamIdx < len(skipped.node.params) {
		matched = skipped.node
		*c.params = (*c.params)[:skipped.paramCnt]
		search = host[skipped.pathIndex:]

		charsMatched = skipped.pathIndex
		skipStatic = true
		childParamIdx = skipped.childParamIdx

		goto Walk
	}

	return n, tsr

}

func lookupByPath(tree *iTree, root *node, path string, c *cTx, lazy bool) (n *node, tsr bool) {

	var (
		charsMatched  int
		skipStatic    bool
		childParamIdx int
		parent        *node
	)

	matched := root
	search := path
	*c.skipStack = (*c.skipStack)[:0]

Walk:
	for len(search) > 0 {
		if !skipStatic {
			label := search[0]
			num := len(matched.statics)
			idx := sort.Search(num, func(i int) bool { return matched.statics[i].label >= label })
			if idx < num && matched.statics[idx].label == label {
				child := matched.statics[idx]
				keyLen := len(child.key)
				if keyLen <= len(search) && search[:keyLen] == child.key {
					if len(matched.params) > 0 || len(matched.wildcards) > 0 {
						*c.skipStack = append(*c.skipStack, skipNode{
							node:      matched,
							parent:    parent,
							pathIndex: charsMatched,
							paramCnt:  len(*c.params),
						})
					}

					parent = matched
					matched = child
					search = search[keyLen:]
					charsMatched += keyLen
					continue
				}

				if !tsr && child.route != nil && strings.HasPrefix(child.key, search) {
					remaining := child.key[len(search):]
					if remaining == "/" {
						tsr = true
						n = child
						if !lazy {
							copyWithResize(c.tsrParams, c.params)
						}
					}
				}
			}
		}

		skipStatic = false
		params := matched.params[childParamIdx:]
		if len(params) > 0 {
			end := strings.IndexByte(search, slashDelim)
			if end == -1 {
				end = len(search)
			}

			// Empty segment, so no more params to eval at this level, but we
			// may have wildcards
			if end == 0 {
				if len(matched.wildcards) > 0 {
					*c.skipStack = append(*c.skipStack, skipNode{
						node:          matched,
						parent:        parent,
						pathIndex:     charsMatched,
						paramCnt:      len(*c.params),
						childParamIdx: len(matched.params), // Don't visit params again
					})
				}
				goto Backtrack
			}

			segment := search[:end]
			for i, paramNode := range params {
				if paramNode.regexp != nil && !paramNode.regexp.MatchString(segment) {
					continue
				}

				// Save other params/wildcards for backtracking (only params after matched + all wildcards)
				nextChildIx := i + 1
				if nextChildIx < len(params) || len(matched.wildcards) > 0 {
					*c.skipStack = append(*c.skipStack, skipNode{
						node:          matched,
						parent:        parent,
						pathIndex:     charsMatched,
						paramCnt:      len(*c.params),
						childParamIdx: nextChildIx + childParamIdx,
					})
				}

				if !lazy {
					*c.params = append(*c.params, segment)
				}

				parent = matched
				matched = paramNode
				search = search[end:] // consume de search
				charsMatched += end
				childParamIdx = 0
				goto Walk
			}
		}

		if len(matched.wildcards) > 0 {
			subCtx := tree.pool.Get().(*cTx)
			for _, wildcardNode := range matched.wildcards {
				offset := charsMatched
				// Infix wildcard are evaluated first over suffix wildcard (longest path).
				if len(wildcardNode.statics) > 0 {
					startPath := offset
					for {
						idx := strings.IndexByte(path[offset:], slashDelim)
						if idx >= 0 {
							*subCtx.params = (*subCtx.params)[:0]
							offset += idx

							capturedValue := path[startPath:offset]

							if wildcardNode.regexp != nil && !wildcardNode.regexp.MatchString(capturedValue) {
								offset++
								continue
							}

							subNode, subTsr := lookupByPath(tree, wildcardNode, path[offset:], subCtx, lazy)
							if subNode == nil || startPath == offset {
								// The wildcard must not be empty e.g. for /*{any}/ but may contain "intermediary" empty segment.
								// '//' this is an empty segment
								// '///' the middle '/' is captured as a dynamic part.
								// This aligns to the ending catch all /*{any} where '//foo' capture '/foo'
								offset++
								continue
							}

							// We have a sub tsr opportunity
							if subTsr {
								// But only if no previous tsr
								if !tsr {
									tsr = true
									n = subNode
									if !lazy {
										*c.tsrParams = (*c.tsrParams)[:0]
										*c.tsrParams = append(*c.tsrParams, *c.params...)
										*c.tsrParams = append(*c.tsrParams, capturedValue)
										*c.tsrParams = append(*c.tsrParams, *subCtx.tsrParams...)
									}
								}

								// Try with next segment
								offset++
								continue
							}

							if !lazy {
								*c.params = append(*c.params, capturedValue)
								*c.params = append(*c.params, *subCtx.params...)
							}

							tree.pool.Put(subCtx)
							return subNode, subTsr
						}

						// We have fully consumed the wildcard node, and may have a tsr opportunity
						// but only if the remaining portion of the path to match is not empty, in order
						// to not match
						if !tsr && len(path[offset:]) > 0 {
							if _, child := wildcardNode.getStaticEdge(slashDelim); child != nil && child.route != nil && child.key == "/" {
								tsr = true
								n = child
								if !lazy {
									*c.tsrParams = (*c.tsrParams)[:0]
									*c.tsrParams = append(*c.tsrParams, path[startPath:])
								}
							}
						}

						break
					}
				}
			}

			tree.pool.Put(subCtx)

			for _, wildcardNode := range matched.wildcards {
				if wildcardNode.isLeaf() {
					if wildcardNode.regexp != nil && !wildcardNode.regexp.MatchString(search) {
						continue
					}

					if !lazy {
						*c.params = append(*c.params, search)
					}

					return wildcardNode, false
				}
			}

			// Note that we don't need to consume the search here, since we arge going to
			// backtrack
		}

		childParamIdx = 0
		goto Backtrack
	}

	if matched.route != nil {
		return matched, false
	}

	if !tsr {
		if _, child := matched.getStaticEdge(slashDelim); child != nil && child.route != nil && child.key == "/" {
			tsr = true
			n = child
			if !lazy {
				copyWithResize(c.tsrParams, c.params)
			}
		}

		if matched.key == "/" && parent != nil && parent.route != nil {
			tsr = true
			n = parent
			if !lazy {
				// Parent params = matched params minus last segment
				copyWithResize(c.tsrParams, c.params)
			}
		}
	}

Backtrack:
	if !tsr && matched.isLeaf() && search == "/" && !strings.HasSuffix(path, "//") {
		tsr = true
		n = matched
		// Save also a copy of the matched params, it should not allocate anything in most case.
		if !lazy {
			copyWithResize(c.tsrParams, c.params)
		}
	}

	if len(*c.skipStack) == 0 {
		return n, tsr
	}

	skipped := c.skipStack.pop()

	if skipped.childParamIdx < len(skipped.node.params) {
		matched = skipped.node
		parent = skipped.parent
		// Truncate params that have been recorder
		*c.params = (*c.params)[:skipped.paramCnt]
		// Restore search term
		search = path[skipped.pathIndex:]
		// Restore path index
		charsMatched = skipped.pathIndex
		skipStatic = true
		// Move to the next params
		childParamIdx = skipped.childParamIdx
		goto Walk
	}

	if len(skipped.node.wildcards) > 0 {
		matched = skipped.node
		parent = skipped.parent
		// Truncate params that have been recorder
		*c.params = (*c.params)[:skipped.paramCnt]
		// Restore search term
		search = path[skipped.pathIndex:]
		// Restore path index
		charsMatched = skipped.pathIndex
		// Don't visit params again
		childParamIdx = skipped.childParamIdx
		// Don't visit statics again
		skipStatic = true
		goto Walk
	}

	return n, tsr
}

type node struct {
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
	statics []*node

	// params contains child nodes for parameters. Regex params are ordered first
	// (in insertion order), followed by at most one non-regex param node ("?").
	params []*node

	// wildcards contains child nodes for catch-all segments. Regex wildcards are
	// ordered first (in insertion order), followed by at most one non-regex wildcard ("*").
	wildcards []*node

	// label is the first byte of the key for static nodes, used for binary search.
	// Set to 0x00 for param and wildcard nodes.
	label byte

	// host indicates this node's key belongs to the hostname portion of the route.
	// A node that belong to a host cannot be merged with a child key that start with a '/'.
	host bool
}

// addStaticEdge inserts a static child node while maintaining sorted order by label byte.
// Uses binary search to find the insertion point.
func (n *node) addStaticEdge(child *node) {
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
func (n *node) addParamEdge(child *node) {
	n.params = append(n.params, child)

	if child.key == "?" {
		return
	}

	idx := slices.IndexFunc(n.params, func(node *node) bool {
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
func (n *node) addWildcardEdge(child *node) {
	n.wildcards = append(n.wildcards, child)

	if child.key == "*" {
		return
	}

	idx := slices.IndexFunc(n.wildcards, func(node *node) bool {
		return node.key == "*"
	})

	if idx >= 0 && idx < len(n.wildcards)-1 {
		lastIdx := len(n.wildcards) - 1
		n.wildcards[idx], n.wildcards[lastIdx] = n.wildcards[lastIdx], n.wildcards[idx]
	}
}

// replaceStaticEdge updates an existing static child node in place.
// Uses binary search to locate the child by label, then replaces it.
func (n *node) replaceStaticEdge(child *node) {
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
func (n *node) getStaticEdge(label byte) (int, *node) {
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
func (n *node) getParamEdge(key string) (int, *node) {
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
func (n *node) getWildcardEdge(key string) (int, *node) {
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
func (n *node) delStaticEdge(label byte) {
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
func (n *node) delParamEdge(key string) {
	idx := slices.IndexFunc(n.params, func(p *node) bool { return p.key == key })
	if idx >= 0 {
		copy(n.params[idx:], n.params[idx+1:])
		n.params[len(n.params)-1] = nil
		n.params = n.params[:len(n.params)-1]
	}
}

// delWildcardEdge removes a wildcard child node by its key (either "*" or a regex pattern).
// Shifts remaining elements left after removal and clears the last slot for GC.
// No-op if no wildcard with the given key exists.
func (n *node) delWildcardEdge(key string) {
	idx := slices.IndexFunc(n.wildcards, func(p *node) bool { return p.key == key })
	if idx >= 0 {
		copy(n.wildcards[idx:], n.wildcards[idx+1:])
		n.wildcards[len(n.wildcards)-1] = nil
		n.wildcards = n.wildcards[:len(n.wildcards)-1]
	}
}

func (n *node) search(key string) (matched *node) {
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

		keyLen := min(len(child.key), len(search))
		if search[:keyLen] != child.key[:keyLen] {
			return nil
		}
		search = search[keyLen:]
		current = child
	}

	return current
}

func (n *node) isLeaf() bool {
	return n.route != nil
}

func (n *node) String() string {
	return n.string(0)
}

func (n *node) string(space int) string {
	sb := strings.Builder{}
	sb.WriteString(strings.Repeat(" ", space))
	sb.WriteString("path: ")
	sb.WriteString(n.key)
	if n.host {
		sb.WriteString(" (host)")
	}

	if len(n.params) > 0 {
		sb.WriteString(" [params: ")
		sb.WriteString(strconv.Itoa(len(n.params)))
		sb.WriteByte(']')
	}

	if len(n.wildcards) > 0 {
		sb.WriteString(" [wildcards: ")
		sb.WriteString(strconv.Itoa(len(n.wildcards)))
		sb.WriteByte(']')
	}

	if n.isLeaf() {
		sb.WriteString(" [leaf=")
		sb.WriteString(n.route.pattern)
		sb.WriteString("]")
	}

	sb.WriteByte('\n')

	for _, child := range n.statics {
		sb.WriteString("  ")
		sb.WriteString(child.string(space + 4))
	}
	for _, child := range n.params {
		sb.WriteString("  ")
		sb.WriteString(child.string(space + 4))
	}
	for _, child := range n.wildcards {
		sb.WriteString("  ")
		sb.WriteString(child.string(space + 4))
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
	node          *node
	parent        *node
	pathIndex     int
	paramCnt      int
	childParamIdx int
}
