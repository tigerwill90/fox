package fox

import (
	"iter"
	"strings"
)

// Route represent a registered route in the route tree.
// Most of the Route API is EXPERIMENTAL and is likely to change in future release.
type Route struct {
	ipStrategy            ClientIPStrategy
	hbase                 HandlerFunc
	hself                 HandlerFunc
	hall                  HandlerFunc
	path                  string
	mws                   []middleware
	tags                  []string
	redirectTrailingSlash bool
	ignoreTrailingSlash   bool
}

// Handle calls the handler with the provided Context. See also HandleMiddleware.
func (r *Route) Handle(c Context) {
	r.hbase(c)
}

// HandleMiddleware calls the handler with route-specific middleware applied, using the provided Context.
func (r *Route) HandleMiddleware(c Context, _ ...struct{}) {
	// The variadic parameter is intentionally added to prevent this method from having the same signature as HandlerFunc.
	// This avoids accidental use of HandleMiddleware where a HandlerFunc is required.
	r.hself(c)
}

// Path returns the route path.
func (r *Route) Path() string {
	return r.path
}

// Tags returns a range iterator over the tags associated with the route.
func (r *Route) Tags() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, tag := range r.tags {
			if !yield(tag) {
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

// ClientIPStrategyEnabled returns whether the route is configured with a ClientIPStrategy.
// This api is EXPERIMENTAL and is likely to change in future release.
func (r *Route) ClientIPStrategyEnabled() bool {
	_, ok := r.ipStrategy.(noClientIPStrategy)
	return !ok
}

func (r *Route) hydrateParams(path string, params *Params) bool {
	rLen := len(r.path)
	pLen := len(path)
	var i, j int
	state := stateDefault

	// Note that we assume that this is a valid route (validated with parseRoute).
OUTER:
	for i < rLen && j < pLen {
		switch state {
		case stateParam:
			startPath := j
			idx := strings.IndexByte(path[j:], slashDelim)
			if idx > 0 {
				j += idx
			} else if idx < 0 {
				j += len(path[j:])
			} else {
				// segment is empty
				return false
			}

			startRoute := i
			idx = strings.IndexByte(r.path[i:], slashDelim)
			if idx >= 0 {
				i += idx
			} else {
				i += len(r.path[i:])
			}

			*params = append(*params, Param{
				Key:   r.path[startRoute : i-1],
				Value: path[startPath:j],
			})

			state = stateDefault

		default:
			if r.path[i] == '{' {
				i++
				state = stateParam
				continue
			}

			if r.path[i] == '*' {
				state = stateCatchAll
				break OUTER
			}

			if r.path[i] == path[j] {
				i++
				j++
				continue
			}

			return false
		}
	}

	if state == stateCatchAll || (i < rLen && r.path[i] == '*') {
		*params = append(*params, Param{
			Key:   r.path[i+2 : rLen-1],
			Value: path[j:],
		})
		return true
	}

	if i == rLen && j == pLen {
		return true
	}

	return false
}
