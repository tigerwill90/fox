package fox

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
)

func TestX(t *testing.T) {
	f, _ := New(AllowRegexpParam(true))

	f.MustHandle(http.MethodGet, "/{iptv:(?:orange|sfr|free)-iptv}/track/", emptyHandler)
	f.MustHandle(http.MethodGet, "/{tvs:(?:orange|sfr|free)-as}/track/", emptyHandler)
	f.MustHandle(http.MethodGet, "/{chain}/track/", emptyHandler)
	f.MustHandle(http.MethodGet, "/{id}/foo/bar", emptyHandler)
	// f.MustHandle(http.MethodGet, "/foo/x", emptyHandler)
	tree := f.getTree()
	fmt.Println(tree.root[http.MethodGet])

	c := tree.pool.Get().(*cTx)
	*c.params = (*c.params)[:0]
	n, tsr := tree.lookup(http.MethodGet, "", "/orange-iptv/track", c, false)
	if n != nil {
		c.tsr = tsr
		c.route = n.route
		fmt.Println(n.route.Pattern())
		fmt.Println(slices.Collect(c.Params()))
	}
}

func TestY(t *testing.T) {
	f, _ := New()
	f.MustHandle(http.MethodGet, "{ab}.{c}.de{f}.com/foo/bar/*{bar}/x*{args}/y/*{z}/{b}", emptyHandler)
	tree := f.getTree()
	fmt.Println(tree.root[http.MethodGet])
	c := tree.pool.Get().(*cTx)
	*c.params = (*c.params)[:0]
	n, tsr := tree.lookup(http.MethodGet, "abab.cccc.deffff.com", "/foo/bar/a/b/c/xa/b/c/y/a/b/c/bbb", c, false)
	if n != nil {
		c.tsr = tsr
		c.route = n.route
		fmt.Println(tsr)
		fmt.Println(n.route.Pattern())
		fmt.Println(slices.Collect(c.Params()))
	}
}

func BenchmarkT(b *testing.B) {
	f, _ := New()
	f.MustHandle(http.MethodGet, "/{a}/{b}/{c}/{d}/{e}", emptyHandler)
	tree := f.getTree()
	c := tree.pool.Get().(*cTx)
	*c.params = (*c.params)[:0]
	n, tsr := tree.lookup(http.MethodGet, "", "/a/b/c/d/e", c, false)
	if n != nil {
		c.tsr = tsr
		c.route = n.route
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		for range c.Params() {
		}
	}
}
