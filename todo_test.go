package fox

import (
	"fmt"
	"net/http"
	"testing"
)

func TestX(t *testing.T) {
	f, _ := New()

	f.MustHandle(http.MethodGet, "/{a}/{b}/{c}/{d}", emptyHandler)
	// f.MustHandle(http.MethodGet, "/foo/x", emptyHandler)
	tree := f.getTree()
	fmt.Println(tree.root[http.MethodGet])

	c := tree.pool.Get().(*cTx)
	n, tsr := tree.lookup(http.MethodGet, "", "/a/b/c/d", c, false)
	fmt.Println(n, tsr)
}
