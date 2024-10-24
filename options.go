// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
)

type Option interface {
	GlobalOption
	PathOption
}

type GlobalOption interface {
	applyGlob(*Router)
}

type PathOption interface {
	applyPath(*Route)
}

type globOptionFunc func(*Router)

func (o globOptionFunc) applyGlob(r *Router) {
	o(r)
}

// nolint:unused
type pathOptionFunc func(*Route)

// nolint:unused
func (o pathOptionFunc) applyPath(r *Route) {
	o(r)
}

type optionFunc func(*Router, *Route)

func (o optionFunc) applyGlob(r *Router) {
	o(r, nil)
}

func (o optionFunc) applyPath(r *Route) {
	o(nil, r)
}

// WithNoRouteHandler register an HandlerFunc which is called when no matching route is found.
// By default, the DefaultNotFoundHandler is used.
func WithNoRouteHandler(handler HandlerFunc) GlobalOption {
	return globOptionFunc(func(r *Router) {
		if handler != nil {
			r.noRoute = handler
		}
	})
}

// WithNoMethodHandler register an HandlerFunc which is called when the request cannot be routed,
// but the same route exist for other methods. The "Allow" header it automatically set before calling the
// handler. By default, the DefaultMethodNotAllowedHandler is used. Note that this option automatically
// enable WithNoMethod.
func WithNoMethodHandler(handler HandlerFunc) GlobalOption {
	return globOptionFunc(func(r *Router) {
		if handler != nil {
			r.noMethod = handler
			r.handleMethodNotAllowed = true
		}
	})
}

// WithOptionsHandler register an HandlerFunc which is called on automatic OPTIONS requests. By default, the router
// respond with a 200 OK status code. The "Allow" header it automatically set before calling the handler. Note that custom OPTIONS
// handler take priority over automatic replies. By default, DefaultOptionsHandler is used. Note that this option
// automatically enable WithAutoOptions.
func WithOptionsHandler(handler HandlerFunc) GlobalOption {
	return globOptionFunc(func(r *Router) {
		if handler != nil {
			r.autoOptions = handler
			r.handleOptions = true
		}
	})
}

// WithMiddleware attaches middleware to the router or to a specific route. The middlewares are executed
// in the order they are added. When applied globally, the middleware affects all handlers, including special handlers
// such as NotFound, MethodNotAllowed, AutoOption, and the internal redirect handler.
//
// This option can be applied on a per-route basis or globally:
// - If applied globally, the middleware will be applied to all routes and handlers by default.
// - If applied to a specific route, the middleware will only apply to that route and will be chained after any global middleware.
//
// Route-specific middleware must be explicitly reapplied when updating a route. If not, any middleware will be removed,
// and the route will fall back to using only global middleware (if any).
func WithMiddleware(m ...MiddlewareFunc) Option {
	return optionFunc(func(router *Router, route *Route) {
		if router != nil {
			for i := range m {
				router.mws = append(router.mws, middleware{m[i], AllHandlers, true})
			}
		}
		if route != nil {
			for i := range m {
				route.mws = append(route.mws, middleware{m[i], RouteHandler, false})
			}
		}
	})
}

// WithMiddlewareFor attaches middleware to the router for a specified scope. Middlewares provided will be chained
// in the order they were added. The scope parameter determines which types of handlers the middleware will be applied to.
// Possible scopes include RouteHandler (regular routes), NoRouteHandler, NoMethodHandler, RedirectHandler, OptionsHandler,
// and any combination of these. Use this option when you need fine-grained control over where the middleware is applied.
func WithMiddlewareFor(scope HandlerScope, m ...MiddlewareFunc) GlobalOption {
	return globOptionFunc(func(r *Router) {
		for i := range m {
			r.mws = append(r.mws, middleware{m[i], scope, true})
		}
	})
}

// WithNoMethod enable to returns 405 Method Not Allowed instead of 404 Not Found
// when the route exist for another http verb. The "Allow" header it automatically set before calling the
// handler. Note that this option is automatically enabled when providing a custom handler with the
// option WithNoMethodHandler.
func WithNoMethod(enable bool) GlobalOption {
	return globOptionFunc(func(r *Router) {
		r.handleMethodNotAllowed = enable
	})
}

// WithAutoOptions enables automatic response to OPTIONS requests with, by default, a 200 OK status code.
// Use the WithOptionsHandler option to customize the response. When this option is enabled, the router automatically
// determines the "Allow" header value based on the methods registered for the given route. Note that custom OPTIONS
// handler take priority over automatic replies. This option is automatically enabled when providing a custom handler with
// the option WithOptionsHandler.
func WithAutoOptions(enable bool) GlobalOption {
	return globOptionFunc(func(r *Router) {
		r.handleOptions = enable
	})
}

// WithRedirectTrailingSlash enable automatic redirection fallback when the current request does not match but
// another handler is found with/without an additional trailing slash. E.g. /foo/bar/ request does not match
// but /foo/bar would match. The client is redirected with a http status code 301 for GET requests and 308 for
// all other methods.
//
// This option can be applied on a per-route basis or globally:
//   - If applied globally, it affects all routes by default.
//   - If applied to a specific route, it will override the global setting for that route.
//   - The option must be explicitly reapplied when updating a route. If not, the route will fall back
//     to the global configuration for trailing slash behavior.
//
// Note that this option is mutually exclusive with WithIgnoreTrailingSlash, and if enabled will
// automatically deactivate WithIgnoreTrailingSlash.
func WithRedirectTrailingSlash(enable bool) Option {
	return optionFunc(func(router *Router, route *Route) {
		if router != nil {
			router.redirectTrailingSlash = enable
			if enable {
				router.ignoreTrailingSlash = false
			}
		}
		if route != nil {
			route.redirectTrailingSlash = enable
			if enable {
				route.ignoreTrailingSlash = false
			}
		}
	})
}

// WithIgnoreTrailingSlash allows the router to match routes regardless of whether a trailing slash is present or not.
// E.g. /foo/bar/ and /foo/bar would both match the same handler. This option prevents the router from issuing
// a redirect and instead matches the request directly.
//
// This option can be applied on a per-route basis or globally:
//   - If applied globally, it affects all routes by default.
//   - If applied to a specific route, it will override the global setting for that route.
//   - The option must be explicitly reapplied when updating a route. If not, the route will fall back
//     to the global configuration for trailing slash behavior.
//
// Note that this option is mutually exclusive with
// WithRedirectTrailingSlash, and if enabled will automatically deactivate WithRedirectTrailingSlash.
func WithIgnoreTrailingSlash(enable bool) Option {
	return optionFunc(func(router *Router, route *Route) {
		if router != nil {
			router.ignoreTrailingSlash = enable
			if enable {
				router.redirectTrailingSlash = false
			}
		}
		if route != nil {
			route.ignoreTrailingSlash = enable
			if enable {
				route.redirectTrailingSlash = false
			}
		}
	})
}

// WithClientIPStrategy sets the strategy for obtaining the "real" client IP address from HTTP requests.
// This strategy is used by the Context.ClientIP method. The strategy must be chosen and tuned for your network
// configuration to ensure it never returns an error -- i.e., never fails to find a candidate for the "real" IP.
// Consequently, getting an error result should be treated as an application error, perhaps even worthy of panicking.
// There is no sane default, so if no strategy is configured, Context.ClientIP returns ErrNoClientIPStrategy.
//
// This option can be applied on a per-route basis or globally:
//   - If applied globally, it affects all routes by default.
//   - If applied to a specific route, it will override the global setting for that route.
//   - The option must be explicitly reapplied when updating a route. If not, the route will fall back
//     to the global client IP strategy (if one is configured).
//   - Setting the strategy to nil is equivalent to no strategy configured.
func WithClientIPStrategy(strategy ClientIPStrategy) Option {
	return optionFunc(func(router *Router, route *Route) {
		if router != nil && strategy != nil {
			router.ipStrategy = strategy
		}

		if route != nil {
			// Apply no strategy if nil provided.
			route.ipStrategy = cmp.Or(strategy, ClientIPStrategy(noClientIPStrategy{}))
		}
	})
}

// WithAnnotations attach arbitrary metadata to routes. Annotations are key-value pairs that allow middleware, handler or
// any other components to modify behavior based on the attached metadata. Unlike context-based metadata, which is tied to
// the request lifetime, annotations are bound to the route's lifetime and remain static across all requests for that route.
// Annotations must be explicitly reapplied when updating a route.
func WithAnnotations(annotations ...Annotation) PathOption {
	return pathOptionFunc(func(route *Route) {
		route.annots = append(route.annots, annotations...)
	})
}

// DefaultOptions configure the router to use the Recovery middleware for the RouteHandler scope, the Logger middleware
// for AllHandlers scope and enable automatic OPTIONS response. Note that DefaultOptions push the Recovery and Logger middleware
// respectively to the first and second position of the middleware chains.
func DefaultOptions() GlobalOption {
	return globOptionFunc(func(r *Router) {
		r.mws = append([]middleware{
			{Recovery(), RouteHandler, true},
			{Logger(), AllHandlers, true},
		}, r.mws...)
		r.handleOptions = true
	})
}
