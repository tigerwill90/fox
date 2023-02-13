package fox

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
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

type Router struct {
	// User-configurable http.Handler which is called when no matching route is found.
	// By default, http.NotFound is used.
	NotFound http.Handler

	// User-configurable http.Handler which is called when the request cannot be routed,
	// but the same route exist for other methods. The "Allow" header it automatically set
	// before calling the handler. Set HandleMethodNotAllowed to true to enable this option. By default,
	// http.Error with http.StatusMethodNotAllowed is used.
	MethodNotAllowed http.Handler

	// Register a function to handle panics recovered from http handlers.
	PanicHandler func(http.ResponseWriter, *http.Request, interface{})

	tree atomic.Pointer[Tree]

	// If enabled, fox return a 405 Method Not Allowed instead of 404 Not Found when the route exist for another http verb.
	HandleMethodNotAllowed bool

	// Enable automatic redirection fallback when the current request does not match but another handler is found
	// after cleaning up superfluous path elements (see CleanPath). E.g. /../foo/bar request does not match but /foo/bar would.
	// The client is redirected with a http status code 301 for GET requests and 308 for all other methods.
	RedirectFixedPath bool

	// Enable automatic redirection fallback when the current request does not match but another handler is found
	// with/without an additional trailing slash. E.g. /foo/bar/ request does not match but /foo/bar would match.
	// The client is redirected with a http status code 301 for GET requests and 308 for all other methods.
	RedirectTrailingSlash bool
}

var _ http.Handler = (*Router)(nil)

// New returns a ready to use Router.
func New(tree *Tree) *Router {
	r := new(Router)
	r.tree.Store(tree)
	return r
}

// Tree atomically return the tree in use.
func (fox *Router) Tree() *Tree {
	return fox.tree.Load()
}

// Swap atomically replace the in use tree with new provided. It returns the previous value.
func (fox *Router) Swap(new *Tree) (old *Tree) {
	return fox.tree.Swap(new)
}

// Update atomically replace the in use tree with the new provided.
func (fox *Router) Update(new *Tree) {
	fox.tree.Store(new)
}

// Lookup allow to do manual lookup of a route. Please note that you should always release params by calling params.Free(t).
// If lazy is set to true, params are not parsed. This function is safe for concurrent use by multiple goroutine.
func Lookup(tree *Tree, method, path string, lazy bool) (handler Handler, params Params, tsr bool) {
	nds := tree.load()
	index := findRootNode(method, nds)
	if index < 0 {
		return nil, nil, false
	}

	n, ps, tsr := tree.lookup(nds[index], path, lazy)
	if n != nil {
		return n.handler, *ps, tsr
	}
	return nil, nil, tsr
}

// Match perform a lazy lookup and return true if the requested method and path match a registered route.
// This function is safe for concurrent use by multiple goroutine.
func Match(tree *Tree, method, path string) bool {
	nds := tree.load()
	index := findRootNode(method, nds)
	if index < 0 {
		return false
	}

	n, _, _ := tree.lookup(nds[index], path, true)
	return n != nil
}

// SkipMethod is used as a return value from WalkFunc to indicate that
// the method named in the call is to be skipped.
var SkipMethod = errors.New("skip method")

// WalkFunc is the type of the function called by Walk to visit each registered routes.
type WalkFunc func(method, path string, handler Handler) error

// Walk allow to walk over all registered route in lexicographical order. If the function
// return the special value SkipMethod, Walk skips the current method. This function is
// safe for concurrent use by multiple goroutine.
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
	if fox.PanicHandler != nil {
		defer fox.recover(w, r)
	}

	var (
		n      *node
		params *Params
		tsr    bool
	)

	path := r.URL.Path

	tree := fox.Tree()
	nds := tree.load()
	index := findRootNode(r.Method, nds)
	if index < 0 {
		goto NO_METHOD_FALLBACK
	}

	n, params, tsr = tree.lookup(nds[index], path, false)
	if n != nil {
		if params != nil {
			n.handler.ServeHTTP(w, r, *params)
			params.Free(tree)
			return
		}
		n.handler.ServeHTTP(w, r, nil)
		return
	}

	if r.Method != http.MethodConnect && path != "/" {

		code := http.StatusMovedPermanently
		if r.Method != http.MethodGet {
			// Will be redirected only with the same method (SEO friendly)
			code = http.StatusPermanentRedirect
		}

		if tsr && fox.RedirectTrailingSlash {
			r.URL.Path = fixTrailingSlash(path)
			http.Redirect(w, r, r.URL.String(), code)
			return
		}

		if fox.RedirectFixedPath {
			cleanedPath := CleanPath(path)
			n, _, tsr := tree.lookup(nds[index], cleanedPath, true)
			if n != nil {
				r.URL.Path = cleanedPath
				http.Redirect(w, r, r.URL.String(), code)
				return
			}
			if tsr && fox.RedirectTrailingSlash {
				r.URL.Path = fixTrailingSlash(cleanedPath)
				http.Redirect(w, r, r.URL.String(), code)
				return
			}
		}

	}

NO_METHOD_FALLBACK:
	if fox.HandleMethodNotAllowed {
		var sb strings.Builder
		for i := 0; i < len(nds); i++ {
			if nds[i].key != r.Method {
				if n, _, _ := tree.lookup(nds[i], path, true); n != nil {
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
			if fox.MethodNotAllowed != nil {
				fox.MethodNotAllowed.ServeHTTP(w, r)
				return
			}
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
	}

	if fox.NotFound != nil {
		fox.NotFound.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func (fox *Router) recover(w http.ResponseWriter, r *http.Request) {
	if val := recover(); val != nil {
		if abortErr, ok := val.(error); ok && errors.Is(abortErr, http.ErrAbortHandler) {
			panic(abortErr)
		}
		fox.PanicHandler(w, r, val)
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

func parseRoute(path string) (string, string, int, error) {
	if !strings.HasPrefix(path, "/") {
		return "", "", -1, fmt.Errorf("path must start with '/': %w", ErrInvalidRoute)
	}

	routeType := func(key byte) string {
		if key == '*' {
			return "catch all"
		}
		return "param"
	}

	var n int
	p := []byte(path)
	for i, c := range p {
		if c != '*' && c != ':' {
			continue
		}
		n++

		// /foo*
		if p[i-1] != '/' && p[i] == '*' {
			return "", "", -1, fmt.Errorf("missing '/' before catch all route segment: %w", ErrInvalidRoute)
		}

		// /foo/:
		if i == len(p)-1 {
			return "", "", -1, fmt.Errorf("missing argument name after %s operator: %w", routeType(c), ErrInvalidRoute)
		}

		// /foo/:/
		if p[i+1] == '/' {
			return "", "", -1, fmt.Errorf("missing argument name after %s operator: %w", routeType(c), ErrInvalidRoute)
		}

		if c == ':' {
			for k := i + 1; k < len(path); k++ {
				if path[k] == '/' {
					break
				}
				// /foo/:abc:xyz
				if path[k] == ':' {
					return "", "", -1, fmt.Errorf("only one param per path segment is allowed: %w", ErrInvalidRoute)
				}
			}
		}

		if c == '*' {
			for k := i + 1; k < len(path); k++ {
				// /foo/*args/
				if path[k] == '/' || path[k] == ':' {
					return "", "", -1, fmt.Errorf("catch all are allowed only at the end of a route: %w", ErrInvalidRoute)
				}
			}
			return path[:i], path[i+1:], n, nil
		}
	}
	return path, "", n, nil
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
