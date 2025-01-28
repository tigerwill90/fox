// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
	"fmt"
	"reflect"
)

type Option interface {
	GlobalOption
	RouteOption
}

type GlobalOption interface {
	applyGlob(sealedOption) error
}

type RouteOption interface {
	applyRoute(sealedOption) error
}

type sealedOption struct {
	router *Router
	route  *Route
}

type globOptionFunc func(sealedOption) error

func (o globOptionFunc) applyGlob(s sealedOption) error {
	return o(s)
}

type routeOptionFunc func(sealedOption) error

func (o routeOptionFunc) applyRoute(s sealedOption) error {
	return o(s)
}

type optionFunc func(sealedOption) error

func (o optionFunc) applyGlob(s sealedOption) error {
	return o(s)
}

func (o optionFunc) applyRoute(s sealedOption) error {
	return o(s)
}

// WithNoRouteHandler register an [HandlerFunc] which is called when no matching route is found.
// By default, the [DefaultNotFoundHandler] is used.
func WithNoRouteHandler(handler HandlerFunc) GlobalOption {
	return globOptionFunc(func(s sealedOption) error {
		if handler == nil {
			return fmt.Errorf("%w: no route handler cannot be nil", ErrInvalidConfig)
		}
		s.router.noRoute = handler
		return nil
	})
}

// WithNoMethodHandler register an [HandlerFunc] which is called when the request cannot be routed,
// but the same route exist for other methods. The "Allow" header it automatically set before calling the
// handler. By default, the [DefaultMethodNotAllowedHandler] is used. Note that this option automatically
// enable [WithNoMethod].
func WithNoMethodHandler(handler HandlerFunc) GlobalOption {
	return globOptionFunc(func(s sealedOption) error {
		if handler == nil {
			return fmt.Errorf("%w: no method handler cannot be nil", ErrInvalidConfig)
		}
		s.router.noMethod = handler
		s.router.handleMethodNotAllowed = true
		return nil
	})
}

// WithOptionsHandler register an [HandlerFunc] which is called on automatic OPTIONS requests. By default, the router
// respond with a 200 OK status code. The "Allow" header it automatically set before calling the handler. Note that custom OPTIONS
// handler take priority over automatic replies. By default, [DefaultOptionsHandler] is used. Note that this option
// automatically enable [WithAutoOptions].
func WithOptionsHandler(handler HandlerFunc) GlobalOption {
	return globOptionFunc(func(s sealedOption) error {
		if handler == nil {
			return fmt.Errorf("%w: options handler cannot be nil", ErrInvalidConfig)
		}
		s.router.autoOptions = handler
		s.router.handleOptions = true
		return nil
	})
}

// WithMaxRouteParams set the maximum number of parameters allowed in a route. The default max is math.MaxUint16.
func WithMaxRouteParams(max uint16) GlobalOption {
	return globOptionFunc(func(s sealedOption) error {
		s.router.maxParams = max
		return nil
	})
}

// WithMaxRouteParamKeyBytes set the maximum number of bytes allowed per parameter key in a route. The default max is
// math.MaxUint16.
func WithMaxRouteParamKeyBytes(max uint16) GlobalOption {
	return globOptionFunc(func(s sealedOption) error {
		s.router.maxParamKeyBytes = max
		return nil
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
	return optionFunc(func(s sealedOption) error {
		if s.router != nil {
			for i := range m {
				if m[i] == nil {
					return fmt.Errorf("%w: middleware cannot be nil", ErrInvalidConfig)
				}
				s.router.mws = append(s.router.mws, middleware{m[i], AllHandlers, true})
			}
		}
		if s.route != nil {
			for i := range m {
				if m[i] == nil {
					return fmt.Errorf("%w: middleware cannot be nil", ErrInvalidConfig)
				}
				s.route.mws = append(s.route.mws, middleware{m[i], RouteHandler, false})
			}
		}
		return nil
	})
}

// WithMiddlewareFor attaches middleware to the router for a specified scope. Middlewares provided will be chained
// in the order they were added. The scope parameter determines which types of handlers the middleware will be applied to.
// Possible scopes include [RouteHandler] (regular routes), [NoRouteHandler], [NoMethodHandler], [RedirectHandler],
// [OptionsHandler], and any combination of these. Use this option when you need fine-grained control over where the
// middleware is applied.
func WithMiddlewareFor(scope HandlerScope, m ...MiddlewareFunc) GlobalOption {
	return globOptionFunc(func(s sealedOption) error {
		for i := range m {
			if m[i] == nil {
				return fmt.Errorf("%w: middleware cannot be nil", ErrInvalidConfig)
			}
			s.router.mws = append(s.router.mws, middleware{m[i], scope, true})
		}
		return nil
	})
}

// WithNoMethod enable to returns 405 Method Not Allowed instead of 404 Not Found
// when the route exist for another http verb. The "Allow" header it automatically set before calling the
// handler. Note that this option is automatically enabled when providing a custom handler with the
// option [WithNoMethodHandler].
func WithNoMethod(enable bool) GlobalOption {
	return globOptionFunc(func(s sealedOption) error {
		s.router.handleMethodNotAllowed = enable
		return nil
	})
}

// WithAutoOptions enables automatic response to OPTIONS requests with, by default, a 200 OK status code.
// Use the [WithOptionsHandler] option to customize the response. When this option is enabled, the router automatically
// determines the "Allow" header value based on the methods registered for the given route. Note that custom OPTIONS
// handler take priority over automatic replies. This option is automatically enabled when providing a custom handler with
// the option [WithOptionsHandler].
func WithAutoOptions(enable bool) GlobalOption {
	return globOptionFunc(func(s sealedOption) error {
		s.router.handleOptions = enable
		return nil
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
// Note that this option is mutually exclusive with [WithIgnoreTrailingSlash], and if enabled will
// automatically deactivate [WithIgnoreTrailingSlash].
func WithRedirectTrailingSlash(enable bool) Option {
	return optionFunc(func(s sealedOption) error {
		if s.router != nil {
			s.router.redirectTrailingSlash = enable
			if enable {
				s.router.ignoreTrailingSlash = false
			}
		}
		if s.route != nil {
			s.route.redirectTrailingSlash = enable
			if enable {
				s.route.ignoreTrailingSlash = false
			}
		}
		return nil
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
// Note that this option is mutually exclusive with [WithRedirectTrailingSlash], and if enabled will automatically
// deactivate [WithRedirectTrailingSlash].
func WithIgnoreTrailingSlash(enable bool) Option {
	return optionFunc(func(s sealedOption) error {
		if s.router != nil {
			s.router.ignoreTrailingSlash = enable
			if enable {
				s.router.redirectTrailingSlash = false
			}
		}
		if s.route != nil {
			s.route.ignoreTrailingSlash = enable
			if enable {
				s.route.redirectTrailingSlash = false
			}
		}
		return nil
	})
}

// WithClientIPResolver sets the resolver for obtaining the "real" client IP address from HTTP requests.
// This resolver is used by the [Context.ClientIP] method. The resolver must be chosen and tuned for your network
// configuration to ensure it never returns an error -- i.e., never fails to find a candidate for the "real" IP.
// Consequently, getting an error result should be treated as an application error, perhaps even worthy of panicking.
// There is no sane default, so if no resolver is configured, [Context.ClientIP] returns [ErrNoClientIPResolver].
//
// This option can be applied on a per-route basis or globally:
//   - If applied globally, it affects all routes by default.
//   - If applied to a specific route, it will override the global setting for that route.
//   - The option must be explicitly reapplied when updating a route. If not, the route will fall back
//     to the global client IP resolver (if one is configured).
//   - Setting the resolver to nil is equivalent to no resolver configured.
func WithClientIPResolver(resolver ClientIPResolver) Option {
	return optionFunc(func(s sealedOption) error {
		if s.router != nil && resolver != nil {
			s.router.clientip = resolver
		}

		if s.route != nil {
			// Apply no resolver if nil provided.
			s.route.clientip = cmp.Or(resolver, ClientIPResolver(noClientIPResolver{}))
		}
		return nil
	})
}

// WithAnnotation attach arbitrary metadata to routes. Annotations are key-value pairs that allow middleware, handler or
// any other components to modify behavior based on the attached metadata. Unlike context-based metadata, which is tied to
// the request lifetime, annotations are bound to the route's lifetime and remain static across all requests for that route.
// The provided key must be comparable and should not be of type string or any other built-in type to avoid collisions between
// packages that use route annotation. Annotations must be explicitly reapplied when updating a route.
func WithAnnotation(key, value any) RouteOption {
	return routeOptionFunc(func(s sealedOption) error {
		if !reflect.TypeOf(key).Comparable() {
			return fmt.Errorf("%w: annotation key is not comparable", ErrInvalidConfig)
		}
		if s.route.annots == nil {
			s.route.annots = make(map[any]any, 1)
		}
		s.route.annots[key] = value
		return nil
	})
}

// DefaultOptions configure the router to use the [Recovery] middleware for the [RouteHandler] scope, the [Logger] middleware
// for [AllHandlers] scope and enable automatic OPTIONS response. Note that DefaultOptions push the [Recovery] and [Logger]
// middleware respectively to the first and second position of the middleware chains.
func DefaultOptions() GlobalOption {
	return globOptionFunc(func(s sealedOption) error {
		s.router.mws = append([]middleware{
			{Recovery(), RouteHandler, true},
			{Logger(), AllHandlers, true},
		}, s.router.mws...)
		s.router.handleOptions = true
		return nil
	})
}
