// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
)

func TestContext_Writer_ReadFrom(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	w := httptest.NewRecorder()

	c := NewTestContextOnly(w, req)

	n, err := c.Writer().ReadFrom(bytes.NewBuffer([]byte("foo bar")))
	require.NoError(t, err)
	assert.Equal(t, int(n), c.Writer().Size())
	assert.True(t, c.Writer().Written())
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int(n), w.Body.Len())
}

func TestContext_SetWriter(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	w := httptest.NewRecorder()

	c := NewTestContextOnly(w, req)

	newRec := new(recorder)
	c.SetWriter(newRec)
	assert.Equal(t, newRec, c.Writer())
}

func TestContext_SetRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	w := httptest.NewRecorder()

	c := NewTestContextOnly(w, req)

	newReq := new(http.Request)
	c.SetRequest(newReq)
	assert.Equal(t, newReq, c.Request())
}

func TestContext_QueryParams(t *testing.T) {
	t.Parallel()
	wantValues := url.Values{
		"a": []string{"b"},
		"c": []string{"d", "e"},
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	req.URL.RawQuery = wantValues.Encode()

	f, _ := New()
	c := newTestContext(f)
	c.req = req
	values := c.QueryParams()
	assert.Equal(t, wantValues, values)
	assert.Equal(t, wantValues, c.cachedQuery)
}

func TestContext_QueryParam(t *testing.T) {
	t.Parallel()
	wantValues := url.Values{
		"a": []string{"b"},
		"c": []string{"d", "e"},
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	req.URL.RawQuery = wantValues.Encode()

	f, _ := New()
	c := newTestContext(f)
	c.req = req
	assert.Equal(t, "b", c.QueryParam("a"))
	assert.Equal(t, "d", c.QueryParam("c"))
	assert.Equal(t, wantValues, c.cachedQuery)
}

func TestContext_Route(t *testing.T) {
	t.Parallel()
	f, _ := New()
	f.MustHandle(http.MethodGet, "/foo", func(c Context) {
		require.NotNil(t, c.Route())
		_, _ = io.WriteString(c.Writer(), c.Route().Pattern())
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	f.ServeHTTP(w, r)
	assert.Equal(t, "/foo", w.Body.String())
}

func TestContext_Path(t *testing.T) {
	t.Parallel()
	f, _ := New()
	f.MustHandle(http.MethodGet, "/{a}", func(c Context) {
		_, _ = io.WriteString(c.Writer(), c.Path())
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	f.ServeHTTP(w, r)
	assert.Equal(t, "/foo", w.Body.String())
}

func TestContext_Host(t *testing.T) {
	t.Parallel()
	f, _ := New()
	f.MustHandle(http.MethodGet, "/{a}", func(c Context) {
		_, _ = io.WriteString(c.Writer(), c.Host())
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	f.ServeHTTP(w, r)
	assert.Equal(t, "example.com", w.Body.String())
}

func TestContext_Annotations(t *testing.T) {
	t.Parallel()
	f, _ := New()
	f.MustHandle(
		http.MethodGet,
		"/foo",
		emptyHandler,
		WithAnnotation("foo", "bar"),
		WithAnnotation("john", 1),
	)
	rte := f.Route(http.MethodGet, "/foo")
	require.NotNil(t, rte)
	assert.Equal(t, "bar", rte.Annotation("foo").(string))
	assert.Equal(t, 1, rte.Annotation("john").(int))
}

func TestContext_Clone(t *testing.T) {
	t.Parallel()
	wantValues := url.Values{
		"a": []string{"b"},
		"c": []string{"d", "e"},
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	req.URL.RawQuery = wantValues.Encode()

	f, _ := New()
	c := newTextContextOnly(f, httptest.NewRecorder(), req)
	*c.params = Params{{Key: "foo", Value: "bar"}}

	buf := []byte("foo bar")
	_, err := c.w.Write(buf)
	require.NoError(t, err)

	cc := c.Clone()
	assert.Equal(t, http.StatusOK, cc.Writer().Status())
	assert.Equal(t, slices.Collect(c.Params()), slices.Collect(cc.Params()))
	assert.Equal(t, len(buf), cc.Writer().Size())
	assert.Equal(t, wantValues, c.QueryParams())
	assert.Panics(t, func() {
		_, _ = cc.Writer().Write([]byte("invalid"))
	})

	c.tsr = true
	*c.tsrParams = Params{{Key: "john", Value: "doe"}}
	cc = c.Clone()
	assert.Equal(t, slices.Collect(c.Params()), slices.Collect(cc.Params()))
}

func TestContext_CloneWith(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	f, _ := New()
	c := newTextContextOnly(f, w, req)
	*c.params = Params{{Key: "foo", Value: "bar"}}

	cp := c.CloneWith(c.Writer(), c.Request())
	cc := unwrapContext(t, cp)
	assert.Equal(t, slices.Collect(c.Params()), slices.Collect(cp.Params()))
	assert.Equal(t, c.Request(), cp.Request())
	assert.Equal(t, c.Writer(), cp.Writer())
	assert.Equal(t, c.Pattern(), cp.Pattern())
	assert.Equal(t, c.Fox(), cp.Fox())
	assert.Nil(t, cc.cachedQuery)

	c.tsr = true
	*c.tsrParams = Params{{Key: "john", Value: "doe"}}
	cp = c.CloneWith(c.Writer(), c.Request())
	assert.Equal(t, slices.Collect(c.Params()), slices.Collect(cp.Params()))
}

func TestContext_Redirect(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	c := NewTestContextOnly(w, r)
	require.NoError(t, c.Redirect(http.StatusTemporaryRedirect, "https://example.com/foo/bar"))
	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.Equal(t, "https://example.com/foo/bar", w.Header().Get(HeaderLocation))
}

func TestContext_Blob(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	c := NewTestContextOnly(w, r)
	buf := []byte("foobar")
	require.NoError(t, c.Blob(http.StatusCreated, MIMETextPlain, buf))
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, http.StatusCreated, c.Writer().Status())
	assert.Equal(t, MIMETextPlain, w.Header().Get(HeaderContentType))
	assert.Equal(t, buf, w.Body.Bytes())
	assert.Equal(t, len(buf), c.Writer().Size())
	assert.True(t, c.Writer().Written())
}

func TestContext_RemoteIP(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	r.RemoteAddr = "192.0.2.1:8080"
	c := NewTestContextOnly(w, r)
	assert.Equal(t, "192.0.2.1", c.RemoteIP().String())

	r.RemoteAddr = "[::1]:80"
	c = NewTestContextOnly(w, r)
	assert.Equal(t, "::1", c.RemoteIP().String())
}

func TestContext_ClientIP(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	r.RemoteAddr = "192.0.2.1:8080"
	c := NewTestContextOnly(w, r)
	_, err := c.ClientIP()
	assert.ErrorIs(t, err, ErrNoClientIPResolver)
}

func TestContext_Stream(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	c := NewTestContextOnly(w, r)
	buf := []byte("foobar")
	require.NoError(t, c.Stream(http.StatusCreated, MIMETextPlain, bytes.NewBuffer(buf)))
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, http.StatusCreated, c.Writer().Status())
	assert.Equal(t, MIMETextPlain, w.Header().Get(HeaderContentType))
	assert.Equal(t, buf, w.Body.Bytes())
	assert.Equal(t, len(buf), c.Writer().Size())
	assert.True(t, c.Writer().Written())
}

func TestContext_String(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	c := NewTestContextOnly(w, r)
	s := "foobar"
	require.NoError(t, c.String(http.StatusCreated, "%s", s))
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, http.StatusCreated, c.Writer().Status())
	assert.Equal(t, MIMETextPlainCharsetUTF8, w.Header().Get(HeaderContentType))
	assert.Equal(t, s, w.Body.String())
	assert.Equal(t, len(s), c.Writer().Size())
	assert.True(t, c.Writer().Written())
}

func TestContext_Writer(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	c := NewTestContextOnly(w, r)
	buf := []byte("foobar")
	c.Writer().WriteHeader(http.StatusCreated)
	assert.Equal(t, 0, c.Writer().Size())
	n, err := c.Writer().Write(buf)
	require.NoError(t, err)
	assert.Equal(t, len(buf), n)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, http.StatusCreated, c.Writer().Status())
	assert.Equal(t, buf, w.Body.Bytes())
	assert.Equal(t, len(buf), c.Writer().Size())
	assert.Equal(t, w, c.Writer().(interface{ Unwrap() http.ResponseWriter }).Unwrap())
	assert.True(t, c.Writer().Written())
}

func TestContext_Header(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	fox, c := NewTestContext(w, r)
	c.SetHeader(HeaderServer, "go")
	c.AddHeader("foo", "bar")
	fox.ServeHTTP(w, r)
	assert.Equal(t, "go", w.Header().Get(HeaderServer))
	assert.Equal(t, "bar", w.Header().Get("foo"))
}

func TestContext_GetHeader(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	r.Header.Set(HeaderAccept, MIMEApplicationJSON)
	c := NewTestContextOnly(w, r)
	assert.Equal(t, MIMEApplicationJSON, c.Header(HeaderAccept))
}

func TestContext_Fox(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)

	f, _ := New()
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/foo", func(c Context) {
		assert.NotNil(t, c.Fox())
	})))

	f.ServeHTTP(w, req)
}

func TestContext_Scope(t *testing.T) {
	t.Parallel()

	f, _ := New(
		WithRedirectTrailingSlash(true),
		WithMiddlewareFor(RedirectHandler, func(next HandlerFunc) HandlerFunc {
			return func(c Context) {
				assert.Equal(t, RedirectHandler, c.Scope())
				next(c)
			}
		}),
		WithNoRouteHandler(func(c Context) {
			assert.Equal(t, NoRouteHandler, c.Scope())
		}),
		WithNoMethodHandler(func(c Context) {
			assert.Equal(t, NoMethodHandler, c.Scope())
		}),
		WithOptionsHandler(func(c Context) {
			assert.Equal(t, OptionsHandler, c.Scope())
		}),
	)

	_, err := f.Handle(http.MethodGet, "/foo", func(c Context) {
		assert.Equal(t, RouteHandler, c.Scope())
	})
	require.NoError(t, err)

	cases := []struct {
		name string
		req  *http.Request
		w    http.ResponseWriter
	}{
		{
			name: "route handler scope",
			req:  httptest.NewRequest(http.MethodGet, "/foo", nil),
			w:    httptest.NewRecorder(),
		},
		{
			name: "redirect handler scope",
			req:  httptest.NewRequest(http.MethodGet, "/foo/", nil),
			w:    httptest.NewRecorder(),
		},
		{
			name: "no method handler scope",
			req:  httptest.NewRequest(http.MethodPost, "/foo", nil),
			w:    httptest.NewRecorder(),
		},
		{
			name: "options handler scope",
			req:  httptest.NewRequest(http.MethodOptions, "/foo", nil),
			w:    httptest.NewRecorder(),
		},
		{
			name: "options handler scope",
			req:  httptest.NewRequest(http.MethodOptions, "/foo", nil),
			w:    httptest.NewRecorder(),
		},
		{
			name: "no route handler scope",
			req:  httptest.NewRequest(http.MethodOptions, "/bar", nil),
			w:    httptest.NewRecorder(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.ServeHTTP(tc.w, tc.req)
		})
	}
}

func TestWrapF(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		handler func(p Params) http.HandlerFunc
		params  *Params
	}{
		{
			name: "wrap handlerFunc without context params",
			handler: func(expectedParams Params) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("fox"))
				}
			},
		},
		{
			name: "wrap handlerFunc with context params",
			handler: func(expectedParams Params) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("fox"))

					p := ParamsFromContext(r.Context())

					assert.Equal(t, expectedParams, p)
				}
			},
			params: &Params{
				{
					Key:   "foo",
					Value: "bar",
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "https://example.com/{foo}", nil)
			f, c := NewTestContext(w, r)
			rte, err := f.NewRoute("/{foo}", emptyHandler)
			require.NoError(t, err)
			c.SetRoute(rte)

			params := make(Params, 0)
			if tc.params != nil {
				params = tc.params.clone()
				c.SetParams(params)
			}

			WrapF(tc.handler(params))(c)

			assert.Equal(t, "fox", w.Body.String())
		})
	}

}

func TestWrapH(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		handler func(p Params) http.Handler
		params  *Params
	}{
		{
			name: "wrap handler without context params",
			handler: func(expectedParams Params) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("fox"))
				})
			},
		},
		{
			name: "wrap handler with context params",
			handler: func(expectedParams Params) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("fox"))

					p := ParamsFromContext(r.Context())

					assert.Equal(t, expectedParams, p)
				})
			},
			params: &Params{
				{
					Key:   "foo",
					Value: "bar",
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "https://example.com/{foo}", nil)
			f, c := NewTestContext(w, r)
			rte, err := f.NewRoute("/{foo}", emptyHandler)
			require.NoError(t, err)
			c.SetRoute(rte)

			params := make(Params, 0)
			if tc.params != nil {
				params = tc.params.clone()
				c.SetParams(params)
			}

			WrapH(tc.handler(params))(c)

			assert.Equal(t, "fox", w.Body.String())
		})
	}
}

func BenchmarkWrapF(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/a/b/c", nil)
	w := httptest.NewRecorder()

	f, _ := New()
	f.MustHandle(http.MethodGet, "/{a}/{b}/{c}", WrapF(func(w http.ResponseWriter, r *http.Request) {

	}))

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		f.ServeHTTP(w, req)
	}

}
