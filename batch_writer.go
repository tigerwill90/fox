package fox

import "net/http"

// BatchWriter provides a mechanism for performing non-transactional updates with a consistent view of
// the router's state. Unlike [Txn], updates are applied immediately. It's safe to run multiple batch
// concurrently and while the router is serving request, however BatchWriter itself is not tread-safe.
// Each BatchWriter must be finalized with [BatchWriter.Release]. For transactional update, see [Router.Txn]
// or [Router.Updates].
type BatchWriter struct {
	tree *Tree
}

// Handle registers a new handler for the given method and route pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrRouteConflict]: If the route conflicts with another.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to add a new handler while the router is serving requests. However, this function is NOT
// thread-safe and should be run serially, along with all other [BatchWriter] APIs that perform write operations.
// To override an existing route, use [BatchWriter.Update].
func (wr BatchWriter) Handle(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	return wr.tree.Handle(method, pattern, handler, opts...)
}

// Update override an existing handler for the given method and route pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to update a handler while the router is serving requests. However, this function is NOT thread-safe
// and should be run serially, along with all other [BatchWriter] APIs that perform write operations. To add a new handler,
// use [BatchWriter.Handle] method.
func (wr BatchWriter) Update(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	return wr.tree.Update(method, pattern, handler, opts...)
}

// Delete deletes an existing handler for the given method and router pattern. If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to delete a handler while the tree is in use for serving requests. However, this function is NOT
// thread-safe and should be run serially, along with all other [BatchWriter] APIs that perform write operations.
func (wr BatchWriter) Delete(method, pattern string) error {
	return wr.tree.Delete(method, pattern)
}

// Has allows to check if the given method and route pattern exactly match a registered route. This function is safe for
// concurrent use by multiple goroutine and while mutation routes are ongoing. See also [BatchWriter.Route] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (wr BatchWriter) Has(method, pattern string) bool {
	return wr.tree.Route(method, pattern) != nil
}

// Route performs a lookup for a registered route matching the given method and route pattern. It returns the [Route] if a
// match is found or nil otherwise. This function is safe for concurrent use by multiple goroutine and while
// mutation on routes are ongoing. See also [BatchWriter.Has] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (wr BatchWriter) Route(method, pattern string) *Route {
	return wr.tree.Route(method, pattern)
}

// Reverse perform a reverse lookup for the given method, host and path and return the matching registered [Route]
// (if any) along with a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). This function is safe for concurrent use by multiple goroutine and while
// mutation on routes ongoing. See also [BatchWriter.Lookup] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (wr BatchWriter) Reverse(method, host, path string) (route *Route, tsr bool) {
	return wr.tree.Reverse(method, host, path)
}

// Lookup performs a manual route lookup for a given [http.Request], returning the matched [Route] along with a
// [ContextCloser], and a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). If there is a direct match or a tsr is possible, Lookup always return a
// [Route] and a [ContextCloser]. The [ContextCloser] should always be closed if non-nil. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing. See also [BatchWriter.Reverse] as an
// alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (wr BatchWriter) Lookup(w ResponseWriter, r *http.Request) (route *Route, cc ContextCloser, tsr bool) {
	return wr.tree.Lookup(w, r)
}

// Iter returns a collection of iterators for traversing the routing tree.
// This function is safe for concurrent use by multiple goroutine and while mutation routes are ongoing.
// This API is EXPERIMENTAL and may change in future releases.
func (wr BatchWriter) Iter() Iter {
	return Iter{t: wr.tree}
}

// Release finalize the batch writer. This is a noop for batch already released.
func (wr BatchWriter) Release() {
	if wr.tree == nil {
		return
	}
	fox := wr.tree.fox
	wr.tree = nil
	fox.mu.Unlock()
}
