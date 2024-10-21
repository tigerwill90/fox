// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
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
	tree                   atomic.Pointer[Tree]
	ipStrategy             ClientIPStrategy
	mws                    []middleware
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

	r.tree.Store(r.NewTree())
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

// NewTree returns a fresh routing [Tree] that inherits all registered router options. It's safe to create multiple [Tree]
// concurrently. However, a Tree itself is not thread-safe and all its APIs that perform write operations should be run
// serially. Note that a [Tree] give direct access to the underlying [sync.Mutex].
// This API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) NewTree() *Tree {
	tree := new(Tree)
	tree.fox = fox

	// Pre instantiate nodes for common http verb
	nds := make([]*node, len(commonVerbs))
	for i := range commonVerbs {
		nds[i] = new(node)
		nds[i].key = commonVerbs[i]
		nds[i].paramChildIndex = -1
	}
	tree.nodes.Store(&nds)

	tree.ctx = sync.Pool{
		New: func() any {
			return tree.allocateContext()
		},
	}
	tree.np = sync.Pool{
		New: func() any { return tree.allocateNode() },
	}

	return tree
}

// Tree atomically loads and return the currently in-use routing tree.
// This API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) Tree() *Tree {
	return fox.tree.Load()
}

// Swap atomically replaces the currently in-use routing tree with the provided new tree, and returns the previous tree.
// Note that the swap will panic if the current tree belongs to a different instance of the router, preventing accidental
// replacement of trees from different routers.
func (fox *Router) Swap(new *Tree) (old *Tree) {
	current := fox.tree.Load()
	if current.fox != new.fox {
		panic("swap failed: current and new routing trees belong to different router instances")
	}
	return fox.tree.Swap(new)
}

// Handle registers a new handler for the given method and path. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
//   - [ErrRouteExist]: If the route is already registered.
//   - [ErrRouteConflict]: If the route conflicts with another.
//   - [ErrInvalidRoute]: If the provided method or path is invalid.
//
// It's safe to add a new handler while the tree is in use for serving requests. This function is safe for concurrent
// use by multiple goroutine. To override an existing route, use [Router.Update].
func (fox *Router) Handle(method, path string, handler HandlerFunc, opts ...PathOption) (*Route, error) {
	t := fox.Tree()
	t.Lock()
	defer t.Unlock()
	return t.Handle(method, path, handler, opts...)
}

// MustHandle registers a new handler for the given method and path. On success, it returns the newly registered [Route]
// This function is a convenience wrapper for the [Router.Handle] function and panics on error. It's perfectly safe to
// add a new handler while the tree is in use for serving requests. This function is safe for concurrent use by multiple
// goroutines. To override an existing route, use [Router.Update].
func (fox *Router) MustHandle(method, path string, handler HandlerFunc, opts ...PathOption) *Route {
	rte, err := fox.Handle(method, path, handler, opts...)
	if err != nil {
		panic(err)
	}
	return rte
}

// Update override an existing handler for the given method and path. On success, it returns the newly registered [Route].
// If an error occurs, it returns one of the following:
// - [ErrRouteNotFound]: if the route does not exist.
// - [ErrInvalidRoute]: If the provided method or path is invalid.
//
// It's safe to update a handler while the tree is in use for serving requests. This function is safe for concurrent
// use by multiple goroutine. To add new handler, use [Router.Handle] method.
func (fox *Router) Update(method, path string, handler HandlerFunc, opts ...PathOption) (*Route, error) {
	t := fox.Tree()
	t.Lock()
	defer t.Unlock()
	return t.Update(method, path, handler, opts...)
}

// Delete deletes an existing handler for the given method and path. If an error occurs, it returns one of the following:
// - [ErrRouteNotFound]: if the route does not exist.
// - [ErrInvalidRoute]: If the provided method or path is invalid.
//
// It's safe to delete a handler while the tree is in use for serving requests. This function is safe for concurrent
// use by multiple goroutine.
func (fox *Router) Delete(method, path string) error {
	t := fox.Tree()
	t.Lock()
	defer t.Unlock()
	return t.Delete(method, path)
}

// Lookup performs a manual route lookup for a given [http.Request], returning the matched [Route] along with a
// [ContextCloser], and a boolean indicating if the route was matched by adding or removing a trailing slash
// (trailing slash action recommended). If there is a direct match or a tsr is possible, Lookup always return a
// [Route] and a [ContextCloser]. The [ContextCloser] should always be closed if non-nil. This function is safe for
// concurrent use by multiple goroutine and while mutation on [Tree] are ongoing. See also [Tree.Reverse] as an alternative.
// This API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) Lookup(w ResponseWriter, r *http.Request) (route *Route, cc ContextCloser, tsr bool) {
	tree := fox.Tree()
	return tree.Lookup(w, r)
}

// Iter returns an iterator that provides access to a collection of iterators for traversing the routing tree.
// This function is safe for concurrent use by multiple goroutines and can operate while the [Tree] is being modified.
// This API is EXPERIMENTAL and may change in future releases.
func (fox *Router) Iter() Iter {
	tree := fox.Tree()
	return tree.Iter()
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

	target := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		target = r.URL.RawPath
	}

	tree := fox.tree.Load()
	c := tree.ctx.Get().(*cTx)
	c.reset(w, r)

	nds := *tree.nodes.Load()
	index := findRootNode(r.Method, nds)
	if index < 0 || len(nds[index].children) == 0 {
		goto NoMethodFallback
	}

	n, tsr = tree.lookup(nds[index].children[0].Load(), target, c, false)
	if !tsr && n != nil {
		c.route = n.route
		c.tsr = tsr
		n.route.hall(c)
		// Put back the context, if not extended more than max params or max depth, allowing
		// the slice to naturally grow within the constraint.
		if cap(*c.params) <= int(tree.maxParams.Load()) && cap(*c.skipNds) <= int(tree.maxDepth.Load()) {
			c.tree.ctx.Put(c)
		}
		return
	}

	if r.Method != http.MethodConnect && r.URL.Path != "/" && tsr {
		if n.route.ignoreTrailingSlash {
			c.route = n.route
			c.tsr = tsr
			n.route.hall(c)
			c.Close()
			return
		}

		if n.route.redirectTrailingSlash && target == CleanPath(target) {
			// Reset params as it may have recorded wildcard segment (the context may still be used in a middleware)
			*c.params = (*c.params)[:0]
			c.route = nil
			c.tsr = false
			c.scope = RedirectHandler
			fox.tsrRedirect(c)
			c.Close()
			return
		}
	}

	// Reset params as it may have recorded wildcard segment (the context may still be used in no route, no method and
	// automatic option handler or middleware)
	*c.params = (*c.params)[:0]
	c.route = nil
	c.tsr = false

NoMethodFallback:
	if r.Method == http.MethodOptions && fox.handleOptions {
		var sb strings.Builder
		// Handle system-wide OPTIONS, see https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods/OPTIONS.
		// Note that http.Server.DisableGeneralOptionsHandler should be disabled.
		if target == "*" {
			for i := 0; i < len(nds); i++ {
				if nds[i].key != http.MethodOptions && len(nds[i].children) > 0 {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(nds[i].key)
				}
			}
		} else {
			// Since different method and route may match (e.g. GET /foo/bar & POST /foo/{name}), we cannot set the path and params.
			for i := 0; i < len(nds); i++ {
				if len(nds[i].children) > 0 {
					if n, tsr := tree.lookup(nds[i].children[0].Load(), target, c, true); n != nil && (!tsr || n.route.ignoreTrailingSlash) {
						if sb.Len() > 0 {
							sb.WriteString(", ")
						}
						sb.WriteString(nds[i].key)
					}
				}
			}
		}
		if sb.Len() > 0 {
			sb.WriteString(", ")
			sb.WriteString(http.MethodOptions)
			w.Header().Set(HeaderAllow, sb.String())
			c.scope = OptionsHandler
			fox.autoOptions(c)
			c.Close()
			return
		}
	} else if fox.handleMethodNotAllowed {
		var sb strings.Builder
		for i := 0; i < len(nds); i++ {
			if nds[i].key != r.Method {
				if len(nds[i].children) > 0 {
					if n, tsr := tree.lookup(nds[i], target, c, true); n != nil && (!tsr || n.route.ignoreTrailingSlash) {
						if sb.Len() > 0 {
							sb.WriteString(", ")
						}
						sb.WriteString(nds[i].key)
					}
				}
			}
		}
		if sb.Len() > 0 {
			// TODO maybe should add OPTIONS ?
			w.Header().Set(HeaderAllow, sb.String())
			c.scope = NoMethodHandler
			fox.noMethod(c)
			c.Close()
			return
		}
	}

	c.scope = NoRouteHandler
	fox.noRoute(c)
	c.Close()
}

type resultType int

const (
	exactMatch resultType = iota
	incompleteMatchToEndOfEdge
	incompleteMatchToMiddleOfEdge
	keyEndMidEdge
)

func (r searchResult) classify() resultType {
	if r.charsMatched == len(r.path) {
		if r.charsMatchedInNodeFound == len(r.matched.key) {
			return exactMatch
		}
		if r.charsMatchedInNodeFound < len(r.matched.key) {
			return keyEndMidEdge
		}
	} else if r.charsMatched < len(r.path) {
		// When the node matched is a root node, charsMatched & charsMatchedInNodeFound are both equals to 0, but the value of
		// the key is the http verb instead of a segment of the path and therefore len(r.matched.key) > 0 instead of empty (0).
		if r.charsMatchedInNodeFound == len(r.matched.key) || r.p == nil {
			return incompleteMatchToEndOfEdge
		}
		if r.charsMatchedInNodeFound < len(r.matched.key) {
			return incompleteMatchToMiddleOfEdge
		}
	}
	panic("internal error: cannot classify the result")
}
func (r searchResult) isExactMatch() bool {
	return r.charsMatched == len(r.path) && r.charsMatchedInNodeFound == len(r.matched.key)
}

func (r searchResult) isKeyMidEdge() bool {
	return r.charsMatched == len(r.path) && r.charsMatchedInNodeFound < len(r.matched.key)
}

func (c resultType) String() string {
	return [...]string{"EXACT_MATCH", "INCOMPLETE_MATCH_TO_END_OF_EDGE", "INCOMPLETE_MATCH_TO_MIDDLE_OF_EDGE", "KEY_END_MID_EDGE"}[c]
}

type searchResult struct {
	matched                 *node
	p                       *node
	pp                      *node
	path                    string
	charsMatched            int
	charsMatchedInNodeFound int
	depth                   uint32
}

func commonPrefix(k1, k2 string) string {
	minLength := min(len(k1), len(k2))
	for i := 0; i < minLength; i++ {
		if k1[i] != k2[i] {
			return k1[:i]
		}
	}
	return k1[:minLength]
}

func findRootNode(method string, nodes []*node) int {
	// Nodes for common http method are pre instantiated.
	switch method {
	case http.MethodGet:
		return 0
	case http.MethodPost:
		return 1
	case http.MethodPut:
		return 2
	case http.MethodDelete:
		return 3
	}

	for i, nd := range nodes[verb:] {
		if nd.key == method {
			return i + verb
		}
	}
	return -1
}

const (
	stateDefault uint8 = iota
	stateParam
	stateCatchAll
)

// parseRoute parse and validate the route in a single pass.
func parseRoute(path string) (string, string, int, error) {

	if !strings.HasPrefix(path, "/") {
		return "", "", -1, fmt.Errorf("%w: path must start with '/'", ErrInvalidRoute)
	}

	state := stateDefault
	previous := stateDefault
	startCatchAll := 0
	paramCnt := 0
	inParam := false

	i := 0
	for i < len(path) {
		switch state {
		case stateParam:
			if path[i] == '}' {
				if !inParam {
					return "", "", -1, fmt.Errorf("%w: missing parameter name between '{}'", ErrInvalidRoute)
				}
				inParam = false
				if previous != stateCatchAll {
					if i+1 < len(path) && path[i+1] != '/' {
						return "", "", -1, fmt.Errorf("%w: unexpected character after '{param}'", ErrInvalidRoute)
					}
				} else {
					if i+1 != len(path) {
						return "", "", -1, fmt.Errorf("%w: catch-all '*{params}' are allowed only at the end of a route", ErrInvalidRoute)
					}
				}
				state = stateDefault
				i++
				continue
			}

			if path[i] == '/' || path[i] == '*' || path[i] == '{' {
				return "", "", -1, fmt.Errorf("%w: unexpected character in '{params}'", ErrInvalidRoute)
			}
			inParam = true
			i++

		case stateCatchAll:
			if path[i] != '{' {
				return "", "", -1, fmt.Errorf("%w: unexpected character after '*' catch-all delimiter", ErrInvalidRoute)
			}
			startCatchAll = i
			previous = state
			state = stateParam
			i++

		default:
			if path[i] == '{' {
				state = stateParam
				paramCnt++
			} else if path[i] == '*' {
				state = stateCatchAll
				paramCnt++
			}
			i++
		}
	}

	if state == stateParam {
		return "", "", -1, fmt.Errorf("%w: unclosed '{params}'", ErrInvalidRoute)
	}
	if state == stateCatchAll {
		return "", "", -1, fmt.Errorf("%w: missing '{params}' after '*' catch-all delimiter", ErrInvalidRoute)
	}

	if startCatchAll > 0 {
		return path[:startCatchAll-1], path[startCatchAll+1 : len(path)-1], paramCnt, nil
	}

	return path, "", paramCnt, nil
}

func getRouteConflict(n *node) []string {
	routes := make([]string, 0)

	// TODO, we have to revise that
	if n.isCatchAll() {
		routes = append(routes, n.route.path)
		return routes
	}

	if n.paramChildIndex >= 0 {
		n = n.children[n.paramChildIndex].Load()
	}
	it := newRawIterator(n)
	for it.hasNext() {
		routes = append(routes, it.current.route.path)
	}
	return routes
}

func isRemovable(method string) bool {
	for _, verb := range commonVerbs {
		if verb == method {
			return false
		}
	}
	return true
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
