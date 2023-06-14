// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"bytes"
	"compress/gzip"
	netcontext "context"
	"crypto/rand"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

func TestContext_QueryParams(t *testing.T) {
	t.Parallel()
	wantValues := url.Values{
		"a": []string{"b"},
		"c": []string{"d", "e"},
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	req.URL.RawQuery = wantValues.Encode()

	c := newTestContextTree(New().Tree())
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

	c := newTestContextTree(New().Tree())
	c.req = req
	assert.Equal(t, "b", c.QueryParam("a"))
	assert.Equal(t, "d", c.QueryParam("c"))
	assert.Equal(t, wantValues, c.cachedQuery)
}

func TestContext_Clone(t *testing.T) {
	t.Parallel()
	wantValues := url.Values{
		"a": []string{"b"},
		"c": []string{"d", "e"},
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	req.URL.RawQuery = wantValues.Encode()

	c := newTextContextOnly(New(), httptest.NewRecorder(), req)

	buf := []byte("foo bar")
	_, err := c.w.Write(buf)
	require.NoError(t, err)

	cc := c.Clone()
	assert.Equal(t, http.StatusOK, cc.Writer().Status())
	assert.Equal(t, len(buf), cc.Writer().Size())
	assert.Equal(t, wantValues, c.QueryParams())
	assert.Empty(t, *c.mw)
	_, err = cc.Writer().Write([]byte("invalid"))
	assert.ErrorIs(t, err, ErrDiscardedResponseWriter)
}

func TestContext_Ctx(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	ctx, cancel := netcontext.WithCancel(netcontext.Background())
	cancel()
	req = req.WithContext(ctx)
	_, c := NewTestContext(httptest.NewRecorder(), req)
	select {
	case <-c.Ctx().Done():
		require.ErrorIs(t, c.Request().Context().Err(), netcontext.Canceled)
	case <-time.After(1):
		t.FailNow()
	}
}

func TestContext_Redirect(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
	require.NoError(t, c.Redirect(http.StatusTemporaryRedirect, "https://example.com/foo/bar"))
	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.Equal(t, "https://example.com/foo/bar", w.Header().Get(HeaderLocation))
}

func TestContext_Blob(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
	buf := []byte("foobar")
	require.NoError(t, c.Blob(http.StatusCreated, MIMETextPlain, buf))
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, http.StatusCreated, c.Writer().Status())
	assert.Equal(t, MIMETextPlain, w.Header().Get(HeaderContentType))
	assert.Equal(t, buf, w.Body.Bytes())
	assert.Equal(t, len(buf), c.Writer().Size())
	assert.True(t, c.Writer().Written())
}

func TestContext_Stream(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
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
	_, c := NewTestContext(w, r)
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
	_, c := NewTestContext(w, r)
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
	assert.Equal(t, w, c.Writer().Unwrap())
	assert.True(t, c.Writer().Written())
}

func TestContext_Header(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	fox, c := NewTestContext(w, r)
	c.SetHeader(HeaderServer, "go")
	fox.ServeHTTP(w, r)
	assert.Equal(t, "go", w.Header().Get(HeaderServer))
}

func TestContext_GetHeader(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	r.Header.Set(HeaderAccept, MIMEApplicationJSON)
	_, c := NewTestContext(w, r)
	assert.Equal(t, MIMEApplicationJSON, c.Header(HeaderAccept))
}

func TestContext_Fox(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)

	f := New()
	require.NoError(t, f.Handle(http.MethodGet, "/foo", func(c Context) {
		assert.NotNil(t, c.Fox())
	}))

	f.ServeHTTP(w, req)
}

func TestContext_Tree(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)

	f := New()
	require.NoError(t, f.Handle(http.MethodGet, "/foo", func(c Context) {
		assert.NotNil(t, c.Tree())
	}))

	f.ServeHTTP(w, req)
}

func TestContext_TeeWriter_h1(t *testing.T) {
	t.Parallel()
	const length = 1 * 1024 * 1024
	buf := make([]byte, length)
	_, _ = rand.Read(buf)

	cases := []struct {
		name    string
		handler func(dumper *bytes.Buffer) HandlerFunc
	}{
		{
			name: "h1 writer",
			handler: func(dumper *bytes.Buffer) HandlerFunc {
				return func(c Context) {
					c.TeeWriter(dumper)
					n, err := c.Writer().Write(buf)
					require.NoError(t, err)
					assert.Equal(t, length, n)
					assert.Equal(t, length, c.Writer().Size())
					assert.True(t, c.Writer().Written())
				}
			},
		},
		{
			name: "h1 string writer",
			handler: func(dumper *bytes.Buffer) HandlerFunc {
				return func(c Context) {
					c.TeeWriter(dumper)
					n, err := io.WriteString(c.Writer(), string(buf))
					require.NoError(t, err)
					assert.Equal(t, length, n)
					assert.Equal(t, length, c.Writer().Size())
					assert.True(t, c.Writer().Written())
				}
			},
		},
		{
			name: "h1 reader from",
			handler: func(dumper *bytes.Buffer) HandlerFunc {
				return func(c Context) {
					c.TeeWriter(dumper)
					rf, ok := c.Writer().(io.ReaderFrom)
					require.True(t, ok)

					n, err := rf.ReadFrom(bytes.NewReader(buf))
					require.NoError(t, err)
					assert.Equal(t, length, int(n))
					assert.Equal(t, length, c.Writer().Size())
					assert.True(t, c.Writer().Written())
				}
			},
		},
		{
			name: "h1 flusher",
			handler: func(dumper *bytes.Buffer) HandlerFunc {
				return func(c Context) {
					c.TeeWriter(dumper)
					flusher, ok := c.Writer().(http.Flusher)
					require.True(t, ok)

					_, err := c.Writer().Write(buf[:1024])
					require.NoError(t, err)
					flusher.Flush()
					_, err = c.Writer().Write(buf[1024:])
					require.NoError(t, err)
					flusher.Flush()
					assert.Equal(t, length, c.Writer().Size())
					assert.True(t, c.Writer().Written())
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := New()
			dumper := bytes.NewBuffer(nil)
			require.NoError(t, f.Handle(http.MethodGet, "/foo", tc.handler(dumper)))

			srv := httptest.NewServer(f)
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/foo", nil)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			out, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, buf, out)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, buf, dumper.Bytes())
		})
	}
}

func TestContext_TeeWriter_flusher(t *testing.T) {
	t.Parallel()
	const length = 1 * 1024 * 1024
	buf := make([]byte, length)
	_, _ = rand.Read(buf)

	cases := []struct {
		name    string
		handler func(dumper *bytes.Buffer) HandlerFunc
	}{
		{
			name: "writer",
			handler: func(dumper *bytes.Buffer) HandlerFunc {
				return func(c Context) {
					c.TeeWriter(dumper)
					n, err := c.Writer().Write(buf)
					require.NoError(t, err)
					assert.Equal(t, length, n)
					assert.Equal(t, length, c.Writer().Size())
					assert.True(t, c.Writer().Written())
				}
			},
		},
		{
			name: "string writer",
			handler: func(dumper *bytes.Buffer) HandlerFunc {
				return func(c Context) {
					c.TeeWriter(dumper)
					n, err := io.WriteString(c.Writer(), string(buf))
					require.NoError(t, err)
					assert.Equal(t, length, n)
					assert.Equal(t, length, c.Writer().Size())
					assert.True(t, c.Writer().Written())
				}
			},
		},
		{
			name: "flusher",
			handler: func(dumper *bytes.Buffer) HandlerFunc {
				return func(c Context) {
					c.TeeWriter(dumper)
					flusher, ok := c.Writer().(http.Flusher)
					require.True(t, ok)

					_, err := c.Writer().Write(buf[:1024])
					require.NoError(t, err)
					flusher.Flush()
					_, err = c.Writer().Write(buf[1024:])
					require.NoError(t, err)
					flusher.Flush()
					assert.Equal(t, length, c.Writer().Size())
					assert.True(t, c.Writer().Written())
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := New()
			dumper := bytes.NewBuffer(nil)
			require.NoError(t, f.Handle(http.MethodGet, "/foo", WrapTestContext(tc.handler(dumper))))

			srv := httptest.NewServer(f)
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/foo", nil)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			out, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, buf, out)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, buf, dumper.Bytes())
		})
	}
}

func TestContext_TeeWriter_h2(t *testing.T) {
	t.Parallel()
	const length = 1 * 1024 * 1024
	buf := make([]byte, length)
	_, _ = rand.Read(buf)

	cases := []struct {
		name    string
		handler func(dumper *bytes.Buffer) HandlerFunc
	}{
		{
			name: "h2 writer",
			handler: func(dumper *bytes.Buffer) HandlerFunc {
				return func(c Context) {
					c.TeeWriter(dumper)
					n, err := c.Writer().Write(buf)
					require.NoError(t, err)
					assert.Equal(t, length, n)
					assert.Equal(t, length, c.Writer().Size())
					assert.True(t, c.Writer().Written())
				}
			},
		},
		{
			name: "h2 string writer",
			handler: func(dumper *bytes.Buffer) HandlerFunc {
				return func(c Context) {
					c.TeeWriter(dumper)
					n, err := io.WriteString(c.Writer(), string(buf))
					require.NoError(t, err)
					assert.Equal(t, length, n)
					assert.Equal(t, length, c.Writer().Size())
					assert.True(t, c.Writer().Written())
				}
			},
		},
		{
			name: "h2 flusher",
			handler: func(dumper *bytes.Buffer) HandlerFunc {
				return func(c Context) {
					c.TeeWriter(dumper)
					flusher, ok := c.Writer().(http.Flusher)
					require.True(t, ok)

					_, err := c.Writer().Write(buf[:1024])
					require.NoError(t, err)
					flusher.Flush()
					_, err = c.Writer().Write(buf[1024:])
					require.NoError(t, err)
					flusher.Flush()
					assert.Equal(t, length, c.Writer().Size())
					assert.True(t, c.Writer().Written())
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := New()
			dumper := bytes.NewBuffer(nil)
			require.NoError(t, f.Handle(http.MethodGet, "/foo", tc.handler(dumper)))

			srv := httptest.NewUnstartedServer(f)

			err := http2.ConfigureServer(srv.Config, new(http2.Server))
			require.NoError(t, err)

			srv.TLS = srv.Config.TLSConfig
			srv.StartTLS()
			defer srv.Close()

			tr := &http.Transport{TLSClientConfig: srv.Config.TLSConfig}
			require.NoError(t, http2.ConfigureTransport(tr))
			tr.TLSClientConfig.InsecureSkipVerify = true
			client := &http.Client{Transport: tr}

			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/foo", nil)
			resp, err := client.Do(req)
			require.NoError(t, err)
			out, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, buf, out)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, buf, dumper.Bytes())
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
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("fox"))
				})
			},
		},
		{
			name: "wrap handlerFunc with context params",
			handler: func(expectedParams Params) http.HandlerFunc {
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
			r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
			_, c := NewTestContext(w, r)

			params := make(Params, 0)
			if tc.params != nil {
				params = tc.params.Clone()
				c.(*context).params = &params
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
			r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
			_, c := NewTestContext(w, r)

			params := make(Params, 0)
			if tc.params != nil {
				params = tc.params.Clone()
				c.(*context).params = &params
			}

			WrapH(tc.handler(params))(c)

			assert.Equal(t, "fox", w.Body.String())
		})
	}
}

func TestWrapM(t *testing.T) {
	t.Parallel()

	type mockWriter struct {
		http.ResponseWriter
	}

	wantSize := func(size int) func(next HandlerFunc) HandlerFunc {
		return func(next HandlerFunc) HandlerFunc {
			return func(c Context) {
				next(c)
				assert.Equal(t, size, c.Writer().Size())
			}
		}
	}

	cases := []struct {
		name       string
		m          MiddlewareFunc
		wantStatus int
		wantBody   string
		wantSize   int
	}{
		{
			name: "using original writer",
			m: WrapM(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					req := r.Clone(r.Context())
					req.Header.Set("foo", "bar")
					next.ServeHTTP(mockWriter{w}, req)
				})
			}, true),
			wantStatus: http.StatusCreated,
			wantBody:   "foo bar",
			wantSize:   7,
		},
		{
			name: "using fox writer",
			m: WrapM(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					req := r.Clone(r.Context())
					req.Header.Set("foo", "bar")
					next.ServeHTTP(w, req)
				})
			}, false),
			wantStatus: http.StatusCreated,
			wantBody:   "foo bar",
			wantSize:   7,
		},
		{
			name: "using fox writer without calling next",
			m: WrapM(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					req := r.Clone(r.Context())
					req.Header.Set("foo", "bar")
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(http.StatusText(http.StatusUnauthorized)))
				})
			}, false),
			wantStatus: http.StatusUnauthorized,
			wantBody:   http.StatusText(http.StatusUnauthorized),
			wantSize:   12,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := New(WithMiddleware(wantSize(tc.wantSize), tc.m))
			require.NoError(t, f.Handle(http.MethodGet, "/foo", func(c Context) {
				assert.Equal(t, "bar", c.Header("foo"))
				_ = c.String(http.StatusCreated, "foo bar")
			}))

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/foo", nil)
			f.ServeHTTP(w, r)
			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantBody, w.Body.String())
		})
	}
}

// This example demonstrates how to capture the HTTP response body by using the TeeWriter method.
// The TeeWriter method attaches the provided io.Writer (in this case a bytes.Buffer) to the existing ResponseWriter.
// Unlike a typical io.MultiWriter, this implementation is designed to ensure that the ResponseWriter remains compatible
// with http interfaces, like io.ReaderFrom or http.Flusher, which might not be the case with a standard MultiWriter.
// Every time data is written to the ResponseWriter, it will also be written to the provided io.Writer.
// It's also worth noting that the TeeWriter method can be called multiple times to add more writers, if needed.
func ExampleContext_TeeWriter() {
	bodyLogger := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			buf := bytes.NewBuffer(nil)
			c.TeeWriter(buf)
			next(c)
			log.Printf("response body: %s", buf.String())
		}
	})

	f := New(WithMiddleware(bodyLogger))
	f.MustHandle(http.MethodGet, "/hello/{name}", func(c Context) {
		_ = c.String(http.StatusOK, "Hello %s\n", c.Param("name"))
	})
}

type gzipResponseWriter struct {
	Writer io.Writer
	http.ResponseWriter
}

// This example demonstrates the usage of the WrapM function which is used to wrap an http.Handler middleware
// and returns a MiddlewareFunc function compatible with Fox.
func ExampleWrapM() {
	// Case 1: Middleware that may write a response and stop execution.
	// This is commonly seen in middleware that implements authorization checks. If the check does not pass,
	// the middleware can stop the execution of the subsequent handlers and write an error response.
	// The "authorizationMiddleware" in this example checks for a specific token in the request's Authorization header.
	// If the token is not the expected value, it sends an HTTP 401 Unauthorized error and stops the execution.
	// In this case, the WrapM function is used with the "useOriginalWriter" set to false, as we want to use
	// the custom ResponseWriter provided by the Fox framework to capture the status code and response size.
	authorizationMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			if token != "valid-token" {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	_ = New(WithMiddleware(WrapM(authorizationMiddleware, false)))

	// Case 2: Middleware that wraps the ResponseWriter with its own implementation.
	// This is typically used in middleware that transforms the response in some way, for instance by applying gzip compression.
	// The "gzipMiddleware" in this example wraps the original ResponseWriter with a gzip writer, which compresses the response data.
	// In this case, the WrapM function is used with the "useOriginalWriter" set to true, as the middleware needs to wrap the original
	// http.ResponseWriter with its own implementation. After wrapping, the Fox framework's ResponseWriter is updated to the new ResponseWriter.
	gzipMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gz := gzip.NewWriter(w)
			defer gz.Close()

			// Create a new ResponseWriter that writes to the gzip writer
			gzw := gzipResponseWriter{Writer: gz, ResponseWriter: w}
			r.Header.Set("Content-Encoding", "gzip")
			next.ServeHTTP(gzw, r)
		})
	}

	_ = New(WithMiddleware(WrapM(gzipMiddleware, true)))
}
