// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
	"fmt"
	"reflect"

	"github.com/tigerwill90/fox/internal/slogpretty"
)

type TrailingSlashOption uint8

const (
	StrictSlash TrailingSlashOption = iota
	RelaxedSlash
	RedirectSlash

	slashOptionSentinel
)

type FixedPathOption uint8

const (
	StrictPath FixedPathOption = iota
	RelaxedPath
	RedirectPath

	pathOptionSentinel
)

type GlobalOption interface {
	applyGlob(sealedOption) error
}

type RouteOption interface {
	applyRoute(sealedOption) error
}

type SubRouterOption interface {
	applySubRouter(sealedOption) error
}

type MatcherOption interface {
	RouteOption
	SubRouterOption
	applyMatcher(sealedOption) error
}

type sealedOption struct {
	router *Router
	route  *Route
}

type optionFunc func(sealedOption) error

func (o optionFunc) applyGlob(s sealedOption) error {
	return o(s)
}

func (o optionFunc) applyRoute(s sealedOption) error {
	return o(s)
}

func (o optionFunc) applySubRouter(s sealedOption) error {
	return o(s)
}

func (o optionFunc) applyMatcher(s sealedOption) error {
	return o(s)
}

// WithNoRouteHandler register an [HandlerFunc] which is called when no matching route is found.
// By default, the [DefaultNotFoundHandler] is used.
func WithNoRouteHandler(handler HandlerFunc) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		if handler == nil {
			return fmt.Errorf("%w: no route handler cannot be nil", ErrInvalidConfig)
		}
		s.router.noRouteBase = handler
		return nil
	})
}

// WithNoMethodHandler register an [HandlerFunc] which is called when the request cannot be routed,
// but the same route exist for other methods. The "Allow" header it automatically set before calling the
// handler. By default, the [DefaultMethodNotAllowedHandler] is used. Note that this option automatically
// enable [WithNoMethod].
func WithNoMethodHandler(handler HandlerFunc) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		if handler == nil {
			return fmt.Errorf("%w: no method handler cannot be nil", ErrInvalidConfig)
		}
		s.router.noMethod = handler
		s.router.handleMethodNotAllowed = true
		return nil
	})
}

// WithOptionsHandler register an [HandlerFunc] which is called on automatic OPTIONS requests. By default, the router
// respond with a 204 status code. The "Allow" header it automatically set before calling the handler (except for CORS preflight request).
// Note that custom OPTIONS handler take priority over automatic replies. By default, [DefaultOptionsHandler] is used. Note that this option
// automatically enable [WithAutoOptions].
func WithOptionsHandler(handler HandlerFunc) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		if handler == nil {
			return fmt.Errorf("%w: options handler cannot be nil", ErrInvalidConfig)
		}
		s.router.autoOPTIONS = handler
		s.router.handleOPTIONS = true
		return nil
	})
}

// WithHandleTrailingSlash configures how the router handles trailing slashes in request paths.
//
// Available slash handling modes:
//   - StrictSlash: Routes are matched exactly as registered. /foo/bar and /foo/bar/ are treated as different routes.
//   - RelaxedSlash: Routes match regardless of trailing slash. Both /foo/bar and /foo/bar/ match the same route.
//   - RedirectSlash: When a route is not found, but exists with/without a trailing slash, issues a redirect to the correct path.
//
// Redirects use URL.RawPath if set, otherwise URL.Path.
//
// This option can be applied on a per-route basis or globally:
//   - If applied globally, it affects all routes by default.
//   - If applied to a specific route, it will override the global setting for that route.
//
// If both /foo/bar and /foo/bar/ are explicitly registered, the exact match always takes precedence.
// The trailing slash handling logic only applies when there is no direct match but a match would be
// possible by adding or removing a trailing slash.
func WithHandleTrailingSlash(opt TrailingSlashOption) interface {
	GlobalOption
	RouteOption
	SubRouterOption
} {
	return optionFunc(func(s sealedOption) error {
		if opt >= slashOptionSentinel {
			return fmt.Errorf("%w: invalid trailing slash option", ErrInvalidConfig)
		}
		if s.router != nil {
			s.router.handleSlash = opt
		}
		if s.route != nil {
			s.route.handleSlash = opt
		}
		return nil
	})
}

// WithHandleFixedPath configures how the router handles non-canonical request paths containing
// extraneous elements like double slashes, dots, or parent directory references.
//
// Available path handling modes:
//   - StrictPath: No path cleaning is performed. Routes are matched only as requested (disables this feature).
//   - RelaxedPath: After normal lookup fails, tries matching with a cleaned path. If found, serves the handler directly.
//   - RedirectPath: After normal lookup fails, tries matching with a cleaned path. If found, redirects to the clean path.
//
// Redirects use URL.RawPath if set, otherwise URL.Path.
//
// This option applies globally to all routes and cannot be configured per-route. See [CleanPath] for details on how
// paths are cleaned.
func WithHandleFixedPath(opt FixedPathOption) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		if opt >= pathOptionSentinel {
			return fmt.Errorf("%w: invalid fixed path option", ErrInvalidConfig)
		}
		s.router.handlePath = opt
		return nil
	})
}

// WithMaxRouteParams set the maximum number of parameters allowed in a route. The default max is math.MaxUint8.
// Routes exceeding this limit will fail with an error that is ErrInvalidRoute and ErrTooManyParams.
func WithMaxRouteParams(max int) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.maxParams = max
		return nil
	})
}

// WithMaxRouteParamKeyBytes set the maximum number of bytes allowed per parameter key in a route. The default max is
// math.MaxUint8. Routes with parameter keys exceeding this limit will fail with an error that Is ErrInvalidRoute and
// ErrParamKeyTooLarge.
func WithMaxRouteParamKeyBytes(max int) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.maxParamKeyBytes = max
		return nil
	})
}

// WithMaxRouteMatchers set the maximum number of matchers allowed in a route. The default max is math.MaxUint8.
// Routes exceeding this limit will fail with an error that Is ErrInvalidRoute and ErrTooManyMatchers.
func WithMaxRouteMatchers(max int) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.maxMatchers = max
		return nil
	})
}

// AllowRegexpParam enables support for regular expressions in route parameters. When enabled, parameters can include
// regex patterns (e.g., {id:[0-9]+}). When disabled, routes containing regex patterns will fail with and error that
// Is ErrInvalidRoute and ErrRegexpNotAllowed.
func AllowRegexpParam(enable bool) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.allowRegexp = enable
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
func WithMiddleware(m ...MiddlewareFunc) interface {
	GlobalOption
	RouteOption
} {
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
// Possible scopes include [RouteHandler] (regular routes), [NoRouteHandler], [NoMethodHandler], [RedirectSlashHandler],
// [OptionsHandler], and any combination of these. Use this option when you need fine-grained control over where the
// middleware is applied.
func WithMiddlewareFor(scope HandlerScope, m ...MiddlewareFunc) GlobalOption {
	return optionFunc(func(s sealedOption) error {
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
	return optionFunc(func(s sealedOption) error {
		s.router.handleMethodNotAllowed = enable
		return nil
	})
}

// WithAutoOptions enables automatic response to OPTIONS requests with, by default, a 204 status code.
// Use the [WithOptionsHandler] option to customize the response. When this option is enabled, the router automatically
// determines the "Allow" header value based on the methods registered for the given route (except for CORS preflight request).
// Note that custom OPTIONS handler take priority over automatic replies. This option is automatically enabled when providing
// a custom handler with the option [WithOptionsHandler].
func WithAutoOptions(enable bool) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.handleOPTIONS = enable
		return nil
	})
}

// WithSystemWideOptions enable automatic response for system-wide OPTIONS request (OPTIONS *). When this option is enabled,
// the router return a 200 OK status code with the "Allow" header containing all registered HTTP methods. This option is enabled
// by default.
func WithSystemWideOptions(enable bool) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.systemWideOPTIONS = enable
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
//   - Setting the resolver to nil is equivalent to no resolver configured.
func WithClientIPResolver(resolver ClientIPResolver) interface {
	GlobalOption
	RouteOption
} {
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
// packages that use route annotation. See also [WithAnnotationFunc]
func WithAnnotation(key, value any) RouteOption {
	return optionFunc(func(s sealedOption) error {
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

// WithAnnotationFunc attaches arbitrary metadata to routes like [WithAnnotation], but the annotation value
// is produced by a function that can also return an error. If the function returns an error, route
// registration fails with that error wrapped in [ErrInvalidConfig].
func WithAnnotationFunc(key any, fn func() (value any, err error)) RouteOption {
	return optionFunc(func(s sealedOption) error {
		if !reflect.TypeOf(key).Comparable() {
			return fmt.Errorf("%w: annotation key is not comparable", ErrInvalidConfig)
		}
		value, err := fn()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
		}
		if s.route.annots == nil {
			s.route.annots = make(map[any]any, 1)
		}
		s.route.annots[key] = value
		return nil
	})
}

// WithName assigns a name to a route for identification and lookup purposes.
// The name must be unique among routes registered with the same HTTP method.
func WithName(name string) interface {
	RouteOption
	SubRouterOption
} {
	return optionFunc(func(s sealedOption) error {
		if name == "" {
			return fmt.Errorf("%w: empty route name", ErrInvalidConfig)
		}
		s.route.name = name
		return nil
	})
}

// WithMatcherPriority sets the priority for a route with matchers. When multiple routes share the same pattern,
// route matchers are evaluated by priority order (highest first). Routes with equal priority may be evaluated in any order.
// Routes without matchers are always evaluated last. If unset or 0, the priority defaults to the number of matchers.
func WithMatcherPriority(priority uint) interface {
	RouteOption
	SubRouterOption
} {
	return optionFunc(func(s sealedOption) error {
		s.route.priority = priority
		return nil
	})
}

// WithQueryMatcher attaches a query parameter matcher to a route. The matcher ensures that requests
// are only routed to the handler if the specified query parameter matches the given value. Multiple
// matchers can be attached to the same route. All matchers must match for the route to be eligible.
func WithQueryMatcher(key, value string) interface {
	RouteOption
	SubRouterOption
	MatcherOption
} {
	return optionFunc(func(s sealedOption) error {
		matcher, err := MatchQuery(key, value)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidMatcher, err)
		}
		s.route.matchers = append(s.route.matchers, matcher)
		return nil
	})
}

// WithQueryRegexpMatcher attaches a query parameter matcher with regular expression support to a route.
// The matcher ensures that requests are only routed to the handler if the specified query parameter value
// matches the given regular expression. The expression is automatically anchored at both ends, requiring a
// full match of the parameter value. Multiple matchers can be attached to the same route. All matchers
// must match for the route to be eligible.
func WithQueryRegexpMatcher(key, expr string) interface {
	RouteOption
	SubRouterOption
	MatcherOption
} {
	return optionFunc(func(s sealedOption) error {
		matcher, err := MatchQueryRegexp(key, expr)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidMatcher, err)
		}
		s.route.matchers = append(s.route.matchers, matcher)
		return nil
	})
}

// WithHeaderMatcher attaches an HTTP header matcher to a route. The matcher ensures that requests
// are only routed to the handler if the specified header matches the given value. Multiple matchers
// can be attached to the same route. All matchers must match for the route to be eligible.
func WithHeaderMatcher(key, value string) interface {
	RouteOption
	SubRouterOption
	MatcherOption
} {
	return optionFunc(func(s sealedOption) error {
		matcher, err := MatchHeader(key, value)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidMatcher, err)
		}
		s.route.matchers = append(s.route.matchers, matcher)
		return nil
	})
}

// WithHeaderRegexpMatcher attaches an HTTP header matcher with regular expression support to a route.
// The matcher ensures that requests are only routed to the handler if the specified header value
// matches the given regular expression. The expression is automatically anchored at both ends, requiring
// a full match of the header value. Multiple matchers can be attached to the same route. All matchers
// must match for the route to be eligible.
func WithHeaderRegexpMatcher(key, expr string) interface {
	RouteOption
	SubRouterOption
	MatcherOption
} {
	return optionFunc(func(s sealedOption) error {
		matcher, err := MatchHeaderRegexp(key, expr)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidMatcher, err)
		}
		s.route.matchers = append(s.route.matchers, matcher)
		return nil
	})
}

// WithClientIPMatcher attaches a client IP address matcher to a route. The matcher ensures that requests
// are only routed to the handler if the client IP address matches the specified CIDR notation or IP address.
// The ip parameter accepts both single IP addresses (e.g., "192.168.1.1") and CIDR ranges (e.g., "192.168.1.0/24").
// Multiple matchers can be attached to the same route. All matchers must match for the route to be eligible.
// See WithClientIPResolver to configure a resolver for obtaining the "real" client IP.
func WithClientIPMatcher(ip string) interface {
	RouteOption
	SubRouterOption
	MatcherOption
} {
	return optionFunc(func(s sealedOption) error {
		matcher, err := MatchClientIP(ip)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidMatcher, err)
		}
		s.route.matchers = append(s.route.matchers, matcher)
		return nil
	})
}

// WithMatcher attaches a custom matcher to a route. Matchers allow for advanced request routing based
// on conditions beyond the request host, path and method. Multiple matchers can be attached to the same route.
// All matchers must match for the route to be eligible.
func WithMatcher(matchers ...Matcher) interface {
	RouteOption
	SubRouterOption
	MatcherOption
} {
	return optionFunc(func(s sealedOption) error {
		for i := range matchers {
			if matchers[i] == nil {
				return fmt.Errorf("%w: matcher cannot be nil", ErrInvalidMatcher)
			}
			s.route.matchers = append(s.route.matchers, matchers[i])
		}
		return nil
	})
}

// DefaultOptions configures the router with sensible production defaults:
//   - Enables automatic OPTIONS responses ([WithAutoOptions])
//   - Enables 405 Method Not Allowed responses ([WithNoMethod])
//   - Enables regular expression support in route parameters ([AllowRegexpParam])
//   - Enables redirect-based path correction for trailing slashes ([WithHandleTrailingSlash] with [RedirectSlash])
//   - Enables redirect-based path correction for non-canonical paths ([WithHandleFixedPath] with [RedirectPath])
//
// For development, consider combining this with [DevelopmentOptions] to add debugging middleware.
func DefaultOptions() GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.handleOPTIONS = true
		s.router.handleMethodNotAllowed = true
		s.router.allowRegexp = true
		s.router.handlePath = RedirectPath
		s.router.handleSlash = RedirectSlash
		return nil
	})
}

// DevelopmentOptions configures the router with middleware useful for local development and debugging.
// It registers the following middleware at the front of the chain:
//   - [Recovery] middleware for the [RouteHandler] scope, which catches panics and logs stack traces
//   - [Logger] middleware for [AllHandlers] scope, which logs request details
//
// Both middleware use a human-readable, colorized slog handler optimized for terminal output.
// This option is intended for development only and should not be used in production, where structured
// logging with a proper [slog.Handler] is typically preferred.
func DevelopmentOptions() GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.mws = append([]middleware{
			{Recovery(slogpretty.DefaultHandler), RouteHandler, true},
			{Logger(slogpretty.DefaultHandler), AllHandlers, true},
		}, s.router.mws...)
		return nil
	})
}
