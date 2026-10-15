package fox

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRouter_Defaults(t *testing.T) {
	f, _ := NewRouter()
	assert.False(t, f.handleOPTIONS)
	assert.False(t, f.handleMethodNotAllowed)
	assert.False(t, f.allowRegexp)
	assert.Equal(t, ExactPath, f.mergeSlash)
	assert.Equal(t, ExactPath, f.collapseDots)
	assert.Equal(t, ExactSlash, f.handleSlash)
}

func TestWithClientIPResolver(t *testing.T) {
	c1 := ClientIPResolverFunc(func(c RequestContext) (netip.Addr, error) {
		return c.RemoteIP(), nil
	})
	f, _ := NewRouter(WithClientIPResolver(c1), WithNoRouteHandler(func(c *Context) {
		assert.Empty(t, c.Pattern())
		ip, err := c.ClientIP()
		assert.NoError(t, err)
		assert.True(t, ip.IsValid())
		DefaultNoRouteHandler(c)
	}))
	f.MustAdd(MethodGet, "/foo", emptyHandler)
	rf := f.Info()
	assert.True(t, rf.ClientIPResolver)

	rte := f.Route(MethodGet, "/foo")
	require.NotNil(t, rte)
	assert.NotNil(t, rte.ClientIPResolver())

	require.NoError(t, onlyError(f.Update(MethodGet, "/foo", emptyHandler, WithClientIPResolver(nil))))
	rte = f.Route(MethodGet, "/foo")
	require.NotNil(t, rte)
	assert.Nil(t, rte.ClientIPResolver())

	// On not found handler, fallback to global ip resolver
	req := httptest.NewRequest(http.MethodGet, "/bar", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestWithNotFoundHandler(t *testing.T) {
	notFound := func(c *Context) {
		_ = c.String(http.StatusNotFound, "NOT FOUND\n")
	}

	f, err := NewRouter(WithNoRouteHandler(notFound))
	require.NoError(t, err)
	require.NoError(t, onlyError(f.Add(MethodGet, "/foo", emptyHandler)))

	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()

	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "NOT FOUND\n", w.Body.String())

	f, err = NewRouter(WithNoRouteHandler(nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestWithMethodNotAllowedHandler(t *testing.T) {
	f, err := NewRouter(WithNoMethodHandler(func(c *Context) {
		c.SetHeader("FOO", "BAR")
		c.Writer().WriteHeader(http.StatusMethodNotAllowed)
	}))
	require.NoError(t, err)

	require.NoError(t, onlyError(f.Add(MethodPost, "/foo/bar", emptyHandler)))
	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.Equal(t, "POST", w.Header().Get("Allow"))
	assert.Equal(t, "BAR", w.Header().Get("FOO"))

	f, err = NewRouter(WithNoMethodHandler(nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestWithOptionsHandler(t *testing.T) {
	f, err := NewRouter(WithOptionsHandler(func(c *Context) {
		assert.Equal(t, "", c.Pattern())
		assert.Empty(t, slices.Collect(c.Params()))
		c.Writer().WriteHeader(http.StatusNoContent)
	}))
	require.NoError(t, err)

	require.NoError(t, onlyError(f.Add(MethodGet, "/foo/{bar}", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodPost, "/foo/{bar}", emptyHandler)))

	req := httptest.NewRequest(http.MethodOptions, "/foo/bar", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "OPTIONS, GET, POST", w.Header().Get(HeaderAllow))
	f, err = NewRouter(WithOptionsHandler(nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestRouter_ServeHTTP_AllowedMethodWithIgnoreTsEnable(t *testing.T) {
	f, _ := NewRouter(WithNoMethod(true), WithTrailingSlash(RelaxedSlash))

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
			want:    []string{"DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"},
		},
		{
			name:    "all route except the first one",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz",
			req:     "/foo/baz/",
			target:  http.MethodGet,
			want:    []string{"DELETE", "HEAD", "OPTIONS", "PATCH", "POST", "PUT", "TRACE"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Add([]string{method}, tc.path, emptyHandler)))
			}
			req := httptest.NewRequest(tc.target, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.Equal(t, strings.Join(tc.want, ", "), w.Header().Get(HeaderAllow))
		})
	}
}

func TestRouter_ServeHTTP_AutomaticOptionsWithIgnoreTsEnable(t *testing.T) {
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
			wantCode: http.StatusOK,
		},
		{
			name:     "regular option request and ignore ts",
			target:   "/foo/",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT", "CONNECT"},
			want:     []string{"OPTIONS", "GET", "PUT", "TRACE"},
			wantCode: http.StatusNoContent,
		},
		{
			name:     "regular option request with handler priority and ignore ts",
			target:   "/foo",
			path:     "/foo/",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     []string{"GET", "TRACE", "PUT", "OPTIONS"},
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

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := MustRouter(WithAutoOptions(true), WithTrailingSlash(RelaxedSlash))
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Add([]string{method}, tc.path, func(c *Context) {
					req := httptest.NewRequest(http.MethodGet, c.Request().URL.Path, nil)
					req.Host = c.Host()
					c.SetHeader("Allow", strings.Join(tc.methods, ", "))
					c.Writer().WriteHeader(http.StatusNoContent)
				})))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, strings.Join(tc.want, ", "), w.Header().Get(HeaderAllow))
		})
	}
}

func TestDevelopmentOptions(t *testing.T) {
	m := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			next(c)
		}
	})
	r, err := NewRouter(WithMiddleware(m), WithPrettyLogs())
	require.NoError(t, err)
	assert.Equal(t, reflect.ValueOf(m).Pointer(), reflect.ValueOf(r.mws[2].m).Pointer())
}

func TestWithScopedMiddleware(t *testing.T) {
	called := false
	m := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			called = true
			next(c)
		}
	})

	r, _ := NewRouter(WithMiddlewareFor(NoRouteHandler, m))
	require.NoError(t, onlyError(r.Add(MethodGet, "/foo/bar", emptyHandler)))

	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.False(t, called)
	req.URL.Path = "/foo"
	r.ServeHTTP(w, req)
	assert.True(t, called)
}

func TestWithMiddleware_Invalid(t *testing.T) {
	_, err := NewRouter(WithMiddleware(Logger(slog.DiscardHandler), nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
	_, err = NewRouter(WithMiddlewareFor(NoRouteHandler, nil, Logger(slog.DiscardHandler)))
	assert.ErrorIs(t, err, ErrInvalidConfig)
	_, err = NewRouter(WithMiddlewareFor(0, Logger(slog.DiscardHandler)))
	assert.ErrorIs(t, err, ErrInvalidConfig)
	_, err = NewRouter(WithMiddlewareFor(HandlerScope(1), Logger(slog.DiscardHandler)))
	assert.ErrorIs(t, err, ErrInvalidConfig)
	f, err := NewRouter()
	require.NoError(t, err)
	require.ErrorIs(t, onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithMiddleware(nil))), ErrInvalidConfig)
}

func TestWithMiddleware_MaxLength(t *testing.T) {
	f, _ := NewRouter(WithPrettyLogs())
	r := f.MustAdd(MethodGet, "/", emptyHandler, WithMiddleware(Recovery(slog.DiscardHandler), Logger(slog.DiscardHandler)))
	assert.Len(t, f.mws, 2)
	assert.Len(t, r.mws, 2)
}

func TestRouter_ServeHTTP_AllowedMethod(t *testing.T) {
	f := MustRouter(WithNoMethod(true))

	type route struct {
		methods []string
		pattern string
	}

	cases := []struct {
		name   string
		method string
		target string
		routes []route
		want   []string
	}{
		{
			name: "all route except the last one",
			routes: []route{
				{[]string{http.MethodGet, http.MethodPost}, "/foo/bar"},
				{[]string{http.MethodPut, http.MethodDelete}, "/foo/bar"},
				{[]string{http.MethodPatch, http.MethodConnect, http.MethodOptions}, "/foo/bar"},
				{[]string{http.MethodHead}, "/foo/bar"},
			},
			target: "/foo/bar",
			method: http.MethodTrace,
			want:   []string{"CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"},
		},
		{
			name: "all route except the first one",
			routes: []route{
				{[]string{http.MethodPost, http.MethodPut}, "/foo/baz"},
				{[]string{http.MethodDelete, http.MethodPatch}, "/foo/baz"},
				{[]string{http.MethodConnect, http.MethodOptions, http.MethodHead}, "/foo/baz"},
				{[]string{http.MethodTrace}, "/foo/baz"},
			},
			target: "/foo/baz",
			method: http.MethodGet,
			want:   []string{"CONNECT", "DELETE", "HEAD", "OPTIONS", "PATCH", "POST", "PUT", "TRACE"},
		},
		{
			name: "all route except patch and delete",
			routes: []route{
				{[]string{http.MethodGet, http.MethodPost}, "/test"},
				{[]string{http.MethodPut}, "/test"},
				{[]string{http.MethodConnect, http.MethodOptions}, "/test"},
				{[]string{http.MethodHead, http.MethodTrace}, "/test"},
			},
			target: "/test",
			method: http.MethodPatch,
			want:   []string{"CONNECT", "GET", "HEAD", "OPTIONS", "POST", "PUT", "TRACE"},
		},
		{
			name: "no auto OPTIONS request with other matching methods",
			routes: []route{
				{[]string{http.MethodGet}, "/buzz"},
				{[]string{http.MethodPost}, "/buzz"},
				{[]string{http.MethodPut}, "/buzz"},
			},
			target: "/buzz",
			method: http.MethodOptions,
			want:   []string{"GET", "POST", "PUT"},
		},
		{
			name: "route with method overlapping",
			routes: []route{
				{[]string{http.MethodGet, http.MethodPost, http.MethodPut}, "/users/123"},
				{[]string{http.MethodGet, http.MethodPut, http.MethodConnect, http.MethodHead}, "/users/{id}"},
				{[]string{http.MethodOptions}, "/users/{id}"},
			},
			target: "/users/123",
			method: http.MethodTrace,
			want:   []string{"CONNECT", "GET", "HEAD", "OPTIONS", "POST", "PUT"},
		},
	}

	rf := f.Info()
	require.True(t, rf.NoMethod)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, r := range tc.routes {
				f.MustAdd(r.methods, r.pattern, emptyHandler)
			}
			req := httptest.NewRequest(tc.method, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.Equal(t, strings.Join(tc.want, ", "), w.Header().Get(HeaderAllow))
		})
	}
}

func TestRouter_ServeHTTP_AllowedMethodWithAutoOptions(t *testing.T) {
	f, _ := NewRouter(WithNoMethod(true), WithAutoOptions(true))

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
			want:    []string{"CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"},
		},
		{
			name:    "all route except the first one and inferred options from auto options",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz/",
			req:     "/foo/baz/",
			target:  http.MethodGet,
			want:    []string{"CONNECT", "DELETE", "HEAD", "PATCH", "POST", "PUT", "TRACE", "OPTIONS"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Add([]string{method}, tc.path, emptyHandler)))
			}
			req := httptest.NewRequest(tc.target, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.Equal(t, strings.Join(tc.want, ", "), w.Header().Get(HeaderAllow))
		})
	}
}

func TestRouter_ServeHTTP_AllowedMethodWithIgnoreTsDisable(t *testing.T) {
	f, _ := NewRouter(WithNoMethod(true))

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
				require.NoError(t, onlyError(f.Add([]string{method}, tc.path, emptyHandler)))
			}
			req := httptest.NewRequest(tc.target, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func TestRouter_ServeHTTP_AutomaticOptions(t *testing.T) {

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
			wantCode: http.StatusOK,
		},
		{
			name:     "system-wide with only options method registered",
			target:   "*",
			path:     "/foo",
			methods:  []string{"OPTIONS"},
			want:     []string{"OPTIONS"},
			wantCode: http.StatusOK,
		},
		{
			name:     "regular OPTIONS request",
			target:   "/foo",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			want:     []string{"OPTIONS", "GET", "PUT", "TRACE"},
			wantCode: http.StatusNoContent,
		},
		{
			name:     "regular OPTIONS request with handler priority",
			target:   "/foo",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     []string{"GET", "TRACE", "PUT", "OPTIONS"},
			wantCode: http.StatusNoContent,
		},
		{
			name:     "regular OPTIONS request with no matching route",
			target:   "/bar",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			wantCode: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAutoOptions(true), WithSystemWideOptions(true))
			rf := f.Info()
			require.True(t, rf.AutoOptions)
			require.True(t, rf.SystemWideOptions)
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Add([]string{method}, tc.path, func(c *Context) {
					req := httptest.NewRequest(http.MethodGet, c.Request().URL.Path, nil)
					req.Host = c.Host()
					c.SetHeader("Allow", strings.Join(tc.methods, ", "))
					c.Writer().WriteHeader(http.StatusNoContent)
				})))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, strings.Join(tc.want, ", "), w.Header().Get(HeaderAllow))
		})
	}
}

func TestRouter_ServeHTTP_AutomaticCORSPreflightOptions(t *testing.T) {

	cases := []struct {
		name     string
		target   string
		path     string
		headers  http.Header
		methods  []string
		wantCode int
	}{
		{
			name:     "CORS preflight OPTIONS request",
			target:   "/foo",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			headers:  http.Header{HeaderOrigin: []string{"https://example.com"}, HeaderAccessControlRequestMethod: []string{http.MethodGet}},
			wantCode: http.StatusNoContent,
		},
		{
			name:     "CORS preflight OPTIONS request with no matching route",
			target:   "/bar",
			headers:  http.Header{HeaderOrigin: []string{"https://example.com"}, HeaderAccessControlRequestMethod: []string{http.MethodGet}},
			path:     "/foo",
			wantCode: http.StatusNoContent,
		},
		{
			name:     "CORS preflight OPTIONS request with no matching ACRM but matched route",
			target:   "/foo",
			methods:  []string{"POST", "PUT"},
			headers:  http.Header{HeaderOrigin: []string{"https://example.com"}, HeaderAccessControlRequestMethod: []string{http.MethodGet}},
			path:     "/foo",
			wantCode: http.StatusNoContent,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := MustRouter(WithAutoOptions(true), WithSystemWideOptions(true), WithNoMethod(true))
			rf := f.Info()
			require.True(t, rf.AutoOptions)
			require.True(t, rf.SystemWideOptions)
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Add([]string{method}, tc.path, emptyHandler)))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			req.Header = tc.headers
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Empty(t, w.Header().Get("Allow"))
		})
	}
}

func TestRouter_ServeHTTP_AutomaticOptionsWithIgnoreTsDisable(t *testing.T) {
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
			f, _ := NewRouter(WithAutoOptions(true))
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Add([]string{method}, tc.path, func(c *Context) {
					req := httptest.NewRequest(http.MethodGet, c.Request().URL.Path, nil)
					req.Host = c.Host()
					c.SetHeader("Allow", strings.Join(tc.methods, ","))
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

func TestWithAnnotation_Invalid(t *testing.T) {
	var nonComparableKey = []int{1, 2, 3}
	var unhashableKey = [2]any{func() {}, 1}
	f, err := NewRouter()
	require.NoError(t, err)
	assert.ErrorIs(t, onlyError(f.Add(MethodGet, "/foo/{bar}", emptyHandler, WithAnnotation(nonComparableKey, nil))), ErrInvalidConfig)
	assert.ErrorIs(t, onlyError(f.Add(MethodGet, "/foo/{bar}", emptyHandler, WithAnnotation(nil, "value"))), ErrInvalidConfig)
	assert.ErrorIs(t, onlyError(f.Add(MethodGet, "/foo/{bar}", emptyHandler, WithAnnotation(unhashableKey, nil))), ErrInvalidConfig)
}

func TestWithTrailingSlash_LeadingDoubleSlash(t *testing.T) {
	f := MustRouter()
	assert.ErrorIs(t, onlyError(f.Add(MethodGet, "//foo", emptyHandler, WithTrailingSlash(RedirectSlash))), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Add(MethodGet, "example.com//foo", emptyHandler, WithTrailingSlash(RedirectSlash))), ErrInvalidRoute)
	assert.NoError(t, onlyError(f.Add(MethodGet, "//foo", emptyHandler, WithTrailingSlash(RelaxedSlash))))
	assert.NoError(t, onlyError(f.Add(MethodGet, "//bar", emptyHandler, WithTrailingSlash(ExactSlash))))
}

func TestWithQueryMatcher(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{
			name:    "valid query matcher",
			key:     "foo",
			value:   "bar",
			wantErr: false,
		},
		{
			name:    "empty key",
			key:     "",
			value:   "bar",
			wantErr: true,
		},
		{
			name:    "empty value is valid",
			key:     "foo",
			value:   "",
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NewRouter()
			require.NoError(t, err)
			err = onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithQueryMatcher(tc.key, tc.value)))
			if tc.wantErr {
				assert.ErrorIs(t, err, ErrInvalidMatcher)
				return
			}
			require.NoError(t, err)
			m, _ := MatchQuery(tc.key, tc.value)
			assert.True(t, f.Has(MethodGet, "/foo", m))
		})
	}
}

func TestWithQueryRegexpMatcher(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		expr    string
		wantErr bool
	}{
		{
			name:    "valid query regexp matcher",
			key:     "id",
			expr:    `\d+`,
			wantErr: false,
		},
		{
			name:    "empty key",
			key:     "",
			expr:    `\d+`,
			wantErr: true,
		},
		{
			name:    "invalid regexp",
			key:     "id",
			expr:    `[`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NewRouter()
			require.NoError(t, err)
			err = onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithQueryRegexpMatcher(tc.key, tc.expr)))
			if tc.wantErr {
				assert.ErrorIs(t, err, ErrInvalidMatcher)
				return
			}
			require.NoError(t, err)
			m, _ := MatchQueryRegexp(tc.key, tc.expr)
			assert.True(t, f.Has(MethodGet, "/foo", m))
		})
	}
}

func TestWithHeaderMatcher(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{
			name:    "valid header matcher",
			key:     "Content-Type",
			value:   "application/json",
			wantErr: false,
		},
		{
			name:    "empty key",
			key:     "",
			value:   "application/json",
			wantErr: true,
		},
		{
			name:    "empty value is valid",
			key:     "X-Custom",
			value:   "",
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NewRouter()
			require.NoError(t, err)
			err = onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithHeaderMatcher(tc.key, tc.value)))
			if tc.wantErr {
				assert.ErrorIs(t, err, ErrInvalidMatcher)
				return
			}
			require.NoError(t, err)
			m, _ := MatchHeader(tc.key, tc.value)
			assert.True(t, f.Has(MethodGet, "/foo", m))
		})
	}
}

func TestWithHeaderRegexpMatcher(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		expr    string
		wantErr bool
	}{
		{
			name:    "valid header regexp matcher",
			key:     "Content-Type",
			expr:    `application/.*`,
			wantErr: false,
		},
		{
			name:    "empty key",
			key:     "",
			expr:    `application/.*`,
			wantErr: true,
		},
		{
			name:    "invalid regexp",
			key:     "Content-Type",
			expr:    `[`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NewRouter()
			require.NoError(t, err)
			err = onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithHeaderRegexpMatcher(tc.key, tc.expr)))
			if tc.wantErr {
				assert.ErrorIs(t, err, ErrInvalidMatcher)
				return
			}
			require.NoError(t, err)
			m, _ := MatchHeaderRegexp(tc.key, tc.expr)
			assert.True(t, f.Has(MethodGet, "/foo", m))
		})
	}
}

func TestWithClientIPMatcher(t *testing.T) {
	cases := []struct {
		name    string
		ip      string
		wantErr bool
	}{
		{
			name:    "valid CIDR",
			ip:      "192.168.1.0/24",
			wantErr: false,
		},
		{
			name:    "valid single IP",
			ip:      "192.168.1.1",
			wantErr: false,
		},
		{
			name:    "valid IPv6 CIDR",
			ip:      "2001:db8::/32",
			wantErr: false,
		},
		{
			name:    "invalid IP",
			ip:      "invalid",
			wantErr: true,
		},
		{
			name:    "empty IP",
			ip:      "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NewRouter()
			require.NoError(t, err)
			err = onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithClientIPMatcher(tc.ip)))
			if tc.wantErr {
				assert.ErrorIs(t, err, ErrInvalidMatcher)
				return
			}
			require.NoError(t, err)
			m, _ := MatchClientIP(tc.ip)
			assert.True(t, f.Has(MethodGet, "/foo", m))
		})
	}
}

func TestWithSchemeMatcher(t *testing.T) {
	cases := []struct {
		name    string
		scheme  string
		wantErr bool
	}{
		{
			name:    "valid http",
			scheme:  "http",
			wantErr: false,
		},
		{
			name:    "valid https",
			scheme:  "https",
			wantErr: false,
		},
		{
			name:    "uppercase HTTPS",
			scheme:  "HTTPS",
			wantErr: false,
		},
		{
			name:    "invalid scheme",
			scheme:  "ws",
			wantErr: true,
		},
		{
			name:    "empty scheme",
			scheme:  "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NewRouter()
			require.NoError(t, err)
			err = onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithSchemeMatcher(tc.scheme)))
			if tc.wantErr {
				assert.ErrorIs(t, err, ErrInvalidMatcher)
				return
			}
			require.NoError(t, err)
			m, _ := MatchScheme(tc.scheme)
			assert.True(t, f.Has(MethodGet, "/foo", m))
		})
	}
}

func TestWithMatcher(t *testing.T) {
	t.Run("valid custom matcher", func(t *testing.T) {
		f, err := NewRouter()
		require.NoError(t, err)
		m, _ := MatchQuery("foo", "bar")
		err = onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithMatcher(m)))
		require.NoError(t, err)
		assert.True(t, f.Has(MethodGet, "/foo", m))
	})

	t.Run("nil matcher", func(t *testing.T) {
		f, err := NewRouter()
		require.NoError(t, err)
		err = onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithMatcher(nil)))
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})

	t.Run("multiple matchers with one nil", func(t *testing.T) {
		f, err := NewRouter()
		require.NoError(t, err)
		m, _ := MatchQuery("foo", "bar")
		err = onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithMatcher(m, nil)))
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})

	t.Run("nil matcher on delete", func(t *testing.T) {
		f, err := NewRouter()
		require.NoError(t, err)
		f.MustAdd(MethodGet, "/foo", emptyHandler)
		_, err = f.Delete(MethodGet, "/foo", WithMatcher(nil))
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
}

func TestWithMaxRegexpSize(t *testing.T) {
	f := MustRouter(WithAllowRegexpParam(true), WithMaxRegexpSize(4))
	require.NoError(t, onlyError(f.Add(MethodGet, "/foo/{a:[0-9]+}", emptyHandler)))
	assert.Equal(t, 4, f.Info().MaxRegexpSize)
}

func TestWithMaxRegexpSize_RejectBeforeCompile(t *testing.T) {
	f := MustRouter(WithAllowRegexpParam(true))
	expr := strings.Repeat("a{1000}", 3000)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := f.NewRoute(MethodGet, "/{a:"+expr+"}", emptyHandler)
	_, merr := MatchHeaderRegexp("X", expr)
	runtime.ReadMemStats(&after)

	assert.Error(t, err)
	assert.Error(t, merr)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(16<<20))
}
