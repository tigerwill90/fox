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

// Route represent a registered route in the route tree.
// Most of the Route API is EXPERIMENTAL and is likely to change in future release.
type Route struct {
	ipStrategy            ClientIPStrategy
	hbase                 HandlerFunc
	hself                 HandlerFunc
	hall                  HandlerFunc
	pattern               string
	mws                   []middleware
	annots                []Annotation
	redirectTrailingSlash bool
	ignoreTrailingSlash   bool
}

// Handle calls the handler with the provided [Context]. See also [HandleMiddleware].
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
// This api is EXPERIMENTAL and is likely to change in future release.
func (r *Route) RedirectTrailingSlashEnabled() bool {
	return r.redirectTrailingSlash
}

// IgnoreTrailingSlashEnabled returns whether the route is configured to ignore
// trailing slashes in requests when matching routes.
// This api is EXPERIMENTAL and is likely to change in future release.
func (r *Route) IgnoreTrailingSlashEnabled() bool {
	return r.ignoreTrailingSlash
}

// ClientIPStrategyEnabled returns whether the route is configured with a [ClientIPStrategy].
// This api is EXPERIMENTAL and is likely to change in future release.
func (r *Route) ClientIPStrategyEnabled() bool {
	_, ok := r.ipStrategy.(noClientIPStrategy)
	return !ok
}
