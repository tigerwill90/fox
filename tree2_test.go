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

func TestZ(t *testing.T) {
	f, _ := New()
	tree := f.newTree2()
	txn := tree.txn()

	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("a.b/bar/", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("a.b/bar", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("a.c/bar", emptyHandler)), modeInsert))

	tree = txn.commit()
	fmt.Println(tree.root[http.MethodGet])

	target := must(f.NewRoute2("a.b/bar/", emptyHandler))
	txn.delete(http.MethodGet, target.tokens)
	fmt.Println(txn.root[http.MethodGet])
}

func Test_lookup(t *testing.T) {
	f, _ := New()
	tree := f.newTree2()
	txn := tree.txn()

	// assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/{a}/foo/", emptyHandler)), modeInsert))
	// assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/{arg}", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/hello", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/bar/", emptyHandler)), modeInsert))
	// assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/bar", emptyHandler)), modeInsert))

	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/a/b", emptyHandler)), modeInsert))
	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/c/", emptyHandler)), modeInsert))

	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/b/", emptyHandler)), modeInsert))
	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/x/", emptyHandler)), modeInsert))

	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/*{x:[a-z]+}/{a}/b/c/barr", emptyHandler)), modeInsert))
	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/*{test}/a/{b}/c/barr", emptyHandler)), modeInsert))
	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/*{args}/bar", emptyHandler)), modeInsert))
	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/*{args}/{bar}/", emptyHandler)), modeInsert))

	// assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/*{a:[0-9]+}", emptyHandler)), modeInsert))

	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/{a}/b{t:[A-z_]+}/yolo", emptyHandler)), modeInsert))
	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/{a}/b{t:[A-z]+}/yolo", emptyHandler)), modeInsert))
	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/{a}/b{t:[a-z-]+}/yolo", emptyHandler)), modeInsert))

	// assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foc/bar", emptyHandler)), modeInsert))
	tree = txn.commit()
	fmt.Println(tree.root[http.MethodGet])

	c := tree.pool.Get().(*cTx)
	n, _ := tree.lookupByPath(tree.root[http.MethodGet], "/foo/bar", c, false)
	if n != nil {
		fmt.Println(n.route)
		fmt.Println(c.params2)
		fmt.Println(c.tsrParams2)
	}

	target := must(f.NewRoute2("/foo/bar", emptyHandler))
	txn.delete(http.MethodGet, target.tokens)

	fmt.Println(txn.root[http.MethodGet])
}

func Test_txn2_insert(t *testing.T) {
	f, _ := New()

	txn := tXn2{
		root: make(map[string]*node2),
	}
	/*	txn.insert("/api/{version}", &Route{})
		txn.insert("/api/{version}/users", &Route{})
		txn.insert("/api/{version}/posts", &Route{})*/

	// assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("example.{bar}/bar", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("a.b.{a}/", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("a.b.{b}/{c}", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("a.b.{b}/*{c}", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodPost, must(f.NewRoute2("example.com/{a:A}", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodPost, must(f.NewRoute2("example.com/{b:B}", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodPost, must(f.NewRoute2("example.com/{c:C}", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodPost, must(f.NewRoute2("example.com/{d:D}", emptyHandler)), modeInsert))

	/*	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("a.b.c/", emptyHandler)), modeInsert))
		assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo", emptyHandler)), modeInsert))
		assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/fob", emptyHandler)), modeInsert))
		assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/fo", emptyHandler)), modeInsert))
		assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/f{bar}", emptyHandler)), modeInsert))
		assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/f{baz}/baz", emptyHandler)), modeInsert))
		assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/f{yolo}/baz", emptyHandler)), modeInsert))
		assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/f{yolo}/damn", emptyHandler)), modeInsert))
		assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foobar/{test}/a", emptyHandler)), modeInsert))
		assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foobar/{one:[A-z]+}/b", emptyHandler)), modeInsert))
		assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foobar/{two:[0-9]+}/c", emptyHandler)), modeInsert))*/

	// assert.NoError(t, txn.insert(http.MethodDelete, must(f.NewRoute2("/f{yolo}/baz/{foo}", emptyHandler)), modeUpdate))
	fmt.Println(txn.root[http.MethodGet])
	fmt.Println(txn.maxDepth)
	fmt.Println(txn.maxParams)
	fmt.Println(txn.size)

	target := must(f.NewRoute2("example.com/foobar", emptyHandler))
	txn.delete(http.MethodGet, target.tokens)

	fmt.Println(txn.root[http.MethodGet])
}

func TestY(t *testing.T) {

	cases := []struct {
		name   string
		routes []struct {
			path string
		}
	}{
		{
			name: "remove slash node, should merge",
			routes: []struct {
				path string
			}{
				{path: "a.b/bar"},
				{path: "a.b/foo"},
			},
		},
		{
			name: "remove slash node, should merge",
			routes: []struct {
				path string
			}{
				{path: "/foo/bar/"},
				{path: "/foo/bar/baz"},
			},
		},
		{
			name: "test delete with merge",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/{foo}/{bar}"},
				{path: "a.b.c.d/{foo}/{bar}"},
				{path: "a.b.c{d}/{foo}/{bar}"},
				{path: "a.b/"},
			},
		},
		{
			name: "test delete with merge",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
				{path: "a.b/"},
			},
		},
		{
			name: "test delete with merge",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/f"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c.d/f"},
				{path: "a.b.c.d/fox"},
				{path: "a.b.c{d}/fox/bar"},
				{path: "a.e.c{d}/fox/bar"},
				{path: "/johnny"},
				{path: "/j"},
				{path: "/x"},
				{path: "a.b/"},
			},
		},
		{
			name: "test delete with merge ppp root",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
			},
		},
		{
			name: "test delete with merge pp root",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
			},
		},
		{
			name: "simple insert and delete",
			routes: []struct {
				path string
			}{
				{path: "a.x.x/"},
			},
		},
		{
			name: "simple insert and delete",
			routes: []struct {
				path string
			}{
				{path: "a.x.x/"},
				{path: "a.x.y/"},
			},
		},
		{
			name: "simple insert and delete",
			routes: []struct {
				path string
			}{
				{path: "aaa/"},
				{path: "aaab/"},
				{path: "aaabc/"},
			},
		},
		{
			name: "test delete with merge ppp root",
			routes: []struct {
				path string
			}{
				{path: "a.b.c/foo/bar"},
				{path: "a.b.c/foo/ba"},
				{path: "a.b.c/foo"},
				{path: "a.b.c/x"},
				{path: "a.b.c.d/foo/bar"},
				{path: "a.b.c{d}/foo/bar"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := New()

			txn := tXn2{
				root: make(map[string]*node2),
			}

			for _, rte := range tc.routes {
				assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2(rte.path, emptyHandler)), modeInsert))
			}
			fmt.Println(txn.root[http.MethodGet])

			for _, rte := range tc.routes {
				r := must(f.NewRoute2(rte.path, emptyHandler))
				_, deleted := txn.delete(http.MethodGet, r.tokens)
				fmt.Println("Deleted:", rte.path)
				fmt.Println(txn.root[http.MethodGet])
				fmt.Println()
				assert.True(t, deleted)
			}
		})
	}

}

func Test_txn2_insertStatic(t *testing.T) {

	f, _ := New()
	txn := tXn2{
		root: make(map[string]*node2),
	}

	for _, rte := range githubAPI {
		assert.NoError(t, txn.insert(rte.method, must(f.NewRoute2(rte.path, emptyHandler)), modeInsert))
	}
	for _, rte := range githubAPI {
		r := must(f.NewRoute2(rte.path, emptyHandler))
		route, ok := txn.delete(rte.method, r.tokens)
		assert.NotNil(t, route)
		assert.Truef(t, ok, rte.path)
	}

	fmt.Println(txn.root)
	fmt.Println(txn.maxDepth)
}

func Test_tokenize(t *testing.T) {
	f, _ := New()
	tokens, _, _, _ := f.parseRoute2("/hello")
	fmt.Println(tokens)
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

// BenchmarkStatic-16    	  177091	      6184 ns/op	       0 B/op	       0 allocs/op
// BenchmarkStatic-16    	  194583	      5248 ns/op	       0 B/op	       0 allocs/op
// BenchmarkStatic-16    	  195931	      5393 ns/op	       0 B/op	       0 allocs/op
func BenchmarkStatic(b *testing.B) {
	f, _ := New()
	tree := f.newTree2()
	txn := tree.txn()

	for _, rte := range staticRoutes {
		assert.NoError(b, txn.insert(rte.method, must(f.NewRoute2(rte.path, emptyHandler)), modeInsert))
	}
	tree = txn.commit()

	root := tree.root[http.MethodGet]

	c := tree.pool.Get().(*cTx)
	b.ReportAllocs()
	for b.Loop() {
		for _, rte := range staticRoutes {
			*c.params2 = (*c.params2)[:0]
			*c.tsrParams2 = (*c.tsrParams2)[:0]
			_, _ = tree.lookupByPath(root, rte.path, c, false)
		}
	}
}
