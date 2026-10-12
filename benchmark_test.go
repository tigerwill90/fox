package fox

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func benchRoute(b *testing.B, router http.Handler, routes []route) {
	w := new(mockResponseWriter)
	r := httptest.NewRequest("GET", "/", nil)
	u := r.URL
	rq := u.RawQuery

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		for _, route := range routes {
			r.Method = route.method
			r.RequestURI = route.path
			u.Path = route.path
			u.RawQuery = rq
			router.ServeHTTP(w, r)
		}
	}
}

func benchHostname(b *testing.B, router http.Handler, routes []route) {
	w := new(mockResponseWriter)
	r := httptest.NewRequest("GET", "/", nil)
	u := r.URL
	rq := u.RawQuery

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		for _, route := range routes {
			r.Method = route.method
			r.Host = route.path
			r.RequestURI = "/"
			u.Path = "/"
			u.RawQuery = rq
			router.ServeHTTP(w, r)
		}
	}
}

func benchRouteParallel(b *testing.B, router http.Handler, rte route) {
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		w := new(mockResponseWriter)
		r, _ := http.NewRequest(rte.method, rte.path, nil)
		for pb.Next() {
			router.ServeHTTP(w, r)
		}
	})
}

func BenchmarkStaticAll(b *testing.B) {
	r, _ := NewRouter()
	for _, route := range staticRoutes {
		require.NoError(b, onlyError(r.Add([]string{route.method}, route.path, emptyHandler)))
	}

	benchRoute(b, r, staticRoutes)
}

func BenchmarkGithubAll(b *testing.B) {
	f := MustRouter()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Add([]string{route.method}, route.path, emptyHandler)))
	}

	data := make([]route, 0, len(githubAPI))
	for _, r := range githubAPI {
		data = append(data, route{method: r.method, path: replaceParams.ReplaceAllString(r.path, "xxx")})
	}

	benchRoute(b, f, data)
}

func BenchmarkGithubAllNormalize(b *testing.B) {
	f := MustRouter(WithMergeSlashes(NormalizePath), WithCollapseDotSegments(NormalizePath))
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Add([]string{route.method}, route.path, emptyHandler)))
	}

	data := make([]route, 0, len(githubAPI))
	for _, r := range githubAPI {
		data = append(data, route{method: r.method, path: replaceParams.ReplaceAllString(r.path, "xxx")})
	}

	benchRoute(b, f, data)
}

func BenchmarkNormalizeDirtyPath(b *testing.B) {
	f := MustRouter(WithMergeSlashes(NormalizePath), WithCollapseDotSegments(NormalizePath))
	require.NoError(b, onlyError(f.Add(MethodGet, "/repos/{owner}/{repo}/issues", emptyHandler)))

	benchRoute(b, f, []route{{method: http.MethodGet, path: "/repos//foo/./bar/../bar/issues"}})
}

func BenchmarkStaticAllMux(b *testing.B) {
	r := http.NewServeMux()
	for _, route := range staticRoutes {
		r.HandleFunc(route.method+" "+route.path, func(w http.ResponseWriter, r *http.Request) {

		})
	}

	benchRoute(b, r, staticRoutes)
}

func BenchmarkStaticHostnameAll(b *testing.B) {
	r, _ := NewRouter()
	for _, route := range staticHostnames {
		require.NoError(b, onlyError(r.Add([]string{route.method}, route.path+"/", emptyHandler)))
	}

	benchHostname(b, r, staticHostnames)
}

func BenchmarkStaticHostnameAllMux(b *testing.B) {
	r := http.NewServeMux()
	for _, route := range staticHostnames {
		r.HandleFunc(route.method+" "+route.path+"/", func(w http.ResponseWriter, r *http.Request) {

		})
	}

	benchHostname(b, r, staticHostnames)
}

func BenchmarkGithubParamsAll(b *testing.B) {
	r, _ := NewRouter()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(r.Add([]string{route.method}, route.path, emptyHandler)))
	}

	req := httptest.NewRequest(http.MethodGet, "/repos/sylvain/fox/hooks/1500", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkGithubParamsHostnameAll(b *testing.B) {
	r, _ := NewRouter()
	for _, route := range wildcardHostnames {
		require.NoError(b, onlyError(r.Add([]string{route.method}, route.path+"/", emptyHandler)))
	}

	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(b, err)
	req.Host = "repos.sylvain.fox.hooks.1500"
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkInfixCatchAll(b *testing.B) {
	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/+{a}/b/+{c}/d/+{e}/f/+{g}/j", emptyHandler)

	req := httptest.NewRequest(http.MethodGet, "/x/y/z/b/x/y/z/d/x/y/z/f/x/y/z/j", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		f.ServeHTTP(w, req)
	}
}

func BenchmarkInfixCatchAllNearMiss(b *testing.B) {
	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/files/+{p}/thumb/+{q}/download", emptyHandler)

	req := httptest.NewRequest(http.MethodGet, "/files"+strings.Repeat("/thumb", 1024), nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		f.ServeHTTP(w, req)
	}
}

func BenchmarkRegexp(b *testing.B) {
	deep := strings.Repeat("abc/", 32) + "abc"
	cases := []struct {
		name  string
		route string
		path  string
	}{
		{name: "param", route: "/users/{id:[0-9]+}", path: "/users/12345"},
		{name: "suffix", route: "/files/+{p:[a-z/]+}", path: "/files/abc/def/ghi"},
		{name: "infix_class_short", route: "/files/+{p:[a-z/]+}/meta", path: "/files/abc/meta"},
		{name: "infix_class_deep", route: "/files/+{p:[a-z/]+}/meta", path: "/files/" + deep + "/meta"},
		{name: "infix_class_param_deep", route: "/files/+{p:[a-z/]+}/{name}", path: "/files/" + deep + "/meta"},
		{name: "infix_short", route: "/files/+{p:(?:[a-z]+/)*[a-z]+}/meta", path: "/files/abc/meta"},
		{name: "infix_deep", route: "/files/+{p:(?:[a-z]+/)*[a-z]+}/meta", path: "/files/" + deep + "/meta"},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			f, _ := NewRouter(WithAllowRegexpParam(true))
			f.MustAdd(MethodGet, tc.route, emptyHandler)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := new(mockResponseWriter)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				f.ServeHTTP(w, req)
			}
		})
	}
}

func BenchmarkRegexpInsert(b *testing.B) {
	f, _ := NewRouter(WithAllowRegexpParam(true))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		f.MustAdd(MethodGet, "/files/+{p:[a-z0-9/._-]+}/meta", emptyHandler)
		_, _ = f.Delete(MethodGet, "/files/+{p:[a-z0-9/._-]+}/meta")
	}
}

func BenchmarkLongParam(b *testing.B) {
	r, _ := NewRouter()
	r.MustAdd(MethodGet, "/foo/{very_very_very_very_very_long_param}", emptyHandler)
	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkOverlappingRoute(b *testing.B) {
	r, _ := NewRouter()
	for _, route := range overlappingRoutes {
		require.NoError(b, onlyError(r.Add([]string{route.method}, route.path, emptyHandler)))
	}

	req := httptest.NewRequest(http.MethodGet, "/foo/abc/id:123/xy", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkWithIgnoreTrailingSlash(b *testing.B) {
	f, _ := NewRouter(WithTrailingSlash(RelaxedSlash))
	f.MustAdd(MethodGet, "/{a}/{b}/e", emptyHandler)
	f.MustAdd(MethodGet, "/{a}/{b}/d", emptyHandler)
	f.MustAdd(MethodGet, "/foo/{b}", emptyHandler)
	f.MustAdd(MethodGet, "/foo/bar/x/", emptyHandler)
	f.MustAdd(MethodGet, "/foo/{b}/y/", emptyHandler)

	req := httptest.NewRequest(http.MethodGet, "/foo/bar/", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		f.ServeHTTP(w, req)
	}
}

func BenchmarkStaticParallel(b *testing.B) {
	r, _ := NewRouter()
	for _, route := range staticRoutes {
		require.NoError(b, onlyError(r.Add([]string{route.method}, route.path, emptyHandler)))
	}
	benchRouteParallel(b, r, route{http.MethodGet, "/progs/image_package4.out"})
}

func BenchmarkCatchAll(b *testing.B) {
	r, _ := NewRouter()
	require.NoError(b, onlyError(r.Add(MethodGet, "/something/+{args}", emptyHandler)))
	w := new(mockResponseWriter)
	req := httptest.NewRequest(http.MethodGet, "/something/awesome", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkCatchAllParallel(b *testing.B) {
	r, _ := NewRouter()
	require.NoError(b, onlyError(r.Add(MethodGet, "/something/+{args}", emptyHandler)))

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		w := new(mockResponseWriter)
		req := httptest.NewRequest("GET", "/something/awesome", nil)
		for pb.Next() {
			r.ServeHTTP(w, req)
		}
	})
}

func BenchmarkContext_CloneWith(b *testing.B) {
	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/hello/{name}", func(c *Context) {
		cp := c.CloneWith(c.Writer(), c.Request())
		cp.Close()
	})
	w := new(mockResponseWriter)
	r := httptest.NewRequest("GET", "/hello/fox", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.ServeHTTP(w, r)
	}
}

func BenchmarkVeryLongPattern(b *testing.B) {
	f := MustRouter()
	f.MustAdd(MethodGet, "/hello/very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very", emptyHandler)
	f.MustAdd(MethodGet, "/hello/add", emptyHandler)
	f.MustAdd(MethodGet, "/help", emptyHandler)

	req := httptest.NewRequest(http.MethodGet, "/hello/very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very_very", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		f.ServeHTTP(w, req)
	}
}
