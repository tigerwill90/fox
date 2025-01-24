package fox

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
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
