// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"context"
	"io"
	"iter"
	"net"
	"net/http"
	"net/url"
	"slices"

	"github.com/tigerwill90/fox/internal/bytesconv"
	"github.com/tigerwill90/fox/internal/netutil"
)

// RequestContext provides read-only access to incoming HTTP request data, including request properties,
// headers, query parameters, and client IP information.
type RequestContext interface {
	// Request returns the current [http.Request].
	Request() *http.Request
	// RemoteIP parses the IP from [http.Request.RemoteAddr], normalizes it, and returns an IP address. The returned [net.IPAddr]
	// may contain a zone identifier. RemoteIP never returns nil, even if parsing the IP fails.
	RemoteIP() *net.IPAddr
	// ClientIP returns the "real" client IP address based on the configured [ClientIPResolver].
	// The resolver is set using the [WithClientIPResolver] option. There is no sane default, so if no resolver is configured,
	// the method returns [ErrNoClientIPResolver].
	//
	// The resolver used must be chosen and tuned for your network configuration. This should result
	// in a resolver never returning an error -- i.e., never failing to find a candidate for the "real" IP.
	// Consequently, getting an error result should be treated as an application error, perhaps even
	// worthy of panicking.
	//
	// The returned [net.IPAddr] may contain a zone identifier.
	ClientIP() (*net.IPAddr, error)
	// Method returns the request method.
	Method() string
	// Path returns the request [url.URL.RawPath] if not empty, or fallback to the [url.URL.Path].
	Path() string
	// Host returns the request host.
	Host() string
	// QueryParams parses the [http.Request] raw query and returns the corresponding values. The result is cached after
	// the first call.
	QueryParams() url.Values
	// QueryParam returns the first query value associated with the given key. The query parameters are parsed and
	// cached on first access.
	QueryParam(name string) string
	// Header retrieves the value of the request header for the given key.
	Header(key string) string
}

// Context holds request-related information and allows interaction with the [ResponseWriter].
type Context struct {
	w         ResponseWriter
	req       *http.Request
	params    *[]string
	tsrParams *[]string
	keys      *[]string
	// TODO use Request.Pattern
	pattern       string
	skipStack     *skipStack
	route         *Route
	tree          *iTree  // no reset
	fox           *Router // no reset
	cachedQueries url.Values
	rec           recorder
	scope         HandlerScope
	tsr           bool
}

// reset resets the [Context] to its initial state, attaching the provided [http.ResponseWriter] and [http.Request].
// Caution: always pass the original [http.ResponseWriter] to this method, not the [ResponseWriter] itself, to
// avoid wrapping the [ResponseWriter] within itself. Use wisely! Note that ServeHTTP is managing the reset of
// c.route and c.tsr.
func (c *Context) reset(w http.ResponseWriter, r *http.Request) {
	c.rec.reset(w)
	c.req = r
	c.w = &c.rec
	c.cachedQueries = nil
	c.scope = RouteHandler
	*c.params = (*c.params)[:0]
}

func (c *Context) resetNil() {
	c.req = nil
	c.w = nil
	c.cachedQueries = nil
	c.route = nil
	*c.params = (*c.params)[:0]
	c.tsr = false
}

// resetWithRequest resets the [Context] to its initial state, with the provided [http.Request]. This is used
// only by caller that don't return the [Context] (e.g. Match). Use wisely! Note that caller is managing the reset of c.tsr.
func (c *Context) resetWithRequest(r *http.Request) {
	c.req = r
	c.w = nil
	c.cachedQueries = nil
	c.route = nil
	*c.params = (*c.params)[:0]
}

// resetWithWriter resets the [Context] to its initial state, with the provided [ResponseWriter] and [http.Request].
// Use wisely! Note that caller is managing the reset of c.route and c.tsr.
func (c *Context) resetWithWriter(w ResponseWriter, r *http.Request) {
	c.req = r
	c.w = w
	c.cachedQueries = nil
	c.scope = RouteHandler
	*c.params = (*c.params)[:0]
}

// Request returns the [http.Request].
func (c *Context) Request() *http.Request {
	return c.req
}

// SetRequest sets the [http.Request].
func (c *Context) SetRequest(r *http.Request) {
	c.cachedQueries = nil // In case r is a different request than c.req
	c.req = r
}

// Writer returns the [ResponseWriter].
func (c *Context) Writer() ResponseWriter {
	return c.w
}

// SetWriter sets the [ResponseWriter].
func (c *Context) SetWriter(w ResponseWriter) {
	c.w = w
}

// RemoteIP parses the IP from [http.Request.RemoteAddr], normalizes it, and returns a [net.IPAddr].
// It never returns nil, even if parsing the IP fails.
func (c *Context) RemoteIP() *net.IPAddr {
	ipStr, _, _ := net.SplitHostPort(c.req.RemoteAddr)

	ip, zone := netutil.SplitHostZone(ipStr)
	ipAddr := &net.IPAddr{
		IP:   net.ParseIP(ip),
		Zone: zone,
	}

	if ipAddr.IP == nil {
		return &net.IPAddr{}
	}

	return ipAddr
}

// ClientIP returns the "real" client IP address based on the configured [ClientIPResolver].
// The resolver is set using the [WithClientIPResolver] option. If no resolver is configured,
// the method returns error [ErrNoClientIPResolver].
//
// The resolver used must be chosen and tuned for your network configuration. This should result
// in a resolver never returning an error -- i.e., never failing to find a candidate for the "real" IP.
// Consequently, getting an error result should be treated as an application error, perhaps even
// worthy of panicking.
func (c *Context) ClientIP() (*net.IPAddr, error) {
	// We may be in a handler which does not match a route like NotFound handler.
	if c.route == nil {
		resolver := c.fox.clientip
		return resolver.ClientIP(c)
	}
	return c.route.clientip.ClientIP(c)
}

// Params returns an iterator over the matched wildcard parameters for the current route.
func (c *Context) Params() iter.Seq[Param] {
	return func(yield func(Param) bool) {
		if c.tsr {
			for i, p := range *c.tsrParams {
				if !yield(Param{Key: (*c.keys)[i], Value: p}) {
					return
				}
			}
			return
		}
		for i, p := range *c.params {
			if !yield(Param{Key: (*c.keys)[i], Value: p}) {
				return
			}
		}
	}
}

// Param retrieve a matching wildcard segment by name.
func (c *Context) Param(name string) string {
	if c.tsr {
		for i := range *c.tsrParams {
			key := (*c.keys)[i]
			if key == name {
				return (*c.tsrParams)[i]
			}
		}
		return ""
	}

	for i := range *c.params {
		key := (*c.keys)[i]
		if key == name {
			return (*c.params)[i]
		}
	}
	return ""
}

// Method returns the request method.
func (c *Context) Method() string {
	return c.req.Method
}

// Path returns the request [url.URL.RawPath] if not empty, or fallback to the [url.URL.Path].
func (c *Context) Path() string {
	path := c.req.URL.Path
	if len(c.req.URL.RawPath) > 0 {
		// Using RawPath to prevent unintended match (e.g. /search/a%2Fb/1)
		path = c.req.URL.RawPath
	}
	return path
}

// Host returns the request host.
func (c *Context) Host() string {
	return c.req.Host
}

// QueryParams parses the [http.Request] raw query and returns the corresponding values. The result is cached after
// the first call.
func (c *Context) QueryParams() url.Values {
	return c.getQueries()
}

// QueryParam returns the first query value associated with the given key. The query parameters are parsed and
// cached on first access.
func (c *Context) QueryParam(name string) string {
	return c.getQueries().Get(name)
}

// SetHeader sets the response header for the given key to the specified value.
func (c *Context) SetHeader(key, value string) {
	c.w.Header().Set(key, value)
}

// AddHeader add the response header for the given key to the specified value.
func (c *Context) AddHeader(key, value string) {
	c.w.Header().Add(key, value)
}

// Header retrieves the value of the request header for the given key.
func (c *Context) Header(key string) string {
	return c.req.Header.Get(key)
}

// Pattern returns the registered route pattern or an empty string if the handler is called in a scope other than [RouteHandler].
func (c *Context) Pattern() string {
	if c.route == nil {
		return ""
	}
	return c.pattern
}

// Route returns the registered [Route] or nil if the handler is called in a scope other than [RouteHandler].
func (c *Context) Route() *Route {
	return c.route
}

// String sends a formatted string with the specified status code.
func (c *Context) String(code int, s string) (err error) {
	if c.w.Header().Get(HeaderContentType) == "" {
		c.w.Header().Set(HeaderContentType, MIMETextPlainCharsetUTF8)
	}
	c.w.WriteHeader(code)
	_, err = c.w.Write(bytesconv.Bytes(s))
	return
}

// Blob sends a byte slice with the specified status code and content type.
func (c *Context) Blob(code int, contentType string, buf []byte) (err error) {
	c.w.Header().Set(HeaderContentType, contentType)
	c.w.WriteHeader(code)
	_, err = c.w.Write(buf)
	return
}

// Stream sends data from an [io.Reader] with the specified status code and content type.
func (c *Context) Stream(code int, contentType string, r io.Reader) (err error) {
	c.w.Header().Set(HeaderContentType, contentType)
	c.w.WriteHeader(code)
	_, err = io.Copy(c.w, r)
	return
}

// TODO superflus, delete me
// Redirect sends an HTTP redirect response with the given status code and URL.
func (c *Context) Redirect(code int, url string) error {
	if code < http.StatusMultipleChoices || code > http.StatusPermanentRedirect {
		return ErrInvalidRedirectCode
	}
	http.Redirect(c.w, c.req, url, code)
	return nil
}

// Fox returns the [Router] instance.
func (c *Context) Fox() *Router {
	return c.fox
}

// Clone returns a deep copy of the [Context] that is safe to use after the [HandlerFunc] returns.
// Any attempt to write on the [ResponseWriter] will panic with the error [ErrDiscardedResponseWriter].
func (c *Context) Clone() *Context {
	cp := Context{
		rec:     c.rec,
		req:     c.req.Clone(c.req.Context()),
		fox:     c.fox,
		route:   c.route,
		scope:   c.scope,
		tsr:     c.tsr,
		pattern: c.pattern,
	}

	cp.rec.ResponseWriter = noopWriter{c.rec.Header().Clone()}
	cp.w = noUnwrap{&cp.rec}

	if !c.tsr {
		params := make([]string, len(*c.params))
		copy(params, *c.params)
		cp.params = &params
	} else {
		tsrParams := make([]string, len(*c.tsrParams))
		copy(tsrParams, *c.tsrParams)
		cp.tsrParams = &tsrParams
	}

	keys := make([]string, len(*c.keys))
	copy(keys, *c.keys)
	cp.keys = &keys

	return &cp
}

// CloneWith returns a shallow copy of the current [Context], substituting its [ResponseWriter] and [http.Request] with the
// provided ones. The method is designed for zero allocation during the copy process. The returned [Context] must
// be closed once no longer needed. This functionality is particularly beneficial for middlewares that need to wrap
// their custom [ResponseWriter] while preserving the state of the original [Context].
func (c *Context) CloneWith(w ResponseWriter, r *http.Request) *Context {
	cp := c.tree.pool.Get().(*Context)
	cp.req = r
	cp.w = w
	cp.route = c.route
	cp.pattern = c.pattern
	cp.scope = c.scope
	cp.cachedQueries = nil // In case r is a different request than c.req
	cp.tsr = c.tsr

	copyWithResize(cp.keys, c.keys)

	if !c.tsr {
		copyWithResize(cp.params, c.params)
	} else {
		copyWithResize(cp.tsrParams, c.tsrParams)
	}

	return cp
}

func copyWithResize[S ~[]T, T any](dst, src *S) {
	if len(*src) > len(*dst) { // TODO could be cap(*dst)
		// Grow dst cap to a least len(src)
		*dst = slices.Grow(*dst, len(*src)-len(*dst))
	}
	// cap(dst) >= len(src)
	// now constraint into len(src) & cap(dst)
	*dst = (*dst)[:len(*src):cap(*dst)]
	copy(*dst, *src)
}

// Scope returns the [HandlerScope] associated with the current [Context].
// This indicates the scope in which the handler is being executed, such as [RouteHandler], [NoRouteHandler], etc.
func (c *Context) Scope() HandlerScope {
	return c.scope
}

// Close releases the context to be reused later.
func (c *Context) Close() {
	c.tree.pool.Put(c)
}

func (c *Context) getQueries() url.Values {
	if c.cachedQueries == nil {
		if c.req != nil {
			c.cachedQueries = c.req.URL.Query()
		} else {
			c.cachedQueries = url.Values{}
		}
	}
	return c.cachedQueries
}

// WrapF is an adapter for wrapping [http.HandlerFunc] and returns a [HandlerFunc] function.
// The route parameters are being accessed by the wrapped handler through the context.
func WrapF(f http.HandlerFunc) HandlerFunc {
	return func(c *Context) {
		if route := c.Route(); route != nil && route.ParamsLen() > 0 {
			params := slices.AppendSeq(make(Params, 0, route.ParamsLen()), c.Params())
			ctx := context.WithValue(c.Request().Context(), paramsKey, params)
			f.ServeHTTP(c.Writer(), c.Request().WithContext(ctx))
			return
		}

		f.ServeHTTP(c.Writer(), c.Request())
	}
}

// WrapH is an adapter for wrapping http.Handler and returns a [HandlerFunc] function.
// The route parameters are being accessed by the wrapped handler through the context.
func WrapH(h http.Handler) HandlerFunc {
	return func(c *Context) {
		if route := c.Route(); route != nil && route.ParamsLen() > 0 {
			params := slices.AppendSeq(make(Params, 0, route.ParamsLen()), c.Params())
			ctx := context.WithValue(c.Request().Context(), paramsKey, params)
			h.ServeHTTP(c.Writer(), c.Request().WithContext(ctx))
			return
		}

		h.ServeHTTP(c.Writer(), c.Request())
	}
}

// WrapM is an adapter for wrapping http.Handler middleware and returns a [MiddlewareFunc] function.
func WrapM(m func(http.Handler) http.Handler) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec := new(recorder)
				rec.reset(w)
				cc := c.CloneWith(rec, r)
				defer cc.Close()
				next(cc)
			})).ServeHTTP(c.Writer(), c.Request())
		}
	}
}
