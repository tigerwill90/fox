// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-toolkit/fox/internal/iterutil"
	"github.com/fox-toolkit/fox/internal/netutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	emptyHandler   = HandlerFunc(func(c *Context) {})
	pathHandler    = HandlerFunc(func(c *Context) { _ = c.String(200, c.Request().URL.Path) })
	patternHandler = HandlerFunc(func(c *Context) { _ = c.String(200, c.Pattern()) })
)

var replaceParams = regexp.MustCompile(`[*+]?\{[^}]+}`)

type mockResponseWriter struct{}

func (m mockResponseWriter) Header() (h http.Header) { return http.Header{} }

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
	{"GET", "/repos/{owner}/{repo}/git/refs/+{ref}"},
	{"GET", "/repos/{owner}/{repo}/git/refs"},
	{"POST", "/repos/{owner}/{repo}/git/refs"},
	{"DELETE", "/repos/{owner}/{repo}/git/refs/+{ref}"},
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
	{"GET", "/repos/{owner}/{repo}/contents/+{path}"},
	{"DELETE", "/repos/{owner}/{repo}/contents/+{path}"},
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

func TestRouter_ServeHTTP_EmptyMethod(t *testing.T) {
	f := MustRouter()
	require.NoError(t, onlyError(f.Add([]string{http.MethodGet, http.MethodPost}, "/foo", emptyHandler)))

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req.Method = ""
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRouter_ServeHTTP_Static(t *testing.T) {
	f := MustRouter()

	for _, route := range staticRoutes {
		require.NoError(t, onlyError(f.Add([]string{route.method}, route.path, pathHandler)))
	}

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}

	assert.Equal(t, iterutil.Len(f.Iter().All()), f.Len())
}

func TestRouter_ServeHTTP_StaticHostname(t *testing.T) {
	f, _ := NewRouter()

	for _, route := range staticHostnames {
		require.NoError(t, onlyError(f.Add([]string{route.method}, route.path+"/foo", patternHandler)))
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

	assert.Equal(t, iterutil.Len(f.Iter().All()), f.Len())
}

func TestRouter_ServeHTTP_StaticTxn(t *testing.T) {
	f, _ := NewRouter()

	require.NoError(t, f.Updates(func(txn *Txn) error {
		for _, route := range staticRoutes {
			if err := onlyError(txn.Add([]string{route.method}, route.path, pathHandler)); err != nil {
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

	assert.Equal(t, iterutil.Len(f.Iter().All()), f.Len())
}

func TestRouter_ServeHTTP_StaticWithStaticDomain(t *testing.T) {
	f, _ := NewRouter()

	for _, route := range staticRoutes {
		require.NoError(t, onlyError(f.Add([]string{route.method}, "example.com"+route.path, pathHandler)))
	}

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Host = "example.com"
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}

	assert.Equal(t, iterutil.Len(f.Iter().All()), f.Len())
}

func TestRouter_ServeHTTP_StaticWithStaticDomainTxn(t *testing.T) {
	f, _ := NewRouter()

	require.NoError(t, f.Updates(func(txn *Txn) error {
		for _, route := range staticRoutes {
			if err := onlyError(txn.Add([]string{route.method}, "example.com"+route.path, pathHandler)); err != nil {
				return err
			}
		}
		return nil
	}))

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Host = "example.com"
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}

	assert.Equal(t, iterutil.Len(f.Iter().All()), f.Len())
}

func TestRouter_ServeHTTP_StaticMalloc(t *testing.T) {
	r, _ := NewRouter()

	for _, route := range staticRoutes {
		require.NoError(t, onlyError(r.Add([]string{route.method}, route.path, emptyHandler)))
	}

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestRouter_ServeHTTP_StaticWithStaticDomainMalloc(t *testing.T) {
	r, _ := NewRouter()

	for _, route := range staticRoutes {
		require.NoError(t, onlyError(r.Add([]string{route.method}, "example.com"+route.path, emptyHandler)))
	}

	for _, route := range staticRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Host = "example.com"
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestRouter_ServeHTTP_Params(t *testing.T) {
	rx := regexp.MustCompile("({|\\+{)[A-z]+[}]")
	r, _ := NewRouter()
	h := func(c *Context) {
		matches := rx.FindAllString(c.Request().URL.Path, -1)
		for _, match := range matches {
			var key string
			if strings.HasPrefix(match, "+") {
				key = match[2 : len(match)-1]
			} else {
				key = match[1 : len(match)-1]
			}
			value := match
			ps, _ := url.PathUnescape(c.Param(key))
			assert.Equal(t, value, ps)
		}
		assert.Equal(t, c.Request().URL.Path, c.Pattern())
		_ = c.String(200, c.Request().URL.Path)
	}
	for _, route := range githubAPI {
		require.NoError(t, onlyError(r.Add([]string{route.method}, route.path, h)))
		if route.method == http.MethodGet {
			require.NoError(t, onlyError(r.Add(MethodAny, route.path, h)))
		}

	}
	for _, route := range githubAPI {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}

	for _, route := range githubAPI {
		if route.method != http.MethodGet {
			continue
		}
		req := httptest.NewRequest("PURGE", route.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, route.path, w.Body.String())
	}
}

func TestRouter_ServeHTTP_ParamsHostname(t *testing.T) {
	rx := regexp.MustCompile("({|\\+{)[A-z]+[}]")
	r, _ := NewRouter()
	h := func(c *Context) {
		matches := rx.FindAllString(c.Request().URL.Path, -1)
		for _, match := range matches {
			var key string
			if strings.HasPrefix(match, "+") {
				key = match[2 : len(match)-1]
			} else {
				key = match[1 : len(match)-1]
			}
			value := match
			assert.Equal(t, value, c.Param(key))
		}

		host := strings.ToLower(netutil.StripHostPort(c.Host()))
		assert.Equal(t, host+c.Request().URL.Path, c.Pattern())
		_ = c.String(200, host+c.Request().URL.Path)
	}
	for _, route := range wildcardHostnames {
		require.NoError(t, onlyError(r.Add([]string{route.method}, route.path+"/foo", h)))
		if route.method == http.MethodGet {
			require.NoError(t, onlyError(r.Add(MethodAny, route.path+"/foo", h)))
		}
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

	t.Run("same case with any method", func(t *testing.T) {
		for _, route := range wildcardHostnames {
			if route.method != http.MethodGet {
				continue
			}
			req, err := http.NewRequest("PURGE", "/foo", nil)
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

	t.Run("case insensitive with any method", func(t *testing.T) {
		for _, route := range wildcardHostnames {
			if route.method != http.MethodGet {
				continue
			}

			req, err := http.NewRequest("PURGE", "/foo", nil)
			require.NoError(t, err)
			req.Host = strings.ToUpper(route.path)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, route.path+"/foo", w.Body.String())
		}
	})
}

func TestRouter_ServeHTTP_ParamsTxn(t *testing.T) {
	rx := regexp.MustCompile("({|\\+{)[A-z]+[}]")
	r, _ := NewRouter()
	h := func(c *Context) {
		matches := rx.FindAllString(c.Request().URL.Path, -1)
		for _, match := range matches {
			var key string
			if strings.HasPrefix(match, "+") {
				key = match[2 : len(match)-1]
			} else {
				key = match[1 : len(match)-1]
			}
			value := match
			ps, _ := url.PathUnescape(c.Param(key))
			assert.Equal(t, value, ps)
		}
		assert.Equal(t, c.Request().URL.Path, c.Pattern())
		_ = c.String(200, c.Request().URL.Path)
	}
	require.NoError(t, r.Updates(func(txn *Txn) error {
		for _, route := range githubAPI {
			if err := onlyError(txn.Add([]string{route.method}, route.path, h)); err != nil {
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

func TestRouter_ServeHTTP_ParamsWithDomain(t *testing.T) {
	rx := regexp.MustCompile("({|\\+{)[A-z]+[}]")
	r, _ := NewRouter()
	h := func(c *Context) {
		matches := rx.FindAllString(c.Request().URL.Path, -1)
		for _, match := range matches {
			var key string
			if strings.HasPrefix(match, "+") {
				key = match[2 : len(match)-1]
			} else {
				key = match[1 : len(match)-1]
			}
			value := match
			ps, _ := url.PathUnescape(c.Param(key))
			assert.Equal(t, value, ps)
		}

		assert.Equal(t, netutil.StripHostPort(c.Host())+c.Request().URL.Path, c.Pattern())
		_ = c.String(200, netutil.StripHostPort(c.Host())+c.Request().URL.Path)
	}
	for _, route := range githubAPI {
		require.NoError(t, onlyError(r.Add([]string{route.method}, "foo.{bar}.com"+route.path, h)))
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

func TestRouter_ServeHTTP_ParamsWithDomainTxn(t *testing.T) {
	rx := regexp.MustCompile("({|\\+{)[A-z]+[}]")
	r, _ := NewRouter()
	h := func(c *Context) {
		matches := rx.FindAllString(c.Request().URL.Path, -1)
		for _, match := range matches {
			var key string
			if strings.HasPrefix(match, "+") {
				key = match[2 : len(match)-1]
			} else {
				key = match[1 : len(match)-1]
			}
			value := match
			ps, _ := url.PathUnescape(c.Param(key))
			assert.Equal(t, value, ps)
		}

		assert.Equal(t, netutil.StripHostPort(c.Host())+c.Request().URL.Path, c.Pattern())
		_ = c.String(200, netutil.StripHostPort(c.Host())+c.Request().URL.Path)
	}
	require.NoError(t, r.Updates(func(txn *Txn) error {
		for _, route := range githubAPI {
			if err := onlyError(txn.Add([]string{route.method}, "foo.{bar}.com"+route.path, h)); err != nil {
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

func TestRouter_ServeHTTP_ParamsMalloc(t *testing.T) {
	r, _ := NewRouter()
	for _, route := range githubAPI {
		require.NoError(t, onlyError(r.Add([]string{route.method}, route.path, emptyHandler)))
	}

	data := make([]route, 0, len(githubAPI))
	for _, r := range githubAPI {
		data = append(data, route{method: r.method, path: replaceParams.ReplaceAllString(r.path, "xxx")})
	}

	for _, route := range data {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
		assert.Equal(t, http.StatusOK, w.Code)
	}
}

func TestRouter_ServeHTTP_Handle(t *testing.T) {
	f, _ := NewRouter()

	t.Run("handle and update route with some option", func(t *testing.T) {
		want, err := f.NewRoute(MethodGet, "/foo", emptyHandler, WithAnnotation("foo", "bar"), WithTrailingSlash(RedirectSlash))
		require.NoError(t, err)
		require.NoError(t, f.AddRoute(want))
		got := f.Route(MethodGet, "/foo")
		assert.Equal(t, want, got)
		assert.Equal(t, RedirectSlash, got.TrailingSlashOption())

		want, err = f.NewRoute(MethodGet, "/foo", emptyHandler, WithAnnotation("baz", "baz"))
		require.NoError(t, err)
		require.NoError(t, f.UpdateRoute(want))
		got = f.Route(MethodGet, "/foo")
		assert.Equal(t, want, got)
		assert.Equal(t, ExactSlash, got.TrailingSlashOption())
	})

	t.Run("route with invalid method", func(t *testing.T) {
		rte, err := f.NewRoute([]string{""}, "/bar", emptyHandler)
		assert.ErrorIs(t, err, ErrInvalidRoute)
		assert.Nil(t, rte)
	})

	t.Run("handle, update and delete route with nil or zero-value route", func(t *testing.T) {
		assert.ErrorIs(t, f.AddRoute(nil), ErrInvalidRoute)
		assert.ErrorIs(t, f.UpdateRoute(nil), ErrInvalidRoute)
		assert.ErrorIs(t, onlyError(f.DeleteRoute(nil)), ErrInvalidRoute)
		assert.ErrorIs(t, f.AddRoute(&Route{}), ErrInvalidRoute)
		assert.ErrorIs(t, f.UpdateRoute(&Route{}), ErrInvalidRoute)
		assert.ErrorIs(t, onlyError(f.DeleteRoute(&Route{})), ErrInvalidRoute)
		assert.Equal(t, 1, f.Len())
	})
}

func TestRouter_ServeHTTP_ParamsWithDomainMalloc(t *testing.T) {
	r, _ := NewRouter()
	for _, route := range githubAPI {
		require.NoError(t, onlyError(r.Add([]string{route.method}, "foo.{bar}.com"+route.path, emptyHandler)))
	}

	data := make([]route, 0, len(githubAPI))
	for _, r := range githubAPI {
		data = append(data, route{method: r.method, path: replaceParams.ReplaceAllString(r.path, "xxx")})
	}

	for _, route := range data {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Host = "foo.{bar}.com"
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestRouter_ServeHTTP_InfixWildcardMalloc(t *testing.T) {
	r, _ := NewRouter(WithAllowRegexpParam(true), WithNoRouteHandler(emptyHandler))
	require.NoError(t, onlyError(r.Add(MethodGet, "/+{a}/x/+{b}/y", emptyHandler)))
	require.NoError(t, onlyError(r.Add(MethodGet, "/+{a}/z", emptyHandler)))
	require.NoError(t, onlyError(r.Add(MethodGet, "/r/+{a}/x/+{b:(?:[ab]+/)*[ab]+}/y", emptyHandler)))
	require.NoError(t, onlyError(r.Add(MethodGet, "/v2/+{name:[a-z]+(?:/[a-z]+)*}/manifests/{reference}", emptyHandler)))
	require.NoError(t, onlyError(r.Add(MethodGet, "/v2/+{name:[a-z]+(?:/[a-z]+)*}/blobs/{digest}", emptyHandler)))
	require.NoError(t, onlyError(r.Add(MethodGet, "/m/+{a}/x/+{b}", emptyHandler, WithHeaderMatcher("X-Tok", "a"))))
	require.NoError(t, onlyError(r.Add(MethodGet, "+{a}.x.+{b}.y.com/foo", emptyHandler)))
	require.NoError(t, onlyError(r.Add(MethodGet, "+{a}.z.com/foo", emptyHandler)))

	cases := []struct {
		host string
		path string
	}{
		{host: "example.com", path: "/q/x/w/z"},
		{host: "example.com", path: "/r/a/x/c/x/b/y"},
		{host: "example.com", path: "/v2/" + strings.Repeat("abc/", 200) + "img/manifests/latest"},
		{host: "q.x.w.z.com", path: "/foo"},
		{host: "example.com", path: "/m" + strings.Repeat("/a/x", 50)},
		{host: "a:b:c", path: "/q/x/w/z"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Host = tc.host
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
		assert.Equal(t, http.StatusOK, w.Code)
	}
}

func TestOverlappingRouteMalloc(t *testing.T) {
	r, _ := NewRouter()
	for _, route := range overlappingRoutes {
		require.NoError(t, onlyError(r.Add([]string{route.method}, route.path, emptyHandler)))
	}

	data := make([]route, 0, len(overlappingRoutes))
	for _, r := range overlappingRoutes {
		data = append(data, route{method: r.method, path: replaceParams.ReplaceAllString(r.path, "xxx")})
	}

	for _, route := range data {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		allocs := testing.AllocsPerRun(100, func() { r.ServeHTTP(w, req) })
		assert.Equal(t, float64(0), allocs)
	}
}

func TestWildcardSuffix(t *testing.T) {
	r, _ := NewRouter(WithAllowRegexpParam(true))

	routes := []struct {
		path string
		key  string
	}{
		{"/github.com/etf1/+{repo}", "/github.com/etf1/mux"},
		{"/github.com/johndoe/+{repo}", "/github.com/johndoe/buzz"},
		{"/foo/bar/+{args}", "/foo/bar/baz"},
		{"/filepath/path=+{path}", "/filepath/path=/file.txt"},
		{"/john/doe/+{any:[A-z/]+}", "/john/doe/a/b/c"},
		{"/filepath/key=+{any:[A-z/.]+}", "/filepath/key=/file.txt"},
	}

	for _, route := range routes {
		require.NoError(t, onlyError(r.Add(MethodGet, route.path, pathHandler)))
	}

	for _, route := range routes {
		req := httptest.NewRequest(http.MethodGet, route.key, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "route: key: %s, path: %s", route.key, route.path)
		assert.Equal(t, route.key, w.Body.String())
	}
}

func TestRegexpParamAlternationPrecedence(t *testing.T) {
	r := MustRouter(WithAllowRegexpParam(true))
	require.NoError(t, onlyError(r.Add(MethodGet, "/role/{role:admin|user|guest}", pathHandler)))
	require.NoError(t, onlyError(r.Add(MethodGet, "/scope/{scope:read|write}/items", pathHandler)))

	cases := []struct {
		path string
		want int
	}{
		{"/role/admin", http.StatusOK},
		{"/role/user", http.StatusOK},
		{"/role/guest", http.StatusOK},
		{"/role/adminBYPASS", http.StatusNotFound},
		{"/role/EVILguest", http.StatusNotFound},
		{"/role/userX", http.StatusNotFound},
		{"/role/Xuser", http.StatusNotFound},
		{"/scope/read/items", http.StatusOK},
		{"/scope/write/items", http.StatusOK},
		{"/scope/readBYPASS/items", http.StatusNotFound},
		{"/scope/EVILwrite/items", http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.want, w.Code)
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
		{
			name: "test mixed path wildcard",
			routes: []struct {
				path string
			}{
				{path: "/+{args}"},
				{path: "/+{a}/b/+{c}/f"},
				{path: "/+{a}/b/+{l}/g/"},
				{path: "/+{a}/b/+{x}/e"},
				{path: "/+{a}/b/+{c}/d/"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter()
			routeCopy := make([]struct{ path string }, len(tc.routes))
			copy(routeCopy, tc.routes)

			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte.path, emptyHandler, WithAnnotation("foo", "bar"))))
			}
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Update(MethodGet, rte.path, emptyHandler, WithAnnotation("foo", "bar"))))
			}
			for _, rte := range tc.routes {
				r := f.Route(MethodGet, rte.path)
				require.NotNilf(t, r, "missing method=%s;path=%s", http.MethodGet, rte.path)
				assert.Equal(t, "bar", r.Annotation("foo").(string))
			}

			for _, rte := range tc.routes {
				deletedRoute, err := f.Delete(MethodGet, rte.path)
				require.NoError(t, err)
				assert.Equal(t, rte.path, deletedRoute.Pattern())
				routeCopy = slices.Delete(routeCopy, 0, 1)
				assert.Falsef(t, f.Has(MethodGet, rte.path), "found method=%s;path=%s", http.MethodGet, rte.path)
				for _, rte := range routeCopy {
					require.NoError(t, onlyError(f.Update(MethodGet, rte.path, emptyHandler, WithAnnotation("john", "doe"))))
				}
				for _, rte := range routeCopy {
					r := f.Route(MethodGet, rte.path)
					require.NotNilf(t, r, "missing method=%s;path=%s", http.MethodGet, rte.path)
					assert.Equal(t, "doe", r.Annotation("john").(string))
				}
			}

			tree := f.getTree()
			assert.Len(t, tree.patterns.statics, 0)
			assert.Len(t, tree.patterns.params, 0)
			assert.Len(t, tree.patterns.wildcards, 0)
			assert.Empty(t, tree.methods)

			// Now let's do it in reverse
			routeCopy = make([]struct{ path string }, len(tc.routes))
			copy(routeCopy, tc.routes)
			for i := len(tc.routes) - 1; i >= 0; i-- {
				require.NoError(t, onlyError(f.Add(MethodGet, tc.routes[i].path, emptyHandler)))
			}
			for i := len(tc.routes) - 1; i >= 0; i-- {
				assert.Truef(t, f.Has(MethodGet, tc.routes[i].path), "missing method=%s;path=%s", http.MethodGet, tc.routes[i].path)
			}
			for i := len(tc.routes) - 1; i >= 0; i-- {
				deletedRoute, err := f.Delete(MethodGet, tc.routes[i].path)
				require.NoError(t, err)
				assert.Equal(t, tc.routes[i].path, deletedRoute.Pattern())
				routeCopy = slices.Delete(routeCopy, len(routeCopy)-1, len(routeCopy))
				assert.Falsef(t, f.Has(MethodGet, tc.routes[i].path), "found method=%s;path=%s", http.MethodGet, tc.routes[i].path)
				for _, rte := range routeCopy {
					assert.Truef(t, f.Has(MethodGet, rte.path), "missing method=%s;path=%s", http.MethodGet, rte.path)
				}
			}

			tree = f.getTree()
			assert.Len(t, tree.patterns.statics, 0)
			assert.Len(t, tree.patterns.params, 0)
			assert.Len(t, tree.patterns.wildcards, 0)
			assert.Empty(t, tree.methods)
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
			f, _ := NewRouter()
			routeCopy := make([]struct{ path string }, len(tc.routes))
			copy(routeCopy, tc.routes)

			require.NoError(t, f.Updates(func(txn *Txn) error {
				for _, rte := range tc.routes {
					if err := onlyError(txn.Add(MethodGet, rte.path, emptyHandler, WithAnnotation("foo", "bar"))); err != nil {
						return err
					}
				}
				return nil
			}))
			require.NoError(t, f.Updates(func(txn *Txn) error {
				for _, rte := range tc.routes {
					if err := onlyError(txn.Update(MethodGet, rte.path, emptyHandler, WithAnnotation("foo", "bar"))); err != nil {
						return err
					}
				}
				return nil
			}))

			for _, rte := range tc.routes {
				r := f.Route(MethodGet, rte.path)
				require.NotNilf(t, r, "missing method=%s;path=%s", http.MethodGet, rte.path)
				assert.Equal(t, "bar", r.Annotation("foo").(string))
			}

			require.NoError(t, f.Updates(func(txn *Txn) error {
				for _, rte := range tc.routes {
					deletedRoute, err := txn.Delete(MethodGet, rte.path)
					if err != nil {
						return err
					}
					assert.Equal(t, rte.path, deletedRoute.Pattern())
					routeCopy = slices.Delete(routeCopy, 0, 1)
					assert.Falsef(t, txn.Has(MethodGet, rte.path), "found method=%s;path=%s", http.MethodGet, rte.path)
					for _, rte := range routeCopy {
						if err := onlyError(txn.Update(MethodGet, rte.path, emptyHandler, WithAnnotation("john", "doe"))); err != nil {
							return err
						}
					}
					for _, rte := range routeCopy {
						r := txn.Route(MethodGet, rte.path)
						if !assert.NotNilf(t, r, "missing method=%s;path=%s", http.MethodGet, rte.path) {
							assert.Equal(t, "doe", r.Annotation("john").(string))
						}
					}
				}
				return nil
			}))

			tree := f.getTree()
			assert.Len(t, tree.patterns.statics, 0)
			assert.Len(t, tree.patterns.params, 0)
			assert.Len(t, tree.patterns.wildcards, 0)
			assert.Empty(t, tree.methods)

			// Now let's do it in reverse
			routeCopy = make([]struct{ path string }, len(tc.routes))
			copy(routeCopy, tc.routes)
			require.NoError(t, f.Updates(func(txn *Txn) error {
				for i := len(tc.routes) - 1; i >= 0; i-- {
					if err := onlyError(txn.Add(MethodGet, tc.routes[i].path, emptyHandler)); err != nil {
						return err
					}
				}
				return nil
			}))
			for i := len(tc.routes) - 1; i >= 0; i-- {
				assert.Truef(t, f.Has(MethodGet, tc.routes[i].path), "missing method=%s;path=%s", http.MethodGet, tc.routes[i].path)
			}
			require.NoError(t, f.Updates(func(txn *Txn) error {
				for i := len(tc.routes) - 1; i >= 0; i-- {
					deletedRoute, err := txn.Delete(MethodGet, tc.routes[i].path)
					if err != nil {
						return err
					}
					assert.Equal(t, tc.routes[i].path, deletedRoute.Pattern())
					routeCopy = slices.Delete(routeCopy, len(routeCopy)-1, len(routeCopy))
					assert.Falsef(t, txn.Has(MethodGet, tc.routes[i].path), "found method=%s;path=%s", http.MethodGet, tc.routes[i].path)
					for _, rte := range routeCopy {
						assert.Truef(t, txn.Has(MethodGet, rte.path), "missing method=%s;path=%s", http.MethodGet, rte.path)
					}
				}
				return nil
			}))

			tree = f.getTree()
			assert.Len(t, tree.patterns.statics, 0)
			assert.Len(t, tree.patterns.params, 0)
			assert.Len(t, tree.patterns.wildcards, 0)
			assert.Empty(t, tree.methods)
		})
	}
}

func TestRouter_Add_Conflict(t *testing.T) {
	cases := []struct {
		name      string
		routes    []string
		insert    string
		wantMatch []string
	}{
		{
			name:      "static route already exist",
			routes:    []string{"/foo/bar", "/foo/baz"},
			insert:    "/foo/bar",
			wantMatch: []string{"/foo/bar"},
		},
		{
			name:      "route with same parameters",
			routes:    []string{"/foo/{foo}"},
			insert:    "/foo/{foo}",
			wantMatch: []string{"/foo/{foo}"},
		},
		{
			name:      "route with same wildcard",
			routes:    []string{"/foo/+{foo}"},
			insert:    "/foo/+{foo}",
			wantMatch: []string{"/foo/+{foo}"},
		},
		{
			name:      "route with same parameters but different name",
			routes:    []string{"/foo/{foo}"},
			insert:    "/foo/{bar}",
			wantMatch: []string{"/foo/{foo}"},
		},
		{
			name:      "route with same wildcard but different name",
			routes:    []string{"/foo/+{foo}"},
			insert:    "/foo/+{bar}",
			wantMatch: []string{"/foo/+{foo}"},
		},
		{
			name:      "route with middle same parameters but different name",
			routes:    []string{"/{foo}/bar"},
			insert:    "/{other}/bar",
			wantMatch: []string{"/{foo}/bar"},
		},
		{
			name:      "route with middle same wildcard but different name",
			routes:    []string{"/+{foo}/bar"},
			insert:    "/+{other}/bar",
			wantMatch: []string{"/+{foo}/bar"},
		},
		{
			name:      "route with same regexp parameter",
			routes:    []string{"/foo/{foo:[A-z]+}"},
			insert:    "/foo/{foo:[A-z]+}",
			wantMatch: []string{"/foo/{foo:[A-z]+}"},
		},
		{
			name:      "route with same regexp parameter but different name",
			routes:    []string{"/foo/{foo:[A-z]+}"},
			insert:    "/foo/{bar:[A-z]+}",
			wantMatch: []string{"/foo/{foo:[A-z]+}"},
		},
		{
			name:      "route with same regexp wildcard",
			routes:    []string{"/foo/+{foo:[A-z]+}"},
			insert:    "/foo/+{foo:[A-z]+}",
			wantMatch: []string{"/foo/+{foo:[A-z]+}"},
		},
		{
			name:      "route with same regexp wildcard but different name",
			routes:    []string{"/foo/+{foo:[A-z]+}"},
			insert:    "/foo/+{bar:[A-z]+}",
			wantMatch: []string{"/foo/+{foo:[A-z]+}"},
		},
		{
			name:      "route with middle same regexp parameter but different name",
			routes:    []string{"/{foo:[A-z]+}/bar"},
			insert:    "/{other:[A-z]+}/bar",
			wantMatch: []string{"/{foo:[A-z]+}/bar"},
		},
		{
			name:      "route with middle same regexp wildcard but different name",
			routes:    []string{"/+{foo:[A-z]+}/bar"},
			insert:    "/+{other:[A-z]+}/bar",
			wantMatch: []string{"/+{foo:[A-z]+}/bar"},
		},
		{
			name:      "simple hostname conflict",
			routes:    []string{"a.{b}.c/fox", "{a}.b.c/fox"},
			insert:    "a.{d}.c/fox",
			wantMatch: []string{"a.{b}.c/fox"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithAllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
			}
			got := onlyError(f.Add(MethodGet, tc.insert, emptyHandler))
			var conflict *RouteConflictError
			require.ErrorAs(t, got, &conflict)
			patterns := iterutil.Map(slices.Values(conflict.Conflicts), func(a *Route) string {
				return a.pattern.str
			})
			assert.Equal(t, tc.wantMatch, slices.Collect(patterns))
		})
	}
}

func TestRouter_Add_ConflictOptionalCatchAll(t *testing.T) {
	get := []string{http.MethodGet}
	post := []string{http.MethodPost}

	cases := []struct {
		name         string
		firstMethods []string
		firstPattern string
		firstOpts    []RouteOption
		methods      []string
		pattern      string
		opts         []RouteOption
		wantErr      bool
	}{
		{
			name:         "same methods exact first",
			firstMethods: get,
			firstPattern: "/foo/",
			methods:      get,
			pattern:      "/foo/*{x}",
			wantErr:      true,
		},
		{
			name:         "same methods catch-all first",
			firstMethods: get,
			firstPattern: "/foo/*{x}",
			methods:      get,
			pattern:      "/foo/",
			wantErr:      true,
		},
		{
			name:         "method-less both exact first",
			firstPattern: "/foo/",
			pattern:      "/foo/*{x}",
			wantErr:      true,
		},
		{
			name:         "method-less both catch-all first",
			firstPattern: "/foo/*{x}",
			pattern:      "/foo/",
			wantErr:      true,
		},
		{
			name:         "method-less exact shadows scoped catch-all",
			firstPattern: "/foo/",
			methods:      get,
			pattern:      "/foo/*{x}",
			wantErr:      true,
		},
		{
			name:         "method-less exact shadows scoped catch-all, catch-all first",
			firstMethods: get,
			firstPattern: "/foo/*{x}",
			pattern:      "/foo/",
			wantErr:      true,
		},
		{
			name:         "scoped exact with method-less catch-all",
			firstMethods: get,
			firstPattern: "/foo/",
			pattern:      "/foo/*{x}",
		},
		{
			name:         "scoped exact with method-less catch-all, catch-all first",
			firstPattern: "/foo/*{x}",
			methods:      get,
			pattern:      "/foo/",
		},
		{
			name:         "disjoint methods",
			firstMethods: get,
			firstPattern: "/foo/",
			methods:      post,
			pattern:      "/foo/*{x}",
		},
		{
			name:         "method-less exact with matcher",
			firstPattern: "/foo/",
			firstOpts:    []RouteOption{WithQueryMatcher("mode", "beta")},
			methods:      get,
			pattern:      "/foo/*{x}",
		},
		{
			name:         "method-less matcher-less exact with matched catch-all",
			firstPattern: "/foo/",
			methods:      get,
			pattern:      "/foo/*{x}",
			opts:         []RouteOption{WithQueryMatcher("mode", "beta")},
			wantErr:      true,
		},
		{
			name:         "method-less matcher-less exact with matched catch-all, catch-all first",
			firstMethods: get,
			firstPattern: "/foo/*{x}",
			firstOpts:    []RouteOption{WithQueryMatcher("mode", "beta")},
			pattern:      "/foo/",
			wantErr:      true,
		},
		{
			name:         "different matchers",
			firstPattern: "/foo/",
			firstOpts:    []RouteOption{WithQueryMatcher("mode", "beta")},
			methods:      get,
			pattern:      "/foo/*{x}",
			opts:         []RouteOption{WithQueryMatcher("mode", "live")},
		},
		{
			name:         "equal matchers",
			firstPattern: "/foo/",
			firstOpts:    []RouteOption{WithQueryMatcher("mode", "beta")},
			methods:      get,
			pattern:      "/foo/*{x}",
			opts:         []RouteOption{WithQueryMatcher("mode", "beta")},
			wantErr:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := MustRouter()
			require.NoError(t, onlyError(f.Add(tc.firstMethods, tc.firstPattern, emptyHandler, tc.firstOpts...)))
			err := onlyError(f.Add(tc.methods, tc.pattern, emptyHandler, tc.opts...))
			if tc.wantErr {
				var conflict *RouteConflictError
				require.ErrorAs(t, err, &conflict)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestRouter_Add_Conflict_MultiMethod(t *testing.T) {
	f := MustRouter()
	f.MustAdd(MethodGet, "/hello/{name}", emptyHandler,
		WithSchemeMatcher("https"),
		WithQueryMatcher("foo", "bar"),
	)
	f.MustAdd(MethodPost, "/hello/{name}", emptyHandler,
		WithSchemeMatcher("https"),
		WithQueryMatcher("foo", "bar"),
	)

	got := onlyError(f.Add([]string{http.MethodGet, http.MethodPost}, "/hello/{name}", emptyHandler,
		WithSchemeMatcher("https"),
		WithQueryMatcher("foo", "bar"),
	))
	var conflict *RouteConflictError
	require.ErrorAs(t, got, &conflict)
	require.Len(t, conflict.Conflicts, 2)
	gotMethods := iterutil.Map(slices.Values(conflict.Conflicts), func(r *Route) string {
		return strings.Join(r.methods, ",")
	})
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, slices.Collect(gotMethods))
}

func TestRouter_Update_Conflict(t *testing.T) {
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
			update:  "/foo/+{baz}",
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
			routes:  []string{"/foo/bar", "/foo/+{args}"},
			update:  "/foo/+{all}",
			wantErr: ErrRouteNotFound,
		},
		{
			name:    "replacing non wildcard by wildcard",
			routes:  []string{"/foo/bar", "/foo/"},
			update:  "/foo/+{all}",
			wantErr: ErrRouteNotFound,
		},
		{
			name:    "replacing wildcard by non wildcard",
			routes:  []string{"/foo/bar", "/foo/+{args}"},
			update:  "/foo/",
			wantErr: ErrRouteNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
			}
			r, err := f.Update(MethodGet, tc.update, emptyHandler)
			if err != nil {
				assert.Nil(t, r)
			}
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestRouter_Add_InvalidPattern(t *testing.T) {
	f := MustRouter()
	var pe *PatternError

	// Invalid route on insert
	assert.ErrorIs(t, onlyError(f.Add([]string{"G\x00ET"}, "/foo", emptyHandler)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Add([]string{""}, "/foo", emptyHandler)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Add(MethodGet, "/foo", nil)), ErrInvalidRoute)
	assert.ErrorAs(t, onlyError(f.Add(MethodGet, "/foo\x00", emptyHandler)), &pe)

	// Invalid route on update
	assert.ErrorIs(t, onlyError(f.Update([]string{""}, "/foo", emptyHandler)), ErrInvalidRoute)
	assert.ErrorIs(t, onlyError(f.Update(MethodGet, "/foo", nil)), ErrInvalidRoute)
	assert.ErrorAs(t, onlyError(f.Update(MethodGet, "/foo\x00", emptyHandler)), &pe)
}

func TestRouter_Update(t *testing.T) {
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
			routes: []string{"/foo/+{bar}", "/foo", "/foo/bar"},
			update: "/foo/+{bar}",
		},
		{
			name:   "replacing infix catch all node",
			routes: []string{"/foo/+{bar}/baz", "/foo", "/foo/bar"},
			update: "/foo/+{bar}/baz",
		},
		{
			name:   "replacing infix inflight catch all node",
			routes: []string{"/foo/abc+{bar}/baz", "/foo", "/foo/abc{bar}"},
			update: "/foo/abc+{bar}/baz",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
			}
			assert.NoError(t, onlyError(f.Update(MethodGet, tc.update, emptyHandler)))
		})
	}
}

func TestRouter_Add_MatchersConstraint(t *testing.T) {
	t.Run("insert: enforce max route matchers", func(t *testing.T) {
		f, _ := NewRouter(WithMaxRouteMatchers(3))
		assert.NoError(t, onlyError(f.Add(MethodGet, "/foo", emptyHandler,
			WithQueryMatcher("a", "b"),
			WithQueryMatcher("b", "c"),
		)))

		assert.NoError(t, onlyError(f.Add(MethodGet, "/foo", emptyHandler,
			WithQueryMatcher("a", "b"),
			WithQueryMatcher("b", "c"),
			WithQueryMatcher("d", "e"),
		)))

		assert.ErrorIs(t, onlyError(f.Add(MethodGet, "/foo", emptyHandler,
			WithQueryMatcher("a", "b"),
			WithQueryMatcher("b", "c"),
			WithQueryMatcher("d", "e"),
			WithQueryMatcher("f", "g"),
		)), ErrInvalidRoute)
	})
	t.Run("update: enforce max route matchers", func(t *testing.T) {
		f, _ := NewRouter(WithMaxRouteMatchers(3))
		f.MustAdd(MethodGet, "/foo", emptyHandler,
			WithQueryMatcher("a", "b"),
			WithQueryMatcher("b", "c"),
			WithQueryMatcher("d", "e"),
		)

		assert.ErrorIs(t, onlyError(f.Update(MethodGet, "/foo", emptyHandler,
			WithQueryMatcher("a", "b"),
			WithQueryMatcher("b", "c"),
			WithQueryMatcher("d", "e"),
			WithQueryMatcher("f", "g"),
		)), ErrInvalidRoute)
	})
	t.Run("insert: no priority or zero priority without matcher", func(t *testing.T) {
		f, _ := NewRouter()
		assert.NoError(t, onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithMatchersPriority(0))))
		assert.ErrorIs(t, onlyError(f.Add(MethodGet, "/foo", emptyHandler, WithMatchersPriority(1))), ErrInvalidRoute)
	})
	t.Run("update: no priority or zero priority without matcher", func(t *testing.T) {
		f, _ := NewRouter()
		assert.NoError(t, onlyError(f.Add(MethodGet, "/foo", emptyHandler)))
		assert.NoError(t, onlyError(f.Update(MethodGet, "/foo", emptyHandler, WithMatchersPriority(0))))
		assert.ErrorIs(t, onlyError(f.Update(MethodGet, "/foo", emptyHandler, WithMatchersPriority(1))), ErrInvalidRoute)
	})
}

func TestRouter_ServeHTTP_IgnoreTrailingSlash(t *testing.T) {
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
		{
			name:     "no relaxed match on pattern ending with double slash",
			paths:    []string{"/foo//"},
			req:      "/foo/",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "no relaxed match on request ending with double slash and standalone slash node",
			paths:    []string{"/foo/", "/foo//bar", "/foo//qux"},
			req:      "/foo//",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "relaxed match on single slash boundary within pattern with infix double slash",
			paths:    []string{"/foo//bar/"},
			req:      "/foo//bar",
			method:   http.MethodGet,
			wantCode: http.StatusOK,
			wantPath: "/foo//bar/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithTrailingSlash(RelaxedSlash))
			rf := f.Info()
			assert.Equal(t, RelaxedSlash, rf.TrailingSlashOption)
			for _, path := range tc.paths {
				require.NoError(t, onlyError(f.Add([]string{tc.method}, path, func(c *Context) {
					_ = c.String(http.StatusOK, c.Pattern())
				})))
				rte := f.Route([]string{tc.method}, path)
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

func TestRouter_ServeHTTP_RedirectTrailingSlash(t *testing.T) {

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
		{
			name:     "no redirect on pattern ending with double slash",
			paths:    []string{"/foo//"},
			req:      "/foo/",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "no redirect on pattern ending with double slash and sibling",
			paths:    []string{"/foo//", "/foo/bar"},
			req:      "/foo/",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "no redirect on request ending with double slash and standalone slash node",
			paths:    []string{"/foo/", "/foo//bar", "/foo//qux"},
			req:      "/foo//",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "no redirect on hostname route with pattern ending with double slash",
			paths:    []string{"ex.com/foo//"},
			req:      "http://ex.com/foo/",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:         "redirect on single slash boundary within pattern with infix double slash",
			paths:        []string{"/foo//bar/"},
			req:          "/foo//bar",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo//bar/",
		},
		{
			name:     "no redirect on path with dot segment",
			paths:    []string{"/+{args}/"},
			req:      "/a/..",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "no redirect on path with single dot segment",
			paths:    []string{"/{a}"},
			req:      "/../",
			method:   http.MethodGet,
			wantCode: http.StatusNotFound,
		},
		{
			name:         "redirect on path with dot segment lookalike",
			paths:        []string{"/+{args}/"},
			req:          "/a/.b",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/a/.b/",
		},
		{
			name:         "hostname tsr wins over path-only direct match",
			paths:        []string{"ex.com/foo/", "/foo"},
			req:          "http://ex.com/foo",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := NewRouter(WithTrailingSlash(RedirectSlash))
			rf := f.Info()
			assert.Equal(t, RedirectSlash, rf.TrailingSlashOption)

			for _, path := range tc.paths {
				require.NoError(t, onlyError(f.Add([]string{tc.method}, path, emptyHandler)))
				rte := f.Route([]string{tc.method}, path)
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

			t.Run("with any", func(t *testing.T) {
				f := MustRouter(WithTrailingSlash(RedirectSlash))

				for _, path := range tc.paths {
					require.NoError(t, onlyError(f.Add(MethodAny, path, emptyHandler)))
					rte := f.Route(MethodAny, path)
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
		})
	}
}

func TestRouter_ServeHTTP_RedirectPath(t *testing.T) {
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
			slashMode:    ExactSlash,
			req:          "/foo//bar",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar",
		},
		{
			name:         "redirect parent dir reference",
			path:         "/bar",
			slashMode:    ExactSlash,
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
			slashMode: ExactSlash,
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
			slashMode:    ExactSlash,
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
			slashMode:    ExactSlash,
			req:          "/foo//bar?1=2",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar?1=2",
		},
		{
			name:         "consecutive slash with encoded space in segment",
			path:         "/foo/{bar}",
			slashMode:    ExactSlash,
			req:          "/foo//baz%20qux",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/baz%20qux",
		},
		{
			name:         "consecutive slash with lowercase hex in segment",
			path:         "/foo/{bar}",
			slashMode:    ExactSlash,
			req:          "/foo//baz%2fqux",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/baz%2Fqux",
		},
		{
			name:         "consecutive slash with encoded sub-delim in segment",
			path:         "/foo/{bar}",
			slashMode:    ExactSlash,
			req:          "/foo//baz%21qux",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/baz%21qux",
		},
		{
			name:      "no redirect when collapsed path does not match wildcard",
			path:      "/files/+{path}",
			slashMode: ExactSlash,
			req:       "/files/a/../../etc/passwd",
			method:    http.MethodGet,
			wantCode:  http.StatusNotFound,
		},
		{
			name:         "redirect with consecutive slash and wildcard",
			path:         "/files/+{path}",
			slashMode:    ExactSlash,
			req:          "/files//secret",
			method:       http.MethodGet,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/files/secret",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := MustRouter(WithMergeSlashes(RedirectPath), WithCollapseDotSegments(RedirectPath), WithTrailingSlash(tc.slashMode))
			rf := f.Info()
			assert.Equal(t, RedirectPath, rf.MergeSlashes)
			assert.Equal(t, RedirectPath, rf.CollapseDotSegments)

			require.NoError(t, onlyError(f.Add([]string{tc.method}, tc.path, emptyHandler)))

			req := httptest.NewRequest(tc.method, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			if w.Code == http.StatusPermanentRedirect || w.Code == http.StatusMovedPermanently {
				assert.Equal(t, tc.wantLocation, w.Header().Get(HeaderLocation))
			}

			t.Run("with any", func(t *testing.T) {
				f := MustRouter(WithMergeSlashes(RedirectPath), WithCollapseDotSegments(RedirectPath), WithTrailingSlash(tc.slashMode))

				require.NoError(t, onlyError(f.Add(MethodAny, tc.path, emptyHandler)))

				req := httptest.NewRequest(tc.method, tc.req, nil)
				w := httptest.NewRecorder()
				f.ServeHTTP(w, req)
				assert.Equal(t, tc.wantCode, w.Code)
				if w.Code == http.StatusPermanentRedirect || w.Code == http.StatusMovedPermanently {
					assert.Equal(t, tc.wantLocation, w.Header().Get(HeaderLocation))
				}
			})
		})
	}

}

func TestRouter_NormalizePathTrailingSlash(t *testing.T) {
	cases := []struct {
		name         string
		slashMode    TrailingSlashOption
		wantCode     int
		wantLocation string
	}{
		{
			name:      "relaxed slash serves normalized path",
			slashMode: RelaxedSlash,
			wantCode:  http.StatusOK,
		},
		{
			name:      "exact slash does not match normalized path",
			slashMode: ExactSlash,
			wantCode:  http.StatusNotFound,
		},
		{
			name:         "redirect slash fixes normalized path",
			slashMode:    RedirectSlash,
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/bar",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := MustRouter(WithMergeSlashes(NormalizePath), WithTrailingSlash(tc.slashMode))
			require.NoError(t, onlyError(f.Add(MethodGet, "/foo/bar", emptyHandler)))

			req := httptest.NewRequest(http.MethodGet, "/foo//bar/", nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantLocation, w.Header().Get(HeaderLocation))
		})
	}
}

func TestRouter_ServeHTTP_NormalizeRewriteURL(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		req         string
		slashMode   TrailingSlashOption
		fixedMode   NormalizeOption
		wantPath    string
		wantRawPath string
	}{
		{
			name:      "trim trailing slash",
			path:      "/foo",
			req:       "/foo/",
			slashMode: RelaxedSlash,
			wantPath:  "/foo",
		},
		{
			name:      "append trailing slash",
			path:      "/foo/",
			req:       "/foo",
			slashMode: RelaxedSlash,
			wantPath:  "/foo/",
		},
		{
			name:      "clean consecutive slash",
			path:      "/foo/bar",
			req:       "/foo//bar",
			fixedMode: NormalizePath,
			wantPath:  "/foo/bar",
		},
		{
			name:      "clean encoded dot segments",
			path:      "/foo/bar",
			req:       "/baz/%2E%2E/foo/bar",
			fixedMode: NormalizePath,
			wantPath:  "/foo/bar",
		},
		{
			name:      "clean path and trim trailing slash",
			path:      "/foo",
			req:       "//foo/",
			slashMode: RelaxedSlash,
			fixedMode: NormalizePath,
			wantPath:  "/foo",
		},
		{
			name:      "decode unreserved characters",
			path:      "/foo",
			req:       "/f%6Fo/",
			slashMode: RelaxedSlash,
			wantPath:  "/foo",
		},
		{
			name:        "preserve encoded slash",
			path:        "/x/{p}",
			req:         "/x/a%2fb/",
			slashMode:   RelaxedSlash,
			wantPath:    "/x/a/b",
			wantRawPath: "/x/a%2Fb",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := MustRouter(WithMergeSlashes(tc.fixedMode), WithCollapseDotSegments(tc.fixedMode), WithTrailingSlash(tc.slashMode))

			wantEscaped := tc.wantRawPath
			if wantEscaped == "" {
				wantEscaped = tc.wantPath
			}

			require.NoError(t, onlyError(f.Add(MethodGet, tc.path, func(c *Context) {
				assert.Equal(t, tc.wantPath, c.Request().URL.Path)
				assert.Equal(t, tc.wantRawPath, c.Request().URL.RawPath)
				assert.Equal(t, wantEscaped, c.Request().URL.EscapedPath())
				assert.Equal(t, wantEscaped, c.RoutingPath())
				c.Writer().WriteHeader(http.StatusOK)
			})))

			req := httptest.NewRequest(http.MethodGet, tc.req, nil)
			originalPath, originalRawPath := req.URL.Path, req.URL.RawPath
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tc.req, req.RequestURI)
			assert.Equal(t, originalPath, req.URL.Path)
			assert.Equal(t, originalRawPath, req.URL.RawPath)
		})
	}

	t.Run("caller url untouched on panic", func(t *testing.T) {
		f := MustRouter(WithTrailingSlash(RelaxedSlash))
		require.NoError(t, onlyError(f.Add(MethodGet, "/foo", func(c *Context) {
			panic("boom")
		})))

		req := httptest.NewRequest(http.MethodGet, "/foo/", nil)
		w := httptest.NewRecorder()
		assert.Panics(t, func() { f.ServeHTTP(w, req) })
		assert.Equal(t, "/foo/", req.URL.Path)
		assert.Empty(t, req.URL.RawPath)
	})
}

func TestRouter_ServeHTTP_ConsecutiveSlash(t *testing.T) {
	cases := []struct {
		name     string
		paths    []string
		req      string
		wantCode int
		wantPath string
	}{
		{
			name:     "double slash and single slash coexist",
			paths:    []string{"/a//b", "/a/b"},
			req:      "/a//b",
			wantCode: http.StatusOK,
			wantPath: "/a//b",
		},
		{
			name:     "single slash not shadowed by double slash",
			paths:    []string{"/a//b", "/a/b"},
			req:      "/a/b",
			wantCode: http.StatusOK,
			wantPath: "/a/b",
		},
		{
			name:     "triple slash does not match double slash",
			paths:    []string{"/a//b"},
			req:      "/a///b",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "param after empty segment",
			paths:    []string{"/a//{p}"},
			req:      "/a//x",
			wantCode: http.StatusOK,
			wantPath: "/a//{p}",
		},
		{
			name:     "param after empty segment does not match single slash",
			paths:    []string{"/a//{p}"},
			req:      "/a/x",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "hostname route with double slash path",
			paths:    []string{"ex.com//foo"},
			req:      "http://ex.com//foo",
			wantCode: http.StatusOK,
			wantPath: "ex.com//foo",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := MustRouter()
			for _, path := range tc.paths {
				require.NoError(t, onlyError(f.Add(MethodGet, path, func(c *Context) {
					_ = c.String(http.StatusOK, c.Pattern())
				})))
			}

			req := httptest.NewRequest(http.MethodGet, tc.req, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			if tc.wantPath != "" {
				assert.Equal(t, tc.wantPath, w.Body.String())
			}
		})
	}
}

func TestRouter_ServeHTTP_EncodedRedirectTrailingSlash(t *testing.T) {
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
			path:         "/+{any}/",
			req:          "//evil.com",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/%2Fevil.com/",
		},
		{
			name:         "open redirect with backslash",
			path:         "/+{any}/",
			req:          "/\\evil.com",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/%5Cevil.com/",
		},
		{
			name:         "encoded space normalized in redirect",
			path:         "/foo/{bar}/",
			req:          "/foo/baz%20qux",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/baz%20qux/",
		},
		{
			name:         "encoded sub-delim preserved in redirect",
			path:         "/foo/{bar}/",
			req:          "/foo/baz%21qux",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/baz%21qux/",
		},
		{
			name:         "encoded non-ascii preserved in redirect",
			path:         "/foo/{bar}/",
			req:          "/foo/baz%C3%A9qux",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/baz%C3%A9qux/",
		},
		{
			name:         "lowercase hex normalized to uppercase in redirect",
			path:         "/foo/{bar}/",
			req:          "/foo/baz%2fqux",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/baz%2Fqux/",
		},
		{
			name:         "encoded crlf preserved encoded in redirect",
			path:         "/foo/{bar}/",
			req:          "/foo/baz%0D%0Aqux",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/baz%0D%0Aqux/",
		},
		{
			name:         "encoded unreserved decoded in redirect",
			path:         "/foo/{bar}/",
			req:          "/foo/b%61z",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/baz/",
		},
		{
			name:         "encoded tilde decoded in redirect",
			path:         "/foo/{bar}/",
			req:          "/foo/%7Euser",
			wantCode:     http.StatusMovedPermanently,
			wantLocation: "/foo/~user/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := NewRouter(WithTrailingSlash(RedirectSlash))
			require.NoError(t, onlyError(r.Add(MethodGet, tc.path, emptyHandler)))

			req := httptest.NewRequest(http.MethodGet, tc.req, nil)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantLocation, w.Header().Get(HeaderLocation))
		})
	}
}

func TestRouter_ServeHTTP_TsrParams(t *testing.T) {
	cases := []struct {
		name       string
		routes     []string
		target     string
		wantParams Params
		wantPath   string
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
		},
		{
			name:       "current not a leaf, with most specifc tsr",
			routes:     []string{"/a/foo/", "/a/foobar", "/{a}/foo"},
			target:     "/a/foo",
			wantParams: Params(nil),
			wantPath:   "/a/foo/",
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
		},
		{
			name:       "current not a leaf, should empty params",
			routes:     []string{"/{a}", "/foo", "/foo/x/", "/foo/y/"},
			target:     "/foo/",
			wantParams: Params(nil),
			wantPath:   "/foo",
		},
		{
			name:   "tsr with empty catch all",
			routes: []string{"/a/foo/*{any}", "/{a}/foo/y", "/{a}/foo/b"},
			target: "/a/foo",
			wantParams: Params{
				{
					Key:   "any",
					Value: "",
				},
			},
			wantPath: "/a/foo/*{any}",
		},
		{
			name:   "tsr with empty catch all behind a split static node",
			routes: []string{"/a/foo/*{any}", "/a/foobar", "/{a}/foo/y", "/{a}/foo/b"},
			target: "/a/foo",
			wantParams: Params{
				{
					Key:   "any",
					Value: "",
				},
			},
			wantPath: "/a/foo/*{any}",
		},
		{
			name:   "tsr with empty catch all and param before",
			routes: []string{"/{a}/foo/*{any}", "/{a}/foo/y", "/{a}/foo/b"},
			target: "/a/foo",
			wantParams: Params{
				{
					Key:   "a",
					Value: "a",
				},
				{
					Key:   "any",
					Value: "",
				},
			},
			wantPath: "/{a}/foo/*{any}",
		},
		{
			name:   "tsr with infix wildcard",
			routes: []string{"/+{args}/"},
			target: "/a/b/c",
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
			wantPath: "/+{args}/",
		},
		{
			name:   "tsr with infix wildcard after param backtrack",
			routes: []string{"/+{args}/", "/{a}/b"},
			target: "/x/c",
			wantParams: Params{
				{
					Key:   "args",
					Value: "x/c",
				},
			},
			wantPath: "/+{args}/",
		},
		{
			name:   "no tsr when a suffix catch-all direct matches",
			routes: []string{"/+{args}/", "/+{args}"},
			target: "/a/b/c",
			wantParams: Params{
				{
					Key:   "args",
					Value: "a/b/c",
				},
			},
			wantPath: "/+{args}",
		},
		{
			name:   "no tsr when a suffix regex catch-all direct matches",
			routes: []string{"/+{w:[a-z/]+}", "/+{w:[a-z/]+}/x"},
			target: "/abc/",
			wantParams: Params{
				{
					Key:   "w",
					Value: "abc/",
				},
			},
			wantPath: "/+{w:[a-z/]+}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := MustRouter(WithTrailingSlash(RelaxedSlash), WithAllowRegexpParam(true))
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, func(c *Context) {
					assert.Equal(t, tc.wantPath, c.Pattern())
					var params Params = slices.Collect(c.Params())
					assert.Equal(t, tc.wantParams, params)
				})))
			}
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)

			t.Run("with any", func(t *testing.T) {
				f := MustRouter(WithTrailingSlash(RelaxedSlash), WithAllowRegexpParam(true))
				for _, rte := range tc.routes {
					require.NoError(t, onlyError(f.Add(MethodAny, rte, func(c *Context) {
						assert.Equal(t, tc.wantPath, c.Pattern())
						var params Params = slices.Collect(c.Params())
						assert.Equal(t, tc.wantParams, params)
					})))
				}
				req := httptest.NewRequest(http.MethodGet, tc.target, nil)
				w := httptest.NewRecorder()
				f.ServeHTTP(w, req)
				assert.Equal(t, http.StatusOK, w.Code)
			})
		})
	}
}

func TestRouter_DeleteError(t *testing.T) {
	f, _ := NewRouter()
	require.NoError(t, onlyError(f.Add(MethodGet, "/foo/bar", emptyHandler)))
	t.Run("delete with empty method", func(t *testing.T) {
		r, err := f.Delete([]string{""}, "/foo/bar")
		assert.ErrorIs(t, err, ErrInvalidRoute)
		assert.Nil(t, r)
	})
	t.Run("delete invalid route", func(t *testing.T) {
		r, err := f.Delete(MethodGet, "/{")
		var pe *PatternError
		assert.ErrorAs(t, err, &pe)
		assert.Nil(t, r)
	})
	t.Run("route does not exist", func(t *testing.T) {
		r, err := f.Delete(MethodGet, "/foo/bar/")
		assert.ErrorIs(t, err, ErrRouteNotFound)
		assert.Nil(t, r)
	})
	t.Run("method does not exist", func(t *testing.T) {
		r, err := f.Delete(MethodTrace, "/foo/bar")
		assert.ErrorIs(t, err, ErrRouteNotFound)
		assert.Nil(t, r)
	})
}

func TestRouter_UpdatesError(t *testing.T) {
	f, _ := NewRouter()
	wantErr := errors.New("error")
	err := f.Updates(func(txn *Txn) error {
		for _, rte := range staticRoutes {
			if err := onlyError(txn.Add([]string{rte.method}, rte.path, emptyHandler)); err != nil {
				return err
			}
		}
		return wantErr
	})
	assert.ErrorIs(t, err, wantErr)
	tree := f.getTree()
	assert.Len(t, tree.patterns.statics, 0)
	assert.Len(t, tree.patterns.params, 0)
	assert.Len(t, tree.patterns.wildcards, 0)
	assert.Empty(t, tree.methods)
}

func TestRouter_UpdatesPanic(t *testing.T) {
	f, _ := NewRouter()

	assert.Panics(t, func() {
		_ = f.Updates(func(txn *Txn) error {
			for _, rte := range staticRoutes {
				if err := onlyError(txn.Add([]string{rte.method}, rte.path, emptyHandler)); err != nil {
					return err
				}
			}
			panic("panic")
		})
	})

	tree := f.getTree()
	assert.Len(t, tree.patterns.statics, 0)
	assert.Len(t, tree.patterns.params, 0)
	assert.Len(t, tree.patterns.wildcards, 0)
	assert.Empty(t, tree.methods)
}

func TestRouter_Update_Middleware(t *testing.T) {
	called := false
	m := MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			called = true
			next(c)
		}
	})
	f, _ := NewRouter(WithMiddleware(Recovery(slog.DiscardHandler)))
	f.MustAdd(MethodGet, "/foo", emptyHandler)
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	w := httptest.NewRecorder()

	// Add middleware
	require.NoError(t, onlyError(f.Update(MethodGet, "/foo", emptyHandler, WithMiddleware(m))))
	f.ServeHTTP(w, req)
	assert.True(t, called)
	called = false

	rte := f.Route(MethodGet, "/foo")
	rte.Handle(newTestContext(f))
	assert.False(t, called)
	called = false

	rte.HandleMiddleware(newTestContext(f))
	assert.True(t, called)
	called = false

	// Remove middleware
	require.NoError(t, onlyError(f.Update(MethodGet, "/foo", emptyHandler)))
	f.ServeHTTP(w, req)
	assert.False(t, called)
	called = false

	rte = f.Route(MethodGet, "/foo")
	rte.Handle(newTestContext(f))
	assert.False(t, called)
	called = false

	rte = f.Route(MethodGet, "/foo")
	rte.HandleMiddleware(newTestContext(f))
	assert.False(t, called)
}

func TestRouter_Lookup(t *testing.T) {
	rx := regexp.MustCompile("({|\\+{)[A-z]+[}]")
	f, _ := NewRouter()
	for _, rte := range githubAPI {
		require.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler)))
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
			if strings.HasPrefix(match, "+") {
				key = match[2 : len(match)-1]
			} else {
				key = match[1 : len(match)-1]
			}
			value := match
			ps, _ := url.PathUnescape(cc.Param(key))
			assert.Equal(t, value, ps)
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
		f, _ := NewRouter()
		for _, rte := range staticRoutes {
			require.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler)))
		}
		for _, rte := range staticRoutes {
			req := httptest.NewRequest(rte.method, rte.path, nil)
			route, tsr := f.Match(rte.method, req)
			assert.False(t, tsr)
			require.NotNil(t, route)
			assert.Equal(t, rte.path, route.Pattern())
		}
	})

	t.Run("reverse with tsr", func(t *testing.T) {
		f, _ := NewRouter(WithTrailingSlash(RelaxedSlash))
		for _, rte := range staticRoutes {
			if rte.path == "/" {
				continue
			}
			require.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path+"/", emptyHandler)))
		}
		for _, rte := range staticRoutes {
			if rte.path == "/" {
				continue
			}
			req := httptest.NewRequest(rte.method, rte.path, nil)
			route, tsr := f.Match(rte.method, req)
			require.True(t, tsr)
			assert.Equal(t, rte.path+"/", route.Pattern())
		}
	})

	t.Run("reverse no tsr", func(t *testing.T) {
		f, _ := NewRouter()
		for _, rte := range staticRoutes {
			require.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler)))
		}
		for _, rte := range staticRoutes {
			req := httptest.NewRequest(rte.method, rte.path, nil)
			route, tsr := f.Match(rte.method, req)
			assert.False(t, tsr)
			require.NotNil(t, route)
			assert.Equal(t, rte.path, route.Pattern())
		}
	})

	t.Run("reverse with hostname", func(t *testing.T) {
		f, _ := NewRouter()
		route, err := f.Add(MethodGet, "{sub}.example.com/foo", emptyHandler)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/foo", nil)
		req.Host = "foo.example.com"
		got, tsr := f.Match(req.Method, req)
		assert.False(t, tsr)
		require.NotNil(t, route)
		assert.Equal(t, route, got)
	})

	t.Run("reverse with hostname (case-insensitive)", func(t *testing.T) {
		f, _ := NewRouter()
		route, err := f.Add(MethodGet, "{sub}.example.com/foo", emptyHandler)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/foo", nil)
		req.Host = "FOO.EXAMPLE.COM"
		got, tsr := f.Match(req.Method, req)
		assert.False(t, tsr)
		require.NotNil(t, route)
		assert.Equal(t, route, got)
	})
}

func TestRouter_Has(t *testing.T) {
	routes := []string{
		"/foo/bar",
		"/welcome/{name}",
		"/welcome/+{name}",
		"/welcome/{name}/ch",
		"/welcome/+{name}/fr",
		"/welcome/{name:[A-z]+}",
		"/welcome/+{name:[A-z]+}",
		"/welcome/{name:[A-z]+}/ch",
		"/welcome/+{name:[A-z]+}/fr",
		"/users/uid_{id}",
		"/users/uid_{id}/ch",
		"/users/uid_{id:[A-z]+}",
		"/users/uid_{id:[A-z]+}/ch",
		"/john/doe/",
		"/foo/*{name}",
		"/foo/uid_*{id}",
		"/a%2Fb",
		"/glob/*0{id}",
		"/glob/+a{id}",
	}

	f, _ := NewRouter(WithAllowRegexpParam(true))
	for _, rte := range routes {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
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
			name: "strict match route regexp params",
			path: "/welcome/{name:[A-z]+}",
			want: true,
		},
		{
			name: "strict match route wildcard",
			path: "/welcome/+{name}",
			want: true,
		},
		{
			name: "strict match route regexp wildcard",
			path: "/welcome/+{name:[A-z]+}",
			want: true,
		},
		{
			name: "strict match infix params",
			path: "/welcome/{name}/ch",
			want: true,
		},
		{
			name: "strict match infix regexp params",
			path: "/welcome/{name:[A-z]+}/ch",
			want: true,
		},
		{
			name: "strict match infix wildcard",
			path: "/welcome/+{name}/fr",
			want: true,
		},
		{
			name: "strict match infix regexp wildcard",
			path: "/welcome/+{name:[A-z]+}/fr",
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
			name: "strict match mid route regexp params",
			path: "/users/uid_{id:[A-z]+}",
			want: true,
		},
		{
			name: "strict match mid route infix params",
			path: "/users/uid_{id}/ch",
			want: true,
		},
		{
			name: "strict match mid route infix regexp params",
			path: "/users/uid_{id:[A-z]+}/ch",
			want: true,
		},
		{
			name: "no match mid route params",
			path: "/users/uid_123",
		},
		{
			name: "strict match encoded route",
			path: "/a%2Fb",
			want: true,
		},
		{
			name: "no match lowercase hex",
			path: "/a%2fb",
		},
		{
			name: "no match malformed escape",
			path: "/a%2%46b",
		},
		{
			name: "strict match literal star before brace segment",
			path: "/glob/*0{id}",
			want: true,
		},
		{
			name: "no match literal star before brace segment",
			path: "/glob/*1{id}",
		},
		{
			name: "strict match literal plus before brace segment",
			path: "/glob/+a{id}",
			want: true,
		},
		{
			name: "no match literal plus before brace segment",
			path: "/glob/+b{id}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, f.Has(MethodGet, tc.path))
		})
	}
}

func TestRouter_HasWithMatchers(t *testing.T) {
	f, _ := NewRouter(WithAllowRegexpParam(true))

	m1, _ := MatchQuery("version", "v1")
	m2, _ := MatchQuery("version", "v2")
	m3, _ := MatchHeader("X-Api-Key", "secret")

	require.NoError(t, onlyError(f.Add(MethodGet, "/api/users", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodGet, "/api/users", emptyHandler, WithMatcher(m1))))
	require.NoError(t, onlyError(f.Add(MethodGet, "/api/users", emptyHandler, WithMatcher(m2))))
	require.NoError(t, onlyError(f.Add(MethodGet, "/api/users", emptyHandler, WithMatcher(m1, m3))))
	require.NoError(t, onlyError(f.Add(MethodGet, "/api/users/{id}", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodGet, "/api/users/{id}", emptyHandler, WithMatcher(m1))))
	require.NoError(t, onlyError(f.Add(MethodGet, "/files/+{path}", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodGet, "/files/+{path}", emptyHandler, WithMatcher(m1))))
	require.NoError(t, onlyError(f.Add(MethodGet, "/items/{id:[0-9]+}", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodGet, "/items/{id:[0-9]+}", emptyHandler, WithMatcher(m1))))
	require.NoError(t, onlyError(f.Add(MethodGet, "/org/{org}/repo/{repo:[a-z]+}", emptyHandler, WithMatcher(m1))))

	cases := []struct {
		name     string
		path     string
		matchers []Matcher
		want     bool
	}{
		{
			name: "static route without matcher",
			path: "/api/users",
			want: true,
		},
		{
			name:     "static route with matching matcher",
			path:     "/api/users",
			matchers: []Matcher{m1},
			want:     true,
		},
		{
			name:     "static route with different matcher value",
			path:     "/api/users",
			matchers: []Matcher{m2},
			want:     true,
		},
		{
			name:     "static route with multiple matchers",
			path:     "/api/users",
			matchers: []Matcher{m1, m3},
			want:     true,
		},
		{
			name:     "static route with multiple matchers in different order",
			path:     "/api/users",
			matchers: []Matcher{m3, m1},
			want:     true,
		},
		{
			name:     "static route with non-registered matcher",
			path:     "/api/users",
			matchers: []Matcher{m3},
			want:     false,
		},
		{
			name:     "static route with partial matchers",
			path:     "/api/users",
			matchers: []Matcher{m1, m2},
			want:     false,
		},
		{
			name: "param route without matcher",
			path: "/api/users/{id}",
			want: true,
		},
		{
			name:     "param route with matcher",
			path:     "/api/users/{id}",
			matchers: []Matcher{m1},
			want:     true,
		},
		{
			name:     "param route with wrong matcher",
			path:     "/api/users/{id}",
			matchers: []Matcher{m2},
			want:     false,
		},
		{
			name: "wildcard route without matcher",
			path: "/files/+{path}",
			want: true,
		},
		{
			name:     "wildcard route with matcher",
			path:     "/files/+{path}",
			matchers: []Matcher{m1},
			want:     true,
		},
		{
			name: "regexp route without matcher",
			path: "/items/{id:[0-9]+}",
			want: true,
		},
		{
			name:     "regexp route with matcher",
			path:     "/items/{id:[0-9]+}",
			matchers: []Matcher{m1},
			want:     true,
		},
		{
			name:     "mixed route with param and regexp",
			path:     "/org/{org}/repo/{repo:[a-z]+}",
			matchers: []Matcher{m1},
			want:     true,
		},
		{
			name: "mixed route without matcher does not exist",
			path: "/org/{org}/repo/{repo:[a-z]+}",
			want: false,
		},
		{
			name: "structurally identical param pattern with different name",
			path: "/api/users/{name}",
			want: false,
		},
		{
			name:     "structurally identical param pattern with different name and matcher",
			path:     "/api/users/{name}",
			matchers: []Matcher{m1},
			want:     false,
		},
		{
			name: "structurally identical regexp pattern with different name",
			path: "/items/{num:[0-9]+}",
			want: false,
		},
		{
			name:     "structurally identical regexp pattern with different name and matcher",
			path:     "/items/{num:[0-9]+}",
			matchers: []Matcher{m1},
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, f.Has(MethodGet, tc.path, tc.matchers...))
		})
	}
}

func TestRouter_Match_Reverse(t *testing.T) {
	routes := []string{
		"/foo/bar",
		"/welcome/{name}",
		"/users/uid_{id}",
	}

	f, _ := NewRouter(WithTrailingSlash(RelaxedSlash))
	for _, rte := range routes {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
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
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			route, tsr := f.Match(req.Method, req)
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

func TestRouter_ReverseWithIgnoreTrailingSlashEnable(t *testing.T) {
	routes := []string{
		"/foo/bar",
		"/welcome/{name}/",
		"/users/uid_{id}",
	}

	f, _ := NewRouter(WithTrailingSlash(RelaxedSlash))
	for _, rte := range routes {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
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
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			route, tsr := f.Match(req.Method, req)
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

func TestRouter_ServeHTTP_EncodedPath(t *testing.T) {
	encodedPath := "run/cmd/S123L%2FA"
	req := httptest.NewRequest(http.MethodGet, "/"+encodedPath, nil)
	w := httptest.NewRecorder()

	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/+{request}", func(c *Context) {
		_ = c.String(http.StatusOK, c.Param("request"))
	})

	f.ServeHTTP(w, req)
	assert.Equal(t, encodedPath, w.Body.String())
}

func TestRouter_ServeHTTP_UnreservedDecoding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		routes      []string
		target      string
		wantCode    int
		wantPattern string
		wantParams  Params
	}{
		{
			name:        "encoded request matches decoded route",
			routes:      []string{"/users/john"},
			target:      "/users/j%6Fhn",
			wantCode:    http.StatusOK,
			wantPattern: "/users/john",
		},
		{
			name:        "raw utf8 request matches encoded route",
			routes:      []string{"/caf%C3%A9"},
			target:      "https://example.com/café",
			wantCode:    http.StatusOK,
			wantPattern: "/caf%C3%A9",
		},
		{
			name:        "param captures decoded unreserved",
			routes:      []string{"/users/{id}"},
			target:      "/users/j%6Fhn",
			wantCode:    http.StatusOK,
			wantPattern: "/users/{id}",
			wantParams:  Params{{Key: "id", Value: "john"}},
		},
		{
			name:        "param keeps reserved encoded",
			routes:      []string{"/users/{id}"},
			target:      "/users/j%C3%A9r%C3%B4me",
			wantCode:    http.StatusOK,
			wantPattern: "/users/{id}",
			wantParams:  Params{{Key: "id", Value: "j%C3%A9r%C3%B4me"}},
		},
		{
			name:        "wildcard captures mixed decoded and encoded",
			routes:      []string{"/files/+{p}"},
			target:      "/files/a%2Fb/c%61t",
			wantCode:    http.StatusOK,
			wantPattern: "/files/+{p}",
			wantParams:  Params{{Key: "p", Value: "a%2Fb/cat"}},
		},
		{
			name:        "regex evaluates decoded segment",
			routes:      []string{"/n/{d:[0-9]+}"},
			target:      "/n/%31%32",
			wantCode:    http.StatusOK,
			wantPattern: "/n/{d:[0-9]+}",
			wantParams:  Params{{Key: "d", Value: "12"}},
		},
		{
			name:     "encoded slash stays distinct",
			routes:   []string{"/a/b"},
			target:   "/a%2Fb",
			wantCode: http.StatusNotFound,
		},
		{
			name:        "literal plus matches raw plus",
			routes:      []string{"/emails/a+b"},
			target:      "/emails/a+b",
			wantCode:    http.StatusOK,
			wantPattern: "/emails/a+b",
		},
		{
			name:     "encoded plus does not match literal plus",
			routes:   []string{"/emails/a+b"},
			target:   "/emails/a%2Bb",
			wantCode: http.StatusNotFound,
		},
		{
			name:        "encoded plus route matches encoded plus request",
			routes:      []string{"/emails/a%2Bb"},
			target:      "/emails/a%2Bb",
			wantCode:    http.StatusOK,
			wantPattern: "/emails/a%2Bb",
		},
		{
			name:        "literal star matches raw star",
			routes:      []string{"/glob/*"},
			target:      "/glob/*",
			wantCode:    http.StatusOK,
			wantPattern: "/glob/*",
		},
		{
			name:     "raw star does not match encoded star route",
			routes:   []string{"/star/%2A"},
			target:   "/star/*",
			wantCode: http.StatusNotFound,
		},
		{
			name:        "encoded brace route matches raw brace request",
			routes:      []string{"/brace/%7Bid%7D"},
			target:      "/brace/{id}",
			wantCode:    http.StatusOK,
			wantPattern: "/brace/%7Bid%7D",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := NewRouter(WithAllowRegexpParam(true))
			require.NoError(t, err)

			var gotParams Params
			h := func(c *Context) {
				gotParams = slices.AppendSeq(make(Params, 0, c.Route().ParamsLen()), c.Params())
				_, _ = io.WriteString(c.Writer(), c.Pattern())
			}
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Add(MethodGet, rte, h)))
			}

			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)

			assert.Equal(t, tc.wantCode, w.Code)
			if tc.wantCode == http.StatusOK {
				assert.Equal(t, tc.wantPattern, w.Body.String())
				if tc.wantParams != nil {
					assert.Equal(t, tc.wantParams, gotParams)
				}
			}
		})
	}
}

func TestRouter_NonCanonicalPatternRejected(t *testing.T) {
	t.Parallel()

	t.Run("add", func(t *testing.T) {
		f, _ := NewRouter()
		var pe *PatternError
		_, err := f.Add(MethodGet, "/users/j%6Fhn", emptyHandler)
		require.ErrorAs(t, err, &pe)
		_, err = f.Add(MethodGet, "/users/john/%7bid%7d", emptyHandler)
		require.ErrorAs(t, err, &pe)
	})

	t.Run("update and delete", func(t *testing.T) {
		f, _ := NewRouter()
		require.NoError(t, onlyError(f.Add(MethodGet, "/users/john", emptyHandler)))
		var pe *PatternError
		_, err := f.Update(MethodGet, "/users/j%6Fhn", emptyHandler)
		require.ErrorAs(t, err, &pe)
		_, err = f.Delete(MethodGet, "/users/j%6Fhn")
		require.ErrorAs(t, err, &pe)
		assert.Equal(t, 1, f.Len())
	})
}

func TestRouter_ServeHTTP_EncodedDotSegments(t *testing.T) {
	t.Parallel()

	t.Run("strict path no match", func(t *testing.T) {
		f, _ := NewRouter()
		require.NoError(t, onlyError(f.Add(MethodGet, "/foo/bar", emptyHandler)))
		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/foo/%2E%2E/bar", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("strict path param captures decoded dots", func(t *testing.T) {
		f, _ := NewRouter()
		var got string
		require.NoError(t, onlyError(f.Add(MethodGet, "/foo/{x}/bar", func(c *Context) {
			got = c.Param("x")
		})))
		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/foo/%2E%2E/bar", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "..", got)
	})

	t.Run("redirect path collapses encoded dots", func(t *testing.T) {
		f, _ := NewRouter(WithMergeSlashes(RedirectPath), WithCollapseDotSegments(RedirectPath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/bar", emptyHandler)))
		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/foo/%2E%2E/bar", nil))
		assert.Equal(t, http.StatusMovedPermanently, w.Code)
		assert.Equal(t, "/bar", w.Header().Get(HeaderLocation))
	})
}

func FuzzRouter_Add(f *testing.F) {
	seeds := []string{
		// Empty / slashes
		"", "/", "//", "///",
		// Static
		"/users", "/api/users/list", "/a/b/c/d/e",
		// Named params
		"/users/{id}", "/users/{id:[0-9]+}",
		"/users/uuid:{id}", "/users/uuid:{id}/config",
		"/{id}/posts/{pid}",
		// Non-optional wildcards (+{}), prefix/infix/suffix
		"/files/+{path}", "/files/+{path:[a-z]+}",
		"/bucket/+{path}/meta", "/assets/+{path}/thumbnail",
		"/src/file=+{path}",
		"/src/+{filepath:[A-Za-z/]+\\.json}",
		// Optional wildcards (*{}) - suffix only
		"/*{filepath}", "/files/*{path}",
		"/api*{mount}", "/src/file=*{path}",
		"/users/*{trail}",
		// Consecutive slashes
		"/foo//bar", "/foo//", "/foo//{bar}", "/foo//+{bar}",
		"example.com//", "example.com//foo",
		// Hostname patterns
		"example.com/", "example.com/users",
		"api.example.com/users/{id}",
		"{sub}.example.com/", "{sub}.example.com/users",
		"+{sub}.example.com/", "+{sub}.example.com/users/{id}",
		"_srv.example.com/",
		// Case-insensitive hostname + underscore label
		"API.Example.COM/", "_foo.example.com/",
		// Malformed / edge cases
		"/{", "/}", "/{}", "/+{}", "/*{}",
		"/{:}", "/+{:}", "/*{:}",
		"/{id:[unclosed", "/{id}{jd}", "/foo{bar}baz",
		".example.com/", "example.com./",
		// Suspicious bytes
		"/\x00", "/\xe9", "/\t", "/\n",
		// Missing-slash / wrong-sigil patterns
		"*{path}", "+{path", "*{path:regex}",
		// Consecutive catch-all (invalid per spec)
		"/+{a}/+{b}", "/*{a}*{b}", "/+{a}+{b}",
		// Optional not as suffix (invalid per spec)
		"/*{path}/foo",
		// Percent-encoding: unreserved decoded, reserved kept, invalid rejected
		"/a%61b", "/foo%2Fbar", "/caf%C3%A9", "/%2E%2E/", "/100%", "/%zz", "/%2",
		"/x%61/{id}", "/{p:[%41]+}",
		// Literal '+' and '*' in path
		"/emails/a+b", "/a+", "/glob/*", "/a*b", "/a*{x}", "/a+{x}",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, rte string) {
		r := MustRouter()
		_ = r.Updates(func(txn *Txn) error {
			_, _ = txn.Add(MethodGet, rte, emptyHandler)
			return nil
		})
	})
}

func FuzzRouter_AddDelete(f *testing.F) {
	seeds := []string{
		// Trivial
		"",
		"/",
		"/foo",
		"/users/{id}",
		// Shared prefixes (tree splitting)
		"/users/{id}\n/users/{id}/posts\n/users/{id}/posts/{pid}",
		"/a\n/a/b\n/a/b/c\n/a/b/c/d",
		"/api/v1/users\n/api/v2/users",
		// Non-optional wildcards (+{}) - suffix and infix
		"/files/+{path}\n/files/list",
		"/assets/+{path}/thumbnail\n/assets/+{path}/preview",
		"/src/file=+{path}\n/src/file=static",
		// Optional wildcards (*{}) - suffix only
		"/*{any}",
		"/files/*{path}\n/files/static",
		"/src/file=*{path}",
		// Mix param + wildcards
		"/users/{id}\n/users/{id}/*{trail}",
		"/users/{id}\n/users/{id}/+{trail}",
		// Registration order + specificity
		"/{a}\n/{b}\n/{c}",
		"/{id:[0-9]+}\n/{id:[a-z]+}\n/{name}",
		// Duplicates and siblings
		"/foo\n/foo\n/bar",
		"/a\n/b\n/c\n/d\n/e",
		// Static hostnames
		"example.com/",
		"example.com/users\nexample.com/admin",
		"api.example.com/users\nweb.example.com/login",
		"a.example.com/\nb.example.com/\nc.example.com/",
		"my-api.example.com/\ntest-env.example.com/v1",
		// Param labels (prefix, middle, multi)
		"{sub}.example.com/\n{sub}.example.com/users",
		"api.{region}.example.com/users",
		"{a}.{b}.example.com/\n{a}.{b}.example.com/users",
		// Wildcard labels (prefix and middle)
		"+{sub}.example.com/\n+{sub}.example.com/posts/{id}",
		"api.+{tenant}.example.com/users",
		// SRV-like underscore labels
		"_srv.example.com/\n_foo.example.com/bar",
		// Mix hostname + path (mode switching on delete)
		"example.com/users\n/fallback\n/api/users",
		// Consecutive slashes (merge/split around "//" keys)
		"/foo//bar\n/foo//\n/foo/",
		"/foo//bar\n/foo//qux\n/foo/bar",
		"example.com//\nexample.com//foo",
		"example.com/\nexample.com//\nexample.com/foo",
		// Percent-encoding (non-canonical escapes rejected)
		"/users/john\n/users/j%6Fhn",
		"/a%61\n/ab\n/a%2Fb",
		"/%2E%2E/\n/100%\n/%zz",
		// Literal '+' and '*' in path
		"/emails/a+b\n/glob/*\n/a+{x}",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		r := MustRouter()

		inserted := 0
		_ = r.Updates(func(txn *Txn) error {
			for pattern := range strings.SplitSeq(input, "\n") {
				rr, err := txn.Add(MethodGet, pattern, emptyHandler, WithName(pattern))
				if err != nil {
					assert.Nilf(t, rr, "route %s", pattern)
					continue
				}
				assert.NotNilf(t, rr, "route %s", pattern)
				inserted++
			}
			return nil
		})

		it := r.Iter()
		countPath := len(slices.Collect(it.All()))
		assert.Equal(t, inserted, countPath)
		countNames := len(slices.Collect(it.Names()))
		assert.Equal(t, inserted, countNames)

		for route := range r.Iter().All() {
			found := r.Route(MethodGet, route.Pattern())
			require.NotNilf(t, found, "route %s", route.Pattern())
		}
		for route := range r.Iter().Names() {
			found := r.Name(route.Name())
			require.NotNilf(t, found, "route %s", route.Name())
		}

		_ = r.Updates(func(txn *Txn) error {
			for route := range r.Iter().All() {
				rte, err := txn.Delete(MethodGet, route.Pattern())
				assert.NoErrorf(t, err, "route %s", route.Pattern())
				assert.NotNil(t, rte, "route %s", route.Pattern())
			}
			return nil
		})

		it = r.Iter()
		countPath = len(slices.Collect(it.All()))
		assert.Equal(t, 0, countPath)
		countNames = len(slices.Collect(it.Names()))
		assert.Equal(t, 0, countNames)
	})
}

func TestRouter_Race_HostnamePathSwitch(t *testing.T) {
	var wg sync.WaitGroup
	start, wait := atomicSync()

	f, _ := NewRouter()

	h := func(c *Context) {}

	require.NoError(t, f.Updates(func(txn *Txn) error {
		for _, rte := range githubAPI {
			name := rte.method + ":" + rte.path
			if err := onlyError(txn.Add([]string{rte.method}, rte.path, h, WithName(name))); err != nil {
				return err
			}
			if err := onlyError(txn.Add([]string{rte.method}, rte.path, h, WithQueryMatcher("a", "b"), WithName(name+":1"))); err != nil {
				return err
			}
			if err := onlyError(txn.Add([]string{rte.method}, rte.path, h, WithQueryMatcher("c", "d"), WithName(name+":2"))); err != nil {
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
				if txn.Has([]string{githubAPI[0].method}, "{sub}.bar.{tld}"+githubAPI[0].path) {
					for _, rte := range githubAPI {
						if _, err := txn.Delete([]string{rte.method}, "{sub}.bar.{tld}"+rte.path); err != nil {
							return err
						}
						if _, err := txn.Delete([]string{rte.method}, "{sub}.bar.{tld}"+rte.path, WithQueryMatcher("a", "b")); err != nil {
							return err
						}
						if _, err := txn.Delete([]string{rte.method}, "{sub}.bar.{tld}"+rte.path, WithQueryMatcher("c", "d")); err != nil {
							return err
						}
					}
					return nil
				}

				for _, rte := range githubAPI {
					name := rte.method + ":" + "{sub}.bar.{tld}" + rte.path
					if err := onlyError(txn.Add([]string{rte.method}, "{sub}.bar.{tld}"+rte.path, h, WithName(name))); err != nil {
						return err
					}
					if err := onlyError(txn.Add([]string{rte.method}, "{sub}.bar.{tld}"+rte.path, h, WithQueryMatcher("a", "b"), WithName(name+":1"))); err != nil {
						return err
					}
					if err := onlyError(txn.Add([]string{rte.method}, "{sub}.bar.{tld}"+rte.path, h, WithQueryMatcher("c", "d"), WithName(name+":2"))); err != nil {
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
				if txn.Has([]string{githubAPI[0].method}, "foo.bar.baz"+githubAPI[0].path) {
					for _, rte := range githubAPI {
						if _, err := txn.Delete([]string{rte.method}, "foo.bar.baz"+rte.path, WithQueryMatcher("a", "b")); err != nil {
							return err
						}
						if _, err := txn.Delete([]string{rte.method}, "foo.bar.baz"+rte.path); err != nil {
							return err
						}
						if _, err := txn.Delete([]string{rte.method}, "foo.bar.baz"+rte.path, WithQueryMatcher("c", "d")); err != nil {
							return err
						}
					}
					return nil
				}

				for _, rte := range githubAPI {
					name := rte.method + ":" + "foo.bar.baz" + rte.path
					if err := onlyError(txn.Add([]string{rte.method}, "foo.bar.baz"+rte.path, h, WithQueryMatcher("a", "b"), WithName(name+":1"))); err != nil {
						return err
					}
					if err := onlyError(txn.Add([]string{rte.method}, "foo.bar.baz"+rte.path, h, WithName(name))); err != nil {
						return err
					}
					if err := onlyError(txn.Add([]string{rte.method}, "foo.bar.baz"+rte.path, h, WithQueryMatcher("c", "d"), WithName(name+":2"))); err != nil {
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
					r, _ := f.Match(req.Method, req)
					require.NotNil(t, r)
					assert.Contains(t, slices.Collect(r.Methods()), rte.method)
				}
			}
		}()
	}

	time.Sleep(500 * time.Millisecond)
	start()
	wg.Wait()

	// With a pair number of iteration, we should always delete all domains
	tree := f.getTree()
	assert.Len(t, tree.patterns.statics, 1)
	assert.Len(t, tree.patterns.params, 0)
	assert.Len(t, tree.patterns.wildcards, 0)
	assert.Len(t, tree.names.statics, 3)
	assert.Len(t, tree.names.params, 0)
	assert.Len(t, tree.names.wildcards, 0)

	methods := make(map[string]uint)
	for r := range f.Iter().All() {
		for method := range r.Methods() {
			methods[method]++
		}
	}
	assert.Equal(t, tree.methods, methods)

}

func TestRouter_Race_Data(t *testing.T) {
	var wg sync.WaitGroup
	start, wait := atomicSync()

	h := HandlerFunc(func(c *Context) {
		c.Pattern()
		for range c.Params() {
		}
	})
	newH := HandlerFunc(func(c *Context) {
		c.Pattern()
		for range c.Params() {
		}
	})

	f, _ := NewRouter()

	w := new(mockResponseWriter)

	wg.Add(len(githubAPI) * 4)
	for _, rte := range githubAPI {
		go func(method, route string) {
			wait()
			defer wg.Done()
			txn := f.Txn(true)
			defer txn.Abort()
			if txn.Has([]string{method}, route) {
				if assert.NoError(t, onlyError(txn.Update([]string{method}, route, h))) {
					txn.Commit()
				}
				return
			}
			if assert.NoError(t, onlyError(txn.Add([]string{method}, route, h))) {
				txn.Commit()
			}
		}(rte.method, rte.path)

		go func(method, route string) {
			wait()
			defer wg.Done()
			txn := f.Txn(true)
			defer txn.Abort()
			if txn.Has([]string{method}, route) {
				_, err := txn.Delete([]string{method}, route)
				if assert.NoError(t, err) {
					txn.Commit()
				}
				return
			}
			if assert.NoError(t, onlyError(txn.Add([]string{method}, route, newH))) {
				txn.Commit()
			}
		}(rte.method, rte.path)

		go func() {
			wait()
			defer wg.Done()
			for route := range f.Iter().All() {
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

func TestRouter_Race_NewRoute(t *testing.T) {
	var wg sync.WaitGroup
	start, wait := atomicSync()

	pass := func(next HandlerFunc) HandlerFunc { return next }
	f := MustRouter(WithMiddleware(pass, pass, pass))

	wg.Add(len(githubAPI) * 2)
	for _, rte := range githubAPI {
		for range 2 {
			go func(method, route string) {
				wait()
				defer wg.Done()
				_, err := f.NewRoute([]string{method}, route, func(c *Context) {}, WithMiddleware(pass))
				assert.NoError(t, err)
			}(rte.method, rte.path)
		}
	}

	time.Sleep(500 * time.Millisecond)
	start()
	wg.Wait()
}

func TestRouter_Race_PikeVM(t *testing.T) {
	f, _ := NewRouter(WithAllowRegexpParam(true))
	path, host := "/+{a}/x/+{b:[a-z/]+}/", "+{h}.x.+{i:(?:[a-z]+\\.)*[a-z]+}.com"
	require.NoError(t, onlyError(f.Add(MethodGet, path+"y", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodGet, host+path+"y", emptyHandler)))
	nearPath, nearHost, hitHost := strings.Repeat("/a/x", 50)+"/Z", strings.Repeat("a.x.", 10)+"org", "a.x.b.x.c.com"
	request := func(path, host string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = host
		return req
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				for _, h := range []string{nearHost, hitHost} {
					w := httptest.NewRecorder()
					f.ServeHTTP(w, request(nearPath, h))
					assert.Equal(t, http.StatusNotFound, w.Code)
					w = httptest.NewRecorder()
					f.ServeHTTP(w, request("/a/x/B/x/c/y", h))
					assert.Equal(t, http.StatusOK, w.Code)
				}
			}
		})
	}
	for g := range 2 {
		wg.Go(func() {
			for i := range 100 {
				assert.NoError(t, f.Updates(func(txn *Txn) error {
					leaf := fmt.Sprintf("w%d_%d", g, i)
					for _, p := range []string{path + leaf, host + path + leaf} {
						require.NoError(t, onlyError(txn.Add(MethodGet, p, emptyHandler)))
					}
					r, _ := txn.Match(http.MethodGet, request(nearPath, nearHost))
					assert.Nil(t, r)
					r, _ = txn.Match(http.MethodGet, request("/a/x/B/x/c/"+leaf, hitHost))
					require.NotNil(t, r)
					assert.Equal(t, host+path+leaf, r.Pattern())
					if i > 0 {
						prev := fmt.Sprintf("w%d_%d", g, i-1)
						for _, p := range []string{path + prev, host + path + prev} {
							require.NoError(t, onlyError(txn.Delete(MethodGet, p)))
						}
					}
					return nil
				}))
			}
		})
	}
	wg.Wait()
}

func TestRouter_ServeHTTP_Concurrent(t *testing.T) {
	r, _ := NewRouter()

	// /repos/{owner}/{repo}/keys
	h1 := HandlerFunc(func(c *Context) {
		assert.Equal(t, "john", c.Param("owner"))
		assert.Equal(t, "fox", c.Param("repo"))
		_ = c.String(200, c.Pattern())
	})

	// /repos/{owner}/{repo}/contents/+{path}
	h2 := HandlerFunc(func(c *Context) {
		assert.Equal(t, "alex", c.Param("owner"))
		assert.Equal(t, "vault", c.Param("repo"))
		assert.Equal(t, "file.txt", c.Param("path"))
		_ = c.String(200, c.Pattern())
	})

	// /users/{user}/received_events/public
	h3 := HandlerFunc(func(c *Context) {
		assert.Equal(t, "go", c.Param("user"))
		_ = c.String(200, c.Pattern())
	})

	require.NoError(t, onlyError(r.Add(MethodGet, "/repos/{owner}/{repo}/keys", h1)))
	require.NoError(t, onlyError(r.Add(MethodGet, "/repos/{owner}/{repo}/contents/+{path}", h2)))
	require.NoError(t, onlyError(r.Add(MethodGet, "/users/{user}/received_events/public", h3)))

	var wg sync.WaitGroup
	wg.Add(300)
	start, wait := atomicSync()
	for range 100 {
		go func() {
			defer wg.Done()
			wait()
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/repos/john/fox/keys", nil))
			assert.Equal(t, "/repos/{owner}/{repo}/keys", w.Body.String())
		}()

		go func() {
			defer wg.Done()
			wait()
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/repos/alex/vault/contents/file.txt", nil))
			assert.Equal(t, "/repos/{owner}/{repo}/contents/+{path}", w.Body.String())
		}()

		go func() {
			defer wg.Done()
			wait()
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/go/received_events/public", nil))
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

// This example demonstrates how to create a simple router with pretty logging,
// which registers the Recovery and Logger middleware.
func ExampleNewRouter() {
	// Create a new router with pretty logging (Recovery + Logger middleware).
	r, _ := NewRouter(WithPrettyLogs())

	// Define a route with the path "/hello/{name}", and set a simple handler that greets the
	// user by their name.
	r.MustAdd([]string{http.MethodGet, http.MethodHead}, "/hello/{name}", func(c *Context) {
		_ = c.String(200, fmt.Sprintf("Hello %s\n", c.Param("name")))
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
		return func(c *Context) {
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

	f, _ := NewRouter(WithMiddleware(metrics))

	f.MustAdd([]string{http.MethodGet, http.MethodHead}, "/hello/{name}", func(c *Context) {
		_ = c.String(200, fmt.Sprintf("Hello %s\n", c.Param("name")))
	})
}

func ExampleRouter_Match() {
	f, _ := NewRouter()
	f.MustAdd([]string{http.MethodGet, http.MethodHead}, "example.com/hello/{name}", emptyHandler)

	req := httptest.NewRequest(http.MethodGet, "/hello/fox", nil)

	route, tsr := f.Match(req.Method, req)
	fmt.Println(route.Pattern(), tsr) // example.com/hello/{name} false
}

func ExampleRouter_Has() {
	f, _ := NewRouter()
	f.MustAdd([]string{http.MethodGet, http.MethodHead}, "/hello/{name}", emptyHandler)
	exist := f.Has([]string{http.MethodGet, http.MethodHead}, "/hello/{name}")
	fmt.Println(exist) // true
}

// This example demonstrate how to create a managed read-write transaction.
func ExampleRouter_Updates() {
	f, _ := NewRouter()

	// Updates executes a function within the context of a read-write managed transaction. If no error is returned
	// from the function then the transaction is committed. If an error is returned then the entire transaction is
	// aborted.
	if err := f.Updates(func(txn *Txn) error {
		if _, err := txn.Add([]string{http.MethodGet, http.MethodHead}, "example.com/hello/{name}", func(c *Context) {
			_ = c.String(http.StatusOK, fmt.Sprintf("Hello %s\n", c.Param("name")))
		}); err != nil {
			return err
		}

		// Iter returns a collection of range iterators for traversing registered routes.
		it := txn.Iter()
		// When Iter() is called on a write transaction, it creates a point-in-time snapshot of the transaction state.
		// It means that writing on the current transaction while iterating is allowed, but the mutation will not be
		// observed in the result returned by PatternPrefix (or any other iterator).
		for route := range it.PatternPrefix("tmp.example.com/") {
			if _, err := txn.Delete(slices.Collect(route.Methods()), route.Pattern()); err != nil {
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
	f, _ := NewRouter()

	// Txn create an unmanaged read-write or read-only transaction.
	txn := f.Txn(true)
	defer txn.Abort()

	if _, err := txn.Add([]string{http.MethodGet, http.MethodHead}, "example.com/hello/{name}", func(c *Context) {
		_ = c.String(http.StatusOK, fmt.Sprintf("Hello %s\n", c.Param("name")))
	}); err != nil {
		log.Printf("error inserting route: %s", err)
		return
	}

	// Iter returns a collection of range iterators for traversing registered routes.
	it := txn.Iter()
	// When Iter() is called on a write transaction, it creates a point-in-time snapshot of the transaction state.
	// It means that writing on the current transaction while iterating is allowed, but the mutation will not be
	// observed in the result returned by PatternPrefix (or any other iterator).
	for route := range it.PatternPrefix("tmp.example.com/") {
		if _, err := txn.Delete(slices.Collect(route.Methods()), route.Pattern()); err != nil {
			log.Printf("error deleting route: %s", err)
			return
		}
	}
	// Finalize the transaction
	txn.Commit()
}

// This example demonstrate how to create a managed read-only transaction.
func ExampleRouter_View() {
	f, _ := NewRouter()

	// View executes a function within the context of a read-only managed transaction.
	_ = f.View(func(txn *Txn) error {
		if txn.Has([]string{http.MethodGet}, "/foo") && txn.Has([]string{http.MethodGet}, "/bar") {
			// Do something
		}
		return nil
	})
}

func onlyError[T any](_ T, err error) error {
	return err
}

func TestRouter_NormalizePathMergeSlashes(t *testing.T) {
	t.Run("rewrite before lookup", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(NormalizePath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/foo/bar", func(c *Context) {
			assert.Equal(t, "/foo/bar", c.RoutingPath())
			assert.Equal(t, "/foo/bar", c.Request().URL.Path)
			c.Writer().WriteHeader(http.StatusOK)
		})))

		req := httptest.NewRequest(http.MethodGet, "//foo///bar", nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "//foo///bar", req.URL.Path)
		assert.Equal(t, "/foo/bar", req.Pattern)
	})

	t.Run("wildcard captures merged path", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(NormalizePath))
		var captured string
		require.NoError(t, onlyError(f.Add(MethodGet, "/files/+{path}", func(c *Context) {
			captured = c.Param("path")
		})))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/files//a///b", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "a/b", captured)
	})

	t.Run("encoded slash is not merged", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(NormalizePath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/x/{p}", func(c *Context) {
			assert.Equal(t, "/x/a%2F%2Fb", c.RoutingPath())
		})))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x/a%2F%2Fb", nil))
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("dot segments are preserved", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(NormalizePath))
		var captured string
		require.NoError(t, onlyError(f.Add(MethodGet, "/files/+{path}", func(c *Context) {
			captured = c.Param("path")
		})))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/files//a/../b", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "a/../b", captured)
	})

	t.Run("merged path preserves encoded slash in param", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(NormalizePath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/foo/{url}", func(c *Context) {
			assert.Equal(t, "/foo/https%3A%2F%2Fbar%2Fbaz", c.RoutingPath())
		})))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/foo//https%3A%2F%2Fbar%2Fbaz", nil))
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("consecutive slash pattern rejected", func(t *testing.T) {
		for _, opt := range []NormalizeOption{NormalizePath, RedirectPath} {
			f := MustRouter(WithMergeSlashes(opt))
			err := onlyError(f.Add(MethodGet, "/a//b", emptyHandler))
			assert.ErrorAs(t, err, new(*PatternError))
		}

		f := MustRouter(WithCollapseDotSegments(RedirectPath))
		assert.NoError(t, onlyError(f.Add(MethodGet, "/a//b", emptyHandler)))
	})
}

func TestRouter_NormalizePathCollapseDots(t *testing.T) {
	t.Run("rewrite before lookup", func(t *testing.T) {
		f := MustRouter(WithCollapseDotSegments(NormalizePath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/bar", func(c *Context) {
			assert.Equal(t, "/bar", c.RoutingPath())
			c.Writer().WriteHeader(http.StatusOK)
		})))

		for _, target := range []string{"/foo/../bar", "/baz/%2E%2E/bar", "/./bar"} {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})

	t.Run("wildcard cannot swallow traversal", func(t *testing.T) {
		strict := MustRouter()
		var captured string
		require.NoError(t, onlyError(strict.Add(MethodGet, "/files/+{path}", func(c *Context) {
			captured = c.Param("path")
		})))

		w := httptest.NewRecorder()
		strict.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/files/a/../../etc/passwd", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "a/../../etc/passwd", captured)

		normalized := MustRouter(WithCollapseDotSegments(NormalizePath))
		require.NoError(t, onlyError(normalized.Add(MethodGet, "/files/+{path}", emptyHandler)))

		w = httptest.NewRecorder()
		normalized.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/files/a/../../etc/passwd", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("above root rejected", func(t *testing.T) {
		f := MustRouter(WithCollapseDotSegments(NormalizePath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/etc", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/../etc", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("custom reject handler and middleware scope", func(t *testing.T) {
		var scoped int
		f := MustRouter(
			WithCollapseDotSegments(NormalizePath),
			WithRejectPathHandler(func(c *Context) {
				assert.Equal(t, RejectPathHandler, c.Scope())
				c.Writer().WriteHeader(http.StatusTeapot)
			}),
			WithMiddlewareFor(RejectPathHandler, func(next HandlerFunc) HandlerFunc {
				return func(c *Context) {
					scoped++
					next(c)
				}
			}),
		)

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/../etc", nil))
		assert.Equal(t, http.StatusTeapot, w.Code)
		assert.Equal(t, 1, scoped)
	})

	t.Run("standalone collapse keeps empty segments", func(t *testing.T) {
		f := MustRouter(WithCollapseDotSegments(NormalizePath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/a/b", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/a//../b", nil))
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestRouter_NormalizePathBoth(t *testing.T) {
	f := MustRouter(WithMergeSlashes(NormalizePath), WithCollapseDotSegments(NormalizePath))
	require.NoError(t, onlyError(f.Add(MethodGet, "/b", emptyHandler)))

	w := httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/a//../b", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRouter_PathEncoding(t *testing.T) {
	t.Run("well-formed paths normalized", func(t *testing.T) {
		f := MustRouter()
		require.NoError(t, onlyError(f.Add(MethodGet, "/foo%2Fbar", emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodGet, "/caf%C3%A9", emptyHandler)))

		for _, target := range []string{"/foo%2Fbar", "/foo%2fbar", "/caf%c3%a9", "/caf%C3%A9"} {
			w := httptest.NewRecorder()
			f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})

	t.Run("raw byte encoded in place preserves escaped slash", func(t *testing.T) {
		f := MustRouter()
		require.NoError(t, onlyError(f.Add(MethodGet, "/foo%2Fcaf%C3%A9", func(c *Context) {
			assert.Equal(t, "/foo%2Fcaf%C3%A9", c.RoutingPath())
			assert.Equal(t, "/foo%2Fcaf%C3%A9", c.Request().URL.EscapedPath())
			c.Writer().WriteHeader(http.StatusOK)
		})))
		require.NoError(t, onlyError(f.Add(MethodGet, "/foo/caf%C3%A9", func(c *Context) {
			c.Writer().WriteHeader(http.StatusTeapot)
		})))

		req := httptest.NewRequest(http.MethodGet, "/foo%2Fcaf\xc3\xa9", nil)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "/foo%2Fcaf\xc3\xa9", req.URL.RawPath)
	})

	t.Run("param captures encoded raw byte", func(t *testing.T) {
		f := MustRouter()
		var captured string
		require.NoError(t, onlyError(f.Add(MethodGet, "/{p}", func(c *Context) {
			captured = c.Param("p")
		})))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/caf\xc3\xa9", nil))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "caf%C3%A9", captured)
	})

	t.Run("malformed escape falls back to path", func(t *testing.T) {
		f := MustRouter()
		var seen *http.Request
		require.NoError(t, onlyError(f.Add(MethodGet, "/a%25zz", func(c *Context) {
			seen = c.Request()
		})))

		req := httptest.NewRequest(http.MethodGet, "/a", nil)
		req.URL.Path = "/a%zz"
		req.URL.RawPath = "/a%zz"
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, seen)
		assert.NotSame(t, req, seen)
		assert.Equal(t, "/a%25zz", seen.URL.EscapedPath())
		assert.Equal(t, "/a%zz", req.URL.Path)
		assert.Equal(t, "/a%zz", req.URL.RawPath)
	})

	t.Run("malformed escape param capture", func(t *testing.T) {
		f := MustRouter()
		var captured string
		require.NoError(t, onlyError(f.Add(MethodGet, "/{p}", func(c *Context) {
			captured = c.Param("p")
		})))

		req := httptest.NewRequest(http.MethodGet, "/a", nil)
		req.URL.Path = "/a%zz"
		req.URL.RawPath = "/a%zz"
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "a%25zz", captured)
	})

	t.Run("malformed escape trailing slash redirect", func(t *testing.T) {
		f := MustRouter(WithTrailingSlash(RedirectSlash))
		require.NoError(t, onlyError(f.Add(MethodGet, "/a%25zz", emptyHandler)))

		req := httptest.NewRequest(http.MethodGet, "/a", nil)
		req.URL.Path = "/a%zz/"
		req.URL.RawPath = "/a%zz/"
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		assert.Equal(t, http.StatusMovedPermanently, w.Code)
		assert.Equal(t, "/a%25zz", w.Header().Get(HeaderLocation))
	})

	t.Run("inconsistent raw path falls back to path", func(t *testing.T) {
		f := MustRouter()
		require.NoError(t, onlyError(f.Add(MethodGet, "/other", func(c *Context) {
			assert.Equal(t, "/other", c.Request().URL.EscapedPath())
			assert.Empty(t, c.Request().URL.RawPath)
		})))

		req := httptest.NewRequest(http.MethodGet, "/foo%2Fbar", nil)
		req.URL.Path = "/other"
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "/foo%2Fbar", req.URL.RawPath)
	})
}

func TestRouter_NormalizeMixedModes(t *testing.T) {
	t.Run("merge normalize with collapse redirect", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(NormalizePath), WithCollapseDotSegments(RedirectPath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/b", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/a//../b", nil))
		assert.Equal(t, http.StatusMovedPermanently, w.Code)
		assert.Equal(t, "/b", w.Header().Get(HeaderLocation))
	})

	t.Run("merge normalize alone stays silent", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(NormalizePath), WithCollapseDotSegments(RedirectPath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/a/b", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/a//b", nil))
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("merge redirect with collapse normalize", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(RedirectPath), WithCollapseDotSegments(NormalizePath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/c/d", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/c//./d", nil))
		assert.Equal(t, http.StatusMovedPermanently, w.Code)
		assert.Equal(t, "/c/d", w.Header().Get(HeaderLocation))
	})
}

func TestRouter_RedirectPathMutatedRequest(t *testing.T) {
	cases := []struct {
		name         string
		mutated      string
		wantLocation string
	}{
		{
			name:         "recompute follows the rewritten path",
			mutated:      "/api/foo/../bar",
			wantLocation: "/api/bar",
		},
		{
			name:         "above root falls back to root",
			mutated:      "/../x",
			wantLocation: "/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := MustRouter(
				WithCollapseDotSegments(RedirectPath),
				WithMiddlewareFor(RedirectPathHandler, func(next HandlerFunc) HandlerFunc {
					return func(c *Context) {
						c.SetRequest(httptest.NewRequest(http.MethodGet, tc.mutated, nil))
						next(c)
					}
				}),
			)
			require.NoError(t, onlyError(f.Add(MethodGet, "/bar", emptyHandler)))

			w := httptest.NewRecorder()
			f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/foo/../bar", nil))
			assert.Equal(t, http.StatusMovedPermanently, w.Code)
			assert.Equal(t, tc.wantLocation, w.Header().Get(HeaderLocation))
		})
	}
}

func TestRouter_RedirectPathAboveRootRejected(t *testing.T) {
	f := MustRouter(WithCollapseDotSegments(RedirectPath))
	require.NoError(t, onlyError(f.Add(MethodGet, "/+{any}", emptyHandler)))

	w := httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/../etc", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRouter_ConnectExactMatch(t *testing.T) {
	for _, opt := range []NormalizeOption{NormalizePath, RedirectPath} {
		f := MustRouter(WithMergeSlashes(opt), WithCollapseDotSegments(opt), WithTrailingSlash(RedirectSlash))
		require.NoError(t, onlyError(f.Add(MethodConnect, "/a/b", emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodConnect, "/c/d/", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodConnect, "/a/b", nil))
		assert.Equal(t, http.StatusOK, w.Code)

		for _, target := range []string{"/a//b", "/a/x/../b", "/../b", "/c/d"} {
			w := httptest.NewRecorder()
			f.ServeHTTP(w, httptest.NewRequest(http.MethodConnect, target, nil))
			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.Empty(t, w.Header().Get(HeaderLocation))
		}
	}
}

func TestRouter_NormalizeInvalidOptions(t *testing.T) {
	_, err := NewRouter(WithMergeSlashes(normalizeOptionSentinel))
	assert.ErrorIs(t, err, ErrInvalidConfig)

	_, err = NewRouter(WithCollapseDotSegments(normalizeOptionSentinel))
	assert.ErrorIs(t, err, ErrInvalidConfig)

	_, err = NewRouter(WithRejectPathHandler(nil))
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestRouter_RedirectPathSingleOp(t *testing.T) {
	t.Run("collapse redirect preserves consecutive slashes", func(t *testing.T) {
		f := MustRouter(WithCollapseDotSegments(RedirectPath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/a//b", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/a//./b", nil))
		assert.Equal(t, http.StatusMovedPermanently, w.Code)
		assert.Equal(t, "/a//b", w.Header().Get(HeaderLocation))
	})

	t.Run("collapse redirect escapes leading slashes", func(t *testing.T) {
		f := MustRouter(WithCollapseDotSegments(RedirectPath))
		require.NoError(t, onlyError(f.Add(MethodGet, "//x", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "//a/../x", nil))
		assert.Equal(t, http.StatusMovedPermanently, w.Code)
		assert.Equal(t, "/%2Fx", w.Header().Get(HeaderLocation))
	})

	t.Run("merge redirect skipped when dot segments remain", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(RedirectPath))
		require.NoError(t, onlyError(f.Add(MethodGet, "/{p}/b", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/..//b", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestRouter_RedirectPathMethodNotAllowed(t *testing.T) {
	t.Run("static route", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(RedirectPath), WithNoMethod(true))
		require.NoError(t, onlyError(f.Add(MethodPost, "/foo/bar", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/foo//bar", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Empty(t, w.Header().Get(HeaderAllow))

		w = httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/foo/bar", nil))
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		assert.Equal(t, "POST", w.Header().Get(HeaderAllow))
	})

	t.Run("wildcard route matching the raw path", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(RedirectPath), WithNoMethod(true))
		require.NoError(t, onlyError(f.Add(MethodPost, "/files/+{path}", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/files//x", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Empty(t, w.Header().Get(HeaderAllow))
	})

	t.Run("param route matching a raw dot segment", func(t *testing.T) {
		f := MustRouter(WithCollapseDotSegments(RedirectPath), WithNoMethod(true))
		require.NoError(t, onlyError(f.Add(MethodPost, "/foo/{param}", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/foo/..", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Empty(t, w.Header().Get(HeaderAllow))
	})

	t.Run("consecutive slashes canonical when merge disabled", func(t *testing.T) {
		f := MustRouter(WithCollapseDotSegments(RedirectPath), WithNoMethod(true))
		require.NoError(t, onlyError(f.Add(MethodPost, "/files/+{path}", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/files//x", nil))
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		assert.Equal(t, "POST", w.Header().Get(HeaderAllow))
	})
}

func TestRouter_RedirectPathAutoOptions(t *testing.T) {
	t.Run("not found on non-canonical path", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(RedirectPath), WithAutoOptions(true))
		require.NoError(t, onlyError(f.Add(MethodGet, "/files/+{path}", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/files//x", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Empty(t, w.Header().Get(HeaderAllow))

		w = httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/files/x", nil))
		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "OPTIONS, GET", w.Header().Get(HeaderAllow))
	})

	t.Run("preflight ignores non-canonical path", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(RedirectPath), WithAutoOptions(true))
		require.NoError(t, onlyError(f.Add(MethodGet, "/files/+{path}", emptyHandler)))

		req := httptest.NewRequest(http.MethodOptions, "/files//x", nil)
		req.Header.Set(HeaderOrigin, "https://example.com")
		req.Header.Set(HeaderAccessControlRequestMethod, http.MethodGet)
		w := httptest.NewRecorder()
		f.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Header().Get(HeaderAllow))
	})

	t.Run("registered options route redirected", func(t *testing.T) {
		f := MustRouter(WithMergeSlashes(RedirectPath), WithAutoOptions(true))
		require.NoError(t, onlyError(f.Add(MethodOptions, "/foo/bar", emptyHandler)))

		w := httptest.NewRecorder()
		f.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/foo//bar", nil))
		assert.Equal(t, http.StatusPermanentRedirect, w.Code)
		assert.Equal(t, "/foo/bar", w.Header().Get(HeaderLocation))
	})
}

func TestRouter_NormalizeRewriteReuseOwnedCopy(t *testing.T) {
	f := MustRouter(WithMergeSlashes(NormalizePath), WithTrailingSlash(RelaxedSlash))
	var seen *http.Request
	require.NoError(t, onlyError(f.Add(MethodGet, "/foo/bar", func(c *Context) {
		seen = c.Request()
	})))

	req := httptest.NewRequest(http.MethodGet, "/foo//bar/", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, seen)
	assert.NotSame(t, req, seen)
	assert.Equal(t, "/foo/bar", seen.URL.Path)
	assert.Equal(t, "/foo//bar/", req.URL.Path)
	assert.Equal(t, "/foo/bar", seen.Pattern)
	assert.Equal(t, "/foo/bar", req.Pattern)
}

func FuzzRouter_ServeHTTP_NormalizeSecurity(f *testing.F) {
	seeds := []string{
		"/", "/files/a/../../etc/passwd", "/files//a", "//../x", "/a//../b", "/%2E%2E/x",
		"/files/..%2F..%2Fetc", "/a/./b//", "/..", "/...", "/a/%2F/../b", "*", "/a/b/c",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, target string) {
		u, err := url.ParseRequestURI(target)
		if err != nil || u.Scheme != "" || u.Host != "" {
			t.Skip()
		}

		check := func(c *Context) {
			rp := c.RoutingPath()
			assert.NotContains(t, rp, "//")
			assert.False(t, hasDotSegment(rp))
			for p := range c.Params() {
				assert.NotContains(t, p.Value, "//")
				assert.False(t, hasDotSegment("/"+p.Value))
			}
		}

		for _, opt := range []NormalizeOption{NormalizePath, RedirectPath} {
			f := MustRouter(WithMergeSlashes(opt), WithCollapseDotSegments(opt))
			require.NoError(t, onlyError(f.Add(MethodGet, "/*{all}", check)))
			require.NoError(t, onlyError(f.Add(MethodGet, "/files/+{path}", check)))

			req := &http.Request{Method: http.MethodGet, URL: u, Host: "fuzz.local", RemoteAddr: "1.2.3.4:5678"}
			w := httptest.NewRecorder()
			f.ServeHTTP(w, req)
		}
	})
}
