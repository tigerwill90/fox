package fox

import (
	"iter"
	"reflect"
	"slices"
	"strings"
)

// Route represents an immutable HTTP route with associated handlers and settings.
type Route struct {
	clientip    ClientIPResolver
	hbase       HandlerFunc
	hself       HandlerFunc
	hall        HandlerFunc
	annots      map[any]any
	pattern     string
	mws         []middleware
	params      []string
	tokens      []token
	matchers    []Matcher
	hostSplit   int // 0 if no host
	handleSlash TrailingSlashOption
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

// TrailingSlashOption returns the configured [TrailingSlashOption] for this route.
func (r *Route) TrailingSlashOption() TrailingSlashOption {
	return r.handleSlash
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
	return len(r.params)
}

func (r *Route) Match(c RequestContext) bool {
	for _, m := range r.matchers {
		if !m.Match(c) {
			return false
		}
	}
	return true
}

func (r *Route) MatchersEqual(matchers []Matcher) bool {
	return slices.EqualFunc(r.matchers, matchers, func(m1 Matcher, m2 Matcher) bool { return m1.Equal(m2) })
}

func (r *Route) MatchersIndex(matcher Matcher) int {
	return slices.IndexFunc(r.matchers, func(m Matcher) bool { return m.Equal(matcher) })
}

func (r *Route) MatchersContains(matcher Matcher) bool {
	return r.MatchersIndex(matcher) >= 0
}

func (r *Route) Matchers() iter.Seq[Matcher] {
	return func(yield func(Matcher) bool) {
		for _, m := range r.matchers {
			if !yield(m) {
				return
			}
		}
	}
}

// TODO delete me
func (r *Route) String() string {
	sb := strings.Builder{}
	sb.WriteString(r.pattern)
	sb.WriteByte('{')
	for _, matcher := range r.matchers {
		sb.WriteString(reflect.TypeOf(matcher).String())
		sb.WriteByte(' ')
	}
	sb.WriteByte('}')
	return sb.String()
}
