package fox

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoute_HandleMiddlewareMalloc(t *testing.T) {
	f, _ := NewRouter()
	for _, rte := range githubAPI {
		require.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler)))
	}

	for _, rte := range githubAPI {
		req := httptest.NewRequest(rte.method, rte.path, nil)
		w := httptest.NewRecorder()
		r, c, _ := f.Lookup(&recorder{ResponseWriter: w}, req)
		allocs := testing.AllocsPerRun(100, func() {
			r.HandleMiddleware(c)
		})
		c.Close()
		assert.Equal(t, float64(0), allocs)
	}
}

func TestRoute_HostnamePath(t *testing.T) {
	cases := []struct {
		name     string
		pattern  string
		wantPath string
		wantHost string
	}{
		{
			name:     "only path",
			pattern:  "/foo/bar",
			wantPath: "/foo/bar",
		},
		{
			name:     "only slash",
			pattern:  "/",
			wantPath: "/",
		},
		{
			name:     "host and path",
			pattern:  "a.b.c/foo/bar",
			wantPath: "/foo/bar",
			wantHost: "a.b.c",
		},
		{
			name:     "host and slash",
			pattern:  "a.b.c/",
			wantPath: "/",
			wantHost: "a.b.c",
		},
		{
			name:     "single letter host and slash",
			pattern:  "a/",
			wantPath: "/",
			wantHost: "a",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter()
			r, err := f.Add(MethodGet, tc.pattern, emptyHandler)
			require.NoError(t, err)
			assert.Equal(t, tc.wantHost, r.Hostname())
			assert.Equal(t, tc.wantPath, r.Path())
		})
	}
}

func TestRoute_HostnameParamsLen(t *testing.T) {
	cases := []struct {
		name          string
		pattern       string
		wantParams    []string
		wantHostCount int
	}{
		{
			name:    "only static path",
			pattern: "/foo/bar",
		},
		{
			name:       "only path params",
			pattern:    "/users/{id}/files/+{path}",
			wantParams: []string{"id", "path"},
		},
		{
			name:       "static host and path params",
			pattern:    "a.b.c/users/{id}",
			wantParams: []string{"id"},
		},
		{
			name:          "host param and slash",
			pattern:       "{sub}.example.com/",
			wantParams:    []string{"sub"},
			wantHostCount: 1,
		},
		{
			name:          "host and path params",
			pattern:       "{a}.{b}.example.com/users/{id}",
			wantParams:    []string{"a", "b", "id"},
			wantHostCount: 2,
		},
		{
			name:          "host and path wildcards",
			pattern:       "+{sub}.example.com/files/+{path}",
			wantParams:    []string{"sub", "path"},
			wantHostCount: 1,
		},
		{
			name:          "regex host param",
			pattern:       "{sub:[a-z]+}.example.com/{id:[0-9]+}",
			wantParams:    []string{"sub", "id"},
			wantHostCount: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true))
			r, err := f.Add(MethodGet, tc.pattern, emptyHandler)
			require.NoError(t, err)
			assert.Equal(t, tc.wantParams, slices.Collect(r.Params()))
			assert.Equal(t, tc.wantHostCount, r.HostnameParamsLen())
		})
	}
}

func TestRoute_Methods(t *testing.T) {
	f := MustRouter()
	f.MustAdd([]string{http.MethodHead, http.MethodOptions, http.MethodGet}, "/foo/bar", emptyHandler)

	route := f.Route([]string{http.MethodOptions, http.MethodHead, http.MethodGet}, "/foo/bar")
	assert.Equal(t, []string{http.MethodGet, http.MethodHead, http.MethodOptions}, slices.Collect(route.Methods()))
}

func TestRoute_String(t *testing.T) {
	t.Run("many methods + name + many matchers", func(t *testing.T) {
		f := MustRouter()
		r := f.MustAdd(
			[]string{http.MethodGet, http.MethodHead}, "/foo/bar",
			emptyHandler,
			WithName("foo"),
			WithQueryMatcher("a", "b"),
			WithHeaderMatcher("a", "b"),
		)
		assert.Equal(t, "method:GET,HEAD pattern:/foo/bar name:foo matchers:{q:a=b,h:A=b}", r.String())
	})
	t.Run("single method + name + single matchers", func(t *testing.T) {
		f := MustRouter()
		r := f.MustAdd(
			[]string{http.MethodGet}, "/foo/bar",
			emptyHandler,
			WithName("foo"),
			WithQueryMatcher("a", "b"),
		)
		assert.Equal(t, "method:GET pattern:/foo/bar name:foo matchers:{q:a=b}", r.String())
	})
	t.Run("no method + pattern", func(t *testing.T) {
		f := MustRouter()
		r := f.MustAdd(
			MethodAny, "/foo/bar",
			emptyHandler,
		)
		assert.Equal(t, "method:* pattern:/foo/bar", r.String())
	})
}

func TestRoute_Middleware(t *testing.T) {
	var c0, c1, c2 bool
	m0 := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			c0 = true
			next(c)
		}
	})

	m1 := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			c1 = true
			next(c)
		}
	})

	m2 := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			c2 = true
			next(c)
		}
	})
	f, err := NewRouter(WithMiddleware(m0))
	require.NoError(t, err)
	f.MustAdd(MethodGet, "/1", emptyHandler, WithMiddleware(m1))
	f.MustAdd(MethodGet, "/2", emptyHandler, WithMiddleware(m2))

	req := httptest.NewRequest(http.MethodGet, "/1", nil)
	w := httptest.NewRecorder()

	f.ServeHTTP(w, req)
	assert.True(t, c0)
	assert.True(t, c1)
	assert.False(t, c2)
	c0, c1, c2 = false, false, false

	req.URL.Path = "/2"
	f.ServeHTTP(w, req)
	assert.True(t, c0)
	assert.False(t, c1)
	assert.True(t, c2)

	c0, c1, c2 = false, false, false
	rte1 := f.Route(MethodGet, "/1")
	require.NotNil(t, rte1)
	rte1.Handle(newTestContext(f))
	assert.False(t, c0)
	assert.False(t, c1)
	assert.False(t, c2)
	c0, c1, c2 = false, false, false

	rte1.HandleMiddleware(newTestContext(f))
	assert.False(t, c0)
	assert.True(t, c1)
	assert.False(t, c2)
	c0, c1, c2 = false, false, false

	rte2 := f.Route(MethodGet, "/2")
	require.NotNil(t, rte2)
	rte2.HandleMiddleware(newTestContext(f))
	assert.False(t, c0)
	assert.False(t, c1)
	assert.True(t, c2)
}
