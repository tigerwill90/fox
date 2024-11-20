package fox

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tigerwill90/fox/internal/iterutil"
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

func TestTxn_Isolation(t *testing.T) {
	t.Run("txn iterator does not observe update once created", func(t *testing.T) {
		f := New()
		_ = f.Updates(func(txn *Txn) error {
			assert.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ab", emptyHandler)))
			assert.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ab/cd", emptyHandler)))
			assert.NoError(t, onlyError(txn.Handle(http.MethodGet, "/ab/cd/ef", emptyHandler)))
			iter := iterutil.Map(iterutil.Right(txn.Iter().All()), func(route *Route) string {
				return route.Pattern()
			})

			patterns := make([]string, 0, 3)
			for pattern := range iter {
				patterns = append(patterns, pattern)
				_ = onlyError(txn.Handle(http.MethodGet, "/ab/cd/ef/gh", emptyHandler))
				_ = onlyError(txn.Handle(http.MethodGet, "/ab/cd/ef/gh/ij", emptyHandler))
				_ = onlyError(txn.Handle(http.MethodGet, "/ab/cd/e", emptyHandler))
				_ = onlyError(txn.Handle(http.MethodGet, "/ax", emptyHandler))
			}
			assert.Equal(t, []string{"/ab", "/ab/cd", "/ab/cd/ef"}, patterns)

			patterns = make([]string, 0, 3)
			for pattern := range iter {
				patterns = append(patterns, pattern)
			}
			assert.Equal(t, []string{"/ab", "/ab/cd", "/ab/cd/ef"}, patterns)
			return nil
		})
	})

	t.Run("read only transaction are isolated from write", func(t *testing.T) {
		f := New()
		for _, rte := range staticRoutes {
			f.MustHandle(rte.method, rte.path, emptyHandler)
		}

		want := 0
		_ = f.View(func(txn *Txn) error {
			want = iterutil.Len2(txn.Iter().All())
			for _, rte := range githubAPI {
				assert.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
			}
			assert.Equal(t, want, iterutil.Len2(txn.Iter().All()))
			assert.False(t, txn.Has(http.MethodGet, "/repos/{owner}/{repo}/comments"))
			assert.False(t, txn.Has(http.MethodGet, "/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))
			assert.True(t, f.Has(http.MethodGet, "/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))
			return nil
		})

		assert.Equal(t, want+len(githubAPI), iterutil.Len2(f.Iter().All()))
	})

	t.Run("read only transaction can run uncoordinated", func(t *testing.T) {
		f := New()
		for _, rte := range staticRoutes {
			f.MustHandle(rte.method, rte.path, emptyHandler)
		}

		txn1 := f.Txn(false)
		defer txn1.Abort()

		for _, rte := range githubAPI {
			f.MustHandle(rte.method, rte.path, emptyHandler)
		}

		txn2 := f.Txn(false)
		defer txn2.Abort()

		assert.Equal(t, len(staticRoutes), iterutil.Len2(txn1.Iter().All()))
		assert.Equal(t, len(staticRoutes)+len(githubAPI), iterutil.Len2(txn2.Iter().All()))
	})
}

func TestTxn_WriteOnReadTransaction(t *testing.T) {
	f := New()
	txn := f.Txn(false)
	defer txn.Abort()
	assert.ErrorIs(t, onlyError(txn.Handle(http.MethodGet, "/foo", emptyHandler)), ErrReadOnlyTxn)
	assert.ErrorIs(t, onlyError(txn.Update(http.MethodGet, "/foo", emptyHandler)), ErrReadOnlyTxn)
	assert.ErrorIs(t, txn.Delete(http.MethodGet, "/foo"), ErrReadOnlyTxn)
	assert.ErrorIs(t, txn.Truncate(), ErrReadOnlyTxn)
	txn.Commit()
}
