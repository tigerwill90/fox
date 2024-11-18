package fox

import (
	"net/http"
	"sync"
)

const defaultModifiedCache = 4096

// Txn is a read-write transaction for managing routes in a [Router]. It's safe to run multiple transaction
// concurrently and while the router is serving request, however Txn itself is not tread-safe.
// Each Txn must be finalized with [Txn.Commit] or [Txn.Abort]. For non-transactional batch updates, see
// [Router.BatchWriter] or [Router.Batch].
type Txn struct {
	snap *Tree
	main *Tree
	once sync.Once
}

// Handle registers a new handler for the given method and route pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrRouteConflict]: If the route conflicts with another.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to add a new handler while the router is serving requests. However, this function is NOT
// thread-safe and should be run serially, along with all other [Txn] APIs that perform write operations.
// To override an existing route, use [Txn.Update].
func (txn *Txn) Handle(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	return txn.snap.Handle(method, pattern, handler, opts...)
}

// Update override an existing handler for the given method and route pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to update a handler while the router is serving requests. However, this function is NOT thread-safe
// and should be run serially, along with all other [Txn] APIs that perform write operations. To add a new handler,
// use [Txn.Handle] method.
func (txn *Txn) Update(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	return txn.snap.Update(method, pattern, handler, opts...)
}

// Delete deletes an existing handler for the given method and router pattern. If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to delete a handler while the tree is in use for serving requests. However, this function is NOT
// thread-safe and should be run serially, along with all other [Txn] APIs that perform write operations.
func (txn *Txn) Delete(method, pattern string) error {
	return txn.snap.Delete(method, pattern)
}

// Truncate delete all registered route for the provided methods. If no method are provided, Truncate deletes all routes.
// It's safe to truncate routes while the router is serving requests. However, this function is NOT thread-safe and
// should be run serially, along with all other [Txn] APIs that perform write operations. To delete a single route,
// use [Txn.Delete].
func (txn *Txn) Truncate(methods ...string) {
	txn.snap.truncate(methods)
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

// Iter returns a collection of iterators for traversing the routing tree.
// This function is safe for concurrent use by multiple goroutine and while mutation on [Tree] are ongoing.
// This API is EXPERIMENTAL and may change in future releases.
func (txn *Txn) Iter() Iter {
	return Iter{t: txn.snap}
}

// Commit finalize this transaction. This is a noop for transactions already aborted or commited.
func (txn *Txn) Commit() {
	txn.once.Do(func() {
		txn.main.maxParams.Store(txn.snap.maxParams.Load())
		txn.main.maxDepth.Store(txn.snap.maxDepth.Load())
		txn.main.root.Store(txn.snap.root.Load())
		txn.snap = nil
		txn.main.race.Store(0)
		txn.main.fox.mu.Unlock()
	})
}

// Abort cancel this transaction. This is a noop for transaction already aborted or commited.
func (txn *Txn) Abort() {
	txn.once.Do(func() {
		txn.snap = nil
		txn.main.race.Store(0)
		txn.main.fox.mu.Unlock()
	})
}
