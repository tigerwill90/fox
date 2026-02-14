// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

package fox

import (
	"context"
	"io"
	"iter"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/fox-toolkit/fox/internal/bytesconv"
	"github.com/fox-toolkit/fox/internal/netutil"
)

// RequestContext provides read-only access to incoming HTTP request data, including request properties,
// headers, query parameters, and client IP information. It is implemented by [Context].
//
// The RequestContext API is not thread-safe. Its lifetime is limited to the scope of its caller:
// within a [Matcher], it is valid only for the duration of the [Matcher.Match] call; within a [HandlerFunc],
// it is valid only for the duration of the handler execution. The underlying context may be reused after
// the call returns.
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
	// Pattern returns the registered route pattern or an empty string if the handler is called in a scope other than [RouteHandler].
	Pattern() string
}

// Context represents the context of the current HTTP request. It provides methods to access request data and
// to write a response. Be aware that the Context API is not thread-safe and its lifetime should be limited to the
// duration of the [HandlerFunc] execution, as the Context may be reused as soon as the handler returns.
type Context struct {
	w             ResponseWriter
	req           *http.Request
	params        *[]string
	paramsKeys    *[]string
	subPatterns   *[]string
	skipStack     *skipStack
	route         *Route
	tree          *iTree  // no reset
	fox           *Router // no reset
	pattern       string
	cachedQueries url.Values
	rec           recorder
	scope         HandlerScope
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
	*c.subPatterns = (*c.subPatterns)[:0]
}

func (c *Context) resetNil() {
	c.req = nil
	c.w = nil
	c.cachedQueries = nil
	c.route = nil
	*c.params = (*c.params)[:0]
	*c.subPatterns = (*c.subPatterns)[:0]
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
	*c.subPatterns = (*c.subPatterns)[:0]
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
		for i, p := range *c.params {
			if !yield(Param{Key: (*c.paramsKeys)[i], Value: p}) {
				return
			}
		}
	}
}

// Param retrieve a matching wildcard segment by name.
func (c *Context) Param(name string) string {
	for i := range *c.params {
		key := (*c.paramsKeys)[i]
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
	if len(c.req.URL.RawPath) > 0 {
		return c.req.URL.RawPath
	}
	return c.req.URL.Path
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
	switch len(*c.subPatterns) {
	case 0:
		return c.pattern
	case 1:
		return (*c.subPatterns)[0] + c.pattern
	}

	var sb strings.Builder
	sb.Grow(len(c.pattern) + sumLen(*c.subPatterns))
	for _, p := range *c.subPatterns {
		sb.WriteString(p)
	}
	sb.WriteString(c.pattern)
	return sb.String()
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

// Router returns the [Router] instance.
func (c *Context) Router() *Router {
	return c.fox
}

// Clone returns a deep copy of the [Context] that is safe to use after the [HandlerFunc] returns.
// Any attempt to write on the [ResponseWriter] will panic with the error [ErrDiscardedResponseWriter].
func (c *Context) Clone() *Context {
	cp := Context{
		rec:     c.rec,
		req:     c.req.Clone(c.req.Context()),
		fox:     c.fox, // Note: no tree here so Context.Close is noop.
		route:   c.route,
		scope:   c.scope,
		pattern: c.pattern,
	}

	cp.rec.ResponseWriter = noopWriter{c.rec.Header().Clone()}
	cp.w = noUnwrap{&cp.rec}

	subPatterns := make([]string, len(*c.subPatterns))
	copy(subPatterns, *c.subPatterns)
	cp.subPatterns = &subPatterns

	params := make([]string, len(*c.params))
	copy(params, *c.params)
	cp.params = &params

	keys := make([]string, len(*c.paramsKeys))
	copy(keys, *c.paramsKeys)
	cp.paramsKeys = &keys

	return &cp
}

// CloneWith returns a shallow copy of the current [Context], substituting its [ResponseWriter] and [http.Request] with the
// provided ones. The method is designed for zero allocation during the copy process. The caller is responsible for
// closing the returned [Context] by calling [Context.Close] when it is no longer needed. This functionality is particularly
// beneficial for middlewares that need to wrap their custom [ResponseWriter] while preserving the state of the original
// [Context].
func (c *Context) CloneWith(w ResponseWriter, r *http.Request) *Context {
	cp := c.tree.pool.Get().(*Context)
	cp.req = r
	cp.w = w
	cp.route = c.route
	cp.scope = c.scope
	cp.pattern = c.pattern
	cp.cachedQueries = nil // For safety, in case r is a different request than c.req

	copyWithResize(cp.subPatterns, c.subPatterns)
	copyWithResize(cp.paramsKeys, c.paramsKeys)
	copyWithResize(cp.params, c.params)

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

// Close releases the context to be reused later. This method must be called for contexts obtained via
// [Context.CloneWith], [Router.Lookup], or [Txn.Lookup]. Contexts passed to a [HandlerFunc] are managed
// automatically by the router and should not be closed manually. See also [Context] for more details.
func (c *Context) Close() {
	if c.tree != nil {
		c.tree.pool.Put(c)
	}
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
	return WrapH(f)
}

// WrapH is an adapter for wrapping http.Handler and returns a [HandlerFunc] function.
// The route parameters are being accessed by the wrapped handler through the context.
func WrapH(h http.Handler) HandlerFunc {
	return wrapH{h: h}.handle
}

// WrapM is an adapter for wrapping http.Handler middleware and returns a [MiddlewareFunc] function.
// The route parameters are being accessed by the wrapped handler through the context.
func WrapM(m func(http.Handler) http.Handler) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return wrapM{next: next, m: m}.handle
	}
}

type wrapH struct {
	h http.Handler
}

func (hw wrapH) handle(c *Context) {
	req := c.Request()

	p := req.Pattern
	defer func() { req.Pattern = p }()

	req.Pattern = c.Pattern()
	if route := c.Route(); route != nil && route.ParamsLen() > 0 {
		params := slices.AppendSeq(make(Params, 0, route.ParamsLen()), c.Params())
		ctx := context.WithValue(req.Context(), paramsKey, params)
		hw.h.ServeHTTP(c.Writer(), req.WithContext(ctx))
		return
	}

	hw.h.ServeHTTP(c.Writer(), req)
}

type wrapM struct {
	next HandlerFunc
	m    func(http.Handler) http.Handler
}

func (mw wrapM) handle(c *Context) {
	req := c.Request()

	p := req.Pattern
	defer func() { req.Pattern = p }()

	req.Pattern = c.Pattern()
	if route := c.Route(); route != nil && route.ParamsLen() > 0 {
		params := slices.AppendSeq(make(Params, 0, route.ParamsLen()), c.Params())
		ctx := context.WithValue(c.Request().Context(), paramsKey, params)
		req = req.WithContext(ctx)
	}

	mw.m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Avoid allocation if w has not been wrapped by m.
		rec, ok := w.(*recorder)
		if !ok {
			rec = new(recorder)
			rec.reset(w)
		}
		cc := c.CloneWith(rec, r)
		defer cc.Close()
		mw.next(cc)
	})).ServeHTTP(c.Writer(), req)
}

func sumLen(s []string) int {
	var n int
	for _, v := range s {
		n += len(v)
	}
	return n
}
