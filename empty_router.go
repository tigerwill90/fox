package meta

type EmptyRouter struct {
	mux *Router
}

func NewEmptyRouter() *EmptyRouter {
	return &EmptyRouter{New()}
}

func (r *EmptyRouter) Use(middlewares ...Middleware) {
	for _, m := range middlewares {
		r.mux.middlewares = append(r.mux.middlewares, m)
	}
}

// GET is a shortcut for Handler(http.MethodGet, path, handler)
func (r *EmptyRouter) GET(path string, handler Handler) error {
	return r.mux.GET(path, handler)
}

// POST is a shortcut for Handler(http.MethodPost, path, handler)
func (r *EmptyRouter) POST(path string, handler Handler) error {
	return r.mux.POST(path, handler)
}

// PUT is a shortcut for Handler(http.MethodPut, path, handler)
func (r *EmptyRouter) PUT(path string, handler Handler) error {
	return r.mux.PUT(path, handler)
}

// PATCH is a shortcut for Handler(http.MethodPatch, path, handler)
func (r *EmptyRouter) PATCH(path string, handler Handler) error {
	return r.mux.PATCH(path, handler)
}

// DELETE is a shortcut for Handler(http.MethodDelete, path, handler)
func (r *EmptyRouter) DELETE(path string, handler Handler) error {
	return r.mux.DELETE(path, handler)
}

// OPTIONS is a shortcut for Handler(http.MethodOptions, path, handler)
func (r *EmptyRouter) OPTIONS(path string, handler Handler) error {
	return r.mux.OPTIONS(path, handler)
}

// CONNECT is a shortcut for Handler(http.MethodConnect, path, handler)
func (r *EmptyRouter) CONNECT(path string, handler Handler) error {
	return r.mux.CONNECT(path, handler)
}

// HEAD is a shortcut for Handler(http.MethodHead, path, handler)
func (r *EmptyRouter) HEAD(path string, handler Handler) error {
	return r.mux.HEAD(path, handler)
}

// TRACE is a shortcut for Handler(http.MethodTrace, path, handler)
func (r *EmptyRouter) TRACE(path string, handler Handler) error {
	return r.mux.TRACE(path, handler)
}

// Handler registers a new http.Handler for the given method and path. If the route is already registered,
// the function return an ErrRouteExist. It's perfectly safe to add a new handler once the server is started.
// This function is safe for concurrent use by multiple goroutine.
// To override an existing route, use Update method.
func (r *EmptyRouter) Handler(method, path string, handler Handler) error {
	return r.mux.Handler(method, path, handler)
}

func (r *EmptyRouter) reset() {
	for i := range r.mux.trees {
		r.mux.updateRoot(i, new(node))
	}
	r.mux.middlewares = nil
}

// Reset clear all resource associated with the current empty router.
// This function is safe for concurrent use by multiple goroutine.
func (r *EmptyRouter) Reset() {
	r.mux.mu.Lock()
	r.reset()
	r.mux.mu.Unlock()
}
