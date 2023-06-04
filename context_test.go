package fox

import (
	"bytes"
	netcontext "context"
	"crypto/rand"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestContext_QueryParams(t *testing.T) {
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
	_, err = cc.Writer().Write([]byte("invalid"))
	assert.ErrorIs(t, err, ErrDiscardedResponseWriter)
}

func TestContext_Ctx(t *testing.T) {
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
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
	require.NoError(t, c.Redirect(http.StatusTemporaryRedirect, "https://example.com/foo/bar"))
	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.Equal(t, "https://example.com/foo/bar", w.Header().Get(HeaderLocation))
}

func TestContext_Blob(t *testing.T) {
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
	assert.True(t, c.Writer().Written())
}

func TestContext_Header(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	fox, c := NewTestContext(w, r)
	c.SetHeader(HeaderServer, "go")
	fox.ServeHTTP(w, r)
	assert.Equal(t, "go", w.Header().Get(HeaderServer))
}

func TestContext_GetHeader(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	r.Header.Set(HeaderAccept, MIMEApplicationJSON)
	_, c := NewTestContext(w, r)
	assert.Equal(t, MIMEApplicationJSON, c.Header(HeaderAccept))
}

func TestContext_TeeWriter_h1(t *testing.T) {

	dumper := bytes.NewBuffer(nil)
	const length = 1 * 1024 * 1024
	buf := make([]byte, length)
	_, _ = rand.Read(buf)

	cases := []struct {
		name    string
		handler HandlerFunc
	}{
		{
			name: "h1 writer",
			handler: func(c Context) {
				dumper.Reset()
				c.TeeWriter(dumper)
				n, err := c.Writer().Write(buf)
				require.NoError(t, err)
				assert.Equal(t, length, n)
			},
		},
		{
			name: "h1 string writer",
			handler: func(c Context) {
				dumper.Reset()
				c.TeeWriter(dumper)
				n, err := io.WriteString(c.Writer(), string(buf))
				require.NoError(t, err)
				assert.Equal(t, length, n)
			},
		},
		{
			name: "h1 reader from",
			handler: func(c Context) {
				dumper.Reset()
				c.TeeWriter(dumper)
				rf, ok := c.Writer().(io.ReaderFrom)
				require.True(t, ok)

				n, err := rf.ReadFrom(bytes.NewReader(buf))
				require.NoError(t, err)
				assert.Equal(t, length, int(n))
			},
		},
		{
			name: "h1 flusher",
			handler: func(c Context) {
				dumper.Reset()
				c.TeeWriter(dumper)
				flusher, ok := c.Writer().(http.Flusher)
				require.True(t, ok)

				_, err := c.Writer().Write(buf[:1024])
				require.NoError(t, err)
				flusher.Flush()
				_, err = c.Writer().Write(buf[1024:])
				require.NoError(t, err)
				flusher.Flush()
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(n *testing.T) {
			f := New()
			f.MustHandle(http.MethodGet, "/foo", tc.handler)

			srv := httptest.NewServer(f)
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/foo", nil)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			out, err := io.ReadAll(resp.Body)
			require.NoError(n, err)
			assert.Equal(n, buf, out)
			require.NoError(n, resp.Body.Close())
			assert.Equal(n, buf, dumper.Bytes())
		})
	}
}

func TestContext_TeeWriter_h2(t *testing.T) {

	dumper := bytes.NewBuffer(nil)
	const length = 1 * 1024 * 1024
	buf := make([]byte, length)
	_, _ = rand.Read(buf)

	cases := []struct {
		name    string
		handler HandlerFunc
	}{
		{
			name: "h2 writer",
			handler: func(c Context) {
				dumper.Reset()
				c.TeeWriter(dumper)
				n, err := c.Writer().Write(buf)
				require.NoError(t, err)
				assert.Equal(t, length, n)
			},
		},
		{
			name: "h2 string writer",
			handler: func(c Context) {
				dumper.Reset()
				c.TeeWriter(dumper)
				n, err := io.WriteString(c.Writer(), string(buf))
				require.NoError(t, err)
				assert.Equal(t, length, n)
			},
		},
		{
			name: "h2 flusher",
			handler: func(c Context) {
				dumper.Reset()
				c.TeeWriter(dumper)
				flusher, ok := c.Writer().(http.Flusher)
				require.True(t, ok)

				_, err := c.Writer().Write(buf[:1024])
				require.NoError(t, err)
				flusher.Flush()
				_, err = c.Writer().Write(buf[1024:])
				require.NoError(t, err)
				flusher.Flush()
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(n *testing.T) {
			f := New()
			f.MustHandle(http.MethodGet, "/foo", tc.handler)

			srv := httptest.NewUnstartedServer(f)

			err := http2.ConfigureServer(srv.Config, new(http2.Server))
			require.NoError(n, err)

			srv.TLS = srv.Config.TLSConfig
			srv.StartTLS()
			defer srv.Close()

			tr := &http.Transport{TLSClientConfig: srv.Config.TLSConfig}
			require.NoError(n, http2.ConfigureTransport(tr))
			tr.TLSClientConfig.InsecureSkipVerify = true
			client := &http.Client{Transport: tr}

			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/foo", nil)
			resp, err := client.Do(req)
			require.NoError(t, err)
			out, err := io.ReadAll(resp.Body)
			require.NoError(n, err)
			assert.Equal(n, buf, out)
			require.NoError(n, resp.Body.Close())
			assert.Equal(n, buf, dumper.Bytes())
		})
	}
}

func TestWrapF(t *testing.T) {
	wrapped := WrapF(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fox"))
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
	wrapped(c)
	assert.Equal(t, "fox", w.Body.String())
}

func TestWrapH(t *testing.T) {
	wrapped := WrapH(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fox"))
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
	wrapped(c)
	assert.Equal(t, "fox", w.Body.String())
}

func TestWrapM(t *testing.T) {
	wrapped := WrapM(func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req := r.Clone(r.Context())
			req.Header.Set("foo", "bar")
			handler.ServeHTTP(w, req)
			_, _ = w.Write([]byte("fox"))
		})
	})
	invoked := false

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)

	fox := New(WithMiddleware(wrapped))
	fox.MustHandle(http.MethodGet, "/foo", func(c Context) {
		assert.Equal(t, "bar", c.Header("foo"))
		invoked = true
	})

	fox.ServeHTTP(w, r)
	assert.Equal(t, "fox", w.Body.String())
	assert.True(t, invoked)
}
