// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"context"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// ContextCloser extends [Context] for manually created instances, adding a Close method
// to release resources after use.
type ContextCloser interface {
	Context
	// Close releases the context to be reused later.
	Close()
}

// Context represents the context of the current HTTP request. It provides methods to access request data and
// to write a response. Be aware that the Context API is not thread-safe and its lifetime should be limited to the
// duration of the [HandlerFunc] execution, as the underlying implementation may be reused a soon as the handler return.
// (see [Context.Clone] method).
type Context interface {
	// Request returns the current [http.Request].
	Request() *http.Request
	// SetRequest sets the [*http.Request].
	SetRequest(r *http.Request)
	// Writer method returns a custom [ResponseWriter] implementation.
	Writer() ResponseWriter
	// SetWriter sets the [ResponseWriter].
	SetWriter(w ResponseWriter)
	// RemoteIP parses the IP from [http.Request.RemoteAddr], normalizes it, and returns an IP address. The returned [net.IPAddr]
	// may contain a zone identifier. RemoteIP never returns nil, even if parsing the IP fails.
	RemoteIP() *net.IPAddr
	// ClientIP returns the "real" client IP address based on the configured [ClientIPStrategy].
	// The strategy is set using the [WithClientIPStrategy] option. There is no sane default, so if no strategy is configured,
	// the method returns [ErrNoClientIPStrategy].
	//
	// The strategy used must be chosen and tuned for your network configuration. This should result
	// in the strategy never returning an error -- i.e., never failing to find a candidate for the "real" IP.
	// Consequently, getting an error result should be treated as an application error, perhaps even
	// worthy of panicking.
	//
	// The returned [net.IPAddr] may contain a zone identifier.
	//
	// This api is EXPERIMENTAL and is likely to change in future release.
	ClientIP() (*net.IPAddr, error)
	// Path returns the registered path or an empty string if the handler is called in a scope other than [RouteHandler].
	Path() string
	// Route returns the registered [Route] or nil if the handler is called in a scope other than [RouteHandler].
	Route() *Route
	// Params returns a range iterator over the matched wildcard parameters for the current route.
	Params() iter.Seq[Param]
	// Param retrieve a matching wildcard parameter by name.
	Param(name string) string
	// QueryParams parses the [http.Request] raw query and returns the corresponding values.
	QueryParams() url.Values
	// QueryParam returns the first query value associated with the given key.
	QueryParam(name string) string
	// SetHeader sets the response header for the given key to the specified value.
	SetHeader(key, value string)
	// Header retrieves the value of the request header for the given key.
	Header(key string) string
	// String sends a formatted string with the specified status code.
	String(code int, format string, values ...any) error
	// Blob sends a byte slice with the specified status code and content type.
	Blob(code int, contentType string, buf []byte) error
	// Stream sends data from an [io.Reader] with the specified status code and content type.
	Stream(code int, contentType string, r io.Reader) error
	// Redirect sends an HTTP redirect response with the given status code and URL.
	Redirect(code int, url string) error
	// Clone returns a copy of the [Context] that is safe to use after the [HandlerFunc] returns.
	Clone() Context
	// CloneWith returns a shallow copy of the current [Context], substituting its [ResponseWriter] and [http.Request]
	// with the provided ones. The method is designed for zero allocation during the copy process. The returned
	// [ContextCloser] must be closed once no longer needed. This functionality is particularly beneficial for
	// middlewares that need to wrap their custom [ResponseWriter] while preserving the state of the original [Context].
	CloneWith(w ResponseWriter, r *http.Request) ContextCloser
	// Scope returns the [HandlerScope] associated with the current [Context].
	// This indicates the scope in which the handler is being executed, such as [RouteHandler], [NoRouteHandler], etc.
	Scope() HandlerScope
	// Tree is a local copy of the [Tree] in use to serve the request.
	Tree() *Tree
	// Fox returns the [Router] instance.
	Fox() *Router
	// Rehydrate updates the current [Context] to serve the provided [Route], bypassing the need for a full tree lookup.
	// It succeeds only if the [http.Request]'s URL path strictly matches the given [Route]. If successful, the internal state
	// of the context is updated, allowing the context to serve the route directly, regardless of whether the route
	// still exists in the routing tree. This provides a key advantage in concurrent scenarios where routes may be
	// modified by other threads, as Rehydrate guarantees success if the path matches, without requiring serial execution
	// or tree lookups. Note that the context's state is only mutated if the rehydration is successful.
	// This api is EXPERIMENTAL and is likely to change in future release.
	Rehydrate(route *Route) bool
}

// cTx holds request-related information and allows interaction with the [ResponseWriter].
type cTx struct {
	w         ResponseWriter
	req       *http.Request
	params    *Params
	tsrParams *Params
	skipNds   *skippedNodes
	route     *Route

	// tree at allocation (read-only, no reset)
	tree *Tree
	// router at allocation (read-only, no reset)
	fox         *Router
	cachedQuery url.Values
	rec         recorder
	scope       HandlerScope
	tsr         bool
}

// Rehydrate updates the current [Context] to serve the provided [Route], bypassing the need for a full tree lookup.
// It succeeds only if the [http.Request]'s URL path strictly matches the given [Route]. If successful, the internal state
// of the context is updated, allowing the context to serve the route directly, regardless of whether the route
// still exists in the routing tree. This provides a key advantage in concurrent scenarios where routes may be
// modified by other threads, as Rehydrate guarantees success if the path matches, without requiring serial execution
// or tree lookups. Note that the context's state is only mutated if the rehydration is successful.
// This api is EXPERIMENTAL and is likely to change in future release.
func (c *cTx) Rehydrate(route *Route) bool {

	target := c.req.URL.Path
	if len(c.req.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		target = c.req.URL.RawPath
	}

	var params *Params
	if c.tsr {
		*c.params = (*c.params)[:0]
		params = c.params
	} else {
		*c.tsrParams = (*c.tsrParams)[:0]
		params = c.tsrParams
	}

	if !route.hydrateParams(target, params) {
		return false
	}

	*c.params, *c.tsrParams = *c.tsrParams, *c.params
	c.cachedQuery = nil
	c.route = route
	c.scope = RouteHandler

	return true
}

// reset resets the [Context] to its initial state, attaching the provided [http.ResponseWriter] and [http.Request].
// Caution: always pass the original [http.ResponseWriter] to this method, not the [ResponseWriter] itself, to
// avoid wrapping the [ResponseWriter] within itself. Use wisely! Note that ServeHTTP is managing the reset of
// c.route and c.tsr.
func (c *cTx) reset(w http.ResponseWriter, r *http.Request) {
	c.rec.reset(w)
	c.req = r
	c.w = &c.rec
	c.cachedQuery = nil
	c.scope = RouteHandler
	*c.params = (*c.params)[:0]
}

func (c *cTx) resetNil() {
	c.req = nil
	c.w = nil
	c.cachedQuery = nil
	c.route = nil
	*c.params = (*c.params)[:0]
}

// resetWithWriter resets the [Context] to its initial state, attaching the provided [ResponseWriter] and [http.Request].
func (c *cTx) resetWithWriter(w ResponseWriter, r *http.Request) {
	c.req = r
	c.w = w
	c.tsr = false
	c.cachedQuery = nil
	c.route = nil
	c.scope = RouteHandler
	*c.params = (*c.params)[:0]
}

// Request returns the [http.Request].
func (c *cTx) Request() *http.Request {
	return c.req
}

// SetRequest sets the [http.Request].
func (c *cTx) SetRequest(r *http.Request) {
	c.req = r
}

// Writer returns the [ResponseWriter].
func (c *cTx) Writer() ResponseWriter {
	return c.w
}

// SetWriter sets the [ResponseWriter].
func (c *cTx) SetWriter(w ResponseWriter) {
	c.w = w
}

// RemoteIP parses the IP from [http.Request.RemoteAddr], normalizes it, and returns a [net.IPAddr].
// It never returns nil, even if parsing the IP fails.
func (c *cTx) RemoteIP() *net.IPAddr {
	ipStr, _, _ := net.SplitHostPort(c.req.RemoteAddr)

	ip, zone := splitHostZone(ipStr)
	ipAddr := &net.IPAddr{
		IP:   net.ParseIP(ip),
		Zone: zone,
	}

	if ipAddr.IP == nil {
		return &net.IPAddr{}
	}

	return ipAddr
}

// ClientIP returns the "real" client IP address based on the configured [ClientIPStrategy].
// The strategy is set using the [WithClientIPStrategy] option. If no strategy is configured,
// the method returns error [ErrNoClientIPStrategy].
//
// The strategy used must be chosen and tuned for your network configuration. This should result
// in the strategy never returning an error -- i.e., never failing to find a candidate for the "real" IP.
// Consequently, getting an error result should be treated as an application error, perhaps even
// worthy of panicking.
// This api is EXPERIMENTAL and is likely to change in future release.
func (c *cTx) ClientIP() (*net.IPAddr, error) {
	// We may be in a handler which does not match a route like NotFound handler.
	if c.route == nil {
		ipStrategy := c.fox.ipStrategy
		return ipStrategy.ClientIP(c)
	}
	return c.route.ipStrategy.ClientIP(c)
}

// Params returns an iterator over the matched wildcard parameters for the current route.
func (c *cTx) Params() iter.Seq[Param] {
	return func(yield func(Param) bool) {
		if c.tsr {
			for _, p := range *c.tsrParams {
				if !yield(p) {
					return
				}
			}
			return
		}
		for _, p := range *c.params {
			if !yield(p) {
				return
			}
		}
	}
}

// Param retrieve a matching wildcard segment by name.
func (c *cTx) Param(name string) string {
	for p := range c.Params() {
		if p.Key == name {
			return p.Value
		}
	}
	return ""
}

// QueryParams parses the [http.Request] raw query and returns the corresponding values.
func (c *cTx) QueryParams() url.Values {
	return c.getQueries()
}

// QueryParam returns the first value associated with the given key.
func (c *cTx) QueryParam(name string) string {
	return c.getQueries().Get(name)
}

// SetHeader sets the response header for the given key to the specified value.
func (c *cTx) SetHeader(key, value string) {
	c.w.Header().Set(key, value)
}

// Header retrieves the value of the request header for the given key.
func (c *cTx) Header(key string) string {
	return c.req.Header.Get(key)
}

// Path returns the registered path or an empty string if the handler is called in a scope other than [RouteHandler].
func (c *cTx) Path() string {
	if c.route == nil {
		return ""
	}
	return c.route.path
}

// Route returns the registered [Route] or nil if the handler is called in a scope other than [RouteHandler].
func (c *cTx) Route() *Route {
	return c.route
}

// String sends a formatted string with the specified status code.
func (c *cTx) String(code int, format string, values ...any) (err error) {
	if c.w.Header().Get(HeaderContentType) == "" {
		c.w.Header().Set(HeaderContentType, MIMETextPlainCharsetUTF8)
	}
	c.w.WriteHeader(code)
	_, err = fmt.Fprintf(c.w, format, values...)
	return
}

// Blob sends a byte slice with the specified status code and content type.
func (c *cTx) Blob(code int, contentType string, buf []byte) (err error) {
	c.w.Header().Set(HeaderContentType, contentType)
	c.w.WriteHeader(code)
	_, err = c.w.Write(buf)
	return
}

// Stream sends data from an [io.Reader] with the specified status code and content type.
func (c *cTx) Stream(code int, contentType string, r io.Reader) (err error) {
	c.w.Header().Set(HeaderContentType, contentType)
	c.w.WriteHeader(code)
	_, err = io.Copy(c.w, r)
	return
}

// Redirect sends an HTTP redirect response with the given status code and URL.
func (c *cTx) Redirect(code int, url string) error {
	if code < http.StatusMultipleChoices || code > http.StatusPermanentRedirect {
		return ErrInvalidRedirectCode
	}
	http.Redirect(c.w, c.req, url, code)
	return nil
}

// Tree is a local copy of the [Tree] in use to serve the request.
func (c *cTx) Tree() *Tree {
	return c.tree
}

// Fox returns the [Router] instance.
func (c *cTx) Fox() *Router {
	return c.fox
}

// Clone returns a deep copy of the [Context] that is safe to use after the [HandlerFunc] returns.
// Any attempt to write on the [ResponseWriter] will panic with the error [ErrDiscardedResponseWriter].
func (c *cTx) Clone() Context {
	cp := cTx{
		rec:   c.rec,
		req:   c.req.Clone(c.req.Context()),
		fox:   c.fox,
		tree:  c.tree,
		route: c.route,
		scope: c.scope,
		tsr:   c.tsr,
	}

	cp.rec.ResponseWriter = noopWriter{c.rec.Header().Clone()}
	cp.w = noUnwrap{&cp.rec}
	if !c.tsr {
		params := make(Params, len(*c.params))
		copy(params, *c.params)
		cp.params = &params
	} else {
		tsrParams := make(Params, len(*c.tsrParams))
		copy(tsrParams, *c.tsrParams)
		cp.tsrParams = &tsrParams
	}

	cp.cachedQuery = nil
	return &cp
}

// CloneWith returns a shallow copy of the current [Context], substituting its [ResponseWriter] and [http.Request] with the
// provided ones. The method is designed for zero allocation during the copy process. The returned [ContextCloser] must
// be closed once no longer needed. This functionality is particularly beneficial for middlewares that need to wrap
// their custom [ResponseWriter] while preserving the state of the original [Context].
func (c *cTx) CloneWith(w ResponseWriter, r *http.Request) ContextCloser {
	cp := c.tree.ctx.Get().(*cTx)
	cp.req = r
	cp.w = w
	cp.route = c.route
	cp.scope = c.scope
	cp.cachedQuery = nil
	cp.tsr = c.tsr

	if !c.tsr {
		copyParams(c.params, cp.params)
	} else {
		copyParams(c.tsrParams, cp.tsrParams)
	}

	return cp
}

func copyParams(src, dst *Params) {
	if cap(*src) > cap(*dst) {
		// Grow dst to a least cap(src)
		*dst = slices.Grow(*dst, cap(*src))
	}
	// cap(dst) >= cap(src)
	// now constraint into len(src) & cap(src)
	*dst = (*dst)[:len(*src):cap(*src)]
	copy(*dst, *src)
}

// Scope returns the HandlerScope associated with the current Context.
// This indicates the scope in which the handler is being executed, such as RouteHandler, NoRouteHandler, etc.
func (c *cTx) Scope() HandlerScope {
	return c.scope
}

// Close releases the context to be reused later.
func (c *cTx) Close() {
	// Put back the context, if not extended more than max params or max depth, allowing
	// the slice to naturally grow within the constraint.
	if cap(*c.params) > int(c.tree.maxParams.Load()) || cap(*c.skipNds) > int(c.tree.maxDepth.Load()) {
		return
	}
	c.tree.ctx.Put(c)
}

func (c *cTx) getQueries() url.Values {
	if c.cachedQuery == nil {
		if c.req != nil {
			c.cachedQuery = c.req.URL.Query()
		} else {
			c.cachedQuery = url.Values{}
		}
	}
	return c.cachedQuery
}

// WrapF is an adapter for wrapping http.HandlerFunc and returns a HandlerFunc function.
// The route parameters are being accessed by the wrapped handler through the context.
func WrapF(f http.HandlerFunc) HandlerFunc {
	return func(c Context) {
		var params Params = slices.Collect(c.Params())
		if len(params) > 0 {
			ctx := context.WithValue(c.Request().Context(), paramsKey, params)
			f.ServeHTTP(c.Writer(), c.Request().WithContext(ctx))
			return
		}

		f.ServeHTTP(c.Writer(), c.Request())
	}
}

// WrapH is an adapter for wrapping http.Handler and returns a HandlerFunc function.
// The route parameters are being accessed by the wrapped handler through the context.
func WrapH(h http.Handler) HandlerFunc {
	return func(c Context) {
		var params Params = slices.Collect(c.Params())
		if len(params) > 0 {
			ctx := context.WithValue(c.Request().Context(), paramsKey, params)
			h.ServeHTTP(c.Writer(), c.Request().WithContext(ctx))
			return
		}

		h.ServeHTTP(c.Writer(), c.Request())
	}
}

func splitHostZone(s string) (host, zone string) {
	// This is copied from an unexported function in the Go stdlib:
	// https://github.com/golang/go/blob/5c9b6e8e63e012513b1cb1a4a08ff23dec4137a1/src/net/ipsock.go#L219-L228

	// The IPv6 scoped addressing zone identifier starts after the last percent sign.
	if i := strings.LastIndexByte(s, '%'); i > 0 {
		host, zone = s[:i], s[i+1:]
	} else {
		host = s
	}
	return
}
