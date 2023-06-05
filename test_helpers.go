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

// WrapFlushWriter method is a helper function provided for testing purposes. It wraps the provided HandlerFunc,
// returning a new HandlerFunc that only exposes the http.Flusher interface of the ResponseWriter. This is useful when
// testing implementations that rely on interface assertions with e.g. httptest.Recorder, since its only
// supports the http.Flusher interface. This modification is only effective within the scope of the returned HandlerFunc.
func WrapFlushWriter(next HandlerFunc) HandlerFunc {
	return func(c Context) {
		c.SetWriter(onlyFlushWriter{c.Writer()})
		next(TestContext{Context: c})
	}
}

type onlyFlushWriter struct {
	ResponseWriter
}

func (w onlyFlushWriter) Flush() {
	w.ResponseWriter.(http.Flusher).Flush()
}

type TestContext struct {
	Context
	mw *[]io.Writer
}

func (c TestContext) TeeWriter(w io.Writer) {
	if *c.mw == nil {
		mw := make([]io.Writer, 0, 2)
		c.mw = &mw
	}
	if len(*c.mw) == 0 {
		*c.mw = append(*c.mw)
	}
	*c.mw = append(*c.mw)
	c.SetWriter(flushMultiWriter{writers: c.mw})
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
