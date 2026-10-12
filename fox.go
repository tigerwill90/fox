// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/fox-toolkit/fox/internal/slicesutil"
	"github.com/fox-toolkit/fox/internal/stringsutil"
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
	ClientIP(c RequestContext) (netip.Addr, error)
}

// The ClientIPResolverFunc type is an adapter to allow the use of ordinary functions as [ClientIPResolver]. If f is a
// function with the appropriate signature, ClientIPResolverFunc(f) is a ClientIPResolverFunc that calls f.
type ClientIPResolverFunc func(c RequestContext) (netip.Addr, error)

// ClientIP calls f(c).
func (f ClientIPResolverFunc) ClientIP(c RequestContext) (netip.Addr, error) {
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
	// RejectPathHandler scope applies to the internal reject path handler, invoked when path normalization rejects a request.
	RejectPathHandler
	// AllHandlers is a combination of all the above scopes, which can be used to apply middlewares to all types of handlers.
	AllHandlers = RouteHandler | NoRouteHandler | NoMethodHandler | RedirectSlashHandler | RedirectPathHandler | OptionsHandler | RejectPathHandler
)

// Router is a lightweight high performance HTTP request router that support mutation on its routing tree
// while handling request concurrently.
type Router struct {
	clientip               ClientIPResolver
	noRoute                HandlerFunc
	noMethod               HandlerFunc
	tsrRedirect            HandlerFunc
	pathRedirect           HandlerFunc
	pathReject             HandlerFunc
	autoOPTIONS            HandlerFunc
	tree                   atomic.Pointer[iTree]
	mws                    []middleware
	maxParams              int
	maxParamKeyBytes       int
	maxMatchers            int
	mu                     sync.Mutex
	handleSlash            TrailingSlashOption
	mergeSlash             NormalizeOption
	collapseDots           NormalizeOption
	hasNormalize           bool
	hasRedirectPath        bool
	handleMethodNotAllowed bool
	handleOPTIONS          bool
	systemWideOPTIONS      bool
	allowRegexp            bool
}

func initRouter() *Router {
	r := new(Router)
	r.noRoute = DefaultNoRouteHandler
	r.noMethod = DefaultNoMethodHandler
	r.autoOPTIONS = DefaultOptionsHandler
	r.tsrRedirect = internalTrailingSlashHandler
	r.pathRedirect = internalPathRedirectHandler
	r.pathReject = DefaultRejectPathHandler
	r.clientip = noClientIPResolver{}
	r.maxParams = math.MaxUint8
	r.maxParamKeyBytes = math.MaxUint8
	r.maxMatchers = math.MaxUint8
	r.handleSlash = ExactSlash
	r.mergeSlash = ExactPath
	r.collapseDots = ExactPath
	r.systemWideOPTIONS = true
	return r
}

// RouterInfo holds information on the configured global options.
type RouterInfo struct {
	MaxRouteParams        int
	MaxRouteParamKeyBytes int
	MaxRouteMatchers      int
	TrailingSlashOption   TrailingSlashOption
	MergeSlashes          NormalizeOption
	CollapseDotSegments   NormalizeOption
	NoMethod              bool
	AutoOptions           bool
	SystemWideOptions     bool
	ClientIPResolver      bool
	AllowRegexpParam      bool
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

	// Clip so NewRoute's append(fox.mws, rte.mws...) can never write into the shared backing array.
	router.mws = slices.Clip(router.mws)

	router.hasNormalize = router.mergeSlash != ExactPath || router.collapseDots != ExactPath
	router.hasRedirectPath = router.mergeSlash == RedirectPath || router.collapseDots == RedirectPath

	router.noRoute = applyMiddleware(NoRouteHandler, router.mws, router.noRoute)
	router.noMethod = applyMiddleware(NoMethodHandler, router.mws, router.noMethod)
	router.tsrRedirect = applyMiddleware(RedirectSlashHandler, router.mws, router.tsrRedirect)
	router.pathRedirect = applyMiddleware(RedirectPathHandler, router.mws, router.pathRedirect)
	router.pathReject = applyMiddleware(RejectPathHandler, router.mws, router.pathReject)
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
//   - [*PatternError]: If the pattern syntax is invalid.
//   - [*RouteConflictError]: If the route conflict with others.
//   - [*RouteNameConflictError]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the method is invalid, the handler is nil or the pattern is empty.
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
//   - [*RouteConflictError]: If the route conflict with others.
//   - [*RouteNameConflictError]: If the route name is already registered.
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
//   - [*PatternError]: If the pattern syntax is invalid.
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [*RouteNameConflictError]: If the route name is already registered.
//   - [ErrInvalidRoute]: If the method is invalid, the handler is nil or the pattern is empty.
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
//   - [*RouteNameConflictError]: If the route name is already registered.
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
// If an error occurs, it returns one of the following:
//   - [*PatternError]: If the pattern syntax is invalid.
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the method is invalid or the pattern is empty.
//   - [ErrInvalidConfig]: If the provided options are invalid.
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
		return r.pattern.str == pattern && slicesutil.EqualUnsorted(r.methods, methods) && r.matchersEqual(matchers)
	})
	if idx < 0 {
		return nil
	}
	return matched.routes[idx]
}

// Name performs a lookup for a registered route matching the given route name. It returns
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

// Match performs a route lookup for the given method and [http.Request]. It returns the matching registered [Route]
// (if any) along with a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). This function is safe for concurrent use by multiple goroutine and while
// mutation on routes are ongoing. See also [Router.Lookup] as an alternative.
func (fox *Router) Match(method string, r *http.Request) (route *Route, tsr bool) {
	if method == "" {
		return nil, false
	}
	tree := fox.getTree()
	c := tree.pool.Get().(*Context)
	defer tree.pool.Put(c)
	c.resetWithRequest(r)

	path := c.RoutingPath()

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
	if r.Method == "" {
		return nil, nil, false
	}
	tree := fox.getTree()
	c := tree.pool.Get().(*Context)
	c.resetWithWriter(w, r)

	path := c.RoutingPath()

	idx, n, tsr := tree.lookup(r.Method, r.Host, path, c, false)
	if n != nil {
		c.route = n.routes[idx]
		return c.route, c, tsr
	}

	tree.pool.Put(c)
	return
}

// NewRoute create a new [Route], configured with the provided options.
// If an error occurs, it returns one of the following:
//   - [*PatternError]: If the pattern syntax is invalid.
//   - [ErrInvalidRoute]: If the method is invalid, the handler is nil or the pattern is empty.
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

	pat, paramsCnt, err := fox.parsePattern(pattern)
	if err != nil {
		return nil, err
	}

	rte := &Route{
		clientip:    fox.clientip,
		hbase:       handler,
		pattern:     pat,
		handleSlash: fox.handleSlash,
	}

	rte.params = make([]string, 0, paramsCnt)
	for _, tk := range pat.tokens {
		if tk.typ != nodeStatic {
			rte.params = append(rte.params, tk.value)
		}
		rte.nested = rte.nested || tk.nested
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
	// A trailing slash redirect on a path starting with "//" would produce a protocol-relative
	// Location that browsers resolve to another host.
	if rte.handleSlash == RedirectSlash && strings.HasPrefix(pat.str[pat.endHost:], "//") {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRoute, "unsafe trailing slash redirect on path starting with '//'")
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

	if len(rte.methods) == 1 && len(rte.matchers) == 0 {
		rte.methodFast = rte.methods[0]
	}

	return rte, nil
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
		patterns: tree.patterns,
		names:    tree.names,
		methods:  tree.methods,
		maxDepth: tree.maxDepth,
	}
}

// Updates executes a function within the context of a read-write managed transaction. If no error is returned from the
// function then the transaction is committed. If an error is returned then the entire transaction is aborted.
// Updates returns any error returned by fn. This function is safe for concurrent use by multiple goroutine and while
// the router is serving request. However [Txn] itself is NOT thread-safe.
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
// However [Txn] itself is NOT thread-safe.
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

// Info returns information on the configured global option.
func (fox *Router) Info() RouterInfo {
	_, ok := fox.clientip.(noClientIPResolver)
	return RouterInfo{
		MaxRouteParams:        fox.maxParams,
		MaxRouteParamKeyBytes: fox.maxParamKeyBytes,
		MaxRouteMatchers:      fox.maxMatchers,
		NoMethod:              fox.handleMethodNotAllowed,
		AutoOptions:           fox.handleOPTIONS,
		TrailingSlashOption:   fox.handleSlash,
		MergeSlashes:          fox.mergeSlash,
		CollapseDotSegments:   fox.collapseDots,
		ClientIPResolver:      !ok,
		AllowRegexpParam:      fox.allowRegexp,
		SystemWideOptions:     fox.systemWideOPTIONS,
	}
}

// Txn create a new read-write or read-only transaction. Each [Txn] must be finalized with [Txn.Commit] or [Txn.Abort].
// It's safe to create transaction from multiple goroutine and while the router is serving request. Creating a write
// transaction blocks while another write transaction is in progress. However, the returned [Txn] itself is NOT thread-safe.
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
	defer tree.pool.Put(c)

	if r.Method == "" {
		// This may only happen if a middleware set r.Method = "" before ServeHTTP is called
		// but since it may produce unexpected match with fastMethod, let's be defensive here.
		c.scope = NoRouteHandler
		fox.noRoute(c)
		return
	}

	path, rewrite := routingPath(r)
	orig := r
	rewritten := false
	nonCanonical := false
	if rewrite {
		// Rewrite so downstream handlers (e.g. reverse proxies) forward the path the router routed on.
		r = rewriteRequest(r, path, false)
		c.req = r
		rewritten = true
	}

	if fox.hasNormalize && r.Method != http.MethodConnect {
		var (
			normalized string
			redirect   bool
			ok         bool
		)
		if fox.hasRedirectPath {
			normalized, redirect, ok = fox.redirectRoutingPath(path)
		} else {
			normalized, ok = fox.normalizeRoutingPath(path)
		}
		if !ok {
			c.scope = RejectPathHandler
			fox.pathReject(c)
			return
		}
		switch {
		case redirect:
			// A "." or ".." path element in the Location may be resolved by the client,
			// redirecting to a different path.
			if fox.collapseDots != ExactPath || !hasDotSegment(normalized) {
				if idx, n, tsr := tree.lookup(r.Method, r.Host, normalized, c, true); n != nil && (!tsr || n.routes[idx].handleSlash != ExactSlash) {
					r.Pattern = ""
					orig.Pattern = ""
					c.scope = RedirectPathHandler
					fox.pathRedirect(c)
					return
				}
			}
			nonCanonical = true
			goto NoMatch
		case len(normalized) != len(path):
			path = normalized
			r = rewriteRequest(r, normalized, rewritten)
			c.req = r
			rewritten = true
		}
	}

	if idx, n, tsr := tree.lookup(r.Method, r.Host, path, c, false); !tsr && n != nil {
		c.route = n.routes[idx]
		r.Pattern = c.route.pattern.str
		orig.Pattern = r.Pattern
		c.route.hall(c)
		return
	} else if tsr && n != nil && r.Method != http.MethodConnect && r.URL.Path != "/" {
		route := n.routes[idx]
		if route.handleSlash == RelaxedSlash {
			c.route = route
			r.Pattern = route.pattern.str
			orig.Pattern = r.Pattern
			c.req = rewriteRequest(r, fixTrailingSlash(path), rewritten)
			c.route.hall(c)
			return
		}

		// A "." or ".." path element in the Location may be resolved by the client,
		// redirecting to a different path.
		if route.handleSlash == RedirectSlash && !hasDotSegment(path) {
			*c.params = (*c.params)[:0]
			r.Pattern = ""
			orig.Pattern = ""
			c.scope = RedirectSlashHandler
			fox.tsrRedirect(c)
			return
		}
	}

NoMatch:
	*c.params = (*c.params)[:0]
	r.Pattern = ""
	orig.Pattern = ""

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
			if sb.Len() > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(http.MethodOptions)
		}

		if sb.Len() > 0 {
			w.Header().Set(HeaderAllow, sb.String())
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if fox.handleOPTIONS && isOPTIONS {
		// A CORS request is an HTTP request that includes an `Origin` header: https://fetch.spec.whatwg.org/#cors-request
		// A CORS preflight request contains at most one ACRM header: https://fetch.spec.whatwg.org/#cors-preflight-fetch
		_, foundOrigin := firstHeader(r.Header, HeaderOrigin)
		_, foundAcrm := firstHeader(r.Header, HeaderAccessControlRequestMethod)

		// A CORS-preflight request is a CORS request that checks to see if the CORS protocol is understood. Preflight should not enforce resource
		// validation (e.g., 404 or 405). The best practice is to let the actual request fail later.
		// See https://stackoverflow.com/questions/64352697/should-a-server-implementing-cors-always-reply-with-a-2xx-code-for-options-metho
		if foundOrigin && foundAcrm {
			c.scope = OptionsHandler
			fox.autoOPTIONS(c)
			return
		}

		if !nonCanonical {
			// Since different method and route may match (e.g. GET /foo/bar & POST /foo/{name}), we cannot set the path and params.
			seen := make(map[string]struct{})
			for method := range tree.methods {
				if _, ok := seen[method]; ok {
					continue
				}
				if idx, n, tsr := tree.lookup(method, r.Host, path, c, true); n != nil && (!tsr || (method != http.MethodConnect && n.routes[idx].handleSlash == RelaxedSlash)) {
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
				return
			}
		}
	} else if fox.handleMethodNotAllowed && !nonCanonical {

		seen := make(map[string]struct{})
		seen[r.Method] = struct{}{}

		for method := range tree.methods {
			if _, ok := seen[method]; ok {
				continue
			}
			if idx, n, tsr := tree.lookup(method, r.Host, path, c, true); n != nil && (!tsr || (method != http.MethodConnect && n.routes[idx].handleSlash == RelaxedSlash)) {
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
			return
		}
	}

	c.scope = NoRouteHandler
	fox.noRoute(c)
}

// DefaultNoRouteHandler is a simple [HandlerFunc] that replies to each request
// with a “404 page not found” reply.
func DefaultNoRouteHandler(c *Context) {
	http.Error(c.Writer(), "404 page not found", http.StatusNotFound)
}

// DefaultNoMethodHandler is a simple [HandlerFunc] that replies to each request
// with a “405 Method Not Allowed” reply.
func DefaultNoMethodHandler(c *Context) {
	http.Error(c.Writer(), http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

// DefaultOptionsHandler is a simple [HandlerFunc] that replies to each request with a "200 OK" reply.
func DefaultOptionsHandler(c *Context) {
	c.Writer().WriteHeader(http.StatusNoContent)
}

// DefaultRejectPathHandler is a simple [HandlerFunc] that replies to each request
// with a “400 Bad Request” reply.
func DefaultRejectPathHandler(c *Context) {
	http.Error(c.Writer(), http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
}

// normalizeRoutingPath applies the active normalization passes to path. It returns ok=false
// when the path must be rejected.
func (fox *Router) normalizeRoutingPath(path string) (string, bool) {
	if fox.mergeSlash != ExactPath {
		path = MergeSlashes(path)
	}
	if fox.collapseDots != ExactPath {
		return CollapseDotSegments(path)
	}
	return path, true
}

// redirectRoutingPath is like normalizeRoutingPath, but also reports whether a RedirectPath
// pass changed the path. In a mixed configuration, only a change made by a pass in RedirectPath
// mode triggers a redirect. Both passes only remove bytes, so a change always shows in the length.
func (fox *Router) redirectRoutingPath(path string) (_ string, redirect, ok bool) {
	if fox.mergeSlash != ExactPath {
		merged := MergeSlashes(path)
		redirect = len(merged) != len(path) && fox.mergeSlash == RedirectPath
		path = merged
	}
	if fox.collapseDots != ExactPath {
		collapsed, ok := CollapseDotSegments(path)
		if !ok {
			return "", false, false
		}
		redirect = redirect || (len(collapsed) != len(path) && fox.collapseDots == RedirectPath)
		path = collapsed
	}
	return path, redirect, true
}

func internalTrailingSlashHandler(c *Context) {
	req := c.Request()

	code := http.StatusMovedPermanently
	if req.Method != http.MethodGet {
		// Will be redirected only with the same method (SEO friendly)
		code = http.StatusPermanentRedirect
	}

	path := escapeLeadingSlashes(fixTrailingSlash(c.RoutingPath()))
	if q := req.URL.RawQuery; q != "" {
		path += "?" + q
	}

	redirect(c.Writer(), req, path, code)
}

func internalPathRedirectHandler(c *Context) {
	req := c.Request()

	code := http.StatusMovedPermanently
	if req.Method != http.MethodGet {
		// Will be redirected only with the same method (SEO friendly)
		code = http.StatusPermanentRedirect
	}

	target, _ := c.fox.normalizeRoutingPath(c.RoutingPath())
	target = escapeLeadingSlashes(target)
	if q := req.URL.RawQuery; q != "" {
		target += "?" + q
	}

	redirect(c.Writer(), req, target, code)
}

// redirect is like [http.Redirect] but does not clean the path.
func redirect(w http.ResponseWriter, r *http.Request, url string, code int) {
	if url == "" {
		url = "/"
	}

	h := w.Header()

	// RFC 7231 notes that a short HTML body is usually included in
	// the response because older user agents may not understand 301/307.
	// Do it only if the request didn't already have a Content-Type header.
	_, hadCT := h[HeaderContentType]

	h.Set(HeaderLocation, hexEscapeNonASCII(url))
	if !hadCT && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		h.Set(HeaderContentType, MIMETextHTMLCharsetUTF8)
	}
	w.WriteHeader(code)

	// Shouldn't send the body for POST or HEAD; that leaves GET.
	if !hadCT && r.Method == http.MethodGet {
		body := "<a href=\"" + htmlEscape(url) + "\">" + http.StatusText(code) + "</a>.\n"
		_, _ = fmt.Fprintln(w, body)
	}
}

// htmlReplacer, htmlEscape and hexEscapeNonASCII are copied from net/http
// (Copyright 2009 The Go Authors, BSD-style license).
var htmlReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	// "&#34;" is shorter than "&quot;".
	`"`, "&#34;",
	// "&#39;" is shorter than "&apos;" and apos was not in HTML until HTML5.
	"'", "&#39;",
)

func htmlEscape(s string) string {
	return htmlReplacer.Replace(s)
}

func hexEscapeNonASCII(s string) string {
	newLen := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			newLen += 3
		} else {
			newLen++
		}
	}
	if newLen == len(s) {
		return s
	}
	b := make([]byte, 0, newLen)
	var pos int
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			if pos < i {
				b = append(b, s[pos:i]...)
			}
			b = append(b, '%')
			b = strconv.AppendInt(b, int64(s[i]), 16)
			pos = i + 1
		}
	}
	if pos < len(s) {
		b = append(b, s[pos:]...)
	}
	return string(b)
}

// rewriteRequest returns a request whose URL is set to the escaped routing path, so downstream
// handlers (e.g. reverse proxies) see the path the router matched on. Unless owned, the request
// is shallow copied so the caller's request is never mutated.
func rewriteRequest(r *http.Request, escaped string, owned bool) *http.Request {
	// escaped always comes from routingPath or the normalize passes, which only
	// produce valid encodings, so PathUnescape cannot fail.
	p, err := url.PathUnescape(escaped)
	if err != nil {
		return r
	}

	if !owned {
		type requestCopy struct {
			req http.Request
			u   url.URL
		}
		cp := new(requestCopy)
		cp.req = *r
		cp.u = *r.URL
		cp.req.URL = &cp.u
		r = &cp.req
	}

	u := r.URL
	u.Path = p
	u.RawPath = ""
	if stringsutil.EscapePath(p) != escaped {
		u.RawPath = escaped
	}
	return r
}

// routingPath returns the canonical routing path for the request and reports whether the request
// URL must be rewritten so downstream handlers see the form the router routed on. That is the
// case when the wire form contains a byte that must be escaped in a routing path, a malformed
// escape, or a URL.RawPath that is not an encoding of URL.Path.
// A wire form that differs from the routing path only by hex case (%2f vs %2F) or by an encoded
// unreserved character (%61 vs a) is not rewritten. RFC 3986 (sections 6.2.2.1 and 6.2.2.2)
// guarantees both forms identify the same resource.
func routingPath(r *http.Request) (path string, rewrite bool) {
	u := r.URL
	if u.RawPath == "" {
		// The wire path was the default encoding of Path, which is always canonical.
		return stringsutil.EscapePath(u.Path), false
	}
	norm, wellFormed := stringsutil.NormalizeRawPath(u.RawPath, u.Path)
	if norm == "" {
		// RawPath has a malformed escape or is not an encoding of Path, like net/url,
		// Path is the source of truth.
		return stringsutil.EscapePath(u.Path), true
	}
	return norm, !wellFormed
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

func (s noClientIPResolver) ClientIP(_ RequestContext) (netip.Addr, error) {
	return netip.Addr{}, ErrNoClientIPResolver
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

func rawExpr(re *regexp.Regexp) string {
	expr := re.String()
	if strings.HasPrefix(expr, "(?i)") {
		return expr[8 : len(expr)-2]
	}
	return expr[4 : len(expr)-2]
}
