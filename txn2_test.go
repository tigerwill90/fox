package fox

import (
	"fmt"
	"net/http"
	"testing"
)

func Test_txn2_insert(t *testing.T) {

	txn := tXn2{
		root: &node2{},
	}
	/*	txn.insert("/api/{version}", &Route{})
		txn.insert("/api/{version}/users", &Route{})
		txn.insert("/api/{version}/posts", &Route{})*/
	txn.insert("/foo", &Route{})
	txn.insert("/fob", &Route{})
	txn.insert("/fo", &Route{})
	txn.insert("/f{bar}", &Route{})
	txn.insert("/f{baz}/baz", &Route{})
	txn.insert("/f{yolo}/baz", &Route{})
	txn.insert("/f{yolo}/baz/{foo}", &Route{})
	txn.insert("/f{yolo}/baz/{foo...}", &Route{})

	fmt.Println(txn.root)
	fmt.Println(txn.depth)
}

func Test_txn2_insertStatic(t *testing.T) {

	txn := tXn2{
		root: &node2{},
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

func Test_searchNode(t *testing.T) {
	txn := tXn2{
		root: &node2{},
	}
	for _, rte := range githubAPI {
		if rte.method != http.MethodGet {
			continue
		}
		txn.insert(rte.path, &Route{pattern: rte.path})
	}

	fmt.Println(txn.root.search("/users/{user}"))
}
