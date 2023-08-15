// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"net/http"
)

// NewTestContext returns a new Router and its associated Context, designed only for testing purpose.
func NewTestContext(w http.ResponseWriter, r *http.Request) (*Router, Context) {
	fox := New()
	c := NewTestContextOnly(fox, w, r)
	return fox, c
}

// NewTestContextOnly returns a new Context associated with the provided Router, designed only for testing purpose.
func NewTestContextOnly(fox *Router, w http.ResponseWriter, r *http.Request) Context {
	return newTextContextOnly(fox, w, r)
}

func newTextContextOnly(fox *Router, w http.ResponseWriter, r *http.Request) *context {
	c := fox.Tree().allocateContext()
	c.resetNil()
	c.fox = fox
	c.req = r
	c.rec.reset(w)
	c.w = flushWriter{&c.rec}
	return c
}

func newTestContextTree(t *Tree) *context {
	c := t.allocateContext()
	c.resetNil()
	return c
}
