package fox

import (
	"fmt"
	"net/http"
	"testing"
)

func TestX(t *testing.T) {
	f, _ := New()
	f.MustHandle(http.MethodGet, "/foo/{foo}", emptyHandler)
	_, err := f.Delete(http.MethodGet, "/foo/{foo}")
	fmt.Println(err)
	tree := f.getRoot()
	fmt.Println(tree.root[http.MethodGet])
}
