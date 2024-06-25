// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// ContextCloser extends Context for manually created instances, adding a Close method
// to release resources after use.
type ContextCloser interface {
	Context
	Close()
}

// Context represents the context of the current HTTP request. It provides methods to access request data and
// to write a response. Be aware that the Context API is not thread-safe and its lifetime should be limited to the
// duration of the HandlerFunc execution, as the underlying implementation may be reused a soon as the handler return.
// (see Clone method).
type Context interface {
	// Request returns the current *http.Request.
	Request() *http.Request
	// SetRequest sets the *http.Request.
	SetRequest(r *http.Request)
	// Writer method returns a custom ResponseWriter implementation.
	Writer() ResponseWriter
	// SetWriter sets the ResponseWriter.
	SetWriter(w ResponseWriter)
	// RemoteIP parses the IP from Request.RemoteAddr, normalizes and returns a net.IP.
	RemoteIP() net.IP
	// Path returns the registered path for the handler.
	Path() string
	// Params returns a Params slice containing the matched
	// wildcard parameters.
	Params() Params
	// Param retrieve a matching wildcard parameter by name.
	Param(name string) string
	// QueryParams parses the Request RawQuery and returns the corresponding values.
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
	// Stream sends data from an io.Reader with the specified status code and content type.
	Stream(code int, contentType string, r io.Reader) error
	// Redirect sends an HTTP redirect response with the given status code and URL.
	Redirect(code int, url string) error
	// Clone returns a copy of the Context that is safe to use after the HandlerFunc returns.
	Clone() Context
	// CloneWith returns a copy of the current Context, substituting its ResponseWriter and
	// http.Request with the provided ones. The method is designed for zero allocation during the
	// copy process. The returned ContextCloser must be closed once no longer needed.
	// This functionality is particularly beneficial for middlewares that need to wrap
	// their custom ResponseWriter while preserving the state of the original Context.
	CloneWith(w ResponseWriter, r *http.Request) ContextCloser
	// Tree is a local copy of the Tree in use to serve the request.
	Tree() *Tree
	// Fox returns the Router instance.
	Fox() *Router
	// Reset resets the Context to its initial state, attaching the provided ResponseWriter and http.Request.
	Reset(w ResponseWriter, r *http.Request)
}

// cTx holds request-related information and allows interaction with the ResponseWriter.
type cTx struct {
	w       ResponseWriter
	req     *http.Request
	params  *Params
	skipNds *skippedNodes

	// tree at allocation (read-only, no reset)
	tree *Tree
	fox  *Router

	cachedQuery url.Values
	path        string
	rec         recorder
}

// Reset resets the Context to its initial state, attaching the provided ResponseWriter and http.Request.
func (c *cTx) Reset(w ResponseWriter, r *http.Request) {
	c.req = r
	c.w = w
	c.path = ""
	c.cachedQuery = nil
	*c.params = (*c.params)[:0]
}

// reset resets the Context to its initial state, attaching the provided http.ResponseWriter and http.Request.
// Caution: You should always pass the original http.ResponseWriter to this method, not the ResponseWriter itself, to
// avoid wrapping the ResponseWriter within itself. Use wisely!
func (c *cTx) reset(w http.ResponseWriter, r *http.Request) {
	c.rec.reset(w)
	c.req = r
	c.w = &c.rec
	c.path = ""
	c.cachedQuery = nil
	*c.params = (*c.params)[:0]
}

func (c *cTx) resetNil() {
	c.req = nil
	c.w = nil
	c.path = ""
	c.cachedQuery = nil
	*c.params = (*c.params)[:0]
}

// Request returns the *http.Request.
func (c *cTx) Request() *http.Request {
	return c.req
}

// SetRequest sets the *http.Request.
func (c *cTx) SetRequest(r *http.Request) {
	c.req = r
}

// Writer returns the ResponseWriter.
func (c *cTx) Writer() ResponseWriter {
	return c.w
}

// SetWriter sets the ResponseWriter.
func (c *cTx) SetWriter(w ResponseWriter) {
	c.w = w
}

// RemoteIP parses the IP from Request.RemoteAddr, normalizes and returns a net.IP.
func (c *cTx) RemoteIP() net.IP {
	ip, _, err := net.SplitHostPort(strings.TrimSpace(c.req.RemoteAddr))
	if err != nil {
		return nil
	}
	return net.ParseIP(ip)
}

// Params returns a Params slice containing the matched
// wildcard parameters.
func (c *cTx) Params() Params {
	return *c.params
}

// Param retrieve a matching wildcard segment by name.
// It's a helper for c.Params.Get(name).
func (c *cTx) Param(name string) string {
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
func (c *cTx) QueryParams() url.Values {
	return c.getQueries()
}

// QueryParam returns the first value associated with the given key.
// It's a helper for c.QueryParams().Get(name).
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

// Path returns the registered path for the handler.
func (c *cTx) Path() string {
	return c.path
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

// Stream sends data from an io.Reader with the specified status code and content type.
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

// Tree is a local copy of the Tree in use to serve the request.
func (c *cTx) Tree() *Tree {
	return c.tree
}

// Fox returns the Router instance.
func (c *cTx) Fox() *Router {
	return c.fox
}

// Clone returns a copy of the Context that is safe to use after the HandlerFunc returns.
// Any attempt to write on the ResponseWriter will panic with the error ErrDiscardedResponseWriter.
func (c *cTx) Clone() Context {
	cp := cTx{
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
func (c *cTx) CloneWith(w ResponseWriter, r *http.Request) ContextCloser {
	cp := c.tree.ctx.Get().(*cTx)
	cp.req = r
	cp.w = w
	cp.path = c.path
	cp.cachedQuery = nil
	if len(*c.params) > len(*cp.params) {
		// Grow cp.params to a least cap(c.params)
		*cp.params = slices.Grow(*cp.params, len(*c.params)-len(*cp.params))
	}
	// cap(cp.params) >= cap(c.params)
	// now constraint into len(c.params) & cap(c.params)
	*cp.params = (*cp.params)[:len(*c.params):cap(*c.params)]
	copy(*cp.params, *c.params)
	return cp
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
		if len(c.Params()) > 0 {
			ctx := context.WithValue(c.Request().Context(), paramsKey, c.Params().Clone())
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
		if len(c.Params()) > 0 {
			ctx := context.WithValue(c.Request().Context(), paramsKey, c.Params().Clone())
			h.ServeHTTP(c.Writer(), c.Request().WithContext(ctx))
			return
		}

		h.ServeHTTP(c.Writer(), c.Request())
	}
}
