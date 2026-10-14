package fox

import (
	"slices"

	"github.com/fox-toolkit/fox/internal/regprog"
)

// lookupState holds what a lookup learns while it backtracks, so it never repeats the same work, and
// the Pike VMs it may run. It lives in the pooled Context and is reset by the next lookup that used
// it.
type lookupState struct {
	// pathPikeVM and hostPikeVM run the path and the hostname. They are allocated by the first lookup
	// that needs them.
	pathPikeVM, hostPikeVM *pikeVM
	// failedMatchers holds the routes whose matchers failed. Matchers never see the captured params.
	failedMatchers map[*Route]struct{}
	// failedPaths holds the hostname nodes whose path lookup failed. A path lookup ignores the
	// hostname captures.
	failedPaths []*node
	// exhausted holds, for each regexp-free infix wildcard scanned to its end, the lowest start of
	// those scans. The walk below a capture depends only on where the capture ends, so the captures
	// ending past that start fail.
	exhausted []exhaustedScan
	// regexps holds the regexp wildcards the lookup tracks, reused by truncating.
	regexps []*regexpWildcard
	// steps counts the capture candidates tried by the walk, the bytes evaluated by the regexps of
	// wildcards and by the path lookups that failed from a hostname, and the threads stepped by the Pike
	// VM, over the lookups run with this state. It exists to make linearity testable.
	steps int
	dirty bool
}

type exhaustedScan struct {
	node  *node
	start int
}

func (s *lookupState) reset() {
	if len(s.failedMatchers) > 0 {
		clear(s.failedMatchers)
	}
	s.failedPaths = s.failedPaths[:0]
	s.exhausted = s.exhausted[:0]
	s.regexps = s.regexps[:0]
	s.dirty = false
}

// scanEnd returns the end of the range [start, end) left to scan for capture ends of n.
func (s *lookupState) scanEnd(n *node, start, end int) int {
	for i := range s.exhausted {
		if s.exhausted[i].node == n {
			return max(start, min(end, s.exhausted[i].start+1))
		}
	}
	return end
}

// markExhausted records that the scan of n starting at start ran to its end, so every capture of n
// ending past start fails.
func (s *lookupState) markExhausted(n *node, start int) {
	s.dirty = true
	for i := range s.exhausted {
		if s.exhausted[i].node == n {
			s.exhausted[i].start = min(s.exhausted[i].start, start)
			return
		}
	}
	s.exhausted = append(s.exhausted, exhaustedScan{node: n, start: start})
}

// pathFailed reports whether the path lookup started from the hostname node n already failed.
func (s *lookupState) pathFailed(n *node) bool {
	return len(s.failedPaths) > 0 && slices.Contains(s.failedPaths, n)
}

func (s *lookupState) setPathFailed(n *node, path string) {
	s.dirty = true
	s.steps += len(path)
	s.failedPaths = append(s.failedPaths, n)
}

// matcherFailed reports whether the matchers of r already failed.
func (s *lookupState) matcherFailed(r *Route) bool {
	_, ok := s.failedMatchers[r]
	return ok
}

func (s *lookupState) setMatcherFailed(r *Route) {
	s.dirty = true
	if s.failedMatchers == nil {
		s.failedMatchers = make(map[*Route]struct{})
	}
	s.failedMatchers[r] = struct{}{}
}

// matchRegexp reports whether in matches the regexp of the wildcard n.
func (s *lookupState) matchRegexp(n *node, in string) bool {
	s.steps += len(in)
	return n.regexp.MatchString(in)
}

// regexpWildcard is a regexp wildcard during a lookup. An infix scan first evaluates the regexp on
// one capture, as most lookups need no more. When it needs a second evaluation, a single pass lists
// the capture ends that match, one at a time. The Pike VM takes over when a nested node is scanned
// over bytes a previous scan looked at, or tried as a suffix a second time.
type regexpWildcard struct {
	node *node
	// ends lists the capture ends of the scan that match.
	ends regprog.Ends
	// scanned and suffixed report whether the walk scanned the infix captures of a nested node, and
	// tried it on the rest of the input. listed reports whether the last scan looked at no byte past
	// the reach of its listing.
	scanned, suffixed, listed bool
}

// regexp returns the regexp wildcard n tracked by the lookup.
func (s *lookupState) regexp(n *node) *regexpWildcard {
	if i := slices.IndexFunc(s.regexps, func(w *regexpWildcard) bool { return w.node == n }); i >= 0 {
		return s.regexps[i]
	}
	s.dirty = true
	// Reuse the entry of an earlier lookup with its buffers, if any.
	var w *regexpWildcard
	if len(s.regexps) < cap(s.regexps) {
		w = s.regexps[:len(s.regexps)+1][len(s.regexps)]
	}
	if w == nil {
		w = new(regexpWildcard)
	} else {
		// The listing of an earlier lookup is never resumed.
		w.ends.Reset()
	}
	s.regexps = append(s.regexps, w)
	w.node, w.scanned, w.suffixed = n, false, false
	return w
}

// retryScan reports whether a scan of the nested node n from start would look at bytes an earlier
// scan looked at, and records this scan. A scan that did not list its ends ran to the end of the input.
func (s *lookupState) retryScan(n *node, start int) bool {
	w := s.regexp(n)
	retry := w.scanned && (!w.listed || w.ends.Reach() >= start)
	w.scanned, w.listed = true, false
	return retry
}

// retrySuffix reports whether the walk tried the nested node n on the rest of the input before, and
// records this try.
func (s *lookupState) retrySuffix(n *node) bool {
	w := s.regexp(n)
	retry := w.suffixed
	w.suffixed = true
	return retry
}

// ends returns the listing of the capture ends of the scan of n from start. A scan asks for its ends
// in increasing order, so its listing runs once. It starts over otherwise.
func (s *lookupState) ends(n *node, in string, start int, delim byte) *regprog.Ends {
	w := s.regexp(n)
	w.listed = true
	w.ends.Start(n.prog, in, start, delim)
	return &w.ends
}

// matchTail reports whether the rest of the input from the start of the scan of n matches its regexp.
// The scan then looked at the input to its end.
func (s *lookupState) matchTail(n *node, in string) bool {
	if n.nested {
		s.regexp(n).listed = false
	}
	return s.matchRegexp(n, in)
}

func (s *lookupState) pathVM() *pikeVM {
	if s.pathPikeVM == nil {
		s.pathPikeVM = &pikeVM{delim: slashDelim}
	}
	return s.pathPikeVM
}

func (s *lookupState) hostVM() *pikeVM {
	if s.hostPikeVM == nil {
		s.hostPikeVM = &pikeVM{delim: dotDelim, host: true}
	}
	return s.hostPikeVM
}
