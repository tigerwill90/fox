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
	"github.com/fox-toolkit/fox/internal/regprog"
	"github.com/fox-toolkit/fox/internal/stringsutil"
)

const offsetZero = 0

type node struct {
	// routes holds the registered handlers if this node is a leaf.
	routes []*Route

	// regexp is an optional compiled regular expression constraint for param and wildcard nodes.
	// When present, captured segments must match this pattern during lookup.
	regexp *regexp.Regexp

	// prog is regexp compiled for simulation, set for wildcard nodes with a regexp. It finds in a
	// single pass the capture ends of an infix wildcard that match, and runs in the Pike VM.
	prog *regprog.Regexp

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

	// nested reports whether a wildcard precedes this wildcard node in the hostname or in the
	// path, so a lookup can reach it at several offsets.
	nested bool
}

func (n *node) lookup(method, hostPort, path string, c *Context, lazy bool) (int, *node, bool) {
	*c.skipStack = (*c.skipStack)[:0]
	if c.lookup.dirty {
		c.lookup.reset()
	}
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
	// Hostname match, direct or tsr. A tsr match counts as a direct match and hostname
	// routes always have priority, so a hostname tsr beats a path-only direct match.
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
				// keyLen == 1 short-circuits the case-insensitive compare: hostname keys are
				// stored lowercase (uppercase is rejected at parse time) and the label match
				// already proved lowercase(search[0]) == child.key[0].
				if keyLen == 1 || (keyLen <= len(search) && stringsutil.EqualStringsASCIIIgnoreCase(search[:keyLen], child.key)) {
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
				// node put in the skip stack, and stop before the capture ends already proven to fail.
				searchStart := charsMatched + wildcardOffset
				end := len(host)
				// A resumed scan already evaluated the regexp once.
				evaluated := wildcardOffset > 0
				var ends *regprog.Ends
				if wildcardNode.regexp == nil {
					end = c.lookup.scanEnd(wildcardNode, searchStart, end)
				} else if evaluated {
					ends = c.lookup.ends(wildcardNode, host, charsMatched, dotDelim)
				} else if wildcardNode.nested && c.lookup.retryScan(wildcardNode) {
					goto VM
				}

				for {
					var captureEnd int
					if ends != nil {
						if captureEnd = ends.Next(searchStart); captureEnd < 0 {
							break
						}
					} else {
						// Search for next dot from current offset
						idx := strings.IndexByte(host[searchStart:end], dotDelim)
						if idx < 0 {
							break
						}
						captureEnd = searchStart + idx
					}

					c.lookup.steps++
					capturedValue := host[charsMatched:captureEnd]

					// Skip empty captures (consecutive dot)
					if capturedValue == "" {
						searchStart++
						continue
					}

					if wildcardNode.regexp != nil && ends == nil {
						// A second evaluation lists every capture end that matches in a single pass.
						if evaluated {
							ends = c.lookup.ends(wildcardNode, host, charsMatched, dotDelim)
							searchStart = captureEnd
							continue
						}
						evaluated = true
						if !wildcardNode.regexp.MatchString(capturedValue) {
							searchStart = captureEnd + 1
							continue
						}
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

				if wildcardNode.regexp == nil {
					c.lookup.markExhausted(wildcardNode, charsMatched)
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

			// After trying all infix wildcards, try suffix catchalls. The path is
			// matched in place so a failed candidate does not end the lookup.
			for _, wildcardNode := range matched.wildcards {
				if len(wildcardNode.statics) == 1 && wildcardNode.statics[0].label == dotDelim {
					continue // Not a suffix catchall
				}

				// The path lookup below does not depend on the hostname capture, so it fails again.
				if c.lookup.pathFailed(wildcardNode) {
					continue
				}

				if wildcardNode.regexp != nil {
					if wildcardNode.nested && c.lookup.retrySuffix(wildcardNode) {
						goto VM
					}
					if !wildcardNode.regexp.MatchString(search) {
						continue
					}
				}

				paramCnt := len(*c.params)
				if !lazy {
					*c.params = append(*c.params, search)
				}

				stackOffset := len(*c.skipStack)
				if idx, subNode, subTsr := lookupByPath(wildcardNode, method, path, c, lazy, stackOffset); subNode != nil {
					return idx, subNode, subTsr
				}
				// Only a backtrack can reach this node again.
				if stackOffset > 0 {
					c.lookup.setPathFailed(wildcardNode)
				}

				*c.skipStack = (*c.skipStack)[:stackOffset]
				*c.params = (*c.params)[:paramCnt]
			}
		}

		goto Backtrack
	}

	if _, pathChild := matched.getStaticEdge(slashDelim); pathChild != nil && !c.lookup.pathFailed(matched) {
		stackOffset := len(*c.skipStack)
		idx, subNode, subTsr := lookupByPath(matched, method, path, c, lazy, stackOffset)
		if subNode != nil {
			return idx, subNode, subTsr
		}
		// Only a backtrack can reach this node again.
		if stackOffset > 0 {
			c.lookup.setPathFailed(matched)
		}

		// Remove any unused skip nodes added during the path lookup.
		*c.skipStack = (*c.skipStack)[:stackOffset]
	}
	goto Backtrack

VM:
	// A nested wildcard is reached again: the Pike VM runs the subtree of the outermost infix
	// wildcard, and the walk resumes below it when nothing matches.
	{
		f := outermostInfix(c, 0)
		if index, n, tsr = c.lookup.hostVM().run(method, host, path, c, lazy, f); n != nil {
			return
		}
	}

Backtrack:
	if len(*c.skipStack) == 0 {
		return
	}

	// A skip node pushed with params left to try is always pushed with childWildcardIdx
	// and wildcardOffset at zero, so restoring every field as saved is correct in all cases.
	skipped := c.skipStack.pop()

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

// lookupByPath returns the node matching path. When tsr is true, the returned node matches the
// path with its trailing slash added or removed. Only routes not registered with ExactSlash are
// eligible for a tsr match, and the node is nil when no route matches either form.
//
// A route registered with RelaxedSlash or RedirectSlash matches with and without its trailing
// slash. A tsr candidate therefore counts as a direct match and keeps the priority of the
// branch where it was found (static > regex param > param > regex wildcard > wildcard). The
// lookup returns it without exploring lower priority alternatives on the skip stack, even when
// one of them matches the path exactly. For example with /foo/ registered as RelaxedSlash and
// /{a}, a request for /foo matches /foo/ because the static branch has priority over the
// param branch.
//
// When both slash variants of the same pattern are registered, each request form is served by
// its exact variant. Static and param patterns need no special handling since the exact leaf
// is checked before any tsr candidate. Catch-alls need special handling because the infix
// wildcard (/+{x}/) is always walked before the suffix (/+{x}) to favor the longest match,
// otherwise the suffix would always win as it matches any remainder. The infix tsr candidate
// is recorded and only used when no suffix catch-all at the same node matches directly.
//
// A tsr candidate never crosses a "//" boundary, so /foo// and /foo/ stay distinct.
//
// A route registered with ExactSlash is never used as a tsr candidate, but a slash correction
// triggered by another route can still redirect traffic to it, for example /foo/ registered with
// ExactSlash and /{a}/ registered with RedirectSlash. A request for /foo finds no direct match
// and the param branch offers /{a}/ as a tsr candidate, so the router redirects to /foo/. The
// client follows the redirect and the new request is a direct match for the static /foo/ route.
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
				// direct == comparaison on string scale way better on long route. The keyLen == 1 case
				// short-circuits the memequal call: we already verified search[0] == child.key[0] via the
				// label match above.
				if keyLen == 1 || (keyLen <= len(search) && search[:keyLen] == child.key) {
					// Only the lookup root can have hostname params and wildcards.
					// A path lookup must skip them.
					if (len(matched.params) > 0 || len(matched.wildcards) > 0) && matched != root {
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

				// Child key is exactly search plus a trailing slash (e.g. search /foo, key /foo/),
				// excluding keys ending in "//". The length check keeps the common mismatch cheap.
				if keyLen == len(search)+1 && child.key[keyLen-1] == '/' && child.key[keyLen-2] != '/' && strings.HasPrefix(child.key, search) {
					if i, tsrNode := matchTrailingSlash(child, method, c, lazy); tsrNode != nil {
						return i, tsrNode, true
					}
				}
			}
		}

		skipStatic = false
		params := matched.params[childParamIdx:]
		if len(params) > 0 && matched != root {
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
		if len(wildcards) > 0 && matched != root {
			offset := charsMatched

		WalkWildcard:
			// Try infix wildcards first
			for i, wildcardNode := range wildcards {
				if len(wildcardNode.statics) == 0 {
					continue // Not an infix wildcard
				}

				// Start searching from the wildcard's base position + any accumulated offset from last
				// node put in the skip stack, and stop before the capture ends already proven to fail.
				searchStart := charsMatched + wildcardOffset
				end := len(path)
				// A resumed scan already evaluated the regexp once.
				evaluated := wildcardOffset > 0
				var ends *regprog.Ends
				if wildcardNode.regexp == nil {
					end = c.lookup.scanEnd(wildcardNode, searchStart, end)
				} else if evaluated {
					ends = c.lookup.ends(wildcardNode, path, charsMatched, slashDelim)
				} else if wildcardNode.nested && c.lookup.retryScan(wildcardNode) {
					goto VM
				}

				// An infix wildcard has a single static child, its key starts with a slash.
				key := wildcardNode.statics[0].key

				for {
					captureEnd := -1
					if ends != nil {
						captureEnd = ends.Next(searchStart)
					} else if idx := strings.IndexByte(path[searchStart:end], slashDelim); idx >= 0 {
						// Search for next slash from current offset
						captureEnd = searchStart + idx
					}

					if captureEnd < 0 {
						// When the search stopped before the end of the path, the tsr below was already tried.
						if end == len(path) && path[len(path)-1] != slashDelim {
							if _, child := wildcardNode.getStaticEdge(slashDelim); child != nil && child.isLeaf() && child.key == "/" {
								// We have the path /foo/x/y/z for the route /foo/+{any:[A-z]}/ that may be matched with a ts,
								// but we need to make sure that the regexp match too.
								if wildcardNode.regexp != nil && !wildcardNode.regexp.MatchString(path[offset:]) {
									break
								}
								for j, route := range child.routes {
									if route.handleSlash != ExactSlash && route.match(method, c) {
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

					c.lookup.steps++
					capturedValue := path[charsMatched:captureEnd]

					// Skip empty captures (consecutive slashes)
					if capturedValue == "" {
						searchStart++
						continue
					}

					// Skip the capture when the rest of the path neither starts with the static child key
					// nor is a prefix of it, as no route below can match it, even with a trailing slash.
					if rest := path[captureEnd:]; !strings.HasPrefix(rest, key) && !strings.HasPrefix(key, rest) {
						searchStart = captureEnd + 1
						continue
					}

					if wildcardNode.regexp != nil && ends == nil {
						// A second evaluation lists every capture end that matches in a single pass.
						if evaluated {
							ends = c.lookup.ends(wildcardNode, path, charsMatched, slashDelim)
							searchStart = captureEnd
							continue
						}
						evaluated = true
						if !wildcardNode.regexp.MatchString(capturedValue) {
							searchStart = captureEnd + 1
							continue
						}
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

				if wildcardNode.regexp == nil {
					c.lookup.markExhausted(wildcardNode, charsMatched)
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

				if wildcardNode.regexp != nil {
					if wildcardNode.nested && c.lookup.retrySuffix(wildcardNode) {
						goto VM
					}
					if !wildcardNode.regexp.MatchString(search) {
						continue
					}
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
			if route.pattern.optionalCatchAll && route.match(method, c) {
				if !lazy {
					*c.params = append(*c.params, "")
				}
				return i, wildcardNode, false
			}
		}
	}

	if _, child := matched.getStaticEdge(slashDelim); child != nil && child.key == "/" && !strings.HasSuffix(path, "/") {
		if i, tsrNode := matchTrailingSlash(child, method, c, lazy); tsrNode != nil {
			return i, tsrNode, true
		}
	} else if matched.key == "/" && parent != nil && parent.isLeaf() && !strings.HasSuffix(path, "//") {
		for i, route := range parent.routes {
			if route.handleSlash != ExactSlash && !route.pattern.catchAll && route.match(method, c) {
				return i, parent, true
			}
		}
	}
	goto Backtrack

VM:
	// A nested wildcard is reached again: the Pike VM runs the subtree of the outermost infix
	// wildcard, and the walk resumes below it when nothing matches.
	{
		f := outermostInfix(c, stackOffset)
		if index, n, tsr = c.lookup.pathVM().run(method, path, path, c, lazy, f); n != nil {
			return
		}
		matched, charsMatched = f.node, f.charsMatched
		search = path[charsMatched:]
	}

Backtrack:
	if search == "/" && matched.isLeaf() && !strings.HasSuffix(path, "//") {
		for i, route := range matched.routes {
			if route.handleSlash != ExactSlash && !route.pattern.catchAll && route.match(method, c) {
				return i, matched, true
			}
		}
	}

	if len(*c.skipStack) == stackOffset {
		return
	}

	// A skip node pushed with params left to try is always pushed with childWildcardIdx
	// and wildcardOffset at zero, so restoring every field as saved is correct in all cases.
	skipped := c.skipStack.pop()

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

// matchTrailingSlash returns a trailing slash candidate on child, a static node whose
// key appends a "/" to the matched path: either a non-strict route on child itself or
// an empty match on one of its optional catch-all. Returns (-1, nil) when none match.
func matchTrailingSlash(child *node, method string, c *Context, lazy bool) (int, *node) {
	if child.isLeaf() {
		for i, route := range child.routes {
			if route.handleSlash != ExactSlash && route.match(method, c) {
				return i, child
			}
		}
	}
	// Since /foo/ and /foo/*{any} are permitted with different set of matchers and methods,
	// we still need to search for a matching empty catch-all.
	for _, wildcardNode := range child.wildcards {
		for i, route := range wildcardNode.routes {
			if route.handleSlash != ExactSlash && route.pattern.optionalCatchAll && route.match(method, c) {
				if !lazy {
					// record empty match
					*c.params = append(*c.params, "")
				}
				return i, wildcardNode
			}
		}
	}
	return -1, nil
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
//   - For wildcards *{name} or +{name}: returns (end, "*")
//   - For params with regex {name:pattern}: returns (end, "pattern")
//   - For wildcards with regex *{name:pattern}: returns (end, "pattern")
//   - For literal '*' or '+' (not followed by '{') and invalid/malformed segments:
//     returns (0, "") to signal static handling
//
// This is a lightweight parser that does not fully validate the segment. It assumes the caller
// will verify that the retrieved route's pattern matches the search pattern after tree lookup.
func parseBraceSegment(pattern string) (int, string) {
	length := len(pattern)
	if length == 0 {
		return 0, pattern
	}

	key := "?"
	if pattern[0] == starDelim || pattern[0] == plusDelim {
		if length < 2 || pattern[1] != bracketDelim {
			// Literal '*' or '+': the wildcard syntax requires an immediate '{'.
			return 0, ""
		}
		key = "*"
	}

	end := braceIndex(pattern, 0)
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
		sb.WriteString(route.pattern.str)

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
