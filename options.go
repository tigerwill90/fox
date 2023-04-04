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

// WithNoRouteHandler register a http.Handler which is called when no matching route is found.
// By default, http.NotFound is used.
func WithNoRouteHandler(handler HandlerFunc) Option {
	return optionFunc(func(r *Router) {
		if handler != nil {
			r.noRoute = handler
		}
	})
}

// WithNoMethodHandler register a http.Handler which is called when the request cannot be routed,
// but the same route exist for other methods. The "Allow" header it automatically set
// before calling the handler. Set WithHandleMethodNotAllowed to enable this option. By default,
// http.Error with http.StatusMethodNotAllowed is used.
func WithNoMethodHandler(handler HandlerFunc) Option {
	return optionFunc(func(r *Router) {
		if handler != nil {
			r.noMethod = handler
		}
	})
}

// WithMiddleware attaches a global middleware to the router. Middlewares provided will be chained
// in the order they were added. Note that it does NOT apply the middlewares to the NotFound and MethodNotAllowed handlers.
func WithMiddleware(middlewares ...MiddlewareFunc) Option {
	return optionFunc(func(r *Router) {
		r.mws = append(r.mws, middlewares...)
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
func DefaultOptions() Option {
	return optionFunc(func(r *Router) {
		r.mws = append(r.mws, Recovery(defaultHandleRecovery))
	})
}
