package fox

import (
	"slices"

	"github.com/fox-toolkit/fox/internal/regprog"
)

// lookupMemo holds what a lookup has proven about wildcard nodes, so its backtracking never repeats
// the same work: the walk below a capture depends only on where the capture ends, and matchers
// never see the captured params. It lives in the pooled Context and is reset by the next lookup.
type lookupMemo struct {
	// vm and hostVM run the path and the hostname. They are allocated by the first lookup that
	// needs them.
	vm, hostVM *pikeVM
	// exhausted holds, for each regexp-free infix wildcard scanned to its end, the lowest start of
	// those scans. Captures ending past it are proven to fail.
	exhausted []exhaustedScan
	// regexps holds the state of the regexp wildcards visited, reused by truncating.
	regexps []*regexpMemo
	// failedPaths holds the hostname nodes whose path lookup failed.
	failedPaths []*node
	// failedMatchers holds the routes whose matchers failed.
	failedMatchers []*Route
	// work counts the capture candidates tried, the bytes evaluated by the regexps of wildcards,
	// the bytes of the failed path lookups started from a hostname and the Pike VM threads stepped,
	// without the steps of the listing passes.
	work int
	used bool
}

type exhaustedScan struct {
	node  *node
	start int
}

func (m *lookupMemo) reset() {
	m.exhausted = m.exhausted[:0]
	m.regexps = m.regexps[:0]
	m.failedPaths = m.failedPaths[:0]
	m.failedMatchers = m.failedMatchers[:0]
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

// regexpMemo is the state of a regexp wildcard during a lookup. An infix scan first evaluates the
// regexp on one capture, as most lookups need no more. When it needs a second evaluation, a single
// pass lists the capture ends that match, one at a time. The Pike VM takes over when a nested node
// is scanned or evaluated as a suffix a second time.
type regexpMemo struct {
	node *node
	// pass lists the capture ends of the scan that match.
	pass regprog.Pass
	// scans counts the infix scans of a nested node.
	scans int
	// suffixes counts the evaluations of a nested node on the rest of the input.
	suffixes int
	listed   bool
}

func (rm *regexpMemo) init(n *node) {
	rm.node = n
	rm.scans = 0
	rm.suffixes = 0
	rm.listed = false
}

// rescan counts a scan of the infix captures of the nested node n, and reports whether n was
// scanned before.
func (m *lookupMemo) rescan(n *node) bool {
	rm := m.regexpState(n)
	rm.scans++
	return rm.scans > 1
}

// listEnds returns the state of n with the capture ends of its scan from start listed. A node is
// scanned from a single start before the Pike VM takes over, and its ends are asked in increasing
// order, so the pass runs once. It starts over otherwise.
func (m *lookupMemo) listEnds(n *node, s string, start int, delim byte) *regexpMemo {
	rm := m.regexpState(n)
	if !rm.listed || !rm.pass.Started(n.prog, s, start) {
		rm.pass.Reset(n.prog, s, start, delim)
		rm.listed = true
	}
	return rm
}

// nextEnd returns the first listed capture end at or after from, or -1 if none.
func (rm *regexpMemo) nextEnd(from int) int {
	return rm.pass.Next(from)
}

// matchRegexp reports whether s matches the regexp of n.
func (m *lookupMemo) matchRegexp(n *node, s string) bool {
	m.work += len(s)
	return n.regexp.MatchString(s)
}

// resuffix counts an evaluation of the nested node n on the rest of the input, and reports whether
// n was evaluated before.
func (m *lookupMemo) resuffix(n *node) bool {
	rm := m.regexpState(n)
	rm.suffixes++
	return rm.suffixes > 1
}
