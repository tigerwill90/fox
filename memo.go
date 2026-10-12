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
	// frontier records, for each regexp-free infix wildcard fully explored, the lowest offset where
	// its captures started. Captures ending past it are proven to fail.
	frontier []frontierEntry
	// regexps holds the state of the regexp wildcards visited, reused by truncating.
	regexps []*regexpMemo
	// failed holds the hostname nodes whose path lookup failed.
	failed []*node
	// matchFailed holds the routes whose matchers failed.
	matchFailed []*Route
	pathDelims  []int
	hostDelims  []int
	scratch     nfa.Scratch
	// work counts the capture candidates tried, the bytes evaluated by the regexps of wildcards
	// and the bytes of the failed path lookups started from a hostname, without the simulation
	// steps.
	work         int
	pathDelimsOK bool
	hostDelimsOK bool
	used         bool
	// big reports whether a buffer above maxMemoBuffer was allocated during the lookup.
	big bool
}

type frontierEntry struct {
	node *node
	off  int
}

func (m *lookupMemo) reset() {
	if m.big {
		m.release()
	}
	m.frontier = m.frontier[:0]
	m.regexps = m.regexps[:0]
	m.failed = m.failed[:0]
	m.matchFailed = m.matchFailed[:0]
	m.pathDelimsOK = false
	m.hostDelimsOK = false
	m.used = false
}

// release drops the buffers above maxMemoBuffer.
func (m *lookupMemo) release() {
	for _, rm := range m.regexps {
		rm.release()
	}
	if cap(m.pathDelims)*8 > maxMemoBuffer {
		m.pathDelims = nil
	}
	if cap(m.hostDelims)*8 > maxMemoBuffer {
		m.hostDelims = nil
	}
	m.big = false
}

// grew records a buffer of size bytes allocated during the lookup.
func (m *lookupMemo) grew(size int) {
	if size > maxMemoBuffer {
		m.big = true
	}
}

// bound returns the end of the range [start, end) left to search for capture ends of n.
func (m *lookupMemo) bound(n *node, start, end int) int {
	for i := range m.frontier {
		if m.frontier[i].node == n {
			return max(start, min(end, m.frontier[i].off+1))
		}
	}
	return end
}

// record marks every capture of n ending past off as failed.
func (m *lookupMemo) record(n *node, off int) {
	m.used = true
	for i := range m.frontier {
		if m.frontier[i].node == n {
			m.frontier[i].off = min(m.frontier[i].off, off)
			return
		}
	}
	m.frontier = append(m.frontier, frontierEntry{node: n, off: off})
}

// pathFailed reports whether the path lookup started from the hostname node n already failed.
func (m *lookupMemo) pathFailed(n *node) bool {
	return slices.Contains(m.failed, n)
}

func (m *lookupMemo) setPathFailed(n *node, path string) {
	m.used = true
	m.work += len(path)
	m.failed = append(m.failed, n)
}

// matcherFailed reports whether the matchers of r already failed.
func (m *lookupMemo) matcherFailed(r *Route) bool {
	return slices.Contains(m.matchFailed, r)
}

func (m *lookupMemo) setMatcherFailed(r *Route) {
	m.used = true
	m.matchFailed = append(m.matchFailed, r)
}

// regexp returns the state of the regexp wildcard n.
func (m *lookupMemo) regexp(n *node) *regexpMemo {
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

func (m *lookupMemo) delims(s string, delim byte) []int {
	if delim == slashDelim {
		if !m.pathDelimsOK {
			m.pathDelims = appendDelims(m.pathDelims[:0], s, delim)
			m.pathDelimsOK = true
			m.grew(cap(m.pathDelims) * 8)
		}
		return m.pathDelims
	}
	if !m.hostDelimsOK {
		m.hostDelims = appendDelims(m.hostDelims[:0], s, delim)
		m.hostDelimsOK = true
		m.grew(cap(m.hostDelims) * 8)
	}
	return m.hostDelims
}

func appendDelims(dst []int, s string, delim byte) []int {
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
// start: from the second scan on, the passes share the states they reach, so the scans of a node
// cost one pass in total.
type regexpMemo struct {
	node *node
	// ends lists the capture ends of the current scan that match, in increasing order.
	ends []int
	// tried marks the capture ends already explored by the scans that share their states.
	tried []uint64
	// starts marks the offsets where the regexp matches the rest of the input.
	starts []uint64
	seen   nfa.Seen
	// start is the capture start of the listed ends.
	start int
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

func (rm *regexpMemo) release() {
	if cap(rm.ends)*8 > maxMemoBuffer {
		rm.ends = nil
	}
	if cap(rm.tried)*8 > maxMemoBuffer {
		rm.tried = nil
	}
	if cap(rm.starts)*8 > maxMemoBuffer {
		rm.starts = nil
	}
	if rm.seen.Size() > maxMemoBuffer {
		rm.seen = nfa.Seen{}
	}
	rm.node = nil
}

// scan returns the state of the regexp wildcard n for the scan of its infix captures starting at
// start. A scan of a nested node is counted when it begins, and lists its capture ends right away
// from the second scan on. Once the regexp was evaluated in the scan, its capture ends are listed.
func (m *lookupMemo) scan(n *node, s string, start int, delim byte, evaluated bool) *regexpMemo {
	rm := m.regexp(n)
	if !evaluated {
		rm.scans++
		rm.listed = false
		if rm.scans > 1 {
			m.list(rm, s, start, delim)
		}
		return rm
	}
	if !rm.listed || rm.start != start {
		m.list(rm, s, start, delim)
	}
	return rm
}

// list computes the capture ends of the current scan that match. From the second scan of the node
// on, the pass drops the states an earlier pass already reached.
func (m *lookupMemo) list(rm *regexpMemo, s string, start int, delim byte) {
	var seen *nfa.Seen
	if rm.scans > 1 {
		if rm.scans == 2 {
			rm.seen.Reset(rm.node.nfa.Len(), m.delims(s, delim))
			rm.tried = resizeBits(rm.tried, len(s))
			m.grew(max(rm.seen.Size(), cap(rm.tried)*8))
		}
		seen = &rm.seen
	}
	rm.ends = rm.node.nfa.Ends(&m.scratch, s, start, delim, seen, rm.ends[:0])
	m.grew(cap(rm.ends) * 8)
	rm.start = start
	rm.cursor = 0
	rm.listed = true
}

// next returns the first listed capture end at or after from and before limit that was not tried,
// or -1 if none.
func (rm *regexpMemo) next(from, limit int) int {
	for ; rm.cursor < len(rm.ends); rm.cursor++ {
		e := rm.ends[rm.cursor]
		if e >= limit {
			return -1
		}
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

// match reports whether s matches the regexp of n.
func (m *lookupMemo) match(n *node, s string) bool {
	m.work += len(s)
	return n.regexp.MatchString(s)
}

// matchesRest reports whether the regexp of n matches s[start:], at the end of the scan starting at
// start. rm is the state of n for the scan, or nil.
func (m *lookupMemo) matchesRest(n *node, rm *regexpMemo, s string, start int) bool {
	if rm != nil && rm.listed && rm.start == start {
		return len(rm.ends) > 0 && rm.ends[len(rm.ends)-1] == len(s)
	}
	return m.match(n, s[start:])
}

// matchSuffix reports whether the regexp of n matches s[start:]. A lookup evaluates a node that
// is not nested once. For a nested node, a single backward pass answers for every start after the
// first evaluation. A wildcard starts a label or a segment, so start
// is 0 or follows a delimiter, as the backward pass requires.
func (m *lookupMemo) matchSuffix(n *node, s string, start int) bool {
	if !n.nested {
		return m.match(n, s[start:])
	}
	rm := m.regexp(n)
	rm.suffixes++
	if rm.suffixes == 1 {
		return m.match(n, s[start:])
	}
	if rm.suffixes == 2 {
		rm.starts = resizeBits(rm.starts, len(s))
		m.grew(cap(rm.starts) * 8)
		n.nfa.Starts(&m.scratch, s, rm.starts)
	}
	return rm.starts[start/64]&(1<<(start%64)) != 0
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
