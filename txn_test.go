package fox

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestTxn_Truncate(t *testing.T) {
	cases := []struct {
		name   string
		routes []struct {
			method string
			path   string
		}
	}{
		{
			name: "common verb node should have a root and no children",
			routes: []struct {
				method string
				path   string
			}{
				{method: http.MethodGet, path: "/foo/bar"},
				{method: http.MethodGet, path: "/foo"},
				{method: http.MethodPost, path: "/foo/bar"},
				{method: http.MethodPost, path: "/foo"},
				{method: http.MethodPut, path: "/foo/bar"},
				{method: http.MethodPut, path: "/foo"},
				{method: http.MethodDelete, path: "/foo/bar"},
				{method: http.MethodDelete, path: "/foo"},
			},
		},
		{
			name: "not common verb should be removed entirely",
			routes: []struct {
				method string
				path   string
			}{
				{method: http.MethodTrace, path: "/foo/bar"},
				{method: http.MethodTrace, path: "/foo"},
				{method: http.MethodPost, path: "/foo/bar"},
				{method: http.MethodPost, path: "/foo"},
				{method: http.MethodPut, path: "/foo/bar"},
				{method: http.MethodPut, path: "/foo"},
				{method: http.MethodOptions, path: "/foo/bar"},
				{method: http.MethodOptions, path: "/foo"},
			},
		},
		{
			name: "custom verb should be removed entirely",
			routes: []struct {
				method string
				path   string
			}{
				{method: http.MethodGet, path: "/foo/bar"},
				{method: http.MethodGet, path: "/foo"},
				{method: http.MethodPost, path: "/foo/bar"},
				{method: http.MethodPost, path: "/foo"},
				{method: http.MethodPut, path: "/foo/bar"},
				{method: http.MethodPut, path: "/foo"},
				{method: "BOULOU", path: "/foo/bar"},
				{method: "BOULOU", path: "/foo"},
				{method: http.MethodDelete, path: "/foo/bar"},
				{method: http.MethodDelete, path: "/foo"},
				{method: "ANY", path: "/foo/bar"},
				{method: "ANY", path: "/foo"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := New()
			for _, rte := range tc.routes {
				require.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
			}
			txn := f.Txn(true)
			defer txn.Abort()

			methods := make([]string, 0)
			for _, rte := range tc.routes {
				methods = append(methods, rte.method)
			}

			if assert.NoError(t, txn.Truncate(methods...)) {
				txn.Commit()
			}

			tree := f.getRoot()
			for _, method := range methods {
				idx := tree.root.methodIndex(method)
				if isRemovable(method) {
					assert.Equal(t, idx, -1)
				} else {
					assert.Len(t, tree.root[idx].children, 0)
				}
			}
			assert.Len(t, tree.root, len(commonVerbs))
		})
	}
}

func TestTxn_TruncateAll(t *testing.T) {
	f := New()
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodPost, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodDelete, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodPut, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodConnect, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodTrace, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Handle("BOULOU", "/foo/bar", emptyHandler)))

	txn := f.Txn(true)
	defer txn.Abort()

	if assert.NoError(t, txn.Truncate()) {
		txn.Commit()
	}

	tree := f.getRoot()
	assert.Len(t, tree.root, len(commonVerbs))
	for _, n := range tree.root {
		assert.Len(t, n.children, 0)
	}
}

func TestX(t *testing.T) {
	f := New()

	f.Updates(func(txn *Txn) error {
		require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ab", emptyHandler)))
		require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ab/cd", emptyHandler)))
		require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ab/cd/ef", emptyHandler)))
		require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ab/cd/ef/gh", emptyHandler)))
		require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ab/cd/ef/gh/ij", emptyHandler)))
		require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ab/c", emptyHandler)))
		require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ax/bba", emptyHandler)))
		require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ax/cbb", emptyHandler)))
		require.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ax/", emptyHandler)))
		return nil
	})
	tree := f.getRoot()
	fmt.Println(tree.root[0])
}
