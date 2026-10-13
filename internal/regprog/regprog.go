// Copyright 2026 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

// Package regprog compiles a regular expression into a program simulated one position at a time,
// so a single pass over a string finds every prefix that matches. The simulation never backtracks
// and a step costs O(len(prog)), as the Pike VM of the regexp package.
package regprog

import (
	"regexp"
	"regexp/syntax"
	"unicode/utf8"
)

// Classes of the bytes around a position, which decide the empty-width assertions there. A byte
// that is not ASCII has the class of any rune it is part of.
const (
	newline = iota
	word
	other
)

var classes = func() (c [256]uint8) {
	for i := range c {
		switch r := rune(i); {
		case r == '\n':
			c[i] = newline
		case syntax.IsWordChar(r):
			c[i] = word
		default:
			c[i] = other
		}
	}
	return c
}()

// Class returns the class of the byte b.
func Class(b byte) uint8 {
	return classes[b]
}

// classRune returns a rune of the class c.
func classRune(c int) rune {
	return [...]rune{'\n', 'a', ' '}[c]
}

// maxClosure is the number of instructions above which the closure of a rune instruction is followed
// at each step, as the regexp package does, instead of being listed at compile time. A listed closure
// then costs at most maxClosure entries, and the lists of a program hold O(len(prog)) entries.
const maxClosure = 16

// Expand marks an entry of a Follow that is an empty instruction: the entries of that instruction
// follow in turn.
const Expand = 1 << 31

// Regexp is a regular expression compiled for simulation. The empty transitions are followed at
// compile time, for every context of the empty-width assertions, unless a closure is too large.
type Regexp struct {
	inst []syntax.Inst
	// ascii holds, for each rune instruction, the set of the ASCII runes it matches.
	ascii Sets
	// first[n] holds the rune instructions of the start of the string, before a byte of class n.
	first [3][]uint32
	// follow[3*p+n] holds the instructions that follow each instruction between bytes of classes p
	// and n.
	follow [9]*Follow
	// last[p] reports, for each rune instruction, whether a match follows it at the end of the
	// string, after a byte of class p.
	last [3][]bool
	// expands reports whether a closure is too large to be listed.
	expands bool
}

// Follow holds the instructions that follow each instruction without consuming a rune. A rune
// instruction lists the rune instructions of its closure, or when the closure has more than
// maxClosure instructions, the instruction it leads to. Then an empty instruction lists the
// instructions it leads to, and the entries marked with Expand, followed in turn, reach the whole
// closure.
type Follow struct {
	off []uint32
	pcs []uint32
}

// Of returns the entries of the instruction pc.
func (f *Follow) Of(pc uint32) []uint32 {
	return f.pcs[f.off[pc]:f.off[pc+1]]
}

// Sets holds, for each rune instruction, the set of the ASCII bytes it matches.
type Sets [][2]uint64

// Has reports whether the rune instruction pc matches the ASCII byte b.
func (s Sets) Has(pc uint32, b byte) bool {
	return s[pc][b>>6&1]&(1<<(b&63)) != 0
}

// Compile compiles re for simulation. A pass agrees with MatchString only when re is anchored at
// both ends, as the expressions of route patterns are.
func Compile(re *regexp.Regexp) (*Regexp, error) {
	expr, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return nil, err
	}
	p, err := syntax.Compile(expr.Simplify())
	if err != nil {
		return nil, err
	}
	r := &Regexp{inst: p.Inst, ascii: make(Sets, len(p.Inst))}
	b := builder{inst: p.Inst, mark: make([]uint32, len(p.Inst))}
	var used syntax.EmptyOp
	for i := range p.Inst {
		switch in := &p.Inst[i]; {
		case in.Op == syntax.InstEmptyWidth:
			used |= syntax.EmptyOp(in.Arg) // nolint:gosec // Arg holds an EmptyOp
		case isRune(in):
			for c := range rune(utf8.RuneSelf) {
				if in.MatchRune(c) {
					r.ascii[i][c/64] |= 1 << (c % 64)
				}
			}
		}
	}

	// Contexts that set the same assertions of the program share their closures.
	firsts := map[syntax.EmptyOp][]uint32{}
	lasts := map[syntax.EmptyOp][]bool{}
	for c := range 3 {
		flag := syntax.EmptyOpContext(-1, classRune(c)) & used
		if _, ok := firsts[flag]; !ok {
			firsts[flag], _ = b.closure(nil, uint32(p.Start), flag, len(p.Inst)) // nolint:gosec // an instruction index
		}
		r.first[c] = firsts[flag]

		flag = syntax.EmptyOpContext(classRune(c), -1) & used
		if lasts[flag] == nil {
			lasts[flag] = b.last(flag)
		}
		r.last[c] = lasts[flag]
	}
	var flags [9]syntax.EmptyOp
	follows := map[syntax.EmptyOp]*Follow{}
	for i := range flags {
		flags[i] = syntax.EmptyOpContext(classRune(i/3), classRune(i%3)) & used
		if follows[flags[i]] == nil {
			var expands bool
			follows[flags[i]], expands = b.follow(flags[i], false)
			r.expands = r.expands || expands
		}
	}
	if r.expands {
		// The empty instructions need their entries in every context.
		for flag := range follows {
			follows[flag], _ = b.follow(flag, true)
		}
	}
	for i, flag := range flags {
		r.follow[i] = follows[flag]
	}
	return r, nil
}

// Len returns the number of instructions of the program of r.
func (r *Regexp) Len() int {
	return len(r.inst)
}

// Match reports whether the rune instruction pc matches c.
func (r *Regexp) Match(pc uint32, c rune) bool {
	if c < utf8.RuneSelf {
		return r.ascii.Has(pc, byte(c)) // nolint:gosec // an ASCII rune
	}
	return r.inst[pc].MatchRune(c)
}

// ASCII returns the sets of the ASCII bytes the rune instructions match.
func (r *Regexp) ASCII() Sets {
	return r.ascii
}

// First returns the rune instructions of the start of the string, before a byte of class next.
func (r *Regexp) First(next uint8) []uint32 {
	return r.first[next]
}

// Follow returns the instructions that follow each instruction between bytes of classes prev and
// next.
func (r *Regexp) Follow(prev, next uint8) *Follow {
	return r.follow[3*prev+next]
}

// Expands reports whether some entries of the closures of r are marked with Expand.
func (r *Regexp) Expands() bool {
	return r.expands
}

// Accepts reports whether a match follows one of the rune instructions pcs at the end of the
// string, after a byte of class prev.
func (r *Regexp) Accepts(prev uint8, pcs []uint32) bool {
	last := r.last[prev]
	for _, pc := range pcs {
		if last[pc] {
			return true
		}
	}
	return false
}

// Next appends to dst the rune instructions that follow the rune instructions pcs between bytes of
// classes prev and next, except those marked in seen with stamp. It marks the instructions it
// reaches, so each one is reached once at a position, and uses stack as scratch.
func (r *Regexp) Next(dst, pcs []uint32, prev, next uint8, seen []uint64, stamp uint64, stack *[]uint32) []uint32 {
	f := r.follow[3*prev+next]
	if !r.expands {
		for _, pc := range pcs {
			for _, q := range f.Of(pc) {
				if seen[q] != stamp {
					seen[q] = stamp
					dst = append(dst, q)
				}
			}
		}
		return dst
	}
	st := (*stack)[:0]
	for _, pc := range pcs {
		for _, q := range f.Of(pc) {
			if seen[q&^Expand] != stamp {
				seen[q&^Expand] = stamp
				if q < Expand {
					dst = append(dst, q)
				} else {
					st = append(st, q&^Expand)
				}
			}
		}
	}
	for len(st) > 0 {
		pc := st[len(st)-1]
		st = st[:len(st)-1]
		for _, q := range f.Of(pc) {
			if seen[q&^Expand] != stamp {
				seen[q&^Expand] = stamp
				if q < Expand {
					dst = append(dst, q)
				} else {
					st = append(st, q&^Expand)
				}
			}
		}
	}
	*stack = st
	return dst
}

func isRune(in *syntax.Inst) bool {
	switch in.Op {
	case syntax.InstRune, syntax.InstRune1, syntax.InstRuneAny, syntax.InstRuneAnyNotNL:
		return true
	}
	return false
}

// empty calls fn for each instruction reached from in by an empty transition when the assertions of
// flag hold, Out last.
func empty(in *syntax.Inst, flag syntax.EmptyOp, fn func(uint32)) {
	switch in.Op {
	case syntax.InstAlt, syntax.InstAltMatch:
		fn(in.Arg)
		fn(in.Out)
	case syntax.InstNop, syntax.InstCapture:
		fn(in.Out)
	case syntax.InstEmptyWidth:
		if syntax.EmptyOp(in.Arg)&^flag == 0 { // nolint:gosec // Arg holds an EmptyOp
			fn(in.Out)
		}
	}
}

// builder follows the empty transitions of a program.
type builder struct {
	inst  []syntax.Inst
	mark  []uint32
	stack []uint32
	gen   uint32
}

// closure appends to dst the rune instructions reached from pc without consuming a rune when the
// assertions of flag hold, in the order of the regexp package: Out before Arg. It reports false
// when it visits more than limit instructions.
func (b *builder) closure(dst []uint32, pc uint32, flag syntax.EmptyOp, limit int) ([]uint32, bool) {
	b.gen++
	visited := 0
	b.stack = append(b.stack[:0], pc)
	for len(b.stack) > 0 {
		pc := b.stack[len(b.stack)-1]
		b.stack = b.stack[:len(b.stack)-1]
		if b.mark[pc] == b.gen {
			continue
		}
		b.mark[pc] = b.gen
		if visited++; visited > limit {
			return dst, false
		}
		if in := &b.inst[pc]; isRune(in) {
			dst = append(dst, pc)
		} else {
			empty(in, flag, func(pc uint32) { b.stack = append(b.stack, pc) })
		}
	}
	return dst, true
}

// follow returns the instructions that follow each instruction when the assertions of flag hold,
// with the entries of the empty instructions when expand is set. It reports whether a closure
// visits more than maxClosure instructions.
func (b *builder) follow(flag syntax.EmptyOp, expand bool) (*Follow, bool) {
	f := &Follow{off: make([]uint32, len(b.inst)+1)}
	expands := false
	for pc := range b.inst {
		switch in := &b.inst[pc]; {
		case isRune(in):
			n := len(f.pcs)
			var ok bool
			if f.pcs, ok = b.closure(f.pcs, in.Out, flag, maxClosure); !ok {
				f.pcs = b.entry(f.pcs[:n], in.Out)
				expands = true
			}
		case expand:
			empty(in, flag, func(pc uint32) { f.pcs = b.entry(f.pcs, pc) })
		}
		f.off[pc+1] = uint32(len(f.pcs)) // nolint:gosec // a closure size
	}
	return f, expands
}

// entry appends the entry of the instruction pc to dst.
func (b *builder) entry(dst []uint32, pc uint32) []uint32 {
	switch in := &b.inst[pc]; {
	case isRune(in):
		return append(dst, pc)
	case in.Op == syntax.InstMatch || in.Op == syntax.InstFail:
		return dst
	}
	return append(dst, pc|Expand)
}

// last reports, for each rune instruction, whether a match follows it when the assertions of flag
// hold. A backward search from the matches visits each instruction once.
func (b *builder) last(flag syntax.EmptyOp) []bool {
	// preds[off[pc]:off[pc+1]] lists the instructions with an empty transition to pc.
	off := make([]uint32, len(b.inst)+2)
	for i := range b.inst {
		empty(&b.inst[i], flag, func(pc uint32) { off[pc+2]++ })
	}
	for i := 2; i < len(off); i++ {
		off[i] += off[i-1]
	}
	preds := make([]uint32, off[len(off)-1])
	for i := range b.inst {
		empty(&b.inst[i], flag, func(pc uint32) {
			preds[off[pc+1]] = uint32(i) // nolint:gosec // an instruction index
			off[pc+1]++
		})
	}

	reach := make([]bool, len(b.inst))
	b.stack = b.stack[:0]
	for i := range b.inst {
		if b.inst[i].Op == syntax.InstMatch {
			reach[i] = true
			b.stack = append(b.stack, uint32(i)) // nolint:gosec // an instruction index
		}
	}
	for len(b.stack) > 0 {
		pc := b.stack[len(b.stack)-1]
		b.stack = b.stack[:len(b.stack)-1]
		for _, q := range preds[off[pc]:off[pc+1]] {
			if !reach[q] {
				reach[q] = true
				b.stack = append(b.stack, q)
			}
		}
	}

	last := make([]bool, len(b.inst))
	for i := range b.inst {
		last[i] = isRune(&b.inst[i]) && reach[b.inst[i].Out]
	}
	return last
}

// Pass lists the ends of the prefixes of s[start:] that match a program: the positions e of delim
// after start such that s[start:e] matches. Each call to Next resumes where the previous one stopped,
// so listing every end costs a single pass, and the memory of a pass depends on the program only.
type Pass struct {
	r                  *Regexp
	s                  string
	cur, next, matched []uint32
	stack              []uint32
	seen               []uint64
	stamp              uint64
	start, pos         int
	// Steps counts the rune instructions stepped.
	Steps    int
	delim    byte
	accepted bool
}

// Reset starts a pass of r over s from start.
func (p *Pass) Reset(r *Regexp, s string, start int, delim byte) {
	p.r, p.s, p.start, p.pos, p.delim, p.accepted = r, s, start, start, delim, false
	if cap(p.seen) < len(r.inst) {
		p.seen = make([]uint64, len(r.inst))
	}
	p.seen = p.seen[:len(r.inst)]
	p.cur = p.cur[:0]
	if start < len(s) {
		p.cur = append(p.cur, r.First(Class(s[start]))...)
	}
}

// Started reports whether p is a pass of r over s from start.
func (p *Pass) Started(r *Regexp, s string, start int) bool {
	return p.r == r && p.s == s && p.start == start
}

// Next returns the first end at or after from, or -1 if there is none. When from is before the end
// returned last, the pass starts over.
func (p *Pass) Next(from int) int {
	if from < p.pos {
		p.Reset(p.r, p.s, p.start, p.delim)
	}
	if p.accepted && p.pos >= from {
		return p.pos
	}
	r, s := p.r, p.s
	for len(p.cur) > 0 && p.pos < len(s) {
		p.Steps += len(p.cur)
		matched := p.matched[:0]
		if b := s[p.pos]; b < utf8.RuneSelf {
			for _, pc := range p.cur {
				if r.ascii.Has(pc, b) {
					matched = append(matched, pc)
				}
			}
			p.pos++
		} else {
			c, w := utf8.DecodeRuneInString(s[p.pos:])
			for _, pc := range p.cur {
				if r.inst[pc].MatchRune(c) {
					matched = append(matched, pc)
				}
			}
			p.pos += w
		}
		p.matched = matched
		prev := Class(s[p.pos-1])
		p.accepted = p.pos < len(s) && s[p.pos] == p.delim && r.Accepts(prev, matched)
		next := p.next[:0]
		if p.pos < len(s) {
			p.stamp++
			next = r.Next(next, matched, prev, Class(s[p.pos]), p.seen, p.stamp, &p.stack)
		}
		p.cur, p.next = next, p.cur
		if p.accepted && p.pos >= from {
			return p.pos
		}
	}
	return -1
}
