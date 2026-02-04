// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/fox-toolkit/fox/internal/slicesutil"
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
// github.com/fox-toolkit/fox/clientip package.
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
	// AllHandlers is a combination of all the above scopes, which can be used to apply middlewares to all types of handlers.
	AllHandlers = RouteHandler | NoRouteHandler | NoMethodHandler | RedirectSlashHandler | RedirectPathHandler | OptionsHandler
)

// Router is a lightweight high performance HTTP request router that support mutation on its routing tree
// while handling request concurrently.
type Router struct {
	clientip               ClientIPResolver
	noRouteBase            HandlerFunc
	noRoute                HandlerFunc
	noMethod               HandlerFunc
	tsrRedirect            HandlerFunc
	pathRedirect           HandlerFunc
	autoOPTIONS            HandlerFunc
	tree                   atomic.Pointer[iTree]
	mws                    []middleware
	maxParams              int
	maxParamKeyBytes       int
	maxMatchers            int
	mu                     sync.Mutex
	handleSlash            TrailingSlashOption
	handlePath             FixedPathOption
	handleMethodNotAllowed bool
	handleOPTIONS          bool
	systemWideOPTIONS      bool
	allowRegexp            bool
}

func initRouter() *Router {
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
	return r
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

// MustRouter returns a ready to use instance of Fox router.
// This function is a convenience wrapper for [NewRouter] and panics on error.
func MustRouter(opts ...GlobalOption) *Router {
	f, err := NewRouter(opts...)
	if err != nil {
		panic(err)
	}
	return f
}

// NewRouter returns a ready to use instance of Fox router.
func NewRouter(opts ...GlobalOption) (*Router, error) {
	router := initRouter()

	for _, opt := range opts {
		if err := opt.applyGlob(sealedOption{router: router}); err != nil {
			return nil, err
		}
	}

	router.noRoute = applyMiddleware(NoRouteHandler, router.mws, router.noRouteBase)
	router.noMethod = applyMiddleware(NoMethodHandler, router.mws, router.noMethod)
	router.tsrRedirect = applyMiddleware(RedirectSlashHandler, router.mws, router.tsrRedirect)
	router.pathRedirect = applyMiddleware(RedirectPathHandler, router.mws, router.pathRedirect)
	router.autoOPTIONS = applyMiddleware(OptionsHandler, router.mws, router.autoOPTIONS)

	router.tree.Store(router.newTree())
	return router, nil
}

// MustAdd registers a new route for the given methods, pattern and matchers. On success, it returns the newly registered [Route].
// This function is a convenience wrapper for the [Router.Add] function and panics on error.
func (fox *Router) MustAdd(methods []string, pattern string, handler HandlerFunc, opts ...RouteOption) *Route {
	rte, err := fox.Add(methods, pattern, handler, opts...)
	if err != nil {
		panic(err)
	}
	return rte
}

// Add registers a new route for the given methods, pattern and matchers. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteConflict]: If the route conflict with others.
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//
// It's safe to add a new handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To override an existing handler, use [Router.Update].
func (fox *Router) Add(methods []string, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	txn := fox.Txn(true)
	defer txn.Abort()
	rte, err := txn.Add(methods, pattern, handler, opts...)
	if err != nil {
		return nil, err
	}
	txn.Commit()
	return rte, nil
}

// AddRoute registers a new [Route]. If an error occurs, it returns one of the following:
//   - [ErrRouteConflict]: If the route conflict with others.
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the route is missing.
//
// It's safe to add a new route while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To override an existing route, use [Router.UpdateRoute].
func (fox *Router) AddRoute(route *Route) error {
	txn := fox.Txn(true)
	defer txn.Abort()
	if err := txn.AddRoute(route); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Update override an existing route for the given methods, pattern and matchers. On success, it returns the newly registered [Route].
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
// multiple goroutine. To add new handler, use [Router.Add] method.
func (fox *Router) Update(methods []string, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	txn := fox.Txn(true)
	defer txn.Abort()
	rte, err := txn.Update(methods, pattern, handler, opts...)
	if err != nil {
		return nil, err
	}
	txn.Commit()
	return rte, nil
}

// UpdateRoute override an existing [Route] for the given new [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrRouteNameExist]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the route is missing.
//
// It's safe to update a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To add new route, use [Router.AddRoute] method.
func (fox *Router) UpdateRoute(route *Route) error {
	txn := fox.Txn(true)
	defer txn.Abort()
	if err := txn.UpdateRoute(route); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Delete deletes an existing route for the given methods, pattern and matchers. On success, it returns the deleted [Route].
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
//
// It's safe to delete a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine.
func (fox *Router) Delete(methods []string, pattern string, opts ...MatcherOption) (*Route, error) {
	txn := fox.Txn(true)
	defer txn.Abort()
	route, err := txn.Delete(methods, pattern, opts...)
	if err != nil {
		return nil, err
	}
	txn.Commit()
	return route, nil
}

// DeleteRoute deletes an existing route that match the provided [Route] pattern and matchers. On success, it returns
// the deleted [Route]. If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the route is missing.
//
// It's safe to delete a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine.
func (fox *Router) DeleteRoute(route *Route) (*Route, error) {
	txn := fox.Txn(true)
	defer txn.Abort()
	route, err := txn.DeleteRoute(route)
	if err != nil {
		return nil, err
	}
	txn.Commit()
	return route, nil
}

// Has allows to check if the given methods, pattern and matchers exactly match a registered route. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing. See also [Router.Route] as an alternative.
func (fox *Router) Has(methods []string, pattern string, matchers ...Matcher) bool {
	return fox.Route(methods, pattern, matchers...) != nil
}

// Route performs a lookup for a registered route matching the given methods, pattern and matchers. It returns the [Route] if a
// match is found or nil otherwise. This function is safe for concurrent use by multiple goroutine and while
// mutation on route are ongoing. See also [Router.Has] or [Iter.Routes] as an alternative.
func (fox *Router) Route(methods []string, pattern string, matchers ...Matcher) *Route {
	tree := fox.getTree()

	root := tree.patterns
	matched := root.searchPattern(pattern)
	if matched == nil || !matched.isLeaf() {
		return nil
	}
	idx := slices.IndexFunc(matched.routes, func(r *Route) bool {
		return r.pattern == pattern && slicesutil.EqualUnsorted(r.methods, methods) && r.matchersEqual(matchers)
	})
	if idx < 0 {
		return nil
	}
	return matched.routes[idx]
}

// Name performs a lookup for a registered route matching the given method and route name. It returns
// the [Route] if a match is found or nil otherwise. This function is safe for concurrent use by multiple
// goroutines and while mutations on routes are ongoing. See also [Router.Route] as an alternative.
func (fox *Router) Name(name string) *Route {
	tree := fox.getTree()

	root := tree.names
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

	idx, n, tsr := tree.lookup(method, r.Host, path, c, true)
	if n != nil {
		return n.routes[idx], tsr
	}
	return
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

	idx, n, tsr := tree.lookup(r.Method, r.Host, path, c, false)
	if n != nil {
		c.route = n.routes[idx]
		c.pattern = c.route.pattern
		*c.paramsKeys = c.route.params
		return c.route, c, tsr
	}

	tree.pool.Put(c)
	return
}

// NewRoute create a new [Route], configured with the provided options.
// If an error occurs, it returns one of the following:
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//   - [ErrInvalidConfig]: If the provided route options are invalid.
//   - [ErrInvalidMatcher]: If the provided matcher options are invalid.
func (fox *Router) NewRoute(methods []string, pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, error) {
	if handler == nil {
		return nil, fmt.Errorf("%w: nil handler", ErrInvalidRoute)
	}

	for _, method := range methods {
		if !validMethod(method) {
			return nil, fmt.Errorf("%w: invalid method '%s'", ErrInvalidRoute, method)
		}
	}

	parsed, err := fox.parseRoute(pattern)
	if err != nil {
		return nil, err
	}

	rte := &Route{
		clientip:    fox.clientip,
		hbase:       handler,
		pattern:     pattern,
		handleSlash: fox.handleSlash,
		hostEnd:     parsed.endHost,
		tokens:      parsed.token,
		catchEmpty:  parsed.startCatchAll > 0 && pattern[parsed.startCatchAll] == starDelim,
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
		return nil, fmt.Errorf("%w: %s", ErrInvalidRoute, "priority requires matchers")
	}

	rte.priority = cmp.Or(rte.priority, uint(len(rte.matchers)))
	rte.hself, rte.hall = applyRouteMiddleware(append(fox.mws, rte.mws...), handler)

	if len(methods) > 0 {
		// As a defensive mesure, keep our own copy of the provided slice.
		rte.methods = make([]string, len(methods))
		copy(rte.methods, methods)
		slices.Sort(rte.methods)
		rte.methods = slices.Compact(rte.methods)
	}

	return rte, nil
}

// HandleNoRoute calls the no route handler with the provided [Context].
// Note that this bypasses any middleware attached to the no route handler.
func (fox *Router) HandleNoRoute(c *Context) {
	if c.scope == NoRouteHandler {
		caller := relevantCaller()
		log.Printf("fox: recursive call to router.HandleNoRoute from %s (%s:%d)", caller.Function, path.Base(caller.File), caller.Line)
		return
	}
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
		methods:  tree.methods,
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
	tree := &iTree{
		fox:      fox,
		patterns: new(node),
		names:    new(node),
		methods:  make(map[string]uint),
	}
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

// ServeHTTP is the main entry point to serve a request. It handles all incoming HTTP requests and dispatches them
// to the appropriate handler function based on the request's method and path.
func (fox *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	tree := fox.getTree()
	c := tree.pool.Get().(*Context)
	c.reset(w, r)

	path := c.Path()

	idx, n, tsr := tree.lookup(r.Method, r.Host, path, c, false)
	if !tsr && n != nil {
		c.route = n.routes[idx]
		c.pattern = c.route.pattern
		*c.paramsKeys = c.route.params
		c.route.hall(c)
		tree.pool.Put(c)
		return
	}

	if r.Method != http.MethodConnect && r.URL.Path != "/" {
		if tsr && n != nil {
			route := n.routes[idx]
			if route.handleSlash == RelaxedSlash {
				c.route = route
				c.pattern = c.route.pattern
				*c.paramsKeys = c.route.params
				route.hall(c)
				tree.pool.Put(c)
				return
			}

			if route.handleSlash == RedirectSlash {
				*c.params = (*c.params)[:0]
				c.route = nil
				c.pattern = ""
				c.scope = RedirectSlashHandler
				fox.tsrRedirect(c)
				tree.pool.Put(c)
				return
			}
		}

		switch fox.handlePath {
		case RelaxedPath:
			*c.params = (*c.params)[:0]
			if idx, n, tsr := tree.lookup(r.Method, r.Host, CleanPath(path), c, false); n != nil && (!tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				c.route = n.routes[idx]
				c.pattern = c.route.pattern
				*c.paramsKeys = c.route.params
				c.route.hall(c)
				tree.pool.Put(c)
				return
			}
		case RedirectPath:
			if idx, n, tsr := tree.lookup(r.Method, r.Host, CleanPath(path), c, true); n != nil && (!tsr || n.routes[idx].handleSlash != StrictSlash) {
				*c.params = (*c.params)[:0]
				c.route = nil
				c.pattern = ""
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
	c.pattern = ""

	isOPTIONS := r.Method == http.MethodOptions

	// Add system-wide OPTIONS, see https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods/OPTIONS.
	// Note that http.Server.DisableGeneralOptionsHandler should be disabled.
	if fox.systemWideOPTIONS && isOPTIONS && path == "*" {
		var sb strings.Builder
		sb.Grow(150)

		_, hasOPTIONS := tree.methods[http.MethodOptions]
		mayHandleOPTIONS := fox.handleOPTIONS && len(tree.methods) > 0

		for method := range tree.methods {
			if method == http.MethodOptions {
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
		_, foundAcrm := firstHeader(r.Header, HeaderAccessControlRequestMethod)

		// A CORS-preflight request is a CORS request that checks to see if the CORS protocol is understood. Preflight should not enforce resource
		// validation (e.g., 404 or 405). The best practice is to let the actual request fail later. Note that if CORS is only enabled
		// for specific API segments, user can use a sub-router to apply CORS middleware to the relevant subtree.
		// See https://stackoverflow.com/questions/64352697/should-a-server-implementing-cors-always-reply-with-a-2xx-code-for-options-metho
		if foundOrigin && foundAcrm {
			c.scope = OptionsHandler
			fox.autoOPTIONS(c)
			tree.pool.Put(c)
			return
		}

		// Since different method and route may match (e.g. GET /foo/bar & POST /foo/{name}), we cannot set the path and params.
		seen := make(map[string]struct{})
		for method := range tree.methods {
			if _, ok := seen[method]; ok {
				continue
			}
			if idx, n, tsr := tree.lookup(method, r.Host, path, c, true); n != nil && (!tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				for _, m := range n.routes[idx].methods {
					seen[m] = struct{}{}
				}
			}
		}

		if len(seen) > 0 {
			var sb strings.Builder
			sb.Grow(150)
			sb.WriteString(http.MethodOptions)
			for method := range seen {
				sb.WriteString(", ")
				sb.WriteString(method)
			}
			w.Header().Set(HeaderAllow, sb.String())
			c.scope = OptionsHandler
			fox.autoOPTIONS(c)
			tree.pool.Put(c)
			return
		}
	} else if fox.handleMethodNotAllowed {

		seen := make(map[string]struct{})
		seen[r.Method] = struct{}{}

		for method := range tree.methods {
			if _, ok := seen[method]; ok {
				continue
			}
			if idx, n, tsr := tree.lookup(method, r.Host, path, c, true); n != nil && (!tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				for _, m := range n.routes[idx].methods {
					seen[m] = struct{}{}
				}
			}
		}

		if len(seen) > 1 {
			var sb strings.Builder
			sb.Grow(150)

			for method := range seen {
				if method == r.Method {
					continue
				}
				if sb.Len() > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(method)
			}

			if _, ok := seen[http.MethodOptions]; !ok && fox.handleOPTIONS {
				if sb.Len() > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(http.MethodOptions)
			}

			w.Header().Set(HeaderAllow, sb.String())
			c.scope = NoMethodHandler
			fox.noMethod(c)
			tree.pool.Put(c)
			return
		}
	}

	c.scope = NoRouteHandler
	fox.noRoute(c)
	tree.pool.Put(c)
}

func (fox *Router) serveSubRouter(c *Context, path string) {

	tree := c.tree
	r := c.Request()
	w := c.Writer()

	paramsOffset := len(*c.params)
	idx, n, tsr := tree.lookupByPath(r.Method, path, c, false)
	if !tsr && n != nil {
		c.route = n.routes[idx]
		*c.paramsKeys = append(*c.paramsKeys, c.route.params...)
		c.pattern = c.route.pattern
		c.route.hall(c)
		return
	}

	if r.Method != http.MethodConnect && r.URL.Path != "/" {
		if tsr && n != nil {
			route := n.routes[idx]
			if route.handleSlash == RelaxedSlash {
				c.route = route
				*c.paramsKeys = append(*c.paramsKeys, route.params...)
				c.pattern = c.route.pattern
				route.hall(c)
				return
			}

			if route.handleSlash == RedirectSlash {
				*c.params = (*c.params)[:0]
				c.route = nil
				c.pattern = ""
				c.scope = RedirectSlashHandler
				fox.tsrRedirect(c)
				return
			}
		}

		switch fox.handlePath {
		case RelaxedPath:
			*c.params = (*c.params)[:paramsOffset]
			if idx, n, tsr := tree.lookupByPath(r.Method, CleanPath(path), c, false); n != nil && (!tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				c.route = n.routes[idx]
				*c.paramsKeys = append(*c.paramsKeys, c.route.params...)
				c.pattern = c.route.pattern
				c.route.hall(c)
				return
			}
		case RedirectPath:
			if idx, n, tsr := tree.lookupByPath(r.Method, CleanPath(path), c, true); n != nil && (!tsr || n.routes[idx].handleSlash != StrictSlash) {
				*c.params = (*c.params)[:0]
				c.route = nil
				c.pattern = ""
				c.scope = RedirectPathHandler
				fox.pathRedirect(c)
				return
			}
		default:
		}
	}

	*c.params = (*c.params)[:0]
	c.route = nil
	c.pattern = ""

	isOPTIONS := r.Method == http.MethodOptions

	if fox.handleOPTIONS && isOPTIONS {
		// A CORS request is an HTTP request that includes an `Origin` header: https://fetch.spec.whatwg.org/#cors-request
		// A CORS preflight request contains at most one ACRM header: https://fetch.spec.whatwg.org/#cors-preflight-fetch
		_, foundOrigin := firstHeader(r.Header, HeaderOrigin)
		_, foundAcrm := firstHeader(r.Header, HeaderAccessControlRequestMethod)

		// A CORS-preflight request is a CORS request that checks to see if the CORS protocol is understood. Preflight should not enforce resource
		// validation (e.g., 404 or 405). The best practice is to let the actual request fail later. Note that if CORS is only enabled
		// for specific API segments, user can use a sub-router to apply CORS middleware to the relevant subtree.
		// See https://stackoverflow.com/questions/64352697/should-a-server-implementing-cors-always-reply-with-a-2xx-code-for-options-metho
		if foundOrigin && foundAcrm {
			c.scope = OptionsHandler
			fox.autoOPTIONS(c)
			tree.pool.Put(c)
			return
		}

		// Since different method and route may match (e.g. GET /foo/bar & POST /foo/{name}), we cannot set the path and params.
		seen := make(map[string]struct{})
		for method := range tree.methods {
			if _, ok := seen[method]; ok {
				continue
			}
			if idx, n, tsr := tree.lookupByPath(method, path, c, true); n != nil && (!tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				for _, m := range n.routes[idx].methods {
					seen[m] = struct{}{}
				}
			}
		}

		if len(seen) > 0 {
			var sb strings.Builder
			sb.Grow(150)
			sb.WriteString(http.MethodOptions)
			for method := range seen {
				sb.WriteString(", ")
				sb.WriteString(method)
			}
			w.Header().Set(HeaderAllow, sb.String())
			c.scope = OptionsHandler
			fox.autoOPTIONS(c)
			tree.pool.Put(c)
			return
		}
	} else if fox.handleMethodNotAllowed {

		seen := make(map[string]struct{})
		seen[r.Method] = struct{}{}

		for method := range tree.methods {
			if _, ok := seen[method]; ok {
				continue
			}
			if idx, n, tsr := tree.lookupByPath(method, path, c, true); n != nil && (!tsr || n.routes[idx].handleSlash == RelaxedSlash) {
				for _, m := range n.routes[idx].methods {
					seen[m] = struct{}{}
				}
			}
		}

		if len(seen) > 1 {
			var sb strings.Builder
			sb.Grow(150)

			for method := range seen {
				if method == r.Method {
					continue
				}
				if sb.Len() > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(method)
			}

			if _, ok := seen[http.MethodOptions]; !ok && fox.handleOPTIONS {
				if sb.Len() > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(http.MethodOptions)
			}

			w.Header().Set(HeaderAllow, sb.String())
			c.scope = NoMethodHandler
			fox.noMethod(c)
			tree.pool.Put(c)
			return
		}
	}

	c.scope = NoRouteHandler
	fox.noRoute(c)
}

const (
	// len(+{any}) == len(any)+3 == len(*{any})
	wildcardExtraChar = 3
	// len({foo}) == len(foo)+2
	paramExtraChar = 2
)

// Sub returns a [HandlerFunc] that mounts the provided [Router] as a sub-router. Requests matching the parent
// route prefix are delegated to the sub-router which handles the remaining path. The parent route pattern
// should end with a catch-all. Parameters captured by the parent route are preserved and accessible alongside
// any parameters matched by the sub-router. Similarly, [http.Request.Pattern] is the concatenation of the
// parent and sub-router patterns. See also [Router.Add] for registering the handler.
func Sub(router *Router) HandlerFunc {
	return func(c *Context) {
		route := c.Route()
		if route == nil {
			panic("fox: invalid use of Sub in non-RouteHandler scope")
		}

		tree := router.getTree()
		subCtx := tree.pool.Get().(*Context)
		subCtx.resetWithWriter(c.Writer(), c.Request())
		// Any recovery middleware would probably be before the mounted route, so let's defer this one for safety.
		defer tree.pool.Put(subCtx)

		*subCtx.subPatterns = append(*subCtx.subPatterns, *c.subPatterns...)

		lastTkType := route.tokens[len(route.tokens)-1].typ
		var p string
		switch lastTkType {
		case nodeWildcard:
			key := (*c.paramsKeys)[len(*c.paramsKeys)-1]
			p = strings.TrimSuffix(c.pattern[:len(c.pattern)-(len(key)+wildcardExtraChar)], "/")
		case nodeParam:
			key := (*c.paramsKeys)[len(*c.paramsKeys)-1]
			p = strings.TrimSuffix(c.pattern[:len(c.pattern)-(len(key)+paramExtraChar)], "/")
		default:
			// Reaching this case means the parent route does not end with a catch-all parameter (e.g., /api/
			// instead of /api/+{rest}). This is technically a misuse of the sub-router API, but we handle it
			// gracefully as a defensive measure: if the parent registers /api and the sub-router registers /,
			// we treat it similarly to /api*{any} (optional wildcard), matching /api with the pattern /api/.
			*subCtx.subPatterns = append(*subCtx.subPatterns, strings.TrimSuffix(c.pattern, "/"))
			router.serveSubRouter(subCtx, "/")
			return
		}

		*subCtx.subPatterns = append(*subCtx.subPatterns, p)

		// If the suffix is non-empty and does not start with a slash, it means we matched a suffix param or
		// wildcard such as /foo/+{args}, where the captured value excludes the leading "/". In that case, we
		// reslice from the original path to include it, avoiding an allocation from "/" + suffix.
		suffix := cmp.Or((*c.params)[len(*c.params)-1], "/")
		if !strings.HasPrefix(suffix, "/") {
			path := c.Path()
			slashPos := len(path) - len(suffix) - 1
			if path[slashPos] == slashDelim {
				suffix = path[slashPos:]
			} else {
				// For a route like /api*{any} with a request path of /apifoobar/, we would end up with the suffix
				// "ifoobar/", which could be problematic if "ifoobar/" is registered as a route (with hostname).
				// While this would likely constitute an abuse of the sub-router API, we clear the suffix as a
				// defensive measure to prevent any match in the sub-router.
				suffix = ""
			}
		}

		// Copy parent params and paramsKeys to the subrouter context, excluding the last
		// entry which is the catch-all wildcard used to mount the subrouter.
		// Subrouters are never evaluated in lazy lookup mode, so params are always
		// captured. If parent has no params beyond the catch-all, this is a no-op.
		*subCtx.params = append(*subCtx.params, (*c.params)[:len(*c.params)-1]...)
		*subCtx.paramsKeys = append((*subCtx.paramsKeys)[:0], (*c.paramsKeys)[:len(*c.paramsKeys)-1]...)

		// Serve the sub router
		router.serveSubRouter(subCtx, suffix)
	}
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
					if url[startCatchAll] == '*' {
						return parsedRoute{}, fmt.Errorf("%w: '*{param}' allowed only as suffix", ErrInvalidRoute)
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
		if totallen > 253 {
			return parsedRoute{}, fmt.Errorf("%w: hostname exceed 253 characters", ErrInvalidRoute)
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
