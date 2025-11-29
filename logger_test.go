package fox

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggerWithHandler(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	f, _ := New(
		WithHandleTrailingSlash(RedirectSlash),
		WithMiddleware(Logger(slog.NewTextHandler(buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == "time" {
					return slog.String("time", "time")
				}
				if a.Key == "latency" {
					return slog.String("latency", "latency")
				}
				return a
			},
		}))),
	)
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/success", func(c Context) {
		c.Writer().WriteHeader(http.StatusOK)
	})))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/failure", func(c Context) {
		c.Writer().WriteHeader(http.StatusInternalServerError)
	})))

	cases := []struct {
		name string
		req  *http.Request
		want string
	}{
		{
			name: "should log info level",
			req:  httptest.NewRequest(http.MethodGet, "/success", nil),
			want: "time=time level=INFO msg=192.0.2.1 status=200 method=GET host=example.com path=/success size=0 latency=latency\n",
		},
		{
			name: "should log error level",
			req:  httptest.NewRequest(http.MethodGet, "/failure", nil),
			want: "time=time level=ERROR msg=192.0.2.1 status=500 method=GET host=example.com path=/failure size=0 latency=latency\n",
		},
		{
			name: "should log warn level",
			req:  httptest.NewRequest(http.MethodGet, "/foobar", nil),
			want: "time=time level=WARN msg=192.0.2.1 status=404 method=GET host=example.com path=/foobar size=19 latency=latency\n",
		},
		{
			name: "should log debug level",
			req:  httptest.NewRequest(http.MethodGet, "/success/", nil),
			want: "time=time level=DEBUG msg=192.0.2.1 status=301 method=GET host=example.com path=/success/ size=43 latency=latency location=/success\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			w := httptest.NewRecorder()
			f.ServeHTTP(w, tc.req)
			assert.Equal(t, tc.want, buf.String())
		})
	}

}
