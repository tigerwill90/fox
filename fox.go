// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"
)

const verb = 4

var commonVerbs = [verb]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}

// HandlerFunc is a function type that responds to an HTTP request.
// It enforces the same contract as http.Handler but provides additional feature
// like matched wildcard route segments via the Context type. The Context is freed once
// the HandlerFunc returns and may be reused later to save resources. If you need
// to hold the context longer, you have to copy it (see Clone method).
//
// Similar to http.Handler, to abort a HandlerFunc so the client sees an interrupted
// response, panic with the value http.ErrAbortHandler.
//
// HandlerFunc functions should be thread-safe, as they will be called concurrently.
type HandlerFunc func(c Context)

// MiddlewareFunc is a function type for implementing HandlerFunc middleware.
// The returned HandlerFunc usually wraps the input HandlerFunc, allowing you to perform operations
// before and/or after the wrapped HandlerFunc is executed. MiddlewareFunc functions should
// be thread-safe, as they will be called concurrently.
type MiddlewareFunc func(next HandlerFunc) HandlerFunc

// Router is a lightweight high performance HTTP request router that support mutation on its routing tree
// while handling request concurrently.
type Router struct {
	noRoute                HandlerFunc
	noMethod               HandlerFunc
	tsrRedirect            HandlerFunc
	autoOptions            HandlerFunc
	tree                   atomic.Pointer[Tree]
	mws                    []middleware
	handleMethodNotAllowed bool
	handleOptions          bool
	redirectTrailingSlash  bool
}

type middleware struct {
	m     MiddlewareFunc
	scope MiddlewareScope
}

var _ http.Handler = (*Router)(nil)

// New returns a ready to use instance of Fox router.
func New(opts ...Option) *Router {
	r := new(Router)

	r.noRoute = DefaultNotFoundHandler()
	r.noMethod = DefaultMethodNotAllowedHandler()
	r.autoOptions = defaultOptionsHandler

	for _, opt := range opts {
		opt.apply(r)
	}

	r.noRoute = applyMiddleware(NoRouteHandler, r.mws, r.noRoute)
	r.noMethod = applyMiddleware(NoMethodHandler, r.mws, r.noMethod)
	r.tsrRedirect = applyMiddleware(RedirectHandler, r.mws, defaultRedirectTrailingSlashHandler)
	r.autoOptions = applyMiddleware(OptionsHandler, r.mws, r.autoOptions)

	r.tree.Store(r.NewTree())
	return r
}

// NewTree returns a fresh routing Tree that inherits all registered router options. It's safe to create multiple Tree
// concurrently. However, a Tree itself is not thread-safe and all its APIs that perform write operations should be run
// serially. Note that a Tree give direct access to the underlying sync.Mutex.
// This API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) NewTree() *Tree {
	tree := new(Tree)
	tree.mws = fox.mws

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

	return tree
}

// Tree atomically loads and return the currently in-use routing tree.
// This API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) Tree() *Tree {
	return fox.tree.Load()
}

// Swap atomically replaces the currently in-use routing tree with the provided new tree, and returns the previous tree.
// This API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) Swap(new *Tree) (old *Tree) {
	return fox.tree.Swap(new)
}

// Handle registers a new handler for the given method and path. This function return an error if the route
// is already registered or conflict with another. It's perfectly safe to add a new handler while the tree is in use
// for serving requests. This function is safe for concurrent use by multiple goroutine.
// To override an existing route, use Update.
func (fox *Router) Handle(method, path string, handler HandlerFunc) error {
	t := fox.Tree()
	t.Lock()
	defer t.Unlock()
	return t.Handle(method, path, handler)
}

// MustHandle registers a new handler for the given method and path. This function is a convenience
// wrapper for the Handle function. It will panic if the route is already registered or conflicts
// with another route. It's perfectly safe to add a new handler while the tree is in use for serving
// requests. This function is safe for concurrent use by multiple goroutines.
// To override an existing route, use Update.
func (fox *Router) MustHandle(method, path string, handler HandlerFunc) {
	if err := fox.Handle(method, path, handler); err != nil {
		panic(err)
	}
}

// Update override an existing handler for the given method and path. If the route does not exist,
// the function return an ErrRouteNotFound. It's perfectly safe to update a handler while the tree is in use for
// serving requests. This function is safe for concurrent use by multiple goroutine.
// To add new handler, use Handle method.
func (fox *Router) Update(method, path string, handler HandlerFunc) error {
	t := fox.Tree()
	t.Lock()
	defer t.Unlock()
	return t.Update(method, path, handler)
}

// Remove delete an existing handler for the given method and path. If the route does not exist, the function
// return an ErrRouteNotFound. It's perfectly safe to remove a handler while the tree is in use for serving requests.
// This function is safe for concurrent use by multiple goroutine.
func (fox *Router) Remove(method, path string) error {
	t := fox.Tree()
	t.Lock()
	defer t.Unlock()
	return t.Remove(method, path)
}

// Lookup performs a manual route lookup for a given http.Request, returning the matched HandlerFunc along with a ContextCloser,
// and a boolean indicating if a trailing slash redirect is recommended. The ContextCloser should always be closed if non-nil.
// This method is primarily intended for integrating the fox router into custom routing solutions. It requires the use of the
// original http.ResponseWriter, typically obtained from ServeHTTP. This function is safe for concurrent use by multiple goroutine
// and while mutation on Tree are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) Lookup(w http.ResponseWriter, r *http.Request) (handler HandlerFunc, cc ContextCloser, tsr bool) {
	tree := fox.tree.Load()

	nds := *tree.nodes.Load()
	index := findRootNode(r.Method, nds)

	if index < 0 {
		return
	}

	c := tree.ctx.Get().(*context)
	c.Reset(fox, w, r)

	target := r.URL.Path
	if len(r.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		target = r.URL.RawPath
	}

	n, tsr := tree.lookup(nds[index], target, c.params, c.skipNds, false)
	if n != nil {
		c.path = n.path
		return n.handler, c, tsr
	}
	c.Close()
	return nil, nil, tsr
}

// SkipMethod is used as a return value from WalkFunc to indicate that
// the method named in the call is to be skipped.
var SkipMethod = errors.New("skip method")

// WalkFunc is the type of the function called by Walk to visit each registered routes.
type WalkFunc func(method, path string, handler HandlerFunc) error

// Walk allow to walk over all registered route in lexicographical order. If the function
// return the special value SkipMethod, Walk skips the current method. This function is
// safe for concurrent use by multiple goroutine and while mutation are ongoing.
// This API is EXPERIMENTAL and is likely to change in future release.
func Walk(tree *Tree, fn WalkFunc) error {
	nds := *tree.nodes.Load()
Next:
	for i := range nds {
		method := nds[i].key
		it := newRawIterator(nds[i])
		for it.hasNext() {
			err := fn(method, it.path, it.current.handler)
			if err != nil {
				if errors.Is(err, SkipMethod) {
					continue Next
				}
				return err
			}
		}

	}
	return nil
}

// DefaultNotFoundHandler returns a simple HandlerFunc that replies to each request
// with a “404 page not found” reply.
func DefaultNotFoundHandler() HandlerFunc {
	return func(c Context) {
		http.Error(c.Writer(), "404 page not found", http.StatusNotFound)
	}
}

// DefaultMethodNotAllowedHandler returns a simple HandlerFunc that replies to each request
// with a “405 Method Not Allowed” reply.
func DefaultMethodNotAllowedHandler() HandlerFunc {
	return func(c Context) {
		http.Error(c.Writer(), http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
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
		url = fixTrailingSlash(req.URL.RawPath)
	} else {
		url = fixTrailingSlash(req.URL.Path)
	}

	if url[len(url)-1] == '/' {
		localRedirect(c.Writer(), req, path.Base(url)+"/", code)
		return
	}
	localRedirect(c.Writer(), req, "../"+path.Base(url), code)
}

func defaultOptionsHandler(c Context) {
	c.Writer().WriteHeader(http.StatusOK)
}

// ServeHTTP is the main entry point to serve a request. It handles all incoming HTTP requests and dispatches them
// to the appropriate handler function based on the request's method and path.
//
// It expects the http.ResponseWriter provided to implement the http.Flusher, http.Hijacker, and io.ReaderFrom
// interfaces for HTTP/1.x requests and the http.Flusher and http.Pusher interfaces for HTTP/2 requests.
// If a custom response writer is used, it is critical to ensure that these methods are properly exposed as Fox
// will invoke them without any prior assertion.
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
	c := tree.ctx.Get().(*context)
	c.Reset(fox, w, r)

	nds := *tree.nodes.Load()
	index := findRootNode(r.Method, nds)
	if index < 0 {
		goto NoMethodFallback
	}

	n, tsr = tree.lookup(nds[index], target, c.params, c.skipNds, false)
	if n != nil {
		c.path = n.path
		n.handler(c)
		// Put back the context, if not extended more than max params or max depth, allowing
		// the slice to naturally grow within the constraint.
		if cap(*c.params) <= int(tree.maxParams.Load()) && cap(*c.skipNds) <= int(tree.maxDepth.Load()) {
			c.tree.ctx.Put(c)
		}
		return
	}

	// Reset params as it may have recorded wildcard segment
	*c.params = (*c.params)[:0]

	if r.Method != http.MethodConnect && r.URL.Path != "/" && tsr && fox.redirectTrailingSlash && target == CleanPath(target) {
		fox.tsrRedirect(c)
		c.Close()
		return
	}

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
			for i := 0; i < len(nds); i++ {
				if n, _ := tree.lookup(nds[i], target, c.params, c.skipNds, true); n != nil {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					} else {
						c.path = n.path
					}
					sb.WriteString(nds[i].key)
				}
			}
		}

		if sb.Len() > 0 {
			sb.WriteString(", ")
			sb.WriteString(http.MethodOptions)
			w.Header().Set("Allow", sb.String())
			fox.autoOptions(c)
			c.Close()
			return
		}
	} else if fox.handleMethodNotAllowed {
		var sb strings.Builder
		for i := 0; i < len(nds); i++ {
			if nds[i].key != r.Method {
				if n, _ := tree.lookup(nds[i], target, c.params, c.skipNds, true); n != nil {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(nds[i].key)
				}
			}
		}
		if sb.Len() > 0 {
			w.Header().Set("Allow", sb.String())
			fox.noMethod(c)
			c.Close()
			return
		}
	}

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

	if n.isCatchAll() {
		routes = append(routes, n.path)
		return routes
	}

	if n.paramChildIndex >= 0 {
		n = n.children[n.paramChildIndex].Load()
	}
	it := newRawIterator(n)
	for it.hasNext() {
		routes = append(routes, it.current.path)
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

func applyMiddleware(scope MiddlewareScope, mws []middleware, h HandlerFunc) HandlerFunc {
	m := h
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i].scope&scope != 0 {
			m = mws[i].m(m)
		}
	}
	return m
}

// localRedirect redirect the client to the new path.
// It does not convert relative paths to absolute paths like Redirect does.
func localRedirect(w http.ResponseWriter, r *http.Request, newPath string, code int) {
	if q := r.URL.RawQuery; q != "" {
		newPath += "?" + q
	}
	w.Header().Set(HeaderLocation, newPath)
	w.WriteHeader(code)
}

// grow increases the slice's capacity, if necessary, to guarantee space for
// another n elements. After Grow(n), at least n elements can be appended
// to the slice without another allocation. If n is negative or too large to
// allocate the memory, Grow panics.
func grow[S ~[]E, E any](s S, n int) S {
	if n < 0 {
		panic("cannot be negative")
	}
	if n -= cap(s) - len(s); n > 0 {
		s = append(s[:cap(s)], make([]E, n)...)[:len(s)]
	}
	return s
}
