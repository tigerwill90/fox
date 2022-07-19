package fox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	fuzz "github.com/google/gofuzz"
	"github.com/julienschmidt/httprouter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockResponseWriter struct{}

func (m *mockResponseWriter) Header() (h http.Header) {
	return http.Header{}
}

func (m *mockResponseWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (m *mockResponseWriter) WriteString(s string) (n int, err error) {
	return len(s), nil
}

func (m *mockResponseWriter) WriteHeader(int) {}

type route struct {
	method string
	path   string
}

var staticRoutes = []route{
	{"GET", "/"},
	{"GET", "/cmd.html"},
	{"GET", "/code.html"},
	{"GET", "/contrib.html"},
	{"GET", "/contribute.html"},
	{"GET", "/debugging_with_gdb.html"},
	{"GET", "/docs.html"},
	{"GET", "/effective_go.html"},
	{"GET", "/files.log"},
	{"GET", "/gccgo_contribute.html"},
	{"GET", "/gccgo_install.html"},
	{"GET", "/go-logo-black.png"},
	{"GET", "/go-logo-blue.png"},
	{"GET", "/go-logo-white.png"},
	{"GET", "/go1.1.html"},
	{"GET", "/go1.2.html"},
	{"GET", "/go1.html"},
	{"GET", "/go1compat.html"},
	{"GET", "/go_faq.html"},
	{"GET", "/go_mem.html"},
	{"GET", "/go_spec.html"},
	{"GET", "/help.html"},
	{"GET", "/ie.css"},
	{"GET", "/install-source.html"},
	{"GET", "/install.html"},
	{"GET", "/logo-153x55.png"},
	{"GET", "/Makefile"},
	{"GET", "/root.html"},
	{"GET", "/share.png"},
	{"GET", "/sieve.gif"},
	{"GET", "/tos.html"},
	{"GET", "/articles"},
	{"GET", "/articles/go_command.html"},
	{"GET", "/articles/index.html"},
	{"GET", "/articles/wiki"},
	{"GET", "/articles/wiki/edit.html"},
	{"GET", "/articles/wiki/final-noclosure.go"},
	{"GET", "/articles/wiki/final-noerror.go"},
	{"GET", "/articles/wiki/final-parsetemplate.go"},
	{"GET", "/articles/wiki/final-template.go"},
	{"GET", "/articles/wiki/final.go"},
	{"GET", "/articles/wiki/get.go"},
	{"GET", "/articles/wiki/http-sample.go"},
	{"GET", "/articles/wiki/index.html"},
	{"GET", "/articles/wiki/Makefile"},
	{"GET", "/articles/wiki/notemplate.go"},
	{"GET", "/articles/wiki/part1-noerror.go"},
	{"GET", "/articles/wiki/part1.go"},
	{"GET", "/articles/wiki/part2.go"},
	{"GET", "/iptv-sfr"},
	{"GET", "/articles/wiki/part3.go"},
	{"GET", "/articles/wiki/test.bash"},
	{"GET", "/articles/wiki/test_edit.good"},
	{"GET", "/articles/wiki/test_Test.txt.good"},
	{"GET", "/articles/wiki/test_view.good"},
	{"GET", "/articles/wiki/view.html"},
	{"GET", "/codewalk"},
	{"GET", "/codewalk/codewalk.css"},
	{"GET", "/codewalk/codewalk.js"},
	{"GET", "/codewalk/codewalk.xml"},
	{"GET", "/codewalk/functions.xml"},
	{"GET", "/codewalk/markov.go"},
	{"GET", "/codewalk/markov.xml"},
	{"GET", "/codewalk/pig.go"},
	{"GET", "/codewalk/popout.png"},
	{"GET", "/codewalk/run"},
	{"GET", "/codewalk/sharemem.xml"},
	{"GET", "/codewalk/urlpoll.go"},
	{"GET", "/devel"},
	{"GET", "/devel/release.html"},
	{"GET", "/devel/weekly.html"},
	{"GET", "/gopher"},
	{"GET", "/gopher/appenginegopher.jpg"},
	{"GET", "/gopher/appenginegophercolor.jpg"},
	{"GET", "/gopher/appenginelogo.gif"},
	{"GET", "/gopher/bumper.png"},
	{"GET", "/gopher/bumper192x108.png"},
	{"GET", "/gopher/bumper320x180.png"},
	{"GET", "/gopher/bumper480x270.png"},
	{"GET", "/gopher/bumper640x360.png"},
	{"GET", "/gopher/doc.png"},
	{"GET", "/gopher/frontpage.png"},
	{"GET", "/gopher/gopherbw.png"},
	{"GET", "/gopher/gophercolor.png"},
	{"GET", "/gopher/gophercolor16x16.png"},
	{"GET", "/gopher/help.png"},
	{"GET", "/gopher/pkg.png"},
	{"GET", "/gopher/project.png"},
	{"GET", "/gopher/ref.png"},
	{"GET", "/gopher/run.png"},
	{"GET", "/gopher/talks.png"},
	{"GET", "/gopher/pencil"},
	{"GET", "/gopher/pencil/gopherhat.jpg"},
	{"GET", "/gopher/pencil/gopherhelmet.jpg"},
	{"GET", "/gopher/pencil/gophermega.jpg"},
	{"GET", "/gopher/pencil/gopherrunning.jpg"},
	{"GET", "/gopher/pencil/gopherswim.jpg"},
	{"GET", "/gopher/pencil/gopherswrench.jpg"},
	{"GET", "/play"},
	{"GET", "/play/fib.go"},
	{"GET", "/play/hello.go"},
	{"GET", "/play/life.go"},
	{"GET", "/play/peano.go"},
	{"GET", "/play/pi.go"},
	{"GET", "/play/sieve.go"},
	{"GET", "/play/solitaire.go"},
	{"GET", "/play/tree.go"},
	{"GET", "/progs"},
	{"GET", "/progs/cgo1.go"},
	{"GET", "/progs/cgo2.go"},
	{"GET", "/progs/cgo3.go"},
	{"GET", "/progs/cgo4.go"},
	{"GET", "/progs/defer.go"},
	{"GET", "/progs/defer.out"},
	{"GET", "/progs/defer2.go"},
	{"GET", "/progs/defer2.out"},
	{"GET", "/progs/eff_bytesize.go"},
	{"GET", "/progs/eff_bytesize.out"},
	{"GET", "/progs/eff_qr.go"},
	{"GET", "/progs/eff_sequence.go"},
	{"GET", "/progs/eff_sequence.out"},
	{"GET", "/progs/eff_unused1.go"},
	{"GET", "/progs/eff_unused2.go"},
	{"GET", "/progs/error.go"},
	{"GET", "/progs/error2.go"},
	{"GET", "/progs/error3.go"},
	{"GET", "/progs/error4.go"},
	{"GET", "/progs/go1.go"},
	{"GET", "/progs/gobs1.go"},
	{"GET", "/progs/gobs2.go"},
	{"GET", "/progs/image_draw.go"},
	{"GET", "/progs/image_package1.go"},
	{"GET", "/progs/image_package1.out"},
	{"GET", "/progs/image_package2.go"},
	{"GET", "/progs/image_package2.out"},
	{"GET", "/progs/image_package3.go"},
	{"GET", "/progs/image_package3.out"},
	{"GET", "/progs/image_package4.go"},
	{"GET", "/progs/image_package4.out"},
	{"GET", "/progs/image_package5.go"},
	{"GET", "/progs/image_package5.out"},
	{"GET", "/progs/image_package6.go"},
	{"GET", "/progs/image_package6.out"},
	{"GET", "/progs/interface.go"},
	{"GET", "/progs/interface2.go"},
	{"GET", "/progs/interface2.out"},
	{"GET", "/progs/json1.go"},
	{"GET", "/progs/json2.go"},
	{"GET", "/progs/json2.out"},
	{"GET", "/progs/json3.go"},
	{"GET", "/progs/json4.go"},
	{"GET", "/progs/json5.go"},
	{"GET", "/progs/run"},
	{"GET", "/progs/slices.go"},
	{"GET", "/progs/timeout1.go"},
	{"GET", "/progs/timeout2.go"},
	{"GET", "/progs/update.bash"},
}

func benchRoutes(b *testing.B, router http.Handler, routes []route) {
	w := new(mockResponseWriter)
	r, _ := http.NewRequest("GET", "/", nil)
	u := r.URL
	rq := u.RawQuery

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, route := range routes {
			r.Method = route.method
			r.RequestURI = route.path
			u.Path = route.path
			u.RawQuery = rq
			router.ServeHTTP(w, r)
		}
	}
}

func benchRouteParallel(b *testing.B, router http.Handler, rte route) {
	w := new(mockResponseWriter)
	r, _ := http.NewRequest(rte.method, rte.path, nil)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			router.ServeHTTP(w, r)
		}
	})
}

// BenchmarkMuxRouter-16    	  106693	     10751 ns/op	       0 B/op	       0 allocs/op
func BenchmarkMuxRouter(b *testing.B) {
	r := New()
	for _, route := range staticRoutes {
		require.NoError(b, r.Get(route.path, HandlerFunc(func(w http.ResponseWriter, r *http.Request, p Params) {})))
	}
	benchRoutes(b, r, staticRoutes)
}

func BenchmarkHttpRouterRouter(b *testing.B) {
	r := httprouter.New()
	for _, route := range staticRoutes {
		r.GET(route.path, func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {})
	}

	benchRoutes(b, r, staticRoutes)
}

// BenchmarkMuxRouterParallel-16    	143326886	         8.252 ns/op	       0 B/op	       0 allocs/op
func BenchmarkMuxRouterParallel(b *testing.B) {
	r := New()
	for _, route := range staticRoutes {
		require.NoError(b, r.Get(route.path, HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {})))
	}
	benchRouteParallel(b, r, route{"GET", "/progs/image_package4.out"})
}

func BenchmarkRouterMuxCatchAll(b *testing.B) {
	r := New()
	require.NoError(b, r.Get("/something/*args", HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {})))
	w := new(mockResponseWriter)
	req, _ := http.NewRequest("GET", "/something/awesome", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkRouterMuxParallelCatchAll(b *testing.B) {
	r := New()
	require.NoError(b, r.Get("/something/*args", HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {})))
	w := new(mockResponseWriter)
	req, _ := http.NewRequest("GET", "/something/awesome", nil)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r.ServeHTTP(w, req)
		}
	})
}

// TODO remove this benchmark
func BenchmarkGinRouter(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	for _, route := range staticRoutes {
		r.GET(route.path, func(context *gin.Context) {})
	}
	benchRoutes(b, r, staticRoutes)
}

// TODO remove this benchmark
func BenchmarkGinRouterParallel(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	for _, route := range staticRoutes {
		r.GET(route.path, func(context *gin.Context) {})
	}
	benchRouteParallel(b, r, route{"GET", "/progs/image_package4.out"})
}

func BenchmarkGinRouterCatchAll(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/something/*args", func(context *gin.Context) {})
	w := new(mockResponseWriter)
	req, _ := http.NewRequest("GET", "/something/awesome", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkGinRouterParallelCatchAll(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/something/*args", func(context *gin.Context) {})
	w := new(mockResponseWriter)
	req, _ := http.NewRequest("GET", "/something/awesome", nil)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r.ServeHTTP(w, req)
		}
	})
}

func TestMuxRouterStatic(t *testing.T) {
	r := New()
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) { w.Write([]byte(r.URL.Path)) })

	for _, route := range staticRoutes {
		require.NoError(t, r.Get(route.path, h))
	}

	for _, route := range staticRoutes {
		req, _ := http.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}
}

func TestMuxRouterWildcard(t *testing.T) {
	r := New()
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) { w.Write([]byte(r.URL.Path)) })

	routes := []struct {
		path string
		key  string
	}{
		{"/github.com/etf1/*repo", "/github.com/etf1/mux"},
		{"/github.com/johndoe/*repo", "/github.com/johndoe/buzz"},
		{"/foo/bar/*args", "/foo/bar/"},
	}

	for _, route := range routes {
		require.NoError(t, r.Get(route.path, h))
	}

	for _, route := range routes {
		req, _ := http.NewRequest(http.MethodGet, route.key, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "route: key: %s, path: %s", route.path)
		assert.Equal(t, route.key, w.Body.String())
	}
}

func TestMuxRouterInsertWildcardConflict(t *testing.T) {
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {})
	cases := []struct {
		name   string
		routes []struct {
			path      string
			wildcard  bool
			wantErr   error
			wantMatch []string
		}
	}{
		{
			name: "key mid edge conflicts",
			routes: []struct {
				path      string
				wildcard  bool
				wantErr   error
				wantMatch []string
			}{
				{path: "/foo/bar", wildcard: false, wantErr: nil, wantMatch: nil},
				{path: "/foo/baz", wildcard: false, wantErr: nil, wantMatch: nil},
				{path: "/foo/", wildcard: true, wantErr: ErrRouteConflict, wantMatch: []string{"/foo/bar", "/foo/baz"}},
				{path: "/foo/bar/baz/", wildcard: true, wantErr: nil},
				{path: "/foo/bar/", wildcard: true, wantErr: ErrRouteConflict, wantMatch: []string{"/foo/bar/baz/*"}},
			},
		},
		{
			name: "incomplete match to the end of edge conflict",
			routes: []struct {
				path      string
				wildcard  bool
				wantErr   error
				wantMatch []string
			}{
				{path: "/foo/", wildcard: true, wantErr: nil, wantMatch: nil},
				{path: "/foo/bar", wildcard: false, wantErr: ErrRouteConflict, wantMatch: []string{"/foo/*"}},
				{path: "/fuzz/baz/bar/", wildcard: true, wantErr: nil, wantMatch: nil},
				{path: "/fuzz/baz/bar/foo", wildcard: false, wantErr: ErrRouteConflict, wantMatch: []string{"/fuzz/baz/bar/*"}},
			},
		},
		{
			name: "exact match conflict",
			routes: []struct {
				path      string
				wildcard  bool
				wantErr   error
				wantMatch []string
			}{
				{path: "/foo/1", wildcard: false, wantErr: nil, wantMatch: nil},
				{path: "/foo/2", wildcard: false, wantErr: nil, wantMatch: nil},
				{path: "/foo/", wildcard: true, wantErr: ErrRouteConflict, wantMatch: []string{"/foo/1", "/foo/2"}},
				{path: "/foo/1/baz/1", wildcard: false, wantErr: nil, wantMatch: nil},
				{path: "/foo/1/baz/2", wildcard: false, wantErr: nil, wantMatch: nil},
				{path: "/foo/1/baz/", wildcard: true, wantErr: ErrRouteConflict, wantMatch: []string{"/foo/1/baz/1", "/foo/1/baz/2"}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			for _, rte := range tc.routes {
				err := r.insert(http.MethodGet, rte.path, "", h, rte.wildcard)
				assert.ErrorIs(t, err, rte.wantErr)
				if cErr, ok := err.(*ConflictError); ok {
					assert.Equal(t, rte.wantMatch, cErr.matching)
				}
			}
		})
	}
}

func TestMuxRouterSwapWildcardConflict(t *testing.T) {
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {})
	cases := []struct {
		name   string
		routes []struct {
			path     string
			wildcard bool
		}
		path      string
		wildcard  bool
		wantErr   error
		wantMatch []string
	}{
		{
			name: "replace existing node with wildcard",
			routes: []struct {
				path     string
				wildcard bool
			}{
				{path: "/foo/bar", wildcard: false},
				{path: "/foo/baz", wildcard: false},
				{path: "/foo/", wildcard: false},
			},
			path:      "/foo/",
			wildcard:  true,
			wantErr:   ErrRouteConflict,
			wantMatch: []string{"/foo/bar", "/foo/baz"},
		},
		{
			name: "replace existing wildcard node with static",
			routes: []struct {
				path     string
				wildcard bool
			}{
				{path: "/foo/", wildcard: true},
			},
			path: "/foo/",
		},
		{
			name: "replace existing wildcard node with another wildcard",
			routes: []struct {
				path     string
				wildcard bool
			}{
				{path: "/foo/", wildcard: true},
			},
			path:     "/foo/",
			wildcard: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			for _, rte := range tc.routes {
				require.NoError(t, r.insert(http.MethodGet, rte.path, "", h, rte.wildcard))
			}
			err := r.update(http.MethodGet, tc.path, "", h, true)
			assert.ErrorIs(t, err, tc.wantErr)
			if cErr, ok := err.(*ConflictError); ok {
				assert.Equal(t, tc.wantMatch, cErr.matching)
			}
		})
	}
}

func TestMuxRouerUpdateRoute(t *testing.T) {
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {
		w.Write([]byte(r.URL.Path))
	})

	cases := []struct {
		name           string
		path           string
		newPath        string
		newWildcardKey string
		newHandler     Handler
	}{
		{
			name:           "update wildcard with another wildcard",
			path:           "/foo/bar/*args",
			newPath:        "/foo/bar/",
			newWildcardKey: "*new",
			newHandler: HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {
				w.Write([]byte(params.Get(ParamRouteKey)))
			}),
		},
		{
			name:    "update wildcard with non wildcard",
			path:    "/foo/bar/*args",
			newPath: "/foo/bar/",
			newHandler: HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {
				w.Write([]byte(r.URL.Path))
			}),
		},
		{
			name:           "update non wildcard with wildcard",
			path:           "/foo/bar/",
			newPath:        "/foo/bar/",
			newWildcardKey: "*foo",
			newHandler: HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {
				w.Write([]byte(params.Get(ParamRouteKey)))
			}),
		},
		{
			name:    "update non wildcard with non wildcard",
			path:    "/foo/bar",
			newPath: "/foo/bar",
			newHandler: HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {
				w.Write([]byte(r.URL.Path))
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			r.AddRouteParam = true
			require.NoError(t, r.Get(tc.path, h))
			require.NoError(t, r.Update(http.MethodGet, tc.newPath+tc.newWildcardKey, tc.newHandler))
			req, _ := http.NewRequest(http.MethodGet, tc.newPath, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tc.newPath+tc.newWildcardKey, w.Body.String())
		})
	}
}

func TestMuxRouterUpsert(t *testing.T) {
	old := HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {})
	new := HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) { w.Write([]byte("new")) })

	r := New()
	require.NoError(t, r.Post("/foo/bar", old))
	require.NoError(t, r.Post("/foo/", old))

	cases := []struct {
		name    string
		path    string
		wantErr error
	}{
		{
			name: "upsert an existing route with no conflict",
			path: "/foo/bar",
		},
		{
			name: "upsert a new route",
			path: "/fizz/buzz",
		},
		{
			name:    "upsert an existing route with wildcard conflict",
			path:    "/foo/*args",
			wantErr: ErrRouteConflict,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := r.Upsert(http.MethodPost, tc.path, new)
			assert.ErrorIs(t, err, tc.wantErr)
			if err == nil {
				req, _ := http.NewRequest(http.MethodPost, tc.path, nil)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				assert.Equal(t, "new", w.Body.String())
			}
		})
	}

}

func TestMuxRouterParseRoute(t *testing.T) {
	cases := []struct {
		name         string
		path         string
		wantErr      error
		wantRoute    string
		wantWildcard bool
	}{
		{
			name:      "valid static route",
			path:      "/foo/bar",
			wantRoute: "/foo/bar",
		},
		{
			name:         "valid wildcard route",
			path:         "/foo/bar/*arg",
			wantRoute:    "/foo/bar/",
			wantWildcard: true,
		},
		{
			name:    "missing prefix slash",
			path:    "foo/bar",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "missing slash before wildcard",
			path:    "/foo/bar*",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "missing arguments name after wildcard",
			path:    "/foo/bar/*",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "catch all in the middle of the route",
			path:    "/foo/bar/*/baz",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "catch all with args in the middle of the route",
			path:    "/foo/bar/*args/baz",
			wantErr: ErrInvalidRoute,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, wildcard, err := parseRoute(tc.path)
			require.ErrorIs(t, err, tc.wantErr)
			if err == nil {
				assert.Equal(t, tc.wantRoute, tc.path[:idx])
				assert.Equal(t, tc.wantWildcard, wildcard)
			}
		})
	}
}

func TestMuxLookupTsr(t *testing.T) {
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {})

	cases := []struct {
		name string
		path string
		key  string
		want bool
	}{
		{
			name: "match mid edge",
			path: "/foo/bar/",
			key:  "/foo/bar",
			want: true,
		},
		{
			name: "incomplete match end of edge",
			path: "/foo/bar",
			key:  "/foo/bar/",
			want: true,
		},
		{
			name: "match mid edge with ts and more char after",
			path: "/foo/bar/buzz",
			key:  "/foo/bar",
		},
		{
			name: "match mid edge with ts and more char before",
			path: "/foo/barr/",
			key:  "/foo/bar",
		},
		{
			name: "incomplete match end of edge with ts and more char after",
			path: "/foo/bar",
			key:  "/foo/bar/buzz",
		},
		{
			name: "incomplete match end of edge with ts and more char before",
			path: "/foo/bar",
			key:  "/foo/barr/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			require.NoError(t, r.insert(http.MethodGet, tc.path, "", h, false))
			nds := *r.trees.Load()
			_, _, got := r.lookup(nds[0], tc.key, true)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMuxRouterRedirectTrailingSlash(t *testing.T) {
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {})

	cases := []struct {
		name   string
		path   string
		key    string
		method string
		want   int
	}{
		{
			name:   "mid edge key with get method and status moved permanently",
			path:   "/foo/bar/",
			key:    "/foo/bar",
			method: http.MethodGet,
			want:   http.StatusMovedPermanently,
		},
		{
			name:   "mid edge key with post method and status permanent redirect",
			path:   "/foo/bar/",
			key:    "/foo/bar",
			method: http.MethodPost,
			want:   http.StatusPermanentRedirect,
		},
		{
			name:   "incomplete match end of edge",
			path:   "/foo/bar",
			key:    "/foo/bar/",
			method: http.MethodGet,
			want:   http.StatusMovedPermanently,
		},
		{
			name:   "incomplete match end of edge",
			path:   "/foo/bar",
			key:    "/foo/bar/",
			method: http.MethodPost,
			want:   http.StatusPermanentRedirect,
		},
		{
			name:   "match mid edge with ts and more char after",
			path:   "/foo/bar/buzz",
			key:    "/foo/bar",
			method: http.MethodGet,
			want:   http.StatusNotFound,
		},
		{
			name:   "match mid edge with ts and more char before",
			path:   "/foo/barr/",
			key:    "/foo/bar",
			method: http.MethodGet,
			want:   http.StatusNotFound,
		},
		{
			name:   "incomplete match end of edge with ts and more char after",
			path:   "/foo/bar",
			key:    "/foo/bar/buzz",
			method: http.MethodGet,
			want:   http.StatusNotFound,
		},
		{
			name:   "incomplete match end of edge with ts and more char before",
			path:   "/foo/bar",
			key:    "/foo/barr/",
			method: http.MethodGet,
			want:   http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			r.RedirectTrailingSlash = true
			require.NoError(t, r.Handler(tc.method, tc.path, h))

			req, _ := http.NewRequest(tc.method, tc.key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.want, w.Code)
			if w.Code == http.StatusPermanentRedirect || w.Code == http.StatusMovedPermanently {
				assert.Equal(t, tc.path, w.Header().Get("Location"))
			}
		})
	}

}

func TestMuxRouterRedirectFixedPath(t *testing.T) {
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {})
	cases := []struct {
		name string
		path string
		key  string
		tsr  bool
		want int
	}{
		{
			name: "clean invalid path traversal",
			path: "/foo/bar/baz",
			key:  "/../foo/bar/baz",
			want: http.StatusMovedPermanently,
		},
		{
			name: "clean invalid path traversal without tsr",
			path: "/foo/bar",
			key:  "/foo/bar/baz/../",
			want: http.StatusNotFound,
		},
		{
			name: "clean invalid path traversal with tsr",
			path: "/foo/bar",
			key:  "/foo/bar/baz/../",
			tsr:  true,
			want: http.StatusMovedPermanently,
		},
		{
			name: "clean invalid root path",
			path: "/",
			key:  ".//",
			want: http.StatusMovedPermanently,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			r.RedirectFixedPath = true
			r.RedirectTrailingSlash = tc.tsr
			require.NoError(t, r.Get(tc.path, h))
			req, _ := http.NewRequest(http.MethodGet, tc.key, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.want, w.Code)
			if w.Code == http.StatusPermanentRedirect || w.Code == http.StatusMovedPermanently {
				assert.Equal(t, tc.path, w.Header().Get("Location"))
			}
		})
	}
}

func TestMuxRouterWithAllowedMethod(t *testing.T) {
	r := New()
	r.HandleMethodNotAllowed = true
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {})

	cases := []struct {
		name    string
		methods []string
		target  string
		path    string
		want    string
	}{
		{
			name:    "all route except the last one",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead},
			path:    "/foo/bar",
			target:  http.MethodTrace,
			want:    "GET, POST, PUT, DELETE, PATCH, CONNECT, OPTIONS, HEAD",
		},
		{
			name:    "all route except the first one",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz",
			target:  http.MethodGet,
			want:    "POST, PUT, DELETE, PATCH, CONNECT, OPTIONS, HEAD, TRACE",
		},
		{
			name:    "all route except patch and delete",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/test",
			target:  http.MethodPatch,
			want:    "GET, POST, PUT, CONNECT, OPTIONS, HEAD, TRACE",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range tc.methods {
				require.NoError(t, r.Handler(method, tc.path, h))
			}
			req, _ := http.NewRequest(tc.target, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("Allow"))
		})
	}
}

func TestRouterPanicHandler(t *testing.T) {
	r := New()
	r.PanicHandler = func(w http.ResponseWriter, r *http.Request, i interface{}) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(i.(string)))
	}
	const errMsg = "unexpected error"
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {
		func() { panic(errMsg) }()
		w.Write([]byte("foo"))
	})

	require.NoError(t, r.Post("/", h))
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, errMsg, w.Body.String())
}

func TestRouterAbortHandler(t *testing.T) {
	r := New()
	r.PanicHandler = func(w http.ResponseWriter, r *http.Request, i interface{}) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(i.(error).Error()))
	}
	const errMsg = "unexpected error"
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {
		func() { panic(http.ErrAbortHandler) }()
		w.Write([]byte("foo"))
	})

	require.NoError(t, r.Post("/", h))
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
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

func TestFuzzRouterInsertLookupAndDelete(t *testing.T) {
	f := fuzz.New().NilChance(0).NumElements(1000, 2000)
	r := New()
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) {})

	routes := make(map[string]struct{})
	f.Fuzz(&routes)

	for rte := range routes {
		err := r.insert(http.MethodGet, "/"+rte, "", h, false)
		require.NoError(t, err)
	}

	countPath := 0
	require.NoError(t, r.WalkRoute(func(route Route, handler Handler) error {
		countPath++
		return nil
	}))
	assert.Equal(t, len(routes), countPath)

	for rte := range routes {
		nds := *r.trees.Load()
		n, _, _ := r.lookup(nds[0], "/"+rte, true)
		require.NotNilf(t, n, "route /%s", rte)
		require.Truef(t, n.isLeaf(), "route /%s", rte)
	}

	for rte := range routes {
		deleted := r.remove(http.MethodGet, "/"+rte)
		require.True(t, deleted)
	}

	countPath = 0
	require.NoError(t, r.WalkRoute(func(route Route, handler Handler) error {
		countPath++
		return nil
	}))
	assert.Equal(t, 0, countPath)
}

func TestFuzzServeHTTP(t *testing.T) {
	r := New()
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) { w.Write([]byte(r.URL.Path)) })

	// no wildcard and invalid escape char
	unicodeRanges := fuzz.UnicodeRanges{
		{First: 0x2E, Last: 0x2E},
		{First: 0x30, Last: 0x3E},
		{First: 0x40, Last: 0x5A},
		{First: 0x61, Last: 0x7E},
		{First: 0xA9, Last: 0xBF},
	}

	f := fuzz.New().NilChance(0).NumElements(1000, 2000).Funcs(unicodeRanges.CustomStringFuzzFunc())

	routes := make(map[string]struct{})
	f.Fuzz(&routes)

	for rte := range routes {
		err := r.Get("/"+rte, h)
		require.NoError(t, err)
	}

	countPath := 0
	require.NoError(t, r.WalkRoute(func(route Route, handler Handler) error {
		countPath++
		return nil
	}))
	assert.Equal(t, len(routes), countPath)

	for rte := range routes {
		req, err := http.NewRequest(http.MethodGet, "/"+rte, nil)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equalf(t, http.StatusOK, w.Code, "route /%s", rte)
		assert.Equal(t, "/"+rte, w.Body.String())
	}

}

func TestFuzzServeHTTPNoPanic(t *testing.T) {
	r := New()
	h := HandlerFunc(func(w http.ResponseWriter, r *http.Request, _ Params) { w.Write([]byte(r.URL.Path)) })

	f := fuzz.New().NilChance(0).NumElements(5000, 10_000)

	routes := make(map[string]struct{})
	f.Fuzz(&routes)

	assert.NotPanics(t, func() {
		for rte := range routes {
			err := r.insert(http.MethodGet, "/"+rte, "", h, false)
			require.NoError(t, err)
		}

		countPath := 0
		require.NoError(t, r.WalkRoute(func(route Route, handler Handler) error {
			countPath++
			return nil
		}))
		assert.Equal(t, len(routes), countPath)

		req, _ := http.NewRequest("GET", "/", nil)
		u := req.URL

		for rte := range routes {
			req.RequestURI = "/" + rte
			u.Path = "/" + rte
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equalf(t, http.StatusOK, w.Code, "route /%s", rte)
			assert.Equal(t, "/"+rte, w.Body.String())
		}
	})
}
