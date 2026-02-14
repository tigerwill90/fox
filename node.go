package fox

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/fox-toolkit/fox/internal/netutil"
	"github.com/fox-toolkit/fox/internal/stringsutil"
)

const offsetZero = 0

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

func (n *node) lookup(method, hostPort, path string, c *Context, lazy bool) (int, *node, bool) {
	*c.skipStack = (*c.skipStack)[:0]
	// The tree for this method, we only have path registered
	if len(n.params) == 0 && len(n.wildcards) == 0 && len(n.statics) == 1 && n.statics[0].label == slashDelim {
		return lookupByPath(n, method, path, c, lazy, offsetZero)
	}

	host := netutil.StripHostPort(hostPort)
	if host == "" {
		return lookupByPath(n, method, path, c, lazy, offsetZero)
	}

	idx, nd, tsr := lookupByHostname(n, method, host, path, c, lazy)
	if nd == nil {
		// No match with hostname, fallback to path-only.
		*c.skipStack = (*c.skipStack)[:0]
		*c.params = (*c.params)[:0]
		if i, pathNode, pathTsr := lookupByPath(n, method, path, c, lazy, offsetZero); pathNode != nil {
			return i, pathNode, pathTsr
		}
	}
	// Hostname direct match
	return idx, nd, tsr
}

func lookupByHostname(root *node, method, host, path string, c *Context, lazy bool) (index int, n *node, tsr bool) {
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
			label := stringsutil.ToLowerASCII(search[0])
			num := len(matched.statics)
			idx := sort.Search(num, func(i int) bool { return matched.statics[i].label >= label })
			if idx < num && matched.statics[idx].label == label {
				child := matched.statics[idx]
				keyLen := len(child.key)
				if keyLen <= len(search) && stringsutil.EqualStringsASCIIIgnoreCase(search[:keyLen], child.key) {
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

			hasWildcards := len(matched.wildcards) > 0
			if end == 0 {
				if hasWildcards {
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
				if nextChildIx < len(params) || hasWildcards {
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

					// Skip empty captures (consecutive dot)
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
		idx, subNode, subTsr := lookupByPath(matched, method, path, c, lazy, stackOffset)
		if subNode != nil {
			return idx, subNode, subTsr
		}

		// Remove any unused skip nodes added during the path lookup.
		*c.skipStack = (*c.skipStack)[:stackOffset]
	}

Backtrack:
	if len(*c.skipStack) == 0 {
		return
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

func lookupByPath(root *node, method, path string, c *Context, lazy bool, stackOffset int) (index int, n *node, tsr bool) {

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
				// While this is less performant than byte-by-byte comparaison for reasonable search size,
				// direct == comparaison on string scale way better on long route.
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

				// Child key is /foo/, we can fully match /foo prefix and the remaining is exactly "/".
				if strings.HasPrefix(child.key, search) && child.key[len(search):] == "/" {
					if child.isLeaf() {
						for i, route := range child.routes {
							if route.handleSlash != StrictSlash && route.match(method, c) {
								return i, child, true
							}
						}
					}
					// Since /foo/ and /foo/*{any} are permitted with different set of matchers and methods, we still need
					// to search for match empty catch-all.
					for _, wildcardNode := range child.wildcards {
						for i, route := range wildcardNode.routes {
							if route.handleSlash != StrictSlash && route.catchEmpty && route.match(method, c) {
								if !lazy {
									// record empty match
									*c.params = append(*c.params, "")
								}
								return i, wildcardNode, true
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

			hasWildcards := len(matched.wildcards) > 0
			if end == 0 {
				if hasWildcards {
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
				if nextChildIx < len(params) || hasWildcards {
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

		WalkWildcard:
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
						if len(path[searchStart:]) > 0 {
							if _, child := wildcardNode.getStaticEdge(slashDelim); child != nil && child.isLeaf() && child.key == "/" {
								// We have the path /foo/x/y/z for the route /foo/+{any:[A-z]}/ that may be matched with a ts,
								// but we need to make sure that the regexp match too.
								if wildcardNode.regexp != nil && !wildcardNode.regexp.MatchString(path[offset:]) {
									break
								}
								for j, route := range child.routes {
									if route.handleSlash != StrictSlash && route.match(method, c) {
										// This is the only case where we don't return a TSR match immediately. Routes like
										// /+{args}/ (with TSR enabled) and /+{args} can coexist. For a request like /a/b/c,
										// the infix /+{args}/ would match with TSR (adding a trailing slash), but we must
										// first check whether a suffix catch-all directly matches. We capture the node here
										// but defer parameter recording as a fallback.
										n = child
										tsr = true
										index = j
										break WalkWildcard
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
					if route.match(method, c) {
						if !lazy {
							*c.params = append(*c.params, search)
						}
						return i, wildcardNode, false
					}
				}
			}

			// fallback to tsr if any recorded
			if tsr {
				if !lazy {
					*c.params = append(*c.params, path[offset:])
				}
				return
			}
		}

		goto Backtrack
	}

	if matched.isLeaf() {
		for i, route := range matched.routes {
			if route.match(method, c) {
				return i, matched, false
			}
		}
	}

	// Try to catch empty for wildcard supporting it.
	for _, wildcardNode := range matched.wildcards {
		for i, route := range wildcardNode.routes {
			if route.catchEmpty && route.match(method, c) {
				if !lazy {
					*c.params = append(*c.params, "")
				}
				return i, wildcardNode, false
			}
		}
	}

	if _, child := matched.getStaticEdge(slashDelim); child != nil && child.isLeaf() && child.key == "/" {
		for i, route := range child.routes {
			if route.handleSlash != StrictSlash && route.match(method, c) {
				return i, child, true
			}
		}
	} else if matched.key == "/" && parent != nil && parent.isLeaf() && parent.key != "*" {
		for i, route := range parent.routes {
			if route.handleSlash != StrictSlash && route.match(method, c) {
				return i, parent, true
			}
		}
	}

Backtrack:
	if matched.isLeaf() && matched.key != "*" && search == "/" && !strings.HasSuffix(path, "//") {
		for i, route := range matched.routes {
			if route.handleSlash != StrictSlash && route.match(method, c) {
				return i, matched, true
			}
		}
	}

	if len(*c.skipStack) == stackOffset {
		return
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

func (n *node) addRoute(route *Route) {
	// Method-less and no matchers is always evaluated last.
	if len(route.methods) == 0 && len(route.matchers) == 0 {
		n.routes = append(n.routes, route)
		return
	}

	pos := n.findInsertPosition(route)
	n.routes = slices.Insert(n.routes, pos, route)
}

func (n *node) replaceRoute(idx int, route *Route) {
	// Priority unchanged or no matchers, replace in place
	if n.routes[idx].priority == route.priority || len(route.matchers) == 0 {
		n.routes[idx] = route
		return
	}

	// Priority changed with matchers, we need to reposition the route.
	n.routes = slices.Delete(n.routes, idx, idx+1)
	pos := n.findInsertPosition(route)
	n.routes = slices.Insert(n.routes, pos, route)
}

func (n *node) findInsertPosition(route *Route) int {
	hasMethods := len(route.methods) > 0
	hasMatchers := len(route.matchers) > 0

	for i, existing := range n.routes {
		existingHasMethods := len(existing.methods) > 0
		existingHasMatchers := len(existing.matchers) > 0

		// Routes with methods before routes without
		if hasMethods && !existingHasMethods {
			return i
		}

		// Within same method tier...
		if hasMethods == existingHasMethods {
			// Routes with matchers before routes without
			if hasMatchers && !existingHasMatchers {
				return i
			}

			// Within same matchers tier, higher priority first
			if hasMatchers && existingHasMatchers && route.priority > existing.priority {
				return i
			}
		}
	}

	return len(n.routes)
}

func (n *node) delRoute(idx int) {
	if idx >= 0 {
		copy(n.routes[idx:], n.routes[idx+1:])
		last := len(n.routes) - 1
		n.routes[last] = nil
		n.routes = n.routes[:last:last]
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

func (n *node) searchPattern(key string) (matched *node) {
	current := n
	search := key

	for len(search) > 0 {
		switch search[0] {
		case bracketDelim:
			end, paramName := parseBraceSegment(search)
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
		case starDelim, plusDelim:
			end, paramName := parseBraceSegment(search)
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

func (n *node) searchName(key string) (matched *node) {
	current := n
	search := key

	for len(search) > 0 {
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

// parseBraceSegment extracts the node key from a param or wildcard segment in a route pattern.
// It returns the index of the closing brace and the corresponding node key used for tree lookups:
//   - For params {name}: returns (end, "?")
//   - For wildcards *{name}: returns (end, "*")
//   - For params with regex {name:pattern}: returns (end, "pattern")
//   - For wildcards with regex *{name:pattern}: returns (end, "pattern")
//   - For invalid/malformed segments: returns (0, "") to signal early exit
//
// This is a lightweight parser that does not fully validate the segment. It assumes the caller
// will verify that the retrieved route's pattern matches the search pattern after tree lookup.
func parseBraceSegment(pattern string) (int, string) {
	length := len(pattern)
	if length == 0 {
		return 0, pattern
	}

	key := "?"
	// TODO this is garbage, I don't like it at all
	if strings.HasPrefix(pattern, "*{") || strings.HasPrefix(pattern, "+{") {
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
	if n.key == "" {
		sb.WriteString("root:")
	} else {
		sb.WriteString("path: ")
		sb.WriteString(n.key)
	}
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

		if len(route.methods) > 0 {
			sb.WriteString(" [methods: ")
			for i, method := range route.methods {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(method)
			}
			sb.WriteByte(']')
		}

		if len(route.matchers) > 0 {
			sb.WriteString(" [matchers: ")
			for i, matcher := range route.matchers {
				if i > 0 {
					sb.WriteString(", ")
				}
				if m, ok := matcher.(fmt.Stringer); ok {
					sb.WriteString(m.String())
				} else {
					sb.WriteString(reflect.TypeOf(matcher).String())
				}
			}
			sb.WriteByte(']')
		}
		sb.WriteString(" [priority: ")
		sb.WriteString(strconv.FormatUint(uint64(route.priority), 10))
		sb.WriteByte(']')
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
