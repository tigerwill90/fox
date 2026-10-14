package fox

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/fox-toolkit/fox/internal/regprog"
	"github.com/fox-toolkit/fox/internal/stringsutil"
)

// Kinds of Pike VM threads.
const (
	vmStatic       uint8 = iota // consumes the key of a static node
	vmParam                     // consumes a param segment
	vmInfix                     // consumes an infix capture without regexp
	vmInfixRegexp               // consumes an infix capture with a regexp
	vmSuffixRegexp              // consumes a suffix capture with a regexp
)

// pikeVM runs the walk of lookupByPath, or of lookupByHostname, below a node in a single pass over
// the input. Threads are ordered by walk priority, and a state reached twice at the same position
// keeps only the first thread, so memory is bounded by the size of the subtree.
type pikeVM struct {
	c        *Context
	bestNode *node
	method   string
	// s is the hostname or the path, path is the path of the lookup.
	s, path string
	nodes   []vmNode
	// kids holds the kid slots of nodes, -1 until linked.
	kids        []int32
	seen        []uint64
	cur, next   *vmList
	lists       [2]vmList
	matched     []uint32
	caps, spare []int
	best        []int
	hits        []vmHit
	hitParams   []string
	stack       []uint32
	capsLimit   int
	stamp       uint64
	pos         uint64
	bestIdx     int
	bestHit     int
	// size is the number of states of the linked nodes.
	size                 int32
	prevClass, nextClass uint8
	delim                byte
	host, lazy           bool
	bestTsr              bool
}

// vmNode is referred to by its index in pikeVM.nodes. Linking grows nodes, so a *vmNode is never
// kept across a call that can link. Indexes into the subtree are bounded by the program and fit in an
// int32.
type vmNode struct {
	n, parent *node
	// id is the state of the visit, id+1 the state of the thread that consumes the key, segment or
	// capture.
	id int32
	// infix and suffix are the first states of the program for an infix capture and a suffix capture.
	infix, suffix int32
	// kids is the first slot in pikeVM.kids, for the statics, params, then wildcards. The root has slots
	// for its wildcards only.
	kids, statics, params int32
}

type vmThread struct {
	kind uint8
	// wait is the number of bytes left in the rune being consumed. The states are then the rune
	// instructions that matched it.
	wait uint8
	node int32
	// lo and hi delimit the regexp states of the thread in vmList.pcs.
	lo, hi int32
	// caps is the offset of the capture block in pikeVM.caps.
	caps int
	// start is the start of the capture, or for a static thread the number of key bytes consumed.
	start int
}

// A capture block in pikeVM.caps holds a number n of captures followed by their n pairs of start and
// end. Blocks never change, so threads share them. noCaps is the offset of the empty block.
const noCaps = 0

type vmList struct {
	threads []vmThread
	pcs     []uint32
}

func (l *vmList) reset() {
	l.threads = l.threads[:0]
	l.pcs = l.pcs[:0]
}

// vmHit is a matching path lookup of a hostname VM.
type vmHit struct {
	from, leaf *node
	idx        int
	// lo and hi delimit the params of the path in pikeVM.hitParams.
	lo, hi int
	tsr    bool
}

// outermostInfix returns the skip node of the outermost infix wildcard of the lookup from
// stackOffset, and truncates the skip stack and the params to it.
func outermostInfix(c *Context, stackOffset int) skipNode {
	stack := (*c.skipStack)[stackOffset:]
	i := slices.IndexFunc(stack, func(f skipNode) bool { return f.wildcardOffset > 0 })
	if i < 0 {
		panic("internal error: nested wildcard outside of an infix capture")
	}
	f := stack[i]
	*c.skipStack = (*c.skipStack)[:stackOffset+i]
	*c.params = (*c.params)[:f.paramCnt]
	return f
}

// run returns the match of the walk from the wildcard f.childWildcardIdx of f.node at f.charsMatched,
// or a nil node.
func (vm *pikeVM) run(method, s, path string, c *Context, lazy bool, f skipNode) (int, *node, bool) {
	vm.c, vm.method, vm.s, vm.path, vm.lazy = c, method, s, path, lazy
	vm.nodes, vm.kids, vm.seen, vm.size = vm.nodes[:0], vm.kids[:0], vm.seen[:0], 0
	root := vm.link(f.node, nil, true)
	vm.bestNode = nil
	vm.hits, vm.hitParams = vm.hits[:0], vm.hitParams[:0]
	vm.caps, vm.capsLimit = append(vm.caps[:0], 0), minCapsLimit

	// Same buffer roles across lookups, so a repeated lookup allocates nothing.
	vm.cur, vm.next = &vm.lists[0], &vm.lists[1]
	vm.next.reset()
	vm.pos = vm.tick()
	from := vm.begin(root, f)
	if from < len(s) {
		vm.nextClass = regprog.Class(s[from])
	}
	moved := false
	for q := from; q < len(s) && len(vm.next.threads) > 0; q++ {
		vm.cur, vm.next = vm.next, vm.cur
		vm.next.reset()
		vm.pos = vm.tick()
		if len(vm.caps) > vm.capsLimit {
			vm.moveCaps()
			moved = !moved
		}
		c.lookup.steps += len(vm.cur.threads)
		vm.prevClass = vm.nextClass
		if q+1 < len(s) {
			vm.nextClass = regprog.Class(s[q+1])
		}
		cur := vm.cur
		for i := range cur.threads {
			if vm.step(&cur.threads[i], cur.pcs, q) {
				break
			}
		}
	}
	if moved {
		vm.caps, vm.spare = vm.spare, vm.caps
	}

	n := vm.bestNode
	if n != nil && !lazy {
		best := vm.best
		for i := 0; i < len(best); i += 2 {
			*c.params = append(*c.params, s[best[i]:best[i+1]])
		}
		if vm.host {
			h := &vm.hits[vm.bestHit]
			*c.params = append(*c.params, vm.hitParams[h.lo:h.hi]...)
		}
	}
	// The pooled VM must not reference tree nodes.
	clear(vm.nodes)
	clear(vm.hits)
	vm.c, vm.bestNode = nil, nil
	if n == nil {
		return 0, nil, false
	}
	return vm.bestIdx, n, vm.bestTsr
}

// begin adds the first threads of the walk from f and returns their position. The captures of the
// wildcard of f ending before the current one already failed, so the walk can resume at the end of
// the current capture. A regexp thread must start at the start of its capture, so begin resumes there
// only when no regexp thread starts at f.charsMatched.
func (vm *pikeVM) begin(root int32, f skipNode) int {
	i := f.childWildcardIdx
	if n := f.node; n.wildcards[i].prog == nil {
		for _, w := range n.wildcards {
			if w.prog != nil && vm.isSuffix(w) {
				vm.wildcards(root, f.charsMatched, i, noCaps)
				return f.charsMatched
			}
		}
		// The suffix catch-alls of f.node come last in the walk, so a later match replaces theirs.
		vm.wildcards(root, f.charsMatched, i+1, noCaps)
		end := f.charsMatched + f.wildcardOffset - 1
		kid := vm.kid(root, int32(i), n.wildcards[i]) // nolint:gosec // a child index
		if !vm.visit(kid, end, noCaps, f.charsMatched) {
			vm.spawn(vmInfix, kid, noCaps, f.charsMatched)
		}
		return end
	}
	vm.wildcards(root, f.charsMatched, i, noCaps)
	return f.charsMatched
}

func (vm *pikeVM) tick() uint64 {
	vm.stamp++
	return vm.stamp
}

// link adds n with its states and kid slots, and returns its index.
func (vm *pikeVM) link(n, parent *node, root bool) int32 {
	v := int32(len(vm.nodes))                                                  // nolint:gosec // a node index
	nd := vmNode{n: n, parent: parent, id: vm.size, kids: int32(len(vm.kids))} // nolint:gosec // a slot index
	vm.size += 2
	if n.prog != nil {
		size := int32(n.prog.Len()) // nolint:gosec // a program size
		nd.infix, nd.suffix = vm.size, vm.size+size
		vm.size += 2 * size
	}
	if !root {
		nd.statics, nd.params = int32(len(n.statics)), int32(len(n.params)) // nolint:gosec // child counts
	}
	vm.nodes = append(vm.nodes, nd)
	for range int(nd.statics+nd.params) + len(n.wildcards) {
		vm.kids = append(vm.kids, -1)
	}
	// States past the previous size hold stamps of earlier runs only.
	vm.seen = slices.Grow(vm.seen, int(vm.size)-len(vm.seen))[:vm.size]
	return v
}

// kid returns the node of the kid slot of v, and links child to it on first use.
func (vm *pikeVM) kid(v, slot int32, child *node) int32 {
	nd := &vm.nodes[v]
	i := nd.kids + slot
	if k := vm.kids[i]; k >= 0 {
		return k
	}
	k := vm.link(child, nd.n, false)
	vm.kids[i] = k
	return k
}

// minCapsLimit is the lowest pikeVM.capsLimit.
const minCapsLimit = 1 << 7

// moveCaps moves the capture blocks of the current threads to the spare buffer and drops the others.
func (vm *pikeVM) moveCaps() {
	old, caps := vm.caps, append(vm.spare[:0], 0)
	for i := range vm.cur.threads {
		t := &vm.cur.threads[i]
		if t.caps == noCaps {
			continue
		}
		// The header of a moved block is replaced by its new offset, as a negative number.
		if n := old[t.caps]; n < 0 {
			t.caps = -1 - n
			continue
		}
		off := len(caps)
		caps = append(caps, old[t.caps:t.caps+1+2*old[t.caps]]...)
		old[t.caps] = -1 - off
		t.caps = off
	}
	vm.caps, vm.spare = caps, old
	vm.capsLimit = max(minCapsLimit, 2*len(caps))
}

// step advances t over the byte at q. pcs holds the regexp states of the current threads. It reports
// whether a match cuts the threads after t.
func (vm *pikeVM) step(t *vmThread, pcs []uint32, q int) bool {
	p := q + 1
	s := vm.s
	nd := &vm.nodes[t.node]
	caps, start := t.caps, t.start
	switch t.kind {
	case vmStatic:
		if start+1 == len(nd.n.key) {
			return vm.visit(t.node, p, caps, -1)
		}
		// The key was checked when the thread was added, and only one thread consumes it.
		vm.next.threads = append(vm.next.threads, vmThread{kind: vmStatic, node: t.node, caps: caps, start: start + 1})
		return false
	case vmParam:
		if p == len(s) || s[p] == vm.delim {
			return vm.visit(t.node, p, caps, start)
		}
		vm.spawn(vmParam, t.node, caps, start)
		return false
	case vmInfix:
		if p == len(s) {
			return !vm.host && vm.infixTsr(nd.n, nd.parent, caps, start)
		}
		if s[p] == vm.delim && vm.visit(t.node, p, caps, start) {
			return true
		}
		vm.spawn(vmInfix, t.node, caps, start)
		return false
	}

	prog, pcs := nd.n.prog, pcs[t.lo:t.hi]
	base := nd.infix
	if t.kind == vmSuffixRegexp {
		base = nd.suffix
	}
	switch {
	case t.wait > 1:
		vm.push(vmThread{kind: t.kind, wait: t.wait - 1, node: t.node, caps: caps, start: start}, pcs)
		return false
	case t.wait == 0:
		matched, w := vm.matched[:0], 1
		if b := s[q]; b < utf8.RuneSelf {
			sets := prog.ASCII()
			for _, pc := range pcs {
				if sets.Has(pc, b) {
					matched = append(matched, pc)
				}
			}
		} else {
			var r rune
			r, w = utf8.DecodeRuneInString(s[q:])
			for _, pc := range pcs {
				if prog.MatchRune(pc, r) {
					matched = append(matched, pc)
				}
			}
		}
		vm.matched = matched
		if len(matched) == 0 {
			return false
		}
		if w > 1 {
			// The instructions that follow are reached once the last byte of the rune is consumed.
			vm.push(vmThread{kind: t.kind, wait: uint8(w - 1), node: t.node, caps: caps, start: start}, matched) // nolint:gosec // a rune size
			return false
		}
		pcs = matched
	}

	// pcs holds the rune instructions that matched the rune ending at p.
	end := p == len(s)
	if (end || t.kind == vmInfixRegexp && s[p] == vm.delim) && prog.Accepts(vm.prevClass, pcs) {
		switch {
		case t.kind == vmSuffixRegexp:
			return vm.matchSuffix(nd.n, vm.capture(caps, start, p))
		case end:
			return !vm.host && vm.infixTsr(nd.n, nd.parent, caps, start)
		case vm.visit(t.node, p, caps, start):
			return true
		}
	}
	if end {
		return false
	}
	list := vm.next
	lo := len(list.pcs)
	next := list.pcs
	if prog.Expands() {
		next = prog.Next(next, pcs, vm.prevClass, vm.nextClass, vm.seen[base:int(base)+prog.Len()], vm.pos, &vm.stack)
	} else {
		seen, pos := vm.seen, vm.pos
		follow := prog.Follow(vm.prevClass, vm.nextClass)
		for _, pc := range pcs {
			for _, f := range follow.Of(pc) {
				if id := base + int32(f); seen[id] != pos { // nolint:gosec // an instruction index
					seen[id] = pos
					next = append(next, f)
				}
			}
		}
	}
	list.pcs = next
	if len(next) > lo {
		list.threads = append(list.threads, vmThread{kind: t.kind, node: t.node, lo: int32(lo), hi: int32(len(next)), caps: caps, start: start}) // nolint:gosec
	}
	return false
}

// matchSuffix reports whether the suffix catch-all n matches, with a capture to the end of the input.
func (vm *pikeVM) matchSuffix(n *node, caps int) bool {
	if vm.host {
		return vm.pathLookup(n, caps)
	}
	for i, route := range n.routes {
		if route.match(vm.method, vm.c) {
			return vm.found(i, n, false, caps)
		}
	}
	return false
}

// visit runs the walk at the node v reached at p, with the capture from start to p unless start is
// negative. It reports whether a match cuts the threads after it.
func (vm *pikeVM) visit(v int32, p, caps, start int) bool {
	nd := &vm.nodes[v]
	if vm.seen[nd.id] == vm.pos {
		return false
	}
	vm.seen[nd.id] = vm.pos
	n, parent := nd.n, nd.parent
	if start >= 0 {
		caps = vm.capture(caps, start, p)
	}
	if vm.host {
		return vm.visitHost(v, p, caps)
	}
	path := vm.s
	if p == len(path) {
		return vm.atEnd(n, parent, caps)
	}

	if i, child := n.getStaticEdge(path[p]); child != nil {
		rest, key := path[p:], child.key
		if strings.HasPrefix(rest, key) {
			vm.spawn(vmStatic, vm.kid(v, int32(i), child), caps, 0) // nolint:gosec // a child index
		} else if len(key) == len(rest)+1 && key[len(key)-1] == slashDelim && key[len(key)-2] != slashDelim && strings.HasPrefix(key, rest) {
			if i, tn := matchTrailingSlash(child, vm.method, vm.c, true); tn != nil {
				return vm.found(i, tn, true, vm.empty(caps, tn != child))
			}
		}
	}

	if len(n.params) > 0 {
		if path[p] == slashDelim {
			if vm.backtrack(n, p, caps) {
				return true
			}
			if len(n.wildcards) == 0 {
				return false
			}
		} else {
			vm.params(v, p, caps)
		}
	}
	return vm.wildcards(v, p, 0, caps)
}

// visitHost runs the walk at the node v reached at p, as lookupByHostname does.
func (vm *pikeVM) visitHost(v int32, p, caps int) bool {
	n, host := vm.nodes[v].n, vm.s
	if p == len(host) {
		if _, child := n.getStaticEdge(slashDelim); child != nil {
			return vm.pathLookup(n, caps)
		}
		return false
	}
	if i, child := n.getStaticEdge(stringsutil.ToLowerASCII(host[p])); child != nil {
		if key := child.key; len(key) <= len(host)-p && stringsutil.EqualStringsASCIIIgnoreCase(host[p:p+len(key)], key) {
			vm.spawn(vmStatic, vm.kid(v, int32(i), child), caps, 0) // nolint:gosec // a child index
		}
	}
	if len(n.params) > 0 && host[p] != dotDelim {
		vm.params(v, p, caps)
	}
	return vm.wildcards(v, p, 0, caps)
}

// params adds the threads of the params of the node v that match the segment at p.
func (vm *pikeVM) params(v int32, p, caps int) {
	nd := &vm.nodes[v]
	n, slot := nd.n, nd.statics
	segment := vm.s[p:]
	if end := strings.IndexByte(segment, vm.delim); end >= 0 {
		segment = segment[:end]
	}
	for i, param := range n.params {
		if param.regexp == nil || param.regexp.MatchString(segment) {
			vm.spawn(vmParam, vm.kid(v, slot+int32(i), param), caps, p) // nolint:gosec // a child index
		}
	}
}

// wildcards runs the walk at the wildcards of the node v from the one at from, reached at p.
func (vm *pikeVM) wildcards(v int32, p, from, caps int) bool {
	nd := &vm.nodes[v]
	n, slot := nd.n, nd.statics+nd.params
	if len(n.wildcards) == 0 {
		return vm.backtrack(n, p, caps)
	}
	for i := from; i < len(n.wildcards); i++ {
		w := n.wildcards[i]
		if !vm.isInfix(w) {
			continue
		}
		kid := vm.kid(v, slot+int32(i), w) // nolint:gosec // a child index
		if w.prog == nil {
			vm.spawn(vmInfix, kid, caps, p)
		} else {
			vm.start(vmInfixRegexp, kid, p, caps)
		}
		if i+1 < len(n.wildcards) && vm.backtrack(n, p, caps) {
			return true
		}
	}

	for i, w := range n.wildcards {
		if !vm.isSuffix(w) {
			continue
		}
		if w.prog != nil {
			vm.start(vmSuffixRegexp, vm.kid(v, slot+int32(i), w), p, caps) // nolint:gosec // a child index
			continue
		}
		if vm.host {
			if vm.pathLookup(w, vm.capture(caps, p, len(vm.s))) {
				return true
			}
			continue
		}
		for j, route := range w.routes {
			if route.match(vm.method, vm.c) {
				return vm.found(j, w, false, vm.capture(caps, p, len(vm.s)))
			}
		}
	}
	return vm.backtrack(n, p, caps)
}

// isInfix reports whether w is an infix wildcard.
func (vm *pikeVM) isInfix(w *node) bool {
	if vm.host {
		return len(w.statics) != 1 || w.statics[0].label != slashDelim
	}
	return len(w.statics) > 0
}

// isSuffix reports whether w is a suffix catch-all.
func (vm *pikeVM) isSuffix(w *node) bool {
	if vm.host {
		return len(w.statics) != 1 || w.statics[0].label != dotDelim
	}
	return w.isLeaf()
}

// backtrack returns the tsr match of lookupByPath at its Backtrack label, at n.
func (vm *pikeVM) backtrack(n *node, p, caps int) bool {
	path := vm.s
	if vm.host || p != len(path)-1 || path[p] != slashDelim || !n.isLeaf() || strings.HasSuffix(path, "//") {
		return false
	}
	for i, route := range n.routes {
		if route.handleSlash != ExactSlash && !route.pattern.catchAll && route.match(vm.method, vm.c) {
			return vm.found(i, n, true, caps)
		}
	}
	return false
}

// atEnd returns the match of lookupByPath at n, a child of parent, once the path is consumed.
func (vm *pikeVM) atEnd(n, parent *node, caps int) bool {
	path := vm.s
	for i, route := range n.routes {
		if route.match(vm.method, vm.c) {
			return vm.found(i, n, false, caps)
		}
	}
	for _, w := range n.wildcards {
		for i, route := range w.routes {
			if route.pattern.optionalCatchAll && route.match(vm.method, vm.c) {
				return vm.found(i, w, false, vm.empty(caps, true))
			}
		}
	}
	if _, child := n.getStaticEdge(slashDelim); child != nil && child.key == "/" && !strings.HasSuffix(path, "/") {
		if i, tn := matchTrailingSlash(child, vm.method, vm.c, true); tn != nil {
			return vm.found(i, tn, true, vm.empty(caps, tn != child))
		}
	} else if n.key == "/" && parent != nil && parent.isLeaf() && !strings.HasSuffix(path, "//") {
		for i, route := range parent.routes {
			if route.handleSlash != ExactSlash && !route.pattern.catchAll && route.match(vm.method, vm.c) {
				return vm.found(i, parent, true, caps)
			}
		}
	}
	return false
}

// infixTsr returns the tsr match lookupByPath records when the infix captures of n from start are
// exhausted, after the suffix catch-alls of parent.
func (vm *pikeVM) infixTsr(n, parent *node, caps, start int) bool {
	path := vm.s
	if path[len(path)-1] == slashDelim {
		return false
	}
	_, child := n.getStaticEdge(slashDelim)
	if child == nil || !child.isLeaf() || child.key != "/" {
		return false
	}
	for j, route := range child.routes {
		if route.handleSlash == ExactSlash || !route.match(vm.method, vm.c) {
			continue
		}
		for _, w := range parent.wildcards {
			if !w.isLeaf() || w.regexp != nil && !vm.c.lookup.matchRegexp(w, path[start:]) {
				continue
			}
			for i, route := range w.routes {
				if route.match(vm.method, vm.c) {
					return vm.found(i, w, false, vm.capture(caps, start, len(path)))
				}
			}
		}
		return vm.found(j, child, true, vm.capture(caps, start, len(path)))
	}
	return false
}

// pathLookup returns the match of the path lookup from the hostname node n.
func (vm *pikeVM) pathLookup(n *node, caps int) bool {
	c := vm.c
	if c.lookup.pathFailed(n) {
		return false
	}
	i := slices.IndexFunc(vm.hits, func(h vmHit) bool { return h.from == n })
	if i < 0 {
		paramCnt, stackOffset := len(*c.params), len(*c.skipStack)
		idx, leaf, tsr := lookupByPath(n, vm.method, vm.path, c, vm.lazy, stackOffset)
		*c.skipStack = (*c.skipStack)[:stackOffset]
		if leaf == nil {
			*c.params = (*c.params)[:paramCnt]
			c.lookup.setPathFailed(n, vm.path)
			return false
		}
		lo := len(vm.hitParams)
		vm.hitParams = append(vm.hitParams, (*c.params)[paramCnt:]...)
		*c.params = (*c.params)[:paramCnt]
		vm.hits = append(vm.hits, vmHit{from: n, leaf: leaf, idx: idx, tsr: tsr, lo: lo, hi: len(vm.hitParams)})
		i = len(vm.hits) - 1
	}
	vm.bestHit = i
	h := &vm.hits[i]
	return vm.found(h.idx, h.leaf, h.tsr, caps)
}

// start adds a thread for a regexp capture of the node v from p. The capture is not empty, and the
// regexp is anchored at p.
func (vm *pikeVM) start(kind uint8, v int32, p, caps int) {
	nd := &vm.nodes[v]
	base := nd.infix
	if kind == vmSuffixRegexp {
		base = nd.suffix
	}
	seen, pos := vm.seen, vm.pos
	lo := len(vm.next.pcs)
	for _, pc := range nd.n.prog.First(regprog.Class(vm.s[p])) {
		if id := base + int32(pc); seen[id] != pos { // nolint:gosec // an instruction index
			seen[id] = pos
			vm.next.pcs = append(vm.next.pcs, pc)
		}
	}
	if len(vm.next.pcs) > lo {
		vm.next.threads = append(vm.next.threads, vmThread{kind: kind, node: v, lo: int32(lo), hi: int32(len(vm.next.pcs)), caps: caps, start: p}) // nolint:gosec
	}
}

// spawn adds a thread of a static, a param or an infix wildcard without regexp, unless its state was
// reached at this position.
func (vm *pikeVM) spawn(kind uint8, v int32, caps, start int) {
	id := vm.nodes[v].id + 1
	if vm.seen[id] == vm.pos {
		return
	}
	vm.seen[id] = vm.pos
	vm.next.threads = append(vm.next.threads, vmThread{kind: kind, node: v, caps: caps, start: start})
}

// push adds the regexp thread t to the next list, with the states pcs.
func (vm *pikeVM) push(t vmThread, pcs []uint32) {
	t.lo = int32(len(vm.next.pcs)) // nolint:gosec // a state index
	vm.next.pcs = append(vm.next.pcs, pcs...)
	t.hi = int32(len(vm.next.pcs)) // nolint:gosec // a state index
	vm.next.threads = append(vm.next.threads, t)
}

// pairs returns the start and end pairs of the capture block at off.
func (vm *pikeVM) pairs(off int) []int {
	n := vm.caps[off]
	return vm.caps[off+1 : off+1+2*n]
}

// capture returns a capture block with the captures of caps followed by [start, end).
func (vm *pikeVM) capture(caps, start, end int) int {
	off := len(vm.caps)
	vm.caps = append(vm.caps, vm.caps[caps]+1)
	vm.caps = append(vm.caps, vm.pairs(caps)...)
	vm.caps = append(vm.caps, start, end)
	return off
}

// empty returns caps followed by an empty capture when add is set.
func (vm *pikeVM) empty(caps int, add bool) int {
	if !add {
		return caps
	}
	return vm.capture(caps, 0, 0)
}

// found records the match of the route i of n, and reports that it cuts the threads after it.
func (vm *pikeVM) found(i int, n *node, tsr bool, caps int) bool {
	vm.bestIdx, vm.bestNode, vm.bestTsr = i, n, tsr
	vm.best = append(vm.best[:0], vm.pairs(caps)...)
	return true
}
