// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"net/http"
	"testing"
)

// NewTestContext returns a new [Router] and its associated [Context], designed only for testing purpose.
func NewTestContext(w http.ResponseWriter, r *http.Request, opts ...GlobalOption) (*Router, Context) {
	fox := New(opts...)
	c := newTextContextOnly(fox, w, r)
	return fox, c
}

// NewTestContextOnly returns a new [Context] designed only for testing purpose.
func NewTestContextOnly(w http.ResponseWriter, r *http.Request, opts ...GlobalOption) Context {
	fox := New(opts...)
	return newTextContextOnly(fox, w, r)
}

func newTextContextOnly(fox *Router, w http.ResponseWriter, r *http.Request) *cTx {
	tree := fox.getRoot()
	c := tree.allocateContext()
	c.resetNil()
	c.reset(w, r)
	return c
}

func newTestContext(fox *Router) *cTx {
	tree := fox.getRoot()
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

func unwrapContext(t *testing.T, c Context) *cTx {
	t.Helper()
	cc, ok := c.(*cTx)
	if !ok {
		t.Fatal("unable to unwrap context")
	}
	return cc
}
