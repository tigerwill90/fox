package fox

import (
	"iter"
	"slices"
)

// Route represents an immutable HTTP route with associated handlers and settings.
type Route struct {
	clientip    ClientIPResolver
	hbase       HandlerFunc
	hself       HandlerFunc
	hall        HandlerFunc
	annots      map[any]any
	sub         *Router
	pattern     string
	name        string
	methods     []string
	mws         []middleware
	params      []string
	tokens      []token
	matchers    []Matcher
	hostSplit   int // 0 if no host
	priority    uint
	handleSlash TrailingSlashOption
	catchEmpty  bool
}

// Handle calls the handler with the provided [Context]. See also [Route.HandleMiddleware].
func (r *Route) Handle(c *Context) {
	r.hbase(c)
}

// HandleMiddleware calls the handler with route-specific middleware applied, using the provided [Context].
func (r *Route) HandleMiddleware(c *Context, _ ...struct{}) {
	// The variadic parameter is intentionally added to prevent this method from having the same signature as HandlerFunc.
	// This avoids accidental use of HandleMiddleware where a HandlerFunc is required.
	r.hself(c)
}

// Methods returns an iterator over all HTTP methods this route responds to (if any), in lexicographical order.
func (r *Route) Methods() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, m := range r.methods {
			if !yield(m) {
				return
			}
		}
	}
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

// Name returns the name of this [Route].
func (r *Route) Name() string {
	return r.name
}

// Annotation returns the value associated with this [Route] for key, or nil if no value is associated with key.
// Successive calls to Annotation with the same key returns the same result.
func (r *Route) Annotation(key any) any {
	return r.annots[key]
}

// TrailingSlashOption returns the configured [TrailingSlashOption] for this [Route].
func (r *Route) TrailingSlashOption() TrailingSlashOption {
	return r.handleSlash
}

// ClientIPResolver returns the [ClientIPResolver] configured for this [Route], if any.
func (r *Route) ClientIPResolver() ClientIPResolver {
	if _, ok := r.clientip.(noClientIPResolver); ok {
		return nil
	}
	return r.clientip
}

// ParamsLen returns the number of parameters for this [Route].
func (r *Route) ParamsLen() int {
	return len(r.params)
}

// Params returns an iterator over all parameters name for this [Route].
func (r *Route) Params() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, param := range r.params {
			if !yield(param) {
				return
			}
		}
	}
}

// MatchersLen returns the number of matchers for this [Route].
func (r *Route) MatchersLen() int {
	return len(r.matchers)
}

// Matchers returns an iterator over all matchers attached to this [Route].
func (r *Route) Matchers() iter.Seq[Matcher] {
	return func(yield func(Matcher) bool) {
		for _, m := range r.matchers {
			if !yield(m) {
				return
			}
		}
	}
}

// Priority returns the matchers priority for this [Route].
func (r *Route) Priority() uint {
	return r.priority
}

// SubRouter returns the [Router] mounted at this route, or nil if this route
// was not created with [Router.NewSubRouter].
func (r *Route) SubRouter() *Router {
	return r.sub
}

// match reports whether the request satisfies this route's method constraint (if any)
// and all attached matchers.
func (r *Route) match(method string, c RequestContext) bool {
	if len(r.methods) > 0 && !slices.Contains(r.methods, method) {
		return false
	}

	for _, m := range r.matchers {
		if !m.Match(c) {
			return false
		}
	}
	return true
}

// matchersEqual reports whether this [Route]'s matchers are equal to the provided matchers.
func (r *Route) matchersEqual(matchers []Matcher) bool {
	if len(r.matchers) != len(matchers) {
		return false
	}

	// Runs in O(n²) time, but the matched slice should be stack-allocated in most cases.
	// A hash-based O(n) approach was considered, but for small arrays the cost of populating
	// a map outweighs the quadratic comparison cost. Additionally, maps with more than 8 elements
	// are heap-allocated, which adds to the cost.
	matched := make([]bool, len(matchers))

outer:
	for _, a := range r.matchers {
		for i, b := range matchers {
			if !matched[i] && a.Equal(b) {
				matched[i] = true
				continue outer
			}
		}
		return false
	}
	return true
}
