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
