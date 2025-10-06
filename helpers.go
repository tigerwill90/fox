// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"net/http"
	"testing"
)

type TestContext struct {
	*cTx
}

// SetRoute affect the provided route to this context. It also set the [RouteHandler] scope.
func (c *TestContext) SetRoute(route *Route) {
	if route != nil {
		c.route = route
		c.scope = RouteHandler
	}
}

// SetParams affect the provided params to this context.
func (c *TestContext) SetParams(params Params) {
	if params != nil && c.route != nil {
		c.route.params = make([]string, 0, len(params))
		*c.params = make([]string, 0, len(params))
		for _, ps := range params {
			c.route.params = append(c.route.params, ps.Key)
			*c.params = append(*c.params, ps.Value)
		}
	}
}

// SetScope affect the provided scope to this context.
func (c *TestContext) SetScope(scope HandlerScope) {
	c.scope = scope
}

// NewTestContext returns a new [Router] and its associated [Context], designed only for testing purpose.
func NewTestContext(w http.ResponseWriter, r *http.Request, opts ...GlobalOption) (*Router, *TestContext) {
	f, err := New(opts...)
	if err != nil {
		panic(err)
	}
	tc := &TestContext{newTextContextOnly(f, w, r)}
	return f, tc
}

// NewTestContextOnly returns a new [Context] designed only for testing purpose.
func NewTestContextOnly(w http.ResponseWriter, r *http.Request, opts ...GlobalOption) *TestContext {
	f, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return &TestContext{newTextContextOnly(f, w, r)}
}

func newTextContextOnly(fox *Router, w http.ResponseWriter, r *http.Request) *cTx {
	tree := fox.getTree()
	c := tree.allocateContext()
	c.resetNil()
	c.reset(w, r)
	return c
}

func newTestContext(fox *Router) *cTx {
	tree := fox.getTree()
	c := tree.allocateContext()
	c.resetNil()
	return c
}

func newResponseWriter(w http.ResponseWriter) ResponseWriter {
	return &recorder{
		ResponseWriter: w,
		size:           notWritten,
		status:         http.StatusOK,
	}
}

// TODO move me in test file
func unwrapContext(t *testing.T, c Context) *cTx {
	t.Helper()
	cc, ok := c.(*cTx)
	if !ok {
		t.Fatal("unable to unwrap context")
	}
	return cc
}
