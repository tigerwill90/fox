// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

const verb = 4

const (
	slashDelim   byte = '/'
	dotDelim     byte = '.'
	bracketDelim byte = '{'
	starDelim    byte = '*'
)

var (
	// regEnLetter matches english letters for http method name.
	regEnLetter = regexp.MustCompile("^[A-Z]+$")
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

// ClientIPStrategy define a strategy for obtaining the "real" client IP from HTTP requests. The strategy used must be
// chosen and tuned for your network configuration. This should result in the strategy never returning an error
// i.e., never failing to find a candidate for the "real" IP. Consequently, getting an error result should be treated as
// an application error, perhaps even worthy of panicking. Builtin best practices strategies can be found in the
// github.com/tigerwill90/fox/strategy package. See https://adam-p.ca/blog/2022/03/x-forwarded-for/ for more details on
// how to choose the right strategy for your use-case and network.
type ClientIPStrategy interface {
	// ClientIP returns the "real" client IP according to the implemented strategy. It returns an error if no valid IP
	// address can be derived using the strategy. This is typically considered a misconfiguration error, unless the strategy
	// involves obtaining an untrustworthy or optional value.
	ClientIP(c Context) (*net.IPAddr, error)
}

// The ClientIPStrategyFunc type is an adapter to allow the use of ordinary functions as [ClientIPStrategy]. If f is a
// function with the appropriate signature, ClientIPStrategyFunc(f) is a ClientIPStrategyFunc that calls f.
type ClientIPStrategyFunc func(c Context) (*net.IPAddr, error)

// ClientIP calls f(c).
func (f ClientIPStrategyFunc) ClientIP(c Context) (*net.IPAddr, error) {
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
	// RedirectHandler scope applies to the internal redirect handler, used for handling requests with trailing slashes.
	RedirectHandler
	// OptionsHandler scope applies to the automatic OPTIONS handler, which handles pre-flight or cross-origin requests.
	OptionsHandler
	// AllHandlers is a combination of all the above scopes, which can be used to apply middlewares to all types of handlers.
	AllHandlers = RouteHandler | NoRouteHandler | NoMethodHandler | RedirectHandler | OptionsHandler
)

// Router is a lightweight high performance HTTP request router that support mutation on its routing tree
// while handling request concurrently.
type Router struct {
	noRoute                HandlerFunc
	noMethod               HandlerFunc
	tsrRedirect            HandlerFunc
	autoOptions            HandlerFunc
	tree                   atomic.Pointer[iTree]
	ipStrategy             ClientIPStrategy
	mws                    []middleware
	mu                     sync.Mutex
	handleMethodNotAllowed bool
	handleOptions          bool
	redirectTrailingSlash  bool
	ignoreTrailingSlash    bool
}

type middleware struct {
	m     MiddlewareFunc
	scope HandlerScope
	g     bool
}

var _ http.Handler = (*Router)(nil)

// New returns a ready to use instance of Fox router.
func New(opts ...GlobalOption) *Router {
	r := new(Router)

	r.noRoute = DefaultNotFoundHandler
	r.noMethod = DefaultMethodNotAllowedHandler
	r.autoOptions = DefaultOptionsHandler
	r.ipStrategy = noClientIPStrategy{}

	for _, opt := range opts {
		opt.applyGlob(r)
	}

	r.noRoute = applyMiddleware(NoRouteHandler, r.mws, r.noRoute)
	r.noMethod = applyMiddleware(NoMethodHandler, r.mws, r.noMethod)
	r.tsrRedirect = applyMiddleware(RedirectHandler, r.mws, defaultRedirectTrailingSlashHandler)
	r.autoOptions = applyMiddleware(OptionsHandler, r.mws, r.autoOptions)

	r.tree.Store(r.newTree())
	return r
}

// MethodNotAllowedEnabled returns whether the router is configured to handle
// requests with methods that are not allowed.
// This api is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) MethodNotAllowedEnabled() bool {
	return fox.handleMethodNotAllowed
}

// AutoOptionsEnabled returns whether the router is configured to automatically
// respond to OPTIONS requests.
// This api is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) AutoOptionsEnabled() bool {
	return fox.handleOptions
}

// RedirectTrailingSlashEnabled returns whether the router is configured to automatically
// redirect requests that include or omit a trailing slash.
// This api is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) RedirectTrailingSlashEnabled() bool {
	return fox.redirectTrailingSlash
}

// IgnoreTrailingSlashEnabled returns whether the router is configured to ignore
// trailing slashes in requests when matching routes.
// This api is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) IgnoreTrailingSlashEnabled() bool {
	return fox.ignoreTrailingSlash
}

// ClientIPStrategyEnabled returns whether the router is configured with a ClientIPStrategy.
// This api is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) ClientIPStrategyEnabled() bool {
	_, ok := fox.ipStrategy.(noClientIPStrategy)
	return !ok
}

// Handle registers a new handler for the given method and route pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrRouteConflict]: If the route conflicts with another.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to add a new handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To override an existing route, use [Router.Update].
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

// MustHandle registers a new handler for the given method and route pattern. On success, it returns the newly registered [Route]
// This function is a convenience wrapper for the [Router.Handle] function and panics on error. It's perfectly safe to
// add a new handler while the router serving requests. This function is safe for concurrent use by multiple goroutines.
// To override an existing route, use [Router.Update].
func (fox *Router) MustHandle(method, pattern string, handler HandlerFunc, opts ...RouteOption) *Route {
	rte, err := fox.Handle(method, pattern, handler, opts...)
	if err != nil {
		panic(err)
	}
	return rte
}

// Update override an existing handler for the given method and route pattern. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to update a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine. To add new handler, use [Router.Handle] method.
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

// Delete deletes an existing handler for the given method and route pattern. If an error occurs, it returns one of the following:
//   - [ErrRouteNotFound]: If the route does not exist.
//   - [ErrInvalidRoute]: If the provided method or pattern is invalid.
//
// It's safe to delete a handler while the router is serving requests. This function is safe for concurrent use by
// multiple goroutine.
func (fox *Router) Delete(method, pattern string) error {
	txn := fox.txnWith(true, false)
	defer txn.Abort()
	if err := txn.Delete(method, pattern); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Has allows to check if the given method and route pattern exactly match a registered route. This function is safe for
// concurrent use by multiple goroutine and while mutation on routes are ongoing. See also [Router.Route] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) Has(method, pattern string) bool {
	return fox.Route(method, pattern) != nil
}

// Route performs a lookup for a registered route matching the given method and route pattern. It returns the [Route] if a
// match is found or nil otherwise. This function is safe for concurrent use by multiple goroutine and while
// mutation on route are ongoing. See also [Router.Has] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
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
// (trailing slash action recommended). This function is safe for concurrent use by multiple goroutine and while
// mutation on routes are ongoing. See also [Router.Lookup] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) Reverse(method, host, path string) (route *Route, tsr bool) {
	tree := fox.getRoot()
	c := tree.ctx.Get().(*cTx)
	c.resetNil()
	n, tsr := tree.lookup(method, host, path, c, true)
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
// concurrent use by multiple goroutine and while mutation on routes are ongoing. See also [Tree.Reverse] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
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

// Iter returns a collection of range iterators for traversing registered methods and routes. It creates a
// point-in-time snapshot of the routing tree. Therefore, all iterators returned by Iter will not observe subsequent
// write on the router. This function is safe for concurrent use by multiple goroutine and while mutation on
// routes are ongoing.
func (fox *Router) Iter() Iter {
	rt := fox.getRoot()
	return Iter{
		tree:     rt,
		root:     rt.root,
		maxDepth: rt.maxDepth,
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

	rootTxn := fox.getRoot().txn(cache)
	return &Txn{
		fox:     fox,
		write:   write,
		rootTxn: rootTxn,
	}
}

// newTree returns a fresh routing Tree that inherits all registered router options.
func (fox *Router) newTree() *iTree {
	tree := new(iTree)
	tree.fox = fox

	// Pre instantiate nodes for common http verb
	nr := make([]*node, len(commonVerbs))
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

// newRoute create a new route, apply route options and apply middleware on the handler.
func (fox *Router) newRoute(pattern string, handler HandlerFunc, opts ...RouteOption) (*Route, uint32, error) {
	n, endHost, err := parseRoute(pattern)
	if err != nil {
		return nil, 0, err
	}

	rte := &Route{
		ipStrategy:            fox.ipStrategy,
		hbase:                 handler,
		pattern:               pattern,
		mws:                   fox.mws,
		redirectTrailingSlash: fox.redirectTrailingSlash,
		ignoreTrailingSlash:   fox.ignoreTrailingSlash,
		hostSplit:             endHost, // 0 if no host
	}

	for _, opt := range opts {
		opt.applyRoute(rte)
	}
	rte.hself, rte.hall = applyRouteMiddleware(rte.mws, handler)

	return rte, n, nil
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

func defaultRedirectTrailingSlashHandler(c Context) {
	req := c.Request()

	code := http.StatusMovedPermanently
	if req.Method != http.MethodGet {
		// Will be redirected only with the same method (SEO friendly)
		code = http.StatusPermanentRedirect
	}

	var url string
	if len(req.URL.RawPath) > 0 {
		url = FixTrailingSlash(req.URL.RawPath)
	} else {
		url = FixTrailingSlash(req.URL.Path)
	}

	if url[len(url)-1] == '/' {
		localRedirect(c.Writer(), req, path.Base(url)+"/", code)
		return
	}
	localRedirect(c.Writer(), req, "../"+path.Base(url), code)
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

	if r.Method != http.MethodConnect && r.URL.Path != "/" && tsr {
		if n.route.ignoreTrailingSlash {
			c.route = n.route
			c.tsr = tsr
			n.route.hall(c)
			tree.ctx.Put(c)
			return
		}

		if n.route.redirectTrailingSlash && path == CleanPath(path) {
			// Reset params as it may have recorded wildcard segment (the context may still be used in a middleware)
			*c.params = (*c.params)[:0]
			c.route = nil
			c.tsr = false
			c.scope = RedirectHandler
			fox.tsrRedirect(c)
			tree.ctx.Put(c)
			return
		}
	}

	// Reset params as it may have recorded wildcard segment (the context may still be used in no route, no method and
	// automatic option handler or middleware)
	*c.params = (*c.params)[:0]
	c.route = nil
	c.tsr = false

	if r.Method == http.MethodOptions && fox.handleOptions {
		var sb strings.Builder
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
				if n, tsr := tree.lookup(tree.root[i].key, r.Host, path, c, true); n != nil && (!tsr || n.route.ignoreTrailingSlash) {
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
		for i := 0; i < len(tree.root); i++ {
			if tree.root[i].key != r.Method {
				if n, tsr := tree.lookup(tree.root[i].key, r.Host, path, c, true); n != nil && (!tsr || n.route.ignoreTrailingSlash) {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(tree.root[i].key)
				}
			}
		}
		if sb.Len() > 0 {
			// TODO maybe should add OPTIONS ?
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
func parseRoute(url string) (uint32, int, error) {

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

			if url[i] == '/' || url[i] == '*' || url[i] == '{' {
				return 0, -1, fmt.Errorf("%w: illegal character '%s' in '*{param}'", ErrInvalidRoute, string(url[i]))
			}
			inParam = true
			i++
		default:

			if i == endHost {
				delim = slashDelim
			}

			if url[i] == '{' {
				state = stateParam
				paramCnt++
			} else if url[i] == '*' {
				if i < endHost {
					return 0, -1, fmt.Errorf("%w: catch-all wildcard not supported in hostname", ErrInvalidRoute)
				}
				state = stateCatchAll
				i++
				paramCnt++
			} else {
				countStatic++
				if i < endHost {
					c := url[i]
					switch {
					case 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_':
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
					default:
						return 0, -1, fmt.Errorf("%w: illegal character '%s' in hostname label", ErrInvalidRoute, string(c))
					}
					last = c
				}
			}

			if paramCnt > math.MaxUint16 {
				return 0, -1, fmt.Errorf("%w: too many params (%d)", ErrInvalidRoute, paramCnt)
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

// localRedirect redirect the client to the new path, but it does not convert relative paths to absolute paths
// like Redirect does. If the Content-Type header has not been set, localRedirect sets it to "text/html; charset=utf-8"
// and writes a small HTML body. Setting the Content-Type header to any value, including nil, disables that behavior.
func localRedirect(w http.ResponseWriter, r *http.Request, path string, code int) {
	if q := r.URL.RawQuery; q != "" {
		path += "?" + q
	}

	h := w.Header()

	// RFC 7231 notes that a short HTML body is usually included in
	// the response because older user agents may not understand 301/307.
	// Do it only if the request didn't already have a Content-Type header.
	_, hadCT := h["Content-Type"]

	h.Set(HeaderLocation, hexEscapeNonASCII(path))
	if !hadCT && (r.Method == "GET" || r.Method == "HEAD") {
		h.Set(HeaderContentType, MIMETextHTMLCharsetUTF8)
	}
	w.WriteHeader(code)

	// Shouldn't send the body for POST or HEAD; that leaves GET.
	if !hadCT && r.Method == "GET" {
		body := "<a href=\"" + htmlEscape(path) + "\">" + http.StatusText(code) + "</a>.\n"
		_, _ = fmt.Fprintln(w, body)
	}
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

type noClientIPStrategy struct{}

func (s noClientIPStrategy) ClientIP(_ Context) (*net.IPAddr, error) {
	return nil, ErrNoClientIPStrategy
}
