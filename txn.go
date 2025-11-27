package fox

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// Txn is a read or write transaction on the routing tree.
type Txn struct {
	fox     *Router
	rootTxn *tXn
	write   bool
}

// Handle registers a new route for the given method, pattern and matchers. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
// To override an existing handler, use [Txn.Update].
func (txn *Txn) Handle(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	if !txn.write {
		return nil, ErrReadOnlyTxn
	}

	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if !validMethod(method) {
		return nil, fmt.Errorf("%w: invalid method", ErrInvalidRoute)
	}

	rte, err := txn.fox.NewRoute(pattern, handler, opts...)
	if err != nil {
		return nil, err
	}

	if err = txn.rootTxn.insert(method, rte, modeInsert); err != nil {
		return nil, err
	}
	return rte, nil
}

// HandleRoute registers a new [Route] for the given method. If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrInvalidRoute]: If the provided method is invalid or the route is missing.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
// To override an existing route, use [Txn.UpdateRoute].
func (txn *Txn) HandleRoute(method string, route *Route) error {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	if !txn.write {
		return ErrReadOnlyTxn
	}

	if route == nil {
		return fmt.Errorf("%w: nil route", ErrInvalidRoute)
	}
	if !validMethod(method) {
		return fmt.Errorf("%w: invalid method", ErrInvalidRoute)
	}

	return txn.rootTxn.insert(method, route, modeInsert)
}

// Update override an existing route for the given method, pattern and matchers. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// Route-specific option and middleware must be reapplied when updating a route. if not, any middleware and option will
// be removed (or reset to their default value), and the route will fall back to using global configuration (if any).
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
// To add a new handler, use [Txn.Handle].
func (txn *Txn) Update(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	if !txn.write {
		return nil, ErrReadOnlyTxn
	}

	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}
	if !validMethod(method) {
		return nil, fmt.Errorf("%w: invalid method", ErrInvalidRoute)
	}

	rte, err := txn.fox.NewRoute(pattern, handler, opts...)
	if err != nil {
		return nil, err
	}

	if err = txn.rootTxn.insert(method, rte, modeUpdate); err != nil {
		return nil, err
	}

	return rte, nil
}

// UpdateRoute override an existing [Route] for the given method and new [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method is invalid or the route is missing.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
// To add a new route, use [Txn.HandleRoute].
func (txn *Txn) UpdateRoute(method string, route *Route) error {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	if !txn.write {
		return ErrReadOnlyTxn
	}

	if route == nil {
		return fmt.Errorf("%w: nil route", ErrInvalidRoute)
	}
	if !validMethod(method) {
		return fmt.Errorf("%w: invalid method", ErrInvalidRoute)
	}

	return txn.rootTxn.insert(method, route, modeUpdate)
}

// Delete deletes an existing route for the given method, pattern and matchers. On success, it returns the deleted [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
func (txn *Txn) Delete(method, pattern string, opts ...MatcherOption) (*Route, error) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	if !txn.write {
		return nil, ErrReadOnlyTxn
	}

	if !validMethod(method) {
		return nil, fmt.Errorf("%w: invalid method", ErrInvalidRoute)
	}

	tokens, _, endHost, err := txn.fox.parseRoute(pattern)
	if err != nil {
		return nil, err
	}

	rte := &Route{
		pattern:   pattern,
		hostSplit: endHost,
		tokens:    tokens,
	}

	for _, opt := range opts {
		if err = opt.applyMatcher(sealedOption{route: rte}); err != nil {
			return nil, err
		}
	}

	if len(rte.matchers) > txn.fox.maxMatchers {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrTooManyMatchers)
	}

	route, deleted := txn.rootTxn.delete(method, rte)
	if !deleted {
		return nil, fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, pattern)
	}

	return route, nil
}

// DeleteRoute deletes an existing route that match the provided [Route] pattern and matchers. On success, it returns
// the deleted [Route]. If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method is invalid or the route is missing.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
func (txn *Txn) DeleteRoute(method string, route *Route) (*Route, error) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	if !txn.write {
		return nil, ErrReadOnlyTxn
	}

	if route == nil {
		return nil, fmt.Errorf("%w: nil route", ErrInvalidRoute)
	}
	if !validMethod(method) {
		return nil, fmt.Errorf("%w: invalid method", ErrInvalidRoute)
	}

	rte, deleted := txn.rootTxn.delete(method, route)
	if !deleted {
		return nil, fmt.Errorf("%w: route %s %s is not registered", ErrRouteNotFound, method, route.pattern)
	}

	return rte, nil
}

// Truncate remove all routes for the provided methods. If no methods are provided,
// all routes are truncated. Truncating on a read-only transaction returns ErrReadOnlyTxn.
func (txn *Txn) Truncate(methods ...string) error {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	if !txn.write {
		return ErrReadOnlyTxn
	}
	txn.rootTxn.truncate(methods)
	return nil
}

// Has allows to check if the given method, pattern and matchers exactly match a registered route. This function is NOT
// thread-safe and should be run serially, along with all other [Txn] APIs. See also [Txn.Route] as an alternative.
func (txn *Txn) Has(method, pattern string, matchers ...Matcher) bool {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	return txn.Route(method, pattern, matchers...) != nil
}

// Route performs a lookup for a registered route matching the given method, pattern and matchers. It returns the [Route] if a
// match is found or nil otherwise. This function is NOT thread-safe and should be run serially, along with all
// other [Txn] APIs. See also [Txn.Has] or [Iter.Routes] as an alternative.
func (txn *Txn) Route(method, pattern string, matchers ...Matcher) *Route {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	root := txn.rootTxn.patterns[method]
	if root == nil {
		return nil
	}

	matched := root.searchPattern(pattern)
	if matched == nil || !matched.isLeaf() {
		return nil
	}
	idx := slices.IndexFunc(matched.routes, func(r *Route) bool { return r.pattern == pattern && r.matchersEqual(matchers) })
	if idx < 0 {
		return nil
	}
	return matched.routes[idx]
}

func (txn *Txn) Name(method, name string) *Route {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	root := txn.rootTxn.names[method]
	if root == nil {
		return nil
	}

	matched := root.searchName(name)
	if matched == nil || !matched.isLeaf() || matched.routes[0].name != name {
		return nil
	}

	return matched.routes[0]
}

// Reverse perform a reverse lookup for the given [http.Request] and return the matching registered [Route]
// (if any) along with a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). This function is NOT thread-safe and should be run serially, along with all
// other [Txn] APIs. See also [Txn.Lookup] as an alternative.
func (txn *Txn) Reverse(r *http.Request) (route *Route, tsr bool) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	tree := txn.rootTxn.tree
	c := tree.pool.Get().(*cTx)
	defer tree.pool.Put(c)
	c.resetWithRequest(r)

	path := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		path = r.URL.RawPath
	}

	idx, n := txn.rootTxn.patterns.lookup(r.Method, r.Host, path, c, true)
	if n != nil {
		return n.routes[idx], c.tsr
	}
	return nil, false
}

// Lookup performs a manual route lookup for a given [http.Request], returning the matched [Route] along with a
// [ContextCloser], and a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). If there is a direct match or a tsr is possible, Lookup always return a
// [Route] and a [ContextCloser]. The [ContextCloser] should always be closed if non-nil. This function is NOT
// thread-safe and should be run serially, along with all other [Txn] APIs. See also [Txn.Reverse] as an alternative.
func (txn *Txn) Lookup(w ResponseWriter, r *http.Request) (route *Route, cc ContextCloser, tsr bool) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	tree := txn.rootTxn.tree
	c := tree.pool.Get().(*cTx)
	c.resetWithWriter(w, r)

	path := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		path = r.URL.RawPath
	}

	idx, n := txn.rootTxn.patterns.lookup(r.Method, r.Host, path, c, false)
	if n != nil {
		c.route = n.routes[idx]
		return n.routes[idx], c, c.tsr
	}
	tree.pool.Put(c)
	return nil, nil, false
}

// Iter returns a collection of range iterators for traversing registered routes. When called on a write transaction,
// Iter creates a point-in-time snapshot of the transaction state. Therefore, writing on the current transaction while
// iterating is allowed, but the mutation will not be observed in the result returned by iterators collection.
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
func (txn *Txn) Iter() Iter {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	patterns, names := txn.rootTxn.patterns, txn.rootTxn.names
	if txn.write {
		patterns, names = txn.rootTxn.snapshot()
	}

	return Iter{
		tree:     txn.rootTxn.tree,
		patterns: patterns,
		names:    names,
		maxDepth: txn.rootTxn.maxDepth,
	}
}

// Len returns the number of registered route.
func (txn *Txn) Len() int {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	return txn.rootTxn.size
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

// Snapshot returns a point in time snapshot of the current state of the transaction.
// Returns a new read-only transaction or nil if the transaction is already aborted
// or commited.
func (txn *Txn) Snapshot() *Txn {
	if txn.rootTxn == nil {
		return nil
	}

	return &Txn{
		fox:     txn.fox,
		rootTxn: txn.rootTxn.clone(),
	}
}

func validMethod(method string) bool {
	/*
	     Method         = "OPTIONS"                ; Section 9.2
	                    | "GET"                    ; Section 9.3
	                    | "HEAD"                   ; Section 9.4
	                    | "POST"                   ; Section 9.5
	                    | "PUT"                    ; Section 9.6
	                    | "DELETE"                 ; Section 9.7
	                    | "TRACE"                  ; Section 9.8
	                    | "CONNECT"                ; Section 9.9
	                    | extension-method
	   extension-method = token
	     token          = 1*<any CHAR except CTLs or separators>
	*/
	return len(method) > 0 && strings.IndexFunc(method, isNotToken) == -1
}

func isNotToken(r rune) bool {
	return !httpguts.IsTokenRune(r)
}
