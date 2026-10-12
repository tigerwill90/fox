package fox

import (
	"slices"
	"strings"

	"github.com/fox-toolkit/fox/internal/nfa"
)

// maxMemoBuffer is the size in bytes above which a memo buffer is released when the next lookup
// resets the memo, so a pooled Context does not keep the memory of an unusually long request.
const maxMemoBuffer = 64 << 10

// lookupMemo holds what a lookup has proven about wildcard nodes, so its backtracking never repeats
// the same work: the walk below a capture depends only on where the capture ends, and matchers
// never see the captured params. It lives in the pooled Context and is reset by the next lookup.
type lookupMemo struct {
	// exhausted holds, for each regexp-free infix wildcard scanned to its end, the lowest start of
	// those scans. Captures ending past it are proven to fail.
	exhausted []exhaustedScan
	// regexps holds the state of the regexp wildcards visited, reused by truncating.
	regexps []*regexpMemo
	// failedPaths holds the hostname nodes whose path lookup failed.
	failedPaths []*node
	// failedMatchers holds the routes whose matchers failed.
	failedMatchers []*Route
	pathDelims     []int
	hostDelims     []int
	scratch        nfa.Scratch
	// work counts the capture candidates tried, the bytes evaluated by the regexps of wildcards
	// and the bytes of the failed path lookups started from a hostname, without the simulation
	// steps.
	work         int
	pathDelimsOK bool
	hostDelimsOK bool
	used         bool
}

type exhaustedScan struct {
	node  *node
	start int
}

func (m *lookupMemo) reset() {
	for _, rm := range m.regexps {
		rm.release()
	}
	if cap(m.pathDelims)*8 > maxMemoBuffer {
		m.pathDelims = nil
	}
	if cap(m.hostDelims)*8 > maxMemoBuffer {
		m.hostDelims = nil
	}
	m.exhausted = m.exhausted[:0]
	m.regexps = m.regexps[:0]
	m.failedPaths = m.failedPaths[:0]
	m.failedMatchers = m.failedMatchers[:0]
	m.pathDelimsOK = false
	m.hostDelimsOK = false
	m.used = false
}

// searchEnd returns the end of the range [start, end) left to search for capture ends of n.
func (m *lookupMemo) searchEnd(n *node, start, end int) int {
	for i := range m.exhausted {
		if m.exhausted[i].node == n {
			return max(start, min(end, m.exhausted[i].start+1))
		}
	}
	return end
}

// markExhausted records that the scan of n starting at start ran to its end, so every capture of n
// ending past start fails.
func (m *lookupMemo) markExhausted(n *node, start int) {
	m.used = true
	for i := range m.exhausted {
		if m.exhausted[i].node == n {
			m.exhausted[i].start = min(m.exhausted[i].start, start)
			return
		}
	}
	m.exhausted = append(m.exhausted, exhaustedScan{node: n, start: start})
}

// pathFailed reports whether the path lookup started from the hostname node n already failed.
func (m *lookupMemo) pathFailed(n *node) bool {
	return len(m.failedPaths) > 0 && slices.Contains(m.failedPaths, n)
}

func (m *lookupMemo) setPathFailed(n *node, path string) {
	m.used = true
	m.work += len(path)
	m.failedPaths = append(m.failedPaths, n)
}

// matcherFailed reports whether the matchers of r already failed.
func (m *lookupMemo) matcherFailed(r *Route) bool {
	return slices.Contains(m.failedMatchers, r)
}

func (m *lookupMemo) setMatcherFailed(r *Route) {
	m.used = true
	m.failedMatchers = append(m.failedMatchers, r)
}

// regexpState returns the state of the regexp wildcard n.
func (m *lookupMemo) regexpState(n *node) *regexpMemo {
	if i := slices.IndexFunc(m.regexps, func(rm *regexpMemo) bool { return rm.node == n }); i >= 0 {
		return m.regexps[i]
	}
	m.used = true
	// Reuse the entry of an earlier lookup with its buffers, if any.
	var rm *regexpMemo
	if len(m.regexps) < cap(m.regexps) {
		rm = m.regexps[:len(m.regexps)+1][len(m.regexps)]
	}
	if rm == nil {
		rm = new(regexpMemo)
	}
	m.regexps = append(m.regexps, rm)
	rm.init(n)
	return rm
}

// delims returns the positions of delim in s, the path or the hostname of the lookup.
func (m *lookupMemo) delims(s string, delim byte) []int {
	if delim == slashDelim {
		if !m.pathDelimsOK {
			m.pathDelims = appendDelims(m.pathDelims[:0], s, delim)
			m.pathDelimsOK = true
		}
		return m.pathDelims
	}
	if !m.hostDelimsOK {
		m.hostDelims = appendDelims(m.hostDelims[:0], s, delim)
		m.hostDelimsOK = true
	}
	return m.hostDelims
}

func appendDelims(dst []int, s string, delim byte) []int {
	dst = slices.Grow(dst, strings.Count(s, string(delim)))
	for i := 0; ; {
		idx := strings.IndexByte(s[i:], delim)
		if idx < 0 {
			return dst
		}
		dst = append(dst, i+idx)
		i += idx + 1
	}
}

// regexpMemo is the state of a regexp wildcard during a lookup. An infix scan first evaluates the
// regexp on one capture, as most lookups need no more. When it needs a second evaluation, a single
// pass lists every capture end that matches. A nested node can be scanned again from another
// start: from the second scan on, the passes share the states they visit, so the scans of a node
// cost one pass in total.
type regexpMemo struct {
	node *node
	// ends lists the capture ends of the current scan that match, in increasing order, without the
	// ends an earlier scan of the node already reached.
	ends []int
	// tried marks the capture ends already explored by the scans that share their states.
	tried []uint64
	// suffixMatch marks the offsets where the regexp matches the rest of the input.
	suffixMatch []uint64
	visited     nfa.Visited
	// listStart is the capture start of the listed ends.
	listStart int
	// cursor is the index in ends of the next capture end to try.
	cursor int
	// scans counts the scans of a nested node.
	scans int
	// suffixes counts the evaluations of the regexp on the rest of the input.
	suffixes int
	listed   bool
}

func (rm *regexpMemo) init(n *node) {
	rm.node = n
	rm.scans = 0
	rm.suffixes = 0
	rm.listed = false
}

// release drops the buffers above maxMemoBuffer.
func (rm *regexpMemo) release() {
	if cap(rm.ends)*8 > maxMemoBuffer {
		rm.ends = nil
	}
	if cap(rm.tried)*8 > maxMemoBuffer {
		rm.tried = nil
	}
	if cap(rm.suffixMatch)*8 > maxMemoBuffer {
		rm.suffixMatch = nil
	}
	if rm.visited.Size() > maxMemoBuffer {
		rm.visited = nfa.Visited{}
	}
}

// beginScan starts a scan of the infix captures of the nested node n from start. From the second
// scan of the node on, its capture ends are listed right away.
func (m *lookupMemo) beginScan(n *node, s string, start int, delim byte) *regexpMemo {
	rm := m.regexpState(n)
	rm.scans++
	rm.listed = false
	if rm.scans > 1 {
		m.computeEnds(rm, s, start, delim)
	}
	return rm
}

// listEnds returns the state of n with the capture ends of its scan from start listed.
func (m *lookupMemo) listEnds(n *node, s string, start int, delim byte) *regexpMemo {
	rm := m.regexpState(n)
	if !rm.listed || rm.listStart != start {
		m.computeEnds(rm, s, start, delim)
	}
	return rm
}

// computeEnds lists the capture ends of the scan from start that match. From the second scan of the
// node on, the pass drops the states an earlier pass already visited.
func (m *lookupMemo) computeEnds(rm *regexpMemo, s string, start int, delim byte) {
	var visited *nfa.Visited
	if rm.scans > 1 {
		if rm.scans == 2 {
			rm.visited.Reset(rm.node.nfa, m.delims(s, delim))
			rm.tried = resizeBits(rm.tried, len(s))
		}
		visited = &rm.visited
	}
	rm.ends = rm.node.nfa.Ends(&m.scratch, s, start, delim, visited, rm.ends[:0])
	rm.listStart = start
	rm.cursor = 0
	rm.listed = true
}

// nextEnd returns the first listed capture end at or after from that was not tried, or -1 if none.
func (rm *regexpMemo) nextEnd(from int) int {
	for ; rm.cursor < len(rm.ends); rm.cursor++ {
		e := rm.ends[rm.cursor]
		if e >= from && (rm.scans < 2 || rm.tried[e/64]&(1<<(e%64)) == 0) {
			return e
		}
	}
	return -1
}

// markTried records that the capture end e is explored.
func (rm *regexpMemo) markTried(e int) {
	if rm.scans > 1 {
		rm.tried[e/64] |= 1 << (e % 64)
	}
}

// matchRegexp reports whether s matches the regexp of n.
func (m *lookupMemo) matchRegexp(n *node, s string) bool {
	m.work += len(s)
	return n.regexp.MatchString(s)
}

// matchSuffix reports whether the regexp of n matches s[start:]. A lookup evaluates a node that is
// not nested once. For a nested node, a single backward pass answers for every start after the first
// evaluation. A wildcard starts a label or a segment, so start is 0 or follows a delimiter, as the
// backward pass requires.
func (m *lookupMemo) matchSuffix(n *node, s string, start int) bool {
	if !n.nested {
		return m.matchRegexp(n, s[start:])
	}
	rm := m.regexpState(n)
	rm.suffixes++
	if rm.suffixes == 1 {
		return m.matchRegexp(n, s[start:])
	}
	if rm.suffixes == 2 {
		rm.suffixMatch = resizeBits(rm.suffixMatch, len(s))
		n.nfa.Starts(&m.scratch, s, rm.suffixMatch)
	}
	return rm.suffixMatch[start/64]&(1<<(start%64)) != 0
}

// resizeBits returns a cleared bitset of n bits, reusing bits when it is large enough.
func resizeBits(bits []uint64, n int) []uint64 {
	words := (n + 63) / 64
	if cap(bits) < words {
		return make([]uint64, words)
	}
	bits = bits[:words]
	clear(bits)
	return bits
}
