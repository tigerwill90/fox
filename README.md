[![Go Reference](https://pkg.go.dev/badge/github.com/tigerwill90/fox.svg)](https://pkg.go.dev/github.com/tigerwill90/fox)
[![tests](https://github.com/tigerwill90/fox/actions/workflows/tests.yaml/badge.svg)](https://github.com/tigerwill90/fox/actions?query=workflow%3Atests)
[![Go Report Card](https://goreportcard.com/badge/github.com/tigerwill90/fox)](https://goreportcard.com/report/github.com/tigerwill90/fox)
[![codecov](https://codecov.io/gh/tigerwill90/fox/branch/master/graph/badge.svg?token=09nfd7v0Bl)](https://codecov.io/gh/tigerwill90/fox)
![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/tigerwill90/fox)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/tigerwill90/fox)
# Fox
Fox is a zero allocation, lightweight, high performance HTTP request router for [Go](https://go.dev/). The main difference with other routers is
that it supports **mutation on its routing tree while handling request concurrently**. Internally, Fox use a 
[Concurrent Radix Tree](https://github.com/npgall/concurrent-trees/blob/master/documentation/TreeDesign.md) that support **lock-free 
reads** while allowing **concurrent writes**. The router tree is optimized for high-concurrency and high performance reads, and low-concurrency write. 

Fox supports various use cases, but it is especially designed for applications that require changes at runtime to their 
routing structure based on user input, configuration changes, or other runtime events.

## Disclaimer
The current api is not yet stabilize. Breaking changes may occur before `v1.0.0` and will be noted on the release note.

## Features
**Runtime updates:** Register, update and delete route handler safely at any time without impact on performance. Fox never block while serving
request!

**Wildcard pattern:** Route can be registered using wildcard parameters. The matched path segment can then be easily retrieved by 
name. Due to Fox design, wildcard route are cheap and scale really well.

**Detect panic:** Comes with a ready-to-use, efficient Recovery middleware that gracefully handles panics.

**Get the current route:** You can easily retrieve the route of the matched request. This actually makes it easier to integrate
observability middleware like open telemetry.

**Flexible routing:**  Fox strikes a balance between routing flexibility, performance and clarity by enforcing clear 
priority rules, ensuring that there are no unintended matches and maintaining high performance even for complex routing pattern.

**Redirect trailing slashes:** Redirect automatically the client, at no extra cost, if another route matches, with or without a trailing slash.

**Ignore trailing slashes:** In contrast to redirecting, this option allows the router to handle requests regardless of an extra 
or missing trailing slash, at no extra cost.

**Automatic OPTIONS replies:** Fox has built-in native support for [OPTIONS requests](https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods/OPTIONS).

**Client IP Derivation:** Accurately determine the "real" client IP address using best practices tailored to your network topology.

Of course, you can also register custom `NotFound` and `MethodNotAllowed` handlers.

## Getting started
### Installation
```shell
go get -u github.com/tigerwill90/fox
```

### Basic example
````go
package main

import (
	"errors"
	"github.com/tigerwill90/fox"
	"log"
	"net/http"
)

func main() {
	f := fox.New(fox.DefaultOptions())

	f.MustHandle(http.MethodGet, "/hello/{name}", func(c fox.Context) {
		_ = c.String(http.StatusOK, "hello %s\n", c.Param("name"))
	})

	if err := http.ListenAndServe(":8080", f); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalln(err)
	}
}
````
#### Error handling
Since new route may be added at any given time, Fox, unlike other router, does not panic when a route is malformed or conflicts with another. 
Instead, it returns the following error values:
```go
ErrRouteExist    = errors.New("route already registered")
ErrRouteConflict = errors.New("route conflict")
ErrInvalidRoute  = errors.New("invalid route")
```

Conflict error may be unwrapped to retrieve conflicting route.
```go
if errors.Is(err, fox.ErrRouteConflict) {
    matched := err.(*fox.RouteConflictError).Matched
    for _, route := range matched {
        fmt.Println(route)
    }
}
```

#### Named parameters
A route can be defined using placeholder (e.g `{name}`). The matching segment are recorder into `fox.Param` accessible 
via `fox.Context`. `fox.Context.Params` provide an iterator to range over `fox.Param` and `fox.Context.Param` allow
to retrieve directly the value of a parameter using the placeholder name.

````
Pattern /avengers/{name}

/avengers/ironman           match
/avengers/thor              match
/avengers/hulk/angry        no match
/avengers/                  no match

Pattern /users/uuid:{id}

/users/uuid:123             match
/users/uuid                 no match
````

#### Catch all parameter
Catch-all parameters can be used to match everything at the end of a route. The placeholder start with `*` followed by a regular
named parameter (e.g. `*{name}`).
````
Pattern /src/*{filepath}

/src/                       match
/src/conf.txt               match
/src/dir/config.txt         match

Patter /src/file=*{path}

/src/file=                  match
/src/file=config.txt        match
/src/file=/dir/config.txt   match
````

#### Priority rules
Routes are prioritized based on specificity, with static segments taking precedence over wildcard segments.

The following rules apply:

- Static segments are always evaluated first.
- A named parameter can only overlap with a catch-all parameter or static segments.
- A catch-all parameter can only overlap with a named parameter or static segments.
- When a named parameter overlaps with a catch-all parameter, the named parameter is evaluated first.

For instance, `GET /users/{id}` and `GET /users/{name}/profile` cannot coexist, as the `{id}` and `{name}` segments 
are overlapping. These limitations help to minimize the number of branches that need to be evaluated in order to find 
the right match, thereby maintaining high-performance routing.

For example, the followings route are allowed:
````
GET /*{filepath}
GET /users/{id}
GET /users/{id}/emails
GET /users/{id}/{actions}
POST /users/{name}/emails
````

Additionally, let's consider an example to illustrate the prioritization:
````
GET /fs/avengers.txt    #1 => match /fs/avengers.txt
GET /fs/{filename}      #2 => match /fs/ironman.txt
GET /fs/*{filepath}     #3 => match /fs/avengers/ironman.txt
````

#### Warning about context
The `fox.Context` instance is freed once the request handler function returns to optimize resource allocation.
If you need to retain `fox.Context` beyond the scope of the handler, use the `fox.Context.Clone` methods.
````go
func Hello(c fox.Context) {
    cc := c.Clone()
    go func() {
        time.Sleep(2 * time.Second)
        log.Println(cc.Param("name")) // Safe
    }()
    _ = c.String(http.StatusOK, "Hello %s\n", c.Param("name"))
}
````

## Concurrency
Fox implements a [Concurrent Radix Tree](https://github.com/npgall/concurrent-trees/blob/master/documentation/TreeDesign.md) that supports **lock-free** 
reads while allowing **concurrent writes**, by calculating the changes which would be made to the tree were it mutable, and assembling those changes 
into a **patch**, which is then applied to the tree in a **single atomic operation**.

For example, here we are inserting the new key `toast` into to the tree which require an existing node to be split:

<p align="center" width="100%">
    <img width="100%" src="https://raw.githubusercontent.com/tigerwill90/concurrent-trees/master/documentation/images/tree-apply-patch.png">
</p>

When traversing the tree during a patch, reading threads will either see the **old version** or the **new version** of the (sub-)tree, but both version are 
consistent view of the tree.

#### Other key points

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
	"github.com/tigerwill90/fox"
	"log"
	"net/http"
	"strings"
)

func Action(c fox.Context) {
	var data map[string]string
	if err := json.NewDecoder(c.Request().Body).Decode(&data); err != nil {
		http.Error(c.Writer(), err.Error(), http.StatusBadRequest)
		return
	}

	method := strings.ToUpper(data["method"])
	path := data["path"]
	text := data["text"]

	if path == "" || method == "" {
		http.Error(c.Writer(), "missing method or path", http.StatusBadRequest)
		return
	}

	var err error
	action := c.Param("action")
	switch action {
	case "add":
		_, err = c.Fox().Handle(method, path, func(c fox.Context) {
			_ = c.String(http.StatusOK, text)
		})
	case "update":
		_, err = c.Fox().Update(method, path, func(c fox.Context) {
			_ = c.String(http.StatusOK, text)
		})
	case "delete":
		err = c.Fox().Delete(method, path)
	default:
		http.Error(c.Writer(), fmt.Sprintf("action %q is not allowed", action), http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(c.Writer(), err.Error(), http.StatusConflict)
		return
	}

	_ = c.String(http.StatusOK, "%s route [%s] %s: success\n", action, method, path)
}

func main() {
	f := fox.New(fox.DefaultOptions())
	f.MustHandle(http.MethodPost, "/routes/{action}", Action)
	
	if err := http.ListenAndServe(":8080", f); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalln(err)
	}
}
````

#### Tree swapping
Fox also enables you to replace the entire tree in a single atomic operation using the `Swap` methods.
Note that router's options apply automatically on the new tree.

````go
package main

import (
	"errors"
	"github.com/tigerwill90/fox"
	"log"
	"net/http"
	"time"
)

func Reload(r *fox.Router) {
	for ; true; <-time.Tick(10 * time.Second) {
		routes := GetRoutes()
		tree := r.NewTree()
		for _, rte := range routes {
			if _, err := tree.Handle(rte.Method, rte.Path, rte.Handler); err != nil {
				log.Printf("failed to register route: %s\n", err)
				continue
			}
		}
		// Swap the currently in-use routing tree with the new provided.
		r.Swap(tree)
		log.Println("route reloaded!")
	}
}

func main() {
	f := fox.New(fox.DefaultOptions())

	go Reload(f)

	if err := http.ListenAndServe(":8080", f); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalln(err)
	}
}
````

#### Advanced usage: consistent view updates
In certain situations, it's necessary to maintain a consistent view of the tree while performing updates.
The `Tree` API allow you to take control of the internal `sync.Mutex` to prevent concurrent writes from
other threads. **Remember that all write operation should be run serially.**

In the following example, the `Upsert` function needs to perform a lookup on the tree to check if a handler
is already registered for the provided method and path. By locking the `Tree`, this operation ensures
atomicity, as it prevents other threads from modifying the tree between the lookup and the write operation.
Note that all read operation on the tree remain lock-free.
````go
func Upsert(t *fox.Tree, method, path string, handler fox.HandlerFunc) (*fox.Route, error) {
    t.Lock()
    defer t.Unlock()
    if t.Has(method, path) {
        return t.Update(method, path, handler)
    }
    return t.Handle(method, path, handler)
}
````

#### Concurrent safety and proper usage of Tree APIs
When working with the `Tree` API, it's important to keep some considerations in mind. Each instance has its 
own `sync.Mutex` that can be used to serialize writes. However, unlike the router API, the lower-level `Tree` API 
does not automatically lock the tree when writing to it. Therefore, it is the user's responsibility to ensure 
all writes are executed serially.

Moreover, since the router tree may be swapped at any given time, you MUST always copy the pointer locally to 
avoid inadvertently causing a deadlock by locking/unlocking the wrong `Tree`.

````go
// Good
t := r.Tree()
t.Lock()
defer t.Unlock()

// Dramatically bad, may cause deadlock
r.Tree().Lock()
defer r.Tree().Unlock()

// Dramatically bad, may cause deadlock
func handle(c fox.Context) {
    c.Fox().Tree().Lock()
    defer c.Fox().Tree().Unlock()
}
```` 

Note that `fox.Context` carries a local copy of the `Tree` that is being used to serve the handler, thereby eliminating 
the risk of deadlock when using the `Tree` within the context.
````go
// Ok
func handle(c fox.Context) {
    c.Tree().Lock()
    defer c.Tree().Unlock()
}
````

## Working with http.Handler
Fox itself implements the `http.Handler` interface which make easy to chain any compatible middleware before the router. Moreover, the router
provides convenient `fox.WrapF` and `fox.WrapH` adapter to be use with `http.Handler`.

The route parameters can be accessed by the wrapped handler through the `context.Context` when the adapters `fox.WrapF` and `fox.WrapH` are used.

Wrapping an `http.Handler`
```go
articles := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    params := fox.ParamsFromContext(r.Context())
    _, _ = fmt.Fprintf(w, "Article id: %s\n", params.Get("id"))
})

f := fox.New(fox.DefaultOptions())
f.MustHandle(http.MethodGet, "/articles/{id}", fox.WrapH(httpRateLimiter.RateLimit(articles)))
```

## Middleware
Middlewares can be registered globally using the `fox.WithMiddleware` option. The example below demonstrates how 
to create and apply automatically a simple logging middleware to all routes (including 404, 405, etc...).

````go
package main

import (
	"github.com/tigerwill90/fox"
	"log"
	"net/http"
	"time"
)

func Logger(next fox.HandlerFunc) fox.HandlerFunc {
	return func(c fox.Context) {
		start := time.Now()
		next(c)
		log.Printf("route: %s, latency: %s, status: %d, size: %d",
			c.Path(),
			time.Since(start),
			c.Writer().Status(),
			c.Writer().Size(),
		)
	}
}

func main() {
	f := fox.New(fox.WithMiddleware(Logger))

	f.MustHandle(http.MethodGet, "/", func(c fox.Context) {
		resp, err := http.Get("https://api.coindesk.com/v1/bpi/currentprice.json")
		if err != nil {
			http.Error(c.Writer(), http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		_ = c.Stream(http.StatusOK, fox.MIMEApplicationJSON, resp.Body)
	})

	log.Fatalln(http.ListenAndServe(":8080", f))
}
````

Additionally, `fox.WithMiddlewareFor` option provide a more fine-grained control over where a middleware is applied, such as
only for 404 or 405 handlers. Possible scopes include `fox.RouteHandlers` (regular routes), `fox.NoRouteHandler`, `fox.NoMethodHandler`, 
`fox.RedirectHandler`, `fox.OptionsHandler` and any combination of these.

````go
f := fox.New(
    fox.WithNoMethod(true),
    fox.WithMiddlewareFor(fox.RouteHandler, fox.Recovery(), Logger),
    fox.WithMiddlewareFor(fox.NoRouteHandler|fox.NoMethodHandler, SpecialLogger),
)
````

Finally, it's also possible to attaches middleware on a per-route basis. Note that route-specific middleware must be explicitly reapplied 
when updating a route. If not, any middleware will be removed, and the route will fall back to using only global middleware (if any).

````go
f := fox.New(
	fox.WithMiddleware(fox.Logger()),
)
f.MustHandle("GET", "/", SomeHandler, fox.WithMiddleware(foxtimeout.Middleware(2*time.Second)))
f.MustHandle("GET", "/foo", SomeOtherHandler)
````

### Official middlewares
* [tigerwill90/otelfox](https://github.com/tigerwill90/otelfox): Distributed tracing with [OpenTelemetry](https://opentelemetry.io/)
* [tigerwill90/foxdump](https://github.com/tigerwill90/foxdump): Body dump middleware for capturing requests and responses payload.
* [tigerwill90/foxtimeout](https://github.com/tigerwill90/foxtimeout): `http.TimeoutHandler` middleware optimized for Fox.
* [tigerwill90/foxwaf](https://github.com/tigerwill90/foxwaf): Coraza WAF middleware (experimental).
* [tigerwill90/foxgeoip](https://github.com/tigerwill90/foxgeoip): Block requests using GeoIP data based on client IP (experimental).

## Handling OPTIONS Requests and CORS Automatically
The `WithAutoOptions` setting or the `WithOptionsHandler` registration enable automatic responses to OPTIONS requests. 
This feature can be particularly useful in the context of Cross-Origin Resource Sharing (CORS).

An OPTIONS request is a type of HTTP request that is used to determine the communication options available for a given resource 
or API endpoint. These requests are commonly used as "preflight" requests in CORS to check if the CORS protocol is understood 
and to get permission to access data based on origin.

When automatic OPTIONS responses is enabled, the router will automatically respond to preflight OPTIONS requests and set the `Allow`
header with the appropriate value. To customize how OPTIONS requests are handled (e.g. adding CORS headers), you may register a middleware for the
`fox.OptionsHandler` scope or override the default handler.

````go
f := fox.New(
    fox.WithOptionsHandler(func(c fox.Context) {
        if c.Header("Access-Control-Request-Method") != "" {
            // Setting CORS headers.
            c.SetHeader("Access-Control-Allow-Methods", c.Writer().Header().Get("Allow"))
            c.SetHeader("Access-Control-Allow-Origin", "*")
        }

        // Respond with a 204 status code.
        c.Writer().WriteHeader(http.StatusNoContent)
    }),
)
````

## Client IP Derivation
The `WithClientIPStrategy` option allows you to set up strategies to resolve the client IP address based on your 
use case and network topology. Accurately determining the client IP is hard, particularly in environments with proxies or 
load balancers. For example, the leftmost IP in the `X-Forwarded-For` header is commonly used and is often regarded as the 
"closest to the client" and "most real," but it can be easily spoofed. Therefore, you should absolutely avoid using it 
for any security-related purposes, such as request throttling.

The strategy used must be chosen and tuned for your network configuration. This should result in the strategy never returning 
an error and if it does, it should be treated as an application issue or a misconfiguration, rather than defaulting to an 
untrustworthy IP.

The sub-package `github.com/tigerwill90/fox/strategy` provides a set of best practices strategies that should cover most use cases.

````go
f := fox.New(
    fox.DefaultOptions(),
    fox.WithClientIPStrategy(
        // We are behind one or many trusted proxies that have all private-space IP addresses.
        strategy.NewRightmostNonPrivate(strategy.XForwardedForKey),
    ),
)

f.MustHandle(http.MethodGet, "/foo/bar", func(c fox.Context) {
    ipAddr, err := c.ClientIP()
        if err != nil {
            // If the current strategy is not able to derive the client IP, an error
            // will be returned rather than falling back on an untrustworthy IP. It
            // should be treated as an application issue or a misconfiguration.
            panic(err)
        }
    fmt.Println(ipAddr.String())
})
````

It is also possible to create a chain with multiple strategies that attempt to derive the client IP, stopping when the first one succeeds.

````go
f = fox.New(
    fox.DefaultOptions(),
    fox.WithClientIPStrategy(
        // A common use for this is if a server is both directly connected to the 
        // internet and expecting a header to check.
        strategy.NewChain(
            strategy.NewLeftmostNonPrivate(strategy.ForwardedKey),
            strategy.NewRemoteAddr(),
        ),
    ),
)

````

Note that there is no "sane" default strategy, so calling `Context.ClientIP` without a strategy configured will return an `ErrNoClientIPStrategy`.

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
- [ ] Improving performance and polishing

## Contributions
This project aims to provide a lightweight, high performance and easy to use http router. It purposely has a limited set of features and exposes a relatively low-level api.
The intention behind these choices is that it can serve as a building block for implementing your own "batteries included" frameworks. Feature requests and PRs along these lines are welcome. 

## Acknowledgements
- [npgall/concurrent-trees](https://github.com/npgall/concurrent-trees): Fox design is largely inspired from Niall Gallagher's Concurrent Trees design.
- [julienschmidt/httprouter](https://github.com/julienschmidt/httprouter): some feature that implements Fox are inspired from Julien Schmidt's router. Most notably,
this package uses the optimized [httprouter.Cleanpath](https://github.com/julienschmidt/httprouter/blob/master/path.go) function.
- [realclientip/realclientip-go](https://github.com/realclientip/realclientip-go): Fox uses a derivative version of Adam Pritchard's `realclientip-go` library. 
See his insightful [blog post](https://adam-p.ca/blog/2022/03/x-forwarded-for/) on the topic for more details.
- The router API is influenced by popular routers such as [Gin](https://github.com/gin-gonic/gin) and [Echo](https://github.com/labstack/echo).
