package fox

import (
	"iter"
)

// Annotation represents a single key-value pair that provides metadata for a route.
// Annotations are typically used to store information that can be leveraged by middleware, handlers, or external
// libraries to modify or customize route behavior.
type Annotation struct {
	Key   string
	Value any
}

// Route represent a registered route in the router.
type Route struct {
	clientip              ClientIPResolver
	hbase                 HandlerFunc
	hself                 HandlerFunc
	hall                  HandlerFunc
	pattern               string
	mws                   []middleware
	annots                []Annotation
	hostSplit             int // 0 if no host
	redirectTrailingSlash bool
	ignoreTrailingSlash   bool
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

// Annotations returns a range iterator over annotations associated with the route.
func (r *Route) Annotations() iter.Seq[Annotation] {
	return func(yield func(Annotation) bool) {
		for _, a := range r.annots {
			if !yield(a) {
				return
			}
		}
	}
}

// RedirectTrailingSlashEnabled returns whether the route is configured to automatically
// redirect requests that include or omit a trailing slash.
func (r *Route) RedirectTrailingSlashEnabled() bool {
	return r.redirectTrailingSlash
}

// IgnoreTrailingSlashEnabled returns whether the route is configured to ignore
// trailing slashes in requests when matching routes.
func (r *Route) IgnoreTrailingSlashEnabled() bool {
	return r.ignoreTrailingSlash
}

// ClientIPResolverEnabled returns whether the route is configured with a [ClientIPResolver].
func (r *Route) ClientIPResolverEnabled() bool {
	_, ok := r.clientip.(noClientIPResolver)
	return !ok
}
