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

func Test_lookup(t *testing.T) {
	f, _ := New()
	tree := f.newTree2()
	txn := tree.txn()

	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/*{args}", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/*{args}/{bar}", emptyHandler)), modeInsert))
	assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/*{args}/{bar}", emptyHandler)), modeInsert))

	// assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foo/*{a:[0-9]+}", emptyHandler)), modeInsert))

	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/{a}/b{t:[A-z_]+}/yolo", emptyHandler)), modeInsert))
	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/{a}/b{t:[A-z]+}/yolo", emptyHandler)), modeInsert))
	//assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/{a}/b{t:[a-z-]+}/yolo", emptyHandler)), modeInsert))

	// assert.NoError(t, txn.insert(http.MethodGet, must(f.NewRoute2("/foc/bar", emptyHandler)), modeInsert))
	tree = txn.commit()
	fmt.Println(tree.root[http.MethodGet])

	c := tree.pool.Get().(*cTx)
	n, _ := tree.lookupByPath(tree.root[http.MethodGet], "/foo/a/b/c/bar", c, false)
	if n != nil {
		fmt.Println(n.route)
		fmt.Println(c.params2)
	}
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

	/*	target := must(f.NewRoute2("example.com/foobar", emptyHandler))
		txn.delete(http.MethodGet, target.tokens)

		fmt.Println(txn.root[http.MethodGet])*/
}

func TestY(t *testing.T) {

	cases := []struct {
		name   string
		routes []struct {
			path string
		}
	}{
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
	tokens, _, _, _ := f.parseRoute2("a.b.c/fox/bar")
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
