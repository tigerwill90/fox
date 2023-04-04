// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ContextCloser extends Context for manually created instances, adding a Close method
// to release resources after use.
type ContextCloser interface {
	Context
	Close()
}

// Context represents the context of the current HTTP request.
// It provides methods to access request data and to write a response.
type Context interface {
	// Done returns a channel that closes when the request's context is
	// cancelled or times out.
	Done() <-chan struct{}
	// Request returns the current *http.Request.
	Request() *http.Request
	// SetRequest sets the *http.Request.
	SetRequest(r *http.Request)
	// Writer returns the ResponseWriter.
	Writer() ResponseWriter
	// SetWriter sets the ResponseWriter.
	SetWriter(w ResponseWriter)
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
	// Tree is a local copy of the Tree in use to serve the request.
	Tree() *Tree
	// Fox returns the Router in use to serve the request.
	Fox() *Router
}

// context holds request-related information and allows interaction with the ResponseWriter.
type context struct {
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

func (c *context) reset(fox *Router, w http.ResponseWriter, r *http.Request) {
	c.rec.reset(w)
	c.req = r
	if r.ProtoMajor == 2 {
		c.w = h2Writer{&c.rec}
	} else {
		c.w = h1Writer{&c.rec}
	}
	c.fox = fox
	c.path = ""
	c.cachedQuery = nil
	*c.params = (*c.params)[:0]
}

func (c *context) resetNil() {
	c.req = nil
	c.w = nil
	c.fox = nil
	c.path = ""
	c.cachedQuery = nil
	*c.params = (*c.params)[:0]
}

// Request returns the *http.Request.
func (c *context) Request() *http.Request {
	return c.req
}

// SetRequest sets the *http.Request.
func (c *context) SetRequest(r *http.Request) {
	c.req = r
}

// Writer returns the ResponseWriter.
func (c *context) Writer() ResponseWriter {
	return c.w
}

// SetWriter sets the ResponseWriter.
func (c *context) SetWriter(w ResponseWriter) {
	c.w = w
}

// Done returns a channel that closes when the request's context is
// cancelled or times out.
func (c *context) Done() <-chan struct{} {
	return c.req.Context().Done()
}

// Params returns a Params slice containing the matched
// wildcard parameters.
func (c *context) Params() Params {
	return *c.params
}

// Param retrieve a matching wildcard segment by name.
// It's a helper for c.Params.Get(name).
func (c *context) Param(name string) string {
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
func (c *context) QueryParams() url.Values {
	c.req.URL.Query()
	return c.getQueries()
}

// QueryParam returns the first value associated with the given key.
// It's a helper for c.QueryParams().Get(name).
func (c *context) QueryParam(name string) string {
	return c.getQueries().Get(name)
}

// Path returns the registered path for the handler.
func (c *context) Path() string {
	return c.path
}

// String sends a formatted string with the specified status code.
func (c *context) String(code int, format string, values ...any) (err error) {
	c.w.Header().Set(HeaderContentType, MIMETextPlainCharsetUTF8)
	c.w.WriteHeader(code)
	_, err = fmt.Fprintf(c.w, format, values...)
	return
}

// Blob sends a byte slice with the specified status code and content type.
func (c *context) Blob(code int, contentType string, buf []byte) (err error) {
	c.w.Header().Set(HeaderContentType, contentType)
	c.w.WriteHeader(code)
	_, err = c.w.Write(buf)
	return
}

// Stream sends data from an io.Reader with the specified status code and content type.
func (c *context) Stream(code int, contentType string, r io.Reader) (err error) {
	c.w.Header().Set(HeaderContentType, contentType)
	c.w.WriteHeader(code)
	_, err = io.Copy(c.w, r)
	return
}

// Redirect sends an HTTP redirect response with the given status code and URL.
func (c *context) Redirect(code int, url string) error {
	if code < http.StatusMultipleChoices || code > http.StatusPermanentRedirect {
		return ErrInvalidRedirectCode
	}
	http.Redirect(c.w, c.req, url, code)
	return nil
}

// Tree is a local copy of the Tree in use to serve the request.
func (c *context) Tree() *Tree {
	return c.tree
}

// Fox returns the Router in use to serve the request.
func (c *context) Fox() *Router {
	return c.fox
}

// Clone returns a copy of the Context that is safe to use after the HandlerFunc returns.
func (c *context) Clone() Context {
	cp := context{
		rec:  c.rec,
		req:  c.req.Clone(c.req.Context()),
		fox:  c.fox,
		tree: c.tree,
	}
	cp.rec.ResponseWriter = noopWriter{}
	cp.w = &cp.rec
	params := make(Params, len(*c.params))
	copy(params, *c.params)
	cp.params = &params
	cp.cachedQuery = nil
	return &cp
}

// Close releases the context to be reused later.
func (c *context) Close() {
	// Put back the context, if not extended more than max params or max depth, allowing
	// the slice to naturally grow within the constraint.
	if cap(*c.params) > int(c.tree.maxParams.Load()) || cap(*c.skipNds) > int(c.tree.maxDepth.Load()) {
		return
	}
	c.tree.ctx.Put(c)
}

func (c *context) getQueries() url.Values {
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
func WrapF(f http.HandlerFunc) HandlerFunc {
	return func(c Context) {
		f.ServeHTTP(c.Writer(), c.Request())
	}
}

// WrapH is an adapter for wrapping http.Handler and returns a HandlerFunc function.
func WrapH(h http.Handler) HandlerFunc {
	return func(c Context) {
		h.ServeHTTP(c.Writer(), c.Request())
	}
}

// WrapM is an adapter for wrapping http.Handler middleware and returns a
// MiddlewareFunc function.
func WrapM(m func(handler http.Handler) http.Handler) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			adapter := m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next(c)
			}))
			adapter.ServeHTTP(c.Writer(), c.Request())
		}
	}
}
