// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"fmt"
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

	"github.com/gorilla/mux"
	"github.com/tigerwill90/fox/internal/iterutil"
	"github.com/tigerwill90/fox/internal/netutil"

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
	{"GET", "makefile"},
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
	{"GET", "articles.wiki.makefile"},
	{"GET", "articles.wiki.notemplate.go"},
	{"GET", "articles.wiki.part1-noerror.go"},
	{"GET", "articles.wiki.part1.go"},
	{"GET", "articles.wiki.part2.go"},
	{"GET", "iptv-sfr"},
	{"GET", "articles.wiki.part3.go"},
	{"GET", "articles.wiki.test.bash"},
	{"GET", "articles.wiki.test_edit.good"},
	{"GET", "articles.wiki.test_test.txt.good"},
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

var wildcardHostnames = []route{
	// OAuth Authorizations
	{"GET", "authorizations"},
	{"GET", "authorizations.{id}"},
	{"POST", "authorizations"},
	{"DELETE", "authorizations.{id}"},
	{"GET", "applications.{client_id}.tokens.{access_token}"},
	{"DELETE", "applications.{client_id}.tokens"},
	{"DELETE", "applications.{client_id}.tokens.{access_token}"},

	// Activity
	{"GET", "events"},
	{"GET", "repos.{owner}.{repo}.events"},
	{"GET", "networks.{owner}.{repo}.events"},
	{"GET", "orgs.{org}.events"},
	{"GET", "users.{user}.received_events"},
	{"GET", "users.{user}.received_events.public"},
	{"GET", "users.{user}.events"},
	{"GET", "users.{user}.events.public"},
	{"GET", "users.{user}.events.orgs.{org}"},
	{"GET", "feeds"},
	{"GET", "notifications"},
	{"GET", "repos.{owner}.{repo}.notifications"},
	{"PUT", "notifications"},
	{"PUT", "repos.{owner}.{repo}.notifications"},
	{"GET", "notifications.threads.{id}"},
	{"GET", "notifications.threads.{id}.subscription"},
	{"PUT", "notifications.threads.{id}.subscription"},
	{"DELETE", "notifications.threads.{id}.subscription"},
	{"GET", "repos.{owner}.{repo}.stargazers"},
	{"GET", "users.{user}.starred"},
	{"GET", "user.starred"},
	{"GET", "user.starred.{owner}.{repo}"},
	{"PUT", "user.starred.{owner}.{repo}"},
	{"DELETE", "user.starred.{owner}.{repo}"},
	{"GET", "repos.{owner}.{repo}.subscribers"},
	{"GET", "users.{user}.subscriptions"},
	{"GET", "user.subscriptions"},
	{"GET", "repos.{owner}.{repo}.subscription"},
	{"PUT", "repos.{owner}.{repo}.subscription"},
	{"DELETE", "repos.{owner}.{repo}.subscription"},
	{"GET", "user.subscriptions.{owner}.{repo}"},
	{"PUT", "user.subscriptions.{owner}.{repo}"},
	{"DELETE", "user.subscriptions.{owner}.{repo}"},

	// Gists
	{"GET", "users.{user}.gists"},
	{"GET", "gists"},
	{"GET", "gists.{id}"},
	{"POST", "gists"},
	{"PUT", "gists.{id}.star"},
	{"DELETE", "gists.{id}.star"},
	{"GET", "gists.{id}.star"},
	{"POST", "gists.{id}.forks"},
	{"DELETE", "gists.{id}"},

	// Git Data
	{"GET", "repos.{owner}.{repo}.git.blobs.{sha}"},
	{"POST", "repos.{owner}.{repo}.git.blobs"},
	{"GET", "repos.{owner}.{repo}.git.commits.{sha}"},
	{"POST", "repos.{owner}.{repo}.git.commits"},
	{"GET", "repos.{owner}.{repo}.git.refs.{ref}"},
	{"GET", "repos.{owner}.{repo}.git.refs"},
	{"POST", "repos.{owner}.{repo}.git.refs"},
	{"DELETE", "repos.{owner}.{repo}.git.refs.{ref}"},
	{"GET", "repos.{owner}.{repo}.git.tags.{sha}"},
	{"POST", "repos.{owner}.{repo}.git.tags"},
	{"GET", "repos.{owner}.{repo}.git.trees.{sha}"},
	{"POST", "repos.{owner}.{repo}.git.trees"},

	// Issues
	{"GET", "issues"},
	{"GET", "user.issues"},
	{"GET", "orgs.{org}.issues"},
	{"GET", "repos.{owner}.{repo}.issues"},
	{"GET", "repos.{owner}.{repo}.issues.{number}"},
	{"POST", "repos.{owner}.{repo}.issues"},
	{"GET", "repos.{owner}.{repo}.assignees"},
	{"GET", "repos.{owner}.{repo}.assignees.assignee"},
	{"GET", "repos.{owner}.{repo}.issues.{number}.comments"},
	{"POST", "repos.{owner}.{repo}.issues.{number}.comments"},
	{"GET", "repos.{owner}.{repo}.issues.{number}.events"},
	{"GET", "repos.{owner}.{repo}.labels"},
	{"GET", "repos.{owner}.{repo}.labels.{name}"},
	{"POST", "repos.{owner}.{repo}.labels"},
	{"DELETE", "repos.{owner}.{repo}.labels.{name}"},
	{"GET", "repos.{owner}.{repo}.issues.{number}.labels"},
	{"POST", "repos.{owner}.{repo}.issues.{number}.labels"},
	{"DELETE", "repos.{owner}.{repo}.issues.{number}.labels.{name}"},
	{"PUT", "repos.{owner}.{repo}.issues.{number}.labels"},
	{"DELETE", "repos.{owner}.{repo}.issues.{number}.labels"},
	{"GET", "repos.{owner}.{repo}.milestones.{number}.labels"},
	{"GET", "repos.{owner}.{repo}.milestones"},
	{"GET", "repos.{owner}.{repo}.milestones.{number}"},
	{"POST", "repos.{owner}.{repo}.milestones"},
	{"DELETE", "repos.{owner}.{repo}.milestones.{number}"},

	// Miscellaneous
	{"GET", "emojis"},
	{"GET", "gitignore.templates"},
	{"GET", "gitignore.templates.{name}"},
	{"POST", "markdown"},
	{"POST", "markdown.raw"},
	{"GET", "meta"},
	{"GET", "rate_limit"},

	// Organizations
	{"GET", "users.{user}.orgs"},
	{"GET", "user.orgs"},
	{"GET", "orgs.{org}"},
	{"GET", "orgs.{org}.members"},
	{"GET", "orgs.{org}.members.{user}"},
	{"DELETE", "orgs.{org}.members.{user}"},
	{"GET", "orgs.{org}.public_members"},
	{"GET", "orgs.{org}.public_members.{user}"},
	{"PUT", "orgs.{org}.public_members.{user}"},
	{"DELETE", "orgs.{org}.public_members.{user}"},
	{"GET", "orgs.{org}.teams"},
	{"GET", "teams.{id}"},
	{"POST", "orgs.{org}.teams"},
	{"DELETE", "teams.{id}"},
	{"GET", "teams.{id}.members"},
	{"GET", "teams.{id}.members.{user}"},
	{"PUT", "teams.{id}.members.{user}"},
	{"DELETE", "teams.{id}.members.{user}"},
	{"GET", "teams.{id}.repos"},
	{"GET", "teams.{id}.repos.{owner}.{repo}"},
	{"PUT", "teams.{id}.repos.{owner}.{repo}"},
	{"DELETE", "teams.{id}.repos.{owner}.{repo}"},
	{"GET", "user.teams"},

	// Pull Requests
	{"GET", "repos.{owner}.{repo}.pulls"},
	{"GET", "repos.{owner}.{repo}.pulls.{number}"},
	{"POST", "repos.{owner}.{repo}.pulls"},
	{"GET", "repos.{owner}.{repo}.pulls.{number}.commits"},
	{"GET", "repos.{owner}.{repo}.pulls.{number}.files"},
	{"GET", "repos.{owner}.{repo}.pulls.{number}.merge"},
	{"PUT", "repos.{owner}.{repo}.pulls.{number}.merge"},
	{"GET", "repos.{owner}.{repo}.pulls.{number}.comments"},
	{"PUT", "repos.{owner}.{repo}.pulls.{number}.comments"},

	// Repositories
	{"GET", "user.repos"},
	{"GET", "users.{user}.repos"},
	{"GET", "orgs.{org}.repos"},
	{"GET", "repositories"},
	{"POST", "user.repos"},
	{"POST", "orgs.{org}.repos"},
	{"GET", "repos.{owner}.{repo}"},
	{"GET", "repos.{owner}.{repo}.contributors"},
	{"GET", "repos.{owner}.{repo}.languages"},
	{"GET", "repos.{owner}.{repo}.teams"},
	{"GET", "repos.{owner}.{repo}.tags"},
	{"GET", "repos.{owner}.{repo}.branches"},
	{"GET", "repos.{owner}.{repo}.branches.{branch}"},
	{"DELETE", "repos.{owner}.{repo}"},
	{"GET", "repos.{owner}.{repo}.collaborators"},
	{"GET", "repos.{owner}.{repo}.collaborators.{user}"},
	{"PUT", "repos.{owner}.{repo}.collaborators.{user}"},
	{"DELETE", "repos.{owner}.{repo}.collaborators.{user}"},
	{"GET", "repos.{owner}.{repo}.comments"},
	{"GET", "repos.{owner}.{repo}.commits.{sha}.comments"},
	{"POST", "repos.{owner}.{repo}.commits.{sha}.comments"},
	{"GET", "repos.{owner}.{repo}.comments.{id}"},
	{"DELETE", "repos.{owner}.{repo}.comments.{id}"},
	{"GET", "repos.{owner}.{repo}.commits"},
	{"GET", "repos.{owner}.{repo}.commits.{sha}"},
	{"GET", "repos.{owner}.{repo}.readme"},
	{"GET", "repos.{owner}.{repo}.contents.{path}"},
	{"DELETE", "repos.{owner}.{repo}.contents.{path}"},
	{"GET", "repos.{owner}.{repo}.keys"},
	{"GET", "repos.{owner}.{repo}.keys.{id}"},
	{"POST", "repos.{owner}.{repo}.keys"},
	{"DELETE", "repos.{owner}.{repo}.keys.{id}"},
	{"GET", "repos.{owner}.{repo}.downloads"},
	{"GET", "repos.{owner}.{repo}.downloads.{id}"},
	{"DELETE", "repos.{owner}.{repo}.downloads.{id}"},
	{"GET", "repos.{owner}.{repo}.forks"},
	{"POST", "repos.{owner}.{repo}.forks"},
	{"GET", "repos.{owner}.{repo}.hooks"},
	{"GET", "repos.{owner}.{repo}.hooks.{id}"},
	{"POST", "repos.{owner}.{repo}.hooks"},
	{"POST", "repos.{owner}.{repo}.hooks.{id}.tests"},
	{"DELETE", "repos.{owner}.{repo}.hooks.{id}"},
	{"POST", "repos.{owner}.{repo}.merges"},
	{"GET", "repos.{owner}.{repo}.releases"},
	{"GET", "repos.{owner}.{repo}.releases.{id}"},
	{"POST", "repos.{owner}.{repo}.releases"},
	{"DELETE", "repos.{owner}.{repo}.releases.{id}"},
	{"GET", "repos.{owner}.{repo}.releases.{id}.assets"},
	{"GET", "repos.{owner}.{repo}.stats.contributors"},
	{"GET", "repos.{owner}.{repo}.stats.commit_activity"},
	{"GET", "repos.{owner}.{repo}.stats.code_frequency"},
	{"GET", "repos.{owner}.{repo}.stats.participation"},
	{"GET", "repos.{owner}.{repo}.stats.punch_card"},
	{"GET", "repos.{owner}.{repo}.statuses.{ref}"},
	{"POST", "repos.{owner}.{repo}.statuses.{ref}"},

	// Search
	{"GET", "search.repositories"},
	{"GET", "search.code"},
	{"GET", "search.issues"},
	{"GET", "search.users"},
	{"GET", "legacy.issues.search.{owner}.{repository}.{state}.{keyword}"},
	{"GET", "legacy.repos.search.{keyword}"},
	{"GET", "legacy.user.search.{keyword}"},
	{"GET", "legacy.user.email.{email}"},

	// Users
	{"GET", "users.{user}"},
	{"GET", "user"},
	{"GET", "users"},
	{"GET", "user.emails"},
	{"POST", "user.emails"},
	{"DELETE", "user.emails"},
	{"GET", "users.{user}.followers"},
	{"GET", "user.followers"},
	{"GET", "users.{user}.following"},
	{"GET", "user.following"},
	{"GET", "user.following.{user}"},
	{"GET", "users.{user}.following.{target_user}"},
	{"PUT", "user.following.{user}"},
	{"DELETE", "user.following.{user}"},
	{"GET", "users.{user}.keys"},
	{"GET", "user.keys"},
	{"GET", "user.keys.{id}"},
	{"POST", "user.keys"},
	{"DELETE", "user.keys.{id}"},
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

// BenchmarkInsert-16    	    9566	    117855 ns/op	  130380 B/op	    2338 allocs/op
// BenchmarkInsert-16    	   12214	     97886 ns/op	  162061 B/op	    2172 allocs/op
// BenchmarkInsert-16    	   16977	     69638 ns/op	   57129 B/op	    1574 allocs/op
// BenchmarkInsert-16    	   14163	     85088 ns/op	   81112 B/op	    1837 allocs/op
// TODO delete me
func BenchmarkInsert(b *testing.B) {
	r, _ := New()

	precomputed := make([]struct {
		method string
		route  *Route
	}, 0, len(staticRoutes))
	for _, route := range staticRoutes {
		rte, _ := r.NewRoute(route.path, emptyHandler)
		precomputed = append(precomputed, struct {
			method string
			route  *Route
		}{method: route.method, route: rte})
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = r.Updates(func(txn *Txn) error {
			for _, route := range precomputed {
				_ = txn.HandleRoute(route.method, route.route)
			}
			for _, route := range precomputed {
				_ = txn.HandleRoute(route.method, route.route)
			}
			return nil
		})
	}
}

// TODO delete me
func BenchmarkOverlappingCatchAll(b *testing.B) {
	r, _ := New(AllowRegexpParam(true))
	routes := []string{
		"/foo/{fallback}/bar",
		"/foo/{b:[a-z]+}",
		"/foo/{b:[a-z]+}/bar",
		"/foo/{c:[A-Z]+}/bar",
	}

	for _, route := range routes {
		require.NoError(b, onlyError(r.Handle(http.MethodGet, route, func(c Context) {
			// fmt.Println(c.Pattern())
		})))
	}

	req := httptest.NewRequest(http.MethodGet, "/foo/123/bar", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkOverlappingCatchAll_GorillaMux(b *testing.B) {
	r := mux.NewRouter()
	routes := []string{
		"/foo/{b:[a-z]+}",
		"/foo/{b:[a-z]+}/bar",
		"/foo/{c:[A-Z]+}/bar",
		"/foo/{fallback}/bar",
	}

	for _, route := range routes {
		r.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
		}).Methods(http.MethodGet)
	}

	req := httptest.NewRequest(http.MethodGet, "/foo/123/bar", nil)
	w := new(mockResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
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

func BenchmarkGithubParamsHostnameAll(b *testing.B) {
	r, _ := New()
	for _, route := range wildcardHostnames {
		require.NoError(b, onlyError(r.Handle(route.method, route.path+"/", emptyHandler)))
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

// BenchmarkInfixCatchAll-16    	 6818233	       175.0 ns/op	       0 B/op	       0 allocs/op
func BenchmarkInfixCatchAll(b *testing.B) {
	f, _ := New()
	f.MustHandle(http.MethodGet, "/*{a}/b/*{c}/d/*{e}/f/*{g}/j", func(c Context) {
	})

	req := httptest.NewRequest(http.MethodGet, "/x/y/z/b/x/y/z/d/x/y/z/f/x/y/z/j", nil)
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
	f, _ := New(WithHandleTrailingSlash(RelaxedSlash))
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

	t.Run("same case", func(t *testing.T) {
		for _, route := range staticHostnames {
			req, err := http.NewRequest(route.method, "/foo", nil)
			require.NoError(t, err)
			req.Host = route.path
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, route.path+"/foo", w.Body.String())
		}
	})

	t.Run("case-insensitive", func(t *testing.T) {
		for _, route := range staticHostnames {
			req, err := http.NewRequest(route.method, "/foo", nil)
			require.NoError(t, err)
			req.Host = strings.ToUpper(route.path)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, route.path+"/foo", w.Body.String())
		}
	})

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

func TestParamsHostnameRoute(t *testing.T) {
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

		host := strings.ToLower(netutil.StripHostPort(c.Host()))
		assert.Equal(t, host+c.Path(), c.Pattern())
		_ = c.String(200, host+c.Path())
	}
	for _, route := range wildcardHostnames {
		require.NoError(t, onlyError(r.Handle(route.method, route.path+"/foo", h)))
	}
	t.Run("same case", func(t *testing.T) {
		for _, route := range wildcardHostnames {
			req, err := http.NewRequest(route.method, "/foo", nil)
			require.NoError(t, err)
			req.Host = route.path
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, route.path+"/foo", w.Body.String())
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		for _, route := range wildcardHostnames {
			req, err := http.NewRequest(route.method, "/foo", nil)
			require.NoError(t, err)
			req.Host = strings.ToUpper(route.path)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, route.path+"/foo", w.Body.String())
		}
	})
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
		want, err := f.NewRoute("/foo", emptyHandler, WithAnnotation("foo", "bar"), WithHandleTrailingSlash(RedirectSlash))
		require.NoError(t, err)
		require.NoError(t, f.HandleRoute(http.MethodGet, want))
		got := f.Route(http.MethodGet, "/foo")
		assert.Equal(t, want, got)
		assert.Equal(t, RedirectSlash, got.TrailingSlashOption())

		want, err = f.NewRoute("/foo", emptyHandler, WithAnnotation("baz", "baz"))
		require.NoError(t, err)
		require.NoError(t, f.UpdateRoute(http.MethodGet, want))
		got = f.Route(http.MethodGet, "/foo")
		assert.Equal(t, want, got)
		assert.Equal(t, StrictSlash, got.TrailingSlashOption())
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

func TestWildcardSuffix(t *testing.T) {
	r, _ := New(AllowRegexpParam(true))

	routes := []struct {
		path string
		key  string
	}{
		{"/github.com/etf1/*{repo}", "/github.com/etf1/mux"},
		{"/github.com/johndoe/*{repo}", "/github.com/johndoe/buzz"},
		{"/foo/bar/*{args}", "/foo/bar/baz"},
		{"/filepath/path=*{path}", "/filepath/path=/file.txt"},
		{"/john/doe/*{any:[A-z/]+}", "/john/doe/a/b/c"},
		{"/filepath/key=*{any:[A-z/.]+}", "/filepath/key=/file.txt"},
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
			name:   "infix wildcard regexp",
			routes: []string{"/foo/*{args:$^}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with children",
			routes: []string{"/foo/*{args}/bar", "/foo/*{args}/caz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with children regexp",
			routes: []string{"/foo/*{args:$^}/bar", "/foo/*{args:$^}/caz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with static edge",
			routes: []string{"/foo/*{args}/bar", "/foo/baz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard with static edge regexp",
			routes: []string{"/foo/*{args:$^}/bar", "/foo/baz"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard and suffix wildcard",
			routes: []string{"/foo/*{args}/bar", "/foo/*{args}"},
			path:   "/foo/",
		},
		{
			name:   "infix wildcard and suffix wildcard regexp",
			routes: []string{"/foo/*{args:$^}/bar", "/foo/*{args:$^}"},
			path:   "/foo/",
		},
		{
			name:   "infix inflight wildcard",
			routes: []string{"/foo/abc*{args}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard regexp",
			routes: []string{"/foo/abc*{args:$^}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with children",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc*{args}/caz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with children regexp",
			routes: []string{"/foo/abc*{args:$^}/bar", "/foo/abc*{args:$^}/caz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with static edge",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc/baz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard with static edge regexp",
			routes: []string{"/foo/abc*{args:$^}/bar", "/foo/abc/baz"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard and suffix wildcard",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc*{args}"},
			path:   "/foo/abc",
		},
		{
			name:   "infix inflight wildcard and suffix wildcard regexp",
			routes: []string{"/foo/abc*{args:$^}/bar", "/foo/abc*{args:$^}"},
			path:   "/foo/abc",
		},
		{
			name:   "suffix wildcard wildcard with param edge",
			routes: []string{"/foo/*{args}", "/foo/{param}"},
			path:   "/foo/",
		},
		{
			name:   "suffix wildcard wildcard with param edge regexp",
			routes: []string{"/foo/*{args:$^}", "/foo/{param:$^}"},
			path:   "/foo/",
		},
		{
			name:   "suffix inflight wildcard wildcard with param edge",
			routes: []string{"/foo/abc*{args}", "/foo/abc{param}"},
			path:   "/foo/abc",
		},
		{
			name:   "suffix inflight wildcard wildcard with param edge regexp",
			routes: []string{"/foo/abc*{args:$^}", "/foo/abc{param:$^}"},
			path:   "/foo/abc",
		},
		{
			name:   "infix wildcard wildcard with param edge",
			routes: []string{"/foo/*{args}/bar", "/foo/{param}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix wildcard wildcard with param edge regexp",
			routes: []string{"/foo/*{args:$^}/bar", "/foo/{param:$^}/bar"},
			path:   "/foo/bar",
		},
		{
			name:   "infix inflight wildcard wildcard with param edge",
			routes: []string{"/foo/abc*{args}/bar", "/foo/abc{param}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix inflight wildcard wildcard with param edge regexp",
			routes: []string{"/foo/abc*{args:$^}/bar", "/foo/abc{param:$^}/bar"},
			path:   "/foo/abc/bar",
		},
		{
			name:   "infix wildcard wildcard with trailing slash",
			routes: []string{"/foo/*{args}/"},
			path:   "/foo//",
		},
		{
			name:   "infix wildcard wildcard with trailing slash regexp",
			routes: []string{"/foo/*{args:$^}/"},
			path:   "/foo//",
		},
		{
			name:   "infix inflight wildcard wildcard with trailing slash",
			routes: []string{"/foo/abc*{args}/"},
			path:   "/foo/abc/",
		},
		{
			name:   "infix inflight wildcard wildcard with trailing slash regexp",
			routes: []string{"/foo/abc*{args:$^}/"},
			path:   "/foo/abc/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(AllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			n, tsr := lookupByPath(tree.root[http.MethodGet], tc.path, c, false)
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

	tree := f.getTree()
	for _, rte := range routes {
		c := newTestContext(f)
		n, tsr := lookupByPath(tree.root[http.MethodGet], rte, c, false)
		require.NotNilf(t, n, "route: %s", rte)
		require.NotNilf(t, n.route, "route: %s", rte)
		assert.False(t, tsr)
		assert.Equal(t, rte, n.route.pattern)
	}
}

func TestRouteParamEmptySegment(t *testing.T) {
	f, _ := New(AllowRegexpParam(true))
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
			name:  "empty segment regexp",
			route: "/cmd/{tool:bar}/{sub}",
			path:  "/cmd//sub",
		},
		{
			name:  "empty inflight end of route",
			route: "/command/exec:{tool}",
			path:  "/command/exec:",
		},
		{
			name:  "empty inflight end of route regexp",
			route: "/command/exec:{tool:bar}",
			path:  "/command/exec:",
		},
		{
			name:  "empty inflight segment",
			route: "/command/exec:{tool}/id",
			path:  "/command/exec:/id",
		},
		{
			name:  "empty inflight segment regexp",
			route: "/command/exec:{tool:bar}/id",
			path:  "/command/exec:/id",
		},
	}

	for _, tc := range cases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, tc.route, emptyHandler)))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := f.getTree()
			c := newTestContext(f)
			n, tsr := lookupByPath(tree.root[http.MethodGet], tc.path, c, false)
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
			name: "basic test most specific with regexp param",
			path: "/products/new",
			routes: []string{
				"/products/{id:[0-9]+}",
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
			name: "basic test less specific with regexp priority",
			path: "/products/123",
			routes: []string{
				"/products/{name}",
				"/products/{id:[0-9]+}",
				"/products/new",
			},
			wantMatch:  "/products/{id:[0-9]+}",
			wantParams: Params{{Key: "id", Value: "123"}},
		},
		{
			name: "basic test less specific with regexp and less specific",
			path: "/products/abc",
			routes: []string{
				"/products/{name}",
				"/products/{id:[0-9]+}",
				"/products/new",
			},
			wantMatch:  "/products/{name}",
			wantParams: Params{{Key: "name", Value: "abc"}},
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
			name: "backtrack to {id} wildcard while deleting {a} with regexp constraint",
			path: "/base/val1/123/new/barr",
			routes: []string{
				"/{base}/val1/{id:[0-9]+}",
				"/{base}/val1/123/{a:[A-z]+}/{id:[0-9]+}",
				"/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
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
			name: "kme+backtrack to {id} wildcard while deleting {a} with regexp constraint",
			path: "/base/val1/123/new/ba",
			routes: []string{
				"/{base}/val1/{id:[0-9]+}",
				"/{base}/val1/123/{a:[A-z]+}/bar",
				"/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
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
			name: "ime+backtrack to {id} wildcard while deleting {a} with regex constraint",
			path: "/base/val1/123/new/bx",
			routes: []string{
				"/{base}/val1/{id:[0-9]+}",
				"/{base}/val1/123/{a:[A-z]+}/bar",
				"/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
				"/{base}/val2",
			},
			wantMatch: "/{base}/val1/{id:[0-9]+}/new/{name:[A-z]+}",
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
			name: "backtrack to catch while deleting {a}, {id} and {name} with regexp constraint",
			path: "/base/val1/123/new/bar/",
			routes: []string{
				"/{base}/val1/{id:[0-9]+}",
				"/{base}/val1/123/{a:[A-z]+}/bar",
				"/{base}/val1/{id:[0-9]+}/new/{name:ba}",
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
			name: "multi node most specific with multi name",
			path: "/foo/1/2/3/bar",
			routes: []string{
				"/foo/{aa}",
				"/foo/{bb}/{cc}",
				"/foo/{dd}/{ee}/{ff}",
				"/foo/{gg}/{hh}/{ii}/bar",
				"/foo/{jj}/{kk}/{ll}/{mm}",
			},
			wantMatch: "/foo/{gg}/{hh}/{ii}/bar",
			wantParams: Params{
				{
					Key:   "gg",
					Value: "1",
				},
				{
					Key:   "hh",
					Value: "2",
				},
				{
					Key:   "ii",
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
			name: "multi node less specific with multi name",
			path: "/foo/1/2/3/john",
			routes: []string{
				"/foo/{aa}",
				"/foo/{bb}/{cc}",
				"/foo/{dd}/{ee}/{ff}",
				"/foo/{gg}/{hh}/{ii}/bar",
				"/foo/{jj}/{kk}/{ll}/{mm}",
			},
			wantMatch: "/foo/{jj}/{kk}/{ll}/{mm}",
			wantParams: Params{
				{
					Key:   "jj",
					Value: "1",
				},
				{
					Key:   "kk",
					Value: "2",
				},
				{
					Key:   "ll",
					Value: "3",
				},
				{
					Key:   "mm",
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
		{
			name: "regexp param priority in register order",
			path: "/foo/123",
			routes: []string{
				"/foo/{fallback}",
				"/foo/{a:[0-9]+}",
				"/foo/{b:[0-9-A-z]+}",
				"/foo/{c:[0-9-A-Z]+}",
			},
			wantMatch: "/foo/{a:[0-9]+}",
			wantParams: Params{
				{
					Key:   "a",
					Value: "123",
				},
			},
		},
		{
			name: "regexp param priority in register order, with last match",
			path: "/foo/abc",
			routes: []string{
				"/foo/{fallback}",
				"/foo/{a:[0-9]+}",
				"/foo/{b:[0-9-A-Z]+}",
				"/foo/{c:[0-9-A-z]+}",
			},
			wantMatch: "/foo/{c:[0-9-A-z]+}",
			wantParams: Params{
				{
					Key:   "c",
					Value: "abc",
				},
			},
		},
		{
			name: "regexp param priority with fallback",
			path: "/foo/*",
			routes: []string{
				"/foo/{fallback}",
				"/foo/{a:[0-9]+}",
				"/foo/{b:[0-9-A-Z]+}",
				"/foo/{c:[0-9-A-z]+}",
			},
			wantMatch: "/foo/{fallback}",
			wantParams: Params{
				{
					Key:   "fallback",
					Value: "*",
				},
			},
		},
		{
			name: "regexp wildcard priority in register order",
			path: "/foo/123",
			routes: []string{
				"/foo/*{fallback}",
				"/foo/*{a:[0-9]+}",
				"/foo/*{b:[0-9-A-z]+}",
				"/foo/*{c:[0-9-A-Z]+}",
			},
			wantMatch: "/foo/*{a:[0-9]+}",
			wantParams: Params{
				{
					Key:   "a",
					Value: "123",
				},
			},
		},
		{
			name: "regexp wildcard priority in register order, with last match",
			path: "/foo/abc",
			routes: []string{
				"/foo/*{fallback}",
				"/foo/*{a:[0-9]+}",
				"/foo/*{b:[0-9-A-Z]+}",
				"/foo/*{c:[0-9-A-z]+}",
			},
			wantMatch: "/foo/*{c:[0-9-A-z]+}",
			wantParams: Params{
				{
					Key:   "c",
					Value: "abc",
				},
			},
		},
		{
			name: "regexp wildcard priority with fallback",
			path: "/foo/*",
			routes: []string{
				"/foo/*{fallback}",
				"/foo/*{a:[0-9]+}",
				"/foo/*{b:[0-9-A-Z]+}",
				"/foo/*{c:[0-9-A-z]+}",
			},
			wantMatch: "/foo/*{fallback}",
			wantParams: Params{
				{
					Key:   "fallback",
					Value: "*",
				},
			},
		},
		{
			name: "regexp infix wildcard priority with fallback",
			path: "/foo/a/b/c/bar",
			routes: []string{
				"/foo/*{fallback}/bar",
				"/foo/*{a:[0-9]+}/bar",
				"/foo/*{b:[0-9-A-Z]+}/bar",
				"/foo/*{c:[0-9-A-z]+}/bar",
			},
			wantMatch: "/foo/*{fallback}/bar",
			wantParams: Params{
				{
					Key:   "fallback",
					Value: "a/b/c",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(AllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}

			tree := f.getTree()

			c := newTestContext(f)
			n, tsr := lookupByPath(tree.root[http.MethodGet], tc.path, c, false)
			require.NotNil(t, n)
			require.NotNil(t, n.route)
			assert.False(t, tsr)
			assert.Equal(t, tc.wantMatch, n.route.pattern)
			c.route = n.route
			if len(tc.wantParams) == 0 {
				assert.Empty(t, slices.Collect(c.Params()))
			} else {
				var params Params = slices.Collect(c.Params())
				assert.Equal(t, tc.wantParams, params)
			}

			// Test with lazy
			c = newTestContext(f)
			n, tsr = lookupByPath(tree.root[http.MethodGet], tc.path, c, true)
			require.NotNil(t, n)
			require.NotNil(t, n.route)
			assert.False(t, tsr)
			c.route = n.route
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
			name:     "simple infix wildcard with regexp",
			routes:   []string{"/foo/*{args:a}/bar"},
			path:     "/foo/a/bar",
			wantPath: "/foo/*{args:a}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix wildcard capture slash",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "simple infix wildcard capture slash with regexp",
			routes:   []string{"/foo/*{args:/}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/*{args:/}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "simple infix wildcard capture anything not empty",
			routes:   []string{"/foo/*{args}/bar"},
			path:     "/foo//a//bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/a/",
				},
			},
		},
		{
			name:     "simple infix wildcard capture anything not empty with regexp",
			routes:   []string{"/foo/*{args:/a/}/bar"},
			path:     "/foo//a//bar",
			wantPath: "/foo/*{args:/a/}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/a/",
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
			name:     "static with infix wildcard regexp child",
			routes:   []string{"/foo/", "/foo/*{args:[A-z]+}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/*{args:[A-z]+}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "bar",
				},
			},
		},
		{
			name:     "static with 2 infix wildcard and regexp child",
			routes:   []string{"/foo/", "/foo/*{args}/baz", "/foo/*{args:[A-z]+}/baz"},
			path:     "/foo/bar/baz",
			wantPath: "/foo/*{args:[A-z]+}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "bar",
				},
			},
		},
		{
			name:     "static with infix wildcard child capture slash",
			routes:   []string{"/foo/", "/foo/*{args}/baz"},
			path:     "/foo///baz",
			wantPath: "/foo/*{args}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "static with infix wildcard regexp child capture slash after fallback",
			routes:   []string{"/foo/", "/foo/*{args}/baz", "/foo/*{args:a}/baz"},
			path:     "/foo///baz",
			wantPath: "/foo/*{args}/baz",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
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
			name:     "simple infix wildcard regexp with route char",
			routes:   []string{"/foo/*{args:.*}/bar"},
			path:     "/foo/*{args:.*}/bar",
			wantPath: "/foo/*{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args:.*}",
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
			name:     "simple infix wildcard regexp with multi segment and route char",
			routes:   []string{"/foo/*{args:.*}/bar"},
			path:     "/foo/*{args:.*}/b/c/bar",
			wantPath: "/foo/*{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args:.*}/b/c",
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
			name:     "simple infix regexp inflight wildcard",
			routes:   []string{"/foo/z*{args:a}/bar"},
			path:     "/foo/za/bar",
			wantPath: "/foo/z*{args:a}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard capture slash",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/z//bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "simple infix regexp inflight wildcard capture slash",
			routes:   []string{"/foo/z*{args:/}/bar"},
			path:     "/foo/z//bar",
			wantPath: "/foo/z*{args:/}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
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
			name:     "simple infix regexp inflight wildcard with route char",
			routes:   []string{"/foo/z*{args:.*}/bar"},
			path:     "/foo/z*{args:.*}/bar",
			wantPath: "/foo/z*{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "*{args:.*}",
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
			name:     "simple infix inflight wildcard regexp with multi segment",
			routes:   []string{"/foo/z*{args:[A-z/]+}/bar"},
			path:     "/foo/za/b/c/bar",
			wantPath: "/foo/z*{args:[A-z/]+}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard regexp with multi segment",
			routes:   []string{"/foo/z*{args:a/b/c}/bar"},
			path:     "/foo/za/b/c/bar",
			wantPath: "/foo/z*{args:a/b/c}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard with multi slash",
			routes:   []string{"/foo/z*{args}/bar"},
			path:     "/foo/z////bar",
			wantPath: "/foo/z*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "///",
				},
			},
		},
		{
			name:     "simple infix inflight wildcard regexp with multi slash",
			routes:   []string{"/foo/z*{args:///}/bar"},
			path:     "/foo/z////bar",
			wantPath: "/foo/z*{args:///}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "///",
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
			name:     "simple infix inflight wildcard regexp with multi segment and route char",
			routes:   []string{"/foo/z*{args:.*}/bar"},
			path:     "/foo/z*{args}/b/c/bar",
			wantPath: "/foo/z*{args:.*}/bar",
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
			name:     "overlapping infix and suffix regexp wildcard match infix",
			routes:   []string{"/foo/*{args:.*}", "/foo/*{args:.*}/bar"},
			path:     "/foo/a/b/c/bar",
			wantPath: "/foo/*{args:.*}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
		},
		{
			name:     "overlapping infix and suffix wildcard match infix with slash",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
				},
			},
		},
		{
			name:     "overlapping infix and suffix regexp wildcard match infix with slash",
			routes:   []string{"/foo/*{args:/}", "/foo/*{args:/}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/*{args:/}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
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
			name:     "overlapping infix and suffix regexp wildcard match suffix",
			routes:   []string{"/foo/*{args:.*}", "/foo/*{args:.*}/bar"},
			path:     "/foo/a/b/c/baz",
			wantPath: "/foo/*{args:.*}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c/baz",
				},
			},
		},
		{
			name:     "overlapping infix and suffix wildcard match suffix with empty slash",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar"},
			path:     "/foo///baz",
			wantPath: "/foo/*{args}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "//baz",
				},
			},
		},
		{
			name:     "overlapping infix and suffix regexp wildcard match suffix with empty slash",
			routes:   []string{"/foo/*{args:.*}", "/foo/*{args:.*}/bar"},
			path:     "/foo///baz",
			wantPath: "/foo/*{args:.*}",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "//baz",
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
			name:     "overlapping infix suffix wildcard and param match infix with empty slash",
			routes:   []string{"/foo/*{args}", "/foo/*{args}/bar", "/foo/{ps}/bar"},
			path:     "/foo///bar",
			wantPath: "/foo/*{args}/bar",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "args",
					Value: "/",
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
			name: "infix regexp wildcard with trailing slash most specific",
			routes: []string{
				"/foo/*{any:.*}",
				"/foo/*{any:.*}/",
			},
			path:     "/foo/x/y/z/",
			wantPath: "/foo/*{any:.*}/",
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
		{
			name: "infix regexp wildcard with trailing slash most specific",
			routes: []string{
				"/foo/*{any:.*}",
				"/foo/*{any:.*}/",
			},
			path:     "/foo/x/y/z",
			wantPath: "/foo/*{any:.*}",
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
			f, _ := New(AllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}
			tree := f.getTree()
			c := newTestContext(f)
			n, tsr := lookupByPath(tree.root[http.MethodGet], tc.path, c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.route.pattern)
			assert.Equal(t, tc.wantTsr, tsr)
			c.route = n.route
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
			name: "wildcard hostname with complex overlapping route with static priority (case-insensitive)",
			routes: []string{
				"exemple.com/foo/bar/baz/{$1}/jo",
				"{any}.com/foo/*{any}/baz/{$1}/jo",
				"exemple.{tld}/foo/{ps}/baz/{$1}/jo",
			},
			host:     "EXEMPLE.COM",
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
			name: "static priority in hostname (case-insensitive)",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
			},
			host:     "FOO.B.C",
			path:     "/foo",
			wantPath: "{a}.b.c/foo",
			wantTsr:  false,
			wantParams: Params{
				{
					Key:   "a",
					Value: "FOO",
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
			name: "fallback to path only (case-insenitive)",
			routes: []string{
				"{a}.{b}.{c}/foo",
				"{a}.{b}.c/foo",
				"{a}.b.c/foo",
				"/foo/bar",
			},
			host:       "FOO.BAR.BAZ",
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
		{
			name: "fallback to tsr with hostname priority (case-insensitive)",
			routes: []string{
				"{a}.{b}.{c}/{d}",
				"{a}.{b}.c/{d}",
				"{a}.b.c/{path}/bar/",
				"/{a}/bar",
			},
			host:     "FOO.B.C",
			path:     "/john/bar",
			wantPath: "{a}.b.c/{path}/bar/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "a",
					Value: "FOO",
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
			tree := f.getTree()
			c := newTestContext(f)
			n, tsr := tree.lookup(http.MethodGet, tc.host, tc.path, c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.route.pattern)
			assert.Equal(t, tc.wantTsr, tsr)
			c.route = n.route
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
			name: "infix wildcard with trailing slash and tsr add and empty slash",
			routes: []string{
				"/foo/*{any}/",
			},
			path:     "/foo//a",
			wantPath: "/foo/*{any}/",
			wantTsr:  true,
			wantParams: Params{
				{
					Key:   "any",
					Value: "/a",
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

			tree := f.getTree()

			c := newTestContext(f)
			n, tsr := lookupByPath(tree.root[http.MethodGet], tc.path, c, false)
			require.NotNil(t, n)
			assert.Equal(t, tc.wantPath, n.route.pattern)
			assert.Equal(t, tc.wantTsr, tsr)
			c.tsr = tsr
			c.route = n.route
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

			tree := f.getTree()
			assert.Len(t, tree.root, 0)

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

			tree = f.getTree()
			assert.Len(t, tree.root, 0)
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
			name: "test delete with merge and child param",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/{foo}/{bar}"},
				{path: "a.b.c.d/{foo}/{bar}"},
				{path: "a.b.c{d}/{foo}/{bar}"},
				{path: "a.b/"},
			},
		},
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

			tree := f.getTree()
			assert.Len(t, tree.root, 0)

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

			tree = f.getTree()
			assert.Len(t, tree.root, 0)
		})
	}
}

func TestInsertConflict(t *testing.T) {
	cases := []struct {
		name      string
		routes    []string
		insert    string
		wantErr   error
		wantMatch string
	}{
		{
			name:      "static route already exist",
			routes:    []string{"/foo/bar", "/foo/baz"},
			insert:    "/foo/bar",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/foo/bar",
		},
		{
			name:      "route with same parameters",
			routes:    []string{"/foo/{foo}"},
			insert:    "/foo/{foo}",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/foo/{foo}",
		},
		{
			name:      "route with same wildcard",
			routes:    []string{"/foo/*{foo}"},
			insert:    "/foo/*{foo}",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/foo/*{foo}",
		},
		{
			name:      "route with same parameters but different name",
			routes:    []string{"/foo/{foo}"},
			insert:    "/foo/{bar}",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/foo/{foo}",
		},
		{
			name:      "route with same wildcard but different name",
			routes:    []string{"/foo/*{foo}"},
			insert:    "/foo/*{bar}",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/foo/*{foo}",
		},
		{
			name:      "route with middle same parameters but different name",
			routes:    []string{"/{foo}/bar"},
			insert:    "/{other}/bar",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/{foo}/bar",
		},
		{
			name:      "route with middle same wildcard but different name",
			routes:    []string{"/*{foo}/bar"},
			insert:    "/*{other}/bar",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/*{foo}/bar",
		},
		{
			name:      "route with same regexp parameter",
			routes:    []string{"/foo/{foo:[A-z]+}"},
			insert:    "/foo/{foo:[A-z]+}",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/foo/{foo:[A-z]+}",
		},
		{
			name:      "route with same regexp parameter but different name",
			routes:    []string{"/foo/{foo:[A-z]+}"},
			insert:    "/foo/{bar:[A-z]+}",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/foo/{foo:[A-z]+}",
		},
		{
			name:      "route with same regexp wildcard",
			routes:    []string{"/foo/*{foo:[A-z]+}"},
			insert:    "/foo/*{foo:[A-z]+}",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/foo/*{foo:[A-z]+}",
		},
		{
			name:      "route with same regexp wildcard but different name",
			routes:    []string{"/foo/*{foo:[A-z]+}"},
			insert:    "/foo/*{bar:[A-z]+}",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/foo/*{foo:[A-z]+}",
		},
		{
			name:      "route with middle same regexp parameter but different name",
			routes:    []string{"/{foo:[A-z]+}/bar"},
			insert:    "/{other:[A-z]+}/bar",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/{foo:[A-z]+}/bar",
		},
		{
			name:      "route with middle same regexp wildcard but different name",
			routes:    []string{"/*{foo:[A-z]+}/bar"},
			insert:    "/*{other:[A-z]+}/bar",
			wantErr:   ErrRouteNotFound,
			wantMatch: "/*{foo:[A-z]+}/bar",
		},
		{
			name:      "simple hostname conflict",
			routes:    []string{"a.{b}.c/fox", "{a}.b.c/fox"},
			insert:    "a.{d}.c/fox",
			wantErr:   ErrRouteNotFound,
			wantMatch: "a.{b}.c/fox",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(AllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
			}
			got := onlyError(f.Handle(http.MethodGet, tc.insert, emptyHandler))
			assert.ErrorIs(t, got, ErrRouteExist)
			var conflict *RouteConflict
			require.ErrorAs(t, got, &conflict)
			assert.Equal(t, tc.wantMatch, conflict.Existing.pattern)
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
			name:    "wildcard have different name",
			routes:  []string{"/foo/bar", "/foo/*{args}"},
			update:  "/foo/*{all}",
			wantErr: ErrRouteNotFound,
		},
		{
			name:    "replacing non wildcard by wildcard",
			routes:  []string{"/foo/bar", "/foo/"},
			update:  "/foo/*{all}",
			wantErr: ErrRouteNotFound,
		},
		{
			name:    "replacing wildcard by non wildcard",
			routes:  []string{"/foo/bar", "/foo/*{args}"},
			update:  "/foo/",
			wantErr: ErrRouteNotFound,
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
	assert.ErrorIs(t, onlyError(f.Handle("G\x00ET", "/foo", emptyHandler)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Handle("", "/foo", emptyHandler)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Handle(http.MethodGet, "/foo", nil)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Handle(http.MethodGet, "/foo\x00", emptyHandler)), ErrInvalidRoute)

	// Invalid route on update
	assert.ErrorIs(t, onlyError(f.Update("", "/foo", emptyHandler)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Update(http.MethodGet, "/foo", nil)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Update(http.MethodGet, "/foo\x00", emptyHandler)), ErrInvalidRoute)
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
	f, _ := New(AllowRegexpParam(true))

	staticToken := func(v string, hsplit bool) token {
		return token{
			value:  v,
			typ:    nodeStatic,
			hsplit: hsplit,
		}
	}

	paramToken := func(v, reg string) token {
		tk := token{
			value: v,
			typ:   nodeParam,
		}
		if reg != "" {
			tk.regexp = regexp.MustCompile("^" + reg + "$")
		}
		return tk
	}

	wildcardToken := func(v, reg string) token {
		tk := token{
			value: v,
			typ:   nodeWildcard,
		}
		if reg != "" {
			tk.regexp = regexp.MustCompile("^" + reg + "$")
		}
		return tk
	}

	cases := []struct {
		wantErr    error
		name       string
		path       string
		wantN      int
		wantTokens []token
	}{
		{
			name:       "valid static route",
			path:       "/foo/bar",
			wantTokens: slices.Collect(iterutil.SeqOf(staticToken("/foo/bar", false))),
		},
		{
			name:  "top level domain",
			path:  "{tld}/foo/bar",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				paramToken("tld", ""),
				staticToken("/foo/bar", false),
			)),
		},
		{
			name:  "valid catch all route",
			path:  "/foo/bar/*{arg}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/", false),
				wildcardToken("arg", ""),
			)),
		},
		{
			name:  "valid param route",
			path:  "/foo/bar/{baz}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/", false),
				paramToken("baz", ""),
			)),
		},
		{
			name:  "valid multi params route",
			path:  "/foo/{bar}/{baz}",
			wantN: 2,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", ""),
				staticToken("/", false),
				paramToken("baz", ""),
			)),
		},
		{
			name:  "valid same params route",
			path:  "/foo/{bar}/{bar}",
			wantN: 2,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", ""),
				staticToken("/", false),
				paramToken("bar", ""),
			)),
		},
		{
			name:  "valid multi params and catch all route",
			path:  "/foo/{bar}/{baz}/*{arg}",
			wantN: 3,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", ""),
				staticToken("/", false),
				paramToken("baz", ""),
				staticToken("/", false),
				wildcardToken("arg", ""),
			)),
		},
		{
			name:  "valid inflight param",
			path:  "/foo/xyz:{bar}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/xyz:", false),
				paramToken("bar", ""),
			)),
		},
		{
			name:  "valid inflight catchall",
			path:  "/foo/xyz:*{bar}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/xyz:", false),
				wildcardToken("bar", ""),
			)),
		},
		{
			name:  "valid multi inflight param and catch all",
			path:  "/foo/xyz:{bar}/abc:{bar}/*{arg}",
			wantN: 3,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/xyz:", false),
				paramToken("bar", ""),
				staticToken("/abc:", false),
				paramToken("bar", ""),
				staticToken("/", false),
				wildcardToken("arg", ""),
			)),
		},
		{
			name:  "catch all with arg in the middle of the route",
			path:  "/foo/bar/*{bar}/baz",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/baz", false),
			)),
		},
		{
			name:  "multiple catch all suffix and inflight with arg in the middle of the route",
			path:  "/foo/bar/*{bar}/x*{args}/y/*{z}/{b}",
			wantN: 4,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/x", false),
				wildcardToken("args", ""),
				staticToken("/y/", false),
				wildcardToken("z", ""),
				staticToken("/", false),
				paramToken("b", ""),
			)),
		},
		{
			name:  "inflight catch all with arg in the middle of the route",
			path:  "/foo/bar/damn*{bar}/baz",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/damn", false),
				wildcardToken("bar", ""),
				staticToken("/baz", false),
			)),
		},
		{
			name:  "catch all with arg in the middle of the route and param after",
			path:  "/foo/bar/*{bar}/{baz}",
			wantN: 2,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/", false),
				paramToken("baz", ""),
			)),
		},
		{
			name:  "simple domain and path",
			path:  "foo/bar",
			wantN: 0,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("foo", true),
				staticToken("/bar", false),
			)),
		},
		{
			name:  "simple domain with trailing slash",
			path:  "foo/",
			wantN: 0,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("foo", true),
				staticToken("/", false),
			)),
		},
		{
			name:  "period in param path allowed",
			path:  "foo/{.bar}",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("foo", true),
				staticToken("/", false),
				paramToken(".bar", ""),
			)),
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
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("a.b.com", true),
				staticToken("/", false),
				wildcardToken("any", ""),
			)),
		},
		{
			name:    "illegal control character in path",
			path:    "example.com/foo\x00",
			wantErr: ErrInvalidRoute,
			wantN:   0,
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
			path:    "uj01dowf1x5lk6lysurbr0lgbdd1wfyw8sm8q17mnt0i9igk774vcwr5rly5dguu.b.com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "middle hostname label exceed 63 characters",
			path:    "a.uj01dowf1x5lk6lysurbr0lgbdd1wfyw8sm8q17mnt0i9igk774vcwr5rly5dguu.com/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "trailing hostname label exceed 63 characters",
			path:    "a.b.uj01dowf1x5lk6lysurbr0lgbdd1wfyw8sm8q17mnt0i9igk774vcwr5rly5dguu/",
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
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("123.", true),
				paramToken("a", ""),
				staticToken(".456", true),
				staticToken("/", false),
			)),
		},
		{
			name:    "all-numeric label with path wildcard",
			path:    "123.456/{abc}",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:    "hostname exceed 255 character",
			path:    "a.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjx.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr/",
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
			name:    "invalid uppercase label",
			path:    "ABC/",
			wantErr: ErrInvalidRoute,
			wantN:   0,
		},
		{
			name:  "2 regular params in domain",
			path:  "{a}.{b}.com/",
			wantN: 2,
			wantTokens: slices.Collect(iterutil.SeqOf(
				paramToken("a", ""),
				staticToken(".", true),
				paramToken("b", ""),
				staticToken(".com", true),
				staticToken("/", false),
			)),
		},
		{
			name:  "255 character with .",
			path:  "78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr/",
			wantN: 0,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr", true),
				staticToken("/", false),
			)),
		},
		{
			name:  "param does not count at character",
			path:  "{a}.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjx.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr/",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				paramToken("a", ""),
				staticToken(".78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjx.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr.78fayzyiqkt3hh2mquv9szfroeexx8qztscu3oudoyfarjl6jmdyxk2cefvzjxr", true),
				staticToken("/", false),
			)),
		},
		{
			name:  "hostname variant with multiple catch all suffix and inflight with arg in the middle of the route",
			path:  "example.com/foo/bar/*{bar}/x*{args}/y/*{z}/{b}",
			wantN: 4,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("example.com", true),
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/x", false),
				wildcardToken("args", ""),
				staticToken("/y/", false),
				wildcardToken("z", ""),
				staticToken("/", false),
				paramToken("b", ""),
			)),
		},
		{
			name:  "hostname variant with inflight catch all with arg in the middle of the route",
			path:  "example.com/foo/bar/damn*{bar}/baz",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("example.com", true),
				staticToken("/foo/bar/damn", false),
				wildcardToken("bar", ""),
				staticToken("/baz", false),
			)),
		},
		{
			name:  "hostname variant catch all with arg in the middle of the route and param after",
			path:  "example.com/foo/bar/*{bar}/{baz}",
			wantN: 2,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("example.com", true),
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/", false),
				paramToken("baz", ""),
			)),
		},
		{
			name:  "complex domain and path",
			path:  "{ab}.{c}.de{f}.com/foo/bar/*{bar}/x*{args}/y/*{z}/{b}",
			wantN: 7,
			wantTokens: slices.Collect(iterutil.SeqOf(
				paramToken("ab", ""),
				staticToken(".", true),
				paramToken("c", ""),
				staticToken(".de", true),
				paramToken("f", ""),
				staticToken(".com", true),
				staticToken("/foo/bar/", false),
				wildcardToken("bar", ""),
				staticToken("/x", false),
				wildcardToken("args", ""),
				staticToken("/y/", false),
				wildcardToken("z", ""),
				staticToken("/", false),
				paramToken("b", ""),
			)),
		},
		// Reject path with traversal pattern
		{
			name:    "path with double slash",
			path:    "/foo//bar",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "path with > double slash",
			path:    "/foo///bar",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "path with slash dot slash",
			path:    "/foo/./bar",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "path with slash dot slash",
			path:    "/foo/././bar",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "path with double dot parent reference",
			path:    "/foo/../bar",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "path with double dot parent reference",
			path:    "/foo/../../bar",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "path ending with slash dot",
			path:    "/foo/.",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "path ending with slash double dot",
			path:    "/foo/..",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "path ending with slash dot",
			path:    "/.",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "path ending with slash double dot",
			path:    "/..",
			wantErr: ErrInvalidRoute,
		},
		// Allowed dot and slash combinaison
		{
			name: "last path segment starting with slash dot and text",
			path: "/foo/.bar",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/.bar", false),
			)),
		},
		{
			name: "last path segment starting with slash dot and text",
			path: "/foo/..bar",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/..bar", false),
			)),
		},
		{
			name: "path segment starting with slash dot and text",
			path: "/foo/.bar/baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/.bar/baz", false),
			)),
		},
		{
			name:  "path segment starting with slash dot and param",
			path:  "/foo/.{foo}/baz",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/.", false),
				paramToken("foo", ""),
				staticToken("/baz", false),
			)),
		},
		{
			name: "path segment starting with slash dot and text",
			path: "/foo/..bar/baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/..bar/baz", false),
			)),
		},
		{
			name:  "path segment starting with slash dot and param",
			path:  "/foo/..{foo}/baz",
			wantN: 1,
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/..", false),
				paramToken("foo", ""),
				staticToken("/baz", false),
			)),
		},
		{
			name: "path segment ending with dot slash",
			path: "/foo/bar./baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar./baz", false),
			)),
		},
		{
			name: "path segment ending with double dot slash",
			path: "/foo/bar../baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar../baz", false),
			)),
		},
		{
			name: "path segment with > double dot",
			path: "/foo/.../baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/.../baz", false),
			)),
		},
		{
			name: "path segment ending with slash and > double dot",
			path: "/foo/...",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/...", false),
			)),
		},
		{
			name: "last path segment ending with dot",
			path: "/foo/bar.",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar.", false),
			)),
		},
		{
			name: "last path segment ending with double dot",
			path: "/foo/bar..",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/bar..", false),
			)),
		},
		{
			name: "path segment with dot",
			path: "/foo/a.b.c",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/a.b.c", false),
			)),
		},
		{
			name: "path segment with double dot",
			path: "/foo/a..b..c",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/a..b..c", false),
			)),
		},
		// Regexp
		{
			name: "simple ending param with regexp",
			path: "/foo/{bar:[A-z]+}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", "[A-z]+"),
			)),
			wantN: 1,
		},
		{
			name: "simple ending param with regexp",
			path: "/foo/*{bar:[A-z]+}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				wildcardToken("bar", "[A-z]+"),
			)),
			wantN: 1,
		},
		{
			name: "simple infix param with regexp",
			path: "/foo/{bar:[A-z]+}/baz",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", "[A-z]+"),
				staticToken("/baz", false),
			)),
			wantN: 1,
		},
		{
			name: "multi infix and ending param with regexp",
			path: "/foo/{bar:[A-z]+}/{baz:[0-9]+}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", "[A-z]+"),
				staticToken("/", false),
				paramToken("baz", "[0-9]+"),
			)),
			wantN: 2,
		},
		{
			name: "multi infix and ending wildcard with regexp",
			path: "/foo/*{bar:[A-z]+}/a*{baz:[0-9]+}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				wildcardToken("bar", "[A-z]+"),
				staticToken("/a", false),
				wildcardToken("baz", "[0-9]+"),
			)),
			wantN: 2,
		},
		{
			name: "consecutive infix regexp wildcard and regexp param allowed",
			path: "/foo/*{bar:[A-z]+}/{baz:[0-9]+}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				wildcardToken("bar", "[A-z]+"),
				staticToken("/", false),
				paramToken("baz", "[0-9]+"),
			)),
			wantN: 2,
		},
		{
			name: "hostname starting with regexp",
			path: "{a:[A-z]+}.b.c/foo",
			wantTokens: slices.Collect(iterutil.SeqOf(
				paramToken("a", "[A-z]+"),
				staticToken(".b.c", true),
				staticToken("/foo", false),
			)),
			wantN: 1,
		},
		{
			name: "hostname with middle param regexp",
			path: "a.{b:[A-z]+}.c/foo",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("a.", true),
				paramToken("b", "[A-z]+"),
				staticToken(".c", true),
				staticToken("/foo", false),
			)),
			wantN: 1,
		},
		{
			name: "hostname ending with param regexp",
			path: "a.b.{c:[A-z]+}/foo",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("a.b.", true),
				paramToken("c", "[A-z]+"),
				staticToken("/foo", false),
			)),
			wantN: 1,
		},
		{
			name: "non capturing group allowed in regexp",
			path: "/foo/{bar:(?:foo|bar)}",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/foo/", false),
				paramToken("bar", "(?:foo|bar)"),
			)),
			wantN: 1,
		},
		{
			name: "regexp wildcard at the beginning of the path",
			path: "/*{foo:[A-z]+}/bar",
			wantTokens: slices.Collect(iterutil.SeqOf(
				staticToken("/", false),
				wildcardToken("foo", "[A-z]+"),
				staticToken("/bar", false),
			)),
			wantN: 1,
		},
		{
			name:    "consecutive infix wildcard at start with regexp not allowed",
			path:    "/*{foo:[A-z]+}/*{baz:[0-9]+}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "consecutive infix wildcard at start with and without regexp not allowed",
			path:    "/*{foo:[A-z]+}/*{baz}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "consecutive infix wildcard at start with regexp not allowed",
			path:    "/*{foo}/*{baz:[0-9]+}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "consecutive infix wildcard with regexp not allowed",
			path:    "/foo/*{bar:[A-z]+}/*{baz:[0-9]+}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "consecutive infix wildcard with first regexp not allowed",
			path:    "/foo/*{bar:[A-z]+}/*{baz}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "consecutive infix wildcard with second regexp not allowed",
			path:    "/foo/*{bar}/*{baz:[A-z]+}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "non slash char after regexp param not allowed",
			path:    "/foo/{bar:[A-z]+}a/",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "non slash char after regexp wildcard not allowed",
			path:    "/foo/*{bar:[A-z]+}a/",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "regexp wildcard not allowed in hostname",
			path:    "*{a:[A-z]+}.b.c/",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "regexp wildcard not allowed in hostname",
			path:    "*{a.{b:[A-z]+}}.c/",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "regexp wildcard not allowed in hostname",
			path:    "*{a.b.{c:[A-z]+}/",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "missing param name with regexp",
			path:    "/foo/{:[A-z]+}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "missing wildcard name with regexp",
			path:    "/foo/*{:[A-z]+}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "missing regular expression",
			path:    "/foo/{a:}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "missing regular expression with only ':'",
			path:    "/foo/{:}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "unbalanced braces in param regexp",
			path:    "/foo/{bar:[A-z]+",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "unbalanced braces in wildcard regexp",
			path:    "/foo/*{bar:[A-z]+",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "balanced braces in param regexp with invalid char after",
			path:    "/foo/{bar:{}}a",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "balanced braces in wildcard regexp with invalid brace after",
			path:    "/foo/{bar:{}}}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "unbalanced braces in regexp complex",
			path:    "/foo/{bar:{{{{}}}}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "invalid regular expression",
			path:    "/foo/{bar:a{5,2}}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "invalid regular expression",
			path:    "/foo/{bar:\\k}",
			wantErr: ErrInvalidRoute,
		},
		{
			name:    "capture group in regexp are not allowed",
			path:    "/foo/{bar:(foo|bar)}",
			wantErr: ErrInvalidRoute,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens, n, hostSplit, err := f.parseRoute(tc.path)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantN, n)
			assert.Equal(t, tc.wantTokens, tokens)
			if err == nil {
				assert.Equal(t, strings.IndexByte(tc.path, '/'), hostSplit)
			}
		})
	}
}

func TestParseRouteParamsConstraint(t *testing.T) {
	t.Run("param limit", func(t *testing.T) {
		f, _ := New(WithMaxRouteParams(3))
		_, _, _, err := f.parseRoute("/{1}/{2}/{3}")
		assert.NoError(t, err)
		_, _, _, err = f.parseRoute("/{1}/{2}/{3}/{4}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/ab{1}/{2}/cd/{3}/{4}/ef")
		assert.Error(t, err)
	})
	t.Run("param key limit", func(t *testing.T) {
		f, _ := New(WithMaxRouteParamKeyBytes(3))
		_, _, _, err := f.parseRoute("/{abc}/{abc}/{abc}")
		assert.NoError(t, err)
		_, _, _, err = f.parseRoute("/{abcd}/{abc}/{abc}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{abc}/{abcd}/{abc}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{abc}/{abc}/{abcd}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{abc}/*{abcd}/{abc}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{abc}/{abc}/*{abcdef}")
		assert.Error(t, err)
	})
	t.Run("param key limit with regexp", func(t *testing.T) {
		f, _ := New(WithMaxRouteParamKeyBytes(3), AllowRegexpParam(true))
		_, _, _, err := f.parseRoute("/{abc:a}/{abc:a}/{abc:a}")
		assert.NoError(t, err)
		_, _, _, err = f.parseRoute("/{abcd:a}/{abc:a}/{abc:a}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{abc:a}/{abcd:a}/{abc:a}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{abc:a}/{abc:a}/{abcd:a}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{abc:a}/*{abcd:a}/{abc:a}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{abc:a}/{abc:a}/*{abcdef:a}")
		assert.Error(t, err)
	})
	t.Run("disabled regexp support for param", func(t *testing.T) {
		f, _ := New()
		_, _, _, err := f.parseRoute("/{a}/{b}/{c}")
		assert.NoError(t, err)
		// path params
		_, _, _, err = f.parseRoute("/{a:a}/{b}/{c}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{a}/{b:b}/{c}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{a}/{b}/{c:c}")
		assert.Error(t, err)
		// hostname params
		_, _, _, err = f.parseRoute("{a:a}.{b}.{c}/")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("{a}.{b:b}.{c}/")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("{a}.{b}.{c:c}/")
		assert.Error(t, err)
	})
	t.Run("disabled regexp support for wildcard", func(t *testing.T) {
		f, _ := New()
		_, _, _, err := f.parseRoute("/{a}/{b}/{c}")
		assert.NoError(t, err)
		// wildcard
		_, _, _, err = f.parseRoute("/*{a:a}/{b}/{c}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{a}/*{b:b}/{c}")
		assert.Error(t, err)
		_, _, _, err = f.parseRoute("/{a}/{b}/*{c:c}")
		assert.Error(t, err)
	})
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
			tree := f.getTree()
			c := newTestContext(f)
			n, got := lookupByPath(tree.root[http.MethodGet], tc.key, c, true)
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
			name:     "current not a leaf with extra ts",
			paths:    []string{"/foo", "/foo/bar", "/foo/baz"},
			req:      "/foo/",
			method:   http.MethodGet,
			wantCode: http.StatusOK,
			wantPath: "/foo",
		},
		{
			name:     "current not a leaf without extra ts",
			paths:    []string{"/foo/", "/foobar"},
			req:      "/foo",
			method:   http.MethodGet,
			wantCode: http.StatusOK,
			wantPath: "/foo/",
		},
		{
			name:     "current not a leaf without extra ts and child not a leaf",
			paths:    []string{"/foo/kam", "/foobar", "/foo/bar"},
			req:      "/foo",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "current not a leaf without extra ts but current not matched completely",
			paths:    []string{"/foo/", "/foobar"},
			req:      "/fo",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "current not a leaf without extra ts and child as more than a slash",
			paths:    []string{"/foo/b", "/foobar"},
			req:      "/a/foo",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
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
			name:     "mid edge key without extra ts",
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
		{
			name:     "incomplete match end of edge with with ts request not cleaned",
			paths:    []string{"/foo", "/foo/", "/foo/x/", "/foo/z/"},
			req:      "/foo///",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "incomplete match end of edge with with ts request not cleaned",
			paths:    []string{"/"},
			req:      "//",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(WithHandleTrailingSlash(RelaxedSlash))
			rf := f.Stats()
			assert.Equal(t, RelaxedSlash, rf.TrailingSlashOption)
			for _, path := range tc.paths {
				require.NoError(t, onlyError(f.Handle(tc.method, path, func(c Context) {
					_ = c.String(http.StatusOK, c.Pattern())
				})))
				rte := f.Route(tc.method, path)
				require.NotNil(t, rte)
				assert.Equal(t, RelaxedSlash, rte.handleSlash)
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
			wantLocation: "/foo",
		},
		{
			name:         "current not a leaf post method and status moved permanently with extra ts",
			paths:        []string{"/foo", "/foo/x/", "/foo/z/"},
			req:          "/foo/",
			method:       http.MethodPost,
			wantCode:     http.StatusPermanentRedirect,
			wantLocation: "/foo",
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
			wantLocation: "/foo/bar/",
		},
		{
			name:         "mid edge key with post method and status permanent redirect with extra ts",
			paths:        []string{"/foo/bar/"},
			req:          "/foo/bar",
			method:       http.MethodPost,
			wantCode:     http.StatusPermanentRedirect,
			wantLocation: "/foo/bar/",
		},
		{
			name:         "mid edge key with get method and status moved permanently without extra ts",
			paths:        []string{"/foo/bar/baz", "/foo/bar"},
			req:          "/foo/bar/",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar",
		},
		{
			name:         "mid edge key with post method and status permanent redirect without extra ts",
			paths:        []string{"/foo/bar/baz", "/foo/bar"},
			req:          "/foo/bar/",
			method:       http.MethodPost,
			wantCode:     http.StatusPermanentRedirect,
			wantLocation: "/foo/bar",
		},
		{
			name:         "incomplete match end of edge with get method",
			paths:        []string{"/foo/bar"},
			req:          "/foo/bar/",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar",
		},
		{
			name:         "incomplete match end of edge with post method",
			paths:        []string{"/foo/bar"},
			req:          "/foo/bar/",
			method:       http.MethodPost,
			wantCode:     http.StatusPermanentRedirect,
			wantLocation: "/foo/bar",
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
			f, _ := New(WithHandleTrailingSlash(RedirectSlash))
			rf := f.Stats()
			assert.Equal(t, RedirectSlash, rf.TrailingSlashOption)

			for _, path := range tc.paths {
				require.NoError(t, onlyError(f.Handle(tc.method, path, emptyHandler)))
				rte := f.Route(tc.method, path)
				require.NotNil(t, rte)
				assert.Equal(t, RedirectSlash, rte.TrailingSlashOption())
			}

			req := httptest.NewRequest(tc.method, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			if w.Code == http.StatusPermanentRedirect || w.Code == http.StatusMovedPermanently {
				assert.Equal(t, tc.wantLocation, w.Header().Get(HeaderLocation))
				if tc.method == http.MethodGet {
					assert.Equal(t, MIMETextHTMLCharsetUTF8, w.Header().Get(HeaderContentType))
				}
			}
		})
	}
}

func TestHandleRedirectFixedPath(t *testing.T) {
	cases := []struct {
		name         string
		path         string
		req          string
		method       string
		slashMode    TrailingSlashOption
		wantCode     int
		wantLocation string
	}{
		{
			name:         "redirect with consecutive slash",
			path:         "/foo/bar",
			slashMode:    StrictSlash,
			req:          "/foo//bar",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar",
		},
		{
			name:         "redirect parent dir reference",
			path:         "/bar",
			slashMode:    StrictSlash,
			req:          "/foo/../bar",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/bar",
		},
		{
			name:         "redirect with consecutive slash and redirect slash",
			path:         "/foo/bar",
			slashMode:    RedirectSlash,
			req:          "/foo//bar/",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar/",
		},
		{
			name:         "redirect with consecutive slash and redirect slash and 308",
			path:         "/foo/bar",
			slashMode:    RedirectSlash,
			req:          "/foo//bar/",
			method:       http.MethodPost,
			wantCode:     http.StatusPermanentRedirect,
			wantLocation: "/foo/bar/",
		},
		{
			name:      "no redirect with consecutive slash and strict slash",
			path:      "/foo/bar",
			slashMode: StrictSlash,
			req:       "/foo//bar/",
			method:    http.MethodPost,
			wantCode:  http.StatusNotFound,
		},
		{
			name:         "redirect with consecutive slash and relaxed slash",
			path:         "/foo/bar",
			slashMode:    RelaxedSlash,
			req:          "/foo//bar/",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar/",
		},
		{
			name:         "redirect with consecutive slash and raw path",
			path:         "/foo/{url}",
			slashMode:    StrictSlash,
			req:          "/foo//https%3A%2F%2Fbar%2Fbaz",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/https%3A%2F%2Fbar%2Fbaz",
		},
		{
			name:         "redirect with consecutive slash, raw path and relaxed slash",
			path:         "/foo/{url}",
			slashMode:    RelaxedSlash,
			req:          "/foo//https%3A%2F%2Fbar%2Fbaz/",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/https%3A%2F%2Fbar%2Fbaz/",
		},
		{
			name:         "redirect with consecutive slash and query",
			path:         "/foo/bar",
			slashMode:    StrictSlash,
			req:          "/foo//bar?1=2",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar?1=2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(WithHandleFixedPath(RedirectPath), WithHandleTrailingSlash(tc.slashMode))
			rf := f.Stats()
			assert.Equal(t, RedirectPath, rf.FixedPathOption)

			require.NoError(t, onlyError(f.Handle(tc.method, tc.path, emptyHandler)))

			req := httptest.NewRequest(tc.method, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			if w.Code == http.StatusPermanentRedirect || w.Code == http.StatusMovedPermanently {
				assert.Equal(t, tc.wantLocation, w.Header().Get(HeaderLocation))
			}
		})
	}
}

func TestHandleRelaxedFixedPath(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		req       string
		slashMode TrailingSlashOption
		wantCode  int
	}{
		{
			name:      "handle with consecutive slash",
			path:      "/foo/bar",
			slashMode: StrictSlash,
			req:       "/foo//bar",
			wantCode:  http.StatusOK,
		},
		{
			name:      "handle with consecutive slash and relaxed slash",
			path:      "/foo/bar",
			slashMode: RelaxedSlash,
			req:       "/foo//bar/",
			wantCode:  http.StatusOK,
		},
		{
			name:      "do not handle with consecutive slash and strict slash",
			path:      "/foo/bar",
			slashMode: StrictSlash,
			req:       "/foo//bar/",
			wantCode:  http.StatusNotFound,
		},
		{
			name:      "do not handle with consecutive slash and redirect slash",
			path:      "/foo/bar",
			slashMode: RedirectSlash,
			req:       "/foo//bar/",
			wantCode:  http.StatusNotFound,
		},
		{
			name:      "handle with consecutive slash and raw path",
			path:      "/foo/{url}",
			slashMode: StrictSlash,
			req:       "/foo//https%3A%2F%2Fbar%2Fbaz",
			wantCode:  http.StatusOK,
		},
		{
			name:      "handle parent dir reference",
			path:      "/bar",
			slashMode: StrictSlash,
			req:       "/foo/../bar",
			wantCode:  http.StatusOK,
		},
		{
			name:      "handle with consecutive slash and query",
			path:      "/foo/bar",
			slashMode: StrictSlash,
			req:       "/foo//bar?1=2",
			wantCode:  http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(WithHandleFixedPath(RelaxedPath), WithHandleTrailingSlash(tc.slashMode))
			rf := f.Stats()
			assert.Equal(t, RelaxedPath, rf.FixedPathOption)

			require.NoError(t, onlyError(f.Handle(http.MethodGet, tc.path, func(c Context) {
				c.Writer().WriteHeader(tc.wantCode)
			})))

			req := httptest.NewRequest(http.MethodGet, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

func TestEncodedRedirectTrailingSlash(t *testing.T) {
	cases := []struct {
		name         string
		path         string
		req          string
		wantCode     int
		wantLocation string
	}{
		{
			name:         "encoded slash redirect",
			path:         "/foo/{bar}/",
			req:          "/foo/bar%2Fbaz",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar%2Fbaz/",
		},
		{
			name:         "encoded slash redirect with query parameters",
			path:         "/foo/{bar}/",
			req:          "/foo/bar%2Fbaz?key=value&foo=bar",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar%2Fbaz/?key=value&foo=bar",
		},
		{
			name:         "open redirect with slash",
			path:         "/*{any}/",
			req:          "//evil.com",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/%2Fevil.com/",
		},
		{
			name:         "open redirect with backslash",
			path:         "/*{any}/",
			req:          "/\\evil.com",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/%5Cevil.com/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := New(WithHandleTrailingSlash(RedirectSlash))
			require.NoError(t, onlyError(r.Handle(http.MethodGet, tc.path, emptyHandler)))

			req := httptest.NewRequest(http.MethodGet, tc.req, nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantLocation, w.Header().Get(HeaderLocation))
		})
	}
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
			name:   "current not a leaf, with leave on exact match",
			routes: []string{"/a/foo/", "/a/foobar", "/{a}/foo"},
			target: "/a/foo",
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
			},
			wantPath: "/{a}/foo",
		},
		{
			name:   "current not a leaf, with child slash match",
			routes: []string{"/{x}/foo/", "/{x}/foobar", "/a/fo"},
			target: "/a/foo",
			wantParams: Params{
				{
					Key:   "x",
					Value: "a",
				},
			},
			wantPath: "/{x}/foo/",
			wantTsr:  true,
		},
		{
			name:     "current not a leaf, with child slash match and backtrack",
			routes:   []string{"/{param}/b/foo/", "/{param}/b/foobar", "/{param}/{b}/fo"},
			target:   "/a/b/foo",
			wantPath: "/{param}/b/foo/",
			wantParams: Params{
				{
					Key:   "param",
					Value: "a",
				},
			},
			wantTsr: true,
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
			f, _ := New(WithHandleTrailingSlash(RelaxedSlash))
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

	tree := f.getTree()
	assert.Equal(t, 0, cnt)
	assert.Equal(t, 0, len(tree.root))
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

	tree := f.getTree()
	assert.Equal(t, 0, cnt)
	assert.Equal(t, 0, len(tree.root))
}

func TestTree_DeleteRoot(t *testing.T) {
	f, _ := New()
	require.NoError(t, onlyError(f.Handle(http.MethodOptions, "/foo/bar", emptyHandler)))
	deletedRoute, err := f.Delete(http.MethodOptions, "/foo/bar")
	require.NoError(t, err)
	assert.Equal(t, "/foo/bar", deletedRoute.Pattern())
	tree := f.getTree()
	assert.Equal(t, 0, len(tree.root))
	require.NoError(t, onlyError(f.Handle(http.MethodOptions, "exemple.com/foo/bar", emptyHandler)))
	deletedRoute, err = f.Delete(http.MethodOptions, "exemple.com/foo/bar")
	require.NoError(t, err)
	assert.Equal(t, "exemple.com/foo/bar", deletedRoute.Pattern())
	tree = f.getTree()
	assert.Equal(t, 0, len(tree.root))
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
	tree := f.getTree()
	assert.Len(t, tree.root, 0)
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

	tree := f.getTree()
	assert.Len(t, tree.root, 0)
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
	f, _ := New(WithHandleTrailingSlash(RelaxedSlash))
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
		want    []string
		methods []string
	}{
		{
			name:    "all route except the last one",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead},
			path:    "/foo/bar",
			target:  http.MethodTrace,
			want:    []string{"GET", "POST", "PUT", "DELETE", "PATCH", "CONNECT", "OPTIONS", "HEAD"},
		},
		{
			name:    "all route except the first one",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz",
			target:  http.MethodGet,
			want:    []string{"POST", "PUT", "DELETE", "PATCH", "CONNECT", "OPTIONS", "HEAD", "TRACE"},
		},
		{
			name:    "all route except patch and delete",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/test",
			target:  http.MethodPatch,
			want:    []string{"GET", "POST", "PUT", "CONNECT", "OPTIONS", "HEAD", "TRACE"},
		},
	}

	parseAllowHeader := func(allow string) []string {
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
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
			assert.ElementsMatch(t, tc.want, parseAllowHeader(w.Header().Get("Allow")))
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
	f, _ := New(WithNoMethod(true), WithHandleTrailingSlash(RelaxedSlash))

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
			want:    []string{"GET", "POST", "PUT", "DELETE", "PATCH", "CONNECT", "OPTIONS", "HEAD"},
		},
		{
			name:    "all route except the first one",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz",
			req:     "/foo/baz/",
			target:  http.MethodGet,
			want:    []string{"POST", "PUT", "DELETE", "PATCH", "CONNECT", "OPTIONS", "HEAD", "TRACE"},
		},
	}

	parseAllowHeader := func(allow string) []string {
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
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
			assert.ElementsMatch(t, tc.want, parseAllowHeader(w.Header().Get("Allow")))
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
		want    []string
		methods []string
	}{
		{
			name:    "all route except the last one",
			methods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodOptions, http.MethodHead},
			path:    "/foo/bar",
			req:     "/foo/bar",
			target:  http.MethodTrace,
			want:    []string{"GET", "POST", "PUT", "DELETE", "PATCH", "CONNECT", "OPTIONS", "HEAD"},
		},
		{
			name:    "all route except the first one and inferred options from auto options",
			methods: []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodConnect, http.MethodHead, http.MethodTrace},
			path:    "/foo/baz/",
			req:     "/foo/baz/",
			target:  http.MethodGet,
			want:    []string{"POST", "PUT", "DELETE", "PATCH", "CONNECT", "HEAD", "TRACE", "OPTIONS"},
		},
	}

	parseAllowHeader := func(allow string) []string {
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
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
			assert.ElementsMatch(t, tc.want, parseAllowHeader(w.Header().Get("Allow")))
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
			wantCode: http.StatusNotFound,
		},
		{
			name:     "regular option request",
			target:   "/foo",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			want:     []string{"GET", "PUT", "TRACE", "OPTIONS"},
			wantCode: http.StatusOK,
		},
		{
			name:     "regular option request with handler priority",
			target:   "/foo",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     []string{"GET", "OPTIONS", "PUT", "TRACE"},
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

	parseAllowHeader := func(allow string) []string {
		if allow == "" {
			return nil
		}
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
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
			assert.ElementsMatch(t, tc.want, parseAllowHeader(w.Header().Get("Allow")))
		})
	}
}

func TestRouterWithAutomaticOptionsAndIgnoreTsOptionEnable(t *testing.T) {
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
			wantCode: http.StatusNotFound,
		},
		{
			name:     "regular option request and ignore ts",
			target:   "/foo/",
			path:     "/foo",
			methods:  []string{"GET", "TRACE", "PUT"},
			want:     []string{"GET", "PUT", "TRACE", "OPTIONS"},
			wantCode: http.StatusOK,
		},
		{
			name:     "regular option request with handler priority and ignore ts",
			target:   "/foo",
			path:     "/foo/",
			methods:  []string{"GET", "TRACE", "PUT", "OPTIONS"},
			want:     []string{"GET", "OPTIONS", "PUT", "TRACE"},
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

	parseAllowHeader := func(allow string) []string {
		if allow == "" {
			return nil
		}
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New(WithAutoOptions(true), WithHandleTrailingSlash(RelaxedSlash))
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
			assert.ElementsMatch(t, tc.want, parseAllowHeader(w.Header().Get("Allow")))
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

	parseAllowHeader := func(allow string) []string {
		if allow == "" {
			return nil
		}
		parts := strings.Split(allow, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.ElementsMatch(t, []string{"GET", "POST", "OPTIONS"}, parseAllowHeader(w.Header().Get("Allow")))
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

	t.Run("reverse with hostname", func(t *testing.T) {
		f, _ := New()
		route, err := f.Handle(http.MethodGet, "{sub}.exemple.com/foo", emptyHandler)
		require.NoError(t, err)
		got, tsr := f.Reverse(http.MethodGet, "foo.exemple.com", "/foo")
		assert.False(t, tsr)
		require.NotNil(t, route)
		assert.Equal(t, route, got)
	})

	t.Run("reverse with hostname (case-insensitive)", func(t *testing.T) {
		f, _ := New()
		route, err := f.Handle(http.MethodGet, "{sub}.exemple.com/foo", emptyHandler)
		require.NoError(t, err)
		got, tsr := f.Reverse(http.MethodGet, "FOO.EXEMPLE.COM", "/foo")
		assert.False(t, tsr)
		require.NotNil(t, route)
		assert.Equal(t, route, got)
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

	f, _ := New(WithHandleTrailingSlash(RelaxedSlash))
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

func TestFuzzInsertNoPanics(t *testing.T) {
	unicodeRanges := fuzz.UnicodeRanges{
		{First: 0x20, Last: 0x6FF},
	}
	f := fuzz.New().NilChance(0).NumElements(1000000, 2000000).Funcs(unicodeRanges.CustomStringFuzzFunc())
	r, _ := New()

	routes := make(map[string]struct{})
	f.Fuzz(&routes)

	_ = r.Updates(func(txn *Txn) error {
		for rte := range routes {
			if rte == "" {
				continue
			}
			require.NotPanicsf(t, func() {
				_, _ = txn.Handle(http.MethodGet, rte, emptyHandler)
			}, fmt.Sprintf("rte: %s", rte))
		}
		return nil
	})

}

func TestFuzzInsertLookupUpdateAndDelete(t *testing.T) {
	// no '*' and '{}' and invalid escape char
	unicodeRanges := fuzz.UnicodeRanges{
		{First: 0x20, Last: 0x29},
		{First: 0x2B, Last: 0x7A},
		{First: 0x7C, Last: 0x7C},
		{First: 0x7E, Last: 0x04FF},
	}

	f := fuzz.New().NilChance(0).NumElements(100000, 200000).Funcs(unicodeRanges.CustomStringFuzzFunc())
	r, _ := New()

	routes := make(map[string]struct{})
	f.Fuzz(&routes)

	inserted := 0
	_ = r.Updates(func(txn *Txn) error {
		for rte := range routes {
			rte, err := txn.Handle(http.MethodGet, "/"+rte, emptyHandler)
			if err != nil {
				assert.Nil(t, rte, "route /%s", rte)
				continue
			}
			assert.NotNilf(t, rte, "route /%s", rte)
			inserted++
		}
		return nil
	})

	it := r.Iter()
	countPath := len(slices.Collect(iterutil.Right(it.All())))
	assert.Equal(t, inserted, countPath)

	for method, route := range r.Iter().All() {
		found := r.Route(method, route.Pattern())
		require.NotNilf(t, found, "route /%s", route.Pattern())
	}

	_ = r.Updates(func(txn *Txn) error {
		for method, route := range r.Iter().All() {
			rte, err := txn.Delete(method, route.Pattern())
			assert.NoErrorf(t, err, "route /%s", route.Pattern())
			assert.NotNil(t, rte, "route /%s", route.Pattern())
		}
		return nil
	})

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
	tree := f.getTree()
	for _, n := range tree.root {
		assert.Len(t, n.statics, 1)
		assert.Len(t, n.params, 0)
		assert.Len(t, n.wildcards, 0)
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
	tree := f.getTree()

	want := `path: GET
      path: /foo/ [params: 1]
          path: ?
              path: / [wildcards: 1]
                  path: * [leaf=/foo/{bar}/*{baz}]`
	assert.Equal(t, want, strings.TrimSuffix(tree.root[http.MethodGet].String(), "\n"))
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
				if !tsr || route.TrailingSlashOption() == RelaxedSlash {
					if err := c.Redirect(code, cleanedPath); err != nil {
						// Only if not in the range 300..308, so not possible here!
						panic(err)
					}
					return
				}

				if err := c.Redirect(code, cleanedPath); err != nil {
					// Only if not in the range 300..308, so not possible here
					panic(err)
				}
				return
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

func TestX(t *testing.T) {
	f, _ := New(AllowRegexpParam(true))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/*{any:[A-z/]+}/bar/{baz}/", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/*{any:[A-z/]+}", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/*{any:[A-z/*]+}", emptyHandler)))

	// require.NoError(t, onlyError(f.Handle(http.MethodGet, "/*{any:a/b/c}", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/{a}/{b}/{c}/bar/foo/f", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/*{any}", emptyHandler)))

	tree := f.getTree()
	fmt.Println(tree.root[http.MethodGet])

	c := newTestContext(f)
	n, tsr := tree.lookup(http.MethodGet, "", "/a/?/c/bar/foo/ff", c, false)
	fmt.Println(n, tsr)
	fmt.Println(c.params)
}

func TestY(t *testing.T) {
	f, _ := New(AllowRegexpParam(true))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "{a}/*{a}/b/*{c}/e", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "a/*{a}/b/*{c}/d/", emptyHandler)))
	tree := f.getTree()
	fmt.Println(tree.root[http.MethodGet])

	c := newTestContext(f)
	n, tsr := tree.lookup(http.MethodGet, "a", "/a/b/c/d", c, false)
	if n != nil {
		c.tsr = tsr
		c.route = n.route
		fmt.Println(tsr)
		fmt.Println(n.route.pattern)
		fmt.Println(slices.Collect(c.Params()))
	}
}

func TestZ(t *testing.T) {
	f, _ := New(AllowRegexpParam(true))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/*{args}", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/*{a}/b/*{c}/f", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/*{a}/b/*{l}/g/", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/*{a}/b/*{x:[A-z]+}/e", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/*{a}/b/*{c}/d/", emptyHandler)))
	tree := f.getTree()
	fmt.Println(tree.root[http.MethodGet])

	c := newTestContext(f)
	n, tsr := tree.lookup(http.MethodGet, "", "/a/b/c/d", c, false)
	if n != nil {
		c.tsr = tsr
		c.route = n.route
		fmt.Println(tsr)
		fmt.Println(n.route.pattern)
		fmt.Println(slices.Collect(c.Params()))
	}
}

func TestXyz(t *testing.T) {
	f, _ := New(AllowRegexpParam(true))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/{a}/b", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/{a:a}/b/c", emptyHandler)))
	tree := f.getTree()
	fmt.Println(tree.root[http.MethodGet])

	c := newTestContext(f)
	n, tsr := tree.lookup(http.MethodGet, "", "/a/", c, false)
	if n != nil {
		c.tsr = tsr
		c.route = n.route
		fmt.Println(tsr)
		fmt.Println(n.route.pattern)
		fmt.Println(slices.Collect(c.Params()))
	}
}
