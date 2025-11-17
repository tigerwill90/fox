package fox

import (
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/tigerwill90/fox/internal/netutil"
	"github.com/tigerwill90/fox/internal/stringutil"
)

const offsetZero = 0

type root map[string]*node

func (rt root) lookup(method, hostPort, path string, c *cTx, lazy bool) (int, *node) {
	root := rt[method]
	if root == nil {
		return 0, nil
	}

	*c.skipStack = (*c.skipStack)[:0]
	// The tree for this method, we only have path registered
	if len(root.params) == 0 && len(root.wildcards) == 0 && len(root.statics) == 1 && root.statics[0].label == slashDelim {
		return lookupByPath(root, path, c, lazy, offsetZero)
	}

	host := netutil.StripHostPort(hostPort)
	if host == "" {
		return lookupByPath(root, path, c, lazy, offsetZero)
	}

	idx, n := lookupByHostname(root, host, path, c, lazy)
	if n == nil || c.tsr {
		// Either no match or match with tsr, try lookup by path with tsr enable
		// so we won't check for tsr again.
		*c.skipStack = (*c.skipStack)[:0]
		*c.params = (*c.params)[:0]
		if i, pathNode := lookupByPath(root, path, c, lazy, 0); pathNode != nil {
			return i, pathNode
		}
	}
	// Hostname direct match
	return idx, n
}

func lookupByHostname(root *node, host, path string, c *cTx, lazy bool) (index int, n *node) {
	var (
		charsMatched     int
		skipStatic       bool
		childParamIdx    int
		childWildcardIdx int
		wildcardOffset   int
	)

	matched := root
	search := host

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
					if len(matched.params) > 0 || len(matched.wildcards) > 0 {
						*c.skipStack = append(*c.skipStack, skipNode{
							node:         matched,
							charsMatched: charsMatched,
							paramCnt:     len(*c.params),
						})
					}

					matched = child
					search = search[keyLen:]
					charsMatched += keyLen
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
				if len(matched.wildcards) > 0 {
					*c.skipStack = append(*c.skipStack, skipNode{
						node:          matched,
						charsMatched:  charsMatched,
						paramCnt:      len(*c.params),
						childParamIdx: len(matched.params),
					})
				}
				goto Backtrack
			}

			segment := search[:end]

			for i, paramNode := range params {
				if paramNode.regexp != nil && !paramNode.regexp.MatchString(segment) {
					continue
				}

				nextChildIx := i + 1
				if nextChildIx < len(params) || len(matched.wildcards) > 0 {
					*c.skipStack = append(*c.skipStack, skipNode{
						node:          matched,
						charsMatched:  charsMatched,
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

		wildcards := matched.wildcards[childWildcardIdx:]
		if len(wildcards) > 0 {
			offset := charsMatched
			// Try infix wildcards first
			for i, wildcardNode := range wildcards {
				if len(wildcardNode.statics) == 1 && wildcardNode.statics[0].label == slashDelim {
					continue // Not an infix wildcard
				}

				// Start searching from the wildcard's base position + any accumulated offset from last
				// node put in the skip stack.
				searchStart := charsMatched + wildcardOffset
				for {
					// Search for next dot from current offset
					idx := strings.IndexByte(host[searchStart:], dotDelim)
					if idx < 0 {
						break
					}

					captureEnd := searchStart + idx
					capturedValue := host[charsMatched:captureEnd]

					// Skip empty captures (consecutive slashes)
					if capturedValue == "" {
						searchStart++
						continue
					}

					if wildcardNode.regexp != nil && !wildcardNode.regexp.MatchString(capturedValue) {
						searchStart = captureEnd + 1
						continue
					}

					// We have a potential match, save backtrack state
					// The next attempt should search starting one position after current searchStart
					*c.skipStack = append(*c.skipStack, skipNode{
						node:             matched,
						charsMatched:     charsMatched,
						paramCnt:         len(*c.params),
						childParamIdx:    len(matched.params),
						childWildcardIdx: i + childWildcardIdx,
						wildcardOffset:   captureEnd - charsMatched + 1,
					})

					if !lazy {
						*c.params = append(*c.params, capturedValue)
					}

					// Descend into wildcard subtree
					matched = wildcardNode
					search = host[captureEnd:]
					charsMatched = captureEnd
					childParamIdx = 0
					childWildcardIdx = 0
					wildcardOffset = 0
					goto Walk
				}

				nextChildIdx := i + 1
				if nextChildIdx < len(wildcards) {
					*c.skipStack = append(*c.skipStack, skipNode{
						node:             matched,
						charsMatched:     offset,
						paramCnt:         len(*c.params),
						childParamIdx:    len(matched.params),
						childWildcardIdx: nextChildIdx + childWildcardIdx,
						wildcardOffset:   0, // Next wildcard starts from beginning
					})
					goto Backtrack
				}
			}

			// After trying all infix wildcards, try suffix catchalls
			for _, wildcardNode := range matched.wildcards {
				if len(wildcardNode.statics) == 1 && wildcardNode.statics[0].label == dotDelim {
					continue // Not a suffix catchall
				}

				if wildcardNode.regexp != nil && !wildcardNode.regexp.MatchString(search) {
					continue
				}

				if !lazy {
					*c.params = append(*c.params, search)
				}

				matched = wildcardNode
				break Walk
			}
		}

		goto Backtrack
	}

	if _, pathChild := matched.getStaticEdge(slashDelim); pathChild != nil {
		stackOffset := len(*c.skipStack)
		idx, subNode := lookupByPath(matched, path, c, lazy, stackOffset)
		if subNode != nil {
			if c.tsr {
				n = subNode
				index = idx
			} else {
				c.tsr = false
				return idx, subNode
			}
		}

		// Remove any unused skip nodes added during the path lookup.
		*c.skipStack = (*c.skipStack)[:stackOffset]
	}

Backtrack:
	if len(*c.skipStack) == 0 {
		return index, n
	}

	skipped := c.skipStack.pop()

	if skipped.childParamIdx < len(skipped.node.params) {
		matched = skipped.node
		*c.params = (*c.params)[:skipped.paramCnt]
		search = host[skipped.charsMatched:]
		charsMatched = skipped.charsMatched
		skipStatic = true
		childParamIdx = skipped.childParamIdx
		childWildcardIdx = 0
		wildcardOffset = 0
		goto Walk
	}

	matched = skipped.node
	*c.params = (*c.params)[:skipped.paramCnt]
	search = host[skipped.charsMatched:]
	charsMatched = skipped.charsMatched
	childParamIdx = skipped.childParamIdx
	childWildcardIdx = skipped.childWildcardIdx
	wildcardOffset = skipped.wildcardOffset
	skipStatic = true

	goto Walk
}

func lookupByPath(root *node, path string, c *cTx, lazy bool, stackOffset int) (index int, n *node) {

	var (
		charsMatched     int
		skipStatic       bool
		childParamIdx    int
		childWildcardIdx int
		wildcardOffset   int
		parent           *node
	)

	matched := root
	search := path

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
							node:         matched,
							parent:       parent,
							charsMatched: charsMatched,
							paramCnt:     len(*c.params),
						})
					}

					parent = matched
					matched = child
					search = search[keyLen:]
					charsMatched += keyLen
					continue
				}

				if !c.tsr && child.isLeaf() && strings.HasPrefix(child.key, search) {
					remaining := child.key[len(search):]
					if remaining == "/" {
						for i, route := range child.routes {
							c.cachedQueries = nil
							if route.Match(c) {
								c.tsr = true
								n = child
								index = i
								if !lazy {
									copyWithResize(c.tsrParams, c.params)
								}
								break
							}
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

			if end == 0 {
				if len(matched.wildcards) > 0 {
					*c.skipStack = append(*c.skipStack, skipNode{
						node:          matched,
						parent:        parent,
						charsMatched:  charsMatched,
						paramCnt:      len(*c.params),
						childParamIdx: len(matched.params),
					})
				}
				goto Backtrack
			}

			segment := search[:end]
			for i, paramNode := range params {
				if paramNode.regexp != nil && !paramNode.regexp.MatchString(segment) {
					continue
				}

				nextChildIx := i + 1
				if nextChildIx < len(params) || len(matched.wildcards) > 0 {
					*c.skipStack = append(*c.skipStack, skipNode{
						node:          matched,
						parent:        parent,
						charsMatched:  charsMatched,
						paramCnt:      len(*c.params),
						childParamIdx: nextChildIx + childParamIdx,
					})
				}

				if !lazy {
					*c.params = append(*c.params, segment)
				}

				parent = matched
				matched = paramNode
				search = search[end:]
				charsMatched += end
				childParamIdx = 0
				goto Walk
			}
		}

		wildcards := matched.wildcards[childWildcardIdx:]
		if len(wildcards) > 0 {
			offset := charsMatched
			// Try infix wildcards first
			for i, wildcardNode := range wildcards {
				if len(wildcardNode.statics) == 0 {
					continue // Not an infix wildcard
				}

				// Start searching from the wildcard's base position + any accumulated offset from last
				// node put in the skip stack.
				searchStart := charsMatched + wildcardOffset

				for {
					// Search for next slash from current offset
					idx := strings.IndexByte(path[searchStart:], slashDelim)
					if idx < 0 {
						if !c.tsr && len(path[searchStart:]) > 0 {
							if _, child := wildcardNode.getStaticEdge(slashDelim); child != nil && child.isLeaf() && child.key == "/" {
								for i, route := range child.routes {
									c.cachedQueries = nil
									if route.Match(c) {
										c.tsr = true
										n = child
										index = i
										if !lazy {
											copyWithResize(c.tsrParams, c.params)
											*c.tsrParams = append(*c.tsrParams, path[charsMatched:])
										}
										break
									}
								}
							}
						}
						break
					}

					captureEnd := searchStart + idx
					capturedValue := path[charsMatched:captureEnd]

					// Skip empty captures (consecutive slashes)
					if capturedValue == "" {
						searchStart++
						continue
					}

					if wildcardNode.regexp != nil && !wildcardNode.regexp.MatchString(capturedValue) {
						searchStart = captureEnd + 1
						continue
					}

					// We have a potential match, save backtrack state
					// The next attempt should search starting one position after current searchStart
					*c.skipStack = append(*c.skipStack, skipNode{
						node:             matched,
						parent:           parent,
						charsMatched:     charsMatched,
						paramCnt:         len(*c.params),
						childParamIdx:    len(matched.params),
						childWildcardIdx: i + childWildcardIdx,
						wildcardOffset:   captureEnd - charsMatched + 1,
					})

					if !lazy {
						*c.params = append(*c.params, capturedValue)
					}

					// Descend into wildcard subtree
					parent = matched
					matched = wildcardNode
					search = path[captureEnd:]
					charsMatched = captureEnd
					childParamIdx = 0
					childWildcardIdx = 0
					wildcardOffset = 0
					goto Walk
				}

				nextChildIdx := i + 1
				if nextChildIdx < len(wildcards) {
					*c.skipStack = append(*c.skipStack, skipNode{
						node:             matched,
						parent:           parent,
						charsMatched:     offset,
						paramCnt:         len(*c.params),
						childParamIdx:    len(matched.params),
						childWildcardIdx: nextChildIdx + childWildcardIdx,
						wildcardOffset:   0, // Next wildcard starts from beginning
					})
					goto Backtrack
				}
			}

			// After trying all infix wildcards, try suffix catchalls
			for _, wildcardNode := range matched.wildcards {
				if !wildcardNode.isLeaf() {
					continue // Not a suffix catchall
				}

				if wildcardNode.regexp != nil && !wildcardNode.regexp.MatchString(search) {
					continue
				}

				for i, route := range wildcardNode.routes {
					c.cachedQueries = nil
					if route.Match(c) {
						if !lazy {
							*c.params = append(*c.params, search)
						}
						c.tsr = false
						return i, wildcardNode
					}
				}
			}
		}

		goto Backtrack
	}

	if matched.isLeaf() {
		for i, route := range matched.routes {
			c.cachedQueries = nil
			if route.Match(c) {
				c.tsr = false
				return i, matched
			}
		}
	}

	if !c.tsr {
		if _, child := matched.getStaticEdge(slashDelim); child != nil && child.isLeaf() && child.key == "/" {
			for i, route := range child.routes {
				c.cachedQueries = nil
				if route.Match(c) {
					c.tsr = true
					n = child
					index = i
					if !lazy {
						copyWithResize(c.tsrParams, c.params)
					}
					break
				}
			}
		}

		if matched.key == "/" && parent != nil && parent.isLeaf() {
			for i, route := range parent.routes {
				c.cachedQueries = nil
				if route.Match(c) {
					c.tsr = true
					n = parent
					index = i
					if !lazy {
						copyWithResize(c.tsrParams, c.params)
					}
					break
				}
			}
		}
	}

Backtrack:
	if !c.tsr && matched.isLeaf() && search == "/" && !strings.HasSuffix(path, "//") {
		for i, route := range matched.routes {
			c.cachedQueries = nil
			if route.Match(c) {
				c.tsr = true
				n = matched
				index = i
				if !lazy {
					copyWithResize(c.tsrParams, c.params)
				}
				break
			}
		}
	}

	if len(*c.skipStack) == stackOffset {
		return index, n
	}

	skipped := c.skipStack.pop()

	if skipped.childParamIdx < len(skipped.node.params) {
		matched = skipped.node
		parent = skipped.parent
		*c.params = (*c.params)[:skipped.paramCnt]
		search = path[skipped.charsMatched:]
		charsMatched = skipped.charsMatched
		skipStatic = true
		childParamIdx = skipped.childParamIdx
		childWildcardIdx = 0
		wildcardOffset = 0
		goto Walk
	}

	matched = skipped.node
	parent = skipped.parent
	*c.params = (*c.params)[:skipped.paramCnt]
	search = path[skipped.charsMatched:]
	charsMatched = skipped.charsMatched
	childParamIdx = skipped.childParamIdx
	childWildcardIdx = skipped.childWildcardIdx
	wildcardOffset = skipped.wildcardOffset
	skipStatic = true

	goto Walk
}

type node struct {
	// routes holds the registered handlers if this node is a leaf.
	routes []*Route

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
	// Param/wildcard nodes with regex contains the regex pattern literal.
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

func (n *node) addRoute(route *Route) {
	n.routes = append(n.routes, route)

	if len(route.matchers) == 0 {
		return
	}

	idx := slices.IndexFunc(n.routes, func(route *Route) bool {
		return len(route.matchers) == 0
	})
	if idx >= 0 && idx < len(n.routes)-1 {
		lastIdx := len(n.routes) - 1
		n.routes[idx], n.routes[lastIdx] = n.routes[lastIdx], n.routes[idx]
	}
}

func (n *node) replaceRoute(route *Route) {
	idx := slices.IndexFunc(n.routes, func(r *Route) bool {
		return r.MatchersEqual(route.matchers)
	})
	if idx >= 0 {
		n.routes[idx] = route
		return
	}
	panic("internal error: replacing missing route")
}

func (n *node) delRoute(route *Route) {
	idx := slices.IndexFunc(n.routes, func(r *Route) bool {
		return r.MatchersEqual(route.matchers)
	})
	if idx >= 0 {
		copy(n.routes[idx:], n.routes[idx+1:])
		n.routes[len(n.routes)-1] = nil
		n.routes = n.routes[:len(n.routes)-1]
	}
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
			end, paramName := parseCanonicalKey(search)
			if paramName == "" {
				goto STATIC
			}
			_, child := current.getParamEdge(paramName)
			if child == nil {
				return nil
			}
			current = child
			search = search[end+1:]
			continue
		}

		if search[0] == starDelim {
			end, paramName := parseCanonicalKey(search)
			if paramName == "" {
				goto STATIC
			}
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

// TODO test that extensively (fuzzing) ??
// parseCanonicalKey ...
func parseCanonicalKey(pattern string) (int, string) {
	length := len(pattern)
	if length == 0 {
		return 0, pattern
	}

	key := "?"
	if strings.HasPrefix(pattern, "*{") {
		key = "*"
	}

	end := braceIndice(pattern, 0)
	if end <= 0 {
		return 0, ""
	}

	idx := strings.IndexByte(pattern[:end], ':')
	if idx == -1 {
		return end, key
	}
	// Handle missing param name such as {:[A-z]} here since it would be an invalid route anyway.
	if idx == 0 {
		return 0, ""
	}

	return end, pattern[idx+1 : end]
}

func (n *node) isLeaf() bool {
	return len(n.routes) > 0
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

	for _, route := range n.routes {
		sb.WriteByte('\n')
		sb.WriteString(strings.Repeat(" ", space+8))
		sb.WriteString("=> ")
		sb.WriteString(route.pattern)
		if len(route.matchers) > 0 {
			sb.WriteString(" [matchers: ")
			for i, matcher := range route.matchers {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(reflect.TypeOf(matcher).String())
			}
			sb.WriteByte(']')
		}
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
	node             *node
	parent           *node
	charsMatched     int
	paramCnt         int
	childParamIdx    int
	childWildcardIdx int
	wildcardOffset   int
}
