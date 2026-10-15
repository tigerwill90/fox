package fox

import (
	"bytes"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"

	"github.com/fox-toolkit/fox/internal/slogpretty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAbortHandler(t *testing.T) {
	m := RecoveryWithFunc(slog.DiscardHandler, func(c *Context, err any) {
		c.Writer().WriteHeader(http.StatusInternalServerError)
		_, _ = c.Writer().Write([]byte(err.(error).Error()))
	})

	f, _ := NewRouter(WithMiddleware(m))

	h := func(c *Context) {
		func() { panic(http.ErrAbortHandler) }()
		_ = c.String(200, "foo")
	}

	require.NoError(t, onlyError(f.Add(MethodPost, "/{foo}", h)))
	req := httptest.NewRequest(http.MethodPost, "/foo", nil)
	req.Header.Set(HeaderAuthorization, "foobar")
	w := httptest.NewRecorder()

	defer func() {
		val := recover()
		require.NotNil(t, val)
		err := val.(error)
		require.NotNil(t, err)
		assert.ErrorIs(t, err, http.ErrAbortHandler)
	}()
	f.ServeHTTP(w, req)
}

func TestRecoveryWithFunc(t *testing.T) {
	woBuf := bytes.NewBuffer(nil)
	weBuf := bytes.NewBuffer(nil)

	m := RecoveryWithFunc(&slogpretty.Handler{
		We:  weBuf,
		Wo:  woBuf,
		Lvl: slog.LevelDebug,
	}, func(c *Context, err any) {
		c.Writer().WriteHeader(http.StatusInternalServerError)
		_, _ = c.Writer().Write([]byte(err.(string)))
	})

	f, _ := NewRouter(WithMiddleware(m))

	const errMsg = "unexpected error"
	h := func(c *Context) {
		func() { panic(errMsg) }()
		_ = c.String(200, "foo")
	}

	require.NoError(t, onlyError(f.Add(MethodPost, "/", h)))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set(HeaderAuthorization, "foobar")
	req.Header.Set(HeaderXCSRFToken, "csrf-secret")
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, errMsg, w.Body.String())
	assert.Equal(t, woBuf.Len(), 0)
	assert.NotEqual(t, weBuf.Len(), 0)
	assert.NotContains(t, weBuf.String(), "foobar")
	assert.NotContains(t, weBuf.String(), "csrf-secret")
}

func TestRecoveryWithFunc_InvalidStatus(t *testing.T) {
	f, _ := NewRouter(WithMiddleware(Recovery(slog.DiscardHandler)))
	require.NoError(t, onlyError(f.Add(MethodGet, "/", func(c *Context) {
		c.Writer().WriteHeader(0)
	})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRecoveryWithFunc_AbortWritten(t *testing.T) {
	cases := []struct {
		name string
		h    HandlerFunc
	}{
		{
			name: "after partial write",
			h: func(c *Context) {
				_, _ = c.Writer().Write([]byte("partial"))
				_ = c.Writer().FlushError()
				panic("boom")
			},
		},
		{
			name: "after hijack",
			h: func(c *Context) {
				conn, _, _ := c.Writer().Hijack()
				_ = conn.Close()
				panic("boom")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			weBuf := bytes.NewBuffer(nil)
			called := false
			done := make(chan struct{})
			f, _ := NewRouter(WithMiddleware(
				MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
					return func(c *Context) {
						defer close(done)
						next(c)
					}
				}),
				RecoveryWithFunc(&slogpretty.Handler{We: weBuf, Wo: weBuf, Lvl: slog.LevelDebug}, func(c *Context, err any) {
					called = true
				}),
			))
			require.NoError(t, onlyError(f.Add(MethodGet, "/", tc.h)))

			srv := httptest.NewUnstartedServer(f)
			srv.Config.ErrorLog = log.New(io.Discard, "", 0)
			srv.Start()
			defer srv.Close()

			res, err := srv.Client().Get(srv.URL)
			if err == nil {
				_, err = io.ReadAll(res.Body)
				_ = res.Body.Close()
			}
			assert.Error(t, err)
			<-done
			assert.False(t, called)
			assert.Contains(t, weBuf.String(), "boom")
		})
	}
}

func TestRecoveryWithFunc_OtherScope(t *testing.T) {
	woBuf := bytes.NewBuffer(nil)
	weBuf := bytes.NewBuffer(nil)

	reset := func() {
		woBuf.Reset()
		weBuf.Reset()
	}

	m := RecoveryWithFunc(&slogpretty.Handler{
		We:  weBuf,
		Wo:  woBuf,
		Lvl: slog.LevelDebug,
	}, func(c *Context, err any) {
		c.Writer().WriteHeader(http.StatusInternalServerError)
		_, _ = c.Writer().Write([]byte(err.(string)))
	})

	const errMsg = "unexpected error"

	panicMiddleware := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			panic(errMsg)
		}
	})

	f, _ := NewRouter(
		WithTrailingSlash(RedirectSlash),
		WithMergeSlashes(RedirectPath),
		WithCollapseDotSegments(RedirectPath),
		WithMiddleware(m),
		WithMiddlewareFor(RedirectSlashHandler|RedirectPathHandler|RejectPathHandler, panicMiddleware),
		WithNoRouteHandler(func(c *Context) {
			panic(errMsg)
		}),
		WithOptionsHandler(func(c *Context) {
			panic(errMsg)
		}),
		WithNoMethodHandler(func(c *Context) {
			panic(errMsg)
		}),
	)

	require.NoError(t, onlyError(f.Add(MethodGet, "/foo", emptyHandler)))

	t.Run("no route handler", func(t *testing.T) {
		reset()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, errMsg, w.Body.String())
		assert.Equal(t, woBuf.Len(), 0)
		assert.NotEqual(t, weBuf.Len(), 0)
	})
	t.Run("no method handler", func(t *testing.T) {
		reset()
		req := httptest.NewRequest(http.MethodPost, "/foo", nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, errMsg, w.Body.String())
		assert.Equal(t, woBuf.Len(), 0)
		assert.NotEqual(t, weBuf.Len(), 0)
	})
	t.Run("redirect trailing slash", func(t *testing.T) {
		reset()
		req := httptest.NewRequest(http.MethodGet, "/foo/", nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, errMsg, w.Body.String())
		assert.Equal(t, woBuf.Len(), 0)
		assert.NotEqual(t, weBuf.Len(), 0)
	})
	t.Run("redirect fixed path", func(t *testing.T) {
		reset()
		req := httptest.NewRequest(http.MethodGet, "//foo/", nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, errMsg, w.Body.String())
		assert.Equal(t, woBuf.Len(), 0)
		assert.NotEqual(t, weBuf.Len(), 0)
	})
	t.Run("reject path", func(t *testing.T) {
		reset()
		req := httptest.NewRequest(http.MethodGet, "/../foo", nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, errMsg, w.Body.String())
		assert.Equal(t, woBuf.Len(), 0)
		assert.NotEqual(t, weBuf.Len(), 0)
		assert.Contains(t, weBuf.String(), "RejectPathHandler")
	})
	t.Run("option handler", func(t *testing.T) {
		reset()
		req := httptest.NewRequest(http.MethodOptions, "/foo", nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, errMsg, w.Body.String())
		assert.Equal(t, woBuf.Len(), 0)
		assert.NotEqual(t, weBuf.Len(), 0)
	})
}

func TestRecoveryWithFunc_BrokenPipe(t *testing.T) {
	woBuf := bytes.NewBuffer(nil)
	weBuf := bytes.NewBuffer(nil)

	expectMsgs := map[syscall.Errno]string{
		syscall.EPIPE:      "broken pipe",
		syscall.ECONNRESET: "connection reset by peer",
	}

	for errno, expectMsg := range expectMsgs {
		t.Run(expectMsg, func(t *testing.T) {
			f, _ := NewRouter(WithMiddleware(RecoveryWithFunc(&slogpretty.Handler{
				We:  weBuf,
				Wo:  woBuf,
				Lvl: slog.LevelDebug,
			}, func(c *Context, err any) {
				if !connIsBroken(err) {
					http.Error(c.Writer(), http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			})))
			require.NoError(t, onlyError(f.Add(MethodGet, "/foo", func(c *Context) {
				e := &net.OpError{Err: &os.SyscallError{Err: errno}}
				panic(e)
			})))

			req := httptest.NewRequest(http.MethodGet, "/foo", nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, woBuf.Len(), 0)
			assert.NotEqual(t, weBuf.Len(), 0)
			woBuf.Reset()
			weBuf.Reset()
		})
	}
}

func BenchmarkRecoveryWithFunc(b *testing.B) {

	f, _ := NewRouter(WithMiddleware(RecoveryWithFunc(slog.DiscardHandler, DefaultHandleRecovery)))
	f.MustAdd(MethodGet, "/{1}/{2}/{3}", func(c *Context) {
		panic("yolo")
	})

	req := httptest.NewRequest(http.MethodGet, "/foo/bar/baz", nil)
	w := new(mockResponseWriter)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		f.ServeHTTP(w, req)
	}
}
