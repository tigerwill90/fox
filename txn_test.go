package fox

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tigerwill90/fox/internal/iterutil"
)

func TestTxn_TruncateAll(t *testing.T) {
	f, _ := NewRouter()
	require.NoError(t, onlyError(f.Add(MethodGet, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodPost, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodDelete, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodPut, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodConnect, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodTrace, "/foo/bar", emptyHandler)))
	require.NoError(t, onlyError(f.Add([]string{"BOULOU"}, "/foo/bar", emptyHandler)))

	txn := f.Txn(true)
	defer txn.Abort()

	if assert.NoError(t, txn.Truncate()) {
		txn.Commit()
	}

	tree := f.getTree()
	assert.Len(t, tree.patterns.statics, 0)
	assert.Len(t, tree.patterns.params, 0)
	assert.Len(t, tree.patterns.wildcards, 0)
	assert.Len(t, tree.methods, 0)
}

func TestTxn_Isolation(t *testing.T) {
	t.Run("txn iterator does not observe update once created", func(t *testing.T) {
		f, _ := NewRouter()
		_ = f.Updates(func(txn *Txn) error {
			assert.NoError(t, onlyError(txn.Add(MethodGet, "/ab", emptyHandler)))
			assert.NoError(t, onlyError(txn.Add(MethodGet, "/ab/cd", emptyHandler)))
			assert.NoError(t, onlyError(txn.Add(MethodGet, "/ab/cd/ef", emptyHandler)))
			iter := iterutil.Map(txn.Iter().All(), func(route *Route) string {
				return route.Pattern()
			})

			patterns := make([]string, 0, 3)
			for pattern := range iter {
				patterns = append(patterns, pattern)
				_ = onlyError(txn.Add(MethodGet, "/ab/cd/ef/gh", emptyHandler))
				_ = onlyError(txn.Add(MethodGet, "/ab/cd/ef/gh/ij", emptyHandler))
				_ = onlyError(txn.Add(MethodGet, "/ab/cd/e", emptyHandler))
				_ = onlyError(txn.Add(MethodGet, "/ax", emptyHandler))
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
		f, _ := NewRouter()
		_ = f.Updates(func(txn *Txn) error {
			for _, rte := range staticRoutes {
				assert.NoError(t, onlyError(txn.Add([]string{rte.method}, rte.path, emptyHandler, WithName(rte.method+":"+rte.path))))
			}
			snapshot := txn.Snapshot()

			for _, rte := range githubAPI {
				assert.NoError(t, onlyError(txn.Add([]string{rte.method}, rte.path, emptyHandler, WithName(rte.method+":"+rte.path))))
			}

			assert.False(t, snapshot.Has(MethodGet, "/repos/{owner}/{repo}/comments"))
			assert.False(t, snapshot.Has(MethodGet, "/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))
			assert.Nil(t, snapshot.Name("GET:/repos/{owner}/{repo}/comments"))
			assert.Nil(t, snapshot.Name("GET:/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))
			assert.True(t, txn.Has(MethodGet, "/repos/{owner}/{repo}/comments"))
			assert.True(t, txn.Has(MethodGet, "/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))
			assert.NotNil(t, txn.Name("GET:/repos/{owner}/{repo}/comments"))
			assert.NotNil(t, txn.Name("GET:/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))

			return nil
		})
	})

	t.Run("read only transaction are isolated from write", func(t *testing.T) {
		f, _ := NewRouter()
		for _, rte := range staticRoutes {
			assert.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler, WithName(rte.method+":"+rte.path))))
		}

		wantPatterns := 0
		wantNames := 0
		_ = f.View(func(txn *Txn) error {
			wantPatterns = iterutil.Len(txn.Iter().All())
			wantNames = iterutil.Len(txn.Iter().Names())
			for _, rte := range githubAPI {
				assert.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler, WithName(rte.method+":"+rte.path))))
			}
			assert.Equal(t, wantPatterns, iterutil.Len(txn.Iter().All()))
			assert.Equal(t, wantNames, iterutil.Len(txn.Iter().Names()))
			assert.False(t, txn.Has(MethodGet, "/repos/{owner}/{repo}/comments"))
			assert.False(t, txn.Has(MethodGet, "/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))
			assert.Nil(t, txn.Name("GET:/repos/{owner}/{repo}/comments"))
			assert.Nil(t, txn.Name("GET:/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))
			assert.True(t, f.Has(MethodGet, "/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))
			assert.NotNil(t, f.Name("GET:/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"))
			return nil
		})

		assert.Equal(t, wantPatterns+len(githubAPI), iterutil.Len(f.Iter().All()))
		assert.Equal(t, wantNames+len(githubAPI), iterutil.Len(f.Iter().Names()))
	})

	t.Run("txn truncate does not reflect on read before commit", func(t *testing.T) {
		f := MustRouter()
		for _, rte := range staticRoutes {
			assert.NoError(t, onlyError(f.Add([]string{rte.method, http.MethodPost}, rte.path, emptyHandler, WithName(rte.method+":"+rte.path))))
		}

		txn := f.Txn(true)
		defer txn.Abort()

		require.NoError(t, txn.Truncate())

		tree := f.getTree()
		assert.Equal(t, map[string]uint{http.MethodGet: 157, http.MethodPost: 157}, tree.methods)
		assert.Equal(t, len(staticRoutes), iterutil.Len(f.Iter().All()))
		assert.Equal(t, len(staticRoutes), iterutil.Len(f.Iter().Names()))
		assert.Equal(t, len(staticRoutes), f.Len())

		readTxn := f.Txn(false)
		defer readTxn.Abort()

		txn.Commit()

		// Reflect after commited
		tree = f.getTree()
		assert.Empty(t, tree.methods)
		assert.Equal(t, 0, iterutil.Len(f.Iter().All()))
		assert.Equal(t, 0, iterutil.Len(f.Iter().Names()))
		assert.Equal(t, 0, f.Len())

		// Read txn created before commit
		assert.Equal(t, map[string]uint{http.MethodGet: 157, http.MethodPost: 157}, readTxn.rootTxn.methods)
		assert.Equal(t, len(staticRoutes), iterutil.Len(readTxn.Iter().All()))
		assert.Equal(t, len(staticRoutes), iterutil.Len(readTxn.Iter().Names()))
		assert.Equal(t, len(staticRoutes), readTxn.Len())
	})

	t.Run("read only transaction can run uncoordinated", func(t *testing.T) {
		f, _ := NewRouter()
		for _, rte := range staticRoutes {
			assert.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler)))
		}

		txn1 := f.Txn(false)
		defer txn1.Abort()

		for _, rte := range githubAPI {
			assert.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler)))
		}

		txn2 := f.Txn(false)
		defer txn2.Abort()

		assert.Equal(t, len(staticRoutes), iterutil.Len(txn1.Iter().All()))
		assert.Equal(t, len(staticRoutes)+len(githubAPI), iterutil.Len(txn2.Iter().All()))
	})

	t.Run("aborted transaction does not write anything", func(t *testing.T) {
		f, _ := NewRouter()
		for _, rte := range staticRoutes {
			assert.NoError(t, onlyError(f.Add([]string{rte.method}, rte.path, emptyHandler, WithName(rte.method+":"+rte.path))))
		}

		want := errors.New("aborted")
		err := f.Updates(func(txn *Txn) error {
			for _, rte := range githubAPI {
				assert.NoError(t, onlyError(txn.Add([]string{rte.method}, rte.path, emptyHandler, WithName(rte.method+":"+rte.path))))
			}
			assert.Equal(t, len(githubAPI)+len(staticRoutes), iterutil.Len(txn.Iter().All()))
			assert.Equal(t, len(githubAPI)+len(staticRoutes), iterutil.Len(txn.Iter().Names()))
			return want
		})
		assert.Equal(t, err, want)
		assert.Equal(t, len(staticRoutes), iterutil.Len(f.Iter().All()))
		assert.Equal(t, len(staticRoutes), iterutil.Len(f.Iter().Names()))
	})

	t.Run("track registered route", func(t *testing.T) {
		f, _ := NewRouter()
		require.NoError(t, f.Updates(func(txn *Txn) error {
			for _, rte := range staticRoutes {
				if _, err := txn.Add([]string{rte.method}, "example.com"+rte.path, emptyHandler); err != nil {
					return err
				}
			}
			assert.Equal(t, len(staticRoutes), txn.Len())

			for _, rte := range staticRoutes {
				if _, err := txn.Delete([]string{rte.method}, "example.com"+rte.path); err != nil {
					return err
				}
			}
			assert.Zero(t, txn.Len())

			return nil
		}))
	})
}

func TestTxn_WriteOnReadTransaction(t *testing.T) {
	f, _ := NewRouter()
	txn := f.Txn(false)
	defer txn.Abort()
	assert.ErrorIs(t, onlyError(txn.Add(MethodGet, "/foo", emptyHandler)), ErrReadOnlyTxn)
	assert.ErrorIs(t, onlyError(txn.Update(MethodGet, "/foo", emptyHandler)), ErrReadOnlyTxn)
	deletedRoute, err := txn.Delete(MethodGet, "/foo")
	assert.ErrorIs(t, err, ErrReadOnlyTxn)
	assert.Nil(t, deletedRoute)
	assert.ErrorIs(t, txn.Truncate(), ErrReadOnlyTxn)
	txn.Commit()
}

func TestTxn_WriteOrReadAfterFinalized(t *testing.T) {
	f, _ := NewRouter()
	txn := f.Txn(true)
	txn.Abort()
	assert.Panics(t, func() {
		_, _ = txn.Add(MethodGet, "/foo", emptyHandler)
	})
	assert.Panics(t, func() {
		_, _ = txn.Update(MethodGet, "/foo", emptyHandler)
	})
	assert.Panics(t, func() {
		_, _ = txn.Delete(MethodGet, "/foo")
	})
	assert.Panics(t, func() {
		txn.Has(MethodGet, "/foo")
	})
	assert.Panics(t, func() {
		req := httptest.NewRequest(http.MethodGet, "example.com/foo", nil)
		txn.Match(req.Method, req)
	})
	assert.Panics(t, func() {
		txn.Lookup(nil, nil)
	})
	assert.NotPanics(t, func() {
		txn.Commit()
		txn.Abort()
	})
}

func TestInsertConflictWithName(t *testing.T) {
	f, _ := NewRouter(AllowRegexpParam(true))
	f.MustAdd(MethodGet, "/users", emptyHandler,
		WithQueryMatcher("version", "v1"),
		WithHeaderMatcher("Authorization", "secret"),
		WithName("users"),
	)
	f.MustAdd(MethodGet, "/users/{name}", emptyHandler,
		WithQueryMatcher("version", "v2"),
		WithHeaderMatcher("Authorization", "secret"),
		WithName("users_name"),
	)
	f.MustAdd(MethodGet, "exemple.com/users/{name}", emptyHandler,
		WithQueryMatcher("version", "v2"),
		WithHeaderMatcher("Authorization", "secret"),
		WithName("hostname_users_name"),
	)

	t.Run("conflict with matchers", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		assert.ErrorIs(t, onlyError(txn.Add(MethodGet, "/users/{id}", emptyHandler,
			WithQueryMatcher("version", "v2"),
			WithHeaderMatcher("Authorization", "secret"),
		)), ErrRouteConflict)
		assert.Nil(t, txn.rootTxn.writable)
	})
	t.Run("conflict with name", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		assert.ErrorIs(t, onlyError(txn.Add(MethodGet, "/users/{id}", emptyHandler,
			WithQueryMatcher("version", "v1"),
			WithHeaderMatcher("Authorization", "secret"),
			WithName("users"),
		)), ErrRouteNameExist)
		assert.Nil(t, txn.rootTxn.writable)

		assert.ErrorIs(t, onlyError(txn.Add(MethodGet, "/users/{id}", emptyHandler,
			WithQueryMatcher("version", "v1"),
			WithHeaderMatcher("Authorization", "secret"),
			WithName("users_name"),
		)), ErrRouteNameExist)
		assert.Nil(t, txn.rootTxn.writable)

		txn.Commit()

		assert.False(t, f.Has(MethodGet, "/users/{id}",
			QueryMatcher{"version", "v1"},
			QueryMatcher{"Authorization", "secret"},
		))
	})

	t.Run("conflict with name on split node", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		assert.ErrorIs(t, onlyError(txn.Add(MethodGet, "/use", emptyHandler,
			WithName("users_name"),
		)), ErrRouteNameExist)
		assert.Nil(t, txn.rootTxn.writable)

		assert.ErrorIs(t, onlyError(txn.Add(MethodGet, "/usa", emptyHandler,
			WithName("users_name"),
		)), ErrRouteNameExist)
		assert.Nil(t, txn.rootTxn.writable)

		assert.ErrorIs(t, onlyError(txn.Add(MethodGet, "/usa/foo", emptyHandler,
			WithName("users_name"),
		)), ErrRouteNameExist)
		assert.Nil(t, txn.rootTxn.writable)

		assert.ErrorIs(t, onlyError(txn.Add(MethodGet, "/users/{name}/email", emptyHandler,
			WithName("users_name"),
		)), ErrRouteNameExist)
		assert.Nil(t, txn.rootTxn.writable)

		assert.ErrorIs(t, onlyError(txn.Add(MethodGet, "/users/{name:aaa}", emptyHandler,
			WithName("users_name"),
		)), ErrRouteNameExist)
		assert.Nil(t, txn.rootTxn.writable)

		assert.ErrorIs(t, onlyError(txn.Add(MethodGet, "exemple/use", emptyHandler,
			WithName("users"),
		)), ErrRouteNameExist)
		assert.Nil(t, txn.rootTxn.writable)

		txn.Commit()
		assert.False(t, f.Has(MethodGet, "/use"))
		assert.False(t, f.Has(MethodGet, "exemple/use"))
		assert.False(t, f.Has(MethodGet, "/users/{name:aaa}"))
		assert.False(t, f.Has(MethodGet, "/users/{name}/email"))
		assert.False(t, f.Has(MethodGet, "/usa/foo"))
		assert.False(t, f.Has(MethodGet, "/usa"))
	})
}

func TestUpdateConflictWithName(t *testing.T) {
	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/users", emptyHandler)
	f.MustAdd(MethodGet, "/users", emptyHandler,
		WithQueryMatcher("version", "v1"),
		WithHeaderMatcher("Authorization", "secret"),
		WithName("users"),
	)
	f.MustAdd(MethodGet, "/users/{name}", emptyHandler,
		WithQueryMatcher("version", "v2"),
		WithHeaderMatcher("Authorization", "secret"),
		WithName("users_name"),
	)

	t.Run("conflict with matchers", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		assert.ErrorIs(t, onlyError(txn.Update(MethodGet, "/users/{name}", emptyHandler,
			WithQueryMatcher("version", "v3"),
			WithHeaderMatcher("Authorization", "secret"),
		)), ErrRouteNotFound)
		assert.Nil(t, txn.rootTxn.writable)
	})

	t.Run("conflict with different param name", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		assert.ErrorIs(t, onlyError(txn.Update(MethodGet, "/users/{id}", emptyHandler,
			WithQueryMatcher("version", "v2"),
			WithHeaderMatcher("Authorization", "secret"),
		)), ErrRouteNotFound)
		assert.Nil(t, txn.rootTxn.writable)
	})

	t.Run("conflict on insert name", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		require.ErrorIs(t, onlyError(txn.Update(MethodGet, "/users", emptyHandler,
			WithName("users"),
		)), ErrRouteNameExist)
		assert.Nil(t, txn.rootTxn.writable)
	})

	t.Run("conflict on update name", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		require.ErrorIs(t, onlyError(txn.Update(MethodGet, "/users", emptyHandler,
			WithQueryMatcher("version", "v1"),
			WithHeaderMatcher("Authorization", "secret"),
			WithName("users_name"),
		)), ErrRouteNameExist)
		assert.Nil(t, txn.rootTxn.writable)
	})
}

func TestUpdateWithName(t *testing.T) {
	f, _ := NewRouter()
	f.MustAdd(MethodGet, "/users", emptyHandler)
	f.MustAdd(MethodGet, "/users", emptyHandler,
		WithQueryMatcher("version", "v1"),
		WithHeaderMatcher("Authorization", "secret"),
		WithName("users"),
	)
	f.MustAdd(MethodGet, "/users/{name}", emptyHandler,
		WithQueryMatcher("version", "v2"),
		WithHeaderMatcher("Authorization", "secret"),
		WithName("users_name"),
	)

	t.Run("delete name on update", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		assert.NotNil(t, txn.Name("users_name"))
		route, err := txn.Update(MethodGet, "/users/{name}", emptyHandler,
			WithQueryMatcher("version", "v2"),
			WithHeaderMatcher("Authorization", "secret"),
		)
		require.NoError(t, err)
		assert.Equal(t, "/users/{name}", route.pattern)
		assert.Empty(t, route.name)
		assert.Nil(t, txn.Name("users_name"))
		txn.Commit()
		assert.Nil(t, f.Name("users_name"))
	})

	t.Run("insert name on update", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		route, err := txn.Update(MethodGet, "/users", emptyHandler,
			WithName("foo"),
		)
		require.NoError(t, err)
		assert.Equal(t, "/users", route.pattern)
		assert.Equal(t, "foo", route.name)
		assert.NotNil(t, txn.Name("foo"))
		txn.Commit()
		assert.NotNil(t, f.Name("foo"))
	})

	t.Run("update route with same name", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		route, err := txn.Update(MethodGet, "/users", emptyHandler,
			WithQueryMatcher("version", "v1"),
			WithHeaderMatcher("Authorization", "secret"),
			WithName("users"),
		)
		require.NoError(t, err)
		assert.Equal(t, "/users", route.pattern)
		assert.Equal(t, "users", route.name)
		assert.NotNil(t, txn.Name("users"))
		txn.Commit()
		assert.NotNil(t, f.Name("users"))
	})

	t.Run("update route with replacing name", func(t *testing.T) {
		txn := f.Txn(true)
		defer txn.Abort()
		route, err := txn.Update(MethodGet, "/users", emptyHandler,
			WithQueryMatcher("version", "v1"),
			WithHeaderMatcher("Authorization", "secret"),
			WithName("new_users"),
		)
		require.NoError(t, err)
		assert.Equal(t, "/users", route.pattern)
		assert.Equal(t, "new_users", route.name)
		assert.Nil(t, txn.Name("users"))
		assert.NotNil(t, txn.Name("new_users"))
		txn.Commit()
		assert.Nil(t, f.Name("users"))
		assert.NotNil(t, f.Name("new_users"))
	})
}

func TestTxn_HasWithMatchers(t *testing.T) {
	f, _ := NewRouter(AllowRegexpParam(true))

	m1, _ := MatchQuery("version", "v1")
	m2, _ := MatchQuery("version", "v2")
	m3, _ := MatchHeader("X-Api-Key", "secret")

	require.NoError(t, f.Updates(func(txn *Txn) error {
		if err := onlyError(txn.Add(MethodGet, "/api/users", emptyHandler)); err != nil {
			return err
		}
		if err := onlyError(txn.Add(MethodGet, "/api/users", emptyHandler, WithMatcher(m1))); err != nil {
			return err
		}
		if err := onlyError(txn.Add(MethodGet, "/api/users", emptyHandler, WithMatcher(m2))); err != nil {
			return err
		}
		if err := onlyError(txn.Add(MethodGet, "/api/users", emptyHandler, WithMatcher(m1, m3))); err != nil {
			return err
		}
		if err := onlyError(txn.Add(MethodGet, "/api/users/{id}", emptyHandler)); err != nil {
			return err
		}
		if err := onlyError(txn.Add(MethodGet, "/api/users/{id}", emptyHandler, WithMatcher(m1))); err != nil {
			return err
		}
		if err := onlyError(txn.Add(MethodGet, "/files/+{path}", emptyHandler)); err != nil {
			return err
		}
		if err := onlyError(txn.Add(MethodGet, "/files/+{path}", emptyHandler, WithMatcher(m1))); err != nil {
			return err
		}
		if err := onlyError(txn.Add(MethodGet, "/items/{id:[0-9]+}", emptyHandler)); err != nil {
			return err
		}
		if err := onlyError(txn.Add(MethodGet, "/items/{id:[0-9]+}", emptyHandler, WithMatcher(m1))); err != nil {
			return err
		}
		if err := onlyError(txn.Add(MethodGet, "/org/{org}/repo/{repo:[a-z]+}", emptyHandler, WithMatcher(m1))); err != nil {
			return err
		}
		return nil
	}))

	cases := []struct {
		name     string
		path     string
		matchers []Matcher
		want     bool
	}{
		{
			name: "static route without matcher",
			path: "/api/users",
			want: true,
		},
		{
			name:     "static route with matching matcher",
			path:     "/api/users",
			matchers: []Matcher{m1},
			want:     true,
		},
		{
			name:     "static route with different matcher value",
			path:     "/api/users",
			matchers: []Matcher{m2},
			want:     true,
		},
		{
			name:     "static route with multiple matchers",
			path:     "/api/users",
			matchers: []Matcher{m1, m3},
			want:     true,
		},
		{
			name:     "static route with multiple matchers in different order",
			path:     "/api/users",
			matchers: []Matcher{m3, m1},
			want:     true,
		},
		{
			name:     "static route with non-registered matcher",
			path:     "/api/users",
			matchers: []Matcher{m3},
			want:     false,
		},
		{
			name:     "static route with partial matchers",
			path:     "/api/users",
			matchers: []Matcher{m1, m2},
			want:     false,
		},
		{
			name: "param route without matcher",
			path: "/api/users/{id}",
			want: true,
		},
		{
			name:     "param route with matcher",
			path:     "/api/users/{id}",
			matchers: []Matcher{m1},
			want:     true,
		},
		{
			name:     "param route with wrong matcher",
			path:     "/api/users/{id}",
			matchers: []Matcher{m2},
			want:     false,
		},
		{
			name: "wildcard route without matcher",
			path: "/files/+{path}",
			want: true,
		},
		{
			name:     "wildcard route with matcher",
			path:     "/files/+{path}",
			matchers: []Matcher{m1},
			want:     true,
		},
		{
			name: "regexp route without matcher",
			path: "/items/{id:[0-9]+}",
			want: true,
		},
		{
			name:     "regexp route with matcher",
			path:     "/items/{id:[0-9]+}",
			matchers: []Matcher{m1},
			want:     true,
		},
		{
			name:     "mixed route with param and regexp",
			path:     "/org/{org}/repo/{repo:[a-z]+}",
			matchers: []Matcher{m1},
			want:     true,
		},
		{
			name: "mixed route without matcher does not exist",
			path: "/org/{org}/repo/{repo:[a-z]+}",
			want: false,
		},
		{
			name: "structurally identical param pattern with different name",
			path: "/api/users/{name}",
			want: false,
		},
		{
			name:     "structurally identical param pattern with different name and matcher",
			path:     "/api/users/{name}",
			matchers: []Matcher{m1},
			want:     false,
		},
		{
			name: "structurally identical regexp pattern with different name",
			path: "/items/{num:[0-9]+}",
			want: false,
		},
		{
			name:     "structurally identical regexp pattern with different name and matcher",
			path:     "/items/{num:[0-9]+}",
			matchers: []Matcher{m1},
			want:     false,
		},
	}

	require.NoError(t, f.View(func(txn *Txn) error {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.want, txn.Has(MethodGet, tc.path, tc.matchers...))
			})
		}
		return nil
	}))
}

func TestX(t *testing.T) {
	f := MustRouter()
	f.MustAdd(MethodGet, "/foo", func(c *Context) {
		cc := c.Clone()
		cc.Close()
	})

	req := httptest.NewRequest("GET", "/foo", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)
}
