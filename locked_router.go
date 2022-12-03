package fox

type LockedRouter struct {
	r      *Router
	locked bool
}

// LockRouter acquire a lock on the router which allow to perform multiple mutation while
// keeping a consistent view of the routing tree. LockedRouter's holder must always ensure
// to call Release in order to unlock the router.
func (fox *Router) LockRouter() *LockedRouter {
	fox.mu.Lock()
	return &LockedRouter{
		r:      fox,
		locked: true,
	}
}

// Handler registers a new handler for the given method and path. This function return an error if the route
// is already registered or conflict with another. It's perfectly safe to add a new handler while serving requests.
// This function is NOT safe for concurrent use by multiple goroutine and panic if called after lr.Release().
func (lr *LockedRouter) Handler(method, path string, handler Handler) error {
	lr.assertLock()
	p, catchAllKey, n, err := parseRoute(path)
	if err != nil {
		return err
	}
	if lr.r.AddRouteParam {
		n += 1
	}
	return lr.r.insert(method, p, catchAllKey, uint32(n), handler)
}

// Update override an existing handler for the given method and path. If the route does not exist,
// the function return an ErrRouteNotFound. It's perfectly safe to update a handler while serving requests.
// This function is NOT safe for concurrent use by multiple goroutine and panic if called after lr.Release().
func (lr *LockedRouter) Update(method, path string, handler Handler) error {
	lr.assertLock()
	p, catchAllKey, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	return lr.r.update(method, p, catchAllKey, handler)
}

// Remove delete an existing handler for the given method and path. If the route does not exist, the function
// return an ErrRouteNotFound. It's perfectly safe to remove a handler while serving requests. This function is
// NOT safe for concurrent use by multiple goroutine and panic if called after lr.Release().
func (lr *LockedRouter) Remove(method, path string) error {
	lr.assertLock()
	path, _, _, err := parseRoute(path)
	if err != nil {
		return err
	}

	if !lr.r.remove(method, path) {
		return ErrRouteNotFound
	}
	return nil
}

// Lookup allow to do manual lookup of a route. Please note that params are only valid until fn callback returns (see Handler interface).
// If lazy is set to true, params are not parsed. This function is safe for concurrent use by multiple goroutine.
func (lr *LockedRouter) Lookup(method, path string, lazy bool, fn func(handler Handler, params Params, tsr bool)) {
	lr.r.Lookup(method, path, lazy, fn)
}

// Match perform a lazy lookup and return true if the requested method and path match a registered route.
// This function is safe for concurrent use by multiple goroutine.
func (lr *LockedRouter) Match(method, path string) bool {
	return lr.r.Match(method, path)
}

// NewIterator returns an Iterator that traverses all registered routes in lexicographic order.
// An Iterator is safe to use when the router is serving request, when routing updates are ongoing or
// in parallel with other Iterators. Note that changes that happen while iterating over routes may not be reflected
// by the Iterator. This api is EXPERIMENTAL and is likely to change in future release.
func (lr *LockedRouter) NewIterator() *Iterator {
	return lr.r.NewIterator()
}

// Release unlock the router. Calling this function on a released LockedRouter is a noop.
func (lr *LockedRouter) Release() {
	if !lr.locked {
		return
	}
	lr.locked = false
	lr.r.mu.Unlock()
}

func (lr *LockedRouter) assertLock() {
	if !lr.locked {
		panic("lock already released")
	}
}
