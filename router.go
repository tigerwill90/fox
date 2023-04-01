package fox

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

const verb = 4

var commonVerbs = [verb]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}

// Handler respond to an HTTP request.
//
// This interface enforce the same contract as http.Handler except that matched wildcard route segment
// are accessible via params. Params slice is freed once ServeHTTP returns and may be reused later to
// save resource. Therefore, if you need to hold params slice longer, you have to copy it (see Clone method).
//
// As for http.Handler interface, to abort a handler so the client sees an interrupted response, panic with
// the value http.ErrAbortHandler.
type Handler interface {
	ServeHTTP(http.ResponseWriter, *http.Request, Params)
}

// HandlerFunc is an adapter to allow the use of ordinary functions as HTTP handlers. If f is a function with the
// appropriate signature, HandlerFunc(f) is a Handler that calls f.
type HandlerFunc func(http.ResponseWriter, *http.Request, Params)

// ServerHTTP calls f(w, r, params)
func (f HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request, params Params) {
	f(w, r, params)
}

// Router is a lightweight high performance HTTP request router that support mutation on its routing tree
// while handling request concurrently.
type Router struct {
	// User-configurable http.Handler which is called when no matching route is found.
	// By default, http.NotFound is used.
	notFound http.Handler

	// User-configurable http.Handler which is called when the request cannot be routed,
	// but the same route exist for other methods. The "Allow" header it automatically set
	// before calling the handler. Set HandleMethodNotAllowed to true to enable this option. By default,
	// http.Error with http.StatusMethodNotAllowed is used.
	methodNotAllowed http.Handler

	// Register a function to handle panics recovered from http handlers.
	panicHandler func(http.ResponseWriter, *http.Request, interface{})

	tree atomic.Pointer[Tree]

	// If enabled, fox return a 405 Method Not Allowed instead of 404 Not Found when the route exist for another http verb.
	handleMethodNotAllowed bool

	// Enable automatic redirection fallback when the current request does not match but another handler is found
	// after cleaning up superfluous path elements (see CleanPath). E.g. /../foo/bar request does not match but /foo/bar would.
	// The client is redirected with a http status code 301 for GET requests and 308 for all other methods.
	redirectFixedPath bool

	// Enable automatic redirection fallback when the current request does not match but another handler is found
	// with/without an additional trailing slash. E.g. /foo/bar/ request does not match but /foo/bar would match.
	// The client is redirected with a http status code 301 for GET requests and 308 for all other methods.
	redirectTrailingSlash bool

	// If enabled, the matched route will be accessible as a Handler parameter.
	// Usage: p.Get(fox.RouteKey)
	saveMatchedRoute bool
}

var _ http.Handler = (*Router)(nil)

// New returns a ready to use Router.
func New(opts ...Option) *Router {
	r := new(Router)
	for _, opt := range opts {
		opt.apply(r)
	}
	r.tree.Store(r.NewTree())
	return r
}

// NewTree returns a fresh routing Tree which allow to register, update and delete route.
// It's safe to create multiple Tree concurrently. However, a Tree itself is not thread safe
// and all its APIs should be run serially. Note that a Tree give direct access to the
// underlying sync.Mutex.
// This api is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) NewTree() *Tree {
	tree := new(Tree)
	tree.saveRoute = fox.saveMatchedRoute
	// Pre instantiate nodes for common http verb
	nds := make([]*node, len(commonVerbs))
	for i := range commonVerbs {
		nds[i] = new(node)
		nds[i].key = commonVerbs[i]
		nds[i].paramChildIndex = -1
	}
	tree.nodes.Store(&nds)

	tree.pp = sync.Pool{
		New: func() any {
			params := make(Params, 0, tree.maxParams.Load())
			return &params
		},
	}

	tree.np = sync.Pool{
		New: func() any {
			skippedNodes := make([]skippedNode, 0, tree.maxDepth.Load())
			return &skippedNodes
		},
	}

	return tree
}

// Handler registers a new handler for the given method and path. This function return an error if the route
// is already registered or conflict with another. It's perfectly safe to add a new handler while the tree is in use
// for serving requests. This function is safe for concurrent use by multiple goroutine.
// To override an existing route, use Update.
func (fox *Router) Handler(method, path string, handler Handler) error {
	t := fox.Tree()
	t.Lock()
	defer t.Unlock()
	return t.Handler(method, path, handler)
}

// Update override an existing handler for the given method and path. If the route does not exist,
// the function return an ErrRouteNotFound. It's perfectly safe to update a handler while the tree is in use for
// serving requests. This function is safe for concurrent use by multiple goroutine.
// To add new handler, use Handler method.
func (fox *Router) Update(method, path string, handler Handler) error {
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

// Use atomically replaces the currently in-use routing tree with the provided new tree.
// This API is EXPERIMENTAL and is likely to change in future release.
func (fox *Router) Use(new *Tree) {
	fox.tree.Store(new)
}

// Lookup allow to do manual lookup of a route and return the matched handler along with parsed params and
// trailing slash redirect recommendation. Note that you should always free Params if NOT nil by calling
// params.Free(t). If lazy is set to true, route params are not parsed. This function is safe for concurrent use
// by multiple goroutine and while mutation on Tree are ongoing.
func Lookup(t *Tree, method, path string, lazy bool) (handler Handler, params *Params, tsr bool) {
	nds := t.load()
	index := findRootNode(method, nds)
	if index < 0 {
		return nil, nil, false
	}

	n, ps, tsr := t.lookup(nds[index], path, lazy)
	if n != nil {
		return n.handler, ps, tsr
	}
	return nil, nil, tsr
}

// Has allows to check if the given method and path exactly match a registered route. This function is safe for
// concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This api is EXPERIMENTAL and is likely to change in future release.
func Has(t *Tree, method, path string) bool {
	nds := t.load()
	index := findRootNode(method, nds)
	if index < 0 {
		return false
	}

	n, _, _ := t.lookup(nds[index], path, true)
	return n != nil && n.path == path
}

// Reverse perform a lookup on the tree for the given method and path and return the matching registered route if any.
// This function is safe for concurrent use by multiple goroutine and while mutation on Tree are ongoing.
// This api is EXPERIMENTAL and is likely to change in future release.
func Reverse(t *Tree, method, path string) string {
	nds := t.load()
	index := findRootNode(method, nds)
	if index < 0 {
		return ""
	}
	n, _, _ := t.lookup(nds[index], path, true)
	if n == nil {
		return ""
	}
	return n.path
}

// SkipMethod is used as a return value from WalkFunc to indicate that
// the method named in the call is to be skipped.
var SkipMethod = errors.New("skip method")

// WalkFunc is the type of the function called by Walk to visit each registered routes.
type WalkFunc func(method, path string, handler Handler) error

// Walk allow to walk over all registered route in lexicographical order. If the function
// return the special value SkipMethod, Walk skips the current method. This function is
// safe for concurrent use by multiple goroutine and while mutation are ongoing.
// This api is EXPERIMENTAL and is likely to change in future release.
func Walk(tree *Tree, fn WalkFunc) error {
	nds := tree.load()
NEXT:
	for i := range nds {
		method := nds[i].key
		it := newRawIterator(nds[i])
		for it.hasNext() {
			err := fn(method, it.fullPath(), it.node().handler)
			if err != nil {
				if errors.Is(err, SkipMethod) {
					continue NEXT
				}
				return err
			}
		}

	}
	return nil
}

func (fox *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if fox.panicHandler != nil {
		defer fox.recover(w, r)
	}

	var (
		n      *node
		params *Params
		tsr    bool
	)

	tree := fox.Tree()
	nds := tree.load()
	index := findRootNode(r.Method, nds)
	if index < 0 {
		goto NO_METHOD_FALLBACK
	}

	n, params, tsr = tree.lookup(nds[index], r.URL.Path, false)
	if n != nil {
		if params != nil {
			n.handler.ServeHTTP(w, r, *params)
			params.Free(tree)
			return
		}
		n.handler.ServeHTTP(w, r, nil)
		return
	}

	if r.Method != http.MethodConnect && r.URL.Path != "/" {

		code := http.StatusMovedPermanently
		if r.Method != http.MethodGet {
			// Will be redirected only with the same method (SEO friendly)
			code = http.StatusPermanentRedirect
		}

		if tsr && fox.redirectTrailingSlash {
			r.URL.Path = fixTrailingSlash(r.URL.Path)
			http.Redirect(w, r, r.URL.String(), code)
			return
		}

		if fox.redirectFixedPath {
			cleanedPath := CleanPath(r.URL.Path)
			n, _, tsr := tree.lookup(nds[index], cleanedPath, true)
			if n != nil {
				r.URL.Path = cleanedPath
				http.Redirect(w, r, r.URL.String(), code)
				return
			}
			if tsr && fox.redirectTrailingSlash {
				r.URL.Path = fixTrailingSlash(cleanedPath)
				http.Redirect(w, r, r.URL.String(), code)
				return
			}
		}

	}

NO_METHOD_FALLBACK:
	if fox.handleMethodNotAllowed {
		var sb strings.Builder
		for i := 0; i < len(nds); i++ {
			if nds[i].key != r.Method {
				if n, _, _ := tree.lookup(nds[i], r.URL.Path, true); n != nil {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(nds[i].key)
				}
			}
		}
		allowed := sb.String()
		if allowed != "" {
			w.Header().Set("Allow", allowed)
			if fox.methodNotAllowed != nil {
				fox.methodNotAllowed.ServeHTTP(w, r)
				return
			}
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
	}

	if fox.notFound != nil {
		fox.notFound.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func (fox *Router) recover(w http.ResponseWriter, r *http.Request) {
	if val := recover(); val != nil {
		if abortErr, ok := val.(error); ok && errors.Is(abortErr, http.ErrAbortHandler) {
			panic(abortErr)
		}
		fox.panicHandler(w, r, val)
	}
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
		return "", "", -1, fmt.Errorf("path must start with '/': %w", ErrInvalidRoute)
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
