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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	slashDelim   byte = '/'
	dotDelim     byte = '.'
	bracketDelim byte = '{'
	starDelim    byte = '*'
	plusDelim    byte = '+'
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
type HandlerFunc func(c *Context)

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
	ClientIP(c RequestContext) (*net.IPAddr, error)
}

// The ClientIPResolverFunc type is an adapter to allow the use of ordinary functions as [ClientIPResolver]. If f is a
// function with the appropriate signature, ClientIPResolverFunc(f) is a ClientIPResolverFunc that calls f.
type ClientIPResolverFunc func(c RequestContext) (*net.IPAddr, error)

// ClientIP calls f(c).
func (f ClientIPResolverFunc) ClientIP(c RequestContext) (*net.IPAddr, error) {
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
	autoOPTIONS            HandlerFunc
	tree                   atomic.Pointer[iTree]
	clientip               ClientIPResolver
	mws                    []middleware
	mu                     sync.Mutex
	maxParams              int
	maxParamKeyBytes       int
	maxMatchers            int
	handleSlash            TrailingSlashOption
	handlePath             FixedPathOption
	handleMethodNotAllowed bool
	handleOPTIONS          bool
	systemWideOPTIONS      bool
	allowRegexp            bool
}

// RouterInfo hold information on the configured global options.
type RouterInfo struct {
	MaxRouteParams        int
	MaxRouteParamKeyBytes int
	MaxRouteMatchers      int
	TrailingSlashOption   TrailingSlashOption
	FixedPathOption       FixedPathOption
	MethodNotAllowed      bool
	AutoOptions           bool
	SystemWideOptions     bool
	ClientIP              bool
	AllowRegexp           bool
}

type middleware struct {
	m     MiddlewareFunc
	scope HandlerScope
	g     bool
}

var _ http.Handler = (*Router)(nil)

// MustNew returns a ready to use instance of Fox router.
// This function is a convenience wrapper for [New] and panics on error.
func MustNew(opts ...GlobalOption) *Router {
	f, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return f
}

// New returns a ready to use instance of Fox router.
func New(opts ...GlobalOption) (*Router, error) {
	r := new(Router)

	r.noRouteBase = DefaultNotFoundHandler
	r.noMethod = DefaultMethodNotAllowedHandler
	r.autoOPTIONS = DefaultOptionsHandler
	r.tsrRedirect = internalTrailingSlashHandler
	r.pathRedirect = internalFixedPathHandler
	r.clientip = noClientIPResolver{}
	r.maxParams = math.MaxUint8
	r.maxParamKeyBytes = math.MaxUint8
	r.maxMatchers = math.MaxUint8
	r.handleSlash = StrictSlash
	r.handlePath = StrictPath
	r.systemWideOPTIONS = true

	for _, opt := range opts {
		if err := opt.applyGlob(sealedOption{router: r}); err != nil {
			return nil, err
		}
	}

	r.noRoute = applyMiddleware(NoRouteHandler, r.mws, r.noRouteBase)
	r.noMethod = applyMiddleware(NoMethodHandler, r.mws, r.noMethod)
	r.tsrRedirect = applyMiddleware(RedirectSlashHandler, r.mws, r.tsrRedirect)
	r.pathRedirect = applyMiddleware(RedirectPathHandler, r.mws, r.pathRedirect)
	r.autoOPTIONS = applyMiddleware(OptionsHandler, r.mws, r.autoOPTIONS)

	r.tree.Store(r.newTree())
	return r, nil
}

// MustHandle registers a new route for the given method, pattern and matchers. On success, it returns the newly registered [Route].
// This function is a convenience wrapper for the [Router.Handle] function and panics on error.
func (fox *Router) MustHandle(method, pattern string, handler HandlerFunc, opts ...RouteOption) *Route {
	rte, err := fox.Handle(method, pattern, handler, opts...)
	if err != nil {
		panic(err)
	}
	return rte
}

// Handle registers a new route for the given method, pattern and matchers. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//
// It's safe to add a new handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To override an existing handler, use [Router.Update].
func (fox *Router) Handle(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	txn := fox.Txn(true)
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
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the provided method is invalid or the route is missing.
//
// It's safe to add a new route while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To override an existing route, use [Router.UpdateRoute].
func (fox *Router) HandleRoute(method string, route *Route) error {
	txn := fox.Txn(true)
	defer txn.Abort()
	if err := txn.HandleRoute(method, route); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Update override an existing route for the given method, pattern and matchers. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//
// Route-specific option and middleware must be reapplied when updating a route. if not, any middleware and option will
// be removed (or reset to their default value), and the route will fall back to using global configuration (if any).
// It's safe to update a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To add new handler, use [Router.Handle] method.
func (fox *Router) Update(method, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	txn := fox.Txn(true)
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
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the provided method is invalid or the route is missing.
//
// It's safe to update a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To add new route, use [Router.HandleRoute] method.
func (fox *Router) UpdateRoute(method string, route *Route) error {
	txn := fox.Txn(true)
	defer txn.Abort()
	if err := txn.UpdateRoute(method, route); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Delete deletes an existing route for the given method, pattern and matchers. On success, it returns the deleted [Route].
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//
// It's safe to delete a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine.
func (fox *Router) Delete(method, pattern string, opts ...MatcherOption) (*Route, error) {
	txn := fox.Txn(true)
	defer txn.Abort()
	route, err := txn.Delete(method, pattern, opts...)
	if err != nil {
		return nil, err
	}
	txn.Commit()
	return route, nil
}

// DeleteRoute deletes an existing route that match the provided [Route] pattern and matchers. On success, it returns
// the deleted [Route]. If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method is invalid or the route is missing.
//
// It's safe to delete a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine.
func (fox *Router) DeleteRoute(method string, route *Route) (*Route, error) {
	txn := fox.Txn(true)
	defer txn.Abort()
	route, err := txn.DeleteRoute(method, route)
	if err != nil {
		return nil, err
	}
	txn.Commit()
	return route, nil
}

// Has allows to check if the given method, pattern and matchers exactly match a registered route. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing. See also [Router.Route] as an alternative.
func (fox *Router) Has(method, pattern string, matchers ...Matcher) bool {
	return fox.Route(method, pattern, matchers...) != nil
}

// Route performs a lookup for a registered route matching the given method, pattern and matchers. It returns the [Route] if a
// match is found or nil otherwise. This function is safe for concurrent use by multiple goroutine and while
// mutation on route are ongoing. See also [Router.Has] or [Iter.Routes] as an alternative.
func (fox *Router) Route(method, pattern string, matchers ...Matcher) *Route {
	tree := fox.getTree()

	root := tree.patterns[method]
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

// Name performs a lookup for a registered route matching the given method and route name. It returns
// the [Route] if a match is found or nil otherwise. This function is safe for concurrent use by multiple
// goroutines and while mutations on routes are ongoing. See also [Router.Route] as an alternative.
func (fox *Router) Name(method, name string) *Route {
	tree := fox.getTree()

	root := tree.names[method]
	if root == nil {
		return nil
	}

	matched := root.searchName(name)
	if matched == nil || !matched.isLeaf() || matched.routes[0].name != name {
		return nil
	}

	return matched.routes[0]
}

// Match perform a reverse lookup for the given method and [http.Request]. It returns the matching registered [Route]
// (if any) along with a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). This function is safe for concurrent use by multiple goroutine and while
// mutation on routes are ongoing. See also [Router.Lookup] as an alternative.
func (fox *Router) Match(method string, r *http.Request) (route *Route, tsr bool) {
	tree := fox.getTree()
	c := tree.pool.Get().(*Context)
	defer tree.pool.Put(c)
	c.resetWithRequest(r)

	path := c.Path()

	idx, n := tree.lookup(method, r.Host, path, c, true)
	if n != nil {
		return n.routes[idx], c.tsr
	}
	return nil, false
}

// Lookup performs a manual route lookup for a given [http.Request], returning the matched [Route] along with a
// [Context], and a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). If there is a direct match or a tsr is possible, Lookup always return a
// [Route] and a [Context]. The [Context] should always be closed if non-nil. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing. See also [Router.Match] as an alternative.
func (fox *Router) Lookup(w ResponseWriter, r *http.Request) (route *Route, cc *Context, tsr bool) {
	tree := fox.getTree()
	c := tree.pool.Get().(*Context)
	c.resetWithWriter(w, r)

	path := c.Path()

	idx, n := tree.lookup(r.Method, r.Host, path, c, false)
	if n != nil {
		c.route = n.routes[idx]
		r.Pattern = c.route.pattern
		*c.paramsKeys = c.route.params
		return c.route, c, c.tsr
	}

	*c.params = (*c.params)[:0]
	idx, n = tree.lookup(MethodAny, r.Host, path, c, false)
	if n != nil {
		c.route = n.routes[idx]
		r.Pattern = c.route.pattern
		*c.paramsKeys = c.route.params
		return c.route, c, c.tsr
	}

	tree.pool.Put(c)
	return nil, nil, false
}

// NewRoute create a new [Route], configured with the provided options.
// If an error occurs, it returns one of the following:
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
func (fox *Router) NewRoute(pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	parsed, err := fox.parseRoute(pattern)
	if err != nil {
		return nil, err
	}

	rte := &Route{
		clientip:    fox.clientip,
		hbase:       handler,
		pattern:     pattern,
		mws:         fox.mws,
		handleSlash: fox.handleSlash,
		hostSplit:   parsed.endHost, // 0 if no host
		priority:    0,
		tokens:      parsed.token,
		catchEmpty:  parsed.startCatchAll > 0 && pattern[parsed.startCatchAll] == plusDelim,
	}

	rte.params = make([]string, 0, parsed.paramCnt)
	for _, tk := range parsed.token {
		if tk.typ != nodeStatic {
			rte.params = append(rte.params, tk.value)
		}
	}

	for _, opt := range opts {
		if err = opt.applyRoute(sealedOption{route: rte}); err != nil {
			return nil, err
		}
	}

	if len(rte.matchers) > fox.maxMatchers {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrTooManyMatchers)
	}
	if len(rte.matchers) == 0 && rte.priority > 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRoute, "priority requires matcher")
	}

	rte.priority = cmp.Or(rte.priority, uint(len(rte.matchers)))
	rte.hself, rte.hall = applyRouteMiddleware(rte.mws, handler)

	return rte, nil
}

// NewSubRouter creates a new [Route] that mounts the provided [Router] at the given pattern.
// The pattern must end with a catch-all wildcard (e.g., /api/*{path} or /api/+{path}).
// The returned route can be used like any other route created with [Router.NewRoute].
// The mounted router can be retrieved using [Route.SubRouter].
// If an error occurs, it returns one of the following:
//   - [ErrInvalidRoute]: If the provided pattern is invalid or does not end with a catch-all wildcard.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
func (fox *Router) NewSubRouter(pattern string, r *Router, opts ...RouteOption) (*Route, error) {
	if fox == r {
		panic("cannot mount router onto itself")
	}

	parsed, err := fox.parseRoute(pattern)
	if err != nil {
		return nil, err
	}

	if parsed.startCatchAll <= 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRoute, "subrouter pattern must end with a suffix wildcard")
	}

	rte := &Route{
		clientip:    fox.clientip,
		pattern:     pattern,
		mws:         fox.mws,
		handleSlash: fox.handleSlash,
		hostSplit:   parsed.endHost, // 0 if no host
		priority:    0,
		tokens:      parsed.token,
		sub:         r,
		catchEmpty:  pattern[parsed.startCatchAll] == plusDelim,
	}

	rte.params = make([]string, 0, parsed.paramCnt)
	for _, tk := range parsed.token {
		if tk.typ != nodeStatic {
			rte.params = append(rte.params, tk.value)
		}
	}

	for _, opt := range opts {
		if err = opt.applyRoute(sealedOption{route: rte}); err != nil {
			return nil, err
		}
	}

	if len(rte.matchers) > fox.maxMatchers {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrTooManyMatchers)
	}
	if len(rte.matchers) == 0 && rte.priority > 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRoute, "priority requires matcher")
	}

	patternPrefix := pattern[:parsed.startCatchAll]
	// Precomputed to avoid alloc in the hot path.
	patternWithSlash := patternPrefix + "/"
	rte.hbase = func(c *Context) {
		tree := r.getTree()
		subCtx := tree.pool.Get().(*Context)
		subCtx.resetWithWriter(c.Writer(), c.Request())

		req := subCtx.Request()
		req.Pattern = patternPrefix

		// Extract the path suffix to be routed by the subrouter. The suffix value may be empty,
		// but there is always at least a catch-all param recorded since we enforce suffix catch-all.
		suffix := (*c.params)[len(*c.params)-1]
		last := len(req.Pattern) - 1
		if suffix != "" {
			if req.Pattern[last] == '/' {
				// Mount pattern ends with slash (e.g., /api/*{any}), so suffix lacks the
				// leading slash (e.g., "users" instead of "/users"). Reslice from the
				// original path to include it, avoiding allocation from "/" + suffix.
				path := c.Path()
				suffix = path[len(path)-len(suffix)-1:]
			} else {
				// Mount pattern has no trailing slash (e.g., /api*{any}), so suffix
				// already includes the leading slash. Use precomputed patternWithSlash
				// to avoid allocation from pattern + "/".
				req.Pattern = patternWithSlash
			}
		}

		// Copy parent params and paramsKeys to the subrouter context, excluding the last
		// entry which is the catch-all wildcard used to mount the subrouter.
		// Subrouters are never evaluated in lazy lookup mode, so params are always
		// captured. If parent has no params beyond the catch-all, this is a no-op.
		*subCtx.params = append(*subCtx.params, (*c.params)[:len(*c.params)-1]...)
		*subCtx.paramsKeys = append((*subCtx.paramsKeys)[:0], (*c.paramsKeys)[:len(*c.paramsKeys)-1]...)

		// Serve the sub router
		r.serveSubRouter(subCtx, cmp.Or(suffix, "/"))

		tree.pool.Put(subCtx)
	}

	rte.priority = cmp.Or(rte.priority, uint(len(rte.matchers)))
	rte.hself, rte.hall = applyRouteMiddleware(rte.mws, rte.hbase)

	return rte, nil
}

// HandleNoRoute calls the no route handler with the provided [Context].
// Note that this bypasses any middleware attached to the no route handler.
func (fox *Router) HandleNoRoute(c *Context) {
	fox.noRouteBase(c)
}

// Len returns the number of registered route.
func (fox *Router) Len() int {
	tree := fox.getTree()
	return tree.size
}

// Iter returns a collection of range iterators for traversing registered methods and routes. It creates a
// point-in-time snapshot of the routing tree. Therefore, all iterators returned by Iter will not observe subsequent
// write on the router. This function is safe for concurrent use by multiple goroutine and while mutation on
// routes are ongoing.
func (fox *Router) Iter() Iter {
	tree := fox.getTree()
	return Iter{
		tree:     tree,
		patterns: tree.patterns,
		names:    tree.names,
		maxDepth: tree.maxDepth,
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

// RouterInfo returns information on the configured global option.
func (fox *Router) RouterInfo() RouterInfo {
	_, ok := fox.clientip.(noClientIPResolver)
	return RouterInfo{
		MaxRouteParams:        fox.maxParams,
		MaxRouteParamKeyBytes: fox.maxParamKeyBytes,
		MaxRouteMatchers:      fox.maxMatchers,
		MethodNotAllowed:      fox.handleMethodNotAllowed,
		AutoOptions:           fox.handleOPTIONS,
		TrailingSlashOption:   fox.handleSlash,
		FixedPathOption:       fox.handlePath,
		ClientIP:              !ok,
		AllowRegexp:           fox.allowRegexp,
		SystemWideOptions:     fox.systemWideOPTIONS,
	}
}

// Txn create a new read-write or read-only transaction. Each [Txn] must be finalized with [Txn.Commit] or [Txn.Abort].
// It's safe to create transaction from multiple goroutine and while the router is serving request.
// However, the returned [Txn] itself is NOT tread-safe.
// See also [Router.Updates] and [Router.View] for managed read-write and read-only transaction.
func (fox *Router) Txn(write bool) *Txn {
	if write {
		fox.mu.Lock()
	}

	return &Txn{
		fox:     fox,
		write:   write,
		rootTxn: fox.getTree().txn(),
	}
}

func (fox *Router) newTree() *iTree {
	tree := new(iTree)
	tree.fox = fox

	tree.patterns = make(root)
	tree.names = make(root)
	tree.pool = sync.Pool{
		New: func() any {
			return tree.allocateContext()
		},
	}
	return tree
}

// getTree load the tree atomically.
func (fox *Router) getTree() *iTree {
	r := fox.tree.Load()
	return r
}

// DefaultNotFoundHandler is a simple [HandlerFunc] that replies to each request
// with a “404 page not found” reply.
func DefaultNotFoundHandler(c *Context) {
	http.Error(c.Writer(), "404 page not found", http.StatusNotFound)
}

// DefaultMethodNotAllowedHandler is a simple [HandlerFunc] that replies to each request
// with a “405 Method Not Allowed” reply.
func DefaultMethodNotAllowedHandler(c *Context) {
	http.Error(c.Writer(), http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

// DefaultOptionsHandler is a simple [HandlerFunc] that replies to each request with a "200 OK" reply.
func DefaultOptionsHandler(c *Context) {
	c.Writer().WriteHeader(http.StatusNoContent)
}

func internalTrailingSlashHandler(c *Context) {
	req := c.Request()

	code := http.StatusMovedPermanently
	if req.Method != http.MethodGet {
		// Will be redirected only with the same method (SEO friendly)
		code = http.StatusPermanentRedirect
	}

	path := escapeLeadingSlashes(fixTrailingSlash(c.Path()))
	if q := req.URL.RawQuery; q != "" {
		path += "?" + q
	}

	http.Redirect(c.Writer(), req, path, code)
}

func internalFixedPathHandler(c *Context) {
	req := c.Request()

	code := http.StatusMovedPermanently
	if req.Method != http.MethodGet {
		// Will be redirected only with the same method (SEO friendly)
		code = http.StatusPermanentRedirect
	}

	cleanedPath := escapeLeadingSlashes(CleanPath(c.Path()))
	if q := req.URL.RawQuery; q != "" {
		cleanedPath += "?" + q
	}

	http.Redirect(c.Writer(), req, cleanedPath, code)
}

// ServeHTTP is the main entry point to serve a request. It handles all incoming HTTP requests and dispatches them
// to the appropriate handler function based on the request's method and path.
func (fox *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	var n *node
	var idx int

	tree := fox.getTree()
	c := tree.pool.Get().(*Context)
	c.reset(w, r)

	path := c.Path()

	idx, n = tree.lookup(r.Method, r.Host, path, c, false)
	if !c.tsr && n != nil {
		c.route = n.routes[idx]
		r.Pattern = c.route.pattern
		*c.paramsKeys = c.route.params
		c.route.hall(c)
		tree.pool.Put(c)
		return
	}

	if n == nil {
		*c.params = (*c.params)[:0]
		idx, n = tree.lookup(MethodAny, r.Host, path, c, false)
		if !c.tsr && n != nil {
			c.route = n.routes[idx]
			r.Pattern = c.route.pattern
			*c.paramsKeys = c.route.params
			c.route.hall(c)
			tree.pool.Put(c)
			return
		}
	}

	if r.Method != http.MethodConnect && r.URL.Path != "/" {
		if c.tsr && n != nil {
			route := n.routes[idx]
			if route.handleSlash == RelaxedSlash {
				c.route = route
				r.Pattern = c.route.pattern
				*c.paramsKeys = c.route.params
				route.hall(c)
				tree.pool.Put(c)
				return
			}

			if route.handleSlash == RedirectSlash {
				*c.params = (*c.params)[:0]
				c.tsr = false
				c.route = nil
				c.scope = RedirectSlashHandler
				fox.tsrRedirect(c)
				tree.pool.Put(c)
				return
			}
		}

		switch fox.handlePath {
		case RelaxedPath:
			*c.params = (*c.params)[:0]
			if idx, n := tree.lookup(r.Method, r.Host, CleanPath(path), c, false); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				c.route = n.routes[idx]
				r.Pattern = c.route.pattern
				*c.paramsKeys = c.route.params
				c.route.hall(c)
				tree.pool.Put(c)
				return
			}
			*c.params = (*c.params)[:0]
			if idx, n := tree.lookup(MethodAny, r.Host, CleanPath(path), c, false); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				c.route = n.routes[idx]
				r.Pattern = c.route.pattern
				*c.paramsKeys = c.route.params
				c.route.hall(c)
				tree.pool.Put(c)
				return
			}
		case RedirectPath:
			if idx, n := tree.lookup(r.Method, r.Host, CleanPath(path), c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash != StrictSlash) {
				*c.params = (*c.params)[:0]
				c.tsr = false
				c.route = nil
				c.scope = RedirectPathHandler
				fox.pathRedirect(c)
				tree.pool.Put(c)
				return
			}
			if idx, n := tree.lookup(MethodAny, r.Host, CleanPath(path), c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash != StrictSlash) {
				*c.params = (*c.params)[:0]
				c.tsr = false
				c.route = nil
				c.scope = RedirectPathHandler
				fox.pathRedirect(c)
				tree.pool.Put(c)
				return
			}
		default:
		}
	}

	*c.params = (*c.params)[:0]
	c.route = nil

	isOPTIONS := r.Method == http.MethodOptions

	// Handle system-wide OPTIONS, see https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods/OPTIONS.
	// Note that http.Server.DisableGeneralOptionsHandler should be disabled.
	if fox.systemWideOPTIONS && isOPTIONS && path == "*" {
		var sb strings.Builder
		sb.Grow(150)

		_, hasOPTIONS := tree.patterns[http.MethodOptions]
		mayHandleOPTIONS := fox.handleOPTIONS && len(tree.patterns) > 0

		for method := range tree.patterns {
			if method == http.MethodOptions || method == MethodAny {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(method)
		}

		// Include OPTIONS in Allow only if explicitly registered or if auto-OPTIONS is enabled
		// with at least one route. A server responding solely to OPTIONS * doesn't meaningfully
		// "support" OPTIONS for resource access.
		if hasOPTIONS || mayHandleOPTIONS {
			sb.WriteString(", ")
			sb.WriteString(http.MethodOptions)
		}

		if sb.Len() > 0 {
			w.Header().Set(HeaderAllow, sb.String())
		}
		w.WriteHeader(http.StatusOK)
		tree.pool.Put(c)
		return
	}

	if fox.handleOPTIONS && isOPTIONS {
		// A CORS request is an HTTP request that includes an `Origin` header: https://fetch.spec.whatwg.org/#cors-request
		// A CORS preflight request contains at most one ACRM header: https://fetch.spec.whatwg.org/#cors-preflight-fetch
		_, foundOrigin := firstHeader(r.Header, HeaderOrigin)
		acrm, foundAcrm := firstHeader(r.Header, HeaderAccessControlRequestMethod)
		if !foundOrigin || !foundAcrm {
			var sb strings.Builder
			sb.Grow(150)
			// Since different method and route may match (e.g. GET /foo/bar & POST /foo/{name}), we cannot set the path and params.
			for method := range tree.patterns {
				if method == MethodAny {
					continue
				}
				if idx, n := tree.lookup(method, r.Host, path, c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(method)
				}
			}

			if sb.Len() > 0 {
				sb.WriteString(", ")
				sb.WriteString(http.MethodOptions)
				w.Header().Set(HeaderAllow, sb.String())
				c.scope = OptionsHandler
				c.tsr = false
				fox.autoOPTIONS(c)
				tree.pool.Put(c)
				return
			}
		} else {
			// For CORS preflight, we only need to verify that the request match at least one route registered for any methods.
			// Unlike regular OPTIONS, we don't include an Allow header since it provides no functional benefit for the
			// preflight's purpose, and building it would require a full lookup across all registered methods.
			// The fast-path is to lookup directly for the ACRM, since in most case a preflight request should target an existing resource,
			// which avoid unnecessary lookup on method which does have a matching route registered.
			if idx, n := tree.lookup(acrm, r.Host, path, c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				c.scope = OptionsHandler
				c.tsr = false
				fox.autoOPTIONS(c)
				tree.pool.Put(c)
				return
			}

			// Else we search for a matching route for any method, but unlike regular OPTIONS, we stop at the first match.
			for method := range tree.patterns {
				if method == acrm || method == MethodAny {
					continue
				}
				if idx, n := tree.lookup(method, r.Host, path, c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
					c.scope = OptionsHandler
					c.tsr = false
					fox.autoOPTIONS(c)
					tree.pool.Put(c)
					return
				}
			}
		}
	} else if fox.handleMethodNotAllowed {
		var sb strings.Builder
		// Grow sb to a reasonable size that should prevent new allocation in most case.
		sb.Grow(150)
		hasOptions := false
		for method := range tree.patterns {
			if method != r.Method {
				if idx, n := tree.lookup(method, r.Host, path, c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					if method == http.MethodOptions {
						hasOptions = true
					}
					sb.WriteString(method)
				}
			}
		}
		if sb.Len() > 0 {
			if fox.handleOPTIONS && !hasOptions {
				sb.WriteString(", ")
				sb.WriteString(http.MethodOptions)
			}
			w.Header().Set(HeaderAllow, sb.String())
			c.scope = NoMethodHandler
			c.tsr = false
			fox.noMethod(c)
			tree.pool.Put(c)
			return
		}
	}

	c.tsr = false
	c.scope = NoRouteHandler
	fox.noRoute(c)
	tree.pool.Put(c)
}

func (fox *Router) serveSubRouter(c *Context, path string) {
	var n *node
	var idx int

	tree := c.tree
	r := c.Request()
	w := c.Writer()

	paramsOffset := len(*c.params)
	idx, n = tree.lookupByPath(r.Method, path, c, false)
	if !c.tsr && n != nil {
		c.route = n.routes[idx]
		*c.paramsKeys = append(*c.paramsKeys, c.route.params...)
		r.Pattern += c.route.pattern[1:]
		c.route.hall(c)
		return
	}

	if n == nil {
		*c.params = (*c.params)[:paramsOffset]
		// Since we are already in a SubRouter, there is no point to lookup for a regular hostname.
		idx, n = tree.lookupByPath(MethodAny, path, c, false)
		if !c.tsr && n != nil {
			c.route = n.routes[idx]
			*c.paramsKeys = append(*c.paramsKeys, c.route.params...)
			r.Pattern += c.route.pattern[1:]
			c.route.hall(c)
			return
		}
	}

	if r.Method != http.MethodConnect && r.URL.Path != "/" {
		if c.tsr && n != nil {
			route := n.routes[idx]
			if route.handleSlash == RelaxedSlash {
				c.route = route
				*c.paramsKeys = append(*c.paramsKeys, route.params...)
				r.Pattern += c.route.pattern[1:]
				route.hall(c)
				return
			}

			if route.handleSlash == RedirectSlash {
				*c.params = (*c.params)[:0]
				c.tsr = false
				c.route = nil
				c.scope = RedirectSlashHandler
				fox.tsrRedirect(c)
				return
			}
		}

		switch fox.handlePath {
		case RelaxedPath:
			*c.params = (*c.params)[:paramsOffset]
			if idx, n := tree.lookupByPath(r.Method, CleanPath(path), c, false); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				c.route = n.routes[idx]
				*c.paramsKeys = append(*c.paramsKeys, c.route.params...)
				r.Pattern += c.route.pattern[1:]
				c.route.hall(c)
				return
			}
			*c.params = (*c.params)[:paramsOffset]
			if idx, n := tree.lookupByPath(MethodAny, CleanPath(path), c, false); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				c.route = n.routes[idx]
				*c.paramsKeys = append(*c.paramsKeys, c.route.params...)
				r.Pattern += c.route.pattern[1:]
				c.route.hall(c)
				return
			}
		case RedirectPath:
			if idx, n := tree.lookupByPath(r.Method, CleanPath(path), c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash != StrictSlash) {
				*c.params = (*c.params)[:0]
				c.tsr = false
				c.route = nil
				c.scope = RedirectPathHandler
				fox.pathRedirect(c)
				return
			}
			if idx, n := tree.lookupByPath(MethodAny, CleanPath(path), c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash != StrictSlash) {
				*c.params = (*c.params)[:0]
				c.tsr = false
				c.route = nil
				c.scope = RedirectPathHandler
				fox.pathRedirect(c)
				return
			}
		default:
		}
	}

	*c.params = (*c.params)[:0]
	c.route = nil

	isOPTIONS := r.Method == http.MethodOptions

	if fox.handleOPTIONS && isOPTIONS {
		// A CORS request is an HTTP request that includes an `Origin` header: https://fetch.spec.whatwg.org/#cors-request
		// A CORS preflight request contains at most one ACRM header: https://fetch.spec.whatwg.org/#cors-preflight-fetch
		_, foundOrigin := firstHeader(r.Header, HeaderOrigin)
		acrm, foundAcrm := firstHeader(r.Header, HeaderAccessControlRequestMethod)
		if !foundOrigin || !foundAcrm {
			var sb strings.Builder
			sb.Grow(150)
			// Since different method and route may match (e.g. GET /foo/bar & POST /foo/{name}), we cannot set the path and params.
			for method := range tree.patterns {
				if idx, n := tree.lookupByPath(method, path, c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(method)
				}
			}

			if sb.Len() > 0 {
				sb.WriteString(", ")
				sb.WriteString(http.MethodOptions)
				w.Header().Set(HeaderAllow, sb.String())
				c.scope = OptionsHandler
				c.tsr = false
				fox.autoOPTIONS(c)
				return
			}
		} else {
			// For CORS preflight, we only need to verify that the request match at least one route registered for any methods.
			// Unlike regular OPTIONS, we don't include an Allow header since it provides no functional benefit for the
			// preflight's purpose, and building it would require a full lookup across all registered methods.
			// The fast-path is to lookup directly for the ACRM, since in most case a preflight request should target an existing resource,
			// which avoid unnecessary lookup on method which does have a matching route registered.
			if idx, n := tree.lookupByPath(acrm, path, c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				c.scope = OptionsHandler
				c.tsr = false
				fox.autoOPTIONS(c)
				return
			}

			// Else we search for a matching route for any method, but unlike regular OPTIONS, we stop at the first match.
			for method := range tree.patterns {
				if method == acrm {
					continue
				}
				if idx, n := tree.lookupByPath(method, path, c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
					c.scope = OptionsHandler
					c.tsr = false
					fox.autoOPTIONS(c)
					return
				}
			}
		}
	} else if fox.handleMethodNotAllowed {
		var sb strings.Builder
		// Grow sb to a reasonable size that should prevent new allocation in most case.
		sb.Grow(150)
		hasOptions := false
		for method := range tree.patterns {
			if method != r.Method {
				if idx, n := tree.lookupByPath(method, path, c, true); n != nil && (!c.tsr || n.routes[idx].handleSlash == RelaxedSlash) {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					if method == http.MethodOptions {
						hasOptions = true
					}
					sb.WriteString(method)
				}
			}
		}
		if sb.Len() > 0 {
			if fox.handleOPTIONS && !hasOptions {
				sb.WriteString(", ")
				sb.WriteString(http.MethodOptions)
			}
			w.Header().Set(HeaderAllow, sb.String())
			c.scope = NoMethodHandler
			c.tsr = false
			fox.noMethod(c)
			return
		}
	}

	c.tsr = false
	c.scope = NoRouteHandler
	fox.noRoute(c)
}

const (
	stateDefault uint8 = iota
	stateParam
	stateCatchAll
	stateRegex
)

type parsedRoute struct {
	token         []token
	paramCnt      int
	endHost       int
	startCatchAll int
}

// parseRoute parse and validate the route in a single pass.
func (fox *Router) parseRoute(url string) (parsedRoute, error) {

	endHost := strings.IndexByte(url, '/')
	if endHost == -1 {
		return parsedRoute{}, fmt.Errorf("%w: missing trailing '/' after hostname", ErrInvalidRoute)
	}
	if strings.HasPrefix(url, ".") {
		return parsedRoute{}, fmt.Errorf("%w: illegal leading '.' in hostname label", ErrInvalidRoute)
	}
	if strings.HasPrefix(url, "-") {
		return parsedRoute{}, fmt.Errorf("%w: illegal leading '-' in hostname label", ErrInvalidRoute)
	}

	var delim byte
	if endHost == 0 {
		delim = slashDelim
	} else {
		delim = dotDelim
	}

	state := stateDefault
	previous := stateDefault
	paramCnt := 0
	countStatic := 2
	startParam := 0
	inParam := false
	nonNumeric := false // true once we've seen a letter or hyphen
	startCatchAll := 0  // start index of +{foo} or *{foo}
	partlen := 0
	totallen := 0
	last := dotDelim
	tokens := make([]token, 0, 1) // At least one segment
	sb := strings.Builder{}

	i := 0
	for i < len(url) {
		switch state {
		case stateParam:
			if url[i] == '}' {
				if !inParam {
					return parsedRoute{}, fmt.Errorf("%w: missing parameter name between '{}'", ErrInvalidRoute)
				}
				inParam = false

				if i+1 < len(url) && url[i+1] != delim && url[i+1] != '/' {
					return parsedRoute{}, fmt.Errorf("%w: illegal character '%s' after '{param}'", ErrInvalidRoute, string(url[i+1]))
				}

				if i < endHost {
					nonNumeric = true
				}

				if previous != stateRegex {
					tokens = append(tokens, token{
						typ:   nodeParam,
						value: url[startParam+1 : i],
					})
				}

				countStatic = 1
				previous = state
				state = stateDefault
				i++
				continue
			}

			if url[i] == ':' {
				previous = state
				state = stateRegex
				i++
				continue
			}

			if i-startParam > fox.maxParamKeyBytes {
				return parsedRoute{}, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrParamKeyTooLarge)
			}

			if url[i] == delim || url[i] == '/' || url[i] == '*' || url[i] == '+' || url[i] == '{' {
				return parsedRoute{}, fmt.Errorf("%w: illegal character '%s' in '{param}'", ErrInvalidRoute, string(url[i]))
			}
			inParam = true
			i++
		case stateCatchAll:
			if url[i] == '}' {
				if !inParam {
					return parsedRoute{}, fmt.Errorf("%w: missing parameter name between '%c{}'", ErrInvalidRoute, url[startCatchAll])
				}
				inParam = false

				if i+1 < len(url) && url[i+1] != delim && url[i+1] != '/' {
					return parsedRoute{}, fmt.Errorf("%w: illegal character '%s' after '%c{param}'", ErrInvalidRoute, string(url[i+1]), url[startCatchAll])
				}

				if previous == stateCatchAll && countStatic <= 1 {
					return parsedRoute{}, fmt.Errorf("%w: consecutive wildcard not allowed", ErrInvalidRoute)
				}

				if i < len(url)-1 {
					if url[startCatchAll] == '+' {
						return parsedRoute{}, fmt.Errorf("%w: '+{param}' allowed only as suffix", ErrInvalidRoute)
					}
					// reset
					startCatchAll = 0
				}

				if i < endHost {
					nonNumeric = true
				}

				if previous != stateRegex {
					tokens = append(tokens, token{
						typ:   nodeWildcard,
						value: url[startParam+1 : i],
					})
				}

				countStatic = 0
				previous = state
				state = stateDefault
				i++
				continue
			}

			if url[i] == ':' {
				previous = state
				state = stateRegex
				i++
				continue
			}

			if i-startParam > fox.maxParamKeyBytes {
				return parsedRoute{}, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrParamKeyTooLarge)
			}

			if url[i] == delim || url[i] == '/' || url[i] == '*' || url[i] == '+' || url[i] == '{' {
				return parsedRoute{}, fmt.Errorf("%w: illegal character '%s' in '%c{param}'", ErrInvalidRoute, string(url[i]), url[startCatchAll])
			}
			inParam = true
			i++
		case stateRegex:
			if !fox.allowRegexp {
				return parsedRoute{}, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrRegexpNotAllowed)
			}
			if previous == stateCatchAll && countStatic <= 1 {
				return parsedRoute{}, fmt.Errorf("%w: consecutive wildcard not allowed", ErrInvalidRoute)
			}

			idx := braceIndice(url[i:], 1)
			if idx == -1 {
				return parsedRoute{}, fmt.Errorf("%w: unbalanced braces in regular expression", ErrInvalidRoute)
			}
			if idx == 0 {
				return parsedRoute{}, fmt.Errorf("%w: missing regular expression", ErrInvalidRoute)
			}

			pattern := url[i : i+idx]
			re, err := regexp.Compile("^" + pattern + "$")
			if err != nil {
				return parsedRoute{}, fmt.Errorf("%w: %w", ErrInvalidRoute, err)
			}

			if re.NumSubexp() > 0 {
				return parsedRoute{}, fmt.Errorf("%w: illegal capture group '%s': use (?:pattern) instead", ErrInvalidRoute, pattern)
			}

			typ := nodeWildcard
			if previous == stateParam {
				typ = nodeParam
			}

			tokens = append(tokens, token{
				typ:    typ,
				value:  url[startParam+1 : i-1],
				regexp: re,
			})

			// restore
			state, previous = previous, state
			i += idx

		default:

			if i == endHost {
				if sb.Len() > 0 {
					tokens = append(tokens, token{
						typ:    nodeStatic,
						value:  sb.String(),
						hsplit: true,
					})
					sb.Reset()
				}
				delim = slashDelim
				countStatic = 2 // reset
			}

			switch url[i] {
			case '{':
				if sb.Len() > 0 {
					tokens = append(tokens, token{
						typ:    nodeStatic,
						value:  sb.String(),
						hsplit: i < endHost,
					})
					sb.Reset()
				}
				state = stateParam
				startParam = i
				paramCnt++
			case '*', '+':
				if sb.Len() > 0 {
					tokens = append(tokens, token{
						typ:    nodeStatic,
						value:  sb.String(),
						hsplit: i < endHost,
					})
					sb.Reset()
				}
				state = stateCatchAll
				startCatchAll = i
				i++
				if i < len(url) && url[i] != '{' {
					return parsedRoute{}, fmt.Errorf("%w: missing '{param}' after '%c' catch-all delimiter", ErrInvalidRoute, url[startCatchAll])
				}
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
							return parsedRoute{}, fmt.Errorf("%w: illegal '-' after '.' in hostname label", ErrInvalidRoute)
						}
						partlen++
						nonNumeric = true
					case c == '.':
						// Byte before dot cannot be dot.
						if last == '.' && url[i-1] != '}' {
							return parsedRoute{}, fmt.Errorf("%w: unexpected consecutive '.' in hostname", ErrInvalidRoute)
						}
						// Byte before dot cannot be dash.
						if last == '-' {
							return parsedRoute{}, fmt.Errorf("%w: illegal '-' before '.' in hostname label", ErrInvalidRoute)
						}
						if partlen > 63 {
							return parsedRoute{}, fmt.Errorf("%w: hostname label exceed 63 characters", ErrInvalidRoute)
						}
						totallen += partlen + 1 // +1 count the current dot
						partlen = 0
					case 'A' <= c && c <= 'Z':
						return parsedRoute{}, fmt.Errorf("%w: illegal uppercase character '%s' in hostname label", ErrInvalidRoute, string(c))
					default:
						return parsedRoute{}, fmt.Errorf("%w: illegal character '%s' in hostname label", ErrInvalidRoute, string(c))
					}
					last = c
				} else {
					c := url[i]
					// reject any ASCII control character.
					if c < ' ' || c == 0x7f {
						return parsedRoute{}, fmt.Errorf("%w: illegal control character in path", ErrInvalidRoute)
					}

					// reject any consecutive slash
					if i > endHost && c == '/' && url[i-1] == '/' {
						return parsedRoute{}, fmt.Errorf("%w: illegal consecutive slashes in path", ErrInvalidRoute)
					}

					// reject dot-based traversal patterns
					if i > endHost && c == '.' && url[i-1] == '/' {
						nextIdx := i + 1
						if nextIdx < len(url) {
							nextChar := url[nextIdx]
							switch nextChar {
							case '/':
								return parsedRoute{}, fmt.Errorf("%w: illegal path traversal pattern '/./'", ErrInvalidRoute)
							case '.':
								nextNextIdx := nextIdx + 1
								if nextNextIdx < len(url) {
									if url[nextNextIdx] == '/' {
										return parsedRoute{}, fmt.Errorf("%w: illegal path traversal pattern '/../'", ErrInvalidRoute)
									}
								} else {
									return parsedRoute{}, fmt.Errorf("%w: illegal path traversal pattern '/..' at end", ErrInvalidRoute)
								}
							}
						} else {
							return parsedRoute{}, fmt.Errorf("%w: illegal path traversal pattern '/.' at end", ErrInvalidRoute)
						}
					}
				}
			}

			if paramCnt > fox.maxParams {
				return parsedRoute{}, fmt.Errorf("%w: %w", ErrInvalidRoute, ErrTooManyParams)
			}
			i++
		}
	}

	if endHost > 0 {
		totallen += partlen
		if last == '-' {
			return parsedRoute{}, fmt.Errorf("%w: illegal trailing '-' in hostname label", ErrInvalidRoute)
		}
		if url[endHost-1] == '.' {
			return parsedRoute{}, fmt.Errorf("%w: illegal trailing '.' in hostname label", ErrInvalidRoute)
		}
		if !nonNumeric {
			return parsedRoute{}, fmt.Errorf("%w: invalid all numeric hostname", ErrInvalidRoute)
		}
		if partlen > 63 {
			return parsedRoute{}, fmt.Errorf("%w: hostname label exceed 63 characters", ErrInvalidRoute)
		}
		if totallen > 255 {
			return parsedRoute{}, fmt.Errorf("%w: hostname exceed 255 characters", ErrInvalidRoute)
		}
	}

	if state == stateParam {
		return parsedRoute{}, fmt.Errorf("%w: unclosed '{param}'", ErrInvalidRoute)
	}

	if state == stateCatchAll {
		prev := len(url) - 1
		if url[prev] == '*' || url[prev] == '+' {
			return parsedRoute{}, fmt.Errorf("%w: missing '{param}' after '%c' catch-all delimiter", ErrInvalidRoute, url[prev])
		}
		return parsedRoute{}, fmt.Errorf("%w: unclosed '%c{param}'", ErrInvalidRoute, url[prev])
	}

	if sb.Len() > 0 {
		tokens = append(tokens, token{
			typ:   nodeStatic,
			value: sb.String(),
		})
	}

	return parsedRoute{
		token:         tokens,
		paramCnt:      paramCnt,
		endHost:       endHost,
		startCatchAll: startCatchAll,
	}, nil
}

// braceIndices returns the index of the closing brace that balances an opening
// brace. It starts at startLevel opened brace.
//
// Example: For pattern "{id:[0-9]{1,3}}", the caller would pass "[0-9]{1,3}}" and 1
// (everything after the initial '{'), and this returns 10 (index of the final '}').
func braceIndice(s string, startLevel int) int {
	level := startLevel

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			level++
		case '}':
			if level--; level == 0 {
				return i
			}
		}
	}
	return -1
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

func (s noClientIPResolver) ClientIP(_ RequestContext) (*net.IPAddr, error) {
	return nil, ErrNoClientIPResolver
}

// firstHeader returns the first value and true if k is present in headers. It assumes that k is in canonical
// format (see [http.CanonicalHeaderKey]).
func firstHeader(headers http.Header, k string) (string, bool) {
	v, found := headers[k]
	if !found || len(v) == 0 {
		return "", false
	}
	return v[0], true
}
