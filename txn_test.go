package fox

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"slices"
	"testing"
)

func TestXyz(t *testing.T) {
	f := New()
	f.MustHandle(http.MethodGet, "a.b.c/a", emptyHandler)
	/*	f.MustHandle(http.MethodGet, "/a", emptyHandler)
		f.MustHandle(http.MethodGet, "/a/b", emptyHandler)
		f.MustHandle(http.MethodGet, "/a/b/c", emptyHandler)
		f.MustHandle(http.MethodGet, "/a/b/c/d", emptyHandler)
		f.MustHandle(http.MethodGet, "/a/b/c/d/e", emptyHandler)
		f.MustHandle(http.MethodGet, "/a/b/c/d/e/f", emptyHandler)*/

	tree := f.Tree()

	txn := f.Txn()
	defer txn.Rollback()

	require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/a/b", emptyHandler)))
	require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/a/b/c", emptyHandler)))
	require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/a/b/c/d", emptyHandler)))
	require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/a/b/c/d/e", emptyHandler)))
	require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/a/b/c/d/e/f", emptyHandler)))
	require.NoError(t, txn.Delete(http.MethodGet, "a.b.c/a"))
	fmt.Println(txn.Has(http.MethodGet, "/a/b/c/d"))

	fmt.Println("current", (*tree.root.Load())[0])

	fmt.Println("isolated", (*txn.snap.root.Load())[0])
	txn.Commit()

	fmt.Println("committed", (*tree.root.Load())[0])

}

func TestZ(t *testing.T) {
	f := New()
	f.MustHandle(http.MethodGet, "a.b.c/a", emptyHandler)
	tree := f.Tree()
	txn := f.Txn()
	defer txn.Rollback()

	require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/a/b", emptyHandler)))
	require.NoError(t, onlyError(txn.Handle(http.MethodPost, "/a/b/c", emptyHandler)))
	require.NoError(t, onlyError(txn.Handle(http.MethodConnect, "/a/b/c/d", emptyHandler)))
	require.NoError(t, onlyError(txn.Handle(http.MethodOptions, "/a/b/c/d/e", emptyHandler)))
	require.NoError(t, onlyError(txn.Handle(http.MethodTrace, "/a/b/c/d/e/f", emptyHandler)))
	txn.Truncate(slices.Collect(txn.Iter().Methods())...)
	for _, n := range *txn.snap.root.Load() {
		fmt.Println("isolated", n)
	}

	fmt.Println("current", (*tree.root.Load())[0])

}

func BenchmarkTx(b *testing.B) {
	f := New()

	for _, route := range staticRoutes {
		f.MustHandle(route.method, route.path, emptyHandler)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		txn := f.Txn()
		txn.Delete(http.MethodGet, "/go1compat.html")
		txn.Delete(http.MethodGet, "/articles/wiki/part1-noerror.go")
		txn.Delete(http.MethodGet, "/gopher/gophercolor16x16.png")
		txn.Handle(http.MethodGet, "/go1compat.html", emptyHandler)
		txn.Handle(http.MethodGet, "/articles/wiki/part1-noerror.go", emptyHandler)
		txn.Handle(http.MethodGet, "/gopher/gophercolor16x16.png", emptyHandler)
		txn.Commit()
	}
}
