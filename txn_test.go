package fox

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tigerwill90/fox/internal/iterutil"
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
			f, _ := New()
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
			assert.Len(t, tree.root, 0)
		})
	}
}

func TestTxn_TruncateAll(t *testing.T) {
	f, _ := New()
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
	assert.Len(t, tree.root, 0)
}

func TestTxn_Isolation(t *testing.T) {
	t.Run("txn iterator does not observe update once created", func(t *testing.T) {
		f, _ := New()
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

	t.Run("txn snapshot does not observe further write", func(t *testing.T) {
		f, _ := New()
		_ = f.Updates(func(txn *Txn) error {
			for _, rte := range staticRoutes {
				assert.NoError(t, onlyError(txn.Handle(rte.method, rte.path, emptyHandler)))
			}
			snapshot := txn.Snapshot()

			for _, rte := range githubAPI {
				assert.NoError(t, onlyError(txn.Handle(rte.method, rte.path, emptyHandler)))
			}

			assert.False(t, snapshot.Has(http.MethodGet, "/repos/{owner}/{repo}/comments"))
			assert.False(t, snapshot.Has(http.MethodGet, "/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))
			assert.True(t, txn.Has(http.MethodGet, "/repos/{owner}/{repo}/comments"))
			assert.True(t, txn.Has(http.MethodGet, "/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))

			return nil
		})
	})

	t.Run("read only transaction are isolated from write", func(t *testing.T) {
		f, _ := New()
		for _, rte := range staticRoutes {
			assert.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
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
		f, _ := New()
		for _, rte := range staticRoutes {
			assert.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
		}

		txn1 := f.Txn(false)
		defer txn1.Abort()

		for _, rte := range githubAPI {
			assert.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
		}

		txn2 := f.Txn(false)
		defer txn2.Abort()

		assert.Equal(t, len(staticRoutes), iterutil.Len2(txn1.Iter().All()))
		assert.Equal(t, len(staticRoutes)+len(githubAPI), iterutil.Len2(txn2.Iter().All()))
	})

	t.Run("aborted transaction does not write anything", func(t *testing.T) {
		f, _ := New()
		for _, rte := range staticRoutes {
			assert.NoError(t, onlyError(f.Handle(rte.method, rte.path, emptyHandler)))
		}

		want := errors.New("aborted")
		err := f.Updates(func(txn *Txn) error {
			for _, rte := range githubAPI {
				assert.NoError(t, onlyError(txn.Handle(rte.method, rte.path, emptyHandler)))
			}
			assert.Equal(t, len(githubAPI)+len(staticRoutes), iterutil.Len2(txn.Iter().All()))
			return want
		})
		assert.Equal(t, err, want)
		assert.Equal(t, len(staticRoutes), iterutil.Len2(f.Iter().All()))
	})

	t.Run("track registered route", func(t *testing.T) {
		f, _ := New()
		require.NoError(t, f.Updates(func(txn *Txn) error {
			for _, rte := range staticRoutes {
				if _, err := txn.Handle(rte.method, "example.com"+rte.path, emptyHandler); err != nil {
					return err
				}
			}
			assert.Equal(t, len(staticRoutes), txn.Len())

			for _, rte := range staticRoutes {
				if _, err := txn.Delete(rte.method, "example.com"+rte.path); err != nil {
					return err
				}
			}
			assert.Zero(t, txn.Len())

			return nil
		}))
	})
}

func TestTxn_WriteOnReadTransaction(t *testing.T) {
	f, _ := New()
	txn := f.Txn(false)
	defer txn.Abort()
	assert.ErrorIs(t, onlyError(txn.Handle(http.MethodGet, "/foo", emptyHandler)), ErrReadOnlyTxn)
	assert.ErrorIs(t, onlyError(txn.Update(http.MethodGet, "/foo", emptyHandler)), ErrReadOnlyTxn)
	deletedRoute, err := txn.Delete(http.MethodGet, "/foo")
	assert.ErrorIs(t, err, ErrReadOnlyTxn)
	assert.Nil(t, deletedRoute)
	assert.ErrorIs(t, txn.Truncate(), ErrReadOnlyTxn)
	txn.Commit()
}

func TestTxn_WriteOrReadAfterFinalized(t *testing.T) {
	f, _ := New()
	txn := f.Txn(true)
	txn.Abort()
	assert.Panics(t, func() {
		_, _ = txn.Handle(http.MethodGet, "/foo", emptyHandler)
	})
	assert.Panics(t, func() {
		_, _ = txn.Update(http.MethodGet, "/foo", emptyHandler)
	})
	assert.Panics(t, func() {
		_, _ = txn.Delete(http.MethodGet, "/foo")
	})
	assert.Panics(t, func() {
		txn.Has(http.MethodGet, "/foo")
	})
	assert.Panics(t, func() {
		txn.Reverse(http.MethodGet, "host", "/foo")
	})
	assert.Panics(t, func() {
		txn.Lookup(nil, nil)
	})
	assert.NotPanics(t, func() {
		txn.Commit()
		txn.Abort()
	})
}
