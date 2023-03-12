package fox

import "net/http"

type Options func(*Router)

// WithNotFoundHandler register a http.Handler which is called when no matching route is found.
// By default, http.NotFound is used.
func WithNotFoundHandler(handler http.Handler) Options {
	return func(r *Router) {
		if handler != nil {
			r.notFound = handler
		}
	}
}

// WithNotAllowedHandler register a http.Handler which is called when the request cannot be routed,
// but the same route exist for other methods. The "Allow" header it automatically set
// before calling the handler. Use WithHandleMethodNotAllowed to enable this option. By default,
// http.Error with http.StatusMethodNotAllowed is used.
func WithNotAllowedHandler(handler http.Handler) Options {
	return func(r *Router) {
		if handler != nil {
			r.methodNotAllowed = handler
		}
	}
}

// WithPanicHandler register a function to handle panics recovered from http handlers.
func WithPanicHandler(fn func(http.ResponseWriter, *http.Request, interface{})) Options {
	return func(r *Router) {
		if fn != nil {
			r.panicHandler = fn
		}
	}
}

// WithHandleMethodNotAllowed enable to returns 405 Method Not Allowed instead of 404 Not Found
// when the route exist for another http verb.
func WithHandleMethodNotAllowed(enable bool) Options {
	return func(r *Router) {
		r.handleMethodNotAllowed = enable
	}
}

// WithRedirectFixedPath enable automatic redirection fallback when the current request does not match but
// another handler is found after cleaning up superfluous path elements (see CleanPath). E.g. /../foo/bar request
// does not match but /foo/bar would. The client is redirected with a http status code 301 for GET requests
// and 308 for all other methods.
func WithRedirectFixedPath(enable bool) Options {
	return func(r *Router) {
		r.redirectFixedPath = enable
	}
}

// WithRedirectTrailingSlash enable automatic redirection fallback when the current request does not match but
// another handler is found with/without an additional trailing slash. E.g. /foo/bar/ request does not match
// but /foo/bar would match. The client is redirected with a http status code 301 for GET requests and 308 for
// all other methods.
func WithRedirectTrailingSlash(enable bool) Options {
	return func(r *Router) {
		r.redirectTrailingSlash = enable
	}
}

// WithSaveMatchedRoute configure the router to make the matched route accessible as a Handler parameter.
// Usage: p.Get(fox.RouteKey)
func WithSaveMatchedRoute(enable bool) Options {
	return func(r *Router) {
		r.saveMatchedRoute = enable
	}
}
