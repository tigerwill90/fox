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
	verb          = 4
	ParamRouteKey = "$fox/path"
)

var (
	commonVerbs = [verb]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}
)

var (
	ErrRouteNotFound = errors.New("route not found")
	ErrRouteExist    = errors.New("route already registered")
	ErrRouteConflict = errors.New("route conflict")
	ErrInvalidRoute  = errors.New("invalid route")
	ErrSkipMethod    = errors.New("skip method")
)

type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request, params Params)
}

type HandlerFunc func(w http.ResponseWriter, r *http.Request, params Params)

func (h HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request, params Params) {
	h(w, r, params)
}

type Router struct {
	NotFound         http.Handler
	MethodNotAllowed http.Handler
	PanicHandler     func(http.ResponseWriter, *http.Request, interface{})

	// If enabled, fox return a 405 Method Not Allowed instead of 404 Not Found when the route exist for another http verb.
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

	// TODO may remove the pointer here
	trees atomic.Pointer[[]*node]
}

var _ http.Handler = (*Router)(nil)

func New() *Router {
	var ptr atomic.Pointer[[]*node]
	// Pre instantiate nodes for common http verb
	nds := make([]*node, len(commonVerbs))
	for i := range commonVerbs {
		nds[i] = new(node)
		nds[i].key = commonVerbs[i]
	}
	ptr.Store(&nds)

	return &Router{
		trees: ptr,
	}
}

// Get is a shortcut for Handler(http.MethodGet, path, handler)
func (fox *Router) Get(path string, handler Handler) error {
	return fox.addRoute(http.MethodGet, path, handler)
}

// Post is a shortcut for Handler(http.MethodPost, path, handler)
func (fox *Router) Post(path string, handler Handler) error {
	return fox.addRoute(http.MethodPost, path, handler)
}

// Put is a shortcut for Handler(http.MethodPut, path, handler)
func (fox *Router) Put(path string, handler Handler) error {
	return fox.addRoute(http.MethodPut, path, handler)
}

// Delete is a shortcut for Handler(http.MethodDelete, path, handler)
func (fox *Router) Delete(path string, handler Handler) error {
	return fox.addRoute(http.MethodDelete, path, handler)
}

// Handler registers a new http.Handler for the given method and path. If the route is already registered,
// the function return an ErrRouteExist. It's perfectly safe to add a new handler once the server is started.
// This function is safe for concurrent use by multiple goroutine.
// To override an existing route, use Update method.
func (fox *Router) Handler(method, path string, handler Handler) error {
	return fox.addRoute(method, path, handler)
}

// Update override an existing handler for the given method and path. If the route does not exist,
// the function return an ErrRouteNotFound. It's perfectly safe to update a handler once the server
// is started. This function is safe for concurrent use by multiple goroutine.
// To add new handler, use Handler method.
func (fox *Router) Update(method, path string, handler Handler) error {
	p, catchAllKey, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	fox.mu.Lock()
	defer fox.mu.Unlock()

	return fox.update(method, p, catchAllKey, handler)
}

func (fox *Router) Upsert(method, path string, handler Handler) error {
	p, catchAllKey, n, err := parseRoute(path)
	if err != nil {
		return err
	}

	fox.mu.Lock()
	defer fox.mu.Unlock()

	updateMaxParams(uint32(n))

	if err = fox.insert(method, p, catchAllKey, handler); errors.Is(err, ErrRouteExist) {
		return fox.update(method, p, catchAllKey, handler)
	}
	return err
}

// Remove delete an existing handler for the given method and path. If the route does not exist, the function
// return an ErrRouteNotFound. It's perfectly safe to remove a handler once the server is started. This
// function is safe for concurrent use by multiple goroutine.
func (fox *Router) Remove(method, path string) error {
	fox.mu.Lock()
	defer fox.mu.Unlock()

	path, _, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	if !fox.remove(method, path) {
		return ErrRouteNotFound
	}
	return nil
}

// Lookup allow to do  manual lookup of a route. Params passed in callback, are only valid within the callback.
// This function is safe for concurrent use by multiple goroutine.
func (fox *Router) Lookup(method, path string, fn func(handler Handler, params Params, tsr bool)) {
	nds := *fox.trees.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		fn(nil, nil, false)
		return
	}

	n, params, tsr := fox.lookup(nds[index], path, false)
	if n != nil {
		fn(n.handler, *params, tsr)
	} else {
		fn(nil, nil, tsr)
	}
	params.free()
	return
}

type Route struct {
	Method      string
	Path        string
	CatchAllKey string
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
		sb.WriteString(r.CatchAllKey)
	}
	return sb.String()
}

type WalkFunc func(r Route, handler Handler) error

// WalkRoute allow to walk over all registered route in lexicographical order. This function is safe for
// concurrent use by multiple goroutine.
func (fox *Router) WalkRoute(fn WalkFunc) error {
	nds := *fox.trees.Load()
NEXT:
	for i := range nds {
		method := nds[i].key
		it := newIterator(nds[i])
		for it.hasNextLeaf() {
			err := fn(Route{
				Method:      method,
				Path:        it.fullPath(),
				CatchAllKey: it.node().catchAllKey,
				isCatchAll:  it.node().isCatchAll(),
			}, it.node().handler)
			if err != nil {
				if errors.Is(err, ErrSkipMethod) {
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

	nds := *fox.trees.Load()
	index := findRootNode(r.Method, nds)
	if index < 0 {
		goto NO_METHOD_FALLBACK
	}

	n, params, tsr = fox.lookup(nds[index], path, false)
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

		if tsr && fox.RedirectTrailingSlash {
			r.URL.Path = fixTrailingSlash(path)
			http.Redirect(w, r, r.URL.String(), code)
			return
		}

		if fox.RedirectFixedPath {
			cleanedPath := CleanPath(path)
			n, _, tsr := fox.lookup(nds[index], cleanedPath, true)
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
				if n, _, _ := fox.lookup(nds[i], path, true); n != nil {
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

func (fox *Router) addRoute(method, path string, handler Handler) error {
	p, catchAllKey, n, err := parseRoute(path)
	if err != nil {
		return err
	}

	fox.mu.Lock()
	defer fox.mu.Unlock()

	updateMaxParams(uint32(n))

	return fox.insert(method, p, catchAllKey, handler)
}

func (fox *Router) recover(w http.ResponseWriter, r *http.Request) {
	if val := recover(); val != nil {
		if abortErr, ok := val.(error); ok && errors.Is(abortErr, http.ErrAbortHandler) {
			panic(abortErr)
		}
		fox.PanicHandler(w, r, val)
	}
}

func (fox *Router) lookup(rootNode *node, path string, lazy bool) (n *node, params *Params, tsr bool) {
	var (
		charsMatched            int
		charsMatchedInNodeFound int
	)

	current := rootNode
STOP:
	for charsMatched < len(path) {
		idx := linearSearch(current.childKeys, path[charsMatched])
		if idx < 0 {
			if !current.paramChild {
				break STOP
			}
			idx = 0
		}

		current = current.get(idx)
		charsMatchedInNodeFound = 0
		for i := 0; charsMatched < len(path); i++ {
			if i >= len(current.key) {
				break
			}

			// s1 := string(current.key[i])
			// s2 := string(path[charsMatched])
			// fmt.Println(s1, s2)
			if current.key[i] != path[charsMatched] || path[charsMatched] == ':' {
				if current.key[i] == ':' {
					startPath := charsMatched
					idx := strings.Index(path[charsMatched:], "/")
					if idx >= 0 {
						charsMatched += idx
					} else {
						charsMatched += len(path[charsMatched:])
					}
					startKey := charsMatchedInNodeFound
					idx = strings.Index(current.key[startKey:], "/")
					if idx >= 0 {
						// -1 since on the next incrementation, if any, 'i' are going to be incremented
						i += idx - 1
						charsMatchedInNodeFound += idx
					} else {
						// -1 since on the next incrementation, if any, 'i' are going to be incremented
						i += len(current.key[charsMatchedInNodeFound:]) - 1
						charsMatchedInNodeFound += len(current.key[charsMatchedInNodeFound:])
					}
					if !lazy {
						if params == nil {
							params = newParams()
						}
						*params = append(*params, Param{Key: current.key[startKey+1 : charsMatchedInNodeFound], Value: path[startPath:charsMatched]})
					}
					continue
				}
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
		if charsMatchedInNodeFound == len(current.key) {
			// Exact match, note that if we match a wildcard node, there is no remaining char to match. So we can
			// safely avoid the extra cost of passing an empty slice params.
			if !lazy && fox.AddRouteParam {
				if params == nil {
					params = newParams()
				}
				*params = append(*params, Param{Key: ParamRouteKey, Value: current.path})
				return current, params, false
			}

			return current, params, false
		} else if charsMatchedInNodeFound < len(current.key) {
			// Key end mid-edge
			// Tsr recommendation: add an extra trailing slash (got an exact match)
			remainingSuffix := current.key[charsMatchedInNodeFound:]
			return nil, nil, len(remainingSuffix) == 1 && remainingSuffix[0] == '/'
		}
	}

	// Incomplete match to end of edge
	if charsMatched < len(path) && charsMatchedInNodeFound == len(current.key) {
		// TODO if current.nType == catchAll => current.get(0) => this should be the right handler
		if current.isCatchAll() {
			if !lazy {
				if params == nil {
					params = newParams()
				}
				*params = append(*params, Param{Key: current.catchAllKey, Value: path[charsMatched:]})
				if fox.AddRouteParam {
					*params = append(*params, Param{Key: ParamRouteKey, Value: current.path})
				}
				return current, params, false
			}
			// Same as exact match, no tsr recommendation
			return current, params, false
		}
		// Tsr recommendation: remove the extra trailing slash (got an exact match)
		remainingKeySuffix := path[charsMatched:]
		return nil, nil, len(remainingKeySuffix) == 1 && remainingKeySuffix[0] == '/'
	}

	return nil, nil, false
}

// updateRoot is not safe for concurrent use.
func (fox *Router) updateRoot(n *node) bool {
	nds := *fox.trees.Load()
	index := findRootNode(n.key, nds)
	if index < 0 {
		return false
	}
	newNds := make([]*node, 0, len(nds))
	newNds = append(newNds, nds[:index]...)
	newNds = append(newNds, n)
	newNds = append(newNds, nds[index+1:]...)
	fox.trees.Store(&newNds)
	return true
}

// addRoot is not safe for concurrent use.
func (fox *Router) addRoot(n *node) {
	nds := *fox.trees.Load()
	newNds := make([]*node, 0, len(nds)+1)
	newNds = append(newNds, nds...)
	newNds = append(newNds, n)
	fox.trees.Store(&newNds)
}

// removeRoot is not safe for concurrent use.
func (fox *Router) removeRoot(method string) bool {
	nds := *fox.trees.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return false
	}
	newNds := make([]*node, 0, len(nds)-1)
	newNds = append(newNds, nds[:index]...)
	newNds = append(newNds, nds[index+1:]...)
	fox.trees.Store(&newNds)
	return true
}

// update is not safe for concurrent use.
func (fox *Router) update(method string, path, catchAllKey string, handler Handler) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	nds := *fox.trees.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return fmt.Errorf("route /%s %s is not registered: %w", method, path, ErrRouteNotFound)
	}

	result := fox.search(nds[index], path)
	if !result.isExactMatch() || !result.matched.isLeaf() {
		return fmt.Errorf("route /%s %s is not registered: %w", method, path, ErrRouteNotFound)
	}

	if catchAllKey != "" && len(result.matched.children) > 0 {
		return newConflictErr(method, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched)[1:], true)
	}

	// We are updating an existing node (could be a leaf or not). We only need to create a new node from
	// the matched one with the updated/added value (handler and wildcard).
	n := newNodeFromRef(result.matched.key, handler, result.matched.children, result.matched.childKeys, catchAllKey, path)
	result.p.updateEdge(n)
	return nil
}

// insert is not safe for concurrent use.
func (fox *Router) insert(method, path, catchAllKey string, handler Handler) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.

	var rootNode *node
	nds := *fox.trees.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		rootNode = &node{key: method}
		fox.addRoot(rootNode)
	} else {
		rootNode = nds[index]
	}

	isCatchAll := catchAllKey != ""

	result := fox.search(rootNode, path)
	switch result.classify() {
	case exactMatch:
		// e.g. matched exactly "te" node when inserting "te" key.
		// te
		// ├── st
		// └── am
		// Create a new node from "st" reference and update the "te" (parent) reference to "st" node.
		if result.matched.isLeaf() {
			return fmt.Errorf("route /%s %s is already registered: %w", method, path, ErrRouteExist)
		}

		// The matched node can only be the result of a previous split and therefore has children.
		if isCatchAll {
			return newConflictErr(method, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched), true)
		}
		// We are updating an existing node. We only need to create a new node from
		// the matched one with the updated/added value (handler and wildcard).
		n := newNodeFromRef(result.matched.key, handler, result.matched.children, result.matched.childKeys, catchAllKey, path)
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

		if isCatchAll {
			return newConflictErr(method, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched), true)
		}

		keyCharsFromStartOfNodeFound := path[result.charsMatched-result.charsMatchedInNodeFound:]
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.key)
		for i := len(cPrefix) - 1; i >= 0; i-- {
			if cPrefix[i] == '/' {
				break
			}
			if cPrefix[i] == ':' {
				return fmt.Errorf("params route, not allowed")
			}
		}
		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)

		child := newNodeFromRef(suffixFromExistingEdge, result.matched.handler, result.matched.children, result.matched.childKeys, result.matched.catchAllKey, result.matched.path)

		// e.g. tree encode /tes/ and /tes/:t
		// /tes/ (paramChild)
		// ├── :t
		// since /tes/xyz will match until /tes/ and when looking for next child, 'x' will match nothing
		// if paramChild == true {
		// 	next = current.get(0)
		// }
		parent := newNode(cPrefix, handler, []*node{child}, catchAllKey, path)
		if strings.HasPrefix(suffixFromExistingEdge, ":") {
			parent.paramChild = true
		}
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

		if result.matched.isCatchAll() {
			return newConflictErr(method, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched), true)
		}

		keySuffix := path[result.charsMatched:]
		// make sure than and existing params :x is not extended to :xy
		// :x/:y is of course valid
		if !strings.HasPrefix(keySuffix, "/") {
			for i := len(result.matched.key) - 1; i >= 0; i-- {
				if result.matched.key[i] == '/' {
					break
				}
				if result.matched.key[i] == ':' {
					return fmt.Errorf("params route, not allowed")
				}
			}
		}
		child := newNode(keySuffix, handler, nil, catchAllKey, path)
		edges := result.matched.getEdgesShallowCopy()
		edges = append(edges, child)
		n := newNode(result.matched.key, result.matched.handler, edges, result.matched.catchAllKey, result.matched.path)
		if result.matched == rootNode {
			n.key = method
			fox.updateRoot(n)
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
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.key)
		for i := len(cPrefix) - 1; i >= 0; i-- {
			if cPrefix[i] == '/' {
				break
			}
			if cPrefix[i] == ':' {
				return fmt.Errorf("params route, not allowed")
			}
		}
		/*		if strings.Contains(cPrefix, ":") && !strings.HasSuffix(cPrefix, "/") {
				return fmt.Errorf("params route, not allowed")
			}*/
		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)
		if strings.HasPrefix(suffixFromExistingEdge, ":") {
			return fmt.Errorf("params route, not allowed")
		}
		keySuffix := path[result.charsMatched:]

		n1 := newNodeFromRef(keySuffix, handler, nil, nil, catchAllKey, path)
		n2 := newNodeFromRef(suffixFromExistingEdge, result.matched.handler, result.matched.children, result.matched.childKeys, result.matched.catchAllKey, result.matched.path)
		n3 := newNode(cPrefix, nil, []*node{n1, n2}, "", "")
		result.p.updateEdge(n3)
	default:
		// safeguard against introducing a new result type
		panic("internal error: unexpected result type")
	}
	return nil
}

/*// insert is not safe for concurrent use.
func (fox *Router) insert(method, path, wildcardKey string, handler Handler, nType nodeType) error {
	// Note that we need a consistent view of the tree during the patching so search must imperatively be locked.
	var rootNode *node
	nds := *fox.trees.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		rootNode = &node{nType: root, key: method}
		fox.addRoot(rootNode)
	} else {
		rootNode = nds[index]
	}

	result := fox.search(rootNode, path)
	switch result.classify() {
	case exactMatch:
		// e.g. matched exactly "te" node when inserting "te" key.
		// te
		// ├── st
		// └── am
		// Create a new node from "st" reference and update the "te" (parent) reference to "st" node.
		if result.matched.isLeaf() {
			return fmt.Errorf("route /%s %s is already registered: %w", method, path, ErrRouteExist)
		}

		// The matched node can only be the result of a previous split and therefore has children.
		if nType == catchAll {
			return newConflictErr(method, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched), true)
		}
		// We are updating an existing node. We only need to create a new node from
		// the matched one with the updated/added value (handler and wildcard).
		n := newNodeFromRef(result.matched.key, handler, result.matched.children, result.matched.childKeys, wildcardKey, nType, path)
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

		if nType == catchAll {
			return newConflictErr(method, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched), true)
		}

		keyCharsFromStartOfNodeFound := path[result.charsMatched-result.charsMatchedInNodeFound:]
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.key)
		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)

		child := newNodeFromRef(suffixFromExistingEdge, result.matched.handler, result.matched.children, result.matched.childKeys, result.matched.catchAllKey, result.matched.nType, result.matched.path)
		parent := newNode(cPrefix, handler, []*node{child}, wildcardKey, nType, path)

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

		if result.matched.nType == catchAll {
			return newConflictErr(method, path, getRouteConflict(path[:result.charsMatched-result.charsMatchedInNodeFound], result.matched), true)
		}

		keySuffix := path[result.charsMatched:]
		child := newNode(keySuffix, handler, nil, wildcardKey, nType, path)
		edges := result.matched.getEdgesShallowCopy()
		edges = append(edges, child)
		n := newNode(result.matched.key, result.matched.handler, edges, result.matched.catchAllKey, result.matched.nType, result.matched.path)
		if result.matched == rootNode {
			n.nType = root
			n.key = method
			fox.updateRoot(n)
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
		cPrefix := commonPrefix(keyCharsFromStartOfNodeFound, result.matched.key)
		suffixFromExistingEdge := strings.TrimPrefix(result.matched.key, cPrefix)
		keySuffix := path[result.charsMatched:]

		n1 := newNodeFromRef(keySuffix, handler, nil, nil, wildcardKey, nType, path)
		n2 := newNodeFromRef(suffixFromExistingEdge, result.matched.handler, result.matched.children, result.matched.childKeys, result.matched.catchAllKey, result.matched.nType, result.matched.path)
		n3 := newNode(cPrefix, nil, []*node{n1, n2}, "", static, "")
		result.p.updateEdge(n3)
	default:
		// safeguard against introducing a new result type
		panic("internal error: unexpected result type")
	}
	return nil
}*/

// remove is not safe for concurrent use.
func (fox *Router) remove(method, path string) bool {
	nds := *fox.trees.Load()
	index := findRootNode(method, nds)
	if index < 0 {
		return false
	}

	result := fox.search(nds[index], path)
	if result.classify() != exactMatch {
		return false
	}

	// This node was created after a split (KEY_END_MID_EGGE operation), therefore we cannot delete
	// this node.
	if !result.matched.isLeaf() {
		return false
	}

	if len(result.matched.children) > 1 {
		n := newNodeFromRef(result.matched.key, nil, result.matched.children, result.matched.childKeys, "", "")
		result.p.updateEdge(n)
		return true
	}

	if len(result.matched.children) == 1 {
		child := result.matched.get(0)
		mergedPath := fmt.Sprintf("%s%s", result.matched.key, child.key)
		n := newNodeFromRef(mergedPath, child.handler, child.children, child.childKeys, child.catchAllKey, child.path)
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

	parentIsRoot := result.p == nds[index]
	var parent *node
	if len(parentEdges) == 1 && !result.p.isLeaf() && !parentIsRoot {
		child := parentEdges[0]
		mergedPath := fmt.Sprintf("%s%s", result.p.key, child.key)
		parent = newNodeFromRef(mergedPath, child.handler, child.children, child.childKeys, child.catchAllKey, child.path)
	} else {
		parent = newNode(result.p.key, result.p.handler, parentEdges, result.p.catchAllKey, result.p.path)
	}

	if parentIsRoot {
		if len(parent.children) == 0 && isRemovable(method) {
			return fox.removeRoot(method)
		}
		parent.key = method
		fox.updateRoot(parent)
		return true
	}

	result.pp.updateEdge(parent)
	return true
}

func (fox *Router) search(rootNode *node, path string) searchResult {
	current := rootNode

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
			if i >= len(current.key) {
				break
			}

			if current.key[i] != path[charsMatched] {
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
		if r.charsMatchedInNodeFound == len(r.matched.key) {
			return exactMatch
		}
		if r.charsMatchedInNodeFound < len(r.matched.key) {
			return keyEndMidEdge
		}
	} else if r.charsMatched < len(r.path) {
		// When the node matched is a root node, charsMatched & charsMatchedInNodeFound are both equals to 0, but the value of
		// the key is the http verb instead of a segment of the path and therefore len(r.matched.key) > 0 instead of empty (0).
		// TODO test with parent == nil, else fallback r.matched.nType == root
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

func findRootNode(method string, nodes []*node) int {
	// Nodes for common http method are pre instantiated at boot up.
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

		if p[i-1] != '/' {
			return "", "", -1, fmt.Errorf("missing '/' before %s route segment: %w", routeType(c), ErrInvalidRoute)
		}

		if i == len(p)-1 {
			return "", "", -1, fmt.Errorf("missing argument name after %s all operator: %w", routeType(c), ErrInvalidRoute)
		}

		if c == '*' {
			for k := i + 1; k < len(path); k++ {
				if path[k] == '/' {
					return "", "", -1, fmt.Errorf("catch all are supported only at the end of a route: %w", ErrInvalidRoute)
				}
			}
			return path[:i], path[i+1:], n, nil
		}
	}
	return path, "", n, nil
}

// todo remove basePath
func getRouteConflict(basePath string, n *node) []string {
	routes := make([]string, 0)
	it := newIterator(n)
	for it.hasNextLeaf() {
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
