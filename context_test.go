// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

package fox

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	f, _ := NewRouter()
	c := newTestContext(f)
	c.req = req
	values := c.QueryParams()
	assert.Equal(t, wantValues, values)
	assert.Equal(t, wantValues, c.cachedQueries)
}

func TestContext_QueryParam(t *testing.T) {
	t.Parallel()
	wantValues := url.Values{
		"a": []string{"b"},
		"c": []string{"d", "e"},
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	req.URL.RawQuery = wantValues.Encode()

	f, _ := NewRouter()
	c := newTestContext(f)
	c.req = req
	assert.Equal(t, "b", c.QueryParam("a"))
	assert.Equal(t, "d", c.QueryParam("c"))
	assert.Equal(t, wantValues, c.cachedQueries)
}

func TestContext_Route(t *testing.T) {
	t.Parallel()
	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/foo", func(c *Context) {
		require.NotNil(t, c.Route())
		_, _ = io.WriteString(c.Writer(), c.Route().Pattern())
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	f.ServeHTTP(w, r)
	assert.Equal(t, "/foo", w.Body.String())
}

func TestContext_RoutingPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		target string
		want   string
	}{
		{
			name:   "ascii canonical",
			target: "https://example.com/foo/bar",
			want:   "/foo/bar",
		},
		{
			name:   "encoded space normalizes encoding",
			target: "https://example.com/foo%20bar",
			want:   "/foo%20bar",
		},
		{
			name:   "encoded sub-delim preserved",
			target: "https://example.com/foo%21bar",
			want:   "/foo%21bar",
		},
		{
			name:   "encoded non-ascii preserved",
			target: "https://example.com/foo%C3%A9bar",
			want:   "/foo%C3%A9bar",
		},
		{
			name:   "lowercase hex normalized to uppercase",
			target: "https://example.com/foo%2fbar",
			want:   "/foo%2Fbar",
		},
		{
			name:   "encoded slash preserved as raw path",
			target: "https://example.com/foo/bar%2Fbaz",
			want:   "/foo/bar%2Fbaz",
		},
		{
			name:   "encoded unreserved letter decoded",
			target: "https://example.com/foo/b%61r",
			want:   "/foo/bar",
		},
		{
			name:   "encoded unreserved uppercase decoded",
			target: "https://example.com/foo/%42AR",
			want:   "/foo/BAR",
		},
		{
			name:   "encoded tilde decoded",
			target: "https://example.com/%7euser",
			want:   "/~user",
		},
		{
			name:   "encoded unreserved decoded next to preserved escape",
			target: "https://example.com/%61%2F%62",
			want:   "/a%2Fb",
		},
		{
			name:   "encoded dot decoded",
			target: "https://example.com/foo/%2E%2E/bar",
			want:   "/foo/../bar",
		},
		{
			name:   "raw plus and star preserved",
			target: "https://example.com/a+b/c*d",
			want:   "/a+b/c*d",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter()
			f.MustAdd(MethodGet, "/*{any}", func(c *Context) {
				_, _ = io.WriteString(c.Writer(), c.RoutingPath())
			})

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, tc.target, nil)
			f.ServeHTTP(w, r)
			assert.Equal(t, tc.want, w.Body.String())
		})
	}
}

func TestContext_Host(t *testing.T) {
	t.Parallel()
	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/{a}", func(c *Context) {
		_, _ = io.WriteString(c.Writer(), c.Host())
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	f.ServeHTTP(w, r)
	assert.Equal(t, "example.com", w.Body.String())
}

func TestContext_Annotations(t *testing.T) {
	t.Parallel()
	f, _ := NewRouter()
	f.MustAdd(
		MethodGet,
		"/foo",
		emptyHandler,
		WithAnnotation("foo", "bar"),
		WithAnnotation("john", 1),
	)
	rte := f.Route(MethodGet, "/foo")
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

	f, _ := NewRouter()
	c := newTextContextOnly(f, httptest.NewRecorder(), req)
	c.route = &Route{params: []string{"a"}}
	*c.params = []string{"a"}

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
}

func TestContext_CloneWith(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	f, _ := NewRouter()
	c := newTextContextOnly(f, w, req)
	c.route = &Route{params: []string{"a"}}
	*c.params = []string{"a"}

	cp := c.CloneWith(c.Writer(), c.Request())
	assert.Equal(t, slices.Collect(c.Params()), slices.Collect(cp.Params()))
	assert.Equal(t, c.Request(), cp.Request())
	assert.Equal(t, c.Writer(), cp.Writer())
	assert.Equal(t, c.Pattern(), cp.Pattern())
	assert.Equal(t, c.Router(), cp.Router())
	assert.Nil(t, cp.cachedQueries)
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

	r.RemoteAddr = "[::ffff:192.0.2.1]:8080"
	c = NewTestContextOnly(w, r)
	assert.Equal(t, netip.MustParseAddr("192.0.2.1"), c.RemoteIP())

	r.RemoteAddr = "[fe80::1%eth0]:8080"
	c = NewTestContextOnly(w, r)
	assert.Equal(t, netip.MustParseAddr("fe80::1%eth0"), c.RemoteIP())

	r.RemoteAddr = "invalid"
	c = NewTestContextOnly(w, r)
	assert.False(t, c.RemoteIP().IsValid())
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
	require.NoError(t, c.String(http.StatusCreated, s))
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

func TestContext_Router(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)

	f, _ := NewRouter()
	require.NoError(t, onlyError(f.Add(MethodGet, "/foo", func(c *Context) {
		assert.NotNil(t, c.Router())
	})))

	f.ServeHTTP(w, req)
}

func TestContext_Scope(t *testing.T) {
	t.Parallel()

	f, _ := NewRouter(
		WithTrailingSlash(RedirectSlash),
		WithMiddlewareFor(RedirectSlashHandler, func(next HandlerFunc) HandlerFunc {
			return func(c *Context) {
				assert.Equal(t, RedirectSlashHandler, c.Scope())
				next(c)
			}
		}),
		WithMergeSlashes(RedirectPath),
		WithCollapseDotSegments(RedirectPath),
		WithMiddlewareFor(RedirectPathHandler, func(next HandlerFunc) HandlerFunc {
			return func(c *Context) {
				assert.Equal(t, RedirectPathHandler, c.Scope())
				next(c)
			}
		}),
		WithNoRouteHandler(func(c *Context) {
			assert.Equal(t, NoRouteHandler, c.Scope())
		}),
		WithNoMethodHandler(func(c *Context) {
			assert.Equal(t, NoMethodHandler, c.Scope())
		}),
		WithOptionsHandler(func(c *Context) {
			assert.Equal(t, OptionsHandler, c.Scope())
		}),
	)

	_, err := f.Add(MethodGet, "/foo", func(c *Context) {
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
			name: "redirect slash handler scope",
			req:  httptest.NewRequest(http.MethodGet, "/foo/", nil),
			w:    httptest.NewRecorder(),
		},
		{
			name: "redirect fixed path handler scope",
			req:  httptest.NewRequest(http.MethodGet, "//foo", nil),
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

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "https://example.com/{foo}", nil)
			f, c := NewTestContext(w, r)
			rte, err := f.NewRoute(MethodGet, "/{foo}", emptyHandler)
			require.NoError(t, err)
			c.route = rte

			params := make(Params, 0)
			if tc.params != nil {
				params = tc.params.clone()
				c.route.params = make([]string, 0, len(params))
				*c.params = make([]string, 0, len(params))
				for _, ps := range params {
					c.route.params = append(c.route.params, ps.Key)
					*c.params = append(*c.params, ps.Value)
				}
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

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "https://example.com/{foo}", nil)
			f, c := NewTestContext(w, r)
			rte, err := f.NewRoute(MethodGet, "/{foo}", emptyHandler)
			require.NoError(t, err)
			c.route = rte

			params := make(Params, 0)
			if tc.params != nil {
				params = tc.params.clone()
				c.route.params = make([]string, 0, len(params))
				*c.params = make([]string, 0, len(params))
				for _, ps := range params {
					c.route.params = append(c.route.params, ps.Key)
					*c.params = append(*c.params, ps.Value)
				}
			}

			WrapH(tc.handler(params))(c)

			assert.Equal(t, "fox", w.Body.String())
		})
	}
}

func TestWrapM(t *testing.T) {
	type writer struct {
		http.ResponseWriter
	}

	mw1 := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r)
		})
	}

	mw2 := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(&writer{w}, r)
		})
	}

	f, _ := NewRouter(WithMiddleware(WrapM(mw1), WrapM(mw2)))
	f.MustAdd(MethodGet, "/foo/{bar}", func(c *Context) {
		w := c.Writer()
		inner := w.(interface{ Unwrap() http.ResponseWriter }).Unwrap()
		assert.IsType(t, &recorder{}, w)
		assert.IsType(t, &writer{}, inner)

		require.NoError(t, c.String(http.StatusOK, "OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)

	assert.Equal(t, "OK", w.Body.String())
}

func TestServeHTTP_SetsRequestPattern(t *testing.T) {
	var mwPattern string
	mw := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mwPattern = r.Pattern
			h.ServeHTTP(w, r)
		})
	}

	f := MustRouter(WithMiddleware(WrapM(mw)))
	f.MustAdd(MethodGet, "/foo/{bar}", func(c *Context) {
		assert.Equal(t, "/foo/{bar}", c.Request().Pattern)
		require.NoError(t, c.String(http.StatusOK, "OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()

	f.ServeHTTP(w, req)

	assert.Equal(t, "/foo/{bar}", mwPattern)
	assert.Equal(t, "/foo/{bar}", req.Pattern)

	req = httptest.NewRequest(http.MethodGet, "/no/match", nil)
	req.Pattern = "/outer/"
	f.ServeHTTP(httptest.NewRecorder(), req)
	assert.Empty(t, req.Pattern)
}

func TestWrapM_FlusherShim(t *testing.T) {
	var sawFlusher bool
	var flushed bool

	mw := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flusher, ok := w.(http.Flusher)
			sawFlusher = ok
			h.ServeHTTP(w, r)
			if ok {
				flusher.Flush()
				flushed = true
			}
		})
	}

	f := MustRouter(WithMiddleware(WrapM(mw)))
	f.MustAdd(MethodGet, "/", func(c *Context) {
		require.NoError(t, c.String(http.StatusOK, "ok"))
	})

	w := httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.True(t, sawFlusher)
	assert.True(t, flushed)
	assert.True(t, w.Flushed)
}

func TestWrapM_UnwrapOnlyMiddleware(t *testing.T) {
	mw := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(unwrapWriter{w}, r)
		})
	}

	f := MustRouter(WithMiddleware(WrapM(mw)))
	f.MustAdd(MethodGet, "/", func(c *Context) {
		require.NoError(t, c.Writer().FlushError())
	})
	f.MustAdd(MethodGet, "/std", WrapH(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, http.NewResponseController(w).Flush())
	})))

	for _, path := range []string{"/", "/std"} {
		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		assert.True(t, w.Flushed)
	}
}

func BenchmarkWrapH(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/a/b/c", nil)
	w := httptest.NewRecorder()

	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/{a}/{b}/{c}", WrapH(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		f.ServeHTTP(w, req)
	}
}

func BenchmarkWrapF(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/a/b/c", nil)
	w := httptest.NewRecorder()

	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/{a}/{b}/{c}", WrapF(func(w http.ResponseWriter, r *http.Request) {}))

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		f.ServeHTTP(w, req)
	}
}

func BenchmarkWrapM(b *testing.B) {

	m := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r)
		})
	}

	f := MustRouter(WithMiddleware(WrapM(m)))
	f.MustAdd(MethodGet, "/{a}/{b}/{c}", emptyHandler)

	req := httptest.NewRequest(http.MethodGet, "/a/b/c", nil)
	w := httptest.NewRecorder()

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		f.ServeHTTP(w, req)
	}
}
