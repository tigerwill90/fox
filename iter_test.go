package fox

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

var routesCases = []string{"/fox/router", "/foo/bar/:baz", "/foo/bar/:baz/:name", "/john/doe/*args", "/john/doe"}

func TestIterator_Rewind(t *testing.T) {
	r := New()
	for _, rte := range routesCases {
		require.NoError(t, r.Handler(http.MethodGet, rte, emptyHandler))
		require.NoError(t, r.Handler(http.MethodPost, rte, emptyHandler))
		require.NoError(t, r.Handler(http.MethodHead, rte, emptyHandler))
	}

	results := make(map[string][]string)

	it := r.NewIterator()
	for it.Rewind(); it.Valid(); it.Next() {
		assert.NotNil(t, it.Handler())
		results[it.Method()] = append(results[it.method], it.Path())
	}

	for key := range results {
		assert.ElementsMatch(t, routesCases, results[key])
	}
}

func TestIterator_SeekMethod(t *testing.T) {
	r := New()
	for _, rte := range routesCases {
		require.NoError(t, r.Handler(http.MethodGet, rte, emptyHandler))
		require.NoError(t, r.Handler(http.MethodPost, rte, emptyHandler))
		require.NoError(t, r.Handler(http.MethodHead, rte, emptyHandler))
	}

	results := make(map[string][]string)

	it := r.NewIterator()
	for it.SeekMethod(http.MethodHead); it.Valid(); it.Next() {
		assert.NotNil(t, it.Handler())
		results[it.Method()] = append(results[it.method], it.Path())
	}

	assert.Len(t, results, 1)
	assert.ElementsMatch(t, routesCases, results[http.MethodHead])
}

func TestIterator_SeekPrefix(t *testing.T) {
	r := New()
	for _, rte := range routesCases {
		require.NoError(t, r.Handler(http.MethodGet, rte, emptyHandler))
		require.NoError(t, r.Handler(http.MethodPost, rte, emptyHandler))
		require.NoError(t, r.Handler(http.MethodHead, rte, emptyHandler))
	}

	want := []string{"/foo/bar/:baz", "/foo/bar/:baz/:name"}
	results := make(map[string][]string)

	it := r.NewIterator()
	for it.SeekPrefix("/foo"); it.Valid(); it.Next() {
		assert.NotNil(t, it.Handler())
		results[it.Method()] = append(results[it.method], it.Path())
	}

	for key := range results {
		assert.ElementsMatch(t, want, results[key])
	}
}

func TestIterator_SeekMethodPrefix(t *testing.T) {
	r := New()
	for _, rte := range routesCases {
		require.NoError(t, r.Handler(http.MethodGet, rte, emptyHandler))
		require.NoError(t, r.Handler(http.MethodPost, rte, emptyHandler))
		require.NoError(t, r.Handler(http.MethodHead, rte, emptyHandler))
	}

	want := []string{"/foo/bar/:baz", "/foo/bar/:baz/:name"}
	results := make(map[string][]string)

	it := r.NewIterator()
	for it.SeekMethodPrefix(http.MethodHead, "/foo"); it.Valid(); it.Next() {
		results[it.Method()] = append(results[it.method], it.Path())
	}

	assert.Len(t, results, 1)
	assert.ElementsMatch(t, want, results[http.MethodHead])
}

func ExampleRouter_NewIterator() {
	r := New()
	it := r.NewIterator()

	// Iterate over all routes
	for it.Rewind(); it.Valid(); it.Next() {
		fmt.Println(it.Method(), it.Path())
	}

	// Iterate over all routes for the GET method
	for it.SeekMethod(http.MethodGet); it.Valid(); it.Next() {
		fmt.Println(it.Method(), it.Path())
	}

	// Iterate over all routes starting with /users
	for it.SeekPrefix("/users"); it.Valid(); it.Next() {
		fmt.Println(it.Method(), it.Path())
	}

	// Iterate over all route starting with /users for the GET method
	for it.SeekMethodPrefix(http.MethodGet, "/user"); it.Valid(); it.Next() {
		fmt.Println(it.Method(), it.Path())
	}
}
