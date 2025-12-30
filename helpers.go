// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"net/http"
)

// NewTestContext returns a new [Router] and its associated [Context], designed only for testing purpose.
func NewTestContext(w http.ResponseWriter, r *http.Request, opts ...GlobalOption) (*Router, *Context) {
	f, err := New(opts...)
	if err != nil {
		panic(err)
	}
	tc := newTextContextOnly(f, w, r)
	return f, tc
}

// NewTestContextOnly returns a new [Context] designed only for testing purpose.
func NewTestContextOnly(w http.ResponseWriter, r *http.Request, opts ...GlobalOption) *Context {
	f, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return newTextContextOnly(f, w, r)
}

func newTextContextOnly(fox *Router, w http.ResponseWriter, r *http.Request) *Context {
	tree := fox.getTree()
	c := tree.allocateContext()
	c.resetNil()
	c.reset(w, r)
	return c
}

func newTestContext(fox *Router) *Context {
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
