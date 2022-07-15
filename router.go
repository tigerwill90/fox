package fox

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	verb          = 9
	ParamRouteKey = "$etf1/mux"
)

var (
	validMethods = [verb]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace}
)

var (
	ErrRouteNotFound = errors.New("route not found")
	ErrRouteExist    = errors.New("route already registered")
	ErrRouteConflict = errors.New("route conflict")
	ErrInvalidMethod = errors.New("invalid method")
	ErrInvalidRoute  = errors.New("invalid route")
)

type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request, params Params)
}

type HandlerFunc func(w http.ResponseWriter, r *http.Request, params Params)

func (h HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request, params Params) {
	h(w, r, params)
}

type Router struct {
	NotFound               http.Handler
	MethodNotAllowed       http.Handler
	PanicHandler           func(http.ResponseWriter, *http.Request, interface{})
	HandleMethodNotAllowed bool
	// Enable passing the matching route as a Handler parameter.
	// Usage: p.Get(ParamRouteKey)
	AddRouteParam bool

	// Enable automatic redirection fallback when the current request does not match but another handler is found
	// after cleaning up superfluous path elements (see CleanPath). E.g. /../foo/bar request does not match but /foo/bar would.
	// The client is redirected with a http status code 301 for GET requests and 308 for all other methods.
	RedirectFixedPath bool
	// Enable automatic redirection fallback when the current request does not match but another handler is found
	// with/without an additional trailing slash. E.g. /foo/bar/ request does not match but /foo/bar would match.
	// The client is redirected with a http status code 301 for GET requests and 308 for all other methods.
	RedirectTrailingSlash bool

	mu sync.Mutex

	// trees contains a pointer to the root node for each http method.
	// Once allocated, this is mostly a ready-only array and, only atomic update
	// of the underlying pointer are permitted.
	trees []atomic.Pointer[node]
}

var _ http.Handler = (*Router)(nil)

func New() *Router {
	nds := make([]atomic.Pointer[node], len(validMethods))
	for i := range validMethods {
		nds[i].Store(&node{isRoot: true})
	}

	return &Router{
		trees: nds,
	}
}

// GET is a shortcut for Handler(http.MethodGet, path, handler)
func (mux *Router) GET(path string, handler Handler) error {
	return mux.addRoute(mustIndexOfMethod(http.MethodGet), path, handler)
}

// POST is a shortcut for Handler(http.MethodPost, path, handler)
func (mux *Router) POST(path string, handler Handler) error {
	return mux.addRoute(mustIndexOfMethod(http.MethodPost), path, handler)
}

// PUT is a shortcut for Handler(http.MethodPut, path, handler)
func (mux *Router) PUT(path string, handler Handler) error {
	return mux.addRoute(mustIndexOfMethod(http.MethodPut), path, handler)
}

// PATCH is a shortcut for Handler(http.MethodPatch, path, handler)
func (mux *Router) PATCH(path string, handler Handler) error {
	return mux.addRoute(mustIndexOfMethod(http.MethodPatch), path, handler)
}

// DELETE is a shortcut for Handler(http.MethodDelete, path, handler)
func (mux *Router) DELETE(path string, handler Handler) error {
	return mux.addRoute(mustIndexOfMethod(http.MethodDelete), path, handler)
}

// OPTIONS is a shortcut for Handler(http.MethodOptions, path, handler)
func (mux *Router) OPTIONS(path string, handler Handler) error {
	return mux.addRoute(mustIndexOfMethod(http.MethodOptions), path, handler)
}

// CONNECT is a shortcut for Handler(http.MethodConnect, path, handler)
func (mux *Router) CONNECT(path string, handler Handler) error {
	return mux.addRoute(mustIndexOfMethod(http.MethodConnect), path, handler)
}

// HEAD is a shortcut for Handler(http.MethodHead, path, handler)
func (mux *Router) HEAD(path string, handler Handler) error {
	return mux.addRoute(mustIndexOfMethod(http.MethodHead), path, handler)
}

// TRACE is a shortcut for Handler(http.MethodTrace, path, handler)
func (mux *Router) TRACE(path string, handler Handler) error {
	return mux.addRoute(mustIndexOfMethod(http.MethodTrace), path, handler)
}

// Handler registers a new http.Handler for the given method and path. If the route is already registered,
// the function return an ErrRouteExist. It's perfectly safe to add a new handler once the server is started.
// This function is safe for concurrent use by multiple goroutine.
// To override an existing route, use Update method.
func (mux *Router) Handler(method, path string, handler Handler) error {
	idx := indexOfMethod(method)
	if idx < 0 {
		return ErrInvalidMethod
	}
	return mux.addRoute(idx, path, handler)
}

// Update override an existing handler for the given method and path. If the route does not exist,
// the function return an ErrRouteNotFound. It's perfectly safe to update a handler once the server
// is started. This function is safe for concurrent use by multiple goroutine.
// To add new handler, use Handler method.
func (mux *Router) Update(method, path string, handler Handler) error {
	idx := indexOfMethod(method)
	if idx < 0 {
		return ErrInvalidMethod
	}

	end, isWildcard, err := parseRoute(path)
	if err != nil {
		return err
	}
	var wildcardKey string
	if isWildcard {
		wildcardKey = path[end+1:]
	}

	mux.mu.Lock()
	defer mux.mu.Unlock()

	return mux.update(idx, path[:end], wildcardKey, handler, isWildcard)
}

func (mux *Router) Upsert(method, path string, handler Handler) error {
	idx := indexOfMethod(method)
	if idx < 0 {
		return ErrInvalidMethod
	}

	end, isWildcard, err := parseRoute(path)
	if err != nil {
		return err
	}
	var wildcardKey string

	mux.mu.Lock()
	defer mux.mu.Unlock()

	if isWildcard {
		wildcardKey = path[end+1:]
		updateMaxParams(1)
	}

	if err = mux.insert(idx, path[:end], wildcardKey, handler, isWildcard); errors.Is(err, ErrRouteExist) {
		return mux.update(idx, path[:end], wildcardKey, handler, isWildcard)
	}
	return err
}

// Remove delete an existing handler for the given method and path. If the route does not exist, the function
// return an ErrRouteNotFound. It's perfectly safe to remove a handler once the server is started. This
// function is safe for concurrent use by multiple goroutine.
func (mux *Router) Remove(method, path string) error {
	idx := indexOfMethod(method)
	if idx < 0 {
		return fmt.Errorf("%s method is not supported: %w", method, ErrInvalidMethod)
	}

	end, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	if !mux.remove(mustIndexOfMethod(method), path[:end]) {
		return ErrRouteNotFound
	}
	return nil
}

// Lookup allow to do  manual lookup of a route. Params passed in callback, are only valid within the callback.
// This function is safe for concurrent use by multiple goroutine.
func (mux *Router) Lookup(method, path string, fn func(handler Handler, params Params, tsr bool)) error {
	idx := indexOfMethod(method)
	if idx < 0 {
		return fmt.Errorf("%s method is not supported: %w", method, ErrInvalidMethod)
	}
	n, params, tsr := mux.lookup(idx, path, false)
	if n != nil {
		fn(n.handler, *params, tsr)
	} else {
		fn(nil, nil, tsr)
	}
	params.free()
	return nil
}

type Route struct {
	Method      string
	Path        string
	WildcardKey string
	isCatchAll  bool
}

func (r Route) IsCatchAll() bool {
	return r.isCatchAll
}

func (r Route) String() string {
	sb := strings.Builder{}
	sb.WriteString(r.Path)
	if r.isCatchAll {
		sb.WriteByte('*')
		sb.WriteString(r.WildcardKey)
	}
	return sb.String()
}

type WalkFunc func(r Route, handler Handler) error

var ErrSkipMethod = errors.New("skip method")

// WalkRoute allow to walk over all registered route in lexicographical order. This function is safe for
// concurrent use by multiple goroutine.
func (mux *Router) WalkRoute(fn WalkFunc) error {
METHODS:
	for i := range mux.trees {
		it := newIterator(mux.getRoot(i))
		for it.hasNextLeaf() {
			err := fn(Route{
				Method:      validMethods[i],
				Path:        it.fullPath(),
				WildcardKey: it.node().wildcardKey,
				isCatchAll:  it.node().wildcard,
			}, it.node().handler)
			if err != nil {
				if errors.Is(err, ErrSkipMethod) {
					continue METHODS
				}
				return err
			}
		}

	}
	return nil
}

func (mux *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if mux.PanicHandler != nil {
		defer mux.recover(w, r)
	}

	idx := indexOfMethod(r.Method)
	if idx < 0 {
		if mux.MethodNotAllowed != nil {
			mux.MethodNotAllowed.ServeHTTP(w, r)
			return
		}
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Path
	n, params, tsr := mux.lookup(idx, path, false)
	if n != nil {
		if params != nil {
			n.handler.ServeHTTP(w, r, *params)
			params.free()
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

		if tsr && mux.RedirectTrailingSlash {
			r.URL.Path = fixTrailingSlash(path)
			http.Redirect(w, r, r.URL.String(), code)
			return
		}

		if mux.RedirectFixedPath {
			cleanedPath := CleanPath(path)
			n, _, tsr := mux.lookup(idx, cleanedPath, true)
			if n != nil {
				r.URL.Path = cleanedPath
				http.Redirect(w, r, r.URL.String(), code)
				return
			}
			if tsr && mux.RedirectTrailingSlash {
				r.URL.Path = fixTrailingSlash(cleanedPath)
				http.Redirect(w, r, r.URL.String(), code)
				return
			}
		}

	}

	if mux.HandleMethodNotAllowed {
		var sb strings.Builder
		for i := 0; i < len(validMethods); i++ {
			if i != idx {
				if n, _, _ := mux.lookup(i, path, true); n != nil {
					if sb.Len() > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(validMethods[i])
				}
			}
		}
		allowed := sb.String()
		if allowed != "" {
			w.Header().Set("Allow", allowed)
			if mux.MethodNotAllowed != nil {
				mux.MethodNotAllowed.ServeHTTP(w, r)
				return
			}
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
	}

	if mux.NotFound != nil {
		mux.NotFound.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func (mux *Router) addRoute(idx int, path string, handler Handler) error {
	end, isWildcard, err := parseRoute(path)
	if err != nil {
		return err
	}
	var wildcardKey string

	mux.mu.Lock()
	defer mux.mu.Unlock()

	if isWildcard {
		wildcardKey = path[end+1:]
		updateMaxParams(1)
	}

	return mux.insert(idx, path[:end], wildcardKey, handler, isWildcard)
}

func (mux *Router) recover(w http.ResponseWriter, r *http.Request) {
	if val := recover(); val != nil {
		mux.PanicHandler(w, r, val)
	}
}

func (mux *Router) lookup(index int, path string, lazy bool) (n *node, params *Params, tsr bool) {
	var (
		charsMatched            int
		charsMatchedInNodeFound int
	)

	current := mux.getRoot(index)
STOP:
	for charsMatched < len(path) {
		next := current.getEdge(path[charsMatched])
		if next == nil {
			break STOP
		}

		current = next
		charsMatchedInNodeFound = 0
		for i := 0; charsMatched < len(path); i++ {
			if i >= len(current.path) {
				break
			}

			if current.path[i] != path[charsMatched] {
				break STOP
			}

			charsMatched++
			charsMatchedInNodeFound++
		}
	}

	if !current.isLeaf() {
		return nil, params, false
	}

	if charsMatched == len(path) {
		if charsMatchedInNodeFound == len(current.path) {
			// Exact match, note that if we match a wildcard node, there is no remaining char to match. So we can
			// safely avoid the extra cost of passing an empty slice params.
			if !lazy && mux.AddRouteParam {
				p := newParams()
				*p = append(*p, Param{Key: ParamRouteKey, Value: current.fullPath})
				return current, p, false
			}

			return current, params, false
		} else if charsMatchedInNodeFound < len(current.path) {
			// Key end mid-edge
			// Tsr recommendation: add an extra trailing slash (got an exact match)
			remainingSuffix := current.path[charsMatchedInNodeFound:]
			return nil, params, len(remainingSuffix) == 1 && remainingSuffix[0] == '/'
		}
	}

	// Incomplete match to end of edge
	if charsMatched < len(path) && charsMatchedInNodeFound == len(current.path) {
		if current.wildcard {
			if !lazy {
				p := newParams()
				*p = append(*p, Param{Key: current.wildcardKey, Value: path[charsMatched:]})
				if mux.AddRouteParam {
					*p = append(*p, Param{Key: ParamRouteKey, Value: current.fullPath})
				}
				return current, p, false
			}
			// Same as exact match, no tsr recommendation
			return current, params, false
		}
		// Tsr recommendation: remove the extra trailing slash (got an exact match)
		remainingKeySuffix := path[charsMatched:]
		return nil, params, len(remainingKeySuffix) == 1 && remainingKeySuffix[0] == '/'
	}

	return nil, params, false
}

func (mux *Router) getRoot(index int) *node {
	// ptrsTree := *(*[]*unsafe.Pointer)(atomic.LoadPointer(mux.trees2))
	return mux.trees[index].Load()
}

func (mux *Router) updateRoot(index int, node *node) {
	mux.trees[index].Store(node)
}

func (mux *Router) update(index int, path, wildcardKey string, handler Handler, isWildcard bool) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	result := mux.search(index, path)
	if !result.isExactMatch() || !result.matched.isLeaf() {
		return fmt.Errorf("route /%s %s is not registered: %w", validMethods[index], path, ErrRouteNotFound)
	}

	if isWildcard && len(result.matched.children) > 0 {
		return newConflictErr(index, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched)[1:], isWildcard)
	}

	// We are updating an existing node (could be a leaf or not). We only need to create a new node from
	// the matched one with the updated/added value (handler and wildcard).
	n := newNodeFromRef(result.matched.path, handler, result.matched.children, result.matched.childKeys, wildcardKey, isWildcard)
	result.p.updateEdge(n)
	return nil
}

func (mux *Router) insert(index int, path, wildcardKey string, handler Handler, isWildcard bool) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	result := mux.search(index, path)
	switch result.classify() {
	case exactMatch:
		// e.g. matched exactly "te" node when inserting "te" key.
		// te
		// ├── st
		// └── am
		// Create a new node from "st" reference and update the "te" (parent) reference to "st" node.
		if result.matched.isLeaf() {
			return fmt.Errorf("route /%s %s is already registered: %w", validMethods[index], path, ErrRouteExist)
		}

		// The matched node can only be the result of a previous split and therefore has children.
		if isWildcard {
			return newConflictErr(index, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched), isWildcard)
		}
		// We are updating an existing node. We only need to create a new node from
		// the matched one with the updated/added value (handler and wildcard).
		n := newNodeFromRef(result.matched.path, handler, result.matched.children, result.matched.childKeys, wildcardKey, isWildcard)
		result.p.updateEdge(n)
	case keyEndMidEdge:
		// e.g. matched until "s" for "st" node when inserting "tes" key.
		// te
		// ├── st
		// └── am
		//
		// After patching
		// te
		// ├── am
		// └── s
		//     └── t
		// It requires to split "st" node.
		// 1. Create a "t" node from "st" reference.
		// 2. Create a new "s" node for "tes" key and link it to the child "t" node.
		// 3. Update the "te" (parent) reference to the new "s" node (we are swapping old "st" to new "s" node, first
		//    char remain the same).

		if isWildcard {
			return newConflictErr(index, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched), isWildcard)
		}

		keyCharsFromStartOfNodeFound := path[result.charsMatched-result.charsMatchedInNodeFound:]
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.path)
		suffixFromExistingEdge := strings.TrimPrefix(result.matched.path, cPrefix)

		child := newNodeFromRef(suffixFromExistingEdge, result.matched.handler, result.matched.children, result.matched.childKeys, result.matched.wildcardKey, result.matched.wildcard)
		parent := newNode(cPrefix, handler, []*node{child}, wildcardKey, isWildcard)

		result.p.updateEdge(parent)
	case incompleteMatchToEndOfEdge:
		// e.g. matched until "st" for "st" node but still have remaining char (ify) when inserting "testify" key.
		// te
		// ├── st
		// └── am
		//
		// After patching
		// te
		// ├── am
		// └── st
		//     └── ify
		// 1. Create a new "ify" child node.
		// 2. Recreate the "st" node and link it to it's existing children and the new "ify" node.
		// 3. Update the "te" (parent) node to the new "st" node.

		if result.matched.wildcard {
			return newConflictErr(index, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched), isWildcard)
		}

		keySuffix := path[result.charsMatched:]
		child := newNode(keySuffix, handler, nil, wildcardKey, isWildcard)
		edges := result.matched.getEdgesShallowCopy()
		edges = append(edges, child)
		n := newNode(result.matched.path, result.matched.handler, edges, result.matched.wildcardKey, result.matched.wildcard)
		if result.matched == mux.getRoot(index) {
			n.isRoot = true
			mux.updateRoot(index, n)
			break
		}
		result.p.updateEdge(n)
	case incompleteMatchToMiddleOfEdge:
		// e.g. matched until "s" for "st" node but still have remaining char ("s") which does not match anything
		// when inserting "tess" key.
		// te
		// ├── st
		// └── am
		//
		// After patching
		// te
		// ├── am
		// └── s
		//     ├── s
		//     └── t
		// It requires to split "st" node.
		// 1. Create a new "s" child node for "tess" key.
		// 2. Create a new "t" node from "st" reference (link "st" children to new "t" node).
		// 3. Create a new "s" node and link it to "s" and "t" node.
		// 4. Update the "te" (parent) node to the new "s" node (we are swapping old "st" to new "s" node, first
		//    char remain the same).

		keyCharsFromStartOfNodeFound := path[result.charsMatched-result.charsMatchedInNodeFound:]
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.path)
		suffixFromExistingEdge := strings.TrimPrefix(result.matched.path, cPrefix)
		keySuffix := path[result.charsMatched:]

		n1 := newNodeFromRef(keySuffix, handler, nil, nil, wildcardKey, isWildcard)
		n2 := newNodeFromRef(suffixFromExistingEdge, result.matched.handler, result.matched.children, result.matched.childKeys, result.matched.wildcardKey, result.matched.wildcard)
		n3 := newNode(cPrefix, nil, []*node{n1, n2}, "", false)
		result.p.updateEdge(n3)
	default:
		// safeguard against introducing a new result type
		panic("internal error: unexpected result type")
	}
	return nil
}

func (mux *Router) remove(index int, path string) bool {
	mux.mu.Lock()
	defer mux.mu.Unlock()
	result := mux.search(index, path)
	if result.classify() != exactMatch {
		return false
	}

	// This node was created after a split (KEY_END_MID_EGGE operation), therefore we cannot delete
	// this node.
	if !result.matched.isLeaf() {
		return false
	}

	if len(result.matched.children) > 1 {
		n := newNodeFromRef(result.matched.path, nil, result.matched.children, result.matched.childKeys, "", false)
		result.p.updateEdge(n)
		return true
	}

	if len(result.matched.children) == 1 {
		child := result.matched.get(0)
		mergedPath := fmt.Sprintf("%s%s", result.matched.path, child.path)
		n := newNodeFromRef(mergedPath, child.handler, child.children, child.childKeys, child.wildcardKey, child.wildcard)
		result.p.updateEdge(n)
		return true
	}

	// recreate the parent edges without the removed node
	parentEdges := make([]*node, len(result.p.children)-1)
	added := 0
	for i := 0; i < len(result.p.children); i++ {
		n := result.p.get(i)
		if n != result.matched {
			parentEdges[added] = n
			added++
		}
	}

	parentIsRoot := result.p == mux.getRoot(index)
	var parent *node
	if len(parentEdges) == 1 && !result.p.isLeaf() && !parentIsRoot {
		child := parentEdges[0]
		mergedPath := fmt.Sprintf("%s%s", result.p.path, child.path)
		parent = newNodeFromRef(mergedPath, child.handler, child.children, child.childKeys, child.wildcardKey, child.wildcard)
	} else {
		parent = newNode(result.p.path, result.p.handler, parentEdges, result.p.wildcardKey, result.p.wildcard)
	}
	if parentIsRoot {
		parent.isRoot = true
		mux.updateRoot(index, parent)
		return true
	}

	result.pp.updateEdge(parent)
	return true
}

func (mux *Router) search(index int, path string) searchResult {
	current := mux.getRoot(index)
	var (
		pp                      *node
		p                       *node
		charsMatched            int
		charsMatchedInNodeFound int
	)

STOP:
	for charsMatched < len(path) {
		next := current.getEdge(path[charsMatched])
		if next == nil {
			break STOP
		}

		pp = p
		p = current
		current = next
		charsMatchedInNodeFound = 0
		for i := 0; charsMatched < len(path); i++ {
			if i >= len(current.path) {
				break
			}

			if current.path[i] != path[charsMatched] {
				break STOP
			}

			charsMatched++
			charsMatchedInNodeFound++
		}
	}

	return searchResult{
		path:                    path,
		matched:                 current,
		charsMatched:            charsMatched,
		charsMatchedInNodeFound: charsMatchedInNodeFound,
		p:                       p,
		pp:                      pp,
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
		if r.charsMatchedInNodeFound == len(r.matched.path) {
			return exactMatch
		}
		if r.charsMatchedInNodeFound < len(r.matched.path) {
			return keyEndMidEdge
		}
	} else if r.charsMatched < len(r.path) {
		if r.charsMatchedInNodeFound == len(r.matched.path) {
			return incompleteMatchToEndOfEdge
		}
		if r.charsMatchedInNodeFound < len(r.matched.path) {
			return incompleteMatchToMiddleOfEdge
		}
	}
	panic("internal error: cannot classify the result")
}
func (r searchResult) isExactMatch() bool {
	return r.charsMatched == len(r.path) && r.charsMatchedInNodeFound == len(r.matched.path)
}

func (r searchResult) isIncompleteMatchToEndOfEdge() bool {
	return r.charsMatched < len(r.path) && r.charsMatchedInNodeFound == len(r.matched.path)
}

func (r searchResult) isKeyMidEdge() bool {
	return r.charsMatched == len(r.path) && r.charsMatchedInNodeFound < len(r.matched.path)
}

func (r *searchResult) isIncompleteMatchToMiddleOfEdge() bool {
	return r.charsMatched < len(r.path) && r.charsMatchedInNodeFound < len(r.matched.path)
}

func (c resultType) String() string {
	return [...]string{"EXACT_MATCH", "INCOMPLETE_MATCH_TO_END_OF_EDGE", "INCOMPLETE_MATCH_TO_MIDDLE_OF_EDGE", "KEY_END_MID_EDGE"}[c]
}

type searchResult struct {
	path                    string
	matched                 *node
	charsMatched            int
	charsMatchedInNodeFound int
	p                       *node
	pp                      *node
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

// indexOfMethod return the index of the corresponding root node for a http method or -1.
// Yes this is ugly, but it's at least 10x - 20x faster than a map or a for loop to retrieve the root
// node. Note that since the tree array is immutable, we are forced to create an empty root node
// for each method even if there is no route registered.
func indexOfMethod(method string) int {
	switch method {
	case http.MethodGet:
		return 0
	case http.MethodPost:
		return 1
	case http.MethodPut:
		return 2
	case http.MethodDelete:
		return 3
	case http.MethodPatch:
		return 4
	case http.MethodConnect:
		return 5
	case http.MethodOptions:
		return 6
	case http.MethodHead:
		return 7
	case http.MethodTrace:
		return 8
	default:
		for i := 9; i < len(validMethods); i++ {
			if method == validMethods[i] {
				return i
			}
		}
	}
	return -1
}

func mustIndexOfMethod(method string) int {
	idx := indexOfMethod(method)
	if idx < 0 {
		panic(fmt.Sprintf("internal error: unsupported %s method", method))
	}
	return idx
}

func parseRoute(path string) (end int, isWildcard bool, err error) {
	if !strings.HasPrefix(path, "/") {
		return -1, false, fmt.Errorf("path must start with '/': %w", ErrInvalidRoute)
	}

	p := []byte(path)
	for i, c := range p {
		if c != '*' {
			continue
		}

		if p[i-1] != '/' {
			return -1, false, fmt.Errorf("missing '/' before wildcard route segment: %w", ErrInvalidRoute)
		}

		if i == len(p)-1 {
			return -1, false, fmt.Errorf("missing argument name after wildcard operator: %w", ErrInvalidRoute)
		}

		for k := i + 1; k < len(path); k++ {
			if path[k] == '/' {
				return -1, false, fmt.Errorf("wildcard are supported only at the end of a route: %w", ErrInvalidRoute)
			}
		}

		return i, true, nil
	}

	return len(path), false, nil
}

func getRouteConflict(basePath string, n *node) []string {
	routes := make([]string, 0)
	it := newIterator(n)
	for it.hasNextLeaf() {
		path := it.fullPath()
		if it.current.wildcard {
			path += "*"
		}
		routes = append(routes, basePath+path)
	}
	return routes
}
