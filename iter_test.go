// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"slices"
	"testing"
)

var routesCases = []string{"/fox/router", "/foo/bar/{baz}", "/foo/bar/{baz}/{name}", "/john/doe/*{args}", "/john/doe"}

func TestIter_Routes(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, tree.Handle(http.MethodGet, rte, emptyHandler))
		require.NoError(t, tree.Handle(http.MethodPost, rte, emptyHandler))
		require.NoError(t, tree.Handle(http.MethodHead, rte, emptyHandler))
	}

	results := make(map[string][]string)
	it := tree.Iter()
	for method, route := range it.Routes(it.Methods(), "/foo/bar/{baz}/{name}") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Path())
	}

	want := []string{"/foo/bar/{baz}/{name}"}
	for key := range results {
		assert.ElementsMatch(t, want, results[key])
	}
}

func TestIter_All(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, tree.Handle(http.MethodGet, rte, emptyHandler))
		require.NoError(t, tree.Handle(http.MethodPost, rte, emptyHandler))
		require.NoError(t, tree.Handle(http.MethodHead, rte, emptyHandler))
	}

	results := make(map[string][]string)

	it := tree.Iter()
	for method, route := range it.All() {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Path())
	}

	for key := range results {
		assert.ElementsMatch(t, routesCases, results[key])
	}
}

func TestIter_RootPrefixOneMethod(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, tree.Handle(http.MethodGet, rte, emptyHandler))
		require.NoError(t, tree.Handle(http.MethodPost, rte, emptyHandler))
		require.NoError(t, tree.Handle(http.MethodHead, rte, emptyHandler))
	}

	results := make(map[string][]string)
	it := tree.Iter()

	for method, route := range it.Prefix(seqOf(http.MethodHead), "/") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Path())
	}

	assert.Len(t, results, 1)
	assert.ElementsMatch(t, routesCases, results[http.MethodHead])
}

func TestIter_Prefix(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, tree.Handle(http.MethodGet, rte, emptyHandler))
		require.NoError(t, tree.Handle(http.MethodPost, rte, emptyHandler))
		require.NoError(t, tree.Handle(http.MethodHead, rte, emptyHandler))
	}

	want := []string{"/foo/bar/{baz}", "/foo/bar/{baz}/{name}"}
	results := make(map[string][]string)

	it := tree.Iter()
	for method, route := range it.Prefix(it.Methods(), "/foo") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Path())
	}

	for key := range results {
		assert.ElementsMatch(t, want, results[key])
	}
}

func TestIter_PrefixWithMethod(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, tree.Handle(http.MethodGet, rte, emptyHandler))
		require.NoError(t, tree.Handle(http.MethodPost, rte, emptyHandler))
		require.NoError(t, tree.Handle(http.MethodHead, rte, emptyHandler))
	}

	want := []string{"/foo/bar/{baz}", "/foo/bar/{baz}/{name}"}
	results := make(map[string][]string)

	it := tree.Iter()
	for method, route := range it.Prefix(seqOf(http.MethodHead), "/foo") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Path())
	}

	assert.Len(t, results, 1)
	assert.ElementsMatch(t, want, results[http.MethodHead])
}

func ExampleIter_All() {
	f := New()

	it := f.Iter()
	for method, route := range it.All() {
		fmt.Println(method, route.Path())
	}
}

func ExampleIter_Prefix() {
	f := New()

	it := f.Iter()
	for method, route := range it.Prefix(slices.Values([]string{"GET", "POST"}), "/foo") {
		fmt.Println(method, route.Path())
	}
}
