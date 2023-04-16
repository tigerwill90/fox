// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

// MiddlewareScope is a type that represents different scopes for applying middleware.
type MiddlewareScope uint8

const (
	// RouteHandlers scope applies middleware only to regular routes registered in the router.
	RouteHandlers MiddlewareScope = 1 << (8 - 1 - iota)
	// NotFoundHandler scope applies middleware to the NotFound handler (when a route is not found).
	NotFoundHandler
	// MethodNotAllowedHandler scope applies middleware to the MethodNotAllowed handler (when the method is not allowed for the requested route).
	MethodNotAllowedHandler
	// RedirectHandler scope applies middleware to the internal redirect handler (for trailing slash and fixed path redirection).
	RedirectHandler
	// AllHandlers is a combination of all the above scopes, which means the middleware will be applied to all types of handlers.
	AllHandlers = RouteHandlers | NotFoundHandler | MethodNotAllowedHandler | RedirectHandler
)

type Option interface {
	apply(*Router)
}

type optionFunc func(*Router)

func (o optionFunc) apply(r *Router) {
	o(r)
}

// WithNotFoundHandler register an HandlerFunc which is called when no matching route is found.
// By default, the DefaultNotFoundHandler is used.
func WithNotFoundHandler(handler HandlerFunc) Option {
	return optionFunc(func(r *Router) {
		if handler != nil {
			r.noRoute = handler
		}
	})
}

// WithMethodNotAllowedHandler register an HandlerFunc which is called when the request cannot be routed,
// but the same route exist for other methods. The "Allow" header it automatically set before calling the
// handler. By default, the DefaultMethodNotAllowedHandler is used. Note that this option automatically
// enable WithMethodNotAllowed.
func WithMethodNotAllowedHandler(handler HandlerFunc) Option {
	return optionFunc(func(r *Router) {
		if handler != nil {
			r.noMethod = handler
			r.handleMethodNotAllowed = true
		}
	})
}

// WithMiddleware attaches a global middleware to the router. Middlewares provided will be chained
// in the order they were added. Note that this option apply middleware to all handler, including NotFound,
// MethodNotAllowed and the internal redirect handler.
func WithMiddleware(m ...MiddlewareFunc) Option {
	return WithScopedMiddleware(AllHandlers, m...)
}

// WithScopedMiddleware attaches middleware to the router with a specified scope. Middlewares provided will be chained
// in the order they were added. The scope parameter determines which types of handlers the middleware will be applied to.
// Possible scopes include RouteHandlers (regular routes), NotFoundHandler, MethodNotAllowedHandler, RedirectHandler, and any combination of these.
// Use this option when you need fine-grained control over where the middleware is applied.
// This api is EXPERIMENTAL and is likely to change in future release.
func WithScopedMiddleware(scope MiddlewareScope, m ...MiddlewareFunc) Option {
	return optionFunc(func(r *Router) {
		for i := range m {
			r.mws = append(r.mws, middleware{m[i], scope})
		}
	})
}

// WithMethodNotAllowed enable to returns 405 Method Not Allowed instead of 404 Not Found
// when the route exist for another http verb. Note that this option is automatically enabled
// when providing a custom handler with the option WithMethodNotAllowedHandler.
func WithMethodNotAllowed(enable bool) Option {
	return optionFunc(func(r *Router) {
		r.handleMethodNotAllowed = enable
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
		r.mws = append([]middleware{{Recovery(HandleRecovery), RouteHandlers}}, r.mws...)
	})
}
