package fox

import (
	"fmt"
	"net/http"
	"testing"
)

func TestX(t *testing.T) {
	f, _ := New()

	f.MustHandle(http.MethodGet, "/{a}/{b}/{c}/{d}/{e}", emptyHandler)
	// f.MustHandle(http.MethodGet, "/foo/x", emptyHandler)
	tree := f.getTree()
	fmt.Println(tree.root[http.MethodGet])

	c := tree.pool.Get().(*cTx)
	n, tsr := tree.lookup(http.MethodGet, "", "/a/b/c/d/e", c, false)
	fmt.Println(n, tsr)
}

func BenchmarkT(b *testing.B) {
	f, _ := New()
	f.MustHandle(http.MethodGet, "/{a}/{b}/{c}/{d}/{e}/{f}/{g}/{h}/{i}/{j}/{k}/{l}/{m}/{n}/{o}/{p}/{q}/{r}/{s}/{t}", emptyHandler)
	tree := f.getTree()
	c := tree.pool.Get().(*cTx)

	b.ReportAllocs()

	for b.Loop() {
		*c.params = (*c.params)[:0]
		_, _ = tree.lookup(http.MethodGet, "", "/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t", c, false)
	}
}
