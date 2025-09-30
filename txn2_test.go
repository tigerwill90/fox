package fox

import (
	"fmt"
	"testing"
)

func Test_txn2_insert(t *testing.T) {

	txn := tXn2{
		root: &node2{},
	}
	txn.Insert("/foobar", &Route{})
	txn.Insert("/foo", &Route{})
	/*	txn.Insert("/foo", &Route{})
		txn.Insert("/fob", &Route{})
		txn.Insert("/fo", &Route{})
		txn.Insert("/johndoe", &Route{})
		txn.Insert("/john{bar}", &Route{})
		txn.Insert("/john{baz}/bar", &Route{})*/

	fmt.Println(txn.root)
}

func Test_tokenize(t *testing.T) {

	segments, _ := tokenizePath("/foo/{bar}/*{bar}")
	fmt.Println(segments)
}
