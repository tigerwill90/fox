// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"fmt"
	"github.com/tigerwill90/fox/internal/iterutil"
	"github.com/tigerwill90/fox/internal/netutil"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	emptyHandler   = HandlerFunc(func(c Context) {})
	pathHandler    = HandlerFunc(func(c Context) { _ = c.String(200, c.Path()) })
	patternHandler = HandlerFunc(func(c Context) { _ = c.String(200, c.Pattern()) })
)

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

// Clone of staticRoutes with hostname transformation
var staticHostnames = []route{
	{"GET", "cmd.html"},
	{"GET", "code.html"},
	{"GET", "contrib.html"},
	{"GET", "contribute.html"},
	{"GET", "debugging_with_gdb.html"},
	{"GET", "docs.html"},
	{"GET", "effective_go.html"},
	{"GET", "files.log"},
	{"GET", "gccgo_contribute.html"},
	{"GET", "gccgo_install.html"},
	{"GET", "go-logo-black.png"},
	{"GET", "go-logo-blue.png"},
	{"GET", "go-logo-white.png"},
	{"GET", "go1.1.html"},
	{"GET", "go1.2.html"},
	{"GET", "go1.html"},
	{"GET", "go1compat.html"},
	{"GET", "go_faq.html"},
	{"GET", "go_mem.html"},
	{"GET", "go_spec.html"},
	{"GET", "help.html"},
	{"GET", "ie.css"},
	{"GET", "install-source.html"},
	{"GET", "install.html"},
	{"GET", "logo-153x55.png"},
	{"GET", "Makefile"},
	{"GET", "root.html"},
	{"GET", "share.png"},
	{"GET", "sieve.gif"},
	{"GET", "tos.html"},
	{"GET", "articles"},
	{"GET", "articles.go_command.html"},
	{"GET", "articles.index.html"},
	{"GET", "articles.wiki"},
	{"GET", "articles.wiki.edit.html"},
	{"GET", "articles.wiki.final-noclosure.go"},
	{"GET", "articles.wiki.final-noerror.go"},
	{"GET", "articles.wiki.final-parsetemplate.go"},
	{"GET", "articles.wiki.final-template.go"},
	{"GET", "articles.wiki.final.go"},
	{"GET", "articles.wiki.get.go"},
	{"GET", "articles.wiki.http-sample.go"},
	{"GET", "articles.wiki.index.html"},
	{"GET", "articles.wiki.Makefile"},
	{"GET", "articles.wiki.notemplate.go"},
	{"GET", "articles.wiki.part1-noerror.go"},
	{"GET", "articles.wiki.part1.go"},
	{"GET", "articles.wiki.part2.go"},
	{"GET", "iptv-sfr"},
	{"GET", "articles.wiki.part3.go"},
	{"GET", "articles.wiki.test.bash"},
	{"GET", "articles.wiki.test_edit.good"},
	{"GET", "articles.wiki.test_Test.txt.good"},
	{"GET", "articles.wiki.test_view.good"},
	{"GET", "articles.wiki.view.html"},
	{"GET", "codewalk"},
	{"GET", "codewalk.codewalk.css"},
	{"GET", "codewalk.codewalk.js"},
	{"GET", "codewalk.codewalk.xml"},
	{"GET", "codewalk.functions.xml"},
	{"GET", "codewalk.markov.go"},
	{"GET", "codewalk.markov.xml"},
	{"GET", "codewalk.pig.go"},
	{"GET", "codewalk.popout.png"},
	{"GET", "codewalk.run"},
	{"GET", "codewalk.sharemem.xml"},
	{"GET", "codewalk.urlpoll.go"},
	{"GET", "devel"},
	{"GET", "devel.release.html"},
	{"GET", "devel.weekly.html"},
	{"GET", "gopher"},
	{"GET", "gopher.appenginegopher.jpg"},
	{"GET", "gopher.appenginegophercolor.jpg"},
	{"GET", "gopher.appenginelogo.gif"},
	{"GET", "gopher.bumper.png"},
	{"GET", "gopher.bumper192x108.png"},
	{"GET", "gopher.bumper320x180.png"},
	{"GET", "gopher.bumper480x270.png"},
	{"GET", "gopher.bumper640x360.png"},
	{"GET", "gopher.doc.png"},
	{"GET", "gopher.frontpage.png"},
	{"GET", "gopher.gopherbw.png"},
	{"GET", "gopher.gophercolor.png"},
	{"GET", "gopher.gophercolor16x16.png"},
	{"GET", "gopher.help.png"},
	{"GET", "gopher.pkg.png"},
	{"GET", "gopher.project.png"},
	{"GET", "gopher.ref.png"},
	{"GET", "gopher.run.png"},
	{"GET", "gopher.talks.png"},
	{"GET", "gopher.pencil"},
	{"GET", "gopher.pencil.gopherhat.jpg"},
	{"GET", "gopher.pencil.gopherhelmet.jpg"},
	{"GET", "gopher.pencil.gophermega.jpg"},
	{"GET", "gopher.pencil.gopherrunning.jpg"},
	{"GET", "gopher.pencil.gopherswim.jpg"},
	{"GET", "gopher.pencil.gopherswrench.jpg"},
	{"GET", "play"},
	{"GET", "play.fib.go"},
	{"GET", "play.hello.go"},
	{"GET", "play.life.go"},
	{"GET", "play.peano.go"},
	{"GET", "play.pi.go"},
	{"GET", "play.sieve.go"},
	{"GET", "play.solitaire.go"},
	{"GET", "play.tree.go"},
	{"GET", "progs"},
	{"GET", "progs.cgo1.go"},
	{"GET", "progs.cgo2.go"},
	{"GET", "progs.cgo3.go"},
	{"GET", "progs.cgo4.go"},
	{"GET", "progs.defer.go"},
	{"GET", "progs.defer.out"},
	{"GET", "progs.defer2.go"},
	{"GET", "progs.defer2.out"},
	{"GET", "progs.eff_bytesize.go"},
	{"GET", "progs.eff_bytesize.out"},
	{"GET", "progs.eff_qr.go"},
	{"GET", "progs.eff_sequence.go"},
	{"GET", "progs.eff_sequence.out"},
	{"GET", "progs.eff_unused1.go"},
	{"GET", "progs.eff_unused2.go"},
	{"GET", "progs.error.go"},
	{"GET", "progs.error2.go"},
	{"GET", "progs.error3.go"},
	{"GET", "progs.error4.go"},
	{"GET", "progs.go1.go"},
	{"GET", "progs.gobs1.go"},
	{"GET", "progs.gobs2.go"},
	{"GET", "progs.image_draw.go"},
	{"GET", "progs.image_package1.go"},
	{"GET", "progs.image_package1.out"},
	{"GET", "progs.image_package2.go"},
	{"GET", "progs.image_package2.out"},
	{"GET", "progs.image_package3.go"},
	{"GET", "progs.image_package3.out"},
	{"GET", "progs.image_package4.go"},
	{"GET", "progs.image_package4.out"},
	{"GET", "progs.image_package5.go"},
	{"GET", "progs.image_package5.out"},
	{"GET", "progs.image_package6.go"},
	{"GET", "progs.image_package6.out"},
	{"GET", "progs.interface.go"},
	{"GET", "progs.interface2.go"},
	{"GET", "progs.interface2.out"},
	{"GET", "progs.json1.go"},
	{"GET", "progs.json2.go"},
	{"GET", "progs.json2.out"},
	{"GET", "progs.json3.go"},
	{"GET", "progs.json4.go"},
	{"GET", "progs.json5.go"},
	{"GET", "progs.run"},
	{"GET", "progs.slices.go"},
	{"GET", "progs.timeout1.go"},
	{"GET", "progs.timeout2.go"},
	{"GET", "progs.update.bash"},
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

func benchRoute(b *testing.B, router http.Handler, routes []route) {
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

func benchHostname(b *testing.B, router http.Handler, routes []route) {
	w := new(mockResponseWriter)
	r := httptest.NewRequest("GET", "/", nil)
	u := r.URL
	rq := u.RawQuery

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
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
	r, _ := New()
	for _, route := range staticRoutes {
		require.NoError(b, onlyError(r.Handle(route.method, route.path, emptyHandler)))
	}

	benchRoute(b, r, staticRoutes)
}

func BenchmarkStaticAllMux(b *testing.B) {
	r := http.NewServeMux()
	for _, route := range staticRoutes {
		r.HandleFunc(route.method+" "+route.path, func(w http.ResponseWriter, r *http.Request) {

		})
	}

	benchRoute(b, r, staticRoutes)
}

// BenchmarkStaticHostnameAll-8   	   36752	     33379 ns/op	       0 B/op	       0 allocs/op
func BenchmarkStaticHostnameAll(b *testing.B) {
	r, _ := New()
	for _, route := range staticHostnames {
		require.NoError(b, onlyError(r.Handle(route.method, route.path+"/", emptyHandler)))
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
	r, _ := New()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(r.Handle(route.method, route.path, emptyHandler)))
	}

	req := httptest.NewRequest(http.MethodGet, "/repos/sylvain/fox/hooks/1500", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkInfixCatchAll(b *testing.B) {
	f, _ := New()
	f.MustHandle(http.MethodGet, "/foo/*{bar}/baz", emptyHandler)

	req := httptest.NewRequest(http.MethodGet, "/foo/a1/b22/c333/baz", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		f.ServeHTTP(w, req)
	}
}

func BenchmarkLongParam(b *testing.B) {
	r, _ := New()
	r.MustHandle(http.MethodGet, "/foo/{very_very_very_very_very_long_param}", emptyHandler)
	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkOverlappingRoute(b *testing.B) {
	r, _ := New()
	for _, route := range overlappingRoutes {
		require.NoError(b, onlyError(r.Handle(route.method, route.path, emptyHandler)))
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
	f, _ := New(WithIgnoreTrailingSlash(true))
	f.MustHandle(http.MethodGet, "/{a}/{b}/e", emptyHandler)
	f.MustHandle(http.MethodGet, "/{a}/{b}/d", emptyHandler)
	f.MustHandle(http.MethodGet, "/foo/{b}", emptyHandler)
	f.MustHandle(http.MethodGet, "/foo/{b}/x/", emptyHandler)
	f.MustHandle(http.MethodGet, "/foo/{b}/y/", emptyHandler)

	req := httptest.NewRequest(http.MethodGet, "/foo/bar/", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		f.ServeHTTP(w, req)
	}
}

func BenchmarkStaticParallel(b *testing.B) {
	r, _ := New()
	for _, route := range staticRoutes {
		require.NoError(b, onlyError(r.Handle(route.method, route.path, emptyHandler)))
	}
	benchRouteParallel(b, r, route{http.MethodGet, "/progs/image_package4.out"})
}

func BenchmarkCatchAll(b *testing.B) {
	r, _ := New()
	require.NoError(b, onlyError(r.Handle(http.MethodGet, "/something/*{args}", emptyHandler)))
	w := new(mockResponseWriter)
	req := httptest.NewRequest(http.MethodGet, "/something/awesome", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkCatchAllParallel(b *testing.B) {
	r, _ := New()
	require.NoError(b, onlyError(r.Handle(http.MethodGet, "/something/*{args}", emptyHandler)))
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
	f, _ := New()
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
	f, _ := New()

	for _, route := range staticRoutes {
		require.NoError(t, onlyError(f.Handle(route.method, route.path, pathHandler)))
	}

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}

	assert.Equal(t, iterutil.Len2(f.Iter().All()), f.Len())
}

func TestStaticHostnameRoute(t *testing.T) {
	f, _ := New()

	for _, route := range staticHostnames {
		require.NoError(t, onlyError(f.Handle(route.method, route.path+"/foo", patternHandler)))
	}

	for _, route := range staticHostnames {
		req, err := http.NewRequest(route.method, "/foo", nil)
		require.NoError(t, err)
		req.Host = route.path
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path+"/foo", w.Body.String())
	}

	assert.Equal(t, iterutil.Len2(f.Iter().All()), f.Len())
}

func TestStaticRouteTxn(t *testing.T) {
	f, _ := New()

	require.NoError(t, f.Updates(func(txn *Txn) error {
		for _, route := range staticRoutes {
			if err := onlyError(txn.Handle(route.method, route.path, pathHandler)); err != nil {
				return err
			}
		}
		return nil
	}))

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}

	assert.Equal(t, iterutil.Len2(f.Iter().All()), f.Len())
}

func TestStaticRouteWithStaticDomain(t *testing.T) {
	f, _ := New()

	for _, route := range staticRoutes {
		require.NoError(t, onlyError(f.Handle(route.method, "exemple.com"+route.path, pathHandler)))
	}

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Host = "exemple.com"
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}

	assert.Equal(t, iterutil.Len2(f.Iter().All()), f.Len())
}

func TestStaticRouteWithStaticDomainTxn(t *testing.T) {
	f, _ := New()

	require.NoError(t, f.Updates(func(txn *Txn) error {
		for _, route := range staticRoutes {
			if err := onlyError(txn.Handle(route.method, "exemple.com"+route.path, pathHandler)); err != nil {
				return err
			}
		}
		return nil
	}))

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Host = "exemple.com"
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}

	assert.Equal(t, iterutil.Len2(f.Iter().All()), f.Len())
}

func TestStaticRouteMalloc(t *testing.T) {
	r, _ := New()

	for _, route := range staticRoutes {
		require.NoError(t, onlyError(r.Handle(route.method, route.path, emptyHandler)))
	}

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestStaticRouteWithStaticDomainMalloc(t *testing.T) {
	r, _ := New()

	for _, route := range staticRoutes {
		require.NoError(t, onlyError(r.Handle(route.method, "exemple.com"+route.path, emptyHandler)))
	}

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Host = "exemple.com"
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestParamsRoute(t *testing.T) {
	rx := regexp.MustCompile("({|\\*{)[A-z]+[}]")
	r, _ := New()
	h := func(c Context) {
		matches := rx.FindAllString(c.Path(), -1)
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
		assert.Equal(t, c.Path(), c.Pattern())
		_ = c.String(200, c.Path())
	}
	for _, route := range githubAPI {
		require.NoError(t, onlyError(r.Handle(route.method, route.path, h)))
	}
	for _, route := range githubAPI {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}
}

func TestParamsRouteTxn(t *testing.T) {
	rx := regexp.MustCompile("({|\\*{)[A-z]+[}]")
	r, _ := New()
	h := func(c Context) {
		matches := rx.FindAllString(c.Path(), -1)
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
		assert.Equal(t, c.Path(), c.Pattern())
		_ = c.String(200, c.Path())
	}
	require.NoError(t, r.Updates(func(txn *Txn) error {
		for _, route := range githubAPI {
			if err := onlyError(txn.Handle(route.method, route.path, h)); err != nil {
				return err
			}
		}
		return nil
	}))

	for _, route := range githubAPI {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}
}

func TestParamsRouteWithDomain(t *testing.T) {
	rx := regexp.MustCompile("({|\\*{)[A-z]+[}]")
	r, _ := New()
	h := func(c Context) {
		matches := rx.FindAllString(c.Path(), -1)
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

		assert.Equal(t, netutil.StripHostPort(c.Host())+c.Path(), c.Pattern())
		_ = c.String(200, netutil.StripHostPort(c.Host())+c.Path())
	}
	for _, route := range githubAPI {
		require.NoError(t, onlyError(r.Handle(route.method, "foo.{bar}.com"+route.path, h)))
	}
	for _, route := range githubAPI {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Host = "foo.{bar}.com"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "foo.{bar}.com"+route.path, w.Body.String())
	}
}

func TestParamsRouteWithDomainTxn(t *testing.T) {
	rx := regexp.MustCompile("({|\\*{)[A-z]+[}]")
	r, _ := New()
	h := func(c Context) {
		matches := rx.FindAllString(c.Path(), -1)
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

		assert.Equal(t, netutil.StripHostPort(c.Host())+c.Path(), c.Pattern())
		_ = c.String(200, netutil.StripHostPort(c.Host())+c.Path())
	}
	require.NoError(t, r.Updates(func(txn *Txn) error {
		for _, route := range githubAPI {
			if err := onlyError(txn.Handle(route.method, "foo.{bar}.com"+route.path, h)); err != nil {
				return err
			}
		}
		return nil
	}))

	for _, route := range githubAPI {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Host = "foo.{bar}.com"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "foo.{bar}.com"+route.path, w.Body.String())
	}
}

func TestParamsRouteMalloc(t *testing.T) {
	r, _ := New()
	for _, route := range githubAPI {
		require.NoError(t, onlyError(r.Handle(route.method, route.path, emptyHandler)))
	}
	for _, route := range githubAPI {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestHandleRoute(t *testing.T) {
	f, _ := New()

	t.Run("handle and update route with some option", func(t *testing.T) {
		want, err := f.NewRoute("/foo", emptyHandler, WithAnnotation("foo", "bar"), WithRedirectTrailingSlash(true))
		require.NoError(t, err)
		require.NoError(t, f.HandleRoute(http.MethodGet, want))
		got := f.Route(http.MethodGet, "/foo")
		assert.Equal(t, want, got)
		assert.True(t, got.RedirectTrailingSlashEnabled())

		want, err = f.NewRoute("/foo", emptyHandler, WithAnnotation("baz", "baz"))
		require.NoError(t, err)
		require.NoError(t, f.UpdateRoute(http.MethodGet, want))
		got = f.Route(http.MethodGet, "/foo")
		assert.Equal(t, want, got)
		assert.False(t, got.RedirectTrailingSlashEnabled())
	})

	t.Run("handle route with invalid method", func(t *testing.T) {
		rte, err := f.NewRoute("/bar", emptyHandler)
		require.NoError(t, err)
		assert.ErrorIs(t, f.HandleRoute("", rte), ErrInvalidRoute)
	})

	t.Run("update route with invalid method", func(t *testing.T) {
		rte, err := f.NewRoute("/baz", emptyHandler)
		require.NoError(t, err)
		require.NoError(t, f.HandleRoute(http.MethodGet, rte))
		assert.ErrorIs(t, f.UpdateRoute("", rte), ErrInvalidRoute)
	})

	t.Run("handle and update route with nil route", func(t *testing.T) {
		assert.ErrorIs(t, f.HandleRoute("/john", nil), ErrInvalidRoute)
		assert.ErrorIs(t, f.UpdateRoute("/foo", nil), ErrInvalidRoute)
	})
}

func TestParamsRouteWithDomainMalloc(t *testing.T) {
	r, _ := New()
	for _, route := range githubAPI {
		require.NoError(t, onlyError(r.Handle(route.method, "foo.{bar}.com"+route.path, emptyHandler)))
	}
	for _, route := range githubAPI {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Host = "foo.{bar}.com"
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestOverlappingRouteMalloc(t *testing.T) {
	r, _ := New()
	for _, route := range overlappingRoutes {
		require.NoError(t, onlyError(r.Handle(route.method, route.path, emptyHandler)))
	}
	for _, route := range overlappingRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestRouterWildcard(t *testing.T) {
	r, _ := New()

	routes := []struct {
		path string
		key  string
	}{
		{"/github.com/etf1/*{repo}", "/github.com/etf1/mux"},
		{"/github.com/johndoe/*{repo}", "/github.com/johndoe/buzz"},
		{"/foo/bar/*{args}", "/foo/bar/baz"},
		{"/filepath/path=*{path}", "/filepath/path=/file.txt"},
	}

	for _, route := range routes {
		require.NoError(t, onlyError(r.Handle(http.MethodGet, route.path, pathHandler)))
	}

	for _, route := range routes {
		req := httptest.NewRequest(http.MethodGet, route.key, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "route: key: %s, path: %s", route.key, route.path)
		assert.Equal(t, route.key, w.Body.String())
	}
}

func TestEmptyCatchAll(t *testing.T) {

	cases := []struct {
		name   string
		routes []string
		path   string
	}{
		{
			name:   "infix wildcard",
			routes: []string{"/foo/*{args}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with children",
			routes: []string{"/foo/*{args}/bar", "/foo/*{args}/caz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with static edge",
			routes: []string{"/foo/*{args}/bar", "/foo/baz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard and suffix wildcard",
			routes: []string{"/foo/*{args}/bar", "/foo/*{args}"},
			path:   "/foo/",
		},
		//
		{
			name:   "infix inflight wildcard",
			routes: []string{"/foo/abc*{args}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with children",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc*{args}/caz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with static edge",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc/baz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard and suffix wildcard",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc*{args}"},
			path:   "/foo/abc",
		},
		{
			name:   "suffix wildcard wildcard with param edge",
			routes: []string{"/foo/*{args}", "/foo/{param}"},
			path:   "/foo/",
		},
		{
			name:   "suffix inflight wildcard wildcard with param edge",
			routes: []string{"/foo/abc*{args}", "/foo/abc{param}"},
			path:   "/foo/abc",
		},
		{
			name:   "infix wildcard wildcard with param edge",
			routes: []string{"/foo/*{args}/bar", "/foo/{param}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix inflight wildcard wildcard with param edge",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc{param}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix wildcard wildcard with trailing slash",
			routes: []string{"/foo/*{args}/"},
			path:   "/foo//",
		},
		{
			name:   "infix inflight wildcard wildcard with trailing slash",
			routes: []string{"/foo/abc*{args}/"},
			path:   "/foo/abc/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}
			tree := f.getRoot()
			c := newTestContext(f)
			n, tsr := lookupByPath(tree, tree.root[0].children[0], tc.path, c, false)
			require.False(t, tsr)
			require.Nil(t, n)
		})
	}

}

func TestRouteWithParams(t *testing.T) {
	f, _ := New()
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
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
	}

	tree := f.getRoot()
	for _, rte := range routes {
		c := newTestContext(f)
		n, tsr := lookupByPath(tree, tree.root[0].children[0], rte, c, false)
		require.NotNilf(t, n, "route: %s", rte)
		require.NotNilf(t, n.route, "route: %s", rte)
		assert.False(t, tsr)
		assert.Equal(t, rte, n.route.pattern)
	}
}

func TestRouteParamEmptySegment(t *testing.T) {
	f, _ := New()
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
		require.NoError(t, onlyError(f.Handle(http.MethodGet, tc.route, emptyHandler)))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := f.getRoot()
			c := newTestContext(f)
			n, tsr := lookupByPath(tree, tree.root[0].children[0], tc.path, c, false)
			assert.Nil(t, n)
			assert.Empty(t, slices.Collect(c.Params()))
			assert.False(t, tsr)
		})
	}
}

func TestOverlappingRoute(t *testing.T) {
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
			path: "/foo/bar/x/y/z",
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
					Key:   "args",
					Value: "x/y/z",
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
			wantMatch: "/foo/*{args}",
			wantParams: Params{
				{
					Key:   "args",
					Value: "bar/",
				},
			},
		},
		{
			name: "param at index 1 with 2 nodes",
			path: "/foo/[barr]",
			routes: []string{
				"/foo/{bar}",
				"/foo/[bar]",
			},
			wantMatch: "/foo/{bar}",
			wantParams: Params{
				{
					Key:   "bar",
					Value: "[barr]",
				},
			},
		},
		{
			name: "param at index 1 with 3 nodes",
			path: "/foo/|barr|",
			routes: []string{
				"/foo/{bar}",
				"/foo/[bar]",
				"/foo/|bar|",
			},
			wantMatch: "/foo/{bar}",
			wantParams: Params{
				{
					Key:   "bar",
					Value: "|barr|",
				},
			},
		},
		{
			name: "param at index 0 with 3 nodes",
			path: "/foo/~barr~",
			routes: []string{
				"/foo/{bar}",
				"/foo/~bar~",
				"/foo/|bar|",
			},
			wantMatch: "/foo/{bar}",
			wantParams: Params{
				{
					Key:   "bar",
					Value: "~barr~",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}

			tree := f.getRoot()

			c := newTestContext(f)
			n, tsr := lookupByPath(tree, tree.root[0].children[0], tc.path, c, false)
			require.NotNil(t, n)
			require.NotNil(t, n.route)
			assert.False(t, tsr)
			assert.Equal(t, tc.wantMatch, n.route.pattern)
			if len(tc.wantParams) == 0 {
				assert.Empty(t, slices.Collect(c.Params()))
			} else {
				var params Params = slices.Collect(c.Params())
				assert.Equal(t, tc.wantParams, params)
			}

			// Test with lazy
			c = newTestContext(f)
			n, tsr = lookupByPath(tree, tree.root[0].children[0], tc.path, c, true)
			require.NotNil(t, n)
			require.NotNil(t, n.route)
			assert.False(t, tsr)
			assert.Empty(t, slices.Collect(c.Params()))
			assert.Equal(t, tc.wantMatch, n.route.pattern)
		})
	}
}

func TestInfixWildcard(t *testing.T) {
	cases := []struct {
		name       string
		routes     []string
		path       string
		wantPath   string
		wantTsr    bool
		wantParams []Param
	}{
		{
			name:     "simple infix wildcard",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo/a/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix wildcard",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo/a/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "static with infix wildcard child",
			routes:   []string{"/foo/", "/foo/*{args}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/*{args}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "bar",
				},
			},
		},
		{
			name:     "simple infix wildcard with route char",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo/*{args}/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args}",
				},
			},
		},
		{
			name:     "simple infix wildcard with multi segment and route char",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo/*{args}/b/c/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args}/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/za/bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with route char",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/z*{args}/bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args}",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with multi segment",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/za/b/c/bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with multi segment and route char",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/z*{args}/b/c/bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args}/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard long",
			routes:   []string{"/foo/xyz*{args}/bar"},
			path:     "/foo/xyza/bar",
			wantPath: "/foo/xyz*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with multi segment long",
			routes:   []string{"/foo/xyz*{args}/bar"},
			path:     "/foo/xyza/b/c/bar",
			wantPath: "/foo/xyz*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "overlapping infix and suffix wildcard match infix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar"},
			path:     "/foo/a/b/c/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "overlapping infix and suffix wildcard match suffix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar"},
			path:     "/foo/a/b/c/baz",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c/baz",
				},
			},
		},
		{
			name:     "overlapping infix and suffix wildcard match suffix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar"},
			path:     "/foo/a/b/c/barito",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c/barito",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match infix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo/a/b/c/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match suffix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo/a/b/c/bili",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c/bili",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match infix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}"},
			path:     "/foo/a/bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match param",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo/a/bar",
			wantPath: "/foo/{ps}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match suffix",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}"},
			path:     "/foo/a/bili",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/bili",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match param",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}"},
			path:     "/foo/a",
			wantPath: "/foo/{ps}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match param with ts",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/"},
			path:     "/foo/a/",
			wantPath: "/foo/{ps}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match suffix without ts",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/"},
			path:     "/foo/a",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix suffix wildcard and param match suffix without ts",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}"},
			path:     "/foo/a/",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/",
				},
			},
		},
		{
			name:     "overlapping infix inflight suffix wildcard and param match param",
			routes:   []string{"/foo/123*{args}", "/foo/123*{args}/bar", "/foo/123{ps}/bar"},
			path:     "/foo/123a/bar",
			wantPath: "/foo/123{ps}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name:     "overlapping infix inflight suffix wildcard and param match suffix",
			routes:   []string{"/foo/123*{args}", "/foo/123*{args}/bar", "/foo/123{ps}"},
			path:     "/foo/123a/bili",
			wantPath: "/foo/123*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/bili",
				},
			},
		},
		{
			name:     "overlapping infix inflight suffix wildcard and param match param",
			routes:   []string{"/foo/123*{args}", "/foo/123*{args}/bar", "/foo/123{ps}"},
			path:     "/foo/123a",
			wantPath: "/foo/123{ps}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "a",
				},
			},
		},
		{
			name:     "infix segment followed by param",
			routes:   []string{"/foo/*{a}/{b}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/*{a}/{b}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a/b/c",
				},
				{
					Key:   "b",
					Value: "d",
				},
			},
		},
		{
			name:     "infix segment followed by two params",
			routes:   []string{"/foo/*{a}/{b}/{c}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/*{a}/{b}/{c}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a/b",
				},
				{
					Key:   "b",
					Value: "c",
				},
				{
					Key:   "c",
					Value: "d",
				},
			},
		},
		{
			name:     "infix segment followed by one param and one wildcard",
			routes:   []string{"/foo/*{a}/{b}/*{c}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/*{a}/{b}/*{c}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
				{
					Key:   "b",
					Value: "b",
				},
				{
					Key:   "c",
					Value: "c/d",
				},
			},
		},
		{
			name:     "param followed by suffix wildcard",
			routes:   []string{"/foo/{a}/*{b}"},
			path:     "/foo/a/b/c/d",
			wantPath: "/foo/{a}/*{b}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
				{
					Key:   "b",
					Value: "b/c/d",
				},
			},
		},
		{
			name:     "infix inflight segment followed by param",
			routes:   []string{"/foo/123*{a}/{b}"},
			path:     "/foo/123a/b/c/d",
			wantPath: "/foo/123*{a}/{b}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a/b/c",
				},
				{
					Key:   "b",
					Value: "d",
				},
			},
		},
		{
			name:     "inflight param followed by suffix wildcard",
			routes:   []string{"/foo/123{a}/*{b}"},
			path:     "/foo/123a/b/c/d",
			wantPath: "/foo/123{a}/*{b}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
				{
					Key:   "b",
					Value: "b/c/d",
				},
			},
		},
		{
			name:     "multi infix segment simple",
			routes:   []string{"/foo/*{$1}/bar/*{$2}/baz"},
			path:     "/foo/a/bar/b/c/d/baz",
			wantPath: "/foo/*{$1}/bar/*{$2}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "a",
				},
				{
					Key:   "$2",
					Value: "b/c/d",
				},
			},
		},
		{
			name:     "multi inflight segment simple",
			routes:   []string{"/foo/123*{$1}/bar/456*{$2}/baz"},
			path:     "/foo/123a/bar/456b/c/d/baz",
			wantPath: "/foo/123*{$1}/bar/456*{$2}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "a",
				},
				{
					Key:   "$2",
					Value: "b/c/d",
				},
			},
		},
		{
			name:     "static priority",
			routes:   []string{"/foo/bar/baz", "/foo/{ps}/baz", "/foo/*{any}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/bar/baz",
			wantTsr:  false,
		},
		{
			name:     "param priority",
			routes:   []string{"/foo/bar/baz", "/foo/{ps}/baz", "/foo/*{any}/baz"},
			path:     "/foo/buzz/baz",
			wantPath: "/foo/{ps}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "buzz",
				},
			},
		},
		{
			name:     "fallback catch all",
			routes:   []string{"/foo/bar/baz", "/foo/{ps}/baz", "/foo/*{any}/baz"},
			path:     "/foo/a/b/baz",
			wantPath: "/foo/*{any}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b",
				},
			},
		},
		{
			name: "complex overlapping route with static priority",
			routes: []string{
				"/foo/bar/baz/{$1}/jo",
				"/foo/*{any}/baz/{$1}/jo",
				"/foo/{ps}/baz/{$1}/jo",
			},
			path:     "/foo/bar/baz/1/jo",
			wantPath: "/foo/bar/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "complex overlapping route with param priority",
			routes: []string{
				"/foo/bar/baz/{$1}/jo",
				"/foo/*{any}/baz/{$1}/jo",
				"/foo/{ps}/baz/{$1}/jo",
			},
			path:     "/foo/bam/baz/1/jo",
			wantPath: "/foo/{ps}/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "bam",
				},
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "complex overlapping route with catch all fallback",
			routes: []string{
				"/foo/bar/baz/{$1}/jo",
				"/foo/*{any}/baz/{$1}/jo",
				"/foo/{ps}/baz/{$1}/jo",
			},
			path:     "/foo/a/b/c/baz/1/jo",
			wantPath: "/foo/*{any}/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "complex overlapping route with catch all fallback",
			routes: []string{
				"/foo/bar/baz/{$1}/jo",
				"/foo/*{any}/baz/{$1}/john",
				"/foo/{ps}/baz/{$1}/johnny",
			},
			path:     "/foo/a/baz/1/john",
			wantPath: "/foo/*{any}/baz/{$1}/john",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a",
				},
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "overlapping static and infix",
			routes: []string{
				"/foo/*{any}/baz",
				"/foo/a/b/baz",
			},
			path:     "/foo/a/b/baz",
			wantPath: "/foo/a/b/baz",
			wantTsr:  false,
		},
		{
			name: "overlapping static and infix with catch all fallback",
			routes: []string{
				"/foo/*{any}/baz",
				"/foo/a/b/baz",
			},
			path:     "/foo/a/b/c/baz",
			wantPath: "/foo/*{any}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash",
			routes: []string{
				"/foo/*{any}/",
			},
			path:     "/foo/a/b/c/",
			wantPath: "/foo/*{any}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "overlapping static and infix with most specific",
			routes: []string{
				"/foo/*{any}/{a}/ddd/",
				"/foo/*{any}/bbb/{d}",
			},
			path:     "/foo/a/b/c/bbb/ddd/",
			wantPath: "/foo/*{any}/{a}/ddd/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
				{
					Key:   "a",
					Value: "bbb",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash",
			routes: []string{
				"/foo/*{any}",
				"/foo/*{any}/b/",
				"/foo/*{any}/c/",
			},
			path:     "/foo/x/y/z/",
			wantPath: "/foo/*{any}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z/",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash most specific",
			routes: []string{
				"/foo/*{any}",
				"/foo/*{any}/",
			},
			path:     "/foo/x/y/z/",
			wantPath: "/foo/*{any}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash most specific",
			routes: []string{
				"/foo/*{any}",
				"/foo/*{any}/",
			},
			path:     "/foo/x/y/z",
			wantPath: "/foo/*{any}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "x/y/z",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}
			tree := f.getRoot()
			c := newTestContext(f)
			n, tsr := lookupByPath(tree, tree.root[0].children[0], tc.path, c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.route.pattern)
			assert.Equal(t, tc.wantTsr, tsr)
			c.tsr = tsr
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}

}

func TestDomainLookup(t *testing.T) {
	cases := []struct {
		name       string
		routes     []string
		host       string
		path       string
		wantPath   string
		wantTsr    bool
		wantParams []Param
	}{
		{
			name: "static hostname with complex overlapping route with static priority",
			routes: []string{
				"exemple.com/foo/bar/baz/{$1}/jo",
				"exemple.com/foo/*{any}/baz/{$1}/jo",
				"exemple.com/foo/{ps}/baz/{$1}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "exemple.com/foo/bar/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "static hostname with complex overlapping route with param priority",
			routes: []string{
				"exemple.com/foo/bar/baz/{$1}/jo",
				"exemple.com/foo/*{any}/baz/{$1}/jo",
				"exemple.com/foo/{ps}/baz/{$1}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bam/baz/1/jo",
			wantPath: "exemple.com/foo/{ps}/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "bam",
				},
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "wildcard hostname with complex overlapping route with static priority",
			routes: []string{
				"exemple.com/foo/bar/baz/{$1}/jo",
				"{any}.com/foo/*{any}/baz/{$1}/jo",
				"exemple.{tld}/foo/{ps}/baz/{$1}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bar/baz/1/jo",
			wantPath: "exemple.com/foo/bar/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "wildcard hostname with complex overlapping route with param priority",
			routes: []string{
				"{sub}.com/foo/bar/baz/{$1}/jo",
				"exemple.{tld}/foo/*{any}/baz/{$1}/jo",
				"exemple.com/foo/{ps}/baz/{$1}/jo",
			},
			host:     "exemple.com",
			path:     "/foo/bam/baz/1/jo",
			wantPath: "exemple.com/foo/{ps}/baz/{$1}/jo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "ps",
					Value: "bam",
				},
				{
					Key:   "$1",
					Value: "1",
				},
			},
		},
		{
			name: "hostname not matching fallback to param",
			routes: []string{
				"{a}/foo",
				"fooxyz/foo",
				"foobar/foo",
			},
			host:     "foo",
			path:     "/foo",
			wantPath: "{a}/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
			},
		},
		{
			name: "static priority in hostname",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
			},
			host:     "foo.b.c",
			path:     "/foo",
			wantPath: "{a}.b.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
			},
		},
		{
			name: "static priority in hostname",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
			},
			host:     "foo.bar.c",
			path:     "/foo",
			wantPath: "{a}.{b}.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "b",
					Value: "bar",
				},
			},
		},
		{
			name: "static priority in hostname",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
			},
			host:     "foo.bar.baz",
			path:     "/foo",
			wantPath: "{a}.{b}.{c}/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "b",
					Value: "bar",
				},
				{
					Key:   "c",
					Value: "baz",
				},
			},
		},
		{
			name: "fallback to path only",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
				"/foo/bar",
			},
			host:       "foo.bar.baz",
			path:       "/foo/bar",
			wantPath:   "/foo/bar",
			wantTsr:    false,
			wantParams: Params(nil),
		},
		{
			name: "fallback to path only with param",
			routes: []string{
				"{a}.{b}.{c}/{d}",
				"{a}.{b}.c/{d}",
				"{a}.b.c/{d}",
				"/{a}/bar",
			},
			host:     "foo.bar.baz",
			path:     "/foo/bar",
			wantPath: "/{a}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
			},
		},
		{
			name: "fallback to tsr with hostname priority",
			routes: []string{
				"{a}.{b}.{c}/{d}",
				"{a}.{b}.c/{d}",
				"{a}.b.c/{path}/bar/",
				"/{a}/bar",
			},
			host:     "foo.b.c",
			path:     "/john/bar",
			wantPath: "{a}.b.c/{path}/bar/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "path",
					Value: "john",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}
			tree := f.getRoot()
			c := newTestContext(f)
			n, tsr := tree.lookup(http.MethodGet, tc.host, tc.path, c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.route.pattern)
			assert.Equal(t, tc.wantTsr, tsr)
			c.tsr = tsr
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}
}

func TestInfixWildcardTsr(t *testing.T) {
	cases := []struct {
		name       string
		routes     []string
		path       string
		wantPath   string
		wantTsr    bool
		wantParams []Param
	}{
		{
			name: "infix wildcard with trailing slash and tsr add",
			routes: []string{
				"/foo/*{any}/",
			},
			path:     "/foo/a/b/c",
			wantPath: "/foo/*{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with tsr but skipped node match",
			routes: []string{
				"/foo/*{any}/",
				"/{x}/a/b/c",
			},
			path:     "/foo/a/b/c",
			wantPath: "/{x}/a/b/c",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "x",
					Value: "foo",
				},
			},
		},
		{
			name: "infix wildcard with tsr but skipped node does not match",
			routes: []string{
				"/foo/*{any}/",
				"/{x}/a/b/x",
			},
			path:     "/foo/a/b/c",
			wantPath: "/foo/*{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with trailing slash and tsr add",
			routes: []string{
				"/foo/*{any}/",
				"/foo/*{any}/abc",
				"/foo/*{any}/bcd",
			},
			path:     "/foo/a/b/c/abd",
			wantPath: "/foo/*{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c/abd",
				},
			},
		},
		{
			name: "infix wildcard with sub-node tsr add fallback",
			routes: []string{
				"/foo/*{any}/{a}/ddd/",
				"/foo/*{any}/bbb/{d}/foo",
			},
			path:     "/foo/a/b/c/bbb/ddd",
			wantPath: "/foo/*{any}/{a}/ddd/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
				{
					Key:   "a",
					Value: "bbb",
				},
			},
		},
		{
			name: "infix wildcard with sub-node tsr at depth 1 but direct match",
			routes: []string{
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbb",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/*{any}/bbb",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with sub-node tsr at depth 1 and 2 but direct match",
			routes: []string{
				"/foo/*{any}/b/c/bbb/",
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbb",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/*{any}/bbb",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a/b/c",
				},
			},
		},
		{
			name: "infix wildcard with sub-node tsr at depth 1 and 2 but fallback first tsr",
			routes: []string{
				"/foo/*{any}/b/c/bbb/",
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/*{any}/b/c/bbb/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a",
				},
			},
		},
		{
			name: "infix wildcard with sub-node tsr at depth 1 and 2 but fallback first tsr",
			routes: []string{
				"/foo/*{any}/",
				"/foo/*{any}/b/c/bbb/",
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/*{any}/b/c/bbb/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "a",
				},
			},
		},
		{
			name: "infix wildcard with depth 0 tsr and sub-node tsr at depth 1 fallback first tsr",
			routes: []string{
				"/foo/a/b/c/bbb/",
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/a/b/c/bbb/",
			wantTsr:  true,
		},
		{
			name: "infix wildcard with depth 0 tsr and sub-node tsr at depth 1 fallback first tsr",
			routes: []string{
				"/foo/{first}/b/c/bbb/",
				"/foo/*{any}/c/bbb/",
				"/foo/*{any}/bbx",
			},
			path:     "/foo/a/b/c/bbb",
			wantPath: "/foo/{first}/b/c/bbb/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "first",
					Value: "a",
				},
			},
		},
		{
			name: "multi infix wildcard with sub-node tsr at depth 1 but direct match",
			routes: []string{
				"/foo/*{any1}/b/c/*{any2}/d/",
				"/foo/*{any1}/c/*{any2}/d",
			},
			path:     "/foo/a/b/c/x/y/z/d",
			wantPath: "/foo/*{any1}/c/*{any2}/d",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any1",
					Value: "a/b",
				},
				{
					Key:   "any2",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "multi infix wildcard with sub-node tsr at depth 1 and fallback first",
			routes: []string{
				"/foo/*{any1}/b/c/*{any2}/d/",
				"/foo/*{any1}/c/*{any2}/x",
			},
			path:     "/foo/a/b/c/x/y/z/d",
			wantPath: "/foo/*{any1}/b/c/*{any2}/d/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any1",
					Value: "a",
				},
				{
					Key:   "any2",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "multi infix wildcard with sub-node tsr and skipped nodes at depth 1 and fallback first",
			routes: []string{
				"/foo/*{any1}/b/c/*{any2}/{a}/",
				"/foo/*{any1}/b/c/*{any2}/d{a}/",
				"/foo/*{any1}/b/c/*{any2}/dd/",
				"/foo/*{any1}/c/*{any2}/x",
			},
			path:     "/foo/a/b/c/x/y/z/dd",
			wantPath: "/foo/*{any1}/b/c/*{any2}/dd/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any1",
					Value: "a",
				},
				{
					Key:   "any2",
					Value: "x/y/z",
				},
			},
		},
		{
			name: "multi infix wildcard with sub-node tsr and skipped nodes at depth 1 and direct match",
			routes: []string{
				"/foo/*{any1}/b/c/*{any2}/{a}/",
				"/foo/*{any1}/b/c/*{any2}/d{a}/",
				"/foo/*{any1}/b/c/*{any2}/dd/",
				"/foo/*{any1}/c/*{any2}/x",
			},
			path:     "/foo/a/b/c/x/y/z/xd/",
			wantPath: "/foo/*{any1}/b/c/*{any2}/{a}/",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "any1",
					Value: "a",
				},
				{
					Key:   "any2",
					Value: "x/y/z",
				},
				{
					Key:   "a",
					Value: "xd",
				},
			},
		},
		{
			name: "multi infix wildcard with sub-node tsr and skipped nodes at depth 1 with direct match depth 0",
			routes: []string{
				"/foo/*{any1}/b/c/*{any2}/{a}/",
				"/foo/*{any1}/b/c/*{any2}/d{a}/",
				"/foo/*{any1}/b/c/*{any2}/dd/",
				"/foo/*{any1}/c/*{any2}/x",
				"/{a}/*{any1}/c/x/y/z/dd",
			},
			path:     "/foo/a/b/c/x/y/z/dd",
			wantPath: "/{a}/*{any1}/c/x/y/z/dd",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "foo",
				},
				{
					Key:   "any1",
					Value: "a/b",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}

			tree := f.getRoot()

			c := newTestContext(f)
			n, tsr := lookupByPath(tree, tree.root[0].children[0], tc.path, c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.route.pattern)
			assert.Equal(t, tc.wantTsr, tsr)
			c.tsr = tsr
			assert.Equal(t, tc.wantParams, slices.Collect(c.Params()))
		})
	}
}

func TestInsertUpdateAndDeleteWithHostname(t *testing.T) {
	cases := []struct {
		name   string
		routes []struct {
			path string
		}
	}{
		{
			name: "test delete with merge",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
				{path: "a.b/"},
			},
		},
		{
			name: "test delete with merge",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/f"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c.d/f"},
				{path: "a.b.c.d/fox"},
				{path: "a.b.c{d}/fox/bar"},
				{path: "a.e.c{d}/fox/bar"},
				{path: "/johnny"},
				{path: "/j"},
				{path: "/x"},
				{path: "a.b/"},
			},
		},
		{
			name: "test delete with merge ppp root",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
			},
		},
		{
			name: "test delete with merge pp root",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
			},
		},
		{
			name: "simple insert and delete",
			routes: []struct {
				path string
			}{
				{path: "a.x.x/"},
			},
		},
		{
			name: "simple insert and delete",
			routes: []struct {
				path string
			}{
				{path: "a.x.x/"},
				{path: "a.x.y/"},
			},
		},
		{
			name: "simple insert and delete",
			routes: []struct {
				path string
			}{
				{path: "aaa/"},
				{path: "aaab/"},
				{path: "aaabc/"},
			},
		},
		{
			name: "test delete with merge ppp root",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c/foo/ba"},
				{path: "a.b.c/foo"},
				{path: "a.b.c/x"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			routeCopy := make([]struct{ path string }, len(tc.routes))
			copy(routeCopy, tc.routes)

			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte.path, emptyHandler, WithAnnotation("foo", "bar"))))
			}
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Update(http.MethodGet, rte.path, emptyHandler, WithAnnotation("foo", "bar"))))
			}
			for _, rte := range tc.routes {
				r := f.Route(http.MethodGet, rte.path)
				require.NotNilf(t, r, "missing method=%s;path=%s", http.MethodGet, rte.path)
				assert.Equal(t, "bar", r.Annotation("foo").(string))
			}

			for _, rte := range tc.routes {
				deletedRoute, err := f.Delete(http.MethodGet, rte.path)
				require.NoError(t, err)
				assert.Equal(t, rte.path, deletedRoute.Pattern())
				routeCopy = slices.Delete(routeCopy, 0, 1)
				assert.Falsef(t, f.Has(http.MethodGet, rte.path), "found method=%s;path=%s", http.MethodGet, rte.path)
				for _, rte := range routeCopy {
					require.NoError(t, onlyError(f.Update(http.MethodGet, rte.path, emptyHandler, WithAnnotation("john", "doe"))))
				}
				for _, rte := range routeCopy {
					r := f.Route(http.MethodGet, rte.path)
					require.NotNilf(t, r, "missing method=%s;path=%s", http.MethodGet, rte.path)
					assert.Equal(t, "doe", r.Annotation("john").(string))
				}
			}

			tree := f.getRoot()
			assert.Equal(t, http.MethodGet, tree.root[0].key)
			assert.Len(t, tree.root[0].children, 0)

			// Now let's do it in reverse
			routeCopy = make([]struct{ path string }, len(tc.routes))
			copy(routeCopy, tc.routes)
			for i := len(tc.routes) - 1; i >= 0; i-- {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, tc.routes[i].path, emptyHandler)))
			}
			for i := len(tc.routes) - 1; i >= 0; i-- {
				assert.Truef(t, f.Has(http.MethodGet, tc.routes[i].path), "missing method=%s;path=%s", http.MethodGet, tc.routes[i].path)
			}
			for i := len(tc.routes) - 1; i >= 0; i-- {
				deletedRoute, err := f.Delete(http.MethodGet, tc.routes[i].path)
				require.NoError(t, err)
				assert.Equal(t, tc.routes[i].path, deletedRoute.Pattern())
				routeCopy = slices.Delete(routeCopy, len(routeCopy)-1, len(routeCopy))
				assert.Falsef(t, f.Has(http.MethodGet, tc.routes[i].path), "found method=%s;path=%s", http.MethodGet, tc.routes[i].path)
				for _, rte := range routeCopy {
					assert.Truef(t, f.Has(http.MethodGet, rte.path), "missing method=%s;path=%s", http.MethodGet, rte.path)
				}
			}

			tree = f.getRoot()
			assert.Equal(t, http.MethodGet, tree.root[0].key)
			assert.Len(t, tree.root[0].children, 0)
		})
	}
}

func TestInsertUpdateAndDeleteWithHostnameTxn(t *testing.T) {
	cases := []struct {
		name   string
		routes []struct {
			path string
		}
	}{
		{
			name: "test delete with merge",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
				{path: "a.b/"},
			},
		},
		{
			name: "test delete with merge",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/f"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c.d/f"},
				{path: "a.b.c.d/fox"},
				{path: "a.b.c{d}/fox/bar"},
				{path: "a.e.c{d}/fox/bar"},
				{path: "/johnny"},
				{path: "/j"},
				{path: "/x"},
				{path: "a.b/"},
			},
		},
		{
			name: "test delete with merge ppp root",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
			},
		},
		{
			name: "test delete with merge pp root",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
			},
		},
		{
			name: "simple insert and delete",
			routes: []struct {
				path string
			}{
				{path: "a.x.x/"},
			},
		},
		{
			name: "simple insert and delete",
			routes: []struct {
				path string
			}{
				{path: "a.x.x/"},
				{path: "a.x.y/"},
			},
		},
		{
			name: "simple insert and delete",
			routes: []struct {
				path string
			}{
				{path: "aaa/"},
				{path: "aaab/"},
				{path: "aaabc/"},
			},
		},
		{
			name: "test delete with merge ppp root",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c/foo/ba"},
				{path: "a.b.c/foo"},
				{path: "a.b.c/x"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			routeCopy := make([]struct{ path string }, len(tc.routes))
			copy(routeCopy, tc.routes)

			require.NoError(t, f.Updates(func(txn *Txn) error {
				for _, rte := range tc.routes {
					if err := onlyError(txn.Handle(http.MethodGet, rte.path, emptyHandler, WithAnnotation("foo", "bar"))); err != nil {
						return err
					}
				}
				return nil
			}))
			require.NoError(t, f.Updates(func(txn *Txn) error {
				for _, rte := range tc.routes {
					if err := onlyError(txn.Update(http.MethodGet, rte.path, emptyHandler, WithAnnotation("foo", "bar"))); err != nil {
						return err
					}
				}
				return nil
			}))

			for _, rte := range tc.routes {
				r := f.Route(http.MethodGet, rte.path)
				require.NotNilf(t, r, "missing method=%s;path=%s", http.MethodGet, rte.path)
				assert.Equal(t, "bar", r.Annotation("foo").(string))
			}

			require.NoError(t, f.Updates(func(txn *Txn) error {
				for _, rte := range tc.routes {
					deletedRoute, err := txn.Delete(http.MethodGet, rte.path)
					if err != nil {
						return err
					}
					assert.Equal(t, rte.path, deletedRoute.Pattern())
					routeCopy = slices.Delete(routeCopy, 0, 1)
					assert.Falsef(t, txn.Has(http.MethodGet, rte.path), "found method=%s;path=%s", http.MethodGet, rte.path)
					for _, rte := range routeCopy {
						if err := onlyError(txn.Update(http.MethodGet, rte.path, emptyHandler, WithAnnotation("john", "doe"))); err != nil {
							return err
						}
					}
					for _, rte := range routeCopy {
						r := txn.Route(http.MethodGet, rte.path)
						if !assert.NotNilf(t, r, "missing method=%s;path=%s", http.MethodGet, rte.path) {
							assert.Equal(t, "doe", r.Annotation("john").(string))
						}
					}
				}
				return nil
			}))

			tree := f.getRoot()
			assert.Equal(t, http.MethodGet, tree.root[0].key)
			assert.Len(t, tree.root[0].children, 0)

			// Now let's do it in reverse
			routeCopy = make([]struct{ path string }, len(tc.routes))
			copy(routeCopy, tc.routes)
			require.NoError(t, f.Updates(func(txn *Txn) error {
				for i := len(tc.routes) - 1; i >= 0; i-- {
					if err := onlyError(txn.Handle(http.MethodGet, tc.routes[i].path, emptyHandler)); err != nil {
						return err
					}
				}
				return nil
			}))
			for i := len(tc.routes) - 1; i >= 0; i-- {
				assert.Truef(t, f.Has(http.MethodGet, tc.routes[i].path), "missing method=%s;path=%s", http.MethodGet, tc.routes[i].path)
			}
			require.NoError(t, f.Updates(func(txn *Txn) error {
				for i := len(tc.routes) - 1; i >= 0; i-- {
					deletedRoute, err := txn.Delete(http.MethodGet, tc.routes[i].path)
					if err != nil {
						return err
					}
					assert.Equal(t, tc.routes[i].path, deletedRoute.Pattern())
					routeCopy = slices.Delete(routeCopy, len(routeCopy)-1, len(routeCopy))
					assert.Falsef(t, txn.Has(http.MethodGet, tc.routes[i].path), "found method=%s;path=%s", http.MethodGet, tc.routes[i].path)
					for _, rte := range routeCopy {
						assert.Truef(t, txn.Has(http.MethodGet, rte.path), "missing method=%s;path=%s", http.MethodGet, rte.path)
					}
				}
				return nil
			}))

			tree = f.getRoot()
			assert.Equal(t, http.MethodGet, tree.root[0].key)
			assert.Len(t, tree.root[0].children, 0)
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
				{path: "/john/", wantErr: nil, wantMatch: nil},
				{path: "/foo/baz", wantErr: nil, wantMatch: nil},
				{path: "/foo/bar", wantErr: nil, wantMatch: nil},
				{path: "/foo/{id}", wantErr: nil, wantMatch: nil},
				{path: "/foo/*{args}", wantErr: nil, wantMatch: nil},
				{path: "/avengers/ironman/{power}", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}/bar", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}/foo", wantErr: nil, wantMatch: nil},
				{path: "/avengers/*{args}", wantErr: nil, wantMatch: nil},
				{path: "/fox/", wantErr: nil, wantMatch: nil},
				{path: "/fox/*{args}", wantErr: nil, wantMatch: nil},
				{path: "/fox/*{args}", wantErr: ErrRouteExist, wantMatch: []string{"/fox/*{args}"}},
				{path: "a.b.c/fox", wantErr: nil, wantMatch: nil},
				{path: "a.b.c/fox", wantErr: ErrRouteExist, wantMatch: []string{"a.b.c/fox"}},
				{path: "a.{b}.c/fox", wantErr: nil, wantMatch: nil},
				{path: "{a}.b.c/fox", wantErr: nil, wantMatch: nil},
				{path: "a.{b}.c/fox", wantErr: ErrRouteExist, wantMatch: []string{"a.{b}.c/fox"}},
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
				{path: "a.b.c/foo/bar", wantErr: nil, wantMatch: nil},
				{path: "a.b.c.d/foo/bar", wantErr: nil, wantMatch: nil},
				{path: "a.b.c{d}/foo/bar", wantErr: nil, wantMatch: nil},
				{path: "a.b.c{d}.com/foo/bar", wantErr: nil, wantMatch: nil},
			},
		},
		{
			name: "no conflict for key match mid-edge",
			routes: []struct {
				wantErr   error
				path      string
				wantMatch []string
			}{
				// Note that this is impossible for route with hostname to
				// end mid-edge in the hostname part since it always end with /
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
				{path: "/gchq/*{id}", wantErr: nil, wantMatch: nil},
				{path: "/gchq/*{abc}", wantErr: ErrRouteConflict, wantMatch: []string{"/gchq/*{id}"}},
				{path: "/foo{id}", wantErr: nil, wantMatch: nil},
				{path: "/foo/a{id}", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}/bar", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}/baz", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{id}", wantErr: nil, wantMatch: nil},
				{path: "/avengers/{abc}", wantErr: ErrRouteConflict, wantMatch: []string{"/avengers/{id}", "/avengers/{id}/bar", "/avengers/{id}/baz"}},
				{path: "/ironman/*{id}/bar", wantErr: nil, wantMatch: nil},
				{path: "/ironman/*{id}/baz", wantErr: nil, wantMatch: nil},
				{path: "/ironman/*{id}", wantErr: nil, wantMatch: nil},
				{path: "/ironman/*{abc}", wantErr: ErrRouteConflict, wantMatch: []string{"/ironman/*{id}", "/ironman/*{id}/bar", "/ironman/*{id}/baz"}},
				{path: "foo.{bar}/baz", wantErr: nil, wantMatch: nil},
				{path: "foo.{bar}.com/baz", wantErr: nil, wantMatch: nil},
				{path: "foo.{baz}/baz", wantErr: ErrRouteConflict, wantMatch: []string{"foo.{bar}.com/baz", "foo.{bar}/baz"}},
				{path: "foo.ab{bar}.com/baz", wantErr: nil, wantMatch: nil},
				{path: "foo.ab{x}.com/baz", wantErr: ErrRouteConflict, wantMatch: []string{"foo.ab{bar}.com/baz"}},
				{path: "foo.{bar}/", wantErr: nil, wantMatch: nil},
				{path: "foo.{yyy}/", wantErr: ErrRouteConflict, wantMatch: []string{"foo.{bar}.com/baz", "foo.{bar}/", "foo.{bar}/baz"}},
				{path: "{foo}.bar/baz", wantErr: nil, wantMatch: nil},
				{path: "{foo}.bar.com/baz", wantErr: nil, wantMatch: nil},
				{path: "{baz}.bar/baz", wantErr: ErrRouteConflict, wantMatch: []string{"{foo}.bar.com/baz", "{foo}.bar/baz"}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			for _, rte := range tc.routes {
				r, err := f.Handle(http.MethodGet, rte.path, emptyHandler)
				if err != nil {
					assert.Nil(t, r)
				}
				assert.ErrorIsf(t, err, rte.wantErr, "route: %s", rte.path)
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
			wantErr:   ErrRouteNotFound,
			wantMatch: []string{"/foo/*{args}"},
		},
		{
			name:      "replacing non wildcard by wildcard",
			routes:    []string{"/foo/bar", "/foo/"},
			update:    "/foo/*{all}",
			wantErr:   ErrRouteNotFound,
			wantMatch: []string{"/foo/"},
		},
		{
			name:      "replacing wildcard by non wildcard",
			routes:    []string{"/foo/bar", "/foo/*{args}"},
			update:    "/foo/",
			wantErr:   ErrRouteNotFound,
			wantMatch: []string{"/foo/*{args}"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}
			r, err := f.Update(http.MethodGet, tc.update, emptyHandler)
			if err != nil {
				assert.Nil(t, r)
			}
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestInvalidRoute(t *testing.T) {
	f, _ := New()
	// Invalid route on insert
	assert.ErrorIs(t, onlyError(f.Handle("get", "/foo", emptyHandler)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Handle("", "/foo", emptyHandler)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Handle(http.MethodGet, "/foo", nil)), ErrInvalidRoute)

	// Invalid route on update
	assert.ErrorIs(t, onlyError(f.Update("", "/foo", emptyHandler)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Update(http.MethodGet, "/foo", nil)), ErrInvalidRoute)
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
		{
			name:   "replacing infix catch all node",
			routes: []string{"/foo/*{bar}/baz", "/foo", "/foo/bar"},
			update: "/foo/*{bar}/baz",
		},
		{
			name:   "replacing infix inflight catch all node",
			routes: []string{"/foo/abc*{bar}/baz", "/foo", "/foo/abc{bar}"},
			update: "/foo/abc*{bar}/baz",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}
			assert.NoError(t, onlyError(f.Update(http.MethodGet, tc.update, emptyHandler)))
		})
	}
}

func TestParseRoute(t *testing.T) {
	f, _ := New()
	cases := []struct {
		wantErr error
		name    string
		path    string
		wantN   uint32
	}{
		{
			name: "valid static route",
			path: "/foo/bar",
		},
		{
			name:  "top level domain",
			path:  "{tld}/foo/bar",
			wantN: 1,
		},
		{
			name:  "valid catch all route",
			path:  "/foo/bar/*{arg}",
			wantN: 1,
		},
		{
			name:  "valid param route",
			path:  "/foo/bar/{baz}",
			wantN: 1,
		},
		{
			name:  "valid multi params route",
			path:  "/foo/{bar}/{baz}",
			wantN: 2,
		},
		{
			name:  "valid same params route",
			path:  "/foo/{bar}/{bar}",
			wantN: 2,
		},
		{
			name:  "valid multi params and catch all route",
			path:  "/foo/{bar}/{baz}/*{arg}",
			wantN: 3,
		},
		{
			name:  "valid inflight param",
			path:  "/foo/xyz:{bar}",
			wantN: 1,
		},
		{
			name:  "valid inflight catchall",
			path:  "/foo/xyz:*{bar}",
			wantN: 1,
		},
		{
			name:  "valid multi inflight param and catch all",
			path:  "/foo/xyz:{bar}/abc:{bar}/*{arg}",
			wantN: 3,
		},
		{
			name:  "catch all with arg in the middle of the route",
			path:  "/foo/bar/*{bar}/baz",
			wantN: 1,
		},
		{
			name:  "multiple catch all suffix and inflight with arg in the middle of the route",
			path:  "/foo/bar/*{bar}/x*{args}/y/*{z}/{b}",
			wantN: 4,
		},
		{
			name:  "inflight catch all with arg in the middle of the route",
			path:  "/foo/bar/damn*{bar}/baz",
			wantN: 1,
		},
		{
			name:  "catch all with arg in the middle of the route and param after",
			path:  "/foo/bar/*{bar}/{baz}",
			wantN: 2,
		},
		{
			name:  "simple domain and path",
			path:  "foo/bar",
			wantN: 0,
		},
		{
			name:  "simple domain with trailing slash",
			path:  "foo/",
			wantN: 0,
		},
		{
			name:  "period in param path allowed",
			path:  "foo/{.bar}",
			wantN: 1,
		},
		{
			name:    "missing a least one slash",
			path:    "foo.com",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "empty parameter",
			path:    "/foo/bar{}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "missing arguments name after catch all",
			path:    "/foo/bar/*",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "missing arguments name after param",
			path:    "/foo/bar/{",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "catch all in the middle of the route",
			path:    "/foo/bar/*/baz",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "empty infix catch all",
			path:    "/foo/bar/*{}/baz",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "empty ending catch all",
			path:    "/foo/bar/baz/*{}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "unexpected character in param",
			path:    "/foo/{{bar}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "unexpected character in param",
			path:    "/foo/{*bar}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "unexpected character in catch-all",
			path:    "/foo/*{/bar}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "catch all not supported in hostname",
			path:    "a.b.c*/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal character in params hostname",
			path:    "a.b.c{/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal character in hostname label",
			path:    "a.b.c}/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "unexpected character in param hostname",
			path:    "a.{.bar}.c/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "unexpected character in param hostname",
			path:    "a.{/bar}.c/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "in flight catch-all after param in one route segment",
			path:    "/foo/{bar}*{baz}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "multiple param in one route segment",
			path:    "/foo/{bar}{baz}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "in flight param after catch all",
			path:    "/foo/*{args}{param}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "consecutive catch all with no slash",
			path:    "/foo/*{args}*{param}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "consecutive catch all",
			path:    "/foo/*{args}/*{param}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "consecutive catch all with inflight",
			path:    "/foo/ab*{args}/*{param}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "unexpected char after inflight catch all",
			path:    "/foo/ab*{args}a",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "unexpected char after catch all",
			path:    "/foo/*{args}a",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal catch-all in hostname",
			path:    "*{any}.com/foo",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal catch-all in hostname",
			path:    "a.*{any}.com/foo",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal catch-all in hostname",
			path:    "a.b.*{any}/foo",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:  "static hostname with catch-all path",
			path:  "a.b.com/*{any}",
			wantN: 1,
		},
		{
			name:    "illegal leading hyphen in hostname",
			path:    "-a.com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal leading dot in hostname",
			path:    ".a.com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal trailing hyphen in hostname",
			path:    "a.com-/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal trailing dot in hostname",
			path:    "a.com./",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal trailing dot in hostname after param",
			path:    "{tld}./foo/bar",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal single dot in hostname",
			path:    "./",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal hyphen before dot",
			path:    "a-.com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal hyphen after dot",
			path:    "a.-com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal double dot",
			path:    "a..com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal double dot with param state",
			path:    "{b}..com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal double dot with inflight param state",
			path:    "a{b}..com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "param not finishing with delimiter in hostname",
			path:    "{a}b{b}.com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "consecutive parameter in hostname",
			path:    "{a}{b}.com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "leading hostname label exceed 63 characters",
			path:    "UJ01DowF1x5Lk6LYsUrbr0LgbDD1wFyw8Sm8q17MnT0I9igK774vCWr5rLY5dGuu.b.com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "middle hostname label exceed 63 characters",
			path:    "a.UJ01DowF1x5Lk6LYsUrbr0LgbDD1wFyw8Sm8q17MnT0I9igK774vCWr5rLY5dGuu.com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "trailing hostname label exceed 63 characters",
			path:    "a.b.UJ01DowF1x5Lk6LYsUrbr0LgbDD1wFyw8Sm8q17MnT0I9igK774vCWr5rLY5dGuu/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "illegal character in domain",
			path:    "a.b!.com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "invalid all-numeric label",
			path:    "123/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:  "all-numeric label with wildcard",
			path:  "123.{a}.456/",
			wantN: 1,
		},
		{
			name:    "all-numeric label with path wildcard",
			path:    "123.456/{abc}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "hostname exceed 255 character",
			path:    "a.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJx.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJxR.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJxR.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJxR/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "invalid all-numeric label",
			path:    "11.22.33/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:  "2 regular params in domain",
			path:  "{a}.{b}.com/",
			wantN: 2,
		},
		{
			name:  "255 character with .",
			path:  "78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJxR.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJxR.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJxR.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJxR/",
			wantN: 0,
		},
		{
			name:  "param does not count at character",
			path:  "{a}.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJx.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJxR.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJxR.78faYZYIQKt3hH2mquv9szfroEexx8QzTScu3OUdoYfArjL6jMDyXK2CEFvzJxR/",
			wantN: 1,
		},
		{
			name:  "hostname variant with multiple catch all suffix and inflight with arg in the middle of the route",
			path:  "example.com/foo/bar/*{bar}/x*{args}/y/*{z}/{b}",
			wantN: 4,
		},
		{
			name:  "hostname variant with inflight catch all with arg in the middle of the route",
			path:  "example.com/foo/bar/damn*{bar}/baz",
			wantN: 1,
		},
		{
			name:  "hostname variant catch all with arg in the middle of the route and param after",
			path:  "example.com/foo/bar/*{bar}/{baz}",
			wantN: 2,
		},
		{
			name:  "complex domain and path",
			path:  "{ab}.{c}.de{f}.com/foo/bar/*{bar}/x*{args}/y/*{z}/{b}",
			wantN: 7,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, _, err := f.parseRoute(tc.path)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantN, n)
		})
	}
}

func TestParseRouteParamsConstraint(t *testing.T) {
	t.Run("param limit", func(t *testing.T) {
		f, _ := New(WithMaxRouteParams(3))
		_, _, err := f.parseRoute("/{1}/{2}/{3}")
		assert.NoError(t, err)
		_, _, err = f.parseRoute("/{1}/{2}/{3}/{4}")
		assert.Error(t, err)
		_, _, err = f.parseRoute("/ab{1}/{2}/cd/{3}/{4}/ef")
		assert.Error(t, err)
	})
	t.Run("param key limit", func(t *testing.T) {
		f, _ := New(WithMaxRouteParamKeyBytes(3))
		_, _, err := f.parseRoute("/{abc}/{abc}/{abc}")
		assert.NoError(t, err)
		_, _, err = f.parseRoute("/{abcd}/{abc}/{abc}")
		assert.Error(t, err)
		_, _, err = f.parseRoute("/{abc}/{abcd}/{abc}")
		assert.Error(t, err)
		_, _, err = f.parseRoute("/{abc}/{abc}/{abcd}")
		assert.Error(t, err)
		_, _, err = f.parseRoute("/{abc}/*{abcd}/{abc}")
		assert.Error(t, err)
		_, _, err = f.parseRoute("/{abc}/{abc}/*{abcdef}")
		assert.Error(t, err)
	})
}

func TestParseRouteMalloc(t *testing.T) {
	f, _ := New()
	var (
		n   uint32
		err error
	)
	allocs := testing.AllocsPerRun(100, func() {
		n, _, err = f.parseRoute("{ab}.{c}.de{f}.com/foo/bar/*{bar}/x*{args}/y/*{z}/{b}")
	})
	assert.Equal(t, float64(0), allocs)
	assert.NoError(t, err)
	assert.Equal(t, uint32(7), n)
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
			name:  "match mid edge in child node with parent not leaf",
			paths: []string{"/test/x", "/tests/"},
			key:   "/test/",
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
			f, _ := New()
			for _, path := range tc.paths {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, path, emptyHandler)))
			}
			tree := f.getRoot()
			c := newTestContext(f)
			n, got := lookupByPath(tree, tree.root[0].children[0], tc.key, c, true)
			assert.Equal(t, tc.want, got)
			if tc.want {
				require.NotNil(t, n)
				require.NotNil(t, n.route)
				assert.Equal(t, tc.wantPath, n.route.pattern)
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
			f, _ := New(WithIgnoreTrailingSlash(true))
			rf := f.Stats()
			require.True(t, rf.IgnoreTrailingSlash)
			for _, path := range tc.paths {
				require.NoError(t, onlyError(f.Handle(tc.method, path, func(c Context) {
					_ = c.String(http.StatusOK, c.Pattern())
				})))
				rte := f.Route(tc.method, path)
				require.NotNil(t, rte)
				assert.True(t, rte.IgnoreTrailingSlashEnabled())
			}

			req := httptest.NewRequest(tc.method, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			if tc.wantPath != "" {
				assert.Equal(t, tc.wantPath, w.Body.String())
			}
		})
	}
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
			f, _ := New(WithRedirectTrailingSlash(true))
			rf := f.Stats()
			require.True(t, rf.RedirectTrailingSlash)
			for _, path := range tc.paths {
				require.NoError(t, onlyError(f.Handle(tc.method, path, emptyHandler)))
				rte := f.Route(tc.method, path)
				require.NotNil(t, rte)
				assert.True(t, rte.RedirectTrailingSlashEnabled())
			}

			req := httptest.NewRequest(tc.method, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			if w.Code == http.StatusPermanentRedirect || w.Code == http.StatusMovedPermanently {
				assert.Equal(t, tc.wantLocation, w.Header().Get(HeaderLocation))
				if tc.method == http.MethodGet {
					assert.Equal(t, MIMETextHTMLCharsetUTF8, w.Header().Get(HeaderContentType))
					assert.Equal(t, "<a href=\""+htmlEscape(w.Header().Get(HeaderLocation))+"\">"+http.StatusText(w.Code)+"</a>.\n\n", w.Body.String())
				}
			}
		})
	}
}

func TestEncodedRedirectTrailingSlash(t *testing.T) {
	r, _ := New(WithRedirectTrailingSlash(true))
	require.NoError(t, onlyError(r.Handle(http.MethodGet, "/foo/{bar}/", emptyHandler)))

	req := httptest.NewRequest(http.MethodGet, "/foo/bar%2Fbaz", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "bar%2Fbaz/", w.Header().Get(HeaderLocation))
}

func TestRouterWithTsrParams(t *testing.T) {
	cases := []struct {
		name       string
		routes     []string
		target     string
		wantParams Params
		wantPath   string
		wantTsr    bool
	}{
		{
			name:   "current not a leaf, with leave on incomplete to end of edge",
			routes: []string{"/{a}", "/foo/{b}", "/foo/{b}/x/", "/foo/{b}/y/"},
			target: "/foo/bar/",
			wantParams: Params{
				{
					Key:   "b",
					Value: "bar",
				},
			},
			wantPath: "/foo/{b}",
			wantTsr:  true,
		},
		{
			name:   "current not a leaf, with leave on end mid-edge",
			routes: []string{"/{a}/x", "/foo/{b}", "/foo/{b}/x/", "/foo/{b}/y/"},
			target: "/foo/bar/",
			wantParams: Params{
				{
					Key:   "b",
					Value: "bar",
				},
			},
			wantPath: "/foo/{b}",
			wantTsr:  true,
		},
		{
			name:   "current not a leaf, with leave on end mid-edge",
			routes: []string{"/{a}/{b}/e", "/foo/{b}", "/foo/{b}/x/", "/foo/{b}/y/"},
			target: "/foo/bar/",
			wantParams: Params{
				{
					Key:   "b",
					Value: "bar",
				},
			},
			wantPath: "/foo/{b}",
			wantTsr:  true,
		},
		{
			name:   "current not a leaf, with leave on not a leaf",
			routes: []string{"/{a}/{b}/e", "/{a}/{b}/d", "/foo/{b}", "/foo/{b}/x/", "/foo/{b}/y/"},
			target: "/foo/bar/",
			wantParams: Params{
				{
					Key:   "b",
					Value: "bar",
				},
			},
			wantPath: "/foo/{b}",
			wantTsr:  true,
		},
		{
			name:   "mid edge key, add an extra ts",
			routes: []string{"/{a}", "/foo/{b}/"},
			target: "/foo/bar",
			wantParams: Params{
				{
					Key:   "b",
					Value: "bar",
				},
			},
			wantPath: "/foo/{b}/",
			wantTsr:  true,
		},
		{
			name:   "mid edge key, remove an extra ts",
			routes: []string{"/{a}", "/foo/{b}/baz", "/foo/{b}"},
			target: "/foo/bar/",
			wantParams: Params{
				{
					Key:   "b",
					Value: "bar",
				},
			},
			wantPath: "/foo/{b}",
			wantTsr:  true,
		},
		{
			name:   "incomplete match end of edge, remove extra ts",
			routes: []string{"/{a}", "/foo/{b}"},
			target: "/foo/bar/",
			wantParams: Params{
				{
					Key:   "b",
					Value: "bar",
				},
			},
			wantPath: "/foo/{b}",
			wantTsr:  true,
		},
		{
			name:       "current not a leaf, should empty params",
			routes:     []string{"/{a}", "/foo", "/foo/x/", "/foo/y/"},
			target:     "/foo/",
			wantParams: Params(nil),
			wantPath:   "/foo",
			wantTsr:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(WithIgnoreTrailingSlash(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, func(c Context) {
					assert.Equal(t, tc.wantPath, c.Pattern())
					var params Params = slices.Collect(c.Params())
					assert.Equal(t, tc.wantParams, params)
					assert.Equal(t, tc.wantTsr, unwrapContext(t, c).tsr)
				})))
			}
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}

}

func TestTree_Delete(t *testing.T) {
	f, _ := New()
	routes := make([]route, len(githubAPI))
	copy(routes, githubAPI)

	for _, rte := range routes {
		require.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
	}

	rand.Shuffle(len(routes), func(i, j int) { routes[i], routes[j] = routes[j], routes[i] })

	for _, rte := range routes {
		deletedRoute, err := f.Delete(rte.method, rte.path)
		require.NoError(t, err)
		assert.Equal(t, rte.path, deletedRoute.Pattern())
	}

	it := f.Iter()
	cnt := len(slices.Collect(iterutil.Right(it.All())))

	tree := f.getRoot()
	assert.Equal(t, 0, cnt)
	assert.Equal(t, 4, len(tree.root))
}

func TestTree_DeleteTxn(t *testing.T) {
	f, _ := New()
	routes := make([]route, len(githubAPI))
	copy(routes, githubAPI)

	for _, rte := range routes {
		require.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
	}

	rand.Shuffle(len(routes), func(i, j int) { routes[i], routes[j] = routes[j], routes[i] })

	require.NoError(t, f.Updates(func(txn *Txn) error {
		for _, rte := range routes {
			deletedRoute, err := txn.Delete(rte.method, rte.path)
			if err != nil {
				return err
			}
			assert.Equal(t, rte.path, deletedRoute.Pattern())
		}
		return nil
	}))

	it := f.Iter()
	cnt := len(slices.Collect(iterutil.Right(it.All())))

	tree := f.getRoot()
	assert.Equal(t, 0, cnt)
	assert.Equal(t, 4, len(tree.root))
}

func TestTree_DeleteRoot(t *testing.T) {
	f, _ := New()
	require.NoError(t, onlyError(f.Handle(http.MethodOptions, "/foo/bar", emptyHandler)))
	deletedRoute, err := f.Delete(http.MethodOptions, "/foo/bar")
	require.NoError(t, err)
	assert.Equal(t, "/foo/bar", deletedRoute.Pattern())
	tree := f.getRoot()
	assert.Equal(t, 4, len(tree.root))
	require.NoError(t, onlyError(f.Handle(http.MethodOptions, "exemple.com/foo/bar", emptyHandler)))
	deletedRoute, err = f.Delete(http.MethodOptions, "exemple.com/foo/bar")
	require.NoError(t, err)
	assert.Equal(t, "exemple.com/foo/bar", deletedRoute.Pattern())
	tree = f.getRoot()
	assert.Equal(t, 4, len(tree.root))
}

func TestRouter_DeleteError(t *testing.T) {
	f, _ := New()
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/foo/bar", emptyHandler)))
	t.Run("delete with empty method", func(t *testing.T) {
		r, err := f.Delete("", "/foo/bar")
		assert.ErrorIs(t, err, ErrInvalidRoute)
		assert.Nil(t, r)
	})
	t.Run("delete invalid route", func(t *testing.T) {
		r, err := f.Delete(http.MethodGet, "/{")
		assert.ErrorIs(t, err, ErrInvalidRoute)
		assert.Nil(t, r)
	})
	t.Run("route does not exist", func(t *testing.T) {
		r, err := f.Delete(http.MethodGet, "/foo/bar/")
		assert.ErrorIs(t, err, ErrRouteNotFound)
		assert.Nil(t, r)
	})
	t.Run("method does not exist", func(t *testing.T) {
		r, err := f.Delete(http.MethodTrace, "/foo/bar")
		assert.ErrorIs(t, err, ErrRouteNotFound)
		assert.Nil(t, r)
	})
}

func TestRouter_UpdatesError(t *testing.T) {
	f, _ := New()
	wantErr := errors.New("error")
	err := f.Updates(func(txn *Txn) error {
		for _, rte := range staticRoutes {
			if err := onlyError(txn.Handle(rte.method, rte.path, emptyHandler)); err != nil {
				return err
			}
		}
		return wantErr
	})
	assert.ErrorIs(t, err, wantErr)
	tree := f.getRoot()
	assert.Len(t, tree.root[0].children, 0)
}

func TestRouter_UpdatesPanic(t *testing.T) {
	f, _ := New()

	assert.Panics(t, func() {
		_ = f.Updates(func(txn *Txn) error {
			for _, rte := range staticRoutes {
				if err := onlyError(txn.Handle(rte.method, rte.path, emptyHandler)); err != nil {
					return err
				}
			}
			panic("panic")
		})
	})

	tree := f.getRoot()
	assert.Len(t, tree.root[0].children, 0)
}

func TestTree_DeleteWildcard(t *testing.T) {
	f, _ := New()
	f.MustHandle(http.MethodGet, "/foo/*{args}", emptyHandler)
	deletedRoute, err := f.Delete(http.MethodGet, "/foo")
	assert.ErrorIs(t, err, ErrRouteNotFound)
	assert.Nil(t, deletedRoute)
	f.MustHandle(http.MethodGet, "/foo/{bar}", emptyHandler)
	deletedRoute, err = f.Delete(http.MethodGet, "/foo/{bar}")
	assert.NoError(t, err)
	assert.Equal(t, "/foo/{bar}", deletedRoute.Pattern())
	assert.True(t, f.Has(http.MethodGet, "/foo/*{args}"))
}

func TestTree_Methods(t *testing.T) {
	f, _ := New()
	for _, rte := range githubAPI {
		require.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
	}

	methods := slices.Sorted(iterutil.Left(f.Iter().Reverse(f.Iter().Methods(), "", "/gists/123/star")))
	assert.Equal(t, []string{"DELETE", "GET", "PUT"}, methods)

	methods = slices.Sorted(f.Iter().Methods())
	assert.Equal(t, []string{"DELETE", "GET", "POST", "PUT"}, methods)

	// Ignore trailing slash disable
	methods = slices.Sorted(iterutil.Left(f.Iter().Reverse(f.Iter().Methods(), "", "/gists/123/star/")))
	assert.Empty(t, methods)
}

func TestTree_MethodsWithIgnoreTsEnable(t *testing.T) {
	f, _ := New(WithIgnoreTrailingSlash(true))
	for _, method := range []string{"DELETE", "GET", "PUT"} {
		require.NoError(t, onlyError(f.Handle(method, "/foo/bar", emptyHandler)))
		require.NoError(t, onlyError(f.Handle(method, "/john/doe/", emptyHandler)))
	}

	methods := slices.Sorted(iterutil.Left(f.Iter().Reverse(f.Iter().Methods(), "", "/foo/bar/")))
	assert.Equal(t, []string{"DELETE", "GET", "PUT"}, methods)

	methods = slices.Sorted(iterutil.Left(f.Iter().Reverse(f.Iter().Methods(), "", "/john/doe")))
	assert.Equal(t, []string{"DELETE", "GET", "PUT"}, methods)

	methods = slices.Sorted(iterutil.Left(f.Iter().Reverse(f.Iter().Methods(), "", "/foo/bar/baz")))
	assert.Empty(t, methods)
}

func TestRouterWithAllowedMethod(t *testing.T) {
	f, _ := New(WithNoMethod(true))

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
			assert.Equal(t, tc.want, w.Header().Get("Allow"))
		})
	}
}

func TestRouterHandleNoRoute(t *testing.T) {
	called := 0
	m := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			called++
			next(c)
		}
	})

	f, err := New(WithMiddleware(m))
	require.NoError(t, err)
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/foo", func(c Context) {
		c.Fox().HandleNoRoute(c)
	})))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, 1, called)

}

func TestRouterWithAllowedMethodAndIgnoreTsEnable(t *testing.T) {
	f, _ := New(WithNoMethod(true), WithIgnoreTrailingSlash(true))

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
				require.NoError(t, onlyError(f.Handle(method, tc.path, emptyHandler)))
			}
			req := httptest.NewRequest(tc.target, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("Allow"))
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
		want    string
		methods []string
	}{
		{
			name:    "all route except the last one",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead},
			path:    "/foo/bar",
			req:     "/foo/bar",
			target:  http.MethodTrace,
			want:    "GET, POST, PUT, DELETE, PATCH, CONNECT, OPTIONS, HEAD",
		},
		{
			name:    "all route except the first one and inferred options from auto options",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz/",
			req:     "/foo/baz/",
			target:  http.MethodGet,
			want:    "POST, PUT, DELETE, PATCH, CONNECT, HEAD, TRACE, OPTIONS",
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
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("Allow"))
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

func TestRouterWithAutomaticOptions(t *testing.T) {

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
			f, _ := New(WithAutoOptions(true))
			rf := f.Stats()
			require.True(t, rf.AutoOptions)
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Handle(method, tc.path, func(c Context) {
					c.SetHeader("Allow", strings.Join(slices.Sorted(iterutil.Left(c.Fox().Iter().Reverse(c.Fox().Iter().Methods(), c.Host(), c.Path()))), ", "))
					c.Writer().WriteHeader(http.StatusNoContent)
				})))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("Allow"))
		})
	}
}

func TestRouterWithAutomaticOptionsAndIgnoreTsOptionEnable(t *testing.T) {
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
			f, _ := New(WithAutoOptions(true), WithIgnoreTrailingSlash(true))
			for _, method := range tc.methods {
				require.NoError(t, onlyError(f.Handle(method, tc.path, func(c Context) {
					c.SetHeader("Allow", strings.Join(slices.Sorted(iterutil.Left(c.Fox().Iter().Reverse(c.Fox().Iter().Methods(), c.Host(), c.Path()))), ", "))
					c.Writer().WriteHeader(http.StatusNoContent)
				})))
			}
			req := httptest.NewRequest(http.MethodOptions, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.want, w.Header().Get("Allow"))
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
					c.SetHeader("Allow", strings.Join(slices.Sorted(iterutil.Left(c.Fox().Iter().Reverse(c.Fox().Iter().Methods(), c.Host(), c.Path()))), ", "))
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
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "GET, POST, OPTIONS", w.Header().Get("Allow"))
	f, err = New(WithOptionsHandler(nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestDefaultOptions(t *testing.T) {
	m := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			next(c)
		}
	})
	r, err := New(WithMiddleware(m), DefaultOptions())
	require.NoError(t, err)
	assert.Equal(t, reflect.ValueOf(m).Pointer(), reflect.ValueOf(r.mws[2].m).Pointer())
	assert.True(t, r.handleOptions)
}

func TestInvalidAnnotation(t *testing.T) {
	var nonComparableKey = []int{1, 2, 3}
	f, err := New()
	require.NoError(t, err)
	assert.ErrorIs(t, onlyError(f.Handle(http.MethodGet, "/foo/{bar}", emptyHandler, WithAnnotation(nonComparableKey, nil))), ErrInvalidConfig)
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

func TestUpdateWithMiddleware(t *testing.T) {
	called := false
	m := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			called = true
			next(c)
		}
	})
	f, _ := New(WithMiddleware(Recovery()))
	f.MustHandle(http.MethodGet, "/foo", emptyHandler)
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	w := httptest.NewRecorder()

	// Add middleware
	require.NoError(t, onlyError(f.Update(http.MethodGet, "/foo", emptyHandler, WithMiddleware(m))))
	f.ServeHTTP(w, req)
	assert.True(t, called)
	called = false

	rte := f.Route(http.MethodGet, "/foo")
	rte.Handle(newTestContext(f))
	assert.False(t, called)
	called = false

	rte.HandleMiddleware(newTestContext(f))
	assert.True(t, called)
	called = false

	// Remove middleware
	require.NoError(t, onlyError(f.Update(http.MethodGet, "/foo", emptyHandler)))
	f.ServeHTTP(w, req)
	assert.False(t, called)
	called = false

	rte = f.Route(http.MethodGet, "/foo")
	rte.Handle(newTestContext(f))
	assert.False(t, called)
	called = false

	rte = f.Route(http.MethodGet, "/foo")
	rte.HandleMiddleware(newTestContext(f))
	assert.False(t, called)
}

func TestRouteMiddleware(t *testing.T) {
	var c0, c1, c2 bool
	m0 := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			c0 = true
			next(c)
		}
	})

	m1 := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			c1 = true
			next(c)
		}
	})

	m2 := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			c2 = true
			next(c)
		}
	})
	f, err := New(WithMiddleware(m0))
	require.NoError(t, err)
	f.MustHandle(http.MethodGet, "/1", emptyHandler, WithMiddleware(m1))
	f.MustHandle(http.MethodGet, "/2", emptyHandler, WithMiddleware(m2))

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
	rte1 := f.Route(http.MethodGet, "/1")
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

	rte2 := f.Route(http.MethodGet, "/2")
	require.NotNil(t, rte2)
	rte2.HandleMiddleware(newTestContext(f))
	assert.False(t, c0)
	assert.False(t, c1)
	assert.True(t, c2)
}

func TestInvalidMiddleware(t *testing.T) {
	_, err := New(WithMiddleware(Logger(), nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
	_, err = New(WithMiddlewareFor(NoRouteHandler, nil, Logger()))
	assert.ErrorIs(t, err, ErrInvalidConfig)
	f, err := New()
	require.NoError(t, err)
	require.ErrorIs(t, onlyError(f.Handle(http.MethodGet, "/foo", emptyHandler, WithMiddleware(nil))), ErrInvalidConfig)
}

func TestMiddlewareLength(t *testing.T) {
	f, _ := New(DefaultOptions())
	r := f.MustHandle(http.MethodGet, "/", emptyHandler, WithMiddleware(Recovery(), Logger()))
	assert.Len(t, f.mws, 2)
	assert.Len(t, r.mws, 4)
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

func TestRouter_Lookup(t *testing.T) {
	rx := regexp.MustCompile("({|\\*{)[A-z]+[}]")
	f, _ := New()
	for _, rte := range githubAPI {
		require.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
	}

	for _, rte := range githubAPI {
		req := httptest.NewRequest(rte.method, rte.path, nil)
		route, cc, _ := f.Lookup(newResponseWriter(mockResponseWriter{}), req)
		require.NotNil(t, cc)
		require.NotNil(t, route)
		assert.Equal(t, rte.path, route.Pattern())

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
	route, cc, _ := f.Lookup(newResponseWriter(mockResponseWriter{}), req)
	assert.Nil(t, route)
	assert.Nil(t, cc)

	// No path match
	req = httptest.NewRequest(http.MethodGet, "/bar", nil)
	route, cc, _ = f.Lookup(newResponseWriter(mockResponseWriter{}), req)
	assert.Nil(t, route)
	assert.Nil(t, cc)
}

func TestRouter_Reverse(t *testing.T) {
	t.Run("reverse no tsr", func(t *testing.T) {
		f, _ := New()
		for _, rte := range staticRoutes {
			require.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
		}
		for _, rte := range staticRoutes {
			route, tsr := f.Reverse(rte.method, "", rte.path)
			assert.False(t, tsr)
			require.NotNil(t, route)
			assert.Equal(t, rte.path, route.Pattern())
		}
	})

	t.Run("reverse with tsr", func(t *testing.T) {
		f, _ := New()
		for _, rte := range staticRoutes {
			if rte.path == "/" {
				continue
			}
			require.NoError(t, onlyError(f.Handle(rte.method, rte.path+"/", emptyHandler)))
		}
		for _, rte := range staticRoutes {
			if rte.path == "/" {
				continue
			}
			route, tsr := f.Reverse(rte.method, "", rte.path)
			require.True(t, tsr)
			assert.Equal(t, rte.path+"/", route.Pattern())
		}
	})
}

func TestTree_Has(t *testing.T) {
	routes := []string{
		"/foo/bar",
		"/welcome/{name}",
		"/users/uid_{id}",
		"/john/doe/",
	}

	f, _ := New()
	for _, rte := range routes {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
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
			name: "strict match static route",
			path: "/john/doe/",
			want: true,
		},
		{
			name: "no match static route (tsr)",
			path: "/foo/bar/",
		},
		{
			name: "no match static route (tsr)",
			path: "/john/doe",
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

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, f.Has(http.MethodGet, tc.path))
		})
	}
}

func TestTree_Route(t *testing.T) {
	routes := []string{
		"/foo/bar",
		"/welcome/{name}",
		"/users/uid_{id}",
	}

	f, _ := New()
	for _, rte := range routes {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
	}

	cases := []struct {
		name    string
		path    string
		want    string
		wantTsr bool
	}{
		{
			name: "reverse static route",
			path: "/foo/bar",
			want: "/foo/bar",
		},
		{
			name:    "reverse static route with tsr disable",
			path:    "/foo/bar/",
			want:    "/foo/bar",
			wantTsr: true,
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
			route, tsr := f.Reverse(http.MethodGet, "", tc.path)
			if tc.want != "" {
				require.NotNil(t, route)
				assert.Equal(t, tc.want, route.Pattern())
				assert.Equal(t, tc.wantTsr, tsr)
				return
			}
			assert.Nil(t, route)
		})
	}
}

func TestTree_RouteWithIgnoreTrailingSlashEnable(t *testing.T) {
	routes := []string{
		"/foo/bar",
		"/welcome/{name}/",
		"/users/uid_{id}",
	}

	f, _ := New(WithIgnoreTrailingSlash(true))
	for _, rte := range routes {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
	}

	cases := []struct {
		name    string
		path    string
		want    string
		wantTsr bool
	}{
		{
			name: "reverse static route",
			path: "/foo/bar",
			want: "/foo/bar",
		},
		{
			name:    "reverse static route with tsr",
			path:    "/foo/bar/",
			want:    "/foo/bar",
			wantTsr: true,
		},
		{
			name: "reverse params route",
			path: "/welcome/fox/",
			want: "/welcome/{name}/",
		},
		{
			name:    "reverse params route with tsr",
			path:    "/welcome/fox",
			want:    "/welcome/{name}/",
			wantTsr: true,
		},
		{
			name: "reverse mid params route",
			path: "/users/uid_123",
			want: "/users/uid_{id}",
		},
		{
			name:    "reverse mid params route with tsr",
			path:    "/users/uid_123/",
			want:    "/users/uid_{id}",
			wantTsr: true,
		},
		{
			name: "reverse no match",
			path: "/users/fox",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route, tsr := f.Reverse(http.MethodGet, "", tc.path)
			if tc.want != "" {
				require.NotNil(t, route)
				assert.Equal(t, tc.want, route.Pattern())
				assert.Equal(t, tc.wantTsr, tsr)
				return
			}
			assert.Nil(t, route)
		})
	}
}

func TestEncodedPath(t *testing.T) {
	encodedPath := "run/cmd/S123L%2FA"
	req := httptest.NewRequest(http.MethodGet, "/"+encodedPath, nil)
	w := httptest.NewRecorder()

	f, _ := New()
	f.MustHandle(http.MethodGet, "/*{request}", func(c Context) {
		_ = c.String(http.StatusOK, "%s", c.Param("request"))
	})

	f.ServeHTTP(w, req)
	assert.Equal(t, encodedPath, w.Body.String())
}

func TestEqualASCIIIgnoreCase(t *testing.T) {
	tests := []struct {
		name string
		s    uint8
		t    uint8
		want bool
	}{
		// Exact matches
		{"same lowercase letter", 'a', 'a', true},
		{"same uppercase letter", 'A', 'A', true},
		{"same digit", '5', '5', true},
		{"same hyphen", '-', '-', true},

		// Case-insensitive letter matches
		{"A and a", 'A', 'a', true},
		{"a and A", 'a', 'A', true},
		{"Z and z", 'Z', 'z', true},
		{"z and Z", 'z', 'Z', true},
		{"M and m", 'M', 'm', true},
		{"m and M", 'm', 'M', true},

		// Different letters (should not match)
		{"A and B", 'A', 'B', false},
		{"a and b", 'a', 'b', false},
		{"A and b", 'A', 'b', false},
		{"a and B", 'a', 'B', false},

		// Digits (only match exactly)
		{"0 and 0", '0', '0', true},
		{"9 and 9", '9', '9', true},
		{"0 and 1", '0', '1', false},
		{"5 and 6", '5', '6', false},

		// Hyphen (only matches exactly)
		{"hyphen and hyphen", '-', '-', true},
		{"hyphen and A", '-', 'A', false},
		{"hyphen and a", '-', 'a', false},
		{"hyphen and 0", '-', '0', false},

		// Characters just outside letter ranges
		{"@ and A", '@', 'A', false},
		{"Z and [", 'Z', '[', false},
		{"` and a", '`', 'a', false},
		{"z and {", 'z', '{', false},

		// Special characters and control chars
		{"null and A", 0, 'A', false},
		{"A and null", 'A', 0, false},
		{"space and A", ' ', 'A', false},
		{"A and space", 'A', ' ', false},
		{"! and A", '!', 'A', false},
		{"A and !", 'A', '!', false},
		{"/ and A", '/', 'A', false},
		{"A and /", 'A', '/', false},

		// High ASCII values
		{"high byte and A", 0xFF, 'A', false},
		{"A and high byte", 'A', 0xFF, false},
		{"high byte and a", 0xFF, 'a', false},
		{"a and high byte", 'a', 0xFF, false},

		// Case difference edge cases
		{"@ and `", '@', '`', false},
		{"0 and P", '0', 'P', false},

		// Boundary cases for the letter ranges
		{"A-1 and a", 'A' - 1, 'a', false},
		{"Z+1 and z", 'Z' + 1, 'z', false},
		{"a-1 and A", 'a' - 1, 'A', false},
		{"z+1 and Z", 'z' + 1, 'Z', false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalASCIIIgnoreCase(tt.s, tt.t); got != tt.want {
				t.Errorf("equalASCIIIgnoreCase(%c=%d, %c=%d) = %v, want %v",
					tt.s, tt.s, tt.t, tt.t, got, tt.want)
			}
		})
	}
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

	r, _ := New()
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
		path := fmt.Sprintf(routeFormat, s1, e1, s2, e2, e3)
		tree := r.getRoot()
		txn := tree.txn(true)
		if err := txn.insert(http.MethodGet, &Route{pattern: path, hself: emptyHandler, psLen: 3}); err == nil {
			c := newTestContext(r)
			n, tsr := lookupByPath(tree, txn.root[0].children[0], fmt.Sprintf(reqFormat, s1, "xxxx", s2, "xxxx", "xxxx"), c, false)
			require.NotNil(t, n)
			require.NotNil(t, n.route)
			assert.False(t, tsr)
			assert.Equal(t, fmt.Sprintf(routeFormat, s1, e1, s2, e2, e3), n.route.pattern)
			assert.Equal(t, "xxxx", c.Param(e1))
			assert.Equal(t, "xxxx", c.Param(e2))
			assert.Equal(t, "xxxx", c.Param(e3))
		}
	}
}

func TestFuzzInsertNoPanics(t *testing.T) {
	f := fuzz.New().NilChance(0).NumElements(5000, 10000)
	r, _ := New()

	routes := make(map[string]struct{})
	f.Fuzz(&routes)

	tree := r.getRoot()
	txn := tree.txn(true)

	for rte := range routes {
		if rte == "" {
			continue
		}
		require.NotPanicsf(t, func() {
			_ = txn.insert(http.MethodGet, &Route{pattern: rte, hself: emptyHandler, hostSplit: max(0, strings.IndexByte(rte, '/'))})
		}, fmt.Sprintf("rte: %s", rte))
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
	r, _ := New()

	routes := make(map[string]struct{})
	f.Fuzz(&routes)

	tree := r.getRoot()
	txn := tree.txn(true)
	for rte := range routes {
		path := "/" + rte
		err := txn.insert(http.MethodGet, &Route{pattern: path, hself: emptyHandler})
		require.NoError(t, err)
	}
	r.tree.Store(txn.commit())

	it := r.Iter()
	countPath := len(slices.Collect(iterutil.Right(it.All())))
	assert.Equal(t, len(routes), countPath)

	tree = r.getRoot()
	txn = tree.txn(true)
	for rte := range routes {
		c := newTestContext(r)
		n, tsr := lookupByPath(tree, tree.root[0].children[0], "/"+rte, c, true)
		require.NotNilf(t, n, "route /%s", rte)
		require.NotNilf(t, n.route, "route /%s", rte)
		require.Falsef(t, tsr, "tsr: %t", tsr)
		require.Truef(t, n.isLeaf(), "route /%s", rte)
		require.Equal(t, "/"+rte, n.route.pattern)
		path := "/" + rte
		require.NoError(t, txn.update(http.MethodGet, &Route{pattern: path, hself: emptyHandler}))
	}

	for rte := range routes {
		n, deleted := txn.remove(http.MethodGet, "/"+rte)
		require.True(t, deleted)
		require.NotNil(t, n)
		require.NotNil(t, n.route)
		assert.Equal(t, "/"+rte, n.route.pattern)
	}
	r.tree.Store(txn.commit())

	it = r.Iter()
	countPath = len(slices.Collect(iterutil.Right(it.All())))
	assert.Equal(t, 0, countPath)
}

func TestRaceHostnamePathSwitch(t *testing.T) {
	var wg sync.WaitGroup
	start, wait := atomicSync()

	f, _ := New()

	h := func(c Context) {}

	require.NoError(t, f.Updates(func(txn *Txn) error {
		for _, rte := range githubAPI {
			if err := onlyError(txn.Handle(rte.method, rte.path, h)); err != nil {
				return err
			}
		}
		return nil
	}))

	wg.Add(1000 * 3)
	for range 1000 {

		go func() {
			wait()
			defer wg.Done()
			require.NoError(t, f.Updates(func(txn *Txn) error {
				if txn.Has(githubAPI[0].method, "{sub}.bar.{tld}"+githubAPI[0].path) {
					for _, rte := range githubAPI {
						if _, err := txn.Delete(rte.method, "{sub}.bar.{tld}"+rte.path); err != nil {
							return err
						}
					}
					return nil
				}

				for _, rte := range githubAPI {
					if err := onlyError(txn.Handle(rte.method, "{sub}.bar.{tld}"+rte.path, h)); err != nil {
						return err
					}
				}
				return nil
			}))

		}()

		go func() {
			wait()
			defer wg.Done()
			require.NoError(t, f.Updates(func(txn *Txn) error {
				if txn.Has(githubAPI[0].method, "foo.bar.baz"+githubAPI[0].path) {
					for _, rte := range githubAPI {
						if _, err := txn.Delete(rte.method, "foo.bar.baz"+rte.path); err != nil {
							return err
						}
					}
					return nil
				}

				for _, rte := range githubAPI {
					if err := onlyError(txn.Handle(rte.method, "foo.bar.baz"+rte.path, h)); err != nil {
						return err
					}
				}
				return nil
			}))
		}()

		go func() {
			wait()
			defer wg.Done()
			for range 5 {
				for _, rte := range githubAPI {
					req := httptest.NewRequest(rte.method, rte.path, nil)
					req.Host = "foo.bar.baz"
					w := httptest.NewRecorder()
					f.ServeHTTP(w, req)
					assert.Equal(t, http.StatusOK, w.Code)
				}
			}
		}()
	}

	time.Sleep(500 * time.Millisecond)
	start()
	wg.Wait()

	// With a pair number of iteration, we should always delete all domains
	tree := f.getRoot()
	for _, n := range tree.root {
		assert.Len(t, n.children, 1)
	}

}

func TestDataRace(t *testing.T) {
	var wg sync.WaitGroup
	start, wait := atomicSync()

	h := HandlerFunc(func(c Context) {
		c.Pattern()
		for range c.Params() {
		}
	})
	newH := HandlerFunc(func(c Context) {
		c.Pattern()
		for range c.Params() {
		}
	})

	f, _ := New()

	w := new(mockResponseWriter)

	wg.Add(len(githubAPI) * 4)
	for _, rte := range githubAPI {
		go func(method, route string) {
			wait()
			defer wg.Done()
			txn := f.Txn(true)
			defer txn.Abort()
			if txn.Has(method, route) {
				if assert.NoError(t, onlyError(txn.Update(method, route, h))) {
					txn.Commit()
				}
				return
			}
			if assert.NoError(t, onlyError(txn.Handle(method, route, h))) {
				txn.Commit()
			}
		}(rte.method, rte.path)

		go func(method, route string) {
			wait()
			defer wg.Done()
			txn := f.Txn(true)
			defer txn.Abort()
			if txn.Has(method, route) {
				_, err := txn.Delete(method, route)
				if assert.NoError(t, err) {
					txn.Commit()
				}
				return
			}
			if assert.NoError(t, onlyError(txn.Handle(method, route, newH))) {
				txn.Commit()
			}
		}(rte.method, rte.path)

		go func() {
			wait()
			defer wg.Done()
			for route := range iterutil.Right(f.Iter().All()) {
				route.Pattern()
				route.Annotation("foo")
			}
		}()

		go func(method, route string) {
			wait()
			defer wg.Done()
			req := httptest.NewRequest(method, route, nil)
			f.ServeHTTP(w, req)
		}(rte.method, rte.path)
	}

	time.Sleep(500 * time.Millisecond)
	start()
	wg.Wait()
}

func TestConcurrentRequestHandling(t *testing.T) {
	r, _ := New()

	// /repos/{owner}/{repo}/keys
	h1 := HandlerFunc(func(c Context) {
		assert.Equal(t, "john", c.Param("owner"))
		assert.Equal(t, "fox", c.Param("repo"))
		_ = c.String(200, c.Pattern())
	})

	// /repos/{owner}/{repo}/contents/*{path}
	h2 := HandlerFunc(func(c Context) {
		assert.Equal(t, "alex", c.Param("owner"))
		assert.Equal(t, "vault", c.Param("repo"))
		assert.Equal(t, "file.txt", c.Param("path"))
		_ = c.String(200, c.Pattern())
	})

	// /users/{user}/received_events/public
	h3 := HandlerFunc(func(c Context) {
		assert.Equal(t, "go", c.Param("user"))
		_ = c.String(200, c.Pattern())
	})

	require.NoError(t, onlyError(r.Handle(http.MethodGet, "/repos/{owner}/{repo}/keys", h1)))
	require.NoError(t, onlyError(r.Handle(http.MethodGet, "/repos/{owner}/{repo}/contents/*{path}", h2)))
	require.NoError(t, onlyError(r.Handle(http.MethodGet, "/users/{user}/received_events/public", h3)))

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

func TestNode_String(t *testing.T) {
	f, _ := New()
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/foo/{bar}/*{baz}", emptyHandler)))
	tree := f.getRoot()

	want := `path: GET
      path: /foo/{bar}/*{baz} [leaf=/foo/{bar}/*{baz}] [bar (10), baz (-1)]`
	assert.Equal(t, want, strings.TrimSuffix(tree.root[0].String(), "\n"))
}

func TestNode_Debug(t *testing.T) {
	f, _ := New()
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/foo/*{any}/bar", emptyHandler)))
	tree := f.getRoot()

	want := `path: GET
      path: /foo/*{any}/bar [leaf=/foo/*{any}/bar] [any (11)]
             inode: /bar`
	assert.Equal(t, want, strings.TrimSuffix(tree.root[0].Debug(), "\n"))
}

// This example demonstrates how to create a simple router using the default options,
// which include the Recovery and Logger middleware.
func ExampleNew() {
	// Create a new router with default options, which include the Recovery and Logger middleware
	r, _ := New(DefaultOptions())

	// Define a route with the path "/hello/{name}", and set a simple handler that greets the
	// user by their name.
	r.MustHandle(http.MethodGet, "/hello/{name}", func(c Context) {
		_ = c.String(200, "Hello %s\n", c.Param("name"))
	})

	// Start the HTTP server using fox router and listen on port 8080
	log.Fatalln(http.ListenAndServe(":8080", r))
}

// This example demonstrates how to register a global middleware that will be
// applied to all routes.
func ExampleWithMiddleware() {
	// Define a custom middleware to measure the time taken for request processing and
	// log the URL, route, time elapsed, and status code.
	metrics := func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			start := time.Now()
			next(c)
			log.Printf(
				"url=%s; route=%s; time=%d; status=%d",
				c.Request().URL,
				c.Pattern(),
				time.Since(start),
				c.Writer().Status(),
			)
		}
	}

	f, _ := New(WithMiddleware(metrics))

	f.MustHandle(http.MethodGet, "/hello/{name}", func(c Context) {
		_ = c.String(200, "Hello %s\n", c.Param("name"))
	})
}

// This example demonstrates how to create a custom middleware that cleans the request path and performs a manual
// lookup on the tree. If the cleaned path matches a registered route, the client is redirected to the valid path.
func ExampleRouter_Lookup() {
	redirectFixedPath := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			req := c.Request()
			target := req.URL.Path
			cleanedPath := CleanPath(target)

			// Nothing to clean, call next handler.
			if cleanedPath == target {
				next(c)
				return
			}

			req.URL.Path = cleanedPath
			route, cc, tsr := c.Fox().Lookup(c.Writer(), req)
			if route != nil {
				defer cc.Close()

				code := http.StatusMovedPermanently
				if req.Method != http.MethodGet {
					code = http.StatusPermanentRedirect
				}

				// Redirect the client if direct match or indirect match.
				if !tsr || route.IgnoreTrailingSlashEnabled() {
					if err := c.Redirect(code, cleanedPath); err != nil {
						// Only if not in the range 300..308, so not possible here!
						panic(err)
					}
					return
				}

				// Add or remove an extra trailing slash and redirect the client.
				if route.RedirectTrailingSlashEnabled() {
					if err := c.Redirect(code, FixTrailingSlash(cleanedPath)); err != nil {
						// Only if not in the range 300..308, so not possible here
						panic(err)
					}
					return
				}
			}

			// rollback to the original path before calling the
			// next handler or middleware.
			req.URL.Path = target
			next(c)
		}
	})

	f, _ := New(
		// Register the middleware for the NoRouteHandler scope.
		WithMiddlewareFor(NoRouteHandler|NoMethodHandler, redirectFixedPath),
	)

	f.MustHandle(http.MethodGet, "/hello/{name}", func(c Context) {
		_ = c.String(200, "Hello %s\n", c.Param("name"))
	})
}

// This example demonstrates how to do a reverse lookup on the tree.
func ExampleRouter_Reverse() {
	f, _ := New()
	f.MustHandle(http.MethodGet, "exemple.com/hello/{name}", emptyHandler)
	route, _ := f.Reverse(http.MethodGet, "exemple.com", "/hello/fox")
	fmt.Println(route.Pattern()) // /hello/{name}
}

// This example demonstrates how to check if a given route is registered in the tree.
func ExampleRouter_Has() {
	f, _ := New()
	f.MustHandle(http.MethodGet, "/hello/{name}", emptyHandler)
	exist := f.Has(http.MethodGet, "/hello/{name}")
	fmt.Println(exist) // true
}

// This example demonstrate how to create a managed read-write transaction.
func ExampleRouter_Updates() {
	f, _ := New()

	// Updates executes a function within the context of a read-write managed transaction. If no error is returned
	// from the function then the transaction is committed. If an error is returned then the entire transaction is
	// aborted.
	if err := f.Updates(func(txn *Txn) error {
		if _, err := txn.Handle(http.MethodGet, "exemple.com/hello/{name}", func(c Context) {
			_ = c.String(http.StatusOK, "hello %s", c.Param("name"))
		}); err != nil {
			return err
		}

		// Iter returns a collection of range iterators for traversing registered routes.
		it := txn.Iter()
		// When Iter() is called on a write transaction, it creates a point-in-time snapshot of the transaction state.
		// It means that writing on the current transaction while iterating is allowed, but the mutation will not be
		// observed in the result returned by Prefix (or any other iterator).
		for method, route := range it.Prefix(it.Methods(), "tmp.exemple.com/") {
			if _, err := f.Delete(method, route.Pattern()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Printf("transaction aborted: %s", err)
	}
}

// This example demonstrate how to create an unmanaged read-write transaction.
func ExampleRouter_Txn() {
	f, _ := New()

	// Txn create an unmanaged read-write or read-only transaction.
	txn := f.Txn(true)
	defer txn.Abort()

	if _, err := txn.Handle(http.MethodGet, "exemple.com/hello/{name}", func(c Context) {
		_ = c.String(http.StatusOK, "hello %s", c.Param("name"))
	}); err != nil {
		log.Printf("error inserting route: %s", err)
		return
	}

	// Iter returns a collection of range iterators for traversing registered routes.
	it := txn.Iter()
	// When Iter() is called on a write transaction, it creates a point-in-time snapshot of the transaction state.
	// It means that writing on the current transaction while iterating is allowed, but the mutation will not be
	// observed in the result returned by Prefix (or any other iterator).
	for method, route := range it.Prefix(it.Methods(), "tmp.exemple.com/") {
		if _, err := f.Delete(method, route.Pattern()); err != nil {
			log.Printf("error deleting route: %s", err)
			return
		}
	}
	// Finalize the transaction
	txn.Commit()
}

// This example demonstrate how to create a managed read-only transaction.
func ExampleRouter_View() {
	f, _ := New()

	// View executes a function within the context of a read-only managed transaction.
	_ = f.View(func(txn *Txn) error {
		if txn.Has(http.MethodGet, "/foo") && txn.Has(http.MethodGet, "/bar") {
			// Do something
		}
		return nil
	})
}

func onlyError[T any](_ T, err error) error {
	return err
}
