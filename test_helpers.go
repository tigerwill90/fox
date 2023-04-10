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
	c.rec.reset(w)
	c.w = flushWriter{&c.rec}
	c.fox = fox
	c.req = r
	return c
}

func newTestContextTree(t *Tree) *context {
	c := t.allocateContext()
	c.resetNil()
	return c
}
