package fox

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
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

	txn := tree.Txn()
	require.NoError(t, txn.insert(http.MethodGet, &Route{pattern: "/a/b"}, 0))
	require.NoError(t, txn.insert(http.MethodGet, &Route{pattern: "/a/b/c"}, 0))
	require.NoError(t, txn.insert(http.MethodGet, &Route{pattern: "/a/b/c/d"}, 0))
	require.NoError(t, txn.insert(http.MethodGet, &Route{pattern: "/a/b/c/d/e"}, 0))
	require.NoError(t, txn.insert(http.MethodGet, &Route{pattern: "/a/b/c/d/e/f"}, 0))
	// fmt.Println(txn.remove(http.MethodGet, "a.b.c/a"))

	fmt.Println("current", (*tree.nodes.Load())[0])

	fmt.Println("txn", (*txn.root.Load())[0])
}
