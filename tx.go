package fox

import (
	"sync/atomic"
)

type Tx struct {
	r   *LockedRouter
	tmp *LockedRouter
}

// NewTransaction creates a new transaction. A transaction allow to perform
// multiple mutation while keeping a consistent view of the routing tree. Unlike
// BatchWriter, transaction are only applied on Commit.
//
// It's safe to run multiple transaction concurrently. However, a transaction itself
// is not thread safe and all Tx APIs should be run serially.
//
// Discard must always be call at the end of the transaction. Internally, Commit API
// runs Discard but running it twice is perfectly OK.
//
// The Tx API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) NewTransaction(reset bool) *Tx {
	r := fox.LockRouter()
	tmp := New().LockRouter()
	if reset {
		return &Tx{
			r:   r,
			tmp: tmp,
		}
	}

	it := r.NewIterator()
	for it.Rewind(); it.Valid(); it.Next() {
		if err := tmp.Handler(it.Method(), it.Path(), it.Handler()); err != nil {
			// Safeguard against regression on Iterator (this should never happen).
			panic("internal error: unexpected error while cloning router")
		}
	}

	return &Tx{
		r:   r,
		tmp: tmp,
	}
}

func (tx *Tx) Handler(method, path string, handler Handler) error {
	return tx.tmp.Handler(method, path, handler)
}

func (tx *Tx) Update(method, path string, handler Handler) error {
	return tx.tmp.Update(method, path, handler)
}

func (tx *Tx) Remove(method, path string) error {
	return tx.tmp.Remove(method, path)
}

func (tx *Tx) Lookup(method, path string, lazy bool, fn func(handler Handler, params Params, tsr bool)) {
	tx.tmp.Lookup(method, path, lazy, fn)
}

func (tx *Tx) Match(method, path string) bool {
	return tx.tmp.Match(method, path)
}

func (tx *Tx) NewIterator() *Iterator {
	return tx.tmp.NewIterator()
}

func (tx *Tx) Commit() {
	tx.r.assertLock()
	nds := tx.tmp.r.trees.Load()
	max := atomic.LoadUint32(&tx.tmp.r.maxParams)
	atomic.StoreUint32(&tx.r.r.maxParams, max)
	tx.r.r.trees.Store(nds)
	tx.Discard()
}

// Discard the transaction. This function must always be call at
// the end of a transaction. Calling this function on a discarded
// transaction is a no-op.
func (tx *Tx) Discard() {
	tx.tmp.Release()
	tx.r.Release()
}
