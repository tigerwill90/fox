package fox

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestRoute_HydrateParams(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		route      *Route
		wantParams Params
		want       bool
	}{
		{
			name:       "static route match",
			path:       "/foo/bar",
			route:      &Route{path: "/foo/bar"},
			wantParams: Params{},
			want:       true,
		},
		{
			name:       "static route do not match",
			path:       "/foo/bar",
			route:      &Route{path: "/foo/ba"},
			wantParams: Params{},
			want:       false,
		},
		{
			name:       "static route do not match",
			path:       "/foo/bar",
			route:      &Route{path: "/foo/barr"},
			wantParams: Params{},
			want:       false,
		},
		{
			name:       "static route do not match",
			path:       "/foo/bar",
			route:      &Route{path: "/foo/bax"},
			wantParams: Params{},
			want:       false,
		},
		{
			name:       "strict trailing slash",
			path:       "/foo/bar",
			route:      &Route{path: "/foo/bar/"},
			wantParams: Params{},
			want:       false,
		},
		{
			name:  "strict trailing slash with param and",
			path:  "/foo/bar",
			route: &Route{path: "/foo/{1}/"},
			wantParams: Params{
				{
					Key:   "1",
					Value: "bar",
				},
			},
			want: false,
		},
		{
			name:  "strict trailing slash with param",
			path:  "/foo/bar/",
			route: &Route{path: "/foo/{2}"},
			wantParams: Params{
				{
					Key:   "2",
					Value: "bar",
				},
			},
			want: false,
		},
		{
			name:       "strict trailing slash",
			path:       "/foo/bar/",
			route:      &Route{path: "/foo/bar"},
			wantParams: Params{},
			want:       false,
		},
		{
			name:  "multi route params and catch all",
			path:  "/foo/ab:1/baz/123/y/bo/lo",
			route: &Route{path: "/foo/ab:{bar}/baz/{x}/{y}/*{zo}"},
			wantParams: Params{
				{
					Key:   "bar",
					Value: "1",
				},
				{
					Key:   "x",
					Value: "123",
				},
				{
					Key:   "y",
					Value: "y",
				},
				{
					Key:   "zo",
					Value: "bo/lo",
				},
			},
			want: true,
		},
		{
			name:  "path with wildcard should be parsed",
			path:  "/foo/ab:{bar}/baz/{x}/{y}/*{zo}",
			route: &Route{path: "/foo/ab:{bar}/baz/{x}/{y}/*{zo}"},
			wantParams: Params{
				{
					Key:   "bar",
					Value: "{bar}",
				},
				{
					Key:   "x",
					Value: "{x}",
				},
				{
					Key:   "y",
					Value: "{y}",
				},
				{
					Key:   "zo",
					Value: "*{zo}",
				},
			},
			want: true,
		},
		{
			name:       "empty param end range",
			path:       "/foo/",
			route:      &Route{path: "/foo/{bar}"},
			wantParams: Params{},
			want:       false,
		},
		{
			name:       "empty param mid range",
			path:       "/foo//baz",
			route:      &Route{path: "/foo/{bar}/baz"},
			wantParams: Params{},
			want:       false,
		},
		{
			name:  "multiple slash",
			path:  "/foo/bar///baz",
			route: &Route{path: "/foo/{bar}/baz"},
			wantParams: Params{
				{
					Key:   "bar",
					Value: "bar",
				},
			},
			want: false,
		},
		{
			name:  "param at end range",
			path:  "/foo/baz",
			route: &Route{path: "/foo/{bar}"},
			wantParams: Params{
				{
					Key:   "bar",
					Value: "baz",
				},
			},
			want: true,
		},
		{
			name:  "full path catch all",
			path:  "/foo/bar/baz",
			route: &Route{path: "/*{args}"},
			wantParams: Params{
				{
					Key:   "args",
					Value: "foo/bar/baz",
				},
			},
			want: true,
		},
	}

	params := make(Params, 0)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params = params[:0]
			got := tc.route.hydrateParams(tc.path, &params)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantParams, params)
		})
	}

}

func TestRoute_HandleMiddlewareMalloc(t *testing.T) {
	f := New()
	for _, rte := range githubAPI {
		require.NoError(t, onlyError(f.Tree().Handle(rte.method, rte.path, emptyHandler)))
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

func TestRoute_HydrateParamsMalloc(t *testing.T) {
	rte := &Route{
		path: "/foo/ab:{bar}/baz/{x}/{y}/*{zo}",
	}
	path := "/foo/ab:1/baz/123/y/bo/lo"
	params := make(Params, 0, 4)

	allocs := testing.AllocsPerRun(100, func() {
		rte.hydrateParams(path, &params)
		params = params[:0]
	})
	assert.Equal(t, float64(0), allocs)
}
