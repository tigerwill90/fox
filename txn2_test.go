package fox

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
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
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/fob", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/fo", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/f{bar}", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/f{baz}/baz", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/f{yolo}/baz", emptyHandler)), modeInsert))
	// assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/f{yolo}/baz/{foo}", emptyHandler)), modeInsert))
	// assert.NoError(t, txn.insert(http.MethodDelete, must(f.NewRoute2("/f{yolo}/baz/{foo}", emptyHandler)), modeUpdate))

	fmt.Println(txn.root[http.MethodGet])
	fmt.Println(txn.depth)

	target := must(f.NewRoute2("/f{yolo}/baz/{foo}", emptyHandler))
	txn.delete(http.MethodGet, target.tokens)

	fmt.Println(txn.root[http.MethodGet])
}

func Test_txn2_insertStatic(t *testing.T) {

	txn := tXn2{
		root: make(map[string]*node2),
	}

	for _, rte := range githubAPI {
		if rte.method != http.MethodGet {
			continue
		}
		txn.insert(rte.path, &Route{}, modeInsert)
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
	n := &node2{}
	n.addParamEdge(&node2{key: "?"})
	n.addParamEdge(&node2{key: "af"})
	n.addParamEdge(&node2{key: "gf"})

	fmt.Println(n.params)
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
