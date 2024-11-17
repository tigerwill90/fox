package fox

import (
	"net/http"
	"sync"
)

const defaultModifiedCache = 8192

type Txn struct {
	snap *Tree
	main *Tree
	once sync.Once
}

func (txn *Txn) Handle(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	return txn.snap.Handle(method, pattern, handler, opts...)
}

func (txn *Txn) Update(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	return txn.snap.Handle(method, pattern, handler, opts...)
}

func (txn *Txn) Delete(method, pattern string) error {
	return txn.snap.Delete(method, pattern)
}

func (txn *Txn) Has(method, pattern string) bool {
	return txn.Route(method, pattern) != nil
}

func (txn *Txn) Route(method, pattern string) *Route {
	return txn.snap.Route(method, pattern)
}

func (txn *Txn) Reverse(method, host, path string) (route *Route, tsr bool) {
	return txn.snap.Reverse(method, host, path)
}

func (txn *Txn) Lookup(w ResponseWriter, r *http.Request) (route *Route, cc ContextCloser, tsr bool) {
	return txn.snap.Lookup(w, r)
}

func (txn *Txn) Iter() Iter {
	return Iter{t: txn.snap}
}

func (txn *Txn) Commit() {
	txn.once.Do(func() {
		// reset the writable tree cache to avoid leaking future writes after commit.
		txn.snap.writable = nil
		txn.main.maxParams.Store(txn.snap.maxParams.Load())
		txn.main.maxDepth.Store(txn.snap.maxDepth.Load())
		txn.main.nodes.Store(txn.snap.nodes.Load())
		txn.main.Unlock()
	})
}

func (txn *Txn) Rollback() {
	txn.once.Do(func() {
		txn.main.Unlock()
	})
}
