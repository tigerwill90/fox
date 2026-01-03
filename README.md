# Fox

<img align="right" width="159px" src="https://raw.githubusercontent.com/tigerwill90/fox/refs/heads/static/fox_logo.png">

[![Go Reference](https://pkg.go.dev/badge/github.com/tigerwill90/fox.svg)](https://pkg.go.dev/github.com/tigerwill90/fox)
[![tests](https://github.com/tigerwill90/fox/actions/workflows/tests.yaml/badge.svg)](https://github.com/tigerwill90/fox/actions?query=workflow%3Atests)
[![Go Report Card](https://goreportcard.com/badge/github.com/tigerwill90/fox)](https://goreportcard.com/report/github.com/tigerwill90/fox)
[![codecov](https://codecov.io/gh/tigerwill90/fox/branch/master/graph/badge.svg?token=09nfd7v0Bl)](https://codecov.io/gh/tigerwill90/fox)
![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/tigerwill90/fox)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/tigerwill90/fox)

Fox is a lightweight and high performance HTTP request router for [Go](https://go.dev/), designed for building reverse proxies,
API gateways, or other applications that require managing routes at runtime based on configuration changes or external events.
It is also well-suited for general use cases such as microservices and REST APIs, though it focuses on routing and does not include 
convenience helpers found in full-featured frameworks, such as automatic binding, content negotiation, file uploads, cookies, etc.

Fox supports **mutation on its routing tree while handling requests concurrently**. Internally, it uses a Radix Tree that supports
**lock-free** reads while allowing a concurrent writer, and is optimized for high-concurrency reads and low-concurrency writes.
The router supports complex routing patterns, enforces clear priority rules, and performs strict validation to prevent misconfigurations.

## Disclaimer
The current api is not yet stabilize. Breaking changes may occur before `v1.0.0` and will be noted on the release note.

## Features
**Runtime updates:** Register, update and delete route handler safely at any time without impact on performance. Fox never block while serving
request!

**Flexible routing:** Fox strikes a balance between routing flexibility, performance and clarity by enforcing clear priority rules, ensuring that
there are no unintended matches and maintaining high performance even for complex routing patterns. Supported features include named parameters,
suffix and infix catch-all, regexp constraints, hostname matching, method and method-less routes, route matchers, and sub-routers.

**Trailing slash handling:** Automatically handle trailing slash inconsistencies by either ignoring them, redirecting to 
the canonical path, or enforcing strict matching based on your needs.

**Path correction:** Automatically handle malformed paths with extra slashes or dots by either serving the cleaned path directly or redirecting to the canonical form.

**Automatic OPTIONS replies:** Fox has built-in native support for [OPTIONS requests](https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods/OPTIONS).

**Client IP Derivation:** Accurately determine the "real" client IP address using best practices tailored to your network topology.

**Rich middleware ecosystem:** Fox offers a robust ecosystem of prebuilt, high-quality middlewares, ready to integrate into your application.

Of course, you can also register custom `NotFound` and `MethodNotAllowed` handlers.

---
* [Getting started](#getting-started)
  * [Install](#install)
  * [Basic example](#basic-example)
  * [Named parameters](#named-parameters)
  * [Named wildcards](#named-wildcards-catch-all)
  * [Optional Named wildcards](#optional-named-wildcards-catch-all)
  * [Route matchers](#route-matchers)
  * [Method-less routes](#method-less-routes)
  * [Sub-Routers](#sub-routers)
  * [Hostname validation & restrictions](#hostname-validation--restrictions)
  * [Priority rules](#priority-rules)
    * [Hostname routing](#hostname-routing)
  * [Warning about context](#warning-about-context)
* [Concurrency](#concurrency)
  * [Managing routes a runtime](#managing-routes-a-runtime)
  * [ACID Transaction](#acid-transaction)
  * [Managed read-write transaction](#managed-read-write-transaction)
  * [Unmanaged read-write transaction](#unmanaged-read-write-transaction)
  * [Managed read-only transaction](#managed-read-only-transaction)
* [Middleware](#middleware)
  * [Official middlewares](#official-middlewares)
* [Working with http.Handler](#working-with-httphandler)
* [Handling OPTIONS Requests and CORS Automatically](#handling-options-requests-and-cors-automatically)
* [Resolving Client IP](#resolving-client-ip)
* [Benchmark](#benchmark)
* [Road to v1](#road-to-v1)
* [Contributions](#contributions)
* [License](#license)
---

## Getting started
#### Install
With a [correctly configured](https://go.dev/doc/install#testing) Go toolchain:
```shell
go get -u github.com/tigerwill90/fox
```

#### Basic example
````go
package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/tigerwill90/fox"
)

func main() {
	f := fox.MustRouter(fox.DefaultOptions())

	f.MustAdd([]string{http.MethodHead, http.MethodGet}, "/hello/{name}", func(c *fox.Context) {
		_ = c.String(http.StatusOK, fmt.Sprintf("Hello %s\n", c.Param("name")))
	})

	if err := http.ListenAndServe(":8080", f); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalln(err)
	}
}
````

#### Named parameters
Routes can include named parameters using curly braces `{name}` to match exactly one non-empty route segment. The matching 
segment are recorder as [Param](https://pkg.go.dev/github.com/tigerwill90/fox#Param) and accessible via the 
[Context](https://pkg.go.dev/github.com/tigerwill90/fox#Context). Named parameters are supported anywhere in 
the route, but only one parameter is allowed per segment (or hostname label) and must appear at the end of the segment.

````
Pattern /avengers/{name}

/avengers/ironman               matches
/avengers/thor                  matches
/avengers/hulk/angry            no matches
/avengers/                      no matches

Pattern /users/uuid:{id}

/users/uuid:123                 matches
/users/uuid:                    no matches

Pattern /users/uuid:{id}/config

/users/uuid:123/config          matches
/users/uuid:/config             no matches

Pattern {sub}.example.com/avengers

first.example.com/avengers      matches
example.com/avengers           no matches
````

Named parameters can include regular expression using the syntax `{name:regexp}`. Regular expressions cannot 
contain capturing groups, but can use non-capturing groups `(?:pattern)` instead. Regexp support is opt-in via
`fox.AllowRegexpParam(true)` option.

````
Pattern /products/{name:[A-Za-z]+}

/products/laptop        matches
/products/123           no matches
````

#### Named Wildcards (Catch-all)
Named wildcard start with an asterisk `*` followed by a name `{param}` and match any sequence of characters
including slashes, but cannot match an empty string. The matching segment are also accessible via
[Context](https://pkg.go.dev/github.com/tigerwill90/fox#Context). Catch-all parameters are supported anywhere in the route, 
but only one parameter is allowed per segment (or hostname label) and must appear at the end of the segment. 
Consecutive catch-all parameter are not allowed.

````
Pattern /src/*{filepath}

/src/conf.txt                      matches
/src/dir/config.txt                 matches
/src/                              no matches

Pattern /src/file=*{path}

/src/file=config.txt                 matches
/src/file=/dir/config.txt            matches
/src/file=                          no matches

Pattern: /assets/*{path}/thumbnail

/assets/images/thumbnail           matches
/assets/photos/2021/thumbnail      matches
/assets//thumbnail                 no matches

Pattern *{sub}.example.com/avengers

first.example.com/avengers          matches
first.second.example.com/avengers   matches
example.com/avengers               no matches
````

#### Optional Named Wildcards (Catch-all)
Optional named wildcard start with a plus sign `+` followed by a name `{param}` and match any sequence of characters
**including empty** strings. Unlike `*{param}`, optional wildcards can only be used as a suffix.

````
Pattern /src/+{filepath}

/src/conf.txt                      matches
/src/dir/config.txt                 matches
/src/                              matches

Pattern /src/file=+{path}

/src/file=config.txt                 matches
/src/file=/dir/config.txt            matches
/src/file=                          matches
````

Named wildcard can include regular expression using the syntax `*{name:regexp}` or `+{name:regexp}`. Regular expressions cannot
contain capturing groups, but can use non-capturing groups `(?:pattern)` instead. Regexp support is opt-in via `fox.AllowRegexpParam(true)` option.

````
Pattern /src/*{filepath:[A-Za-z/]+\.json}

/src/dir/config.json            matches
/src/dir/config.txt             no matches
````

#### Route matchers

Route matchers enable routing decisions based on request properties beyond methods, hostname and path. Multiple routes can share
the same pattern and methods and be differentiated by query parameters, headers, client IP, or custom criteria.

````
f.MustAdd(fox.MethodGet, "/api/users", PremiumHandler,
	fox.WithQueryMatcher("tier", "premium"),
)

f.MustAdd(fox.MethodGet, "/api/users", StandardHandler,
	fox.WithHeaderMatcher("X-API-Version", "v2"),
)

f.MustAdd(fox.MethodGet, "/api/users", DefaultHandler) // Fallback route
````

Built-in matchers include `fox.WithQueryMatcher`, `fox.WithQueryRegexpMatcher`, `fox.WithHeaderMatcher`, `fox.WithHeaderRegexpMatcher`,
and `fox.WithClientIPMatcher`. Multiple matchers on a route use AND logic. Routes without matchers serve as fallbacks.
For custom matching logic, implement the `fox.Matcher` interface and use `fox.WithMatcher`. See [Priority rules](#priority-rules) for matcher
evaluation order.

#### Method-less routes

Routes can be registered without specifying an HTTP method to match any method. The constant `fox.MethodAny` is
a convenience placeholder equivalent to an empty method set (nil or empty slice).

````go
// Handle any method on /health
f.MustAdd(fox.MethodAny, "/health", HealthHandler)
// Forward all requests to a backend service
f.MustAdd(fox.MethodAny, "/api/+{any}", ProxyHandler)
````

Routes registered with a specific HTTP method always take precedence over method-less routes. This allows defining method-specific
behavior while falling back to a generic handler for other methods.
````go
// Specific handler for GET requests
f.MustAdd(fox.MethodGet, "/resource", GetHandler)
// All other methods handled here
f.MustAdd(fox.MethodAny, "/resource", FallbackHandler)
````

#### Sub-Routers
Fox supports mounting a router as a regular route, enabling modular route management and isolated configuration.

```go
f := fox.MustRouter(
	fox.DefaultOptions(),
	fox.DevelopmentOptions(),
)
f.MustAdd([]string{http.MethodHead, http.MethodGet}, "/+{filepath}", fox.WrapH(http.FileServer(http.Dir("./public/"))))

api, r := f.MustSubRouter(fox.MethodAny, "/api+{mount}", fox.WithMiddleware(AuthMiddleware()))
api.MustAdd([]string{http.MethodHead, http.MethodGet}, "/", HelloHandler)
api.MustAdd([]string{http.MethodHead, http.MethodGet}, "/users", ListUser)
api.MustAdd([]string{http.MethodHead, http.MethodGet}, "/users/{id}", GetUser)
api.MustAdd(fox.MethodPost, "/users", CreateUser)
f.MustAddRoute(r)
```

The subrouter pattern must end with a catch-all parameter (`*{param}` or `+{param}`). Requests matching the prefix are delegated
to the mounted router with the remaining path.

Use cases include:
- Applying middleware or global router option to a route subtree
- Managing entire route subtree at runtime (e.g. insert, update, or delete via the parent router)
- Organizing routes into groups with shared configuration

#### Hostname validation & restrictions

Hostnames are validated to conform to the [LDH (letters, digits, hyphens) rule](https://datatracker.ietf.org/doc/html/rfc3696.html#section-2)
(lowercase only) and SRV-like "underscore labels". Wildcard segments within hostnames, such as {sub}.example.com/, are exempt from LDH validation
since they act as placeholders rather than actual domain labels. As such, they do not count toward the hard limit of 63 characters per label,
nor the 255-character limit for the full hostname (including periods). Internationalized domain names (IDNs) should be specified using an ASCII
(Punycode) representation.

The DNS specification permits a trailing period to be used to denote the root, e.g., `example.com` and `example.com.` are equivalent,
but the latter is more explicit and is required to be accepted by applications. Fox will reject route registered with
trailing period. However, the router will automatically strip any trailing period from incoming request host so it can match
the route regardless of a trailing period. Note that FQDN (with trailing period) does not play well with golang
TLS stdlib (see traefik/traefik#9157 (comment)).

#### Priority rules

The router is designed to balance routing flexibility, performance, and predictability. Internally, it uses a radix tree to
store routes efficiently. When a request arrives, Fox evaluates routes in the following order:

1. **Hostname matching**
    - Routes with hostnames are evaluated before path-only routes

2. **Pattern matching** (longest match, most specific first)
    - Static segments
    - Named parameters with regex constraints
    - Named parameters without constraints
    - Catch-all parameters with regex constraints
    - Catch-all parameters without constraints
    - Infix catch-all are evaluated before suffix catch-all (e.g., `/bucket/*{path}/meta` before `/bucket/*{path}`)
    - At the same level, multiple regex-constrained parameters are evaluated in registration order

3. **Method matching**
    - Routes with specific methods are evaluated before method-less routes

4. **Matcher evaluation** (for routes sharing the same pattern and overlapping methods)
    - Routes with matchers are evaluated before routes without
    - Among routes with matchers, higher priority is evaluated first (configurable via `fox.WithMatcherPriority`, or defaults to the number of matchers)
    - Routes with equal priority may be evaluated in any order

If a match candidate fails to complete the full route, including matchers, Fox returns to the last decision point and tries the next available
alternative following the same priority order.

##### Hostname routing

The router can transition instantly and transparently from path-only mode to hostname-prioritized mode without any 
additional configuration or action. If any route with a hostname is registered, the router automatically switches to 
prioritize hostname matching. Conversely, if no hostname-specific routes are registered, the router reverts to 
path-priority mode.

- If the router has no routes registered with hostnames, the router will perform a path-based lookup only.
- If the router includes at least one route with a hostname, the router will prioritize lookup based 
on the request host and path. If no match is found, the router will then fall back to a path-only lookup.

Hostname matching is **case-insensitive**, so requests to `Example.COM`, `example.com`, and `EXAMPLE.COM` will all match a route registered for `example.com`.

#### Warning about context
The `fox.Context` instance is freed once the request handler function returns to optimize resource allocation.
If you need to retain `fox.Context` beyond the scope of the handler, use the `fox.Context.Clone` methods.
````go
func Hello(c *fox.Context) {
    cc := c.Clone()
    go func() {
        time.Sleep(2 * time.Second)
        log.Println(cc.Param("name")) // Safe
    }()
    _ = c.String(http.StatusOK, "Hello %s\n", c.Param("name"))
}
````

## Concurrency
Fox implements an immutable Radix Tree that supports lock-free read while allowing a single writer to make progress.
Updates are applied by calculating the change which would be made to the tree were it mutable, assembling those changes
into a **patch** which is propagated to the root and applied in a **single atomic operation**. The result is a shallow copy 
of the tree, where only the modified path and its ancestors are cloned, ensuring minimal memory overhead.
Multiple patches can be applied in a single transaction, with intermediate nodes cached during the process to prevent 
redundant cloning.

### Other key points

- Routing requests is lock-free (reading thread never block, even while writes are ongoing)
- The router always see a consistent version of the tree while routing request
- Reading threads do not block writing threads (adding, updating or removing a handler can be done concurrently)
- Writing threads block each other but never block reading threads

As such threads that route requests should never encounter latency due to ongoing writes or other concurrent readers.

### Managing routes a runtime
#### Routing mutation
In this example, the handler for `routes/{action}` allow to dynamically register, update and delete handler for the
given route and method. Thanks to Fox's design, those actions are perfectly safe and may be executed concurrently.

````go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/tigerwill90/fox"
)

type Data struct {
	Pattern string   `json:"pattern"`
	Methods []string `json:"methods"`
	Text    string   `json:"text"`
}

func Action(c *fox.Context) {
	data := new(Data)
	if err := json.NewDecoder(c.Request().Body).Decode(data); err != nil {
		http.Error(c.Writer(), err.Error(), http.StatusBadRequest)
		return
	}

	var err error
	action := c.Param("action")
	switch action {
	case "add":
		_, err = c.Fox().Add(data.Methods, data.Pattern, func(c *fox.Context) {
			_ = c.String(http.StatusOK, data.Text)
		})
	case "update":
		_, err = c.Fox().Update(data.Methods, data.Pattern, func(c *fox.Context) {
			_ = c.String(http.StatusOK, data.Text)
		})
	case "delete":
		_, err = c.Fox().Delete(data.Methods, data.Pattern)
	default:
		http.Error(c.Writer(), fmt.Sprintf("action %q is not allowed", action), http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(c.Writer(), err.Error(), http.StatusConflict)
		return
	}

	_ = c.String(http.StatusOK, fmt.Sprintf("%s route [%s] %s: success\n", action, strings.Join(data.Methods, ","), data.Pattern))
}

func main() {
	f := fox.MustRouter(fox.DefaultOptions())

	f.MustAdd(fox.MethodPost, "/routes/{action}", Action)

	if err := http.ListenAndServe(":8080", f); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalln(err)
	}
}
````

#### ACID Transaction
Fox supports read-write and read-only transactions (with Atomicity, Consistency, and Isolation; Durability is not supported 
as transactions are in memory). Thread that route requests always see a consistent version of the routing tree and are 
fully isolated from an ongoing transaction until committed. Read-only transactions capture a point-in-time snapshot of 
the tree, ensuring they do not observe any ongoing or committed changes made after their creation.

#### Managed read-write transaction
````go
// Updates executes a function within the context of a read-write managed transaction. If no error is returned
// from the function then the transaction is committed. If an error is returned then the entire transaction is
// aborted.
if err := f.Updates(func(txn *fox.Txn) error {
	if _, err := txn.Add(fox.MethodGet, "exemple.com/hello/{name}", Handler); err != nil {
		return err
	}

	// Iter returns a collection of range iterators for traversing registered routes.
	it := txn.Iter()
	// When Iter() is called on a write transaction, it creates a point-in-time snapshot of the transaction state.
	// It means that writing on the current transaction while iterating is allowed, but the mutation will not be
	// observed in the result returned by PatternPrefix (or any other iterator).
	for route := range it.PatternPrefix("tmp.exemple.com/") {
		if _, err := txn.Delete(slices.Collect(route.Methods()), route.Pattern()); err != nil {
			return err
		}
	}
	return nil
}); err != nil {
	log.Printf("transaction aborted: %s", err)
}
````

#### Managed read-only transaction
````go
_ = f.View(func(txn *fox.Txn) error {
	if txn.Has(fox.MethodGet, "/foo") {
		if txn.Has(fox.MethodGet, "/bar") {
			// do something
		}
	}
	return nil
})
````

#### Unmanaged read-write transaction
````go
// Txn create an unmanaged read-write or read-only transaction.
txn := f.Txn(true)
defer txn.Abort()

if _, err := txn.Add(fox.MethodGet, "exemple.com/hello/{name}", Handler); err != nil {
	log.Printf("error inserting route: %s", err)
	return
}

// Iter returns a collection of range iterators for traversing registered routes.
it := txn.Iter()
// When Iter() is called on a write transaction, it creates a point-in-time snapshot of the transaction state.
// It means that writing on the current transaction while iterating is allowed, but the mutation will not be
// observed in the result returned by PatternPrefix (or any other iterator).
for route := range it.PatternPrefix("tmp.exemple.com/") {
	if _, err := txn.Delete(slices.Collect(route.Methods()), route.Pattern()); err != nil {
		log.Printf("error deleting route: %s", err)
		return
	}
}
// Finalize the transaction
txn.Commit()
````

## Middleware
Middlewares can be registered globally using the `fox.WithMiddleware` option. The example below demonstrates how 
to create and apply automatically a simple logging middleware to all routes (including 404, 405, etc...).

````go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/tigerwill90/fox"
)

func Logger(next fox.HandlerFunc) fox.HandlerFunc {
	return func(c *fox.Context) {
		start := time.Now()
		next(c)
		log.Printf("route: %s, latency: %s, status: %d, size: %d",
			c.Pattern(),
			time.Since(start),
			c.Writer().Status(),
			c.Writer().Size(),
		)
	}
}

func main() {
	f := fox.MustRouter(fox.WithMiddleware(Logger))

	f.MustAdd(fox.MethodGet, "/", func(c *fox.Context) {
		_ = c.String(http.StatusOK, "Hello World")
	})

	log.Fatalln(http.ListenAndServe(":8080", f))
}
````

Additionally, `fox.WithMiddlewareFor` option provide a more fine-grained control over where a middleware is applied, such as
only for 404 or 405 handlers. Possible scopes include `fox.RouteHandlers` (regular routes), `fox.NoRouteHandler`, `fox.NoMethodHandler`, 
`fox.RedirectSlashHandler`, `fox.RedirectPathHandler`, `fox.OptionsHandler` and any combination of these.

````gp
f  := fox.MustRouter(
	fox.WithMiddlewareFor(fox.RouteHandler, Logger),
	fox.WithMiddlewareFor(fox.NoRouteHandler|fox.NoMethodHandler, SpecialLogger),
)
````

Finally, it's also possible to attaches middleware on a per-route basis. Note that route-specific middleware must be explicitly reapplied 
when updating a route. If not, any middleware will be removed, and the route will fall back to using only global middleware (if any).

````go
f := fox.MustRouter(
	fox.WithMiddleware(fox.Logger(slog.NewTextHandler(os.Stdout, nil))),
)
f.MustAdd(fox.MethodGet, "/", SomeHandler, fox.WithMiddleware(foxtimeout.Middleware(2*time.Second)))
f.MustAdd(fox.MethodGet, "/foo", SomeOtherHandler)
````

### Official middlewares
* [tigerwill90/otelfox](https://github.com/tigerwill90/otelfox): Distributed tracing with [OpenTelemetry](https://opentelemetry.io/)
* [tigerwill90/foxdump](https://github.com/tigerwill90/foxdump): Body dump middleware for capturing requests and responses payload.
* [tigerwill90/foxtimeout](https://github.com/tigerwill90/foxtimeout): `http.TimeoutHandler` middleware optimized for Fox.
* [tigerwill90/foxwaf](https://github.com/tigerwill90/foxwaf): Coraza WAF middleware (experimental).
* [tigerwill90/foxgeoip](https://github.com/tigerwill90/foxgeoip): Block requests using GeoIP data based on client IP (experimental).

## Working with http.Handler
Fox itself implements the `http.Handler` interface which make easy to chain any compatible middleware before the router. Moreover, the router
provides convenient `fox.WrapF`, `fox.WrapH` and `fox.WrapM` adapter to be use with `http.Handler`.

The route parameters can be accessed by the wrapped handler through the request `context.Context` when the adapters are used.

Wrapping an `http.Handler`
````go
articles := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	params := fox.ParamsFromContext(r.Context())
	// Article id: 80
	// Matched route: /articles/{id}
	_, _ = fmt.Fprintf(w, "Article id: %s\nMatched route: %s\n", params.Get("id"), r.Pattern)
})

f := fox.MustRouter()
f.MustAdd(fox.MethodGet, "/articles/{id}", fox.WrapH(articles))
````

Wrapping any standard `http.Hanlder` middleware
````go
corsMw, _ := cors.NewMiddleware(cors.Config{
	Origins:        []string{"https://example.com"},
	Methods:        []string{http.MethodGet, http.MethodPost, http.MethodPut},
	RequestHeaders: []string{"Authorization"},
})

f := fox.MustRouter(
	fox.WithMiddlewareFor(fox.RouteHandler|fox.OptionsHandler, fox.WrapM(corsMw.Wrap)),
)
````

## Handling OPTIONS Requests and CORS Automatically
The `WithAutoOptions` setting or the `WithOptionsHandler` registration enable automatic responses to [OPTIONS requests](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Methods/OPTIONS).
This feature is particularly useful for handling Cross-Origin Resource Sharing (CORS) preflight requests.

When automatic OPTIONS responses is enabled, Fox distinguishes between regular OPTIONS requests and CORS preflight requests:
- **Regular OPTIONS requests:** The router responds with the `Allow` header populated with all HTTP methods registered for the matched resource. If no route matches, the `NoRoute` handler is called.
- **CORS preflight requests:** The router responds to every preflight request by calling the OPTIONS handler, regardless of whether the resource exists.

To customize how OPTIONS requests are handled (e.g. adding CORS headers), you may register a middleware for the `fox.OptionsHandler` scope
or provide a custom handler via `WithOptionsHandler`. Note that custom OPTIONS handlers always take priority over automatic replies.

````go
package main

import (
	"errors"
	"log"
	"net/http"

	"github.com/jub0bs/cors"
	"github.com/tigerwill90/fox"
)

func main() {
	corsMw, err := cors.NewMiddleware(cors.Config{
		Origins:        []string{"https://example.com"},
		Methods:        []string{http.MethodGet, http.MethodPost},
		RequestHeaders: []string{"Authorization"},
	})
	if err != nil {
		log.Fatal(err)
	}
	corsMw.SetDebug(true) // turn debug mode on (optional)

	f := fox.MustRouter(
		fox.WithAutoOptions(true), // let Fox automatically handle OPTIONS requests
		fox.WithMiddlewareFor(fox.OptionsHandler, fox.WrapM(corsMw.Wrap)),
	)

	f.MustAdd(fox.MethodGet, "/api/users", ListUsers)
	f.MustAdd(fox.MethodPost, "/api/users", CreateUsers)

	if err := http.ListenAndServe(":8080", f); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
````

Alternatively, you can use a sub-router to apply CORS only to a specific section of your API. This approach is useful when
you want to serve static files or other routes without CORS handling, while enabling it exclusively for API endpoints.

````go
package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/jub0bs/cors"
	"github.com/tigerwill90/fox"
)

func main() {
	corsMw, err := cors.NewMiddleware(cors.Config{
		Origins:        []string{"https://example.com"},
		Methods:        []string{http.MethodGet, http.MethodGet, http.MethodPost},
		RequestHeaders: []string{"Authorization"},
	})
	if err != nil {
		log.Fatal(err)
	}
	corsMw.SetDebug(true) // turn debug mode on (optional)

	f := fox.MustRouter()
	f.MustAdd([]string{http.MethodHead, http.MethodGet}, "/+{filepath}", fox.WrapH(http.FileServer(http.Dir("./public/"))))

	sub, r := f.MustSubRouter(
		fox.MethodAny, "/api/*{any}",
		fox.WithAutoOptions(true), // let Fox automatically handle OPTIONS requests
		fox.WithMiddlewareFor(fox.OptionsHandler, fox.WrapM(corsMw.Wrap)),
	)
	sub.MustAdd([]string{http.MethodHead, http.MethodGet}, "/users", ListUsers)
	sub.MustAdd(fox.MethodPost, "/users", CreateUser)

	f.MustAddRoute(r)

	if err := http.ListenAndServe(":8080", f); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
````

The CORS protocol is complex and security-sensitive. We do **NOT** recommend implementing CORS handling manually. Instead,
consider using [jub0bs/cors](https://github.com/jub0bs/cors), which performs extensive validation before allowing middleware creation, helping you avoid common pitfalls.

## Resolving Client IP
The `WithClientIPResolver` option allows you to set up strategies to resolve the client IP address based on your 
use case and network topology. Accurately determining the client IP is hard, particularly in environments with proxies or 
load balancers. For example, the leftmost IP in the `X-Forwarded-For` header is commonly used and is often regarded as the 
"closest to the client" and "most real," but it can be easily spoofed. Therefore, you should absolutely avoid using it 
for any security-related purposes, such as request throttling.

The resolver used must be chosen and tuned for your network configuration. This should result in a resolver never returning 
an error and if it does, it should be treated as an application issue or a misconfiguration, rather than defaulting to an 
untrustworthy IP.

The sub-package `github.com/tigerwill90/fox/clientip` provides a set of best practices resolvers that should cover most use cases.

````go
package main

import (
	"fmt"

	"github.com/tigerwill90/fox"
	"github.com/tigerwill90/fox/clientip"
)

func main() {
	resolver, err := clientip.NewRightmostNonPrivate(clientip.XForwardedForKey)
	if err != nil {
		panic(err)
	}
	f := fox.MustRouter(
		fox.DefaultOptions(),
		fox.WithClientIPResolver(
			resolver,
		),
	)

	f.MustAdd(fox.MethodGet, "/foo/bar", func(c *fox.Context) {
		ipAddr, err := c.ClientIP()
		if err != nil {
			// If the current resolver is not able to derive the client IP, an error
			// will be returned rather than falling back on an untrustworthy IP. It
			// should be treated as an application issue or a misconfiguration.
			panic(err)
		}
		fmt.Println(ipAddr.String())
	})
}
````

It is also possible to create a chain with multiple resolvers that attempt to derive the client IP, stopping when the first one succeeds.

````go
resolver, _ := clientip.NewLeftmostNonPrivate(clientip.ForwardedKey, 10)
f := fox.MustRouter(
	fox.DefaultOptions(),
	fox.WithClientIPResolver(
		// A common use for this is if a server is both directly connected to the
		// internet and expecting a header to check.
		clientip.NewChain(
			resolver,
			clientip.NewRemoteAddr(),
		),
	),
)
````

Note that there is no "sane" default strategy, so calling `Context.ClientIP` without a resolver configured will return 
an `ErrNoClientIPResolver`.

See this [blog post](https://adam-p.ca/blog/2022/03/x-forwarded-for/) for general guidance on choosing a strategy that fit your needs.

## Benchmark
The primary goal of Fox is to be a lightweight, high performance router which allow routes modification at runtime.
The following benchmarks attempt to compare Fox to various popular alternatives, including both fully-featured web frameworks
and lightweight request routers. These benchmarks are based on the [julienschmidt/go-http-routing-benchmark](https://github.com/julienschmidt/go-http-routing-benchmark) 
repository.

Please note that these benchmarks should not be taken too seriously, as the comparison may not be entirely fair due to 
the differences in feature sets offered by each framework. Performance should be evaluated in the context of your specific 
use case and requirements. While Fox aims to excel in performance, it's important to consider the trade-offs and 
functionality provided by different web frameworks and routers when making your selection.

### Config
```
GOOS:   Linux
GOARCH: amd64
GO:     1.20
CPU:    Intel(R) Core(TM) i9-9900K CPU @ 3.60GHz
```
### Static Routes
It is just a collection of random static paths inspired by the structure of the Go directory. It might not be a realistic URL-structure.

**GOMAXPROCS: 1**
```
BenchmarkHttpRouter_StaticAll     161659              7570 ns/op               0 B/op          0 allocs/op
BenchmarkHttpTreeMux_StaticAll    132446              8836 ns/op               0 B/op          0 allocs/op
BenchmarkFox_StaticAll            102577             11348 ns/op               0 B/op          0 allocs/op
BenchmarkStdMux_StaticAll          91304             13382 ns/op               0 B/op          0 allocs/op
BenchmarkGin_StaticAll             78224             15433 ns/op               0 B/op          0 allocs/op
BenchmarkEcho_StaticAll            77923             15739 ns/op               0 B/op          0 allocs/op
BenchmarkBeego_StaticAll           10000            101094 ns/op           55264 B/op        471 allocs/op
BenchmarkGorillaMux_StaticAll       2283            525683 ns/op          113041 B/op       1099 allocs/op
BenchmarkMartini_StaticAll          1330            936928 ns/op          129210 B/op       2031 allocs/op
BenchmarkTraffic_StaticAll          1064           1140959 ns/op          753611 B/op      14601 allocs/op
BenchmarkPat_StaticAll               967           1230424 ns/op          602832 B/op      12559 allocs/op
```
In this benchmark, Fox performs as well as `Gin` and `Echo` which are both Radix Tree based routers. An interesting fact is
that [HttpTreeMux](https://github.com/dimfeld/httptreemux) also support [adding route while serving request concurrently](https://github.com/dimfeld/httptreemux#concurrency).
However, it takes a slightly different approach, by using an optional `RWMutex` that may not scale as well as Fox under heavy load. The next
test compare `HttpTreeMux` with and without the `*SafeAddRouteFlag` (concurrent reads and writes) and `Fox` in parallel benchmark.

**GOMAXPROCS: 16**
```
Route: all

BenchmarkFox_StaticAll-16                          99322             11369 ns/op               0 B/op          0 allocs/op
BenchmarkFox_StaticAllParallel-16                 831354              1422 ns/op               0 B/op          0 allocs/op
BenchmarkHttpTreeMux_StaticAll-16                 135560              8861 ns/op               0 B/op          0 allocs/op
BenchmarkHttpTreeMux_StaticAllParallel-16*        172714              6916 ns/op               0 B/op          0 allocs/op
```
As you can see, this benchmark highlight the cost of using higher synchronisation primitive like `RWMutex` to be able to register new route while handling requests.

### Micro Benchmarks
The following benchmarks measure the cost of some very basic operations.

In the first benchmark, only a single route, containing a parameter, is loaded into the routers. Then a request for a URL 
matching this pattern is made and the router has to call the respective registered handler function. End.

**GOMAXPROCS: 1**
```
BenchmarkFox_Param              33024534                36.61 ns/op            0 B/op          0 allocs/op
BenchmarkEcho_Param             31472508                38.71 ns/op            0 B/op          0 allocs/op
BenchmarkGin_Param              25826832                52.88 ns/op            0 B/op          0 allocs/op
BenchmarkHttpRouter_Param       21230490                60.83 ns/op           32 B/op          1 allocs/op
BenchmarkHttpTreeMux_Param       3960292                280.4 ns/op          352 B/op          3 allocs/op
BenchmarkBeego_Param             2247776                518.9 ns/op          352 B/op          3 allocs/op
BenchmarkPat_Param               1603902                676.6 ns/op          512 B/op         10 allocs/op
BenchmarkGorillaMux_Param        1000000                 1011 ns/op         1024 B/op          8 allocs/op
BenchmarkTraffic_Param            648986                 1686 ns/op         1848 B/op         21 allocs/op
BenchmarkMartini_Param            485839                 2446 ns/op         1096 B/op         12 allocs/op
```
Same as before, but now with multiple parameters, all in the same single route. The intention is to see how the routers scale with the number of parameters.

**GOMAXPROCS: 1**
```
BenchmarkFox_Param5             16608495                72.84 ns/op            0 B/op          0 allocs/op
BenchmarkGin_Param5             13098740                92.22 ns/op            0 B/op          0 allocs/op
BenchmarkEcho_Param5            12025460                96.33 ns/op            0 B/op          0 allocs/op
BenchmarkHttpRouter_Param5       8233530                148.1 ns/op          160 B/op          1 allocs/op
BenchmarkHttpTreeMux_Param5      1986019                616.9 ns/op          576 B/op          6 allocs/op
BenchmarkBeego_Param5            1836229                655.3 ns/op          352 B/op          3 allocs/op
BenchmarkGorillaMux_Param5        757936                 1572 ns/op         1088 B/op          8 allocs/op
BenchmarkPat_Param5               645847                 1724 ns/op          800 B/op         24 allocs/op
BenchmarkTraffic_Param5           424431                 2729 ns/op         2200 B/op         27 allocs/op
BenchmarkMartini_Param5           424806                 2772 ns/op         1256 B/op         13 allocs/op


BenchmarkGin_Param20             4636416               244.6 ns/op             0 B/op          0 allocs/op
BenchmarkFox_Param20             4667533               250.7 ns/op             0 B/op          0 allocs/op
BenchmarkEcho_Param20            4352486               277.1 ns/op             0 B/op          0 allocs/op
BenchmarkHttpRouter_Param20      2618958               455.2 ns/op           640 B/op          1 allocs/op
BenchmarkBeego_Param20            847029                1688 ns/op           352 B/op          3 allocs/op
BenchmarkHttpTreeMux_Param20      369500                2972 ns/op          3195 B/op         10 allocs/op
BenchmarkGorillaMux_Param20       318134                3561 ns/op          3195 B/op         10 allocs/op
BenchmarkMartini_Param20          223070                5117 ns/op          3619 B/op         15 allocs/op
BenchmarkPat_Param20              157380                7442 ns/op          4094 B/op         73 allocs/op
BenchmarkTraffic_Param20          119677                9864 ns/op          7847 B/op         47 allocs/op
```

Now let's see how expensive it is to access a parameter. The handler function reads the value (by the name of the parameter, e.g. with a map 
lookup; depends on the router) and writes it to `/dev/null`

**GOMAXPROCS: 1**
```
BenchmarkFox_ParamWrite                 16707409                72.53 ns/op            0 B/op          0 allocs/op
BenchmarkHttpRouter_ParamWrite          16478174                73.30 ns/op           32 B/op          1 allocs/op
BenchmarkGin_ParamWrite                 15828385                75.73 ns/op            0 B/op          0 allocs/op
BenchmarkEcho_ParamWrite                13187766                95.18 ns/op            8 B/op          1 allocs/op
BenchmarkHttpTreeMux_ParamWrite          4132832               279.9 ns/op           352 B/op          3 allocs/op
BenchmarkBeego_ParamWrite                2172572               554.3 ns/op           360 B/op          4 allocs/op
BenchmarkPat_ParamWrite                  1200334               996.8 ns/op           936 B/op         14 allocs/op
BenchmarkGorillaMux_ParamWrite           1000000              1005 ns/op            1024 B/op          8 allocs/op
BenchmarkMartini_ParamWrite               454255              2667 ns/op            1168 B/op         16 allocs/op
BenchmarkTraffic_ParamWrite               511766              2021 ns/op            2272 B/op         25 allocs/op
```

In those micro benchmarks, we can see that `Fox` scale really well, even with long wildcard routes. Like `Gin`, this router reuse the
data structure (e.g. `fox.Context` slice) containing the matching parameters in order to remove completely heap allocation. 

### Github
Finally, this benchmark execute a request for each GitHub API route (203 routes).

**GOMAXPROCS: 1**
```
BenchmarkFox_GithubAll             63984             18555 ns/op               0 B/op          0 allocs/op
BenchmarkEcho_GithubAll            49312             23353 ns/op               0 B/op          0 allocs/op
BenchmarkGin_GithubAll             48422             24926 ns/op               0 B/op          0 allocs/op
BenchmarkHttpRouter_GithubAll      45706             26818 ns/op           14240 B/op        171 allocs/op
BenchmarkHttpTreeMux_GithubAll     14731             80133 ns/op           67648 B/op        691 allocs/op
BenchmarkBeego_GithubAll            7692            137926 ns/op           72929 B/op        625 allocs/op
BenchmarkTraffic_GithubAll           636           1916586 ns/op          845114 B/op      14634 allocs/op
BenchmarkMartini_GithubAll           530           2205947 ns/op          238546 B/op       2813 allocs/op
BenchmarkGorillaMux_GithubAll        529           2246380 ns/op          203844 B/op       1620 allocs/op
BenchmarkPat_GithubAll               424           2899405 ns/op         1843501 B/op      29064 allocs/op
```

## Road to v1
- [x] [Update route syntax](https://github.com/tigerwill90/fox/pull/10#issue-1643728309) @v0.6.0
- [x] [Route overlapping](https://github.com/tigerwill90/fox/pull/9#issue-1642887919) @v0.7.0
- [x] [Route overlapping (catch-all and params)](https://github.com/tigerwill90/fox/pull/24#issue-1784686061) @v0.10.0
- [x] [Ignore trailing slash](https://github.com/tigerwill90/fox/pull/32), [Builtin Logger Middleware](https://github.com/tigerwill90/fox/pull/33), [Client IP Derivation](https://github.com/tigerwill90/fox/pull/33) @v0.14.0
- [x] [Support infix wildcard](https://github.com/tigerwill90/fox/pull/46), [Support hostname routing](https://github.com/tigerwill90/fox/pull/48), [Support ACID transaction](https://github.com/tigerwill90/fox/pull/49) @v0.18.0
- [x] [Support regexp params](https://github.com/tigerwill90/fox/pull/68) @v0.25.0
- [x] [Support route matchers](https://github.com/tigerwill90/fox/pull/69), [Support SubRouter](https://github.com/tigerwill90/fox/pull/70), [Method-less tree](https://github.com/tigerwill90/fox/pull/71) @v0.26.0
- [ ] Programmatic error handling
- [ ] Improving performance and polishing
- [ ] Stabilizing API

## Contributions
This project aims to provide a lightweight, high-performance router that is easy to use and hard to misuse, designed for building API gateways and reverse proxies.
Features are chosen carefully with an emphasis on composability, and each addition is evaluated against this core mission. The router exposes a relatively low-level API,
allowing it to serve as a building block for implementing your own "batteries included" frameworks. Feature requests and PRs along these lines are welcome. 

## License

Fox is licensed under the **Apache License 2.0**. See [`LICENSE.txt`](./LICENSE.txt) for details.

The [**Fox logo**](https://github.com/tigerwill90/fox/blob/static/fox_logo.png) is licensed separately under [**CC BY-NC-ND 4.0**](https://creativecommons.org/licenses/by-nc-nd/4.0/?ref=chooser-v1). 
See [`LICENSE-fox-logo.txt`](https://github.com/tigerwill90/fox/blob/static/LICENSE-fox-logo.txt) for details.

## Acknowledgements
- [hashicorp/go-immutable-radix](https://github.com/hashicorp/go-immutable-radix): Fox Tree design is inspired by Hashicorp's Immutable Radix Tree.
- [julienschmidt/httprouter](https://github.com/julienschmidt/httprouter): some feature that implements Fox are inspired from Julien Schmidt's router. Most notably,
this package uses the optimized [httprouter.Cleanpath](https://github.com/julienschmidt/httprouter/blob/master/path.go) function.
- [realclientip/realclientip-go](https://github.com/realclientip/realclientip-go): Fox uses a derivative version of Adam Pritchard's `realclientip-go` library. 
See his insightful [blog post](https://adam-p.ca/blog/2022/03/x-forwarded-for/) on the topic for more details.
- The router API is influenced by popular routers such as [Gin](https://github.com/gin-gonic/gin) and [Echo](https://github.com/labstack/echo).
