// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tigerwill90/fox/internal/iterutil"
)

var routesCases = []string{"/fox/router", "/foo/bar/{baz}", "/foo/bar/{baz}/{name}", "/john/doe/*{args}", "/john/doe"}

func TestIter_Routes(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler)))
	}

	results := make(map[string][]string)
	it := f.Iter()
	for method, route := range it.Routes(it.Methods(), "/foo/bar/{baz}/{name}") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Pattern())
	}

	want := []string{"/foo/bar/{baz}/{name}"}
	for key := range results {
		assert.ElementsMatch(t, want, results[key])
	}
}

func TestIter_RoutesWithHostname(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, "exemple.com"+rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, "exemple.com"+rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, "exemple.com"+rte, emptyHandler)))
	}

	results := make(map[string][]string)
	it := f.Iter()
	for method, route := range it.Routes(it.Methods(), "exemple.com/foo/bar/{baz}/{name}") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Pattern())
	}

	want := []string{"exemple.com/foo/bar/{baz}/{name}"}
	assert.Len(t, results, 3)
	for key := range results {
		assert.ElementsMatch(t, want, results[key])
	}
}

func TestIter_All(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler)))
	}

	results := make(map[string][]string)

	it := f.Iter()
	for method, route := range it.All() {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Pattern())
	}

	for key := range results {
		assert.ElementsMatch(t, routesCases, results[key])
	}
}

func TestIter_AllWithHostname(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, "exemple.com"+rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, "exemple.com"+rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, "exemple.com"+rte, emptyHandler)))
	}

	results := make(map[string][]string)

	it := f.Iter()
	for method, route := range it.All() {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Pattern())
	}

	routesCasesHostname := slices.Collect(iterutil.Map(slices.Values(routesCases), func(path string) string {
		return "exemple.com" + path
	}))

	for key := range results {
		assert.ElementsMatch(t, routesCasesHostname, results[key])
	}
}

func TestIter_AllBreak(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler)))
	}

	it := f.Iter()
	iteration := 0
	for range it.All() {
		iteration++
		break
	}
	assert.Equal(t, 1, iteration)
}

func TestIter_ReverseBreak(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler)))
	}

	it := f.Iter()
	iteration := 0
	for range it.Reverse(it.Methods(), "", "/john/doe/1/2/3") {
		iteration++
		break
	}
	assert.Equal(t, 1, iteration)
}

func TestIter_RouteBreak(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler)))
	}

	it := f.Iter()
	iteration := 0
	for range it.Routes(it.Methods(), "/john/doe/*{args}") {
		iteration++
		break
	}
	assert.Equal(t, 1, iteration)
}

func TestIter_RootPrefixOneMethod(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler)))
	}

	results := make(map[string][]string)
	it := f.Iter()

	for method, route := range it.Prefix(iterutil.SeqOf(http.MethodHead), "/") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Pattern())
	}

	assert.Len(t, results, 1)
	assert.ElementsMatch(t, routesCases, results[http.MethodHead])
}

func TestIter_Prefix(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler)))
	}

	want := []string{"/foo/bar/{baz}", "/foo/bar/{baz}/{name}"}
	results := make(map[string][]string)

	it := f.Iter()
	for method, route := range it.Prefix(it.Methods(), "/foo") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Pattern())
	}

	for key := range results {
		assert.ElementsMatch(t, want, results[key])
	}
}

func TestIter_EdgeCase(t *testing.T) {
	f, _ := New()
	it := f.Iter()

	assert.Empty(t, slices.Collect(iterutil.Left(it.Prefix(iterutil.SeqOf("GET"), "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Prefix(iterutil.SeqOf("CONNECT"), "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Reverse(iterutil.SeqOf("GET"), "", "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Reverse(iterutil.SeqOf("CONNECT"), "", "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Routes(iterutil.SeqOf("GET"), "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Routes(iterutil.SeqOf("CONNECT"), "/"))))
}

func TestIter_PrefixWithMethod(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler)))
	}

	want := []string{"/foo/bar/{baz}", "/foo/bar/{baz}/{name}"}
	results := make(map[string][]string)

	it := f.Iter()
	for method, route := range it.Prefix(iterutil.SeqOf(http.MethodHead), "/foo") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Pattern())
	}

	assert.Len(t, results, 1)
	assert.ElementsMatch(t, want, results[http.MethodHead])
}

func BenchmarkIter_Methods(b *testing.B) {
	f, _ := New()
	for _, route := range staticRoutes {
		require.NoError(b, onlyError(f.Handle(route.method, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.Methods() {

		}
	}
}

func BenchmarkIter_Reverse(b *testing.B) {
	f, _ := New()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Handle(route.method, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.Reverse(it.Methods(), "", "/user/subscriptions/fox/fox") {

		}
	}
}

func BenchmarkIter_Route(b *testing.B) {
	f, _ := New()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Handle(route.method, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.Routes(it.Methods(), "/user/subscriptions/{owner}/{repo}") {

		}
	}
}

func BenchmarkIter_Prefix(b *testing.B) {
	f, _ := New()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Handle(route.method, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.Prefix(it.Methods(), "/") {

		}
	}
}

func ExampleIter_All() {
	f, _ := New()
	it := f.Iter()
	for method, route := range it.All() {
		fmt.Println(method, route.Pattern())
	}
}

func ExampleIter_Methods() {
	f, _ := New()
	it := f.Iter()
	for method := range it.Methods() {
		fmt.Println(method)
	}
}

func ExampleIter_Reverse() {
	f, _ := New()
	it := f.Iter()
	for method, route := range it.Reverse(slices.Values([]string{"GET", "POST"}), "exemple.com", "/foo") {
		fmt.Println(method, route.Pattern())
	}
}

func ExampleIter_Routes() {
	f, _ := New()
	it := f.Iter()
	for method, route := range it.Routes(slices.Values([]string{"GET", "POST"}), "/hello/{name}") {
		fmt.Println(method, route.Pattern())
	}
}

func ExampleIter_Prefix() {
	f, _ := New()
	it := f.Iter()
	for method, route := range it.Prefix(slices.Values([]string{"GET", "POST"}), "/foo") {
		fmt.Println(method, route.Pattern())
	}
}
