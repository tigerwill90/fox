# Fox
Fox is a lightweight high performance HTTP request router for [Go](https://go.dev/). The main difference with other routers is
that it supports **mutation on its routing table while handling request concurrently**. Internally, Fox use a 
[Concurrent Radix Tree](https://github.com/npgall/concurrent-trees/blob/master/documentation/TreeDesign.md) that support lock-free 
reads while allowing concurrent writes.

The router is optimized for high performance and a small memory footprint. In many case, it does not do a single heap allocation.

## Features
**Routing mutation:** Register, update and remove route handler at any time without impacting the performance. Fox never block while serving
request!

**Wildcard pattern:** Route can be registered using wildcard parameters. The matched path segment can then be easily retrieved by 
name. Due to Fox design, wildcard route are cheap and scale really well.

**Detect panic:** You can register a fallback handler that is fire in case of panics occurring during handling a HTTP request.

**Only explicit matches:** Inspired from [httprouter](https://github.com/julienschmidt/httprouter), a request can only match
exactly one or no route. As a result, there are also no unintended matches, which makes it great for SEO and improves the 
user experience.

**Redirect trailing slashes:** Inspired from [httprouter](https://github.com/julienschmidt/httprouter), the router automatically 
redirects the client, at no extra cost, if another route match with or without a trailing slash (disable by default). 

**Path auto-correction:** Inspired from [httprouter](https://github.com/julienschmidt/httprouter), the router can remove superfluous path
elements like `../` or `//` and automatically redirect the client if the cleaned path match a handler (disable by default).

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
Since new route may be added at any given time, Fox, unlike other router, does not panic when a registered route is malformed or conflicts with another one. 
Instead, it's the user responsibility to handle them gracefully. 

#### Named parameters
A route can be defined using placeholder (e.g `:name`). The values are accessible via `fox.Params`, which is just a slice of `fox.Param`.
The `Get` method can is a helper to retrieve the value using the placeholder name.

Named parameter only match a single path segment.
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

**Note:** Since this router has only explicit matches, you can not register static routes and parameters for the same method and path segment.

#### Catch all parameter

#### Warning about params slice
`Params` slice is freed once ServeHTTP returns and may be reused later to save resource. Therefore, if you need to hold params `fox.Params`
longer, you have to copy it.
```go
func (h *HelloHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, params fox.Params) {
	p := make(fox.Params, len(params))
	copy(p, params)
	go func(){
		time.Sleep(1 * time.Second)
		p.Get("name") // Safe
	}()
	_, _ = fmt.Fprintf(w, "Hello %s\n", params.Get("name"))
}
```