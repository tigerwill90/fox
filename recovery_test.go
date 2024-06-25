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

	r := New(WithMiddleware(m))

	h := func(c Context) {
		func() { panic(http.ErrAbortHandler) }()
		_ = c.String(200, "foo")
	}

	require.NoError(t, r.Tree().Handle(http.MethodPost, "/{foo}", h))
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
	r.ServeHTTP(w, req)
}

func TestRecoveryMiddleware(t *testing.T) {
	woBuf := bytes.NewBuffer(nil)
	weBuf := bytes.NewBuffer(nil)

	m := CustomRecoveryWithLogHandler(&slogpretty.LogHandler{
		We:  weBuf,
		Wo:  woBuf,
		Lvl: slog.LevelDebug,
	}, func(c Context, err any) {
		c.Writer().WriteHeader(http.StatusInternalServerError)
		_, _ = c.Writer().Write([]byte(err.(string)))
	})

	r := New(WithMiddleware(m))

	const errMsg = "unexpected error"
	h := func(c Context) {
		func() { panic(errMsg) }()
		_ = c.String(200, "foo")
	}

	require.NoError(t, r.Tree().Handle(http.MethodPost, "/", h))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set(HeaderAuthorization, "foobar")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, errMsg, w.Body.String())
	assert.Equal(t, woBuf.Len(), 0)
	assert.NotEqual(t, weBuf.Len(), 0)
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
			f := New(WithMiddleware(CustomRecoveryWithLogHandler(&slogpretty.LogHandler{
				We:  weBuf,
				Wo:  woBuf,
				Lvl: slog.LevelDebug,
			}, func(c Context, err any) {
				if !connIsBroken(err) {
					http.Error(c.Writer(), http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			})))
			require.NoError(t, f.Handle(http.MethodGet, "/foo", func(c Context) {
				e := &net.OpError{Err: &os.SyscallError{Err: errno}}
				panic(e)
			}))

			req := httptest.NewRequest(http.MethodGet, "/foo", nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, woBuf.Len(), 0)
			assert.NotEqual(t, weBuf.Len(), 0)
		})
	}
}
