// Copyright 2026 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

// Package nfa simulates a compiled regular expression one position at a time, so a single pass
// over a string finds every prefix, or every suffix, that matches. The simulation runs in
// O(len(s) * len(prog)) time and never backtracks.
package nfa

import (
	"errors"
	"regexp"
	"regexp/syntax"
	"slices"
	"unicode/utf8"
)

// Regexp is a regular expression compiled for simulation, forward to find the prefixes of a
// string that match and backward to find the suffixes.
type Regexp struct {
	fwd, rev prog
}

// Compile compiles re for simulation. The expression of re must start with a ^ outside any
// repetition, as the expressions of route patterns do.
func Compile(re *regexp.Regexp) (*Regexp, error) {
	expr, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return nil, err
	}
	expr = expr.Simplify()
	var r Regexp
	if r.fwd, err = compile(expr); err != nil {
		return nil, err
	}
	if !r.fwd.anchored() {
		return nil, errors.New("expression not anchored at the start")
	}
	if r.rev, err = compile(reverse(expr)); err != nil {
		return nil, err
	}
	return &r, nil
}

// Len returns the number of instructions of the forward program.
func (r *Regexp) Len() int {
	return len(r.fwd.inst)
}

type prog struct {
	inst  []syntax.Inst
	start uint32
}

func compile(expr *syntax.Regexp) (prog, error) {
	p, err := syntax.Compile(expr)
	if err != nil {
		return prog{}, err
	}
	return prog{inst: p.Inst, start: uint32(p.Start)}, nil // nolint:gosec // an instruction index of p
}

// anchored reports whether p starts with a ^ that no loop returns to. Seen relies on it: a
// pass seeds its start state only once, so the seed never collides with a recorded state.
func (p *prog) anchored() bool {
	i := p.inst[p.start]
	if i.Op != syntax.InstEmptyWidth || syntax.EmptyOp(i.Arg) != syntax.EmptyBeginText { // nolint:gosec // Arg holds an EmptyOp
		return false
	}
	for _, i := range p.inst {
		if i.Out == p.start || ((i.Op == syntax.InstAlt || i.Op == syntax.InstAltMatch) && i.Arg == p.start) {
			return false
		}
	}
	return true
}

// reverse returns an expression matching the reversal of every string expr matches. It expects a
// simplified expression, without counted repetitions.
func reverse(expr *syntax.Regexp) *syntax.Regexp {
	r := *expr
	r.Sub = make([]*syntax.Regexp, len(expr.Sub))
	for i, sub := range expr.Sub {
		r.Sub[i] = reverse(sub)
	}
	switch expr.Op {
	case syntax.OpConcat:
		slices.Reverse(r.Sub)
	case syntax.OpLiteral:
		r.Rune = slices.Clone(expr.Rune)
		slices.Reverse(r.Rune)
	case syntax.OpBeginText:
		r.Op = syntax.OpEndText
	case syntax.OpEndText:
		r.Op = syntax.OpBeginText
	case syntax.OpBeginLine:
		r.Op = syntax.OpEndLine
	case syntax.OpEndLine:
		r.Op = syntax.OpBeginLine
	}
	return &r
}

// Scratch holds the thread sets of a simulation. It is reused across passes and grows to the
// largest program it runs.
type Scratch struct {
	cur, next, tmp sparseSet
	stack          []uint32
	// Steps counts the threads stepped since the scratch was created.
	Steps int
}

func (s *Scratch) grow(n int) {
	if len(s.cur.sparse) >= n {
		return
	}
	s.cur.grow(n)
	s.next.grow(n)
	s.tmp.grow(n)
	s.stack = make([]uint32, 0, n)
}

// Seen records the states reached at delimiter positions by the passes of one program over one
// string. A pass drops a state already seen, since every match reachable from it was already
// reported by an earlier pass.
type Seen struct {
	delims []int
	bits   []uint64
}

// Reset prepares s for a program of n instructions over a string whose delimiter positions, in
// increasing order, are delims.
func (s *Seen) Reset(n int, delims []int) {
	s.delims = delims
	words := (n*len(delims) + 63) / 64
	if cap(s.bits) < words {
		s.bits = make([]uint64, words)
		return
	}
	s.bits = s.bits[:words]
	clear(s.bits)
}

// Size returns the memory held by s in bytes.
func (s *Seen) Size() int {
	return cap(s.bits) * 8
}

func (s *Seen) testAndSet(pc uint32, ord int) bool {
	k := int(pc)*len(s.delims) + ord
	w, b := k/64, uint64(1)<<(k%64)
	if s.bits[w]&b != 0 {
		return true
	}
	s.bits[w] |= b
	return false
}

// Ends appends to dst, in increasing order, every position e such that s[start:e] matches r and
// e is either a position of delim after start or len(s). When seen is not nil, the states the
// pass reaches at a delimiter are recorded in seen, and the states recorded by earlier passes are
// dropped.
func (r *Regexp) Ends(sc *Scratch, s string, start int, delim byte, seen *Seen, dst []int) []int {
	p := &r.fwd
	sc.grow(len(p.inst))
	// ord is the index in seen.delims of the next delimiter the pass reaches.
	ord := 0
	if seen != nil {
		ord, _ = slices.BinarySearch(seen.delims, start+1)
	}

	cur, next := &sc.cur, &sc.next
	cur.clear()
	cur.add(p.start)
	prev := rune(-1)
	pos := start
	for {
		if pos > start && (pos == len(s) || s[pos] == delim) && p.accepts(sc, cur, syntax.EmptyOpContext(prev, -1)) {
			dst = append(dst, pos)
		}
		if pos == len(s) {
			return dst
		}

		c, w := utf8.DecodeRuneInString(s[pos:])
		p.step(sc, cur, next, prev, c)
		pos += w
		if seen != nil && pos < len(s) && s[pos] == delim {
			n := 0
			for _, pc := range next.dense {
				if !seen.testAndSet(pc, ord) {
					next.sparse[pc] = n
					next.dense[n] = pc
					n++
				}
			}
			next.dense = next.dense[:n]
			ord++
		}
		if len(next.dense) == 0 {
			return dst
		}
		cur, next = next, cur
		prev = c
	}
}

// Starts sets bit i of dst for every i in [0, len(s)) such that s[i:] matches r, where i is 0 or
// follows an ASCII byte. Other bits are unspecified. dst must hold len(s) bits.
func (r *Regexp) Starts(sc *Scratch, s string, dst []uint64) {
	p := &r.rev
	sc.grow(len(p.inst))
	cur, next := &sc.cur, &sc.next
	cur.clear()
	cur.add(p.start)
	prev := rune(-1)
	pos := len(s)
	for pos > 0 {
		c, w := utf8.DecodeLastRuneInString(s[:pos])
		p.step(sc, cur, next, prev, c)
		if len(next.dense) == 0 {
			return
		}
		cur, next = next, cur
		prev = c
		pos -= w
		if p.accepts(sc, cur, syntax.EmptyOpContext(prev, -1)) {
			dst[pos/64] |= 1 << (pos % 64)
		}
	}
}

// step advances every thread of cur over r into next.
func (p *prog) step(sc *Scratch, cur, next *sparseSet, prev, r rune) {
	sc.tmp.clear()
	flag := syntax.EmptyOpContext(prev, r)
	for _, pc := range cur.dense {
		p.closure(sc, pc, flag)
	}
	next.clear()
	for _, pc := range sc.tmp.dense {
		i := &p.inst[pc]
		switch i.Op {
		case syntax.InstRune, syntax.InstRune1, syntax.InstRuneAny, syntax.InstRuneAnyNotNL:
			sc.Steps++
			if i.MatchRune(r) && !next.has(i.Out) {
				next.add(i.Out)
			}
		}
	}
}

// accepts reports whether a thread of cur reaches a match under flag.
func (p *prog) accepts(sc *Scratch, cur *sparseSet, flag syntax.EmptyOp) bool {
	sc.tmp.clear()
	matched := false
	for _, pc := range cur.dense {
		if p.closure(sc, pc, flag) {
			matched = true
		}
	}
	return matched
}

// closure adds to sc.tmp the instructions reachable from pc without consuming a rune under flag,
// and reports whether a match is among them.
func (p *prog) closure(sc *Scratch, pc uint32, flag syntax.EmptyOp) bool {
	matched := false
	sc.stack = append(sc.stack[:0], pc)
	for len(sc.stack) > 0 {
		pc := sc.stack[len(sc.stack)-1]
		sc.stack = sc.stack[:len(sc.stack)-1]
		if sc.tmp.has(pc) {
			continue
		}
		sc.tmp.add(pc)
		i := &p.inst[pc]
		switch i.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			sc.stack = append(sc.stack, i.Arg, i.Out)
		case syntax.InstNop, syntax.InstCapture:
			sc.stack = append(sc.stack, i.Out)
		case syntax.InstEmptyWidth:
			if syntax.EmptyOp(i.Arg)&^flag == 0 { // nolint:gosec // Arg holds an EmptyOp
				sc.stack = append(sc.stack, i.Out)
			}
		case syntax.InstMatch:
			matched = true
		}
	}
	return matched
}

// sparseSet is a set of instruction indexes that clears in constant time.
type sparseSet struct {
	dense  []uint32
	sparse []int
}

func (s *sparseSet) grow(n int) {
	s.dense = make([]uint32, 0, n)
	s.sparse = make([]int, n)
}

func (s *sparseSet) has(pc uint32) bool {
	i := s.sparse[pc]
	return i < len(s.dense) && s.dense[i] == pc
}

func (s *sparseSet) add(pc uint32) {
	s.sparse[pc] = len(s.dense)
	s.dense = append(s.dense, pc)
}

func (s *sparseSet) clear() {
	s.dense = s.dense[:0]
}
