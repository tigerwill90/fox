package fox

import (
	"fmt"
	"net/http"
	"testing"
)

func TestX(t *testing.T) {
	f, _ := New()

	f.MustHandle(http.MethodGet, "/foo", emptyHandler)
	f.MustHandle(http.MethodGet, "/foo/b", emptyHandler)
	f.MustHandle(http.MethodGet, "/foo/x", emptyHandler)
	// f.MustHandle(http.MethodGet, "/foo/x", emptyHandler)
	tree := f.getTree()
	fmt.Println(tree.root[http.MethodGet])

	c := tree.pool.Get().(*cTx)
	n, tsr := tree.lookup(http.MethodGet, "", "/foo/", c, false)
	fmt.Println(n, tsr)
}
