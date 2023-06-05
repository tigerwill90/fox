package fox

import (
	"io"
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

type testContext struct {
	*context
}

func (c testContext) TeeWriter(w io.Writer) {
	if w != nil {
		if len(*c.mw) == 0 {
			*c.mw = append(*c.mw, c.w)
		}
		*c.mw = append(*c.mw, w)
		c.w = flushMultiWriter{c.mw}
	}
}

func newTextContextOnly(fox *Router, w http.ResponseWriter, r *http.Request) testContext {
	c := fox.Tree().allocateContext()
	c.resetNil()
	c.rec.reset(w)
	c.w = flushWriter{&c.rec}
	c.fox = fox
	c.req = r
	return testContext{c}
}

func newTestContextTree(t *Tree) testContext {
	c := t.allocateContext()
	c.resetNil()
	return testContext{c}
}
