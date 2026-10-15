// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
	"fmt"
	"reflect"

	"github.com/fox-toolkit/fox/internal/slogpretty"
)

type TrailingSlashOption uint8

const (
	ExactSlash TrailingSlashOption = iota
	RelaxedSlash
	RedirectSlash

	slashOptionSentinel
)

type NormalizeOption uint8

const (
	ExactPath NormalizeOption = iota
	NormalizePath
	RedirectPath

	normalizeOptionSentinel
)

type GlobalOption interface {
	applyGlob(sealedOption) error
}

type RouteOption interface {
	applyRoute(sealedOption) error
}

type MatcherOption interface {
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

func (o optionFunc) applyMatcher(s sealedOption) error {
	return o(s)
}

// WithNoRouteHandler register an [HandlerFunc] which is called when no matching route is found.
// By default, the [DefaultNoRouteHandler] is used.
func WithNoRouteHandler(handler HandlerFunc) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		if handler == nil {
			return fmt.Errorf("%w: no route handler cannot be nil", ErrInvalidConfig)
		}
		s.router.noRoute = handler
		return nil
	})
}

// WithNoMethodHandler register an [HandlerFunc] which is called when the request cannot be routed,
// but the same route exist for other methods. The "Allow" header it automatically set before calling the
// handler. By default, the [DefaultNoMethodHandler] is used. Note that this option automatically
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

// WithRejectPathHandler register an [HandlerFunc] which is called when path normalization rejects the request.
// This happens when [WithCollapseDotSegments] is enabled and a ".." segment escapes above the root.
// By default, the [DefaultRejectPathHandler] replies with a 400 Bad Request.
func WithRejectPathHandler(handler HandlerFunc) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		if handler == nil {
			return fmt.Errorf("%w: reject path handler cannot be nil", ErrInvalidConfig)
		}
		s.router.pathReject = handler
		return nil
	})
}

// WithTrailingSlash configures how the router handles trailing slashes in request paths.
//
// Available slash handling modes:
//   - ExactSlash: Routes are matched exactly as registered. /foo/bar and /foo/bar/ are treated as different routes.
//   - RelaxedSlash: Routes match regardless of trailing slash. Both /foo/bar and /foo/bar/ match the same route.
//   - RedirectSlash: When a route is not found, but exists with/without a trailing slash, issues a redirect to the correct path.
//
// Redirects use the canonical path that the router routed on (see [Context.RoutingPath]).
//
// This option can be applied on a per-route basis or globally:
//   - If applied globally, it affects all routes by default.
//   - If applied to a specific route, it will override the global setting for that route.
//
// If both /foo/bar and /foo/bar/ are explicitly registered, the exact match always takes precedence.
// Otherwise a trailing slash match keeps the route's normal priority, like a direct match.
func WithTrailingSlash(opt TrailingSlashOption) interface {
	GlobalOption
	RouteOption
} {
	return optionFunc(func(s sealedOption) error {
		if opt >= slashOptionSentinel {
			return fmt.Errorf("%w: invalid trailing slash option", ErrInvalidConfig)
		}
		if s.router != nil {
			s.router.handleSlash = opt
			return nil
		}
		if s.route != nil {
			s.route.handleSlash = opt
			return nil
		}
		return nil
	})
}

// WithMergeSlashes configures how the router handles consecutive slashes in request paths.
//
// Available modes:
//   - ExactPath: Consecutive slashes are matched as-is (default).
//   - NormalizePath: Slashes are merged before lookup.
//   - RedirectPath: When merging changes the path, issues a redirect to the merged path if it matches a route.
//
// Redirects use the canonical path that the router routed on (see [Context.RoutingPath]).
//
// Only raw slashes are merged, an encoded %2F never matches a slash (see [MergeSlashes]). When combined
// with [WithCollapseDotSegments], slashes are merged before dot segments are collapsed.
//
// This option applies globally to all routes.
func WithMergeSlashes(opt NormalizeOption) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		if opt >= normalizeOptionSentinel {
			return fmt.Errorf("%w: invalid merge slashes option", ErrInvalidConfig)
		}
		s.router.mergeSlash = opt
		return nil
	})
}

// WithCollapseDotSegments configures how the router handles "." and ".." segments in request paths.
// Dot segments are removed as defined by RFC 3986 (see [CollapseDotSegments]). A ".." escaping above the
// root is rejected with a 400 (see [WithRejectPathHandler]) before lookup.
//
// Available modes:
//   - ExactPath: Dot segments are matched as-is (default).
//   - NormalizePath: Dot segments are collapsed before lookup.
//   - RedirectPath: When collapsing changes the path, issues a redirect to the collapsed path if it matches a route.
//
// Redirects use the canonical path that the router routed on (see [Context.RoutingPath]).
//
// When combined with [WithMergeSlashes], slashes are merged before dot segments are collapsed.
//
// This option applies globally to all routes.
func WithCollapseDotSegments(opt NormalizeOption) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		if opt >= normalizeOptionSentinel {
			return fmt.Errorf("%w: invalid collapse dot segments option", ErrInvalidConfig)
		}
		s.router.collapseDots = opt
		return nil
	})
}

// WithMaxRouteParams set the maximum number of parameters allowed in a route. The default max is math.MaxUint8.
// Routes exceeding this limit will fail with a [*PatternError].
func WithMaxRouteParams(max uint16) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.maxParams = int(max)
		return nil
	})
}

// WithMaxRouteParamKeyBytes set the maximum number of bytes allowed per parameter key in a route. The default max is
// math.MaxUint8. Routes with parameter keys exceeding this limit will fail with a [*PatternError].
func WithMaxRouteParamKeyBytes(max uint16) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.maxParamKeyBytes = int(max)
		return nil
	})
}

// WithMaxRouteMatchers set the maximum number of matchers allowed in a route. The default max is math.MaxUint8.
// Routes exceeding this limit will fail with an error that Is ErrInvalidRoute and ErrTooManyMatchers.
func WithMaxRouteMatchers(max uint16) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.maxMatchers = int(max)
		return nil
	})
}

// WithMaxRegexpSize set the maximum size of a regular expression in a route pattern, as the number of instructions of its
// compiled program. The default max is 1000. Routes exceeding this limit will fail with a [*PatternError].
func WithMaxRegexpSize(max uint16) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.maxRegexpSize = int(max)
		return nil
	})
}

// WithAllowRegexpParam enables support for regular expressions in route parameters. When enabled, parameters can include
// regex patterns (e.g., {id:[0-9]+}). When disabled, routes containing regex patterns will fail with a [*PatternError].
func WithAllowRegexpParam(enable bool) GlobalOption {
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
			return nil
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
// [RedirectPathHandler], [RejectPathHandler], [OptionsHandler], and any combination of these. Use this option when you
// need fine-grained control over where the middleware is applied.
func WithMiddlewareFor(scope HandlerScope, m ...MiddlewareFunc) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		if scope == 0 || scope&^AllHandlers != 0 {
			return fmt.Errorf("%w: invalid handler scope", ErrInvalidConfig)
		}
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

// WithAutoOptions enables automatic responses to OPTIONS requests with, by default, a 204 status code. For regular OPTIONS requests,
// the router responds with the "Allow" header listing methods registered for the matched routes, or calls the NoRoute handler if
// no route matches. For CORS preflight requests, the router always responds to all request by calling the OPTIONS handler.
// Note that custom OPTIONS handler take priority over automatic replies. This option is automatically enabled when providing
// a custom handler with the option [WithOptionsHandler].
func WithAutoOptions(enable bool) GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.handleOPTIONS = enable
		return nil
	})
}

// WithSystemWideOptions enable automatic response for system-wide OPTIONS request (OPTIONS *). When this option is enabled,
// the router responds with a 200 OK status code and the "Allow" header listing all HTTP methods used across registered routes.
// Note that to let Fox handle system-wide OPTIONS requests, http.Server.DisableGeneralOptionsHandler must be set to true.
// This option is enabled by default.
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
		if s.router != nil {
			if resolver == nil {
				return fmt.Errorf("%w: client ip resolver cannot be nil", ErrInvalidConfig)
			}
			s.router.clientip = resolver
			return nil
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
// packages that use route annotation.
func WithAnnotation(key, value any) RouteOption {
	return optionFunc(func(s sealedOption) error {
		if key == nil {
			return fmt.Errorf("%w: annotation key is nil", ErrInvalidConfig)
		}
		if !reflect.ValueOf(key).Comparable() {
			return fmt.Errorf("%w: annotation key is not comparable", ErrInvalidConfig)
		}
		if s.route.annots == nil {
			s.route.annots = make(map[any]any, 1)
		}
		s.route.annots[key] = value
		return nil
	})
}

// WithName assigns a name to a route for identification and lookup purposes.
// The name must be unique among all other routes registered.
func WithName(name string) RouteOption {
	return optionFunc(func(s sealedOption) error {
		if name == "" {
			return fmt.Errorf("%w: empty route name", ErrInvalidConfig)
		}
		s.route.name = name
		return nil
	})
}

// WithMatchersPriority sets the priority for a route with matchers. When multiple routes share the same pattern
// (regardless of param names) and have overlapping methods, matchers are evaluated by priority (highest first).
// Routes with equal priority may be evaluated in any order. Routes without matchers are always evaluated last.
// If unset or 0, the priority defaults to the number of matchers. Note that routes with specific methods are
// always evaluated before method-less routes, regardless of priority.
func WithMatchersPriority(priority uint) RouteOption {
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

// WithSchemeMatcher attaches a URL scheme matcher to a route. The matcher ensures that requests are
// only routed to the handler if the connection scheme matches the given value. Only "http" and "https"
// are accepted. The scheme is ONLY derived from the TLS state of the connection between the client and
// the server. Behind a TLS-terminating reverse proxy, this matcher therefore reflects the proxy-to-server
// hop, not the original client connection; to route on the original client scheme (e.g. via
// X-Forwarded-Proto), implement a custom [Matcher] that reads the header within a trust boundary you
// control. Note that r.URL.Scheme is intentionally ignored: it can be set by an attacker via HTTP/1.1
// absolute-form requests. Multiple matchers can be attached to the same route. All matchers
// must match for the route to be eligible.
func WithSchemeMatcher(scheme string) interface {
	RouteOption
	MatcherOption
} {
	return optionFunc(func(s sealedOption) error {
		matcher, err := MatchScheme(scheme)
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
	MatcherOption
} {
	return optionFunc(func(s sealedOption) error {
		for i := range matchers {
			if matchers[i] == nil {
				return fmt.Errorf("%w: matcher cannot be nil", ErrInvalidConfig)
			}
			s.route.matchers = append(s.route.matchers, matchers[i])
		}
		return nil
	})
}

// WithPrettyLogs configures the router with human-readable, colorized logging optimized for terminal output.
// It registers the following middleware at the front of the chain:
//   - [Recovery] middleware for the [RouteHandler] scope, which catches panics and logs stack traces
//   - [Logger] middleware for [AllHandlers] scope, which logs request details
//
// This option prioritizes readability over performance and is not recommended for high-throughput applications.
// For production workloads, prefer structured logging with a performance-oriented [slog.Handler] such as
// zerolog or zap.
func WithPrettyLogs() GlobalOption {
	return optionFunc(func(s sealedOption) error {
		s.router.mws = append([]middleware{
			{Recovery(slogpretty.DefaultHandler), RouteHandler, true},
			{Logger(slogpretty.DefaultHandler), AllHandlers, true},
		}, s.router.mws...)
		return nil
	})
}
