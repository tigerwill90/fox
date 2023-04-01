package fox

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestX(t *testing.T) {
	tree := New().Tree()

	/*
		path: GET
		      path: /foo/ [paramIdx=1]
		          path: abc/{yolo} [leaf=/foo/abc/{yolo}]
		              path: /boom [leaf=/foo/abc/{yolo}/boom]
		          path: {yo}/{yolo} [leaf=/foo/{yo}/{yolo}]
		              path: /{id} [leaf=/foo/{yo}/{yolo}/{id}]
	*/

	require.NoError(t, tree.insert("GET", "/foo/abc", "", 1, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/abc/{yolo}", "", 1, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/{yo}/{yolo}", "", 1, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/abc/{yolo}/boom", "", 1, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/{yo}/{yolo}/{id}", "", 1, emptyHandler))

	nds := tree.load()
	fmt.Println(nds[0])
	// barr/
	n, ps, tsr := tree.lookup(nds[0], "/foo/abc/123/boom/", false)
	fmt.Println("matched")
	fmt.Println(n)
	fmt.Println(ps)
	fmt.Println(tsr)
}

func TestY(t *testing.T) {
	tree := New().Tree()

	/*
		path: GET
		      path: /foo/{ab} [leaf=/foo/{ab}]
		          path: /{bc} [leaf=/foo/{ab}/{bc}]
		              path: /{de} [leaf=/foo/{ab}/{bc}/{de}]
		                  path: / [paramIdx=1]
		                      path: boom [leaf=/foo/{ab}/{bc}/{de}/boom]
		                      path: {fg} [leaf=/foo/{ab}/{bc}/{de}/{fg}]
	*/

	require.NoError(t, tree.insert("GET", "/foo/{ab}", "", 1, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/{ab}/{bc}", "", 1, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/{ab}/{bc}/{de}", "", 3, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/{ab}/{bc}/{de}/boom", "", 3, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/{ab}/{bc}/{de}/{fg}", "", 1, emptyHandler))

	nds := tree.load()
	fmt.Println(nds[0])

	fmt.Println("depth =", tree.maxDepth.Load())

	// barr/
	n, ps, tsr := tree.lookup(nds[0], "/foo/ab/bc/de/boom", false)
	fmt.Println("matched")
	fmt.Println(n)
	fmt.Println(ps)
	fmt.Println(tsr)
}

func TestZ(t *testing.T) {
	tree := New().Tree()

	/*
		path: GET
		      path: /foo/ [paramIdx=1]
		          path: eee/ [paramIdx=1] [leaf=/foo/eee/]
		              path: baz/bar [leaf=/foo/eee/baz/bar]
		              path: {aa}/{yolo} [leaf=/foo/eee/{aa}/{yolo}]
		          path: {aa}/abc/foo [leaf=/foo/{aa}/abc/foo]
	*/

	require.NoError(t, tree.insert("GET", "/foo/{aa}/abc/foo", "", 1, emptyHandler))
	// require.NoError(t, tree.insert("GET", "/foo/eee/{aa}/yolo", "", 1, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/eee/{aa}/{yolo}", "", 1, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/eee/baz/bar/", "", 1, emptyHandler))
	require.NoError(t, tree.insert("GET", "/foo/eee/", "", 1, emptyHandler))

	nds := tree.load()
	fmt.Println(nds[0])

	fmt.Println("depth =", tree.maxDepth.Load())

	n, ps, tsr := tree.lookup(nds[0], "/foo/er/abc/foo", false)
	fmt.Println("matched")
	fmt.Println(n)
	fmt.Println(ps)
	fmt.Println(tsr)
}

func TestA(t *testing.T) {
	tree := New().Tree()
	for _, route := range overlappingRoutes {
		require.NoError(t, tree.Handler(route.method, route.path, HandlerFunc(func(w http.ResponseWriter, r *http.Request, p Params) {})))
	}

	/**
	path: GET
	      path: /foo/ [paramIdx=1]
	          path: abc/id:{id}/xyz [leaf=/foo/abc/id:{id}/xyz]
	          path: {name}/id:{id}/ [paramIdx=1]
	              path: xyz [leaf=/foo/{name}/id:{id}/xyz]
	              path: {name} [leaf=/foo/{name}/id:{id}/{name}]
	*/

	nds := tree.load()
	fmt.Println(nds[0])
	n, ps, tsr := tree.lookup(nds[0], "/foo/ab/id:123/xyz", false)
	fmt.Println("matched")
	fmt.Println(n)
	fmt.Println(ps)
	fmt.Println(tsr)

}

func TestO(t *testing.T) {
	tree := New().Tree()

	/*
		path: GET
		      path: /{foo}/ [paramIdx=1]
		          path: eee/ [paramIdx=0] [leaf=/{foo}/eee/]
		              path: {aa}/foo/ba
		                  path: r [leaf=/{foo}/eee/{aa}/foo/bar]
		                  path: z [leaf=/{foo}/eee/{aa}/foo/baz]
		          path: {aa}/abc/foo [leaf=/{foo}/{aa}/abc/foo]
	*/

	require.NoError(t, tree.Handler("GET", "/{foo}/{aa}/abc/foo", emptyHandler))
	// require.NoError(t, tree.insert("GET", "/foo/eee/{aa}/yolo", "", 1, emptyHandler))
	require.NoError(t, tree.Handler("GET", "/{foo}/eee/{aa}/foo/bar", emptyHandler))
	require.NoError(t, tree.Handler("GET", "/{foo}/eee/{aa}/foo/baz", emptyHandler))
	require.NoError(t, tree.Handler("GET", "/{foo}/eee/", emptyHandler))

	nds := tree.load()
	fmt.Println(nds[0])

	fmt.Println("depth =", tree.maxDepth.Load())

	n, ps, tsr := tree.lookup(nds[0], "/foo/eee/abc/foo", false)
	fmt.Println("matched")
	fmt.Println(n)
	fmt.Println(ps)
	fmt.Println(tsr)
}

func BenchmarkO(b *testing.B) {
	r := New()
	require.NoError(b, r.Tree().Handler("GET", "/{foo}/{aa}/abc/foo", emptyHandler))
	require.NoError(b, r.Tree().Handler("GET", "/{foo}/eee/{aa}/yep", emptyHandler))
	require.NoError(b, r.Tree().Handler("GET", "/{foo}/eee/baz/bar/", emptyHandler))
	require.NoError(b, r.Tree().Handler("GET", "/{foo}/eee/", emptyHandler))

	b.ReportAllocs()
	b.ResetTimer()

	tree := r.Tree()
	for i := 0; i < b.N; i++ {
		_, ps, _ := Lookup(tree, "GET", "/foo/eee/abc/foo", false)
		if ps != nil {
			ps.Free(tree)
		}
	}
}
