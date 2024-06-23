// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var emptyHandler = HandlerFunc(func(c Context) {})
var pathHandler = HandlerFunc(func(c Context) { _ = c.String(200, c.Request().URL.Path) })

type mockResponseWriter struct{}

func (m mockResponseWriter) Header() (h http.Header) {
	return http.Header{}
}

func (m mockResponseWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (m mockResponseWriter) WriteString(s string) (n int, err error) {
	return len(s), nil
}

func (m mockResponseWriter) WriteHeader(int) {}

type route struct {
	method string
	path   string
}

var overlappingRoutes = []route{
	{"GET", "/foo/abc/id:{id}/xyz"},
	{"GET", "/foo/{name}/id:{id}/{name}"},
	{"GET", "/foo/{name}/id:{id}/xyz"},
}

// From https://github.com/julienschmidt/go-http-routing-benchmark
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

// From https://github.com/julienschmidt/go-http-routing-benchmark
var githubAPI = []route{
	// OAuth Authorizations
	{"GET", "/authorizations"},
	{"GET", "/authorizations/{id}"},
	{"POST", "/authorizations"},
	{"DELETE", "/authorizations/{id}"},
	{"GET", "/applications/{client_id}/tokens/{access_token}"},
	{"DELETE", "/applications/{client_id}/tokens"},
	{"DELETE", "/applications/{client_id}/tokens/{access_token}"},

	// Activity
	{"GET", "/events"},
	{"GET", "/repos/{owner}/{repo}/events"},
	{"GET", "/networks/{owner}/{repo}/events"},
	{"GET", "/orgs/{org}/events"},
	{"GET", "/users/{user}/received_events"},
	{"GET", "/users/{user}/received_events/public"},
	{"GET", "/users/{user}/events"},
	{"GET", "/users/{user}/events/public"},
	{"GET", "/users/{user}/events/orgs/{org}"},
	{"GET", "/feeds"},
	{"GET", "/notifications"},
	{"GET", "/repos/{owner}/{repo}/notifications"},
	{"PUT", "/notifications"},
	{"PUT", "/repos/{owner}/{repo}/notifications"},
	{"GET", "/notifications/threads/{id}"},
	{"GET", "/notifications/threads/{id}/subscription"},
	{"PUT", "/notifications/threads/{id}/subscription"},
	{"DELETE", "/notifications/threads/{id}/subscription"},
	{"GET", "/repos/{owner}/{repo}/stargazers"},
	{"GET", "/users/{user}/starred"},
	{"GET", "/user/starred"},
	{"GET", "/user/starred/{owner}/{repo}"},
	{"PUT", "/user/starred/{owner}/{repo}"},
	{"DELETE", "/user/starred/{owner}/{repo}"},
	{"GET", "/repos/{owner}/{repo}/subscribers"},
	{"GET", "/users/{user}/subscriptions"},
	{"GET", "/user/subscriptions"},
	{"GET", "/repos/{owner}/{repo}/subscription"},
	{"PUT", "/repos/{owner}/{repo}/subscription"},
	{"DELETE", "/repos/{owner}/{repo}/subscription"},
	{"GET", "/user/subscriptions/{owner}/{repo}"},
	{"PUT", "/user/subscriptions/{owner}/{repo}"},
	{"DELETE", "/user/subscriptions/{owner}/{repo}"},

	// Gists
	{"GET", "/users/{user}/gists"},
	{"GET", "/gists"},
	{"GET", "/gists/{id}"},
	{"POST", "/gists"},
	{"PUT", "/gists/{id}/star"},
	{"DELETE", "/gists/{id}/star"},
	{"GET", "/gists/{id}/star"},
	{"POST", "/gists/{id}/forks"},
	{"DELETE", "/gists/{id}"},

	// Git Data
	{"GET", "/repos/{owner}/{repo}/git/blobs/{sha}"},
	{"POST", "/repos/{owner}/{repo}/git/blobs"},
	{"GET", "/repos/{owner}/{repo}/git/commits/{sha}"},
	{"POST", "/repos/{owner}/{repo}/git/commits"},
	{"GET", "/repos/{owner}/{repo}/git/refs/*{ref}"},
	{"GET", "/repos/{owner}/{repo}/git/refs"},
	{"POST", "/repos/{owner}/{repo}/git/refs"},
	{"DELETE", "/repos/{owner}/{repo}/git/refs/*{ref}"},
	{"GET", "/repos/{owner}/{repo}/git/tags/{sha}"},
	{"POST", "/repos/{owner}/{repo}/git/tags"},
	{"GET", "/repos/{owner}/{repo}/git/trees/{sha}"},
	{"POST", "/repos/{owner}/{repo}/git/trees"},

	// Issues
	{"GET", "/issues"},
	{"GET", "/user/issues"},
	{"GET", "/orgs/{org}/issues"},
	{"GET", "/repos/{owner}/{repo}/issues"},
	{"GET", "/repos/{owner}/{repo}/issues/{number}"},
	{"POST", "/repos/{owner}/{repo}/issues"},
	{"GET", "/repos/{owner}/{repo}/assignees"},
	{"GET", "/repos/{owner}/{repo}/assignees/:assignee"},
	{"GET", "/repos/{owner}/{repo}/issues/{number}/comments"},
	{"POST", "/repos/{owner}/{repo}/issues/{number}/comments"},
	{"GET", "/repos/{owner}/{repo}/issues/{number}/events"},
	{"GET", "/repos/{owner}/{repo}/labels"},
	{"GET", "/repos/{owner}/{repo}/labels/{name}"},
	{"POST", "/repos/{owner}/{repo}/labels"},
	{"DELETE", "/repos/{owner}/{repo}/labels/{name}"},
	{"GET", "/repos/{owner}/{repo}/issues/{number}/labels"},
	{"POST", "/repos/{owner}/{repo}/issues/{number}/labels"},
	{"DELETE", "/repos/{owner}/{repo}/issues/{number}/labels/{name}"},
	{"PUT", "/repos/{owner}/{repo}/issues/{number}/labels"},
	{"DELETE", "/repos/{owner}/{repo}/issues/{number}/labels"},
	{"GET", "/repos/{owner}/{repo}/milestones/{number}/labels"},
	{"GET", "/repos/{owner}/{repo}/milestones"},
	{"GET", "/repos/{owner}/{repo}/milestones/{number}"},
	{"POST", "/repos/{owner}/{repo}/milestones"},
	{"DELETE", "/repos/{owner}/{repo}/milestones/{number}"},

	// Miscellaneous
	{"GET", "/emojis"},
	{"GET", "/gitignore/templates"},
	{"GET", "/gitignore/templates/{name}"},
	{"POST", "/markdown"},
	{"POST", "/markdown/raw"},
	{"GET", "/meta"},
	{"GET", "/rate_limit"},

	// Organizations
	{"GET", "/users/{user}/orgs"},
	{"GET", "/user/orgs"},
	{"GET", "/orgs/{org}"},
	{"GET", "/orgs/{org}/members"},
	{"GET", "/orgs/{org}/members/{user}"},
	{"DELETE", "/orgs/{org}/members/{user}"},
	{"GET", "/orgs/{org}/public_members"},
	{"GET", "/orgs/{org}/public_members/{user}"},
	{"PUT", "/orgs/{org}/public_members/{user}"},
	{"DELETE", "/orgs/{org}/public_members/{user}"},
	{"GET", "/orgs/{org}/teams"},
	{"GET", "/teams/{id}"},
	{"POST", "/orgs/{org}/teams"},
	{"DELETE", "/teams/{id}"},
	{"GET", "/teams/{id}/members"},
	{"GET", "/teams/{id}/members/{user}"},
	{"PUT", "/teams/{id}/members/{user}"},
	{"DELETE", "/teams/{id}/members/{user}"},
	{"GET", "/teams/{id}/repos"},
	{"GET", "/teams/{id}/repos/{owner}/{repo}"},
	{"PUT", "/teams/{id}/repos/{owner}/{repo}"},
	{"DELETE", "/teams/{id}/repos/{owner}/{repo}"},
	{"GET", "/user/teams"},

	// Pull Requests
	{"GET", "/repos/{owner}/{repo}/pulls"},
	{"GET", "/repos/{owner}/{repo}/pulls/{number}"},
	{"POST", "/repos/{owner}/{repo}/pulls"},
	{"GET", "/repos/{owner}/{repo}/pulls/{number}/commits"},
	{"GET", "/repos/{owner}/{repo}/pulls/{number}/files"},
	{"GET", "/repos/{owner}/{repo}/pulls/{number}/merge"},
	{"PUT", "/repos/{owner}/{repo}/pulls/{number}/merge"},
	{"GET", "/repos/{owner}/{repo}/pulls/{number}/comments"},
	{"PUT", "/repos/{owner}/{repo}/pulls/{number}/comments"},

	// Repositories
	{"GET", "/user/repos"},
	{"GET", "/users/{user}/repos"},
	{"GET", "/orgs/{org}/repos"},
	{"GET", "/repositories"},
	{"POST", "/user/repos"},
	{"POST", "/orgs/{org}/repos"},
	{"GET", "/repos/{owner}/{repo}"},
	{"GET", "/repos/{owner}/{repo}/contributors"},
	{"GET", "/repos/{owner}/{repo}/languages"},
	{"GET", "/repos/{owner}/{repo}/teams"},
	{"GET", "/repos/{owner}/{repo}/tags"},
	{"GET", "/repos/{owner}/{repo}/branches"},
	{"GET", "/repos/{owner}/{repo}/branches/{branch}"},
	{"DELETE", "/repos/{owner}/{repo}"},
	{"GET", "/repos/{owner}/{repo}/collaborators"},
	{"GET", "/repos/{owner}/{repo}/collaborators/{user}"},
	{"PUT", "/repos/{owner}/{repo}/collaborators/{user}"},
	{"DELETE", "/repos/{owner}/{repo}/collaborators/{user}"},
	{"GET", "/repos/{owner}/{repo}/comments"},
	{"GET", "/repos/{owner}/{repo}/commits/{sha}/comments"},
	{"POST", "/repos/{owner}/{repo}/commits/{sha}/comments"},
	{"GET", "/repos/{owner}/{repo}/comments/{id}"},
	{"DELETE", "/repos/{owner}/{repo}/comments/{id}"},
	{"GET", "/repos/{owner}/{repo}/commits"},
	{"GET", "/repos/{owner}/{repo}/commits/{sha}"},
	{"GET", "/repos/{owner}/{repo}/readme"},
	{"GET", "/repos/{owner}/{repo}/contents/*{path}"},
	{"DELETE", "/repos/{owner}/{repo}/contents/*{path}"},
	{"GET", "/repos/{owner}/{repo}/keys"},
	{"GET", "/repos/{owner}/{repo}/keys/{id}"},
	{"POST", "/repos/{owner}/{repo}/keys"},
	{"DELETE", "/repos/{owner}/{repo}/keys/{id}"},
	{"GET", "/repos/{owner}/{repo}/downloads"},
	{"GET", "/repos/{owner}/{repo}/downloads/{id}"},
	{"DELETE", "/repos/{owner}/{repo}/downloads/{id}"},
	{"GET", "/repos/{owner}/{repo}/forks"},
	{"POST", "/repos/{owner}/{repo}/forks"},
	{"GET", "/repos/{owner}/{repo}/hooks"},
	{"GET", "/repos/{owner}/{repo}/hooks/{id}"},
	{"POST", "/repos/{owner}/{repo}/hooks"},
	{"POST", "/repos/{owner}/{repo}/hooks/{id}/tests"},
	{"DELETE", "/repos/{owner}/{repo}/hooks/{id}"},
	{"POST", "/repos/{owner}/{repo}/merges"},
	{"GET", "/repos/{owner}/{repo}/releases"},
	{"GET", "/repos/{owner}/{repo}/releases/{id}"},
	{"POST", "/repos/{owner}/{repo}/releases"},
	{"DELETE", "/repos/{owner}/{repo}/releases/{id}"},
	{"GET", "/repos/{owner}/{repo}/releases/{id}/assets"},
	{"GET", "/repos/{owner}/{repo}/stats/contributors"},
	{"GET", "/repos/{owner}/{repo}/stats/commit_activity"},
	{"GET", "/repos/{owner}/{repo}/stats/code_frequency"},
	{"GET", "/repos/{owner}/{repo}/stats/participation"},
	{"GET", "/repos/{owner}/{repo}/stats/punch_card"},
	{"GET", "/repos/{owner}/{repo}/statuses/{ref}"},
	{"POST", "/repos/{owner}/{repo}/statuses/{ref}"},

	// Search
	{"GET", "/search/repositories"},
	{"GET", "/search/code"},
	{"GET", "/search/issues"},
	{"GET", "/search/users"},
	{"GET", "/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"},
	{"GET", "/legacy/repos/search/{keyword}"},
	{"GET", "/legacy/user/search/{keyword}"},
	{"GET", "/legacy/user/email/{email}"},

	// Users
	{"GET", "/users/{user}"},
	{"GET", "/user"},
	{"GET", "/users"},
	{"GET", "/user/emails"},
	{"POST", "/user/emails"},
	{"DELETE", "/user/emails"},
	{"GET", "/users/{user}/followers"},
	{"GET", "/user/followers"},
	{"GET", "/users/{user}/following"},
	{"GET", "/user/following"},
	{"GET", "/user/following/{user}"},
	{"GET", "/users/{user}/following/{target_user}"},
	{"PUT", "/user/following/{user}"},
	{"DELETE", "/user/following/{user}"},
	{"GET", "/users/{user}/keys"},
	{"GET", "/user/keys"},
	{"GET", "/user/keys/{id}"},
	{"POST", "/user/keys"},
	{"DELETE", "/user/keys/{id}"},
}

func benchRoutes(b *testing.B, router http.Handler, routes []route) {
	w := new(mockResponseWriter)
	r := httptest.NewRequest("GET", "/", nil)
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

func BenchmarkStaticAll(b *testing.B) {
	r := New()
	for _, route := range staticRoutes {
		require.NoError(b, r.Tree().Handle(route.method, route.path, emptyHandler))
	}

	benchRoutes(b, r, staticRoutes)
}

func BenchmarkGithubParamsAll(b *testing.B) {
	r := New()
	for _, route := range githubAPI {
		require.NoError(b, r.Tree().Handle(route.method, route.path, emptyHandler))
	}

	req := httptest.NewRequest("GET", "/repos/sylvain/fox/hooks/1500", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkOverlappingRoute(b *testing.B) {
	r := New()
	for _, route := range overlappingRoutes {
		require.NoError(b, r.Tree().Handle(route.method, route.path, emptyHandler))
	}

	req := httptest.NewRequest("GET", "/foo/abc/id:123/xy", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkStaticParallel(b *testing.B) {
	r := New()
	for _, route := range staticRoutes {
		require.NoError(b, r.Tree().Handle(route.method, route.path, emptyHandler))
	}
	benchRouteParallel(b, r, route{"GET", "/progs/image_package4.out"})
}

func BenchmarkCatchAll(b *testing.B) {
	r := New()
	require.NoError(b, r.Tree().Handle(http.MethodGet, "/something/*{args}", emptyHandler))
	w := new(mockResponseWriter)
	req := httptest.NewRequest("GET", "/something/awesome", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkCatchAllParallel(b *testing.B) {
	r := New()
	require.NoError(b, r.Tree().Handle(http.MethodGet, "/something/*{args}", emptyHandler))
	w := new(mockResponseWriter)
	req := httptest.NewRequest("GET", "/something/awesome", nil)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r.ServeHTTP(w, req)
		}
	})
}

func BenchmarkCloneWith(b *testing.B) {
	f := New()
	f.MustHandle(http.MethodGet, "/hello/{name}", func(c Context) {
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

func TestStaticRoute(t *testing.T) {
	r := New()

	for _, route := range staticRoutes {
		require.NoError(t, r.Tree().Handle(route.method, route.path, pathHandler))
	}

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}
}

func TestStaticRouteMalloc(t *testing.T) {
	r := New()

	for _, route := range staticRoutes {
		require.NoError(t, r.Tree().Handle(route.method, route.path, emptyHandler))
	}

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestParamsRoute(t *testing.T) {
	rx := regexp.MustCompile("({|\\*{)[A-z]+[}]")
	r := New()
	h := func(c Context) {
		matches := rx.FindAllString(c.Request().URL.Path, -1)
		for _, match := range matches {
			var key string
			if strings.HasPrefix(match, "*") {
				key = match[2 : len(match)-1]
			} else {
				key = match[1 : len(match)-1]
			}
			value := match
			assert.Equal(t, value, c.Param(key))
		}
		assert.Equal(t, c.Request().URL.Path, c.Path())
		_ = c.String(200, c.Request().URL.Path)
	}
	for _, route := range githubAPI {
		require.NoError(t, r.Tree().Handle(route.method, route.path, h))
	}
	for _, route := range githubAPI {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}
}

func TestParamsRouteMalloc(t *testing.T) {
	r := New()
	for _, route := range githubAPI {
		require.NoError(t, r.Tree().Handle(route.method, route.path, emptyHandler))
	}
	for _, route := range githubAPI {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestOverlappingRouteMalloc(t *testing.T) {
	r := New()
	for _, route := range overlappingRoutes {
		require.NoError(t, r.Tree().Handle(route.method, route.path, emptyHandler))
	}
	for _, route := range overlappingRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestRouterWildcard(t *testing.T) {
	r := New()

	routes := []struct {
		path string
		key  string
	}{
		{"/github.com/etf1/*{repo}", "/github.com/etf1/mux"},
		{"/github.com/johndoe/*{repo}", "/github.com/johndoe/buzz"},
		{"/foo/bar/*{args}", "/foo/bar/"},
		{"/filepath/path=*{path}", "/filepath/path=/file.txt"},
	}

	for _, route := range routes {
		require.NoError(t, r.Tree().Handle(http.MethodGet, route.path, pathHandler))
	}

	for _, route := range routes {
		req := httptest.NewRequest(http.MethodGet, route.key, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "route: key: %s, path: %s", route.path)
		assert.Equal(t, route.key, w.Body.String())
	}
}

func TestRouteWithParams(t *testing.T) {
	tree := New().Tree()
	routes := [...]string{
		"/",
		"/cmd/{tool}/{sub}",
		"/cmd/{tool}/",
		"/src/*{filepath}",
		"/search/",
		"/search/{query}",
		"/user_{name}",
		"/user_{name}/about",
		"/files/{dir}/*{filepath}",
		"/doc/",
		"/doc/go_faq.html",
		"/doc/go1.html",
		"/info/{user}/public",
		"/info/{user}/project/{project}",
	}
	for _, rte := range routes {
		require.NoError(t, tree.Handle(http.MethodGet, rte, emptyHandler))
	}

	nds := *tree.nodes.Load()
	for _, rte := range routes {
		c := newTestContextTree(tree)
		n, tsr := tree.lookup(nds[0], rte, c.params, c.skipNds, false)
		require.NotNil(t, n)
		assert.False(t, tsr)
		assert.Equal(t, rte, n.path)
	}
}

func TestRouteParamEmptySegment(t *testing.T) {
	tree := New().Tree()
	cases := []struct {
		name  string
		route string
		path  string
	}{
		{
			name:  "empty segment",
			route: "/cmd/{tool}/{sub}",
			path:  "/cmd//sub",
		},
		{
			name:  "empty inflight end of route",
			route: "/command/exec:{tool}",
			path:  "/command/exec:",
		},
		{
			name:  "empty inflight segment",
			route: "/command/exec:{tool}/id",
			path:  "/command/exec:/id",
		},
	}

	for _, tc := range cases {
		require.NoError(t, tree.Handle(http.MethodGet, tc.route, emptyHandler))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nds := *tree.nodes.Load()
			c := newTestContextTree(tree)
			n, tsr := tree.lookup(nds[0], tc.path, c.params, c.skipNds, false)
			assert.Nil(t, n)
			assert.Empty(t, c.Params())
			assert.False(t, tsr)
		})
	}
}

func TestOverlappingRoute(t *testing.T) {
	r := New()
	cases := []struct {
		name       string
		path       string
		routes     []string
		wantMatch  string
		wantParams Params
	}{
		{
			name: "basic test most specific",
			path: "/products/new",
			routes: []string{
				"/products/{id}",
				"/products/new",
			},
			wantMatch: "/products/new",
		},
		{
			name: "basic test less specific",
			path: "/products/123",
			routes: []string{
				"/products/{id}",
				"/products/new",
			},
			wantMatch:  "/products/{id}",
			wantParams: Params{{Key: "id", Value: "123"}},
		},
		{
			name: "ieof+backtrack to {id} wildcard while deleting {a}",
			path: "/base/val1/123/new/barr",
			routes: []string{
				"/{base}/val1/{id}",
				"/{base}/val1/123/{a}/bar",
				"/{base}/val1/{id}/new/{name}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id}/new/{name}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "id",
					Value: "123",
				},
				{
					Key:   "name",
					Value: "barr",
				},
			},
		},
		{
			name: "kme+backtrack to {id} wildcard while deleting {a}",
			path: "/base/val1/123/new/ba",
			routes: []string{
				"/{base}/val1/{id}",
				"/{base}/val1/123/{a}/bar",
				"/{base}/val1/{id}/new/{name}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id}/new/{name}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "id",
					Value: "123",
				},
				{
					Key:   "name",
					Value: "ba",
				},
			},
		},
		{
			name: "ime+backtrack to {id} wildcard while deleting {a}",
			path: "/base/val1/123/new/bx",
			routes: []string{
				"/{base}/val1/{id}",
				"/{base}/val1/123/{a}/bar",
				"/{base}/val1/{id}/new/{name}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id}/new/{name}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "id",
					Value: "123",
				},
				{
					Key:   "name",
					Value: "bx",
				},
			},
		},
		{
			name: "backtrack to catch while deleting {a}, {id} and {name}",
			path: "/base/val1/123/new/bar/",
			routes: []string{
				"/{base}/val1/{id}",
				"/{base}/val1/123/{a}/bar",
				"/{base}/val1/{id}/new/{name}",
				"/{base}/val*{all}",
			},
			wantMatch: "/{base}/val*{all}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "all",
					Value: "1/123/new/bar/",
				},
			},
		},
		{
			name: "notleaf+backtrack to catch while deleting {a}, {id}",
			path: "/base/val1/123/new",
			routes: []string{
				"/{base}/val1/123/{a}/baz",
				"/{base}/val1/123/{a}/bar",
				"/{base}/val1/{id}/new/{name}",
				"/{base}/val*{all}",
			},
			wantMatch: "/{base}/val*{all}",
			wantParams: Params{
				{
					Key:   "base",
					Value: "base",
				},
				{
					Key:   "all",
					Value: "1/123/new",
				},
			},
		},
		{
			name: "multi node most specific",
			path: "/foo/1/2/3/bar",
			routes: []string{
				"/foo/{ab}",
				"/foo/{ab}/{bc}",
				"/foo/{ab}/{bc}/{de}",
				"/foo/{ab}/{bc}/{de}/bar",
				"/foo/{ab}/{bc}/{de}/{fg}",
			},
			wantMatch: "/foo/{ab}/{bc}/{de}/bar",
			wantParams: Params{
				{
					Key:   "ab",
					Value: "1",
				},
				{
					Key:   "bc",
					Value: "2",
				},
				{
					Key:   "de",
					Value: "3",
				},
			},
		},
		{
			name: "multi node less specific",
			path: "/foo/1/2/3/john",
			routes: []string{
				"/foo/{ab}",
				"/foo/{ab}/{bc}",
				"/foo/{ab}/{bc}/{de}",
				"/foo/{ab}/{bc}/{de}/bar",
				"/foo/{ab}/{bc}/{de}/{fg}",
			},
			wantMatch: "/foo/{ab}/{bc}/{de}/{fg}",
			wantParams: Params{
				{
					Key:   "ab",
					Value: "1",
				},
				{
					Key:   "bc",
					Value: "2",
				},
				{
					Key:   "de",
					Value: "3",
				},
				{
					Key:   "fg",
					Value: "john",
				},
			},
		},
		{
			name: "backtrack on empty mid key parameter",
			path: "/foo/abc/bar",
			routes: []string{
				"/foo/abc{id}/bar",
				"/foo/{name}/bar",
			},
			wantMatch: "/foo/{name}/bar",
			wantParams: Params{
				{
					Key:   "name",
					Value: "abc",
				},
			},
		},
		{
			name: "most specific wildcard between catch all",
			path: "/foo/123",
			routes: []string{
				"/foo/{id}",
				"/foo/a*{args}",
				"/foo*{args}",
			},
			wantMatch: "/foo/{id}",
			wantParams: Params{
				{
					Key:   "id",
					Value: "123",
				},
			},
		},
		{
			name: "most specific catch all with param",
			path: "/foo/abc",
			routes: []string{
				"/foo/{id}",
				"/foo/a*{args}",
				"/foo*{args}",
			},
			wantMatch: "/foo/a*{args}",
			wantParams: Params{
				{
					Key:   "args",
					Value: "bc",
				},
			},
		},
		{
			name: "named parameter priority over catch-all",
			path: "/foo/abc",
			routes: []string{
				"/foo/{id}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id}",
			wantParams: Params{
				{
					Key:   "id",
					Value: "abc",
				},
			},
		},
		{
			name: "static priority over named parameter and catch-all",
			path: "/foo/abc",
			routes: []string{
				"/foo/abc",
				"/foo/{id}",
				"/foo/*{args}",
			},
			wantMatch:  "/foo/abc",
			wantParams: Params{},
		},
		{
			name: "no match static with named parameter fallback",
			path: "/foo/abd",
			routes: []string{
				"/foo/abc",
				"/foo/{id}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id}",
			wantParams: Params{
				{
					Key:   "id",
					Value: "abd",
				},
			},
		},
		{
			name: "no match static with catch all fallback",
			path: "/foo/abc/foo",
			routes: []string{
				"/foo/abc",
				"/foo/{id}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/*{args}",
			wantParams: Params{
				{
					Key:   "args",
					Value: "abc/foo",
				},
			},
		},
		{
			name: "most specific catch all with static",
			path: "/foo/bar/abd",
			routes: []string{
				"/foo/{id}/abc",
				"/foo/{id}/*{args}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id}/*{args}",
			wantParams: Params{
				{
					Key:   "id",
					Value: "bar",
				},
				{
					Key:   "args",
					Value: "abd",
				},
			},
		},
		{
			name: "most specific catch all with static and named parameter",
			path: "/foo/bar/abc/def",
			routes: []string{
				"/foo/{id_1}/abc",
				"/foo/{id_1}/{id_2}",
				"/foo/{id_1}/*{args}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id_1}/*{args}",
			wantParams: Params{
				{
					Key:   "id_1",
					Value: "bar",
				},
				{
					Key:   "args",
					Value: "abc/def",
				},
			},
		},
		{
			name: "backtrack to most specific named parameter with 2 skipped catch all",
			path: "/foo/bar/def",
			routes: []string{
				"/foo/{id_1}/abc",
				"/foo/{id_1}/*{args}",
				"/foo/{id_1}/{id_2}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id_1}/{id_2}",
			wantParams: Params{
				{
					Key:   "id_1",
					Value: "bar",
				},
				{
					Key:   "id_2",
					Value: "def",
				},
			},
		},
		{
			name: "backtrack to most specific catch-all with an exact match",
			path: "/foo/bar/",
			routes: []string{
				"/foo/{id_1}/abc",
				"/foo/{id_1}/*{args}",
				"/foo/{id_1}/{id_2}",
				"/foo/*{args}",
			},
			wantMatch: "/foo/{id_1}/*{args}",
			wantParams: Params{
				{
					Key:   "id_1",
					Value: "bar",
				},
				{
					Key: "args",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := r.NewTree()
			for _, rte := range tc.routes {
				require.NoError(t, tree.Handle(http.MethodGet, rte, emptyHandler))
			}
			nds := *tree.nodes.Load()

			c := newTestContextTree(tree)
			n, tsr := tree.lookup(nds[0], tc.path, c.params, c.skipNds, false)
			require.NotNil(t, n)
			require.NotNil(t, n.handler)
			assert.False(t, tsr)
			assert.Equal(t, tc.wantMatch, n.path)
			if len(tc.wantParams) == 0 {
				assert.Empty(t, c.Params())
			} else {
				assert.Equal(t, tc.wantParams, c.Params())
			}

			// Test with lazy
			c = newTestContextTree(tree)
			n, tsr = tree.lookup(nds[0], tc.path, c.params, c.skipNds, true)
			require.NotNil(t, n)
			require.NotNil(t, n.handler)
			assert.False(t, tsr)
			assert.Empty(t, c.Params())
			assert.Equal(t, tc.wantMatch, n.path)
		})
	}
}

func TestInsertConflict(t *testing.T) {
	cases := []struct {
		name   string
		routes []struct {
			wantErr   error
			path      string
			wantMatch []string
		}
	}{
		{
			name: "exact match conflict",
			routes: []struct {
				wantErr   error
				path      string
				wantMatch []string
			}{
				{path: "/john/*{x}", wantErr: nil, wantMatch: nil},
				{path: "/john/*{y}", wantErr: ErrRouteConflict, wantMatch: []string{"/john/*{x}"}},
				{path: "/john/", wantErr: ErrRouteExist, wantMatch: nil},
				{path: "/foo/baz", wantErr: nil, wantMatch: nil},
				{path: "/foo/bar", wantErr: nil, wantMatch: nil},
				{path: "/foo/{id}", wantErr: nil, wantMatch: nil},
				{path: "/foo/*{args}", wantErr: nil, wantMatch: nil},
				{path: "/avengers/ironman/{power}", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}/bar", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}/foo", wantErr: nil, wantMatch: nil},
				{path: "/avengers/*{args}", wantErr: nil, wantMatch: nil},
				{path: "/fox/", wantErr: nil, wantMatch: nil},
				{path: "/fox/*{args}", wantErr: ErrRouteExist, wantMatch: nil},
			},
		},
		{
			name: "no conflict for incomplete match to end of edge",
			routes: []struct {
				wantErr   error
				path      string
				wantMatch []string
			}{
				{path: "/foo/bar", wantErr: nil, wantMatch: nil},
				{path: "/foo/baz", wantErr: nil, wantMatch: nil},
				{path: "/foo/*{args}", wantErr: nil, wantMatch: nil},
				{path: "/foo/{id}", wantErr: nil, wantMatch: nil},
			},
		},
		{
			name: "no conflict for key match mid-edge",
			routes: []struct {
				wantErr   error
				path      string
				wantMatch []string
			}{
				{path: "/foo/{id}", wantErr: nil, wantMatch: nil},
				{path: "/foo/*{args}", wantErr: nil, wantMatch: nil},
				{path: "/foo/a*{args}", wantErr: nil, wantMatch: nil},
				{path: "/foo*{args}", wantErr: nil, wantMatch: nil},
				{path: "/john{doe}", wantErr: nil, wantMatch: nil},
				{path: "/john*{doe}", wantErr: nil, wantMatch: nil},
				{path: "/john/{doe}", wantErr: nil, wantMatch: nil},
				{path: "/joh{doe}", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}/foo", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}/bar", wantErr: nil, wantMatch: nil},
				{path: "/avengers/*{args}", wantErr: nil, wantMatch: nil},
			},
		},
		{
			name: "incomplete match to middle of edge",
			routes: []struct {
				wantErr   error
				path      string
				wantMatch []string
			}{
				{path: "/foo/{id}", wantErr: nil, wantMatch: nil},
				{path: "/foo/{abc}", wantErr: ErrRouteConflict, wantMatch: []string{"/foo/{id}"}},
				{path: "/foo{id}", wantErr: nil, wantMatch: nil},
				{path: "/foo/a{id}", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}/bar", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}/baz", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{abc}", wantErr: ErrRouteConflict, wantMatch: []string{"/avengers/{id}", "/avengers/{id}/bar", "/avengers/{id}/baz"}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := New().Tree()
			for _, rte := range tc.routes {
				err := tree.Handle(http.MethodGet, rte.path, emptyHandler)
				assert.ErrorIs(t, err, rte.wantErr)
				if cErr, ok := err.(*RouteConflictError); ok {
					assert.Equal(t, rte.wantMatch, cErr.Matched)
				}
			}
		})
	}
}

func TestUpdateConflict(t *testing.T) {
	cases := []struct {
		name      string
		routes    []string
		update    string
		wantErr   error
		wantMatch []string
	}{
		{
			name:    "wildcard parameter route not registered",
			routes:  []string{"/foo/{bar}"},
			update:  "/foo/{baz}",
			wantErr: ErrRouteNotFound,
		},
		{
			name:    "wildcard catch all route not registered",
			routes:  []string{"/foo/{bar}"},
			update:  "/foo/*{baz}",
			wantErr: ErrRouteNotFound,
		},
		{
			name:    "route match but not a leaf",
			routes:  []string{"/foo/bar/baz"},
			update:  "/foo/bar",
			wantErr: ErrRouteNotFound,
		},
		{
			name:      "wildcard have different name",
			routes:    []string{"/foo/bar", "/foo/*{args}"},
			update:    "/foo/*{all}",
			wantErr:   ErrRouteConflict,
			wantMatch: []string{"/foo/*{args}"},
		},
		{
			name:      "replacing non wildcard by wildcard",
			routes:    []string{"/foo/bar", "/foo/"},
			update:    "/foo/*{all}",
			wantErr:   ErrRouteConflict,
			wantMatch: []string{"/foo/"},
		},
		{
			name:      "replacing wildcard by non wildcard",
			routes:    []string{"/foo/bar", "/foo/*{args}"},
			update:    "/foo/",
			wantErr:   ErrRouteConflict,
			wantMatch: []string{"/foo/*{args}"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := New().Tree()
			for _, rte := range tc.routes {
				require.NoError(t, tree.Handle(http.MethodGet, rte, emptyHandler))
			}
			err := tree.Update(http.MethodGet, tc.update, emptyHandler)
			assert.ErrorIs(t, err, tc.wantErr)
			if cErr, ok := err.(*RouteConflictError); ok {
				assert.Equal(t, tc.wantMatch, cErr.Matched)
			}
		})
	}
}

func TestUpdateRoute(t *testing.T) {
	cases := []struct {
		name   string
		routes []string
		update string
	}{
		{
			name:   "replacing ending static node",
			routes: []string{"/foo/", "/foo/bar", "/foo/baz"},
			update: "/foo/bar",
		},
		{
			name:   "replacing middle static node",
			routes: []string{"/foo/", "/foo/bar", "/foo/baz"},
			update: "/foo/",
		},
		{
			name:   "replacing ending wildcard node",
			routes: []string{"/foo/", "/foo/bar", "/foo/{baz}"},
			update: "/foo/{baz}",
		},
		{
			name:   "replacing ending inflight wildcard node",
			routes: []string{"/foo/", "/foo/bar_xyz", "/foo/bar_{baz}"},
			update: "/foo/bar_{baz}",
		},
		{
			name:   "replacing middle wildcard node",
			routes: []string{"/foo/{bar}", "/foo/{bar}/baz", "/foo/{bar}/xyz"},
			update: "/foo/{bar}",
		},
		{
			name:   "replacing middle inflight wildcard node",
			routes: []string{"/foo/id:{bar}", "/foo/id:{bar}/baz", "/foo/id:{bar}/xyz"},
			update: "/foo/id:{bar}",
		},
		{
			name:   "replacing catch all node",
			routes: []string{"/foo/*{bar}", "/foo", "/foo/bar"},
			update: "/foo/*{bar}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := New().Tree()
			for _, rte := range tc.routes {
				require.NoError(t, tree.Handle(http.MethodGet, rte, emptyHandler))
			}
			assert.NoError(t, tree.Update(http.MethodGet, tc.update, emptyHandler))
		})
	}
}

func TestParseRoute(t *testing.T) {
	cases := []struct {
		wantErr         error
		name            string
		path            string
		wantCatchAllKey string
		wantPath        string
		wantN           int
	}{
		{
			name:     "valid static route",
			path:     "/foo/bar",
			wantPath: "/foo/bar",
		},
		{
			name:            "valid catch all route",
			path:            "/foo/bar/*{arg}",
			wantN:           1,
			wantCatchAllKey: "arg",
			wantPath:        "/foo/bar/",
		},
		{
			name:     "valid param route",
			path:     "/foo/bar/{baz}",
			wantN:    1,
			wantPath: "/foo/bar/{baz}",
		},
		{
			name:     "valid multi params route",
			path:     "/foo/{bar}/{baz}",
			wantN:    2,
			wantPath: "/foo/{bar}/{baz}",
		},
		{
			name:     "valid same params route",
			path:     "/foo/{bar}/{bar}",
			wantN:    2,
			wantPath: "/foo/{bar}/{bar}",
		},
		{
			name:            "valid multi params and catch all route",
			path:            "/foo/{bar}/{baz}/*{arg}",
			wantN:           3,
			wantCatchAllKey: "arg",
			wantPath:        "/foo/{bar}/{baz}/",
		},
		{
			name:     "valid inflight param",
			path:     "/foo/xyz:{bar}",
			wantN:    1,
			wantPath: "/foo/xyz:{bar}",
		},
		{
			name:            "valid inflight catchall",
			path:            "/foo/xyz:*{bar}",
			wantN:           1,
			wantPath:        "/foo/xyz:",
			wantCatchAllKey: "bar",
		},
		{
			name:            "valid multi inflight param and catch all",
			path:            "/foo/xyz:{bar}/abc:{bar}/*{arg}",
			wantN:           3,
			wantCatchAllKey: "arg",
			wantPath:        "/foo/xyz:{bar}/abc:{bar}/",
		},
		{
			name:    "missing prefix slash",
			path:    "foo/bar",
			wantErr: ErrInvalidRoute,
			wantN:   -1,
		},
		{
			name:    "empty parameter",
			path:    "/foo/bar{}",
			wantErr: ErrInvalidRoute,
			wantN:   -1,
		},
		{
			name:    "missing arguments name after catch all",
			path:    "/foo/bar/*",
			wantErr: ErrInvalidRoute,
			wantN:   -1,
		},
		{
			name:    "missing arguments name after param",
			path:    "/foo/bar/{",
			wantErr: ErrInvalidRoute,
			wantN:   -1,
		},
		{
			name:    "catch all in the middle of the route",
			path:    "/foo/bar/*/baz",
			wantErr: ErrInvalidRoute,
			wantN:   -1,
		},
		{
			name:    "catch all with arg in the middle of the route",
			path:    "/foo/bar/*{bar}/baz",
			wantErr: ErrInvalidRoute,
			wantN:   -1,
		},
		{
			name:    "unexpected character in param",
			path:    "/foo/{{bar}",
			wantErr: ErrInvalidRoute,
			wantN:   -1,
		},
		{
			name:    "in flight catch-all after param in one route segment",
			path:    "/foo/{bar}*{baz}",
			wantErr: ErrInvalidRoute,
			wantN:   -1,
		},
		{
			name:    "multiple param in one route segment",
			path:    "/foo/{bar}{baz}",
			wantErr: ErrInvalidRoute,
			wantN:   -1,
		},
		{
			name:    "in flight param after catch all",
			path:    "/foo/*{args}{param}",
			wantErr: ErrInvalidRoute,
			wantN:   -1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, key, n, err := parseRoute(tc.path)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantN, n)
			assert.Equal(t, tc.wantCatchAllKey, key)
			assert.Equal(t, tc.wantPath, path)
		})
	}
}

func TestTree_LookupTsr(t *testing.T) {
	cases := []struct {
		name     string
		paths    []string
		key      string
		want     bool
		wantPath string
	}{
		{
			name:     "match mid edge",
			paths:    []string{"/foo/bar/"},
			key:      "/foo/bar",
			want:     true,
			wantPath: "/foo/bar/",
		},
		{
			name:     "incomplete match end of edge",
			paths:    []string{"/foo/bar"},
			key:      "/foo/bar/",
			want:     true,
			wantPath: "/foo/bar",
		},
		{
			name:     "match mid edge with child node",
			paths:    []string{"/users/", "/users/{id}"},
			key:      "/users",
			want:     true,
			wantPath: "/users/",
		},
		{
			name:     "match mid edge in child node",
			paths:    []string{"/users", "/users/{id}"},
			key:      "/users/",
			want:     true,
			wantPath: "/users",
		},
		{
			name:  "match mid edge in child node with invalid remaining prefix",
			paths: []string{"/users/{id}"},
			key:   "/users/",
		},
		{
			name:  "match mid edge with child node with invalid remaining suffix",
			paths: []string{"/users/{id}"},
			key:   "/users",
		},
		{
			name:  "match mid edge with ts and more char after",
			paths: []string{"/foo/bar/buzz"},
			key:   "/foo/bar",
		},
		{
			name:  "match mid edge with ts and more char before",
			paths: []string{"/foo/barr/"},
			key:   "/foo/bar",
		},
		{
			name:  "incomplete match end of edge with ts and more char after",
			paths: []string{"/foo/bar"},
			key:   "/foo/bar/buzz",
		},
		{
			name:  "incomplete match end of edge with ts and more char before",
			paths: []string{"/foo/bar"},
			key:   "/foo/barr/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := New().Tree()
			for _, path := range tc.paths {
				require.NoError(t, tree.insert(http.MethodGet, path, "", 0, emptyHandler))
			}
			nds := *tree.nodes.Load()
			c := newTestContextTree(tree)
			n, got := tree.lookup(nds[0], tc.key, c.params, c.skipNds, true)
			assert.Equal(t, tc.want, got)
			if tc.want {
				require.NotNil(t, n)
				assert.Equal(t, tc.wantPath, n.path)
			}
		})
	}
}

func TestRouterWithIgnoreTrailingSlash(t *testing.T) {
	cases := []struct {
		name     string
		paths    []string
		req      string
		method   string
		wantCode int
		wantPath string
	}{
		{
			name:     "current not a leaf with extra ts",
			paths:    []string{"/foo", "/foo/x/", "/foo/z/"},
			req:      "/foo/",
			method:   http.MethodGet,
			wantCode: http.StatusOK,
			wantPath: "/foo",
		},
		{
			name:     "current not a leaf and path does not end with ts",
			paths:    []string{"/foo", "/foo/x/", "/foo/z/"},
			req:      "/foo/c",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "current not a leaf and path end with extra char and ts",
			paths:    []string{"/foo", "/foo/x/", "/foo/z/"},
			req:      "/foo/c/",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "current not a leaf and path end with ts but last is not a leaf",
			paths:    []string{"/foo/a/a", "/foo/a/b", "/foo/c/"},
			req:      "/foo/a/",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "mid edge key with extra ts",
			paths:    []string{"/foo/bar/"},
			req:      "/foo/bar",
			method:   http.MethodGet,
			wantCode: http.StatusOK,
			wantPath: "/foo/bar/",
		},
		{
			name:     "mid edge key with without extra ts",
			paths:    []string{"/foo/bar/baz", "/foo/bar"},
			req:      "/foo/bar/",
			method:   http.MethodGet,
			wantCode: http.StatusOK,
			wantPath: "/foo/bar",
		},
		{
			name:     "mid edge key without extra ts",
			paths:    []string{"/foo/bar/baz", "/foo/bar"},
			req:      "/foo/bar/",
			method:   http.MethodPost,
			wantCode: http.StatusOK,
			wantPath: "/foo/bar",
		},
		{
			name:     "incomplete match end of edge",
			paths:    []string{"/foo/bar"},
			req:      "/foo/bar/",
			method:   http.MethodGet,
			wantCode: http.StatusOK,
			wantPath: "/foo/bar",
		},
		{
			name:     "match mid edge with ts and more char after",
			paths:    []string{"/foo/bar/buzz"},
			req:      "/foo/bar",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "match mid edge with ts and more char before",
			paths:    []string{"/foo/barr/"},
			req:      "/foo/bar",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "incomplete match end of edge with ts and more char after",
			paths:    []string{"/foo/bar"},
			req:      "/foo/bar/buzz",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "incomplete match end of edge with ts and more char before",
			paths:    []string{"/foo/bar"},
			req:      "/foo/barr/",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New(WithIgnoreTrailingSlash(true))
			for _, path := range tc.paths {
				require.NoError(t, r.Tree().Handle(tc.method, path, func(c Context) {
					_ = c.String(http.StatusOK, c.Path())
				}))
			}

			req := httptest.NewRequest(tc.method, tc.req, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			if tc.wantPath != "" {
				assert.Equal(t, tc.wantPath, w.Body.String())
			}
		})
	}
}

func TestRedirectTrailingSlash(t *testing.T) {

	cases := []struct {
		name         string
		paths        []string
		req          string
		method       string
		wantCode     int
		wantLocation string
	}{
		{
			name:         "current not a leaf get method and status moved permanently with extra ts",
			paths:        []string{"/foo", "/foo/x/", "/foo/z/"},
			req:          "/foo/",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "../foo",
		},
		{
			name:         "current not a leaf post method and status moved permanently with extra ts",
			paths:        []string{"/foo", "/foo/x/", "/foo/z/"},
			req:          "/foo/",
			method:       http.MethodPost,
			wantCode:     http.StatusPermanentRedirect,
			wantLocation: "../foo",
		},
		{
			name:     "current not a leaf and path does not end with ts",
			paths:    []string{"/foo", "/foo/x/", "/foo/z/"},
			req:      "/foo/c",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "current not a leaf and path end with extra char and ts",
			paths:    []string{"/foo", "/foo/x/", "/foo/z/"},
			req:      "/foo/c/",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "current not a leaf and path end with ts but last is not a leaf",
			paths:    []string{"/foo/a/a", "/foo/a/b", "/foo/c/"},
			req:      "/foo/a/",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:         "mid edge key with get method and status moved permanently with extra ts",
			paths:        []string{"/foo/bar/"},
			req:          "/foo/bar",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "bar/",
		},
		{
			name:         "mid edge key with post method and status permanent redirect with extra ts",
			paths:        []string{"/foo/bar/"},
			req:          "/foo/bar",
			method:       http.MethodPost,
			wantCode:     http.StatusPermanentRedirect,
			wantLocation: "bar/",
		},
		{
			name:         "mid edge key with get method and status moved permanently without extra ts",
			paths:        []string{"/foo/bar/baz", "/foo/bar"},
			req:          "/foo/bar/",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "../bar",
		},
		{
			name:         "mid edge key with post method and status permanent redirect without extra ts",
			paths:        []string{"/foo/bar/baz", "/foo/bar"},
			req:          "/foo/bar/",
			method:       http.MethodPost,
			wantCode:     http.StatusPermanentRedirect,
			wantLocation: "../bar",
		},
		{
			name:         "incomplete match end of edge with get method",
			paths:        []string{"/foo/bar"},
			req:          "/foo/bar/",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "../bar",
		},
		{
			name:         "incomplete match end of edge with post method",
			paths:        []string{"/foo/bar"},
			req:          "/foo/bar/",
			method:       http.MethodPost,
			wantCode:     http.StatusPermanentRedirect,
			wantLocation: "../bar",
		},
		{
			name:     "match mid edge with ts and more char after",
			paths:    []string{"/foo/bar/buzz"},
			req:      "/foo/bar",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "match mid edge with ts and more char before",
			paths:    []string{"/foo/barr/"},
			req:      "/foo/bar",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "incomplete match end of edge with ts and more char after",
			paths:    []string{"/foo/bar"},
			req:      "/foo/bar/buzz",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "incomplete match end of edge with ts and more char before",
			paths:    []string{"/foo/bar"},
			req:      "/foo/barr/",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New(WithRedirectTrailingSlash(true))
			for _, path := range tc.paths {
				require.NoError(t, r.Tree().Handle(tc.method, path, emptyHandler))
			}

			req := httptest.NewRequest(tc.method, tc.req, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			if w.Code == http.StatusPermanentRedirect || w.Code == http.StatusMovedPermanently {
				assert.Equal(t, tc.wantLocation, w.Header().Get("Location"))
			}
		})
	}
}

func TestEncodedRedirectTrailingSlash(t *testing.T) {
	r := New(WithRedirectTrailingSlash(true))
	require.NoError(t, r.Handle(http.MethodGet, "/foo/{bar}/", emptyHandler))

	req := httptest.NewRequest(http.MethodGet, "/foo/bar%2Fbaz", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "bar%2Fbaz/", w.Header().Get(HeaderLocation))
}

func TestTree_Remove(t *testing.T) {
	tree := New().Tree()
	routes := make([]route, len(githubAPI))
	copy(routes, githubAPI)

	for _, rte := range routes {
		require.NoError(t, tree.Handle(rte.method, rte.path, emptyHandler))
	}

	rand.Shuffle(len(routes), func(i, j int) { routes[i], routes[j] = routes[j], routes[i] })

	for _, rte := range routes {
		require.NoError(t, tree.Remove(rte.method, rte.path))
	}

	cnt := 0
	_ = Walk(tree, func(method, path string, handler HandlerFunc) error {
		cnt++
		return nil
	})

	assert.Equal(t, 0, cnt)
	assert.Equal(t, 4, len(*tree.nodes.Load()))
}

func TestTree_RemoveRoot(t *testing.T) {
	tree := New().Tree()
	require.NoError(t, tree.Handle(http.MethodOptions, "/foo/bar", emptyHandler))
	require.NoError(t, tree.Remove(http.MethodOptions, "/foo/bar"))
	assert.Equal(t, 4, len(*tree.nodes.Load()))
}

func TestTree_Methods(t *testing.T) {
	f := New()
	for _, rte := range githubAPI {
		require.NoError(t, f.Handle(rte.method, rte.path, emptyHandler))
	}

	methods := f.Tree().Methods("/gists/123/star")
	assert.Equal(t, []string{"DELETE", "GET", "PUT"}, methods)

	methods = f.Tree().Methods("*")
	assert.Equal(t, []string{"DELETE", "GET", "POST", "PUT"}, methods)

	// Ignore trailing slash disable
	methods = f.Tree().Methods("/gists/123/star/")
	assert.Empty(t, methods)
}

func TestTree_MethodsWithIgnoreTsEnable(t *testing.T) {
	f := New(WithIgnoreTrailingSlash(true))
	for _, method := range []string{"DELETE", "GET", "PUT"} {
		require.NoError(t, f.Handle(method, "/foo/bar", emptyHandler))
		require.NoError(t, f.Handle(method, "/john/doe/", emptyHandler))
	}

	methods := f.Tree().Methods("/foo/bar/")
	assert.Equal(t, []string{"DELETE", "GET", "PUT"}, methods)

	methods = f.Tree().Methods("/john/doe")
	assert.Equal(t, []string{"DELETE", "GET", "PUT"}, methods)

	methods = f.Tree().Methods("/foo/bar/baz")
	assert.Empty(t, methods)
}

func TestRouterWithAllowedMethod(t *testing.T) {
	r := New(WithNoMethod(true))

	cases := []struct {
		name    string
		target  string
		path    string
		want    string
		methods []string
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
				require.NoError(t, r.Tree().Handle(method, tc.path, emptyHandler))
			}
			req := httptest.NewRequest(tc.target, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("Allow"))
		})
	}
}

func TestRouterWithAllowedMethodAndIgnoreTsEnable(t *testing.T) {
	r := New(WithNoMethod(true), WithIgnoreTrailingSlash(true))

	// Support for ignore Trailing slash
	cases := []struct {
		name    string
		target  string
		path    string
		req     string
		want    string
		methods []string
	}{
		{
			name:    "all route except the last one",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead},
			path:    "/foo/bar/",
			req:     "/foo/bar",
			target:  http.MethodTrace,
			want:    "GET, POST, PUT, DELETE, PATCH, CONNECT, OPTIONS, HEAD",
		},
		{
			name:    "all route except the first one",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz",
			req:     "/foo/baz/",
			target:  http.MethodGet,
			want:    "POST, PUT, DELETE, PATCH, CONNECT, OPTIONS, HEAD, TRACE",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range tc.methods {
				require.NoError(t, r.Tree().Handle(method, tc.path, emptyHandler))
			}
			req := httptest.NewRequest(tc.target, tc.req, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("Allow"))
		})
	}
}

func TestRouterWithAllowedMethodAndIgnoreTsDisable(t *testing.T) {
	r := New(WithNoMethod(true))

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
				require.NoError(t, r.Tree().Handle(method, tc.path, emptyHandler))
			}
			req := httptest.NewRequest(tc.target, tc.req, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func TestRouterWithMethodNotAllowedHandler(t *testing.T) {
	f := New(WithNoMethodHandler(func(c Context) {
		c.SetHeader("FOO", "BAR")
		c.Writer().WriteHeader(http.StatusMethodNotAllowed)
	}))

	require.NoError(t, f.Handle(http.MethodPost, "/foo/bar", emptyHandler))
	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.Equal(t, "POST", w.Header().Get("Allow"))
	assert.Equal(t, "BAR", w.Header().Get("FOO"))
}

func TestRouterWithAutomaticOptions(t *testing.T) {
	f := New(WithAutoOptions(true))

	cases := []struct {
		name     string
		target   string
		path     string
		want     string
		wantCode int
		methods  []string
	}{
		{
			name:     "system-wide requests",
			target:   "*",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			want:     "GET, PUT, TRACE, OPTIONS",
			wantCode: http.StatusOK,
		},
		{
			name:     "system-wide with custom options registered",
			target:   "*",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     "GET, PUT, TRACE, OPTIONS",
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
			want:     "GET, PUT, TRACE, OPTIONS",
			wantCode: http.StatusOK,
		},
		{
			name:     "regular option request with handler priority",
			target:   "/foo",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     "GET, OPTIONS, PUT, TRACE",
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
			for _, method := range tc.methods {
				require.NoError(t, f.Tree().Handle(method, tc.path, func(c Context) {
					c.SetHeader("Allow", strings.Join(c.Tree().Methods(c.Request().URL.Path), ", "))
					c.Writer().WriteHeader(http.StatusNoContent)
				}))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("Allow"))
			// Reset
			f.Swap(f.NewTree())
		})
	}
}

func TestRouterWithAutomaticOptionsAndIgnoreTsOptionEnable(t *testing.T) {
	f := New(WithAutoOptions(true), WithIgnoreTrailingSlash(true))

	cases := []struct {
		name     string
		target   string
		path     string
		want     string
		wantCode int
		methods  []string
	}{
		{
			name:     "system-wide requests",
			target:   "*",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			want:     "GET, PUT, TRACE, OPTIONS",
			wantCode: http.StatusOK,
		},
		{
			name:     "system-wide with custom options registered",
			target:   "*",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     "GET, PUT, TRACE, OPTIONS",
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
			want:     "GET, PUT, TRACE, OPTIONS",
			wantCode: http.StatusOK,
		},
		{
			name:     "regular option request with handler priority and ignore ts",
			target:   "/foo",
			path:     "/foo/",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     "GET, OPTIONS, PUT, TRACE",
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
			for _, method := range tc.methods {
				require.NoError(t, f.Tree().Handle(method, tc.path, func(c Context) {
					c.SetHeader("Allow", strings.Join(c.Tree().Methods(c.Request().URL.Path), ", "))
					c.Writer().WriteHeader(http.StatusNoContent)
				}))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("Allow"))
			// Reset
			f.Swap(f.NewTree())
		})
	}
}

func TestRouterWithAutomaticOptionsAndIgnoreTsOptionDisable(t *testing.T) {
	f := New(WithAutoOptions(true))

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
			for _, method := range tc.methods {
				require.NoError(t, f.Tree().Handle(method, tc.path, func(c Context) {
					c.SetHeader("Allow", strings.Join(c.Tree().Methods(c.Request().URL.Path), ", "))
					c.Writer().WriteHeader(http.StatusNoContent)
				}))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			// Reset
			f.Swap(f.NewTree())
		})
	}
}

func TestRouterWithOptionsHandler(t *testing.T) {
	f := New(WithOptionsHandler(func(c Context) {
		assert.Equal(t, "/foo/bar", c.Path())
		c.Writer().WriteHeader(http.StatusNoContent)
	}))

	require.NoError(t, f.Handle(http.MethodGet, "/foo/bar", emptyHandler))
	require.NoError(t, f.Handle(http.MethodPost, "/foo/bar", emptyHandler))

	req := httptest.NewRequest(http.MethodOptions, "/foo/bar", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "GET, POST, OPTIONS", w.Header().Get("Allow"))
}

func TestDefaultOptions(t *testing.T) {
	m := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			next(c)
		}
	})
	r := New(WithMiddleware(m), DefaultOptions())
	assert.Equal(t, reflect.ValueOf(m).Pointer(), reflect.ValueOf(r.mws[1].m).Pointer())
	assert.True(t, r.handleOptions)
}

func TestWithScopedMiddleware(t *testing.T) {
	called := false
	m := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			called = true
			next(c)
		}
	})

	r := New(WithMiddlewareFor(NoRouteHandler, m))
	require.NoError(t, r.Handle(http.MethodGet, "/foo/bar", emptyHandler))

	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.False(t, called)
	req.URL.Path = "/foo"
	r.ServeHTTP(w, req)
	assert.True(t, called)
}

func TestWithNotFoundHandler(t *testing.T) {
	notFound := func(c Context) {
		_ = c.String(http.StatusNotFound, "NOT FOUND\n")
	}

	f := New(WithNoRouteHandler(notFound))
	require.NoError(t, f.Handle(http.MethodGet, "/foo", emptyHandler))

	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := httptest.NewRecorder()

	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "NOT FOUND\n", w.Body.String())
}

func TestRouter_Lookup(t *testing.T) {
	rx := regexp.MustCompile("({|\\*{)[A-z]+[}]")
	f := New()
	for _, rte := range githubAPI {
		require.NoError(t, f.Handle(rte.method, rte.path, emptyHandler))
	}

	for _, rte := range githubAPI {
		req := httptest.NewRequest(rte.method, rte.path, nil)
		handler, cc, _ := f.Lookup(mockResponseWriter{}, req)
		require.NotNil(t, cc)
		assert.NotNil(t, handler)

		matches := rx.FindAllString(rte.path, -1)
		for _, match := range matches {
			var key string
			if strings.HasPrefix(match, "*") {
				key = match[2 : len(match)-1]
			} else {
				key = match[1 : len(match)-1]
			}
			value := match
			assert.Equal(t, value, cc.Param(key))
		}

		cc.Close()
	}

	// No method match
	req := httptest.NewRequest("ANY", "/bar", nil)
	handler, cc, _ := f.Lookup(mockResponseWriter{}, req)
	assert.Nil(t, handler)
	assert.Nil(t, cc)

	// No path match
	req = httptest.NewRequest(http.MethodGet, "/bar", nil)
	handler, cc, _ = f.Lookup(mockResponseWriter{}, req)
	assert.Nil(t, handler)
	assert.Nil(t, cc)
}

func TestTree_Has(t *testing.T) {
	routes := []string{
		"/foo/bar",
		"/welcome/{name}",
		"/users/uid_{id}",
	}

	r := New()
	for _, rte := range routes {
		require.NoError(t, r.Handle(http.MethodGet, rte, emptyHandler))
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "strict match static route",
			path: "/foo/bar",
			want: true,
		},
		{
			name: "no match static route (no tsr)",
			path: "/foo/bar/",
		},
		{
			name: "strict match route params",
			path: "/welcome/{name}",
			want: true,
		},
		{
			name: "no match route params",
			path: "/welcome/fox",
		},
		{
			name: "strict match mid route params",
			path: "/users/uid_{id}",
			want: true,
		},
		{
			name: "no match mid route params",
			path: "/users/uid_123",
		},
	}

	tree := r.Tree()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tree.Has(http.MethodGet, tc.path))
		})
	}
}

func TestTree_HasWithIgnoreTrailingSlashEnable(t *testing.T) {
	routes := []string{
		"/foo/bar",
		"/welcome/{name}/",
		"/users/uid_{id}",
	}

	r := New(WithIgnoreTrailingSlash(true))
	for _, rte := range routes {
		require.NoError(t, r.Handle(http.MethodGet, rte, emptyHandler))
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "strict match static route",
			path: "/foo/bar",
			want: true,
		},
		{
			name: "no match static route with tsr",
			path: "/foo/bar/",
			want: true,
		},
		{
			name: "strict match route params",
			path: "/welcome/{name}/",
			want: true,
		},
		{
			name: "strict match route params with tsr",
			path: "/welcome/{name}",
			want: true,
		},
		{
			name: "no match route params with ts",
			path: "/welcome/fox",
		},
		{
			name: "no match route params without ts",
			path: "/welcome/fox/",
		},
		{
			name: "strict match mid route params",
			path: "/users/uid_{id}",
			want: true,
		},
		{
			name: "strict match mid route params with tsr",
			path: "/users/uid_{id}/",
			want: true,
		},
		{
			name: "no match mid route params without ts",
			path: "/users/uid_123",
		},
		{
			name: "no match mid route params with ts",
			path: "/users/uid_123",
		},
	}

	tree := r.Tree()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tree.Has(http.MethodGet, tc.path))
		})
	}
}

func TestTree_Match(t *testing.T) {
	routes := []string{
		"/foo/bar",
		"/welcome/{name}",
		"/users/uid_{id}",
	}

	r := New()
	for _, rte := range routes {
		require.NoError(t, r.Handle(http.MethodGet, rte, emptyHandler))
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "reverse static route",
			path: "/foo/bar",
			want: "/foo/bar",
		},
		{
			name: "reverse static route with tsr disable",
			path: "/foo/bar/",
		},
		{
			name: "reverse params route",
			path: "/welcome/fox",
			want: "/welcome/{name}",
		},
		{
			name: "reverse mid params route",
			path: "/users/uid_123",
			want: "/users/uid_{id}",
		},
		{
			name: "reverse no match",
			path: "/users/fox",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, r.Tree().Match(http.MethodGet, tc.path))
		})
	}
}

func TestTree_MatchWithIgnoreTrailingSlashEnable(t *testing.T) {
	routes := []string{
		"/foo/bar",
		"/welcome/{name}/",
		"/users/uid_{id}",
	}

	r := New(WithIgnoreTrailingSlash(true))
	for _, rte := range routes {
		require.NoError(t, r.Handle(http.MethodGet, rte, emptyHandler))
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "reverse static route",
			path: "/foo/bar",
			want: "/foo/bar",
		},
		{
			name: "reverse static route with tsr",
			path: "/foo/bar/",
			want: "/foo/bar",
		},
		{
			name: "reverse params route",
			path: "/welcome/fox/",
			want: "/welcome/{name}/",
		},
		{
			name: "reverse params route with tsr",
			path: "/welcome/fox",
			want: "/welcome/{name}/",
		},
		{
			name: "reverse mid params route",
			path: "/users/uid_123",
			want: "/users/uid_{id}",
		},
		{
			name: "reverse mid params route with tsr",
			path: "/users/uid_123/",
			want: "/users/uid_{id}",
		},
		{
			name: "reverse no match",
			path: "/users/fox",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, r.Tree().Match(http.MethodGet, tc.path))
		})
	}
}

func TestEncodedPath(t *testing.T) {
	encodedPath := "run/cmd/S123L%2FA"
	req := httptest.NewRequest(http.MethodGet, "/"+encodedPath, nil)
	w := httptest.NewRecorder()

	r := New()
	r.MustHandle(http.MethodGet, "/*{request}", func(c Context) {
		_ = c.String(http.StatusOK, "%s", c.Param("request"))
	})

	r.ServeHTTP(w, req)
	assert.Equal(t, encodedPath, w.Body.String())
}

func TestFuzzInsertLookupParam(t *testing.T) {
	// no '*', '{}' and '/' and invalid escape char
	unicodeRanges := fuzz.UnicodeRanges{
		{First: 0x20, Last: 0x29},
		{First: 0x2B, Last: 0x2E},
		{First: 0x30, Last: 0x7A},
		{First: 0x7C, Last: 0x7C},
		{First: 0x7E, Last: 0x04FF},
	}

	tree := New().Tree()
	f := fuzz.New().NilChance(0).Funcs(unicodeRanges.CustomStringFuzzFunc())
	routeFormat := "/%s/{%s}/%s/{%s}/{%s}"
	reqFormat := "/%s/%s/%s/%s/%s"
	for i := 0; i < 2000; i++ {
		var s1, e1, s2, e2, e3 string
		f.Fuzz(&s1)
		f.Fuzz(&e1)
		f.Fuzz(&s2)
		f.Fuzz(&e2)
		f.Fuzz(&e3)
		if s1 == "" || s2 == "" || e1 == "" || e2 == "" || e3 == "" {
			continue
		}
		if err := tree.insert(http.MethodGet, fmt.Sprintf(routeFormat, s1, e1, s2, e2, e3), "", 3, emptyHandler); err == nil {
			nds := *tree.nodes.Load()

			c := newTestContextTree(tree)
			n, tsr := tree.lookup(nds[0], fmt.Sprintf(reqFormat, s1, "xxxx", s2, "xxxx", "xxxx"), c.params, c.skipNds, false)
			require.NotNil(t, n)
			assert.False(t, tsr)
			assert.Equal(t, fmt.Sprintf(routeFormat, s1, e1, s2, e2, e3), n.path)
			assert.Equal(t, "xxxx", c.Param(e1))
			assert.Equal(t, "xxxx", c.Param(e2))
			assert.Equal(t, "xxxx", c.Param(e3))
		}
	}
}

func TestFuzzInsertNoPanics(t *testing.T) {
	f := fuzz.New().NilChance(0).NumElements(5000, 10000)
	tree := New().Tree()

	routes := make(map[string]struct{})
	f.Fuzz(&routes)

	for rte := range routes {
		var catchAllKey string
		f.Fuzz(&catchAllKey)
		if rte == "" {
			continue
		}
		require.NotPanicsf(t, func() {
			_ = tree.insert(http.MethodGet, rte, catchAllKey, 0, emptyHandler)
		}, fmt.Sprintf("rte: %s, catch all: %s", rte, catchAllKey))
	}
}

func TestFuzzInsertLookupUpdateAndDelete(t *testing.T) {
	// no '*' and '{}' and invalid escape char
	unicodeRanges := fuzz.UnicodeRanges{
		{First: 0x20, Last: 0x29},
		{First: 0x2B, Last: 0x7A},
		{First: 0x7C, Last: 0x7C},
		{First: 0x7E, Last: 0x04FF},
	}

	f := fuzz.New().NilChance(0).NumElements(1000, 2000).Funcs(unicodeRanges.CustomStringFuzzFunc())
	tree := New().Tree()

	routes := make(map[string]struct{})
	f.Fuzz(&routes)

	for rte := range routes {
		err := tree.insert(http.MethodGet, "/"+rte, "", 0, emptyHandler)
		require.NoError(t, err)
	}

	countPath := 0
	require.NoError(t, Walk(tree, func(method, path string, handler HandlerFunc) error {
		countPath++
		return nil
	}))
	assert.Equal(t, len(routes), countPath)

	for rte := range routes {
		nds := *tree.nodes.Load()
		c := newTestContextTree(tree)
		n, tsr := tree.lookup(nds[0], "/"+rte, c.params, c.skipNds, true)
		require.NotNilf(t, n, "route /%s", rte)
		require.Falsef(t, tsr, "tsr: %t", tsr)
		require.Truef(t, n.isLeaf(), "route /%s", rte)
		require.Equal(t, "/"+rte, n.path)
		require.NoError(t, tree.update(http.MethodGet, "/"+rte, "", emptyHandler))
	}

	for rte := range routes {
		deleted := tree.remove(http.MethodGet, "/"+rte)
		require.True(t, deleted)
	}

	countPath = 0
	require.NoError(t, Walk(tree, func(method, path string, handler HandlerFunc) error {
		countPath++
		return nil
	}))
	assert.Equal(t, 0, countPath)
}

func TestDataRace(t *testing.T) {
	var wg sync.WaitGroup
	start, wait := atomicSync()

	h := HandlerFunc(func(c Context) {})
	newH := HandlerFunc(func(c Context) {})

	r := New()

	w := new(mockResponseWriter)

	wg.Add(len(githubAPI) * 3)
	for _, rte := range githubAPI {
		go func(method, route string) {
			wait()
			defer wg.Done()
			tree := r.Tree()
			tree.Lock()
			defer tree.Unlock()
			if tree.Has(method, route) {
				assert.NoError(t, tree.Update(method, route, h))
				return
			}
			assert.NoError(t, tree.Handle(method, route, h))
			// assert.NoError(t, r.Handle("PING", route, h))
		}(rte.method, rte.path)

		go func(method, route string) {
			wait()
			defer wg.Done()
			tree := r.Tree()
			tree.Lock()
			defer tree.Unlock()
			if tree.Has(method, route) {
				assert.NoError(t, tree.Remove(method, route))
				return
			}
			assert.NoError(t, tree.Handle(method, route, newH))
		}(rte.method, rte.path)

		go func(method, route string) {
			wait()
			req := httptest.NewRequest(method, route, nil)
			r.ServeHTTP(w, req)
			wg.Done()
		}(rte.method, rte.path)
	}

	time.Sleep(500 * time.Millisecond)
	start()
	wg.Wait()
}

func TestConcurrentRequestHandling(t *testing.T) {
	r := New()

	// /repos/{owner}/{repo}/keys
	h1 := HandlerFunc(func(c Context) {
		assert.Equal(t, "john", c.Param("owner"))
		assert.Equal(t, "fox", c.Param("repo"))
		_ = c.String(200, c.Path())
	})

	// /repos/{owner}/{repo}/contents/*{path}
	h2 := HandlerFunc(func(c Context) {
		assert.Equal(t, "alex", c.Param("owner"))
		assert.Equal(t, "vault", c.Param("repo"))
		assert.Equal(t, "file.txt", c.Param("path"))
		_ = c.String(200, c.Path())
	})

	// /users/{user}/received_events/public
	h3 := HandlerFunc(func(c Context) {
		assert.Equal(t, "go", c.Param("user"))
		_ = c.String(200, c.Path())
	})

	require.NoError(t, r.Handle(http.MethodGet, "/repos/{owner}/{repo}/keys", h1))
	require.NoError(t, r.Handle(http.MethodGet, "/repos/{owner}/{repo}/contents/*{path}", h2))
	require.NoError(t, r.Handle(http.MethodGet, "/users/{user}/received_events/public", h3))

	r1 := httptest.NewRequest(http.MethodGet, "/repos/john/fox/keys", nil)
	r2 := httptest.NewRequest(http.MethodGet, "/repos/alex/vault/contents/file.txt", nil)
	r3 := httptest.NewRequest(http.MethodGet, "/users/go/received_events/public", nil)

	var wg sync.WaitGroup
	wg.Add(300)
	start, wait := atomicSync()
	for i := 0; i < 100; i++ {
		go func() {
			defer wg.Done()
			wait()
			w := httptest.NewRecorder()
			r.ServeHTTP(w, r1)
			assert.Equal(t, "/repos/{owner}/{repo}/keys", w.Body.String())
		}()

		go func() {
			defer wg.Done()
			wait()
			w := httptest.NewRecorder()
			r.ServeHTTP(w, r2)
			assert.Equal(t, "/repos/{owner}/{repo}/contents/*{path}", w.Body.String())
		}()

		go func() {
			defer wg.Done()
			wait()
			w := httptest.NewRecorder()
			r.ServeHTTP(w, r3)
			assert.Equal(t, "/users/{user}/received_events/public", w.Body.String())
		}()
	}

	start()
	wg.Wait()
}

func atomicSync() (start func(), wait func()) {
	var n int32

	start = func() {
		atomic.StoreInt32(&n, 1)
	}

	wait = func() {
		for atomic.LoadInt32(&n) != 1 {
			time.Sleep(1 * time.Microsecond)
		}
	}

	return
}

// This example demonstrates how to create a simple router using the default options,
// which include the Recovery middleware. A basic route is defined, along with a
// custom middleware to log the request metrics.
func ExampleNew() {

	// Create a new router with default options, which include the Recovery middleware
	r := New(DefaultOptions())

	// Define a custom middleware to measure the time taken for request processing and
	// log the URL, route, time elapsed, and status code
	metrics := func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			start := time.Now()
			next(c)
			log.Printf("url=%s; route=%s; time=%d; status=%d", c.Request().URL, c.Path(), time.Since(start), c.Writer().Status())
		}
	}

	// Define a route with the path "/hello/{name}", apply the custom "metrics" middleware,
	// and set a simple handler that greets the user by their name
	r.MustHandle(http.MethodGet, "/hello/{name}", metrics(func(c Context) {
		_ = c.String(200, "Hello %s\n", c.Param("name"))
	}))

	// Start the HTTP server using the router as the handler and listen on port 8080
	log.Fatalln(http.ListenAndServe(":8080", r))
}

// This example demonstrates how to register a global middleware that will be
// applied to all routes.

func ExampleWithMiddleware() {

	// Define a custom middleware to measure the time taken for request processing and
	// log the URL, route, time elapsed, and status code
	metrics := func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			start := time.Now()
			next(c)
			log.Printf(
				"url=%s; route=%s; time=%d; status=%d",
				c.Request().URL,
				c.Path(),
				time.Since(start),
				c.Writer().Status(),
			)
		}
	}

	r := New(WithMiddleware(metrics))

	r.MustHandle(http.MethodGet, "/hello/{name}", func(c Context) {
		_ = c.String(200, "Hello %s\n", c.Param("name"))
	})
}

// This example demonstrates some important considerations when using the Tree API.
func ExampleRouter_Tree() {
	r := New()

	// Each tree as its own sync.Mutex that is used to lock write on the tree. Since the router tree may be swapped at
	// any given time, you MUST always copy the pointer locally, This ensures that you do not inadvertently cause a
	// deadlock by locking/unlocking the wrong tree.
	tree := r.Tree()
	upsert := func(method, path string, handler HandlerFunc) error {
		tree.Lock()
		defer tree.Unlock()
		if tree.Has(method, path) {
			return tree.Update(method, path, handler)
		}
		return tree.Handle(method, path, handler)
	}

	_ = upsert(http.MethodGet, "/foo/bar", func(c Context) {
		// Note the tree accessible from fox.Context is already a local copy so the golden rule above does not apply.
		c.Tree().Lock()
		defer c.Tree().Unlock()
		_ = c.String(200, "foo bar")
	})

	// Bad, instead make a local copy of the tree!
	_ = func(method, path string, handler HandlerFunc) error {
		r.Tree().Lock()
		defer r.Tree().Unlock()
		if r.Tree().Has(method, path) {
			return r.Tree().Update(method, path, handler)
		}
		return r.Tree().Handle(method, path, handler)
	}
}

// This example demonstrates how to create a custom middleware that cleans the request path and performs a manual
// lookup on the tree. If the cleaned path matches a registered route, the client is redirected with a 301 status
// code (Moved Permanently).
func ExampleTree_Match() {
	redirectFixedPath := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			req := c.Request()

			cleanedPath := CleanPath(req.URL.Path)
			if match := c.Tree().Match(req.Method, cleanedPath); match != "" {
				// 301 redirect and returns.
				req.URL.Path = cleanedPath
				http.Redirect(c.Writer(), req, req.URL.String(), http.StatusMovedPermanently)
				return
			}

			next(c)
		}
	})

	f := New(
		// Register the middleware for the NoRouteHandler scope.
		WithMiddlewareFor(NoRouteHandler, redirectFixedPath),
	)

	f.MustHandle(http.MethodGet, "/foo/bar", func(c Context) {
		_ = c.String(http.StatusOK, "foo bar")
	})
}
