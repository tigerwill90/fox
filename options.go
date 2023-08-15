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
	apply(*Router)
}

type optionFunc func(*Router)

func (o optionFunc) apply(r *Router) {
	o(r)
}

// WithNoRouteHandler register an HandlerFunc which is called when no matching route is found.
// By default, the DefaultNotFoundHandler is used.
func WithNoRouteHandler(handler HandlerFunc) Option {
	return optionFunc(func(r *Router) {
		if handler != nil {
			r.noRoute = handler
		}
	})
}

// WithNoMethodHandler register an HandlerFunc which is called when the request cannot be routed,
// but the same route exist for other methods. The "Allow" header it automatically set before calling the
// handler. By default, the DefaultMethodNotAllowedHandler is used. Note that this option automatically
// enable WithNoMethod.
func WithNoMethodHandler(handler HandlerFunc) Option {
	return optionFunc(func(r *Router) {
		if handler != nil {
			r.noMethod = handler
			r.handleMethodNotAllowed = true
		}
	})
}

// WithOptionsHandler register an HandlerFunc which is called on automatic OPTIONS requests. By default, the router
// respond with a 200 OK status code. The "Allow" header it automatically set before calling the handler. Note that custom OPTIONS
// handler take priority over automatic replies. This option automatically enable WithAutoOptions.
// This api is EXPERIMENTAL and is likely to change in future release.
func WithOptionsHandler(handler HandlerFunc) Option {
	return optionFunc(func(r *Router) {
		if handler != nil {
			r.autoOptions = handler
			r.handleOptions = true
		}
	})
}

// WithMiddleware attaches a global middleware to the router. Middlewares provided will be chained
// in the order they were added. Note that this option apply middleware to all handler, including NotFound,
// MethodNotAllowed and the internal redirect handler.
func WithMiddleware(m ...MiddlewareFunc) Option {
	return WithMiddlewareFor(AllHandlers, m...)
}

// WithMiddlewareFor attaches middleware to the router for a specified scope. Middlewares provided will be chained
// in the order they were added. The scope parameter determines which types of handlers the middleware will be applied to.
// Possible scopes include RouteHandlers (regular routes), NoRouteHandler, NoMethodHandler, RedirectHandler, OptionsHandler,
// and any combination of these. Use this option when you need fine-grained control over where the middleware is applied.
// This api is EXPERIMENTAL and is likely to change in future release.
func WithMiddlewareFor(scope MiddlewareScope, m ...MiddlewareFunc) Option {
	return optionFunc(func(r *Router) {
		for i := range m {
			r.mws = append(r.mws, middleware{m[i], scope})
		}
	})
}

// WithNoMethod enable to returns 405 Method Not Allowed instead of 404 Not Found
// when the route exist for another http verb. The "Allow" header it automatically set before calling the
// handler. Note that this option is automatically enabled when providing a custom handler with the
// option WithNoMethodHandler.
func WithNoMethod(enable bool) Option {
	return optionFunc(func(r *Router) {
		r.handleMethodNotAllowed = enable
	})
}

// WithAutoOptions enables automatic response to OPTIONS requests with, by default, a 200 OK status code.
// Use the WithOptionsHandler option to customize the response. When this option is enabled, the router automatically
// determines the "Allow" header value based on the methods registered for the given route. Note that custom OPTIONS
// handler take priority over automatic replies. This option is automatically enabled when providing a custom handler with
// the option WithOptionsHandler. This api is EXPERIMENTAL and is likely to change in future release.
func WithAutoOptions(enable bool) Option {
	return optionFunc(func(r *Router) {
		r.handleOptions = enable
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

// WithWriterSafety controls the rigor of inspections on the http.ResponseWriter for supported interfaces.
//
// When disabled, the protocol handling is optimistic and is chosen based solely on the request's ProtoMajor.
// It assumes the provided http.ResponseWriter supports all relevant interfaces for the chosen protocol, offering better
// performance. However, this can introduce risks of unexpected panics if the writer doesn't truly support the assumed interfaces.
// This option is designed for situations where you're certain about the capabilities of the original http.ResponseWriter.
//
// When enabled, the protocol handling is more conservative. The chosen protocol is based on a combination of
// the request's ProtoMajor and explicit type assertions against the http.ResponseWriter, ensuring compatibility with
// varying implementations and safety against unexpected panics. This mode offers a safer but slightly less performant approach.
//
// In most scenarios, users are recommended to keep the safety checks intact unless there's a compelling performance reason and
// confidence in the provided http.ResponseWriter's capabilities.
//
// This API is EXPERIMENTAL and is likely to change in future releases.
func WithWriterSafety(enable bool) Option {
	return optionFunc(func(r *Router) {
		r.writerSafety = enable
	})
}

// DefaultOptions configure the router to use the Recovery middleware for the RouteHandlers scope, enable automatic
// OPTIONS response and writer safety checks. Note that DefaultOptions push the Recovery middleware to the first
// position of the middleware chains.
func DefaultOptions() Option {
	return optionFunc(func(r *Router) {
		r.mws = append([]middleware{{Recovery(DefaultHandleRecovery), RouteHandlers}}, r.mws...)
		r.handleOptions = true
		r.writerSafety = true
	})
}
