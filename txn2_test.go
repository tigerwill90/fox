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

	segments, _ := tokenizeKey("/foo/{bar}/*{bar}")
	fmt.Println(segments)
}
