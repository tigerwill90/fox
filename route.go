package fox

// Route represents an immutable HTTP route with associated handlers and settings.
type Route struct {
	clientip  ClientIPResolver
	hbase     HandlerFunc
	hself     HandlerFunc
	hall      HandlerFunc
	annots    map[any]any
	pattern   string
	mws       []middleware
	hostSplit int // 0 if no host
	psLen     uint32
	options   optFlag
}

// Handle calls the handler with the provided [Context]. See also [Route.HandleMiddleware].
func (r *Route) Handle(c Context) {
	r.hbase(c)
}

// HandleMiddleware calls the handler with route-specific middleware applied, using the provided [Context].
func (r *Route) HandleMiddleware(c Context, _ ...struct{}) {
	// The variadic parameter is intentionally added to prevent this method from having the same signature as HandlerFunc.
	// This avoids accidental use of HandleMiddleware where a HandlerFunc is required.
	r.hself(c)
}

// Pattern returns the registered route pattern.
func (r *Route) Pattern() string {
	return r.pattern
}

// Hostname returns the hostname part of the registered pattern if any.
func (r *Route) Hostname() string {
	return r.pattern[:r.hostSplit]
}

// Path returns the path part of the registered pattern.
func (r *Route) Path() string {
	return r.pattern[r.hostSplit:]
}

// Annotation returns the value associated with this [Route] for key, or nil if no value is associated with key.
// Successive calls to Annotation with the same key returns the same result.
func (r *Route) Annotation(key any) any {
	return r.annots[key]
}

// RedirectTrailingSlashEnabled returns whether the route is configured to automatically
// redirect requests that include or omit a trailing slash.
func (r *Route) RedirectTrailingSlashEnabled() bool {
	return r.options&redirectTrailingSlash != 0
}

// IgnoreTrailingSlashEnabled returns whether the route is configured to ignore
// trailing slashes in requests when matching routes.
func (r *Route) IgnoreTrailingSlashEnabled() bool {
	return r.options&ignoreTrailingSlash != 0
}

// ClientIPResolver returns the [ClientIPResolver] configured for the route, if any.
func (r *Route) ClientIPResolver() ClientIPResolver {
	if _, ok := r.clientip.(noClientIPResolver); ok {
		return nil
	}
	return r.clientip
}

// ParamsLen returns the number of wildcard parameter for the route.
func (r *Route) ParamsLen() int {
	return int(r.psLen)
}
