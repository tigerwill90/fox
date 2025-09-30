package fox

import (
	"fmt"
	"testing"
)

func Test_txn2_insert(t *testing.T) {

	txn := tXn2{
		root: &node2{},
	}
	txn.insert("/api/{version}", &Route{})
	txn.insert("/api/{version}/users", &Route{})
	txn.insert("/api/{version}/posts", &Route{})
	/*	txn.insert("/foo", &Route{})
		txn.insert("/fob", &Route{})
		txn.insert("/fo", &Route{})
		txn.insert("/f{bar}", &Route{})
		txn.insert("/f{bar}/baz", &Route{})*/

	fmt.Println(txn.root)

	txn.delete("/api/{version}/posts")
	fmt.Println(txn.root)
}

func Test_tokenize(t *testing.T) {

	segments, _ := tokenizeKey("/foo/{bar}/*{bar}")
	fmt.Println(segments)
}
