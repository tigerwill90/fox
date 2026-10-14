package fox

import (
	"slices"

	"github.com/fox-toolkit/fox/internal/regprog"
)

type lookupState struct {
	pathPikeVM, hostPikeVM *pikeVM
	failedMatchers         map[*Route]struct{}
	failedPaths            []*node
	// For each infix wildcard without regexp scanned to the end, the lowest start of those scans.
	// The walk below a capture depends only on where it ends, so captures ending past that start fail.
	exhausted []exhaustedScan
	regexps   []*regexpWildcard
	// steps is only read by tests.
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

func (s *lookupState) pathFailed(n *node) bool {
	return len(s.failedPaths) > 0 && slices.Contains(s.failedPaths, n)
}

func (s *lookupState) setPathFailed(n *node, path string) {
	s.dirty = true
	s.steps += len(path)
	s.failedPaths = append(s.failedPaths, n)
}

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

func (s *lookupState) matchRegexp(n *node, in string) bool {
	s.steps += len(in)
	return n.regexp.MatchString(in)
}

// regexpWildcard is the lookup state of a regexp wildcard. An infix scan matches the regexp against
// its first capture only, as most lookups need no more. From the second evaluation, the matching
// capture ends are listed in a single pass. A nested node runs in the Pike VM when a scan overlaps
// a previous scan, or on its second evaluation as a suffix catch-all.
type regexpWildcard struct {
	node *node
	ends regprog.Ends
	// listed reports whether the last scan ended within the reach of its listing.
	scanned, suffixed, listed bool
}

// regexp returns the lookup state of n, added on first use.
func (s *lookupState) regexp(n *node) *regexpWildcard {
	if i := slices.IndexFunc(s.regexps, func(w *regexpWildcard) bool { return w.node == n }); i >= 0 {
		return s.regexps[i]
	}
	s.dirty = true
	// reset truncates regexps, so the entries past its length keep their buffers.
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

// retryScan records a scan of the nested node n from start, and reports whether it overlaps an
// earlier scan. A scan without listing is assumed to run to the end of the input.
func (s *lookupState) retryScan(n *node, start int) bool {
	w := s.regexp(n)
	retry := w.scanned && (!w.listed || w.ends.Reach() >= start)
	w.scanned, w.listed = true, false
	return retry
}

// retrySuffix records an evaluation of the nested node n as a suffix catch-all, and reports whether
// it is not the first.
func (s *lookupState) retrySuffix(n *node) bool {
	w := s.regexp(n)
	retry := w.suffixed
	w.suffixed = true
	return retry
}

// ends returns the listing of the capture ends of n from start. A scan calls Next with increasing
// positions, so the listing runs once.
func (s *lookupState) ends(n *node, in string, start int, delim byte) *regprog.Ends {
	w := s.regexp(n)
	w.listed = true
	w.ends.Start(n.prog, in, start, delim)
	return &w.ends
}

// matchTail reports whether in, the input from the scan start to the end, matches the regexp of n.
// The scan then extends to the end of the input.
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
