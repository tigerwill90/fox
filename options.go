// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

type Option interface {
	apply(*Router)
}

type optionFunc func(*Router)

func (o optionFunc) apply(r *Router) {
	o(r)
}

// WithRouteNotFound register an HandlerFunc which is called when no matching route is found.
// By default, the NotFoundHandler is used.
func WithRouteNotFound(handler HandlerFunc, m ...MiddlewareFunc) Option {
	return optionFunc(func(r *Router) {
		if handler != nil {
			r.noRoute = applyMiddleware(m, handler)
		}
	})
}

// WithMethodNotAllowed register an HandlerFunc which is called when the request cannot be routed,
// but the same route exist for other methods. The "Allow" header it automatically set
// before calling the handler. Set WithHandleMethodNotAllowed to enable this option. By default,
// the MethodNotAllowedHandler is used.
func WithMethodNotAllowed(handler HandlerFunc, m ...MiddlewareFunc) Option {
	return optionFunc(func(r *Router) {
		if handler != nil {
			r.noMethod = applyMiddleware(m, handler)
		}
	})
}

// WithRouteError register an ErrorHandlerFunc which is called when an HandlerFunc returns an error.
// By default, the RouteErrorHandler is used.
func WithRouteError(handler ErrorHandlerFunc) Option {
	return optionFunc(func(r *Router) {
		if handler != nil {
			r.errRoute = handler
		}
	})
}

// WithMiddleware attaches a global middleware to the router. Middlewares provided will be chained
// in the order they were added. Note that it does NOT apply the middlewares to the NotFound and MethodNotAllowed handlers.
func WithMiddleware(m ...MiddlewareFunc) Option {
	return optionFunc(func(r *Router) {
		r.mws = append(r.mws, m...)
	})
}

// WithHandleMethodNotAllowed enable to returns 405 Method Not Allowed instead of 404 Not Found
// when the route exist for another http verb.
func WithHandleMethodNotAllowed(enable bool) Option {
	return optionFunc(func(r *Router) {
		r.handleMethodNotAllowed = enable
	})
}

// WithRedirectFixedPath enable automatic redirection fallback when the current request does not match but
// another handler is found after cleaning up superfluous path elements (see CleanPath). E.g. /../foo/bar request
// does not match but /foo/bar would. The client is redirected with a http status code 301 for GET requests
// and 308 for all other methods.
func WithRedirectFixedPath(enable bool) Option {
	return optionFunc(func(r *Router) {
		r.redirectFixedPath = enable
	})
}

// WithRedirectTrailingSlash enable automatic redirection fallback when the current request does not match but
// another handler is found with/without an additional trailing slash. E.g. /foo/bar/ request does not match
// but /foo/bar would match. The client is redirected with a http status code 301 for GET requests and 308 for
// all other methods.
func WithRedirectTrailingSlash(enable bool) Option {
	return optionFunc(func(r *Router) {
		r.redirectTrailingSlash = enable
	})
}

// DefaultOptions configure the router to use the Recovery middleware.
// Note that DefaultOptions push the Recovery middleware to the first position of the middleware chains.
func DefaultOptions() Option {
	return optionFunc(func(r *Router) {
		r.mws = append([]MiddlewareFunc{Recovery(HandleRecovery)}, r.mws...)
	})
}
