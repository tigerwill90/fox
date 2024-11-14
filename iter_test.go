// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tigerwill90/fox/internal/iterutil"
	"net/http"
	"slices"
	"testing"
)

var routesCases = []string{"/fox/router", "/foo/bar/{baz}", "/foo/bar/{baz}/{name}", "/john/doe/*{args}", "/john/doe"}

func TestIter_Routes(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(tree.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodHead, rte, emptyHandler)))
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
		require.NoError(t, onlyError(tree.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodHead, rte, emptyHandler)))
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

func TestIter_AllBreak(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(tree.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodHead, rte, emptyHandler)))
	}

	var (
		lastMethod string
		lastRoute  *Route
	)
	it := tree.Iter()
	for method, route := range it.All() {
		lastMethod = method
		lastRoute = route
		break
	}
	assert.Equal(t, "GET", lastMethod)
	assert.Equal(t, "/foo/bar/{baz}", lastRoute.Path())
}

func TestIter_ReverseBreak(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(tree.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodHead, rte, emptyHandler)))
	}

	var (
		lastMethod string
		lastRoute  *Route
	)
	it := tree.Iter()
	for method, route := range it.Reverse(it.Methods(), "", "/john/doe/1/2/3") {
		lastMethod = method
		lastRoute = route
		break
	}
	assert.Equal(t, "GET", lastMethod)
	assert.Equal(t, "/john/doe/*{args}", lastRoute.Path())
}

func TestIter_RouteBreak(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(tree.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodHead, rte, emptyHandler)))
	}

	var (
		lastMethod string
		lastRoute  *Route
	)
	it := tree.Iter()
	for method, route := range it.Routes(it.Methods(), "/john/doe/*{args}") {
		lastMethod = method
		lastRoute = route
		break
	}
	assert.Equal(t, "GET", lastMethod)
	assert.Equal(t, "/john/doe/*{args}", lastRoute.Path())
}

func TestIter_RootPrefixOneMethod(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(tree.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodHead, rte, emptyHandler)))
	}

	results := make(map[string][]string)
	it := tree.Iter()

	for method, route := range it.Prefix(iterutil.SeqOf(http.MethodHead), "/") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Path())
	}

	assert.Len(t, results, 1)
	assert.ElementsMatch(t, routesCases, results[http.MethodHead])
}

func TestIter_Prefix(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(tree.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodHead, rte, emptyHandler)))
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

func TestIter_EdgeCase(t *testing.T) {
	tree := New().Tree()
	it := tree.Iter()

	assert.Empty(t, slices.Collect(iterutil.Left(it.Prefix(iterutil.SeqOf("GET"), "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Prefix(iterutil.SeqOf("CONNECT"), "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Reverse(iterutil.SeqOf("GET"), "", "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Reverse(iterutil.SeqOf("CONNECT"), "", "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Routes(iterutil.SeqOf("GET"), "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Routes(iterutil.SeqOf("CONNECT"), "/"))))
}

func TestIter_PrefixWithMethod(t *testing.T) {
	tree := New().Tree()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(tree.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(tree.Handle(http.MethodHead, rte, emptyHandler)))
	}

	want := []string{"/foo/bar/{baz}", "/foo/bar/{baz}/{name}"}
	results := make(map[string][]string)

	it := tree.Iter()
	for method, route := range it.Prefix(iterutil.SeqOf(http.MethodHead), "/foo") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Path())
	}

	assert.Len(t, results, 1)
	assert.ElementsMatch(t, want, results[http.MethodHead])
}

func BenchmarkIter_Methods(b *testing.B) {
	f := New()
	for _, route := range staticRoutes {
		require.NoError(b, onlyError(f.Tree().Handle(route.method, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for _ = range it.Methods() {

		}
	}
}

func BenchmarkIter_Reverse(b *testing.B) {
	f := New()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Tree().Handle(route.method, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for _, _ = range it.Reverse(it.Methods(), "", "/user/subscriptions/fox/fox") {

		}
	}
}

func BenchmarkIter_Route(b *testing.B) {
	f := New()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Tree().Handle(route.method, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for _, _ = range it.Reverse(it.Methods(), "", "/user/subscriptions/{owner}/{repo}") {

		}
	}
}

func BenchmarkIter_Prefix(b *testing.B) {
	f := New()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Tree().Handle(route.method, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for _, _ = range it.Prefix(it.Methods(), "/") {

		}
	}
}

func ExampleIter_All() {
	f := New()
	it := f.Iter()
	for method, route := range it.All() {
		fmt.Println(method, route.Path())
	}
}

func ExampleIter_Methods() {
	f := New()
	it := f.Iter()
	for method := range it.Methods() {
		fmt.Println(method)
	}
}

func ExampleIter_Prefix() {
	f := New()
	it := f.Iter()
	for method, route := range it.Prefix(slices.Values([]string{"GET", "POST"}), "/foo") {
		fmt.Println(method, route.Path())
	}
}
