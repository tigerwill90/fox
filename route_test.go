package fox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoute_HandleMiddlewareMalloc(t *testing.T) {
	f, _ := New()
	for _, rte := range githubAPI {
		require.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
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
			f, _ := New()
			r, err := f.Handle(http.MethodGet, tc.pattern, emptyHandler)
			require.NoError(t, err)
			assert.Equal(t, tc.wantHost, r.Hostname())
			assert.Equal(t, tc.wantPath, r.Path())
		})
	}
}

func TestRoute_Equal(t *testing.T) {

	f, err := New()
	require.NoError(t, err)
	should := valueOrFail[*Route](t)

	cases := []struct {
		name string
		r1   *Route
		r2   *Route
		want bool
	}{
		{
			name: "r1 equal r2 without matchers",
			r1:   should(f.NewRoute("example.com/foo", emptyHandler)),
			r2:   should(f.NewRoute("example.com/foo", emptyHandler)),
			want: true,
		},
		{
			name: "r1 not equal r2 without matchers",
			r1:   should(f.NewRoute("example.com/foo", emptyHandler)),
			r2:   should(f.NewRoute("example.com/bar", emptyHandler)),
			want: false,
		},
		{
			name: "r1 equal r2 with ordered matchers",
			r1: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("c", "d"),
				WithHeaderMatcher("e", "f"),
			)),
			r2: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("c", "d"),
				WithHeaderMatcher("e", "f"),
			)),
			want: true,
		},
		{
			name: "r1 equal r2 with unordered matchers",
			r1: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("c", "d"),
				WithHeaderMatcher("e", "f"),
			)),
			r2: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("c", "d"),
				WithHeaderMatcher("e", "f"),
				WithQueryMatcher("a", "b"),
			)),
			want: true,
		},
		{
			name: "r1 equal r2 with duplicated matchers",
			r1: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("e", "f"),
			)),
			r2: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("e", "f"),
			)),
			want: true,
		},
		{
			name: "r1 equal r2 with duplicated unordered matchers",
			r1: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("e", "f"),
			)),
			r2: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("e", "f"),
				WithQueryMatcher("a", "b"),
			)),
			want: true,
		},
		{
			name: "r1 not equal r2 with matchers",
			r1: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("c", "d"),
				WithHeaderMatcher("e", "f"),
			)),
			r2: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("x", "y"),
				WithHeaderMatcher("e", "f"),
			)),
			want: false,
		},
		{
			name: "r1 not equal r2 with duplicated matchers",
			r1: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("e", "f"),
			)),
			r2: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("e", "f"),
				WithQueryMatcher("e", "f"),
			)),
			want: false,
		},
		{
			name: "r1 not equal r2 with duplicated unordered matchers",
			r1: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("e", "f"),
			)),
			r2: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("e", "f"),
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("e", "f"),
			)),
			want: false,
		},
		{
			name: "r1 not equal r2 with different matchers length",
			r1: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("e", "f"),
			)),
			r2: should(f.NewRoute(
				"example.com/foo",
				emptyHandler,
				WithQueryMatcher("a", "b"),
				WithQueryMatcher("e", "f"),
				WithQueryMatcher("e", "f"),
			)),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.r1.Equal(tc.r2))
		})
	}
}
