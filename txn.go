package fox

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/tigerwill90/fox/internal/slicesutil"
	"golang.org/x/net/http/httpguts"
)

// Txn is a read or write transaction on the routing tree.
type Txn struct {
	fox     *Router
	rootTxn *tXn
	write   bool
}

// Add registers a new route for the given methods, pattern and matchers. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteConflict]: If the route conflict with others.
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
// To override an existing handler, use [Txn.Update].
func (txn *Txn) Add(methods []string, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	if !txn.write {
		return nil, ErrReadOnlyTxn
	}

	rte, err := txn.fox.NewRoute(methods, pattern, handler, opts...)
	if err != nil {
		return nil, err
	}

	if err = txn.rootTxn.insert(rte, modeInsert); err != nil {
		return nil, err
	}
	return rte, nil
}

// AddRoute registers a new [Route]. If an error occurs, it returns one of the following:
//   - [ErrRouteConflict]: If the route conflict with others.
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the route is missing.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
// To override an existing route, use [Txn.UpdateRoute].
func (txn *Txn) AddRoute(route *Route) error {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	if !txn.write {
		return ErrReadOnlyTxn
	}
	if route == nil {
		return fmt.Errorf("%w: nil route", ErrInvalidRoute)
	}
	if route.owner != txn.fox {
		panic("route belongs to a different router")
	}

	return txn.rootTxn.insert(route, modeInsert)
}

// Update override an existing route for the given methods, pattern and matchers. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// Route-specific option and middleware must be reapplied when updating a route. if not, any middleware and option will
// be removed (or reset to their default value), and the route will fall back to using global configuration (if any).
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
// To add a new handler, use [Txn.Add].
func (txn *Txn) Update(methods []string, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	if !txn.write {
		return nil, ErrReadOnlyTxn
	}

	rte, err := txn.fox.NewRoute(methods, pattern, handler, opts...)
	if err != nil {
		return nil, err
	}

	if err = txn.rootTxn.insert(rte, modeUpdate); err != nil {
		return nil, err
	}

	return rte, nil
}

// UpdateRoute override an existing [Route] for the given new [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the route is missing.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
// To add a new route, use [Txn.AddRoute].
func (txn *Txn) UpdateRoute(route *Route) error {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	if !txn.write {
		return ErrReadOnlyTxn
	}
	if route == nil {
		return fmt.Errorf("%w: nil route", ErrInvalidRoute)
	}
	if route.owner != txn.fox {
		panic("route belongs to a different router")
	}

	return txn.rootTxn.insert(route, modeUpdate)
}

// Delete deletes an existing route for the given methods, pattern and matchers. On success, it returns the deleted [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
func (txn *Txn) Delete(methods []string, pattern string, opts ...MatcherOption) (*Route, error) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	if !txn.write {
		return nil, ErrReadOnlyTxn
	}

	for _, method := range methods {
		if !validMethod(method) {
			return nil, fmt.Errorf("%w: invalid method '%s'", ErrInvalidRoute, method)
		}
	}

	parsed, err := txn.fox.parseRoute(pattern)
	if err != nil {
		return nil, err
	}

	rte := &Route{
		pattern:   txn.fox.prefix + pattern,
		hostEnd:   parsed.endHost,
		prefixEnd: len(txn.fox.prefix),
		tokens:    parsed.token,
	}

	for _, opt := range opts {
		if err = opt.applyMatcher(sealedOption{route: rte}); err != nil {
			return nil, err
		}
	}

	if len(rte.matchers) > txn.fox.maxMatchers {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrTooManyMatchers)
	}

	// If this route is registered with methods, push the internal matcher at first position.
	if len(methods) > 0 {
		// As a defensive mesure, keep our own copy of the provided slice.
		rte.methods = make([]string, len(methods))
		copy(rte.methods, methods)
		slices.Sort(rte.methods)
		rte.methods = slices.Compact(rte.methods)
	}

	route, deleted := txn.rootTxn.delete(rte)
	if !deleted {
		return nil, newRouteNotFoundError(rte)
	}

	return route, nil
}

// DeleteRoute deletes an existing route that match the provided [Route] pattern and matchers. On success, it returns
// the deleted [Route]. If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the route is missing.
//   - [ErrReadOnlyTxn]: On write in a read-only transaction.
//
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
func (txn *Txn) DeleteRoute(route *Route) (*Route, error) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}
	if !txn.write {
		return nil, ErrReadOnlyTxn
	}

	if route == nil {
		return nil, fmt.Errorf("%w: nil route", ErrInvalidRoute)
	}

	if route.owner != txn.fox {
		panic("route belongs to a different router")
	}

	rte, deleted := txn.rootTxn.delete(route)
	if !deleted {
		return nil, newRouteNotFoundError(route)
	}

	return rte, nil
}

// Truncate remove all routes registered in the router. Truncating on a read-only transaction returns ErrReadOnlyTxn.
func (txn *Txn) Truncate() error {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	if !txn.write {
		return ErrReadOnlyTxn
	}
	txn.rootTxn.truncate()
	return nil
}

// Has allows to check if the given methods, pattern and matchers exactly match a registered route. This function is NOT
// thread-safe and should be run serially, along with all other [Txn] APIs. See also [Txn.Route] as an alternative.
func (txn *Txn) Has(methods []string, pattern string, matchers ...Matcher) bool {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	return txn.Route(methods, pattern, matchers...) != nil
}

// Route performs a lookup for a registered route matching the given methods, pattern and matchers. It returns the [Route] if a
// match is found or nil otherwise. This function is NOT thread-safe and should be run serially, along with all
// other [Txn] APIs. See also [Txn.Has] or [Iter.Routes] as an alternative.
func (txn *Txn) Route(methods []string, pattern string, matchers ...Matcher) *Route {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	root := txn.rootTxn.patterns
	matched := root.searchPattern(pattern)
	if matched == nil || !matched.isLeaf() {
		return nil
	}
	idx := slices.IndexFunc(matched.routes, func(r *Route) bool {
		return r.Pattern() == pattern && slicesutil.EqualUnsorted(r.methods, methods) && r.matchersEqual(matchers)
	})
	if idx < 0 {
		return nil
	}
	return matched.routes[idx]
}

// Name performs a lookup for a registered route matching the given method and route name. It returns
// the [Route] if a match is found or nil otherwise. This function is NOT thread-safe and should be run serially,
// along with all other [Txn] APIs. See also [Txn.Route] as an alternative.
func (txn *Txn) Name(name string) *Route {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	root := txn.rootTxn.names
	matched := root.searchName(name)
	if matched == nil || !matched.isLeaf() || matched.routes[0].name != name {
		return nil
	}

	return matched.routes[0]
}

// Match perform a reverse lookup for the given method and [http.Request]. It returns the matching registered [Route]
// (if any) along with a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). This function is NOT thread-safe and should be run serially, along with all
// other [Txn] APIs. See also [Txn.Lookup] as an alternative.
func (txn *Txn) Match(method string, r *http.Request) (route *Route, tsr bool) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	tree := txn.rootTxn.tree
	c := tree.pool.Get().(*Context)
	defer tree.pool.Put(c)
	c.resetWithRequest(r)

	path := c.Path()

	idx, n, tsr := txn.rootTxn.patterns.lookup(method, r.Host, path, c, true)
	if n != nil {
		return n.routes[idx], tsr
	}
	return
}

// Lookup performs a manual route lookup for a given [http.Request], returning the matched [Route] along with a
// [Context], and a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). If there is a direct match or a tsr is possible, Lookup always return a
// [Route] and a [Context]. The [Context] should always be closed if non-nil. This function is NOT
// thread-safe and should be run serially, along with all other [Txn] APIs. See also [Txn.Match] as an alternative.
func (txn *Txn) Lookup(w ResponseWriter, r *http.Request) (route *Route, cc *Context, tsr bool) {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	tree := txn.rootTxn.tree
	c := tree.pool.Get().(*Context)
	c.resetWithWriter(w, r)

	path := c.Path()

	idx, n, tsr := txn.rootTxn.patterns.lookup(r.Method, r.Host, path, c, false)
	if n != nil {
		c.route = n.routes[idx]
		r.Pattern = c.route.pattern
		*c.paramsKeys = c.route.params
		return c.route, c, tsr
	}

	tree.pool.Put(c)
	return
}

// Iter returns a collection of range iterators for traversing registered routes. When called on a write transaction,
// Iter creates a point-in-time snapshot of the transaction state. Therefore, writing on the current transaction while
// iterating is allowed, but the mutation will not be observed in the result returned by iterators collection.
// This function is NOT thread-safe and should be run serially, along with all other [Txn] APIs.
func (txn *Txn) Iter() Iter {
	if txn.rootTxn == nil {
		panic(ErrSettledTxn)
	}

	patterns, names, methods := txn.rootTxn.patterns, txn.rootTxn.names, txn.rootTxn.methods
	if txn.write {
		patterns, names, methods = txn.rootTxn.snapshot()
	}

	return Iter{
		tree:     txn.rootTxn.tree,
		patterns: patterns,
		names:    names,
		methods:  methods,
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
