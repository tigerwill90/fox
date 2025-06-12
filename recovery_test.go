package fox

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tigerwill90/fox/internal/slogpretty"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
)

func TestAbortHandler(t *testing.T) {
	m := CustomRecovery(func(c Context, err any) {
		c.Writer().WriteHeader(http.StatusInternalServerError)
		_, _ = c.Writer().Write([]byte(err.(error).Error()))
	})

	f, _ := New(WithMiddleware(m))

	h := func(c Context) {
		func() { panic(http.ErrAbortHandler) }()
		_ = c.String(200, "foo")
	}

	require.NoError(t, onlyError(f.Handle(http.MethodPost, "/{foo}", h)))
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

func TestRecoveryMiddleware(t *testing.T) {
	woBuf := bytes.NewBuffer(nil)
	weBuf := bytes.NewBuffer(nil)

	m := CustomRecoveryWithLogHandler(&slogpretty.Handler{
		We:  weBuf,
		Wo:  woBuf,
		Lvl: slog.LevelDebug,
	}, func(c Context, err any) {
		c.Writer().WriteHeader(http.StatusInternalServerError)
		_, _ = c.Writer().Write([]byte(err.(string)))
	})

	f, _ := New(WithMiddleware(m))

	const errMsg = "unexpected error"
	h := func(c Context) {
		func() { panic(errMsg) }()
		_ = c.String(200, "foo")
	}

	require.NoError(t, onlyError(f.Handle(http.MethodPost, "/", h)))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set(HeaderAuthorization, "foobar")
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, errMsg, w.Body.String())
	assert.Equal(t, woBuf.Len(), 0)
	assert.NotEqual(t, weBuf.Len(), 0)
}

func TestRecoveryMiddlewareOtherScope(t *testing.T) {
	woBuf := bytes.NewBuffer(nil)
	weBuf := bytes.NewBuffer(nil)

	reset := func() {
		woBuf.Reset()
		weBuf.Reset()
	}

	m := CustomRecoveryWithLogHandler(&slogpretty.Handler{
		We:  weBuf,
		Wo:  woBuf,
		Lvl: slog.LevelDebug,
	}, func(c Context, err any) {
		c.Writer().WriteHeader(http.StatusInternalServerError)
		_, _ = c.Writer().Write([]byte(err.(string)))
	})

	const errMsg = "unexpected error"

	panicMiddleware := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			panic(errMsg)
		}
	})

	f, _ := New(
		WithRedirectTrailingSlash(true),
		WithMiddleware(m),
		WithMiddlewareFor(RedirectSlashHandler, panicMiddleware),
		WithNoRouteHandler(func(c Context) {
			panic(errMsg)
		}),
		WithOptionsHandler(func(c Context) {
			panic(errMsg)
		}),
		WithNoMethodHandler(func(c Context) {
			panic(errMsg)
		}),
	)

	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/foo", emptyHandler)))

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

func TestRecoveryMiddlewareWithBrokenPipe(t *testing.T) {
	woBuf := bytes.NewBuffer(nil)
	weBuf := bytes.NewBuffer(nil)

	expectMsgs := map[syscall.Errno]string{
		syscall.EPIPE:      "broken pipe",
		syscall.ECONNRESET: "connection reset by peer",
	}

	for errno, expectMsg := range expectMsgs {
		t.Run(expectMsg, func(t *testing.T) {
			f, _ := New(WithMiddleware(CustomRecoveryWithLogHandler(&slogpretty.Handler{
				We:  weBuf,
				Wo:  woBuf,
				Lvl: slog.LevelDebug,
			}, func(c Context, err any) {
				if !connIsBroken(err) {
					http.Error(c.Writer(), http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			})))
			require.NoError(t, onlyError(f.Handle(http.MethodGet, "/foo", func(c Context) {
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

func BenchmarkRecoveryMiddleware(b *testing.B) {

	f, _ := New(WithMiddleware(CustomRecoveryWithLogHandler(slog.DiscardHandler, DefaultHandleRecovery)))
	f.MustHandle(http.MethodGet, "/{1}/{2}/{3}", func(c Context) {
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
