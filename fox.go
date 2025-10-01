// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
	"fmt"
	"math"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

const verb = 4

const (
	slashDelim   byte = '/'
	dotDelim     byte = '.'
	bracketDelim byte = '{'
	starDelim    byte = '*'
)

var (
	// commonVerbs define http method for which node are pre instantiated.
	commonVerbs = [verb]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}
)

// HandlerFunc is a function type that responds to an HTTP request.
// It enforces the same contract as [http.Handler] but provides additional feature
// like matched wildcard route segments via the [Context] type. The [Context] is freed once
// the HandlerFunc returns and may be reused later to save resources. If you need
// to hold the context longer, you have to copy it (see [Context.Clone] method).
//
// Similar to [http.Handler], to abort a HandlerFunc so the client sees an interrupted
// response, panic with the value [http.ErrAbortHandler].
//
// HandlerFunc functions should be thread-safe, as they will be called concurrently.
type HandlerFunc func(c Context)

// MiddlewareFunc is a function type for implementing [HandlerFunc] middleware.
// The returned [HandlerFunc] usually wraps the input [HandlerFunc], allowing you to perform operations
// before and/or after the wrapped [HandlerFunc] is executed. MiddlewareFunc functions should
// be thread-safe, as they will be called concurrently.
type MiddlewareFunc func(next HandlerFunc) HandlerFunc

// ClientIPResolver define a resolver for obtaining the "real" client IP from HTTP requests. The resolver used must be
// chosen and tuned for your network configuration. This should result in a resolver never returning an error
// i.e., never failing to find a candidate for the "real" IP. Consequently, getting an error result should be treated as
// an application error, perhaps even worthy of panicking. Builtin best practices resolver can be found in the
// github.com/tigerwill90/fox/clientip package.
type ClientIPResolver interface {
	// ClientIP returns the "real" client IP according to the implemented resolver. It returns an error if no valid IP
	// address can be derived. This is typically considered a misconfiguration error, unless the resolver involves
	// obtaining an untrustworthy or optional value.
	ClientIP(c Context) (*net.IPAddr, error)
}

// The ClientIPResolverFunc type is an adapter to allow the use of ordinary functions as [ClientIPResolver]. If f is a
// function with the appropriate signature, ClientIPResolverFunc(f) is a ClientIPResolverFunc that calls f.
type ClientIPResolverFunc func(c Context) (*net.IPAddr, error)

// ClientIP calls f(c).
func (f ClientIPResolverFunc) ClientIP(c Context) (*net.IPAddr, error) {
	return f(c)
}

// HandlerScope represents different scopes where a handler may be called. It also allows for fine-grained control
// over where middleware is applied.
type HandlerScope uint8

const (
	// RouteHandler scope applies to regular routes registered in the router.
	RouteHandler HandlerScope = 1 << (8 - 1 - iota)
	// NoRouteHandler scope applies to the NoRoute handler, which is invoked when no route matches the request.
	NoRouteHandler
	// NoMethodHandler scope applies to the NoMethod handler, which is invoked when a route exists, but the method is not allowed.
	NoMethodHandler
	// RedirectSlashHandler scope applies to the internal redirect trailing slash handler, used for handling requests with trailing slashes.
	RedirectSlashHandler
	// RedirectPathHandler scope applies to the internal redirect fixed path handler, used for handling requests that need path cleaning.
	RedirectPathHandler
	// OptionsHandler scope applies to the automatic OPTIONS handler, which handles pre-flight or cross-origin requests.
	OptionsHandler
)

const (
	// AllHandlers is a combination of all the above scopes, which can be used to apply middlewares to all types of handlers.
	AllHandlers = RouteHandler | NoRouteHandler | NoMethodHandler | RedirectSlashHandler | RedirectPathHandler | OptionsHandler
)

// Router is a lightweight high performance HTTP request router that support mutation on its routing tree
// while handling request concurrently.
type Router struct {
	noRouteBase            HandlerFunc
	noRoute                HandlerFunc
	noMethod               HandlerFunc
	tsrRedirect            HandlerFunc
	pathRedirect           HandlerFunc
	autoOptions            HandlerFunc
	tree                   atomic.Pointer[iTree]
	clientip               ClientIPResolver
	mws                    []middleware
	mu                     sync.Mutex
	maxParams              uint16
	maxParamKeyBytes       uint16
	handleSlash            TrailingSlashOption
	handlePath             FixedPathOption
	handleMethodNotAllowed bool
	handleOptions          bool
}

// RouterInfo hold information on the configured global options.
type RouterInfo struct {
	MaxRouteParams        uint16
	MaxRouteParamKeyBytes uint16
	TrailingSlashOption   TrailingSlashOption
	FixedPathOption       FixedPathOption
	MethodNotAllowed      bool
	AutoOptions           bool
	ClientIP              bool
}

type middleware struct {
	m     MiddlewareFunc
	scope HandlerScope
	g     bool
}

var _ http.Handler = (*Router)(nil)

// New returns a ready to use instance of Fox router.
func New(opts ...GlobalOption) (*Router, error) {
	r := new(Router)

	r.noRouteBase = DefaultNotFoundHandler
	r.noMethod = DefaultMethodNotAllowedHandler
	r.autoOptions = DefaultOptionsHandler
	r.tsrRedirect = internalTrailingSlashHandler
	r.pathRedirect = internalFixedPathHandler
	r.clientip = noClientIPResolver{}
	r.maxParams = math.MaxUint16
	r.maxParamKeyBytes = math.MaxUint16
	r.handleSlash = StrictSlash
	r.handlePath = StrictPath

	for _, opt := range opts {
		if err := opt.applyGlob(sealedOption{router: r}); err != nil {
			return nil, err
		}
	}

	r.noRoute = applyMiddleware(NoRouteHandler, r.mws, r.noRouteBase)
	r.noMethod = applyMiddleware(NoMethodHandler, r.mws, r.noMethod)
	r.tsrRedirect = applyMiddleware(RedirectSlashHandler, r.mws, r.tsrRedirect)
	r.pathRedirect = applyMiddleware(RedirectPathHandler, r.mws, r.pathRedirect)
	r.autoOptions = applyMiddleware(OptionsHandler, r.mws, r.autoOptions)

	r.tree.Store(r.newTree())
	return r, nil
}

// MustHandle registers a new route for the given method and pattern. On success, it returns the newly registered [Route].
// This function is a convenience wrapper for the [Router.Handle] function and panics on error.
func (fox *Router) MustHandle(method, pattern string, handler HandlerFunc, opts ...RouteOption) *Route {
	rte, err := fox.Handle(method, pattern, handler, opts...)
	if err != nil {
		panic(err)
	}
	return rte
}

// Handle registers a new route for the given method and pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrRouteConflict]: If the route conflicts with another.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//
// It's safe to add a new handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To override an existing handler, use [Router.Update].
func (fox *Router) Handle(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	txn := fox.txnWith(true, false)
	defer txn.Abort()
	rte, err := txn.Handle(method, pattern, handler, opts...)
	if err != nil {
		return nil, err
	}
	txn.Commit()
	return rte, nil
}

// HandleRoute registers a new [Route] for the given method. If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrRouteConflict]: If the route conflicts with another.
//   - [ErrInvalidRoute]: If the provided method is invalid or the route is missing.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//
// It's safe to add a new route while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To override an existing route, use [Router.UpdateRoute].
func (fox *Router) HandleRoute(method string, route *Route) error {
	txn := fox.txnWith(true, false)
	defer txn.Abort()
	if err := txn.HandleRoute(method, route); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Update override an existing route for the given method and pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//
// Route-specific option and middleware must be reapplied when updating a route. if not, any middleware and option will
// be removed, and the route will fall back to using global configuration (if any). It's safe to update a handler while
// the router is serving requests. This function is safe for concurrent use by multiple goroutine. To add new handler,
// use [Router.Handle] method.
func (fox *Router) Update(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	txn := fox.txnWith(true, false)
	defer txn.Abort()
	rte, err := txn.Update(method, pattern, handler, opts...)
	if err != nil {
		return nil, err
	}
	txn.Commit()
	return rte, nil
}

// UpdateRoute override an existing [Route] for the given method and new [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method is invalid or the route is missing.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//
// It's safe to update a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To add new route, use [Router.HandleRoute] method.
func (fox *Router) UpdateRoute(method string, route *Route) error {
	txn := fox.txnWith(true, false)
	defer txn.Abort()
	if err := txn.UpdateRoute(method, route); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Delete deletes an existing route for the given method and pattern. On success, it returns the deleted [Route].
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to delete a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine.
func (fox *Router) Delete(method, pattern string) (*Route, error) {
	txn := fox.txnWith(true, false)
	defer txn.Abort()
	route, err := txn.Delete(method, pattern)
	if err != nil {
		return nil, err
	}
	txn.Commit()
	return route, nil
}

// Has allows to check if the given method and route pattern exactly match a registered route. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing. See also [Router.Route] as an alternative.
func (fox *Router) Has(method, pattern string) bool {
	return fox.Route(method, pattern) != nil
}

// Route performs a lookup for a registered route matching the given method and route pattern. It returns the [Route] if a
// match is found or nil otherwise. This function is safe for concurrent use by multiple goroutine and while
// mutation on route are ongoing. See also [Router.Has] as an alternative.
func (fox *Router) Route(method, pattern string) *Route {
	tree := fox.getRoot()
	c := tree.ctx.Get().(*cTx)
	c.resetNil()

	host, path := SplitHostPath(pattern)
	n, tsr := tree.lookup(method, host, path, c, true)
	tree.ctx.Put(c)
	if n != nil && !tsr && n.route.pattern == pattern {
		return n.route
	}
	return nil
}

// Reverse perform a reverse lookup for the given method, host and path and return the matching registered [Route]
// (if any) along with a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). If the path is empty, a default slash is automatically added. This function
// is safe for concurrent use by multiple goroutine and while mutation on routes are ongoing. See also [Router.Lookup]
// as an alternative.
func (fox *Router) Reverse(method, host, path string) (route *Route, tsr bool) {
	tree := fox.getRoot()
	c := tree.ctx.Get().(*cTx)
	c.resetNil()
	n, tsr := tree.lookup(method, host, cmp.Or(path, "/"), c, true)
	tree.ctx.Put(c)
	if n != nil {
		return n.route, tsr
	}
	return nil, false
}

// Lookup performs a manual route lookup for a given [http.Request], returning the matched [Route] along with a
// [ContextCloser], and a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). If there is a direct match or a tsr is possible, Lookup always return a
// [Route] and a [ContextCloser]. The [ContextCloser] should always be closed if non-nil. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing. See also [Router.Reverse] as an alternative.
func (fox *Router) Lookup(w ResponseWriter, r *http.Request) (route *Route, cc ContextCloser, tsr bool) {
	tree := fox.getRoot()
	c := tree.ctx.Get().(*cTx)
	c.resetWithWriter(w, r)

	path := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		path = r.URL.RawPath
	}

	n, tsr := tree.lookup(r.Method, r.Host, path, c, false)
	if n != nil {
		c.route = n.route
		c.tsr = tsr
		return n.route, c, tsr
	}
	tree.ctx.Put(c)
	return nil, nil, tsr
}

// NewRoute create a new [Route], configured with the provided options.
// If an error occurs, it returns one of the following:
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
func (fox *Router) NewRoute(pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	n, endHost, err := fox.parseRoute(pattern)
	if err != nil {
		return nil, err
	}

	rte := &Route{
		clientip:    fox.clientip,
		hbase:       handler,
		pattern:     pattern,
		mws:         fox.mws,
		handleSlash: fox.handleSlash,
		psLen:       n,
		hostSplit:   endHost, // 0 if no host
	}

	for _, opt := range opts {
		if err = opt.applyRoute(sealedOption{route: rte}); err != nil {
			return nil, err
		}
	}
	rte.hself, rte.hall = applyRouteMiddleware(rte.mws, handler)

	return rte, nil
}

// NewRoute2 create a new [Route], configured with the provided options.
// If an error occurs, it returns one of the following:
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
func (fox *Router) NewRoute2(pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	tokens, n, err := fox.parseRoute2(pattern)
	if err != nil {
		return nil, err
	}

	rte := &Route{
		clientip:    fox.clientip,
		hbase:       handler,
		pattern:     pattern,
		mws:         fox.mws,
		handleSlash: fox.handleSlash,
		psLen:       n,
		tokens:      tokens,
	}

	rte.params = make([]string, 0, n)
	for _, tk := range tokens {
		if tk.typ != nodeStatic {
			rte.params = append(rte.params, tk.value)
		}
	}

	for _, opt := range opts {
		if err = opt.applyRoute(sealedOption{route: rte}); err != nil {
			return nil, err
		}
	}
	rte.hself, rte.hall = applyRouteMiddleware(rte.mws, handler)

	return rte, nil
}

// HandleNoRoute calls the no route handler with the provided [Context].
func (fox *Router) HandleNoRoute(c Context) {
	fox.noRouteBase(c)
}

// Len returns the number of registered route.
func (fox *Router) Len() int {
	tree := fox.getRoot()
	return tree.size
}

// Iter returns a collection of range iterators for traversing registered methods and routes. It creates a
// point-in-time snapshot of the routing tree. Therefore, all iterators returned by Iter will not observe subsequent
// write on the router. This function is safe for concurrent use by multiple goroutine and while mutation on
// routes are ongoing.
func (fox *Router) Iter() Iter {
	rt := fox.getRoot()
	return Iter{
		tree:     rt,
		root:     rt.root,
		maxDepth: rt.depth,
	}
}

// Updates executes a function within the context of a read-write managed transaction. If no error is returned from the
// function then the transaction is committed. If an error is returned then the entire transaction is aborted.
// Updates returns any error returned by fn. This function is safe for concurrent use by multiple goroutine and while
// the router is serving request. However [Txn] itself is NOT tread-safe.
// See also [Router.Txn] for unmanaged transaction and [Router.View] for managed read-only transaction.
func (fox *Router) Updates(fn func(txn *Txn) error) error {
	txn := fox.Txn(true)
	defer func() {
		if p := recover(); p != nil {
			txn.Abort()
			panic(p)
		}
		txn.Abort()
	}()
	if err := fn(txn); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// View executes a function within the context of a read-only managed transaction. View returns any error returned
// by fn. This function is safe for concurrent use by multiple goroutine and while mutation on routes are ongoing.
// However [Txn] itself is NOT tread-safe.
// See also [Router.Txn] for unmanaged transaction and [Router.Updates] for managed read-write transaction.
func (fox *Router) View(fn func(txn *Txn) error) error {
	txn := fox.Txn(false)
	defer func() {
		if p := recover(); p != nil {
			txn.Abort()
			panic(p)
		}
		txn.Abort()
	}()
	return fn(txn)
}

// Stats returns information on the configured global option.
func (fox *Router) Stats() RouterInfo {
	_, ok := fox.clientip.(noClientIPResolver)
	return RouterInfo{
		MaxRouteParams:        fox.maxParams,
		MaxRouteParamKeyBytes: fox.maxParamKeyBytes,
		MethodNotAllowed:      fox.handleMethodNotAllowed,
		AutoOptions:           fox.handleOptions,
		TrailingSlashOption:   fox.handleSlash,
		FixedPathOption:       fox.handlePath,
		ClientIP:              !ok,
	}
}

// Txn create a new read-write or read-only transaction. Each [Txn] must be finalized with [Txn.Commit] or [Txn.Abort].
// It's safe to create transaction from multiple goroutine and while the router is serving request.
// However, the returned [Txn] itself is NOT tread-safe.
// See also [Router.Updates] and [Router.View] for managed read-write and 	read-only transaction.
func (fox *Router) Txn(write bool) *Txn {
	return fox.txnWith(write, true)
}

func (fox *Router) txnWith(write, cache bool) *Txn {
	if write {
		fox.mu.Lock()
	}

	return &Txn{
		fox:     fox,
		write:   write,
		rootTxn: fox.getRoot().txn(cache),
	}
}

// newTree returns a fresh routing tree that inherits all registered router options.
func (fox *Router) newTree() *iTree {
	tree := new(iTree)
	tree.fox = fox

	// Pre instantiate nodes for common http verb
	nr := make(roots, len(commonVerbs))
	for i := range commonVerbs {
		nr[i] = new(node)
		nr[i].key = commonVerbs[i]
		nr[i].paramChildIndex = -1
		nr[i].wildcardChildIndex = -1
	}
	tree.root = nr
	tree.ctx = sync.Pool{
		New: func() any {
			return tree.allocateContext()
		},
	}

	return tree
}

// getRoot load the tree atomically.
func (fox *Router) getRoot() *iTree {
	r := fox.tree.Load()
	return r
}

// DefaultNotFoundHandler is a simple [HandlerFunc] that replies to each request
// with a “404 page not found” reply.
func DefaultNotFoundHandler(c Context) {
	http.Error(c.Writer(), "404 page not found", http.StatusNotFound)
}

// DefaultMethodNotAllowedHandler is a simple [HandlerFunc] that replies to each request
// with a “405 Method Not Allowed” reply.
func DefaultMethodNotAllowedHandler(c Context) {
	http.Error(c.Writer(), http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

// DefaultOptionsHandler is a simple [HandlerFunc] that replies to each request with a "200 OK" reply.
func DefaultOptionsHandler(c Context) {
	c.Writer().WriteHeader(http.StatusOK)
}

func internalTrailingSlashHandler(c Context) {
	req := c.Request()

	code := http.StatusMovedPermanently
	if req.Method != http.MethodGet {
		// Will be redirected only with the same method (SEO friendly)
		code = http.StatusPermanentRedirect
	}

	path := escapeLeadingSlashes(FixTrailingSlash(cmp.Or(req.URL.RawPath, req.URL.Path)))
	if q := req.URL.RawQuery; q != "" {
		path += "?" + q
	}

	http.Redirect(c.Writer(), req, path, code)
}

func internalFixedPathHandler(c Context) {
	req := c.Request()

	code := http.StatusMovedPermanently
	if req.Method != http.MethodGet {
		// Will be redirected only with the same method (SEO friendly)
		code = http.StatusPermanentRedirect
	}

	cleanedPath := escapeLeadingSlashes(CleanPath(cmp.Or(req.URL.RawPath, req.URL.Path)))
	if q := req.URL.RawQuery; q != "" {
		cleanedPath += "?" + q
	}

	http.Redirect(c.Writer(), req, cleanedPath, code)
}

// ServeHTTP is the main entry point to serve a request. It handles all incoming HTTP requests and dispatches them
// to the appropriate handler function based on the request's method and path.
func (fox *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	var (
		n   *node
		tsr bool
	)

	path := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		path = r.URL.RawPath
	}

	tree := fox.getRoot()
	c := tree.ctx.Get().(*cTx)
	c.reset(w, r)

	n, tsr = tree.lookup(r.Method, r.Host, path, c, false)
	if !tsr && n != nil {
		c.route = n.route
		c.tsr = tsr
		n.route.hall(c)
		tree.ctx.Put(c)
		return
	}

	if r.Method != http.MethodConnect && r.URL.Path != "/" {
		if tsr {
			if n.route.handleSlash == RelaxedSlash {
				c.route = n.route
				c.tsr = tsr
				n.route.hall(c)
				tree.ctx.Put(c)
				return
			}

			if n.route.handleSlash == RedirectSlash {
				// Since is redirect, we should not share the route even if internally its available, so we reset params as
				// it may have recorded wildcard segment (the context may still be used in a middleware or handler)
				*c.params = (*c.params)[:0]
				c.route = nil
				c.tsr = false
				c.scope = RedirectSlashHandler
				fox.tsrRedirect(c)
				tree.ctx.Put(c)
				return
			}
		}

		if fox.handlePath == RelaxedPath {
			*c.params = (*c.params)[:0]
			if n, tsr := tree.lookup(r.Method, r.Host, CleanPath(path), c, false); n != nil && (!tsr || n.route.handleSlash == RelaxedSlash) {
				c.route = n.route
				c.tsr = tsr
				n.route.hall(c)
				tree.ctx.Put(c)
				return
			}
		}

		if fox.handlePath == RedirectPath {
			*c.params = (*c.params)[:0]
			if n, tsr := tree.lookup(r.Method, r.Host, CleanPath(path), c, true); n != nil && (!tsr || n.route.handleSlash != StrictSlash) {
				c.route = nil
				c.tsr = false
				c.scope = RedirectPathHandler
				fox.pathRedirect(c)
				tree.ctx.Put(c)
				return
			}
		}
	}

	// Reset params as it may have recorded wildcard segment (the context may still be used in no route, no method and
	// automatic option handler or middleware)
	*c.params = (*c.params)[:0]
	c.route = nil
	c.tsr = false

	if r.Method == http.MethodOptions && fox.handleOptions {
		var sb strings.Builder
		// Grow sb to a reasonable size that should prevent new allocation in most case.
		sb.Grow(min((len(tree.root)+1)*5, 150))
		// Handle system-wide OPTIONS, see https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods/OPTIONS.
		// Note that http.Server.DisableGeneralOptionsHandler should be disabled.
		if path == "*" {
			for i := 0; i < len(tree.root); i++ {
				if tree.root[i].key != http.MethodOptions && len(tree.root[i].children) > 0 {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(tree.root[i].key)
				}
			}
		} else {
			// Since different method and route may match (e.g. GET /foo/bar & POST /foo/{name}), we cannot set the path and params.
			for i := 0; i < len(tree.root); i++ {
				if n, tsr := tree.lookup(tree.root[i].key, r.Host, path, c, true); n != nil && (!tsr || n.route.handleSlash == RelaxedSlash) {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(tree.root[i].key)
				}
			}
		}
		if sb.Len() > 0 {
			sb.WriteString(", ")
			sb.WriteString(http.MethodOptions)
			w.Header().Set(HeaderAllow, sb.String())
			c.scope = OptionsHandler
			fox.autoOptions(c)
			tree.ctx.Put(c)
			return
		}
	} else if fox.handleMethodNotAllowed {
		var sb strings.Builder
		// Grow sb to a reasonable size that should prevent new allocation in most case.
		sb.Grow(min((len(tree.root)+1)*5, 150))
		hasOptions := false
		for i := 0; i < len(tree.root); i++ {
			if tree.root[i].key != r.Method {
				if n, tsr := tree.lookup(tree.root[i].key, r.Host, path, c, true); n != nil && (!tsr || n.route.handleSlash == RelaxedSlash) {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					if tree.root[i].key == http.MethodOptions {
						hasOptions = true
					}
					sb.WriteString(tree.root[i].key)
				}
			}
		}
		if sb.Len() > 0 {
			if fox.handleOptions && !hasOptions {
				sb.WriteString(", ")
				sb.WriteString(http.MethodOptions)
			}
			w.Header().Set(HeaderAllow, sb.String())
			c.scope = NoMethodHandler
			fox.noMethod(c)
			tree.ctx.Put(c)
			return
		}
	}

	c.scope = NoRouteHandler
	fox.noRoute(c)
	tree.ctx.Put(c)
}

const (
	stateDefault uint8 = iota
	stateParam
	stateCatchAll
)

// parseRoute parse and validate the route in a single pass.
func (fox *Router) parseRoute(url string) (uint32, int, error) {

	endHost := strings.IndexByte(url, '/')
	if endHost == -1 {
		return 0, -1, fmt.Errorf("%w: missing trailing '/' after hostname", ErrInvalidRoute)
	}
	if strings.HasPrefix(url, ".") {
		return 0, -1, fmt.Errorf("%w: illegal leading '.' in hostname label", ErrInvalidRoute)
	}
	if strings.HasPrefix(url, "-") {
		return 0, -1, fmt.Errorf("%w: illegal leading '-' in hostname label", ErrInvalidRoute)
	}

	var delim byte
	if endHost == 0 {
		delim = slashDelim
	} else {
		delim = dotDelim
	}

	state := stateDefault
	previous := stateDefault
	paramCnt := uint32(0)
	countStatic := 0
	startParam := 0
	inParam := false
	nonNumeric := false // true once we've seen a letter or hyphen
	partlen := 0
	totallen := 0
	last := dotDelim

	i := 0
	for i < len(url) {
		switch state {
		case stateParam:
			if url[i] == '}' {
				if !inParam {
					return 0, -1, fmt.Errorf("%w: missing parameter name between '{}'", ErrInvalidRoute)
				}
				inParam = false

				if i+1 < len(url) && url[i+1] != delim && url[i+1] != '/' {
					return 0, -1, fmt.Errorf("%w: illegal character '%s' after '{param}'", ErrInvalidRoute, string(url[i+1]))
				}

				if i < endHost {
					nonNumeric = true
				}

				countStatic = 0
				previous = state
				state = stateDefault
				i++
				continue
			}

			if i-startParam > int(fox.maxParamKeyBytes) {
				return 0, -1, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrParamKeyTooLarge)
			}

			if url[i] == delim || url[i] == '/' || url[i] == '*' || url[i] == '{' {
				return 0, -1, fmt.Errorf("%w: illegal character '%s' in '{param}'", ErrInvalidRoute, string(url[i]))
			}
			inParam = true
			i++
		case stateCatchAll:
			if url[i] == '}' {
				if !inParam {
					return 0, -1, fmt.Errorf("%w: missing parameter name between '*{}'", ErrInvalidRoute)
				}
				inParam = false

				if i+1 < len(url) && url[i+1] != '/' {
					return 0, -1, fmt.Errorf("%w: illegal character '%s' after '*{param}'", ErrInvalidRoute, string(url[i+1]))
				}

				if previous == stateCatchAll && countStatic <= 1 {
					return 0, -1, fmt.Errorf("%w: consecutive wildcard not allowed", ErrInvalidRoute)
				}

				countStatic = 0
				previous = state
				state = stateDefault
				i++
				continue
			}

			if i-startParam > int(fox.maxParamKeyBytes) {
				return 0, -1, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrParamKeyTooLarge)
			}

			if url[i] == '/' || url[i] == '*' || url[i] == '{' {
				return 0, -1, fmt.Errorf("%w: illegal character '%s' in '*{param}'", ErrInvalidRoute, string(url[i]))
			}
			inParam = true
			i++
		default:

			if i == endHost {
				delim = slashDelim
			}

			switch url[i] {
			case '{':
				state = stateParam
				startParam = i
				paramCnt++
			case '*':
				if i < endHost {
					return 0, -1, fmt.Errorf("%w: catch-all wildcard not supported in hostname", ErrInvalidRoute)
				}
				state = stateCatchAll
				i++
				startParam = i
				paramCnt++
			default:
				countStatic++
				if i < endHost {
					c := url[i]
					switch {
					case 'a' <= c && c <= 'z' || c == '_':
						nonNumeric = true
						partlen++
					case '0' <= c && c <= '9':
						// fine
						partlen++
					case c == '-':
						// Byte before dash cannot be dot.
						if last == '.' {
							return 0, -1, fmt.Errorf("%w: illegal '-' after '.' in hostname label", ErrInvalidRoute)
						}
						partlen++
						nonNumeric = true
					case c == '.':
						// Byte before dot cannot be dot.
						if last == '.' && url[i-1] != '}' {
							return 0, -1, fmt.Errorf("%w: unexpected consecutive '.' in hostname", ErrInvalidRoute)
						}
						// Byte before dot cannot be dash.
						if last == '-' {
							return 0, -1, fmt.Errorf("%w: illegal '-' before '.' in hostname label", ErrInvalidRoute)
						}
						if partlen > 63 {
							return 0, -1, fmt.Errorf("%w: hostname label exceed 63 characters", ErrInvalidRoute)
						}
						totallen += partlen + 1 // +1 count the current dot
						partlen = 0
					case 'A' <= c && c <= 'Z':
						return 0, -1, fmt.Errorf("%w: illegal uppercase character '%s' in hostname label", ErrInvalidRoute, string(c))
					default:
						return 0, -1, fmt.Errorf("%w: illegal character '%s' in hostname label", ErrInvalidRoute, string(c))
					}
					last = c
				} else {
					c := url[i]
					// reject any ASCII control character.
					if c < ' ' || c == 0x7f {
						return 0, -1, fmt.Errorf("%w: illegal control character in path", ErrInvalidRoute)
					}

					// reject any consecutive slash
					if i > endHost && c == '/' && url[i-1] == '/' {
						return 0, -1, fmt.Errorf("%w: illegal consecutive slashes in path", ErrInvalidRoute)
					}

					// reject dot-based traversal patterns
					if i > endHost && c == '.' && url[i-1] == '/' {
						nextIdx := i + 1
						if nextIdx < len(url) {
							nextChar := url[nextIdx]
							switch nextChar {
							case '/':
								return 0, -1, fmt.Errorf("%w: illegal path traversal pattern '/./'", ErrInvalidRoute)
							case '.':
								nextNextIdx := nextIdx + 1
								if nextNextIdx < len(url) {
									if url[nextNextIdx] == '/' {
										return 0, -1, fmt.Errorf("%w: illegal path traversal pattern '/../'", ErrInvalidRoute)
									}
								} else {
									return 0, -1, fmt.Errorf("%w: illegal path traversal pattern '/..' at end", ErrInvalidRoute)
								}
							}
						} else {
							return 0, -1, fmt.Errorf("%w: illegal path traversal pattern '/.' at end", ErrInvalidRoute)
						}
					}
				}
			}

			if paramCnt > uint32(fox.maxParams) {
				return 0, -1, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrTooManyParams)
			}

			i++
		}
	}

	if endHost > 0 {
		totallen += partlen
		if last == '-' {
			return 0, -1, fmt.Errorf("%w: illegal trailing '-' in hostname label", ErrInvalidRoute)
		}
		if url[endHost-1] == '.' {
			return 0, -1, fmt.Errorf("%w: illegal trailing '.' in hostname label", ErrInvalidRoute)
		}
		if !nonNumeric {
			return 0, -1, fmt.Errorf("%w: invalid all numeric hostname", ErrInvalidRoute)
		}
		if partlen > 63 {
			return 0, -1, fmt.Errorf("%w: hostname label exceed 63 characters", ErrInvalidRoute)
		}
		if totallen > 255 {
			return 0, -1, fmt.Errorf("%w: hostname exceed 255 characters", ErrInvalidRoute)
		}
	}

	if state == stateParam {
		return 0, -1, fmt.Errorf("%w: unclosed '{param}'", ErrInvalidRoute)
	}

	if state == stateCatchAll {
		if url[len(url)-1] == '*' {
			return 0, -1, fmt.Errorf("%w: missing '{param}' after '*' catch-all delimiter", ErrInvalidRoute)
		}
		return 0, -1, fmt.Errorf("%w: unclosed '*{param}'", ErrInvalidRoute)
	}

	return paramCnt, endHost, nil
}

// parseRoute parse and validate the route in a single pass.
func (fox *Router) parseRoute2(url string) ([]token, uint32, error) {

	endHost := strings.IndexByte(url, '/')
	if endHost == -1 {
		return nil, 0, fmt.Errorf("%w: missing trailing '/' after hostname", ErrInvalidRoute)
	}
	if strings.HasPrefix(url, ".") {
		return nil, 0, fmt.Errorf("%w: illegal leading '.' in hostname label", ErrInvalidRoute)
	}
	if strings.HasPrefix(url, "-") {
		return nil, 0, fmt.Errorf("%w: illegal leading '-' in hostname label", ErrInvalidRoute)
	}

	var delim byte
	if endHost == 0 {
		delim = slashDelim
	} else {
		delim = dotDelim
	}

	state := stateDefault
	previous := stateDefault
	paramCnt := uint32(0)
	countStatic := 0
	startParam := 0
	inParam := false
	nonNumeric := false // true once we've seen a letter or hyphen
	partlen := 0
	totallen := 0
	last := dotDelim
	tokens := make([]token, 0)
	sb := strings.Builder{}

	i := 0
	for i < len(url) {
		switch state {
		case stateParam:
			if url[i] == '}' {
				if !inParam {
					return nil, 0, fmt.Errorf("%w: missing parameter name between '{}'", ErrInvalidRoute)
				}
				inParam = false

				if i+1 < len(url) && url[i+1] != delim && url[i+1] != '/' {
					return nil, 0, fmt.Errorf("%w: illegal character '%s' after '{param}'", ErrInvalidRoute, string(url[i+1]))
				}

				if i < endHost {
					nonNumeric = true
				}

				paramName, re, err := parseRegex(url[startParam+1 : i])
				if err != nil {
					return nil, 0, err
				}
				tokens = append(tokens, token{
					typ:   nodeParam,
					value: paramName,
					regex: re,
				})
				countStatic = 0
				previous = state
				state = stateDefault
				i++
				continue
			}

			if i-startParam > int(fox.maxParamKeyBytes) {
				return nil, 0, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrParamKeyTooLarge)
			}

			if url[i] == delim || url[i] == '/' || url[i] == '*' || url[i] == '{' {
				return nil, 0, fmt.Errorf("%w: illegal character '%s' in '{param}'", ErrInvalidRoute, string(url[i]))
			}
			inParam = true
			i++
		case stateCatchAll:
			if url[i] == '}' {
				if !inParam {
					return nil, 0, fmt.Errorf("%w: missing parameter name between '*{}'", ErrInvalidRoute)
				}
				inParam = false

				if i+1 < len(url) && url[i+1] != '/' {
					return nil, 0, fmt.Errorf("%w: illegal character '%s' after '*{param}'", ErrInvalidRoute, string(url[i+1]))
				}

				if previous == stateCatchAll && countStatic <= 1 {
					return nil, 0, fmt.Errorf("%w: consecutive wildcard not allowed", ErrInvalidRoute)
				}

				paramName, re, err := parseRegex(url[startParam+1 : i])
				if err != nil {
					return nil, 0, err
				}
				tokens = append(tokens, token{
					typ:   nodeWildcard,
					value: paramName,
					regex: re,
				})
				countStatic = 0
				previous = state
				state = stateDefault
				i++
				continue
			}

			if i-startParam > int(fox.maxParamKeyBytes) {
				return nil, 0, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrParamKeyTooLarge)
			}

			if url[i] == '/' || url[i] == '*' || url[i] == '{' {
				return nil, 0, fmt.Errorf("%w: illegal character '%s' in '*{param}'", ErrInvalidRoute, string(url[i]))
			}
			inParam = true
			i++
		default:

			if i == endHost {
				delim = slashDelim
			}

			switch url[i] {
			case '{':
				tokens = append(tokens, token{
					typ:   nodeStatic,
					value: sb.String(),
				})
				sb.Reset()
				state = stateParam
				startParam = i
				paramCnt++
			case '*':
				if i < endHost {
					return nil, 0, fmt.Errorf("%w: catch-all wildcard not supported in hostname", ErrInvalidRoute)
				}
				tokens = append(tokens, token{
					typ:   nodeStatic,
					value: sb.String(),
				})
				sb.Reset()
				state = stateCatchAll
				i++
				startParam = i
				paramCnt++
			default:
				sb.WriteByte(url[i])
				countStatic++
				if i < endHost {
					c := url[i]
					switch {
					case 'a' <= c && c <= 'z' || c == '_':
						nonNumeric = true
						partlen++
					case '0' <= c && c <= '9':
						// fine
						partlen++
					case c == '-':
						// Byte before dash cannot be dot.
						if last == '.' {
							return nil, 0, fmt.Errorf("%w: illegal '-' after '.' in hostname label", ErrInvalidRoute)
						}
						partlen++
						nonNumeric = true
					case c == '.':
						// Byte before dot cannot be dot.
						if last == '.' && url[i-1] != '}' {
							return nil, 0, fmt.Errorf("%w: unexpected consecutive '.' in hostname", ErrInvalidRoute)
						}
						// Byte before dot cannot be dash.
						if last == '-' {
							return nil, 0, fmt.Errorf("%w: illegal '-' before '.' in hostname label", ErrInvalidRoute)
						}
						if partlen > 63 {
							return nil, 0, fmt.Errorf("%w: hostname label exceed 63 characters", ErrInvalidRoute)
						}
						totallen += partlen + 1 // +1 count the current dot
						partlen = 0
					case 'A' <= c && c <= 'Z':
						return nil, 0, fmt.Errorf("%w: illegal uppercase character '%s' in hostname label", ErrInvalidRoute, string(c))
					default:
						return nil, 0, fmt.Errorf("%w: illegal character '%s' in hostname label", ErrInvalidRoute, string(c))
					}
					last = c
				} else {
					c := url[i]
					// reject any ASCII control character.
					if c < ' ' || c == 0x7f {
						return nil, 0, fmt.Errorf("%w: illegal control character in path", ErrInvalidRoute)
					}

					// reject any consecutive slash
					if i > endHost && c == '/' && url[i-1] == '/' {
						return nil, 0, fmt.Errorf("%w: illegal consecutive slashes in path", ErrInvalidRoute)
					}

					// reject dot-based traversal patterns
					if i > endHost && c == '.' && url[i-1] == '/' {
						nextIdx := i + 1
						if nextIdx < len(url) {
							nextChar := url[nextIdx]
							switch nextChar {
							case '/':
								return nil, 0, fmt.Errorf("%w: illegal path traversal pattern '/./'", ErrInvalidRoute)
							case '.':
								nextNextIdx := nextIdx + 1
								if nextNextIdx < len(url) {
									if url[nextNextIdx] == '/' {
										return nil, 0, fmt.Errorf("%w: illegal path traversal pattern '/../'", ErrInvalidRoute)
									}
								} else {
									return nil, 0, fmt.Errorf("%w: illegal path traversal pattern '/..' at end", ErrInvalidRoute)
								}
							}
						} else {
							return nil, 0, fmt.Errorf("%w: illegal path traversal pattern '/.' at end", ErrInvalidRoute)
						}
					}
				}
			}

			if paramCnt > uint32(fox.maxParams) {
				return nil, 0, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrTooManyParams)
			}
			i++
		}
	}

	if endHost > 0 {
		totallen += partlen
		if last == '-' {
			return nil, 0, fmt.Errorf("%w: illegal trailing '-' in hostname label", ErrInvalidRoute)
		}
		if url[endHost-1] == '.' {
			return nil, 0, fmt.Errorf("%w: illegal trailing '.' in hostname label", ErrInvalidRoute)
		}
		if !nonNumeric {
			return nil, 0, fmt.Errorf("%w: invalid all numeric hostname", ErrInvalidRoute)
		}
		if partlen > 63 {
			return nil, 0, fmt.Errorf("%w: hostname label exceed 63 characters", ErrInvalidRoute)
		}
		if totallen > 255 {
			return nil, 0, fmt.Errorf("%w: hostname exceed 255 characters", ErrInvalidRoute)
		}
	}

	if state == stateParam {
		return nil, 0, fmt.Errorf("%w: unclosed '{param}'", ErrInvalidRoute)
	}

	if state == stateCatchAll {
		if url[len(url)-1] == '*' {
			return nil, 0, fmt.Errorf("%w: missing '{param}' after '*' catch-all delimiter", ErrInvalidRoute)
		}
		return nil, 0, fmt.Errorf("%w: unclosed '*{param}'", ErrInvalidRoute)
	}

	tokens = append(tokens, token{
		typ:   nodeStatic,
		value: sb.String(),
	})

	return tokens, paramCnt, nil
}

func parseRegex(param string) (string, *regexp.Regexp, error) {
	idx := strings.IndexByte(param, ':')
	if idx == -1 {
		return param, nil, nil
	}
	pattern := param[idx+1:]
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrInvalidRoute, err)
	}

	if re.NumSubexp() > 0 {
		return "", nil, fmt.Errorf("%w: illegal capture group '%s': use (?:pattern) instead", ErrInvalidRoute, pattern)
	}
	return param[:idx], re, nil
}

func applyMiddleware(scope HandlerScope, mws []middleware, h HandlerFunc) HandlerFunc {
	m := h
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i].scope&scope != 0 {
			m = mws[i].m(m)
		}
	}
	return m
}

func applyRouteMiddleware(mws []middleware, base HandlerFunc) (HandlerFunc, HandlerFunc) {
	rte := base
	all := base
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i].scope&RouteHandler != 0 {
			all = mws[i].m(all)
			// route specific only
			if !mws[i].g {
				rte = mws[i].m(rte)
			}
		}
	}
	return rte, all
}

type noClientIPResolver struct{}

func (s noClientIPResolver) ClientIP(_ Context) (*net.IPAddr, error) {
	return nil, ErrNoClientIPResolver
}
