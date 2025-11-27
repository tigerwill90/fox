package fox

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tigerwill90/fox/internal/iterutil"
)

func TestDefaultOptions(t *testing.T) {
	f, _ := New(DefaultOptions())
	assert.True(t, f.handleOptions)
	assert.True(t, f.handleMethodNotAllowed)
	assert.True(t, f.allowRegexp)
	assert.Equal(t, f.handlePath, RedirectPath)
	assert.Equal(t, f.handleSlash, RedirectSlash)
}

func TestRouterWithClientIP(t *testing.T) {
	c1 := ClientIPResolverFunc(func(c Context) (*net.IPAddr, error) {
		return c.RemoteIP(), nil
	})
	f, _ := New(WithClientIPResolver(c1), WithNoRouteHandler(func(c Context) {
		assert.Empty(t, c.Pattern())
		ip, err := c.ClientIP()
		assert.NoError(t, err)
		assert.NotNil(t, ip)
		DefaultNotFoundHandler(c)
	}))
	f.MustHandle(http.MethodGet, "/foo", emptyHandler)
	rf := f.Stats()
	assert.True(t, rf.ClientIP)

	rte := f.Route(http.MethodGet, "/foo")
	require.NotNil(t, rte)
	assert.NotNil(t, rte.ClientIPResolver())

	require.NoError(t, onlyError(f.Update(http.MethodGet, "/foo", emptyHandler, WithClientIPResolver(nil))))
	rte = f.Route(http.MethodGet, "/foo")
	require.NotNil(t, rte)
	assert.Nil(t, rte.ClientIPResolver())

	// On not found handler, fallback to global ip resolver
	req := httptest.NewRequest(http.MethodGet, "/bar", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestWithNotFoundHandler(t *testing.T) {
	notFound := func(c Context) {
		_ = c.String(http.StatusNotFound, "NOT FOUND\n")
	}

	f, err := New(WithNoRouteHandler(notFound))
	require.NoError(t, err)
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/foo", emptyHandler)))

	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()

	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "NOT FOUND\n", w.Body.String())

	f, err = New(WithNoRouteHandler(nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestRouterWithMethodNotAllowedHandler(t *testing.T) {
	f, err := New(WithNoMethodHandler(func(c Context) {
		c.SetHeader("FOO", "BAR")
		c.Writer().WriteHeader(http.StatusMethodNotAllowed)
	}))
	require.NoError(t, err)

	require.NoError(t, onlyError(f.Handle(http.MethodPost, "/foo/bar", emptyHandler)))
	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.Equal(t, "POST", w.Header().Get("Allow"))
	assert.Equal(t, "BAR", w.Header().Get("FOO"))

	f, err = New(WithNoMethodHandler(nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestRouterWithOptionsHandler(t *testing.T) {
	f, err := New(WithOptionsHandler(func(c Context) {
		assert.Equal(t, "", c.Pattern())
		assert.Empty(t, slices.Collect(c.Params()))
		c.Writer().WriteHeader(http.StatusNoContent)
	}))
	require.NoError(t, err)

	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/foo/{bar}", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodPost, "/foo/{bar}", emptyHandler)))

	req := httptest.NewRequest(http.MethodOptions, "/foo/bar", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)

	parseAllowHeader := func(allow string) []string {
		if allow == "" {
			return nil
		}
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.ElementsMatch(t, []string{"GET", "POST", "OPTIONS"}, parseAllowHeader(w.Header().Get("Allow")))
	f, err = New(WithOptionsHandler(nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestRouterWithAllowedMethodAndIgnoreTsEnable(t *testing.T) {
	f, _ := New(WithNoMethod(true), WithHandleTrailingSlash(RelaxedSlash))

	// Support for ignore Trailing slash
	cases := []struct {
		name    string
		target  string
		path    string
		req     string
		want    []string
		methods []string
	}{
		{
			name:    "all route except the last one",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead},
			path:    "/foo/bar/",
			req:     "/foo/bar",
			target:  http.MethodTrace,
			want:    []string{"GET", "POST", "PUT", "DELETE", "PATCH", "CONNECT", "OPTIONS", "HEAD"},
		},
		{
			name:    "all route except the first one",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz",
			req:     "/foo/baz/",
			target:  http.MethodGet,
			want:    []string{"POST", "PUT", "DELETE", "PATCH", "CONNECT", "OPTIONS", "HEAD", "TRACE"},
		},
	}

	parseAllowHeader := func(allow string) []string {
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Handle(method, tc.path, emptyHandler)))
			}
			req := httptest.NewRequest(tc.target, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.ElementsMatch(t, tc.want, parseAllowHeader(w.Header().Get("Allow")))
		})
	}
}

func TestRouterWithAutomaticOptionsAndIgnoreTsOptionEnable(t *testing.T) {
	cases := []struct {
		name     string
		target   string
		path     string
		want     []string
		wantCode int
		methods  []string
	}{
		{
			name:     "system-wide requests",
			target:   "*",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			want:     []string{"GET", "PUT", "TRACE", "OPTIONS"},
			wantCode: http.StatusOK,
		},
		{
			name:     "system-wide with custom options registered",
			target:   "*",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     []string{"GET", "PUT", "TRACE", "OPTIONS"},
			wantCode: http.StatusOK,
		},
		{
			name:     "system-wide requests with empty router",
			target:   "*",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "regular option request and ignore ts",
			target:   "/foo/",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			want:     []string{"GET", "PUT", "TRACE", "OPTIONS"},
			wantCode: http.StatusOK,
		},
		{
			name:     "regular option request with handler priority and ignore ts",
			target:   "/foo",
			path:     "/foo/",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     []string{"GET", "OPTIONS", "PUT", "TRACE"},
			wantCode: http.StatusNoContent,
		},
		{
			name:     "regular option request with no matching route",
			target:   "/bar",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			wantCode: http.StatusNotFound,
		},
	}

	parseAllowHeader := func(allow string) []string {
		if allow == "" {
			return nil
		}
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(WithAutoOptions(true), WithHandleTrailingSlash(RelaxedSlash))
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Handle(method, tc.path, func(c Context) {
					req := httptest.NewRequest(http.MethodGet, c.Path(), nil)
					req.Host = c.Host()
					c.SetHeader("Allow", strings.Join(slices.Sorted(iterutil.Left(c.Fox().Iter().Reverse(c.Fox().Iter().Methods(), req))), ", "))
					c.Writer().WriteHeader(http.StatusNoContent)
				})))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.ElementsMatch(t, tc.want, parseAllowHeader(w.Header().Get("Allow")))
		})
	}
}

func TestDeveloppementOptions(t *testing.T) {
	m := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			next(c)
		}
	})
	r, err := New(WithMiddleware(m), DevelopmentOptions())
	require.NoError(t, err)
	assert.Equal(t, reflect.ValueOf(m).Pointer(), reflect.ValueOf(r.mws[2].m).Pointer())
}

func TestWithScopedMiddleware(t *testing.T) {
	called := false
	m := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			called = true
			next(c)
		}
	})

	r, _ := New(WithMiddlewareFor(NoRouteHandler, m))
	require.NoError(t, onlyError(r.Handle(http.MethodGet, "/foo/bar", emptyHandler)))

	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.False(t, called)
	req.URL.Path = "/foo"
	r.ServeHTTP(w, req)
	assert.True(t, called)
}

func TestInvalidMiddleware(t *testing.T) {
	_, err := New(WithMiddleware(Logger(slog.DiscardHandler), nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
	_, err = New(WithMiddlewareFor(NoRouteHandler, nil, Logger(slog.DiscardHandler)))
	assert.ErrorIs(t, err, ErrInvalidConfig)
	f, err := New()
	require.NoError(t, err)
	require.ErrorIs(t, onlyError(f.Handle(http.MethodGet, "/foo", emptyHandler, WithMiddleware(nil))), ErrInvalidConfig)
}

func TestMiddlewareLength(t *testing.T) {
	f, _ := New(DevelopmentOptions())
	r := f.MustHandle(http.MethodGet, "/", emptyHandler, WithMiddleware(Recovery(slog.DiscardHandler), Logger(slog.DiscardHandler)))
	assert.Len(t, f.mws, 2)
	assert.Len(t, r.mws, 4)
}

func TestRouterWithAllowedMethod(t *testing.T) {
	f, _ := New(WithNoMethod(true))

	cases := []struct {
		name    string
		target  string
		path    string
		want    []string
		methods []string
	}{
		{
			name:    "all route except the last one",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead},
			path:    "/foo/bar",
			target:  http.MethodTrace,
			want:    []string{"GET", "POST", "PUT", "DELETE", "PATCH", "CONNECT", "OPTIONS", "HEAD"},
		},
		{
			name:    "all route except the first one",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz",
			target:  http.MethodGet,
			want:    []string{"POST", "PUT", "DELETE", "PATCH", "CONNECT", "OPTIONS", "HEAD", "TRACE"},
		},
		{
			name:    "all route except patch and delete",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/test",
			target:  http.MethodPatch,
			want:    []string{"GET", "POST", "PUT", "CONNECT", "OPTIONS", "HEAD", "TRACE"},
		},
	}

	parseAllowHeader := func(allow string) []string {
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}

	rf := f.Stats()
	require.True(t, rf.MethodNotAllowed)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Handle(method, tc.path, emptyHandler)))
			}
			req := httptest.NewRequest(tc.target, tc.path, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.ElementsMatch(t, tc.want, parseAllowHeader(w.Header().Get("Allow")))
		})
	}
}

func TestRouterWithAllowedMethodAndAutoOptions(t *testing.T) {
	f, _ := New(WithNoMethod(true), WithAutoOptions(true))

	// Support for ignore Trailing slash
	cases := []struct {
		name    string
		target  string
		path    string
		req     string
		want    []string
		methods []string
	}{
		{
			name:    "all route except the last one",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead},
			path:    "/foo/bar",
			req:     "/foo/bar",
			target:  http.MethodTrace,
			want:    []string{"GET", "POST", "PUT", "DELETE", "PATCH", "CONNECT", "OPTIONS", "HEAD"},
		},
		{
			name:    "all route except the first one and inferred options from auto options",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz/",
			req:     "/foo/baz/",
			target:  http.MethodGet,
			want:    []string{"POST", "PUT", "DELETE", "PATCH", "CONNECT", "HEAD", "TRACE", "OPTIONS"},
		},
	}

	parseAllowHeader := func(allow string) []string {
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Handle(method, tc.path, emptyHandler)))
			}
			req := httptest.NewRequest(tc.target, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.ElementsMatch(t, tc.want, parseAllowHeader(w.Header().Get("Allow")))
		})
	}
}

func TestRouterWithAllowedMethodAndIgnoreTsDisable(t *testing.T) {
	f, _ := New(WithNoMethod(true))

	// Support for ignore Trailing slash
	cases := []struct {
		name    string
		target  string
		path    string
		req     string
		want    int
		methods []string
	}{
		{
			name:    "all route except the last one",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead},
			path:    "/foo/bar/",
			req:     "/foo/bar",
			target:  http.MethodTrace,
		},
		{
			name:    "all route except the first one",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz",
			req:     "/foo/baz/",
			target:  http.MethodGet,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Handle(method, tc.path, emptyHandler)))
			}
			req := httptest.NewRequest(tc.target, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func TestRouterWithAutomaticOptions(t *testing.T) {

	cases := []struct {
		name     string
		target   string
		path     string
		want     []string
		wantCode int
		methods  []string
	}{
		{
			name:     "system-wide requests",
			target:   "*",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			want:     []string{"GET", "PUT", "TRACE", "OPTIONS"},
			wantCode: http.StatusOK,
		},
		{
			name:     "system-wide with custom options registered",
			target:   "*",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     []string{"GET", "PUT", "TRACE", "OPTIONS"},
			wantCode: http.StatusOK,
		},
		{
			name:     "system-wide requests with empty router",
			target:   "*",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "regular option request",
			target:   "/foo",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			want:     []string{"GET", "PUT", "TRACE", "OPTIONS"},
			wantCode: http.StatusOK,
		},
		{
			name:     "regular option request with handler priority",
			target:   "/foo",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     []string{"GET", "OPTIONS", "PUT", "TRACE"},
			wantCode: http.StatusNoContent,
		},
		{
			name:     "regular option request with no matching route",
			target:   "/bar",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			wantCode: http.StatusNotFound,
		},
	}

	parseAllowHeader := func(allow string) []string {
		if allow == "" {
			return nil
		}
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(WithAutoOptions(true))
			rf := f.Stats()
			require.True(t, rf.AutoOptions)
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Handle(method, tc.path, func(c Context) {
					req := httptest.NewRequest(http.MethodGet, c.Path(), nil)
					req.Host = c.Host()
					c.SetHeader("Allow", strings.Join(slices.Sorted(iterutil.Left(c.Fox().Iter().Reverse(c.Fox().Iter().Methods(), req))), ", "))
					c.Writer().WriteHeader(http.StatusNoContent)
				})))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.ElementsMatch(t, tc.want, parseAllowHeader(w.Header().Get("Allow")))
		})
	}
}

func TestRouterWithAutomaticOptionsAndIgnoreTsOptionDisable(t *testing.T) {
	cases := []struct {
		name     string
		target   string
		path     string
		wantCode int
		methods  []string
	}{
		{
			name:     "regular option request and ignore ts",
			target:   "/foo/",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			wantCode: http.StatusNotFound,
		},
		{
			name:     "regular option request with handler priority and ignore ts",
			target:   "/foo",
			path:     "/foo/",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			wantCode: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(WithAutoOptions(true))
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Handle(method, tc.path, func(c Context) {
					req := httptest.NewRequest(http.MethodGet, c.Path(), nil)
					req.Host = c.Host()
					c.SetHeader("Allow", strings.Join(slices.Sorted(iterutil.Left(c.Fox().Iter().Reverse(c.Fox().Iter().Methods(), req))), ", "))
					c.Writer().WriteHeader(http.StatusNoContent)
				})))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestInvalidAnnotation(t *testing.T) {
	var nonComparableKey = []int{1, 2, 3}
	f, err := New()
	require.NoError(t, err)
	assert.ErrorIs(t, onlyError(f.Handle(http.MethodGet, "/foo/{bar}", emptyHandler, WithAnnotation(nonComparableKey, nil))), ErrInvalidConfig)
}

func TestAnnotationFuncWithError(t *testing.T) {
	f, err := New()
	require.NoError(t, err)
	want := errors.New("some error")
	fn := func() (any, error) {
		return nil, want
	}

	err = onlyError(f.Handle(http.MethodGet, "/foo/{bar}", emptyHandler, WithAnnotationFunc("foo", fn)))
	assert.ErrorIs(t, err, ErrInvalidConfig)
	assert.ErrorIs(t, err, want)
}
