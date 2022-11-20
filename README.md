# Fox
Fox is a lightweight high performance HTTP request router for [Go](https://go.dev/). The main difference with other routers is
that it supports **mutation on its routing table while handling request concurrently**. Internally, Fox use a 
[Concurrent Radix Tree](https://github.com/npgall/concurrent-trees/blob/master/documentation/TreeDesign.md) that support **lock-free 
reads** while allowing **concurrent writes**.

The router tree is optimized for high-concurrency and high performance reads, and low-concurrency write. Fox has a small memory footprint, and 
in many case, it does not do a single heap allocation while handling request.

## Disclaimer
The current api is not yet stabilize. Breaking change may happen before `v1.0.0`

## Features
**Routing mutation:** Register, update and remove route handler safely at any time without impact on performance. Fox never block while serving
request!

**Wildcard pattern:** Route can be registered using wildcard parameters. The matched path segment can then be easily retrieved by 
name. Due to Fox design, wildcard route are cheap and scale really well.

**Detect panic:** You can register a fallback handler that is fire in case of panics occurring during handling an HTTP request.

**Get the current route:** You can easily retrieve the route for the current matched request. This actually makes it easier to integrate
observability middleware like open telemetry (disable by default).

**Only explicit matches:** Inspired from [httprouter](https://github.com/julienschmidt/httprouter), a request can only match
exactly one or no route. As a result, there are also no unintended matches, which makes it great for SEO and improves the 
user experience.

**Redirect trailing slashes:** Inspired from [httprouter](https://github.com/julienschmidt/httprouter), the router automatically 
redirects the client, at no extra cost, if another route match with or without a trailing slash (disable by default). 

**Path auto-correction:** Inspired from [httprouter](https://github.com/julienschmidt/httprouter), the router can remove superfluous path
elements like `../` or `//` and automatically redirect the client if the cleaned path match a handler (disable by default).

Of course, you can also register custom `NotFound` and `MethodNotAllowed` handlers.

## Getting started
### Installation
```shell
go get -u tigerwill90/github.com/fox
```

### Basic example
````go
package main

import (
	"fmt"
	"github.com/tigerwill90/fox"
	"log"
	"net/http"
)

type HelloHandler struct {}

func (h *HelloHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, params fox.Params) {
	_, _ = fmt.Fprintf(w, "Hello %s\n", params.Get("name"))
}

func main() {
	r := fox.New()

	Must(r.Get("/", fox.HandlerFunc(func(w http.ResponseWriter, r *http.Request, params fox.Params) {
		_, _ = fmt.Fprint(w, "Welcome!\n")
	})))
	Must(r.Get("/hello/:name", new(HelloHandler)))
	
	log.Fatalln(http.ListenAndServe(":8080", r))
}

func Must(err error) {
	if err != nil {
		panic(err)
	}
}
````
#### Error handling
Since new route may be added at any given time, Fox, unlike other router, does not panic at registration when a route is malformed or conflicts with another one. 
Instead, it returns the following error type
```go
ErrRouteNotFound = errors.New("route not found")
ErrRouteExist    = errors.New("route already registered")
ErrRouteConflict = errors.New("route conflict")
ErrInvalidRoute  = errors.New("invalid route")
```

Conflict error may be unwrapped to retrieve conflicting route.
```go
if errC := err.(*fox.RouteConflictError); ok {
    for _, route := range errC.Matched {
        fmt.Println(route)
    }
}
```

#### Named parameters
A route can be defined using placeholder (e.g `:name`). The values are accessible via `fox.Params`, which is just a slice of `fox.Param`.
The `Get` method is a helper to retrieve the value using the placeholder name.

```
Pattern /avengers/:name

/avengers/ironman       match
/avengers/thor          match
/avengers/hulk/angry    no match
/avengers/              no match

Pattern /users/uuid_:id

/users/uuid_xyz         match
/users/uuid             no match
```

#### Catch all parameter
Catch-all parameters can be used to match everything at the end of a route. The placeholder start with `*` followed by a name.
```
Pattern /src/*filepath

/src/                   match
/src/conf.txt           match
/src/dir/config.txt     match
```

#### Warning about params slice
`Params` slice is freed once ServeHTTP returns and may be reused later to save resource. Therefore, if you need to hold `fox.Params`
longer, you have to copy it.
```go
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, params fox.Params) {
	p := params.Clone()
	go func(){
		time.Sleep(1 * time.Second)
		log.Println(p.Get("name")) // Safe
	}()
	_, _ = fmt.Fprintf(w, "Hello %s\n", params.Get("name"))
}
```

### Adding, updating and removing route
In this is example, the handler for `route/:action` allow to dynamically register, update and remove handler for the given route and method.
Due to Fox design, those actions are perfectly safe and can be executed concurrently. Even better, it does not block the router 
from handling request in parallel.

```go
type ActionHandler struct {
	fox *fox.Router
}

func (h *ActionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, params fox.Params) {
	path := r.URL.Query().Get("path")
	text := r.URL.Query().Get("text")
	method := strings.ToUpper(r.URL.Query().Get("method"))
	if text == "" || path == "" || method == "" {
		http.Error(w, "missing required query parameters", http.StatusBadRequest)
		return
	}

	var err error
	action := params.Get("action")
	switch action {
	case "add":
		err = h.fox.Handler(method, path, fox.HandlerFunc(func(w http.ResponseWriter, r *http.Request, params fox.Params) {
			_, _ = fmt.Fprintln(w, text)
		}))
	case "update":
		err = h.fox.Update(method, path, fox.HandlerFunc(func(w http.ResponseWriter, r *http.Request, params fox.Params) {
			_, _ = fmt.Fprintln(w, text)
		}))
	case "delete":
		err = h.fox.Remove(method, path)
	default:
		http.Error(w, fmt.Sprintf("action %q is not allowed", action), http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func main() {
	r := fox.New()
	Must(r.Get("/routes/:action", &ActionHandler{fox: r}))
	log.Fatalln(http.ListenAndServe(":8080", r))
}

func Must(err error) {
	if err != nil {
		panic(err)
	}
}
```

## Working with http.Handler
Fox itself implements the `http.Handler` interface which make easy to chain any compatible middleware before the router. Moreover, the router
provides convenient `fox.WrapF` and `fox.WrapH` adapter to be use with `http.Handler`. Named and catch all parameters are forwarded via the
request context
```go
_ = r.Get("/users/:id", fox.WrapF(func(w http.ResponseWriter, r *http.Request) {
    params := fox.ParamsFromContext(r.Context())
    fmt.Fprintf(w, "user id: %s\n", params.Get("id"))
}))
```

## TODO
- [ ] Iterator (method, prefix, suffix)
- [ ] Batch write (aka the transaction api)
- [ ] Alloc optimization on tree write
- [ ] Automatic OPTIONS responses and CORS ?