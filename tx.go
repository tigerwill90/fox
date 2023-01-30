package fox

import (
	"sync/atomic"
)

type Txn struct {
	r   *LockedRouter
	tmp *Router
}

// NewTransaction creates a new transaction. A transaction allow to perform
// multiple mutation while keeping a consistent view of the routing tree. Unlike
// BatchWriter, transaction are only applied on Commit.
//
// It's safe to run multiple transaction concurrently. However, a transaction itself
// is not thread safe and all Txn APIs should be run serially.
//
// Discard must always be call at the end of the transaction. Internally, Commit API
// runs Discard but running it twice is perfectly OK.
//
// The Txn API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) NewTransaction(reset bool) *Txn {
	r := fox.LockRouter()
	var ptr atomic.Pointer[[]*node]
	initTree(&ptr)
	tmp := &Router{
		trees: &ptr,
	}
	if reset {
		return &Txn{
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

	return &Txn{
		r:   r,
		tmp: tmp,
	}
}

func (txn *Txn) Handler(method, path string, handler Handler) error {
	return txn.tmp.Handler(method, path, handler)
}

func (txn *Txn) Update(method, path string, handler Handler) error {
	return txn.tmp.Update(method, path, handler)
}

func (txn *Txn) Remove(method, path string) error {
	return txn.tmp.Remove(method, path)
}

func (txn *Txn) Lookup(method, path string, lazy bool, fn func(handler Handler, params Params, tsr bool)) {
	txn.tmp.Lookup(method, path, lazy, fn)
}

func (txn *Txn) Match(method, path string) bool {
	return txn.tmp.Match(method, path)
}

func (txn *Txn) NewIterator() *Iterator {
	return txn.tmp.NewIterator()
}

func (txn *Txn) Commit() {
	txn.r.assertLock()
	nds := txn.tmp.trees.Load()
	max := atomic.LoadUint32(&txn.tmp.maxParams)
	atomic.StoreUint32(&txn.r.r.maxParams, max)
	txn.r.r.trees.Store(nds)
	txn.Discard()
}

// Discard the transaction. This function must always be call at
// the end of a transaction. Calling this function on a discarded
// transaction is a no-op.
func (txn *Txn) Discard() {
	txn.r.Release()
}
