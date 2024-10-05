// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

// MiddlewareScope is a type that represents different scopes for applying middleware.
type MiddlewareScope uint8

const (
	// RouteHandlers scope applies middleware only to regular routes registered in the router.
	RouteHandlers MiddlewareScope = 1 << (8 - 1 - iota)
	// NoRouteHandler scope applies middleware to the NoRoute handler.
	NoRouteHandler
	// NoMethodHandler scope applies middleware to the NoMethod handler.
	NoMethodHandler
	// RedirectHandler scope applies middleware to the internal redirect trailing slash handler.
	RedirectHandler
	// OptionsHandler scope applies middleware to the automatic OPTIONS handler.
	OptionsHandler
	// AllHandlers is a combination of all the above scopes, which means the middleware will be applied to all types of handlers.
	AllHandlers = RouteHandlers | NoRouteHandler | NoMethodHandler | RedirectHandler | OptionsHandler
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
// This api is EXPERIMENTAL and is likely to change in future release.
func WithOptionsHandler(handler HandlerFunc) GlobalOption {
	return globOptionFunc(func(r *Router) {
		if handler != nil {
			r.autoOptions = handler
			r.handleOptions = true
		}
	})
}

// WithMiddleware attaches a middleware to the router or a path. Middlewares provided will be chained in the order they
// were added. Note that this option, when used globally, apply middleware to all handler, including NotFound, MethodNotAllowed,
// AutoOption and the internal redirect handler.
func WithMiddleware(m ...MiddlewareFunc) Option {
	return optionFunc(func(router *Router, route *Route) {
		if router != nil {
			for i := range m {
				router.mws = append(router.mws, middleware{m[i], AllHandlers})
			}
		}
		if route != nil {
			for i := range m {
				route.mws = append(route.mws, middleware{m[i], RouteHandlers})
			}
		}
	})
}

// WithMiddlewareFor attaches middleware to the router for a specified scope. Middlewares provided will be chained
// in the order they were added. The scope parameter determines which types of handlers the middleware will be applied to.
// Possible scopes include RouteHandlers (regular routes), NoRouteHandler, NoMethodHandler, RedirectHandler, OptionsHandler,
// and any combination of these. Use this option when you need fine-grained control over where the middleware is applied.
func WithMiddlewareFor(scope MiddlewareScope, m ...MiddlewareFunc) GlobalOption {
	return globOptionFunc(func(r *Router) {
		for i := range m {
			r.mws = append(r.mws, middleware{m[i], scope})
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
// the option WithOptionsHandler. This api is EXPERIMENTAL and is likely to change in future release.
func WithAutoOptions(enable bool) GlobalOption {
	return globOptionFunc(func(r *Router) {
		r.handleOptions = enable
	})
}

// WithRedirectTrailingSlash enable automatic redirection fallback when the current request does not match but
// another handler is found with/without an additional trailing slash. E.g. /foo/bar/ request does not match
// but /foo/bar would match. The client is redirected with a http status code 301 for GET requests and 308 for
// all other methods. Note that this option is mutually exclusive with WithIgnoreTrailingSlash, and if both are
// enabled, WithIgnoreTrailingSlash takes precedence.
func WithRedirectTrailingSlash(enable bool) Option {
	return optionFunc(func(router *Router, route *Route) {
		if router != nil {
			router.redirectTrailingSlash = enable
		}
		if route != nil {
			route.redirectTrailingSlash = enable
		}
	})
}

// WithIgnoreTrailingSlash allows the router to match routes regardless of whether a trailing slash is present or not.
// E.g. /foo/bar/ and /foo/bar would both match the same handler. This option prevents the router from issuing
// a redirect and instead matches the request directly. Note that this option is mutually exclusive with
// WithRedirectTrailingSlash, and if both are enabled, WithIgnoreTrailingSlash takes precedence.
// This api is EXPERIMENTAL and is likely to change in future release.
func WithIgnoreTrailingSlash(enable bool) Option {
	return optionFunc(func(router *Router, route *Route) {
		if router != nil {
			router.ignoreTrailingSlash = enable
		}
		if route != nil {
			route.ignoreTrailingSlash = enable
		}
	})
}

// WithClientIPStrategy sets the strategy for obtaining the "real" client IP address from HTTP requests.
// This strategy is used by the Context.ClientIP method. The strategy must be chosen and tuned for your network
// configuration to ensure it never returns an error -- i.e., never fails to find a candidate for the "real" IP.
// Consequently, getting an error result should be treated as an application error, perhaps even worthy of panicking.
// There is no sane default, so if no strategy is configured, Context.ClientIP returns ErrNoClientIPStrategy.
// This API is EXPERIMENTAL and is likely to change in future releases.
func WithClientIPStrategy(strategy ClientIPStrategy) Option {
	return optionFunc(func(router *Router, route *Route) {
		if strategy != nil {
			if router != nil {
				router.ipStrategy = strategy
			}
			if route != nil {
				route.ipStrategy = strategy
			}
		}
	})
}

// DefaultOptions configure the router to use the Recovery middleware for the RouteHandlers scope, the Logger middleware
// for AllHandlers scope and enable automatic OPTIONS response. Note that DefaultOptions push the Recovery and Logger middleware
// respectively to the first and second position of the middleware chains.
func DefaultOptions() GlobalOption {
	return globOptionFunc(func(r *Router) {
		r.mws = append([]middleware{
			{Recovery(), RouteHandlers},
			{Logger(), AllHandlers},
		}, r.mws...)
		r.handleOptions = true
	})
}
