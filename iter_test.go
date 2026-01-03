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
	f, _ := NewRouter()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodHead, rte, emptyHandler)))
	}

	results := make(map[string][]string)
	it := f.Iter()
	for route := range it.Routes("/foo/bar/{baz}/{name}") {
		assert.NotNil(t, route)
		method := iterutil.First(route.Methods())
		assert.NotEmpty(t, method)
		results[method] = append(results[method], route.Pattern())
	}

	want := []string{"/foo/bar/{baz}/{name}"}
	for key := range results {
		assert.ElementsMatch(t, want, results[key])
	}
}

func TestIter_RoutesWithHostname(t *testing.T) {
	f, _ := NewRouter()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Add(MethodGet, "exemple.com"+rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodPost, "exemple.com"+rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodHead, "exemple.com"+rte, emptyHandler)))
	}

	results := make(map[string][]string)
	it := f.Iter()
	for route := range it.Routes("exemple.com/foo/bar/{baz}/{name}") {
		assert.NotNil(t, route)
		method := iterutil.First(route.Methods())
		assert.NotEmpty(t, method)
		results[method] = append(results[method], route.Pattern())
	}

	want := []string{"exemple.com/foo/bar/{baz}/{name}"}
	assert.Len(t, results, 3)
	for key := range results {
		assert.ElementsMatch(t, want, results[key])
	}
}

func TestIter_All(t *testing.T) {
	f, _ := NewRouter()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodHead, rte, emptyHandler)))
	}

	results := make(map[string][]string)

	it := f.Iter()
	for route := range it.All() {
		assert.NotNil(t, route)
		method := iterutil.First(route.Methods())
		assert.NotEmpty(t, method)
		results[method] = append(results[method], route.Pattern())
	}

	for key := range results {
		assert.ElementsMatch(t, routesCases, results[key])
	}
}

func TestIter_AllWithHostname(t *testing.T) {
	f, _ := NewRouter()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Add(MethodGet, "exemple.com"+rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodPost, "exemple.com"+rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodHead, "exemple.com"+rte, emptyHandler)))
	}

	results := make(map[string][]string)

	it := f.Iter()
	for route := range it.All() {
		assert.NotNil(t, route)
		method := iterutil.First(route.Methods())
		assert.NotEmpty(t, method)
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
	f, _ := NewRouter()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodHead, rte, emptyHandler)))
	}

	it := f.Iter()
	iteration := 0
	for range it.All() {
		iteration++
		break
	}
	assert.Equal(t, 1, iteration)
}

func TestIter_NamesBreak(t *testing.T) {
	f, _ := NewRouter()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler, WithName(http.MethodGet+":"+rte))))
		require.NoError(t, onlyError(f.Add(MethodPost, rte, emptyHandler, WithName(http.MethodPost+":"+rte))))
		require.NoError(t, onlyError(f.Add(MethodHead, rte, emptyHandler, WithName(http.MethodHead+":"+rte))))
	}

	it := f.Iter()
	iteration := 0
	for range it.Names() {
		iteration++
		break
	}
	assert.Equal(t, 1, iteration)
}

func TestIter_RouteBreak(t *testing.T) {
	f, _ := NewRouter()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodHead, rte, emptyHandler)))
	}

	it := f.Iter()
	iteration := 0
	for range it.Routes("/john/doe/*{args}") {
		iteration++
		break
	}
	assert.Equal(t, 1, iteration)
}

func TestIter_PatternPrefix(t *testing.T) {
	f, _ := NewRouter()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Add(MethodHead, rte, emptyHandler)))
	}

	want := []string{"/foo/bar/{baz}", "/foo/bar/{baz}/{name}"}
	results := make(map[string][]string)

	it := f.Iter()
	for route := range it.PatternPrefix("/foo") {
		assert.NotNil(t, route)
		method := iterutil.First(route.Methods())
		assert.NotEmpty(t, method)
		results[method] = append(results[method], route.Pattern())
	}

	for key := range results {
		assert.ElementsMatch(t, want, results[key])
	}
}

func TestIter_PatternStrictPrefix(t *testing.T) {
	f, _ := NewRouter()
	require.NoError(t, onlyError(f.Add(MethodGet, "/{a}/b/x", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodGet, "/{a}/b/y", emptyHandler)))
	require.NoError(t, onlyError(f.Add(MethodGet, "/{other}/b/z", emptyHandler)))

	want := []string{"/{a}/b/x", "/{a}/b/y"}
	results := make(map[string][]string)

	it := f.Iter()
	for route := range it.PatternPrefix("/{a}/b") {
		assert.NotNil(t, route)
		method := iterutil.First(route.Methods())
		assert.NotEmpty(t, method)
		results[method] = append(results[method], route.Pattern())
	}

	for key := range results {
		assert.Equal(t, want, results[key])
	}
}

func TestIter_NamesPrefix(t *testing.T) {
	f, _ := NewRouter()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Add(MethodGet, rte, emptyHandler, WithName(http.MethodGet+":"+rte))))
		require.NoError(t, onlyError(f.Add(MethodPost, rte, emptyHandler, WithName(http.MethodPost+":"+rte))))
		require.NoError(t, onlyError(f.Add(MethodHead, rte, emptyHandler, WithName(http.MethodHead+":"+rte))))
	}

	want := []string{"/foo/bar/{baz}", "/foo/bar/{baz}/{name}", "/fox/router", "/john/doe", "/john/doe/*{args}"}

	it := f.Iter()
	result := slices.Collect(iterutil.Map(it.NamePrefix("GET"), func(a *Route) string {
		return a.Pattern()
	}))
	assert.Equal(t, want, result)
}

func TestIter_NoData(t *testing.T) {
	f, _ := NewRouter()
	it := f.Iter()

	assert.Empty(t, slices.Collect(it.PatternPrefix("/")))
	assert.Empty(t, slices.Collect(it.NamePrefix("GET")))
	assert.Empty(t, slices.Collect(it.Routes("/")))
	assert.Empty(t, slices.Collect(it.PatternPrefix("")))
	assert.Empty(t, slices.Collect(it.NamePrefix("")))
	assert.Empty(t, slices.Collect(it.Routes("")))
}

func BenchmarkIter_Methods(b *testing.B) {
	f, _ := NewRouter()
	for _, route := range staticRoutes {
		require.NoError(b, onlyError(f.Add([]string{route.method}, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.Methods() {

		}
	}
}

func BenchmarkIter_Route(b *testing.B) {
	f, _ := NewRouter()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Add([]string{route.method}, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.Routes("/user/subscriptions/{owner}/{repo}") {

		}
	}
}

func BenchmarkIter_PatternPrefix(b *testing.B) {
	f, _ := NewRouter()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Add([]string{route.method}, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.PatternPrefix("/") {

		}
	}
}

func BenchmarkIter_NamePrefix(b *testing.B) {
	f, _ := NewRouter()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Add([]string{route.method}, route.path, emptyHandler, WithName(route.method+":"+route.path))))
	}

	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.NamePrefix("") {

		}
	}
}

func BenchmarkIter_All(b *testing.B) {
	f, _ := NewRouter()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Add([]string{route.method}, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.All() {

		}
	}
}

func ExampleIter_All() {
	f, _ := NewRouter()
	it := f.Iter()
	for route := range it.All() {
		fmt.Println(slices.Collect(route.Methods()), route.Pattern())
	}
}

func ExampleIter_Names() {
	f, _ := NewRouter()
	it := f.Iter()
	for route := range it.Names() {
		fmt.Println(slices.Collect(route.Methods()), route.Pattern())
	}
}

func ExampleIter_Methods() {
	f, _ := NewRouter()
	it := f.Iter()
	for method := range it.Methods() {
		fmt.Println(method)
	}
}

func ExampleIter_Routes() {
	f, _ := NewRouter()
	it := f.Iter()
	for route := range it.Routes("/hello/{name}") {
		fmt.Println(slices.Collect(route.Methods()), route.Pattern())
	}
}

func ExampleIter_PatternPrefix() {
	f, _ := NewRouter()
	it := f.Iter()
	for route := range it.PatternPrefix("/v1/") {
		fmt.Println(slices.Collect(route.Methods()), route.Pattern())
	}
}

func ExampleIter_NamePrefix() {
	f, _ := NewRouter()
	it := f.Iter()
	for route := range it.NamePrefix("ns:default/") {
		fmt.Println(slices.Collect(route.Methods()), route.Name())
	}
}
