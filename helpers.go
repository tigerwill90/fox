// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"net/http"
)

// NewTestContext returns a new Router and its associated Context, designed only for testing purpose.
func NewTestContext[T Context](fn ContextBuilderFunc[T], w http.ResponseWriter, r *http.Request) (*Router[T], T) {
	fox := NewWithContext(fn)
	c := NewTestContextOnly(fox, w, r)
	return fox, c
}

// NewTestContextOnly returns a new Context associated with the provided Router, designed only for testing purpose.
func NewTestContextOnly[T Context](fox *Router[T], w http.ResponseWriter, r *http.Request) T {
	return newTextContextOnly(fox, w, r)
}

func newTextContextOnly[T Context](fox *Router[T], w http.ResponseWriter, r *http.Request) T {
	c := fox.Tree().builder.Get()
	c.Reset(w, r)

	return c
}

func newTestContextTree[T Context](t *Tree[T]) T {
	c := t.builder.Get()
	return c
}
