package fox

import (
	"fmt"
	"net/http"
	"testing"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func Test_txn2_insert(t *testing.T) {
	f, _ := New()

	txn := tXn2{
		root: make(map[string]*node2),
	}
	/*	txn.insert("/api/{version}", &Route{})
		txn.insert("/api/{version}/users", &Route{})
		txn.insert("/api/{version}/posts", &Route{})*/
	txn.insert(http.MethodGet, must(f.NewRoute2("/foo", emptyHandler)))
	txn.insert(http.MethodGet, must(f.NewRoute2("/fob", emptyHandler)))
	txn.insert(http.MethodGet, must(f.NewRoute2("/fo", emptyHandler)))
	txn.insert(http.MethodGet, must(f.NewRoute2("/f{bar}", emptyHandler)))
	txn.insert(http.MethodGet, must(f.NewRoute2("/f{baz}/baz", emptyHandler)))
	txn.insert(http.MethodGet, must(f.NewRoute2("/f{yolo}/baz", emptyHandler)))
	txn.insert(http.MethodGet, must(f.NewRoute2("/f{yolo}/baz/{foo}", emptyHandler)))
	txn.insert(http.MethodGet, must(f.NewRoute2("/f{yolo}/baz/*{foo}", emptyHandler)))

	fmt.Println(txn.root[http.MethodGet])
	fmt.Println(txn.depth)
}

func Test_txn2_insertStatic(t *testing.T) {

	txn := tXn2{
		root: make(map[string]*node2),
	}

	for _, rte := range githubAPI {
		if rte.method != http.MethodGet {
			continue
		}
		txn.insert(rte.path, &Route{})
	}
	/*	for _, rte := range githubAPI {
		if rte.method != http.MethodGet {
			continue
		}
		route, ok := txn.delete(rte.path)
		assert.NotNil(t, route)
		assert.Truef(t, ok, rte.path)
	}*/

	fmt.Println(txn.root)
	fmt.Println(txn.depth)
}

func Test_tokenize(t *testing.T) {
	f, _ := New()
	tokens, _, _ := f.parseRoute2("/foo/{bar:(?:a)}/*{foo:[A-z]}/{bar}/boulou")
	for _, token := range tokens {
		fmt.Println(token.value)
	}
}

/*
func Test_searchNode(t *testing.T) {
	txn := tXn2{
		root: make(map[string]*node2),
	}
	for _, rte := range githubAPI {
		if rte.method != http.MethodGet {
			continue
		}
		txn.insert(rte.path, &Route{pattern: rte.path})
	}

	fmt.Println(txn.search("/users/{user}"))
}
*/
