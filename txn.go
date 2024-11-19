package fox

import (
	"fmt"
	"net/http"
)

type Txn struct {
	fox     *Router
	rootTxn *txn
	write   bool
}

// Handle registers a new handler for the given method and route pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrRouteConflict]: If the route conflicts with another.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
// To override an existing route, use [Txn.Update].
func (txn *Txn) Handle(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if !txn.write {
		return nil, ErrReadOnlyTxn
	}

	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if matched := regEnLetter.MatchString(method); !matched {
		return nil, fmt.Errorf("%w: missing or invalid http method", ErrInvalidRoute)
	}

	rte, n, err := txn.fox.newRoute(pattern, handler, opts...)
	if err != nil {
		return nil, err
	}

	if err = txn.rootTxn.insert(method, rte, n); err != nil {
		return nil, err
	}
	return rte, nil
}

// Update override an existing handler for the given method and route pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
// To add a new handler, use [Txn.Handle].
func (txn *Txn) Update(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if !txn.write {
		return nil, ErrReadOnlyTxn
	}

	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if method == "" {
		return nil, fmt.Errorf("%w: missing http method", ErrInvalidRoute)
	}

	rte, _, err := txn.fox.newRoute(pattern, handler, opts...)
	if err != nil {
		return nil, err
	}

	if err = txn.rootTxn.update(method, rte); err != nil {
		return nil, err
	}

	return rte, nil
}

// Delete deletes an existing handler for the given method and router pattern. If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
func (txn *Txn) Delete(method, pattern string) error {
	if !txn.write {
		return ErrReadOnlyTxn
	}

	if method == "" {
		return fmt.Errorf("%w: missing http method", ErrInvalidRoute)
	}

	_, _, err := parseRoute(pattern)
	if err != nil {
		return err
	}

	if !txn.rootTxn.remove(method, pattern) {
		return fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, pattern)
	}

	return nil
}

func (txn *Txn) Truncate(methods ...string) error {
	if !txn.write {
		return ErrReadOnlyTxn
	}
	txn.rootTxn.truncate(methods)
	return nil
}

// Has allows to check if the given method and route pattern exactly match a registered route. This function is NOT
// thread-safe and should be run serially, along with all other [Txn] APIs. See also [Txn.Route] as an alternative.
func (txn *Txn) Has(method, pattern string) bool {
	return txn.Route(method, pattern) != nil
}

// Route performs a lookup for a registered route matching the given method and route pattern. It returns the [Route] if a
// match is found or nil otherwise. This function is NOT thread-safe and should be run serially, along with all
// other [Txn] APIs. See also [Tree.Has] as an alternative.
func (txn *Txn) Route(method, pattern string) *Route {
	tree := txn.rootTxn.tree
	c := tree.ctx.Get().(*cTx)
	c.resetNil()

	host, path := SplitHostPath(pattern)
	n, tsr := txn.rootTxn.root.lookup(tree, method, host, path, c, true)
	tree.ctx.Put(c)
	if n != nil && !tsr && n.route.pattern == pattern {
		return n.route
	}
	return nil
}

// Reverse perform a reverse lookup for the given method, host and path and return the matching registered [Route]
// (if any) along with a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). This function is NOT thread-safe and should be run serially, along with all
// other [Txn] APIs. See also [Txn.Lookup] as an alternative.
func (txn *Txn) Reverse(method, host, path string) (route *Route, tsr bool) {
	tree := txn.rootTxn.tree
	c := tree.ctx.Get().(*cTx)
	c.resetNil()
	n, tsr := txn.rootTxn.root.lookup(tree, method, host, path, c, true)
	tree.ctx.Put(c)
	if n != nil {
		return n.route, tsr
	}
	return nil, false
}

// Lookup performs a manual route lookup for a given [http.Request], returning the matched [Route] along with a
// [ContextCloser], and a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). If there is a direct match or a tsr is possible, Lookup always return a
// [Route] and a [ContextCloser]. The [ContextCloser] should always be closed if non-nil. This function is NOT
// thread-safe and should be run serially, along with all other [Txn] APIs. See also [Txn.Reverse] as an alternative.
func (txn *Txn) Lookup(w ResponseWriter, r *http.Request) (route *Route, cc ContextCloser, tsr bool) {
	tree := txn.rootTxn.tree
	c := tree.ctx.Get().(*cTx)
	c.resetWithWriter(w, r)

	path := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		path = r.URL.RawPath
	}

	n, tsr := txn.rootTxn.root.lookup(tree, r.Method, r.Host, path, c, false)
	if n != nil {
		c.route = n.route
		c.tsr = tsr
		return n.route, c, tsr
	}
	tree.ctx.Put(c)
	return nil, nil, tsr
}

// Iter returns a collection of range iterators for traversing registered routes. When called on a write transaction,
// Iter creates a point-in-time snapshot of the transaction state. Therefore, writing on the current transaction while
// iterating is allowed, but the mutation will not be observed in the result returned by iterators collection.
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
func (txn *Txn) Iter() Iter {
	rt := txn.rootTxn.root
	if txn.write {
		rt = txn.rootTxn.snapshot()
	}

	return Iter{
		tree:     txn.rootTxn.tree,
		root:     rt,
		maxDepth: txn.rootTxn.maxDepth,
	}
}

// Commit finalize the transaction. This is a noop for read transactions, already aborted or
// committed transactions. This function is NOT thread-safe and should be run serially,
// along with all other [Txn] APIs.
func (txn *Txn) Commit() {
	// Noop for a read transaction
	if !txn.write {
		return
	}

	// Check if already aborted or committed
	if txn.rootTxn == nil {
		return
	}

	newRoot := txn.rootTxn.commit()
	txn.fox.tree.Store(newRoot)

	// Clear the txn
	txn.rootTxn = nil
	txn.fox.mu.Unlock()
}

// Abort cancel the transaction. This is a noop for read transactions, already aborted or
// committed transactions. This function is NOT thread-safe and should be run serially,
// along with all other [Txn] APIs.
func (txn *Txn) Abort() {
	// Noop for a read transaction
	if !txn.write {
		return
	}

	// Check if already aborted or committed
	if txn.rootTxn == nil {
		return
	}

	// Clear the txn
	txn.rootTxn = nil
	txn.fox.mu.Unlock()
}