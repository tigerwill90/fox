package fox

import (
	"bytes"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestLoggerWithHandler(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	f := New(
		WithRedirectTrailingSlash(true),
		WithMiddleware(LoggerWithHandler(slog.NewTextHandler(buf, &slog.HandlerOptions{
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
	require.NoError(t, f.Handle(http.MethodGet, "/success", func(c Context) {
		c.Writer().WriteHeader(http.StatusOK)
	}))
	require.NoError(t, f.Handle(http.MethodGet, "/failure", func(c Context) {
		c.Writer().WriteHeader(http.StatusInternalServerError)
	}))
	require.NoError(t, f.Handle(http.MethodGet, "/tags", func(c Context) {
		c.Writer().WriteHeader(http.StatusOK)
		fmt.Println(slices.Collect(c.Route().Tags()))
	}, WithTags("foo", "bar", "baz")))

	cases := []struct {
		name string
		req  *http.Request
		want string
	}{
		{
			name: "should log info level",
			req:  httptest.NewRequest(http.MethodGet, "/success", nil),
			want: "time=time level=INFO msg=192.0.2.1 status=200 method=GET path=/success latency=latency tags=[]\n",
		},
		{
			name: "should log error level",
			req:  httptest.NewRequest(http.MethodGet, "/failure", nil),
			want: "time=time level=ERROR msg=192.0.2.1 status=500 method=GET path=/failure latency=latency tags=[]\n",
		},
		{
			name: "should log warn level",
			req:  httptest.NewRequest(http.MethodGet, "/foobar", nil),
			want: "time=time level=WARN msg=192.0.2.1 status=404 method=GET path=/foobar latency=latency tags=[]\n",
		},
		{
			name: "should log debug level",
			req:  httptest.NewRequest(http.MethodGet, "/success/", nil),
			want: "time=time level=DEBUG msg=192.0.2.1 status=301 method=GET path=/success/ latency=latency tags=[] location=../success\n",
		},
		{
			name: "should log info level tags",
			req:  httptest.NewRequest(http.MethodGet, "/tags", nil),
			want: "time=time level=INFO msg=192.0.2.1 status=200 method=GET path=/tags latency=latency tags=\"[foo bar baz]\"\n",
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
