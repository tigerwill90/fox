// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	netcontext "context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
)

// Context represents the context of the current HTTP request. It provides methods to access request data and
// to write a response. Be aware that the Context API is not designed to be thread-safe and its lifetime should be limited to the
// duration of the HandlerFunc execution, as the underlying implementation may be reused a soon as the handler return.
type Context interface {
	// Request returns the current *http.Request.
	Request() *http.Request
	// SetRequest sets the *http.Request.
	SetRequest(r *http.Request)
	// Writer method returns a custom ResponseWriter implementation. The returned ResponseWriter object implements additional
	// http.Flusher, http.Hijacker, io.ReaderFrom interfaces for HTTP/1.x requests and http.Flusher, http.Pusher interfaces
	// for HTTP/2 requests. These additional interfaces provide extra functionality and are used by underlying HTTP protocols
	// for specific tasks.
	//
	// In actual workload scenarios, the custom ResponseWriter satisfies interfaces for HTTP/1.x and HTTP/2 protocols,
	// however, if testing with e.g. httptest.Recorder, only the http.Flusher is available to the underlying ResponseWriter.
	// Therefore, while asserting interfaces like http.Hijacker will not fail, invoking Hijack method will panic if the
	// underlying ResponseWriter does not implement this interface.
	//
	// To facilitate testing with e.g. httptest.Recorder, use the WrapTestContextFlusher helper function which only exposes the
	// http.Flusher interface for the ResponseWriter.
	Writer() ResponseWriter
	// SetWriter sets the ResponseWriter.
	SetWriter(w ResponseWriter)
	// Path returns the registered path for the handler.
	Path() string
	// SetPath set the matching path for the handler.
	SetPath(path string)
	// Params returns a Params slice containing the matched
	// wildcard parameters.
	Params() Params
	// Param retrieve a matching wildcard parameter by name.
	Param(name string) string
	// Reset resets the Context to its initial state, attaching the provided Router,
	// http.ResponseWriter, and *http.Request.
	Reset(w http.ResponseWriter, r *http.Request)
	// Close release the Context and it's resource.
	Close()
}

type CtxBuilder struct {
	p sync.Pool
}

func NewCtxBuilder(fox *Router[*Ctx]) ContextBuilder[*Ctx] {
	b := new(CtxBuilder)
	b.p.New = func() any {
		params := make(Params, 0)
		skipNds := make(SkippedNodes[*Ctx], 0)
		return &Ctx{
			fox:     fox,
			tree:    fox.Tree(),
			skipNds: &skipNds,
			params:  &params,
			b:       b,
		}
	}

	return b
}

func (b *CtxBuilder) Get() *Ctx {
	c := b.p.Get().(*Ctx)
	c.path = ""
	c.cachedQuery = nil
	return c
}

func (b *CtxBuilder) Params(c *Ctx) *Params {
	return c.params
}

func (b *CtxBuilder) SkippedNodes(c *Ctx) *SkippedNodes[*Ctx] {
	return c.skipNds
}

// Ctx holds request-related information and allows interaction with the ResponseWriter.
type Ctx struct {
	w       ResponseWriter
	req     *http.Request
	params  *Params
	skipNds *SkippedNodes[*Ctx]

	// tree at allocation (read-only, no reset)
	tree *Tree[*Ctx]
	fox  *Router[*Ctx]
	b    *CtxBuilder

	cachedQuery url.Values
	path        string
	rec         recorder
}

// Reset resets the Context to its initial state, attaching the provided Router, http.ResponseWriter, and *http.Request.
// Caution: You should pass the original http.ResponseWriter to this method, not the ResponseWriter itself, to avoid
// wrapping the ResponseWriter within itself.
func (c *Ctx) Reset(w http.ResponseWriter, r *http.Request) {
	c.rec.reset(w)
	c.req = r
	c.w = &c.rec
}

// Request returns the *http.Request.
func (c *Ctx) Request() *http.Request {
	return c.req
}

// SetRequest sets the *http.Request.
func (c *Ctx) SetRequest(r *http.Request) {
	c.req = r
}

// Writer returns the ResponseWriter.
func (c *Ctx) Writer() ResponseWriter {
	return c.w
}

// SetWriter sets the ResponseWriter.
func (c *Ctx) SetWriter(w ResponseWriter) {
	c.w = w
}

// Ctx returns the context associated with the current request.
func (c *Ctx) Ctx() netcontext.Context {
	return c.req.Context()
}

// Params returns a Params slice containing the matched
// wildcard parameters.
func (c *Ctx) Params() Params {
	return *c.params
}

// Param retrieve a matching wildcard segment by name.
// It's a helper for c.Params.Get(name).
func (c *Ctx) Param(name string) string {
	for _, p := range c.Params() {
		if p.Key == name {
			return p.Value
		}
	}
	return ""
}

// QueryParams parses RawQuery and returns the corresponding values.
// It's a helper for c.Request.URL.Query(). Note that the parsed
// result is cached.
func (c *Ctx) QueryParams() url.Values {
	return c.getQueries()
}

// QueryParam returns the first value associated with the given key.
// It's a helper for c.QueryParams().Get(name).
func (c *Ctx) QueryParam(name string) string {
	return c.getQueries().Get(name)
}

// SetHeader sets the response header for the given key to the specified value.
func (c *Ctx) SetHeader(key, value string) {
	c.w.Header().Set(key, value)
}

// Header retrieves the value of the request header for the given key.
func (c *Ctx) Header(key string) string {
	return c.req.Header.Get(key)
}

// Path returns the registered path for the handler.
func (c *Ctx) Path() string {
	return c.path
}

// SetPath set the matching path for the handler.
func (c *Ctx) SetPath(path string) {
	c.path = path
}

// String sends a formatted string with the specified status code.
func (c *Ctx) String(code int, format string, values ...any) (err error) {
	if c.w.Header().Get(HeaderContentType) == "" {
		c.w.Header().Set(HeaderContentType, MIMETextPlainCharsetUTF8)
	}
	c.w.WriteHeader(code)
	_, err = fmt.Fprintf(c.w, format, values...)
	return
}

// Blob sends a byte slice with the specified status code and content type.
func (c *Ctx) Blob(code int, contentType string, buf []byte) (err error) {
	c.w.Header().Set(HeaderContentType, contentType)
	c.w.WriteHeader(code)
	_, err = c.w.Write(buf)
	return
}

// Stream sends data from an io.Reader with the specified status code and content type.
func (c *Ctx) Stream(code int, contentType string, r io.Reader) (err error) {
	c.w.Header().Set(HeaderContentType, contentType)
	c.w.WriteHeader(code)
	_, err = io.Copy(c.w, r)
	return
}

// Redirect sends an HTTP redirect response with the given status code and URL.
func (c *Ctx) Redirect(code int, url string) error {
	if code < http.StatusMultipleChoices || code > http.StatusPermanentRedirect {
		return ErrInvalidRedirectCode
	}
	http.Redirect(c.w, c.req, url, code)
	return nil
}

// Tree is a local copy of the Tree in use to serve the request.
func (c *Ctx) Tree() *Tree[*Ctx] {
	return c.tree
}

// Fox returns the Router in use to serve the request.
func (c *Ctx) Fox() *Router[*Ctx] {
	return c.fox
}

// Clone returns a copy of the Context that is safe to use after the HandlerFunc returns.
// Any attempt to write on the ResponseWriter will panic with the error ErrDiscardedResponseWriter.
func (c *Ctx) Clone() *Ctx {
	cp := Ctx{
		rec:  c.rec,
		req:  c.req.Clone(c.req.Context()),
		fox:  c.fox,
		tree: c.tree,
	}

	cp.rec.ResponseWriter = noopWriter{c.rec.Header().Clone()}
	cp.w = noUnwrap{&cp.rec}
	params := make(Params, len(*c.params))
	copy(params, *c.params)
	cp.params = &params
	cp.cachedQuery = nil
	return &cp
}

// CloneWith returns a copy of the current Context, substituting its ResponseWriter and
// http.Request with the provided ones. The method is designed for zero allocation during the
// copy process. The returned ContextCloser must be closed once no longer needed.
// This functionality is particularly beneficial for middlewares that need to wrap
// their custom ResponseWriter while preserving the state of the original Context.
func (c *Ctx) CloneWith(w ResponseWriter, r *http.Request) *Ctx {
	cp := c.b.Get()
	cp.req = r
	cp.w = w
	cp.path = c.path
	cp.fox = c.fox
	cp.cachedQuery = nil
	if len(*c.params) > len(*cp.params) {
		// Grow cp.params to a least cap(c.params)
		*cp.params = grow(*cp.params, len(*c.params)-len(*cp.params))
	}
	// cap(cp.params) >= cap(c.params)
	// now constraint into len(c.params) & cap(c.params)
	*cp.params = (*cp.params)[:len(*c.params):cap(*c.params)]
	copy(*cp.params, *c.params)
	return cp
}

// Close releases the context to be reused later.
func (c *Ctx) Close() {
	c.b.p.Put(c)
}

func (c *Ctx) getQueries() url.Values {
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
func WrapF[T Context](f http.HandlerFunc) HandlerFunc[T] {
	return func(c T) {
		if len(c.Params()) > 0 {
			ctx := netcontext.WithValue(c.Request().Context(), paramsKey, c.Params().Clone())
			f.ServeHTTP(c.Writer(), c.Request().WithContext(ctx))
			return
		}

		f.ServeHTTP(c.Writer(), c.Request())
	}
}

// WrapH is an adapter for wrapping http.Handler and returns a HandlerFunc function.
// The route parameters are being accessed by the wrapped handler through the context.
func WrapH[T Context](h http.Handler) HandlerFunc[T] {
	return func(c T) {
		if len(c.Params()) > 0 {
			ctx := netcontext.WithValue(c.Request().Context(), paramsKey, c.Params().Clone())
			h.ServeHTTP(c.Writer(), c.Request().WithContext(ctx))
			return
		}

		h.ServeHTTP(c.Writer(), c.Request())
	}
}
