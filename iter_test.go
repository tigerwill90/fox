// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestIter_NamesBreak(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler, WithName(http.MethodGet+":"+rte))))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler, WithName(http.MethodPost+":"+rte))))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler, WithName(http.MethodHead+":"+rte))))
	}

	it := f.Iter()
	iteration := 0
	for range it.Names() {
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
	req := httptest.NewRequest(http.MethodGet, "/john/doe/1/2/3", nil)
	for range it.Reverse(it.Methods(), req) {
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

	for method, route := range it.PatternPrefix(iterutil.SeqOf(http.MethodHead), "/") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Pattern())
	}

	assert.Len(t, results, 1)
	assert.ElementsMatch(t, routesCases, results[http.MethodHead])
}

func TestIter_PatternPrefix(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler)))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler)))
	}

	want := []string{"/foo/bar/{baz}", "/foo/bar/{baz}/{name}"}
	results := make(map[string][]string)

	it := f.Iter()
	for method, route := range it.PatternPrefix(it.Methods(), "/foo") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Pattern())
	}

	for key := range results {
		assert.ElementsMatch(t, want, results[key])
	}
}

func TestIter_PatternStrictPrefix(t *testing.T) {
	f, _ := New()
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/{a}/b/x", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/{a}/b/y", emptyHandler)))
	require.NoError(t, onlyError(f.Handle(http.MethodGet, "/{other}/b/z", emptyHandler)))

	want := []string{"/{a}/b/x", "/{a}/b/y"}
	results := make(map[string][]string)

	it := f.Iter()
	for method, route := range it.PatternPrefix(it.Methods(), "/{a}/b") {
		assert.NotNil(t, route)
		results[method] = append(results[method], route.Pattern())
	}

	for key := range results {
		assert.Equal(t, want, results[key])
	}
}

func TestIter_NamesPrefix(t *testing.T) {
	f, _ := New()
	for _, rte := range routesCases {
		require.NoError(t, onlyError(f.Handle(http.MethodGet, rte, emptyHandler, WithName(http.MethodGet+":"+rte))))
		require.NoError(t, onlyError(f.Handle(http.MethodPost, rte, emptyHandler, WithName(http.MethodPost+":"+rte))))
		require.NoError(t, onlyError(f.Handle(http.MethodHead, rte, emptyHandler, WithName(http.MethodHead+":"+rte))))
	}

	want := []string{"/foo/bar/{baz}", "/foo/bar/{baz}/{name}", "/fox/router", "/john/doe", "/john/doe/*{args}"}

	it := f.Iter()
	result := slices.Collect(iterutil.Map(iterutil.Right(it.NamePrefix(it.Methods(), "GET")), func(a *Route) string {
		return a.Pattern()
	}))
	assert.Equal(t, want, result)
}

func TestIter_NoData(t *testing.T) {
	f, _ := New()
	it := f.Iter()

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	assert.Empty(t, slices.Collect(iterutil.Left(it.PatternPrefix(iterutil.SeqOf("GET"), "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.PatternPrefix(iterutil.SeqOf("CONNECT"), "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Reverse(iterutil.SeqOf("GET"), req))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Reverse(iterutil.SeqOf("CONNECT"), req))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Routes(iterutil.SeqOf("GET"), "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.Routes(iterutil.SeqOf("CONNECT"), "/"))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.NamePrefix(iterutil.SeqOf("GET"), ""))))
	assert.Empty(t, slices.Collect(iterutil.Left(it.NamePrefix(iterutil.SeqOf("CONNECT"), ""))))
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
	for method, route := range it.PatternPrefix(iterutil.SeqOf(http.MethodHead), "/foo") {
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

	req := httptest.NewRequest(http.MethodGet, "/user/subscriptions/fox/fox", nil)
	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.Reverse(it.Methods(), req) {

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

func BenchmarkIter_PatternPrefix(b *testing.B) {
	f, _ := New()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Handle(route.method, route.path, emptyHandler)))
	}
	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.PatternPrefix(it.Methods(), "/") {

		}
	}
}

func BenchmarkIter_NamePrefix(b *testing.B) {
	f, _ := New()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Handle(route.method, route.path, emptyHandler, WithName(route.method+":"+route.path))))
	}

	it := f.Iter()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for range it.NamePrefix(it.Methods(), "") {

		}
	}
}

func BenchmarkIter_All(b *testing.B) {
	f, _ := New()
	for _, route := range githubAPI {
		require.NoError(b, onlyError(f.Handle(route.method, route.path, emptyHandler)))
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
	f, _ := New()
	it := f.Iter()
	for method, route := range it.All() {
		fmt.Println(method, route.Pattern())
	}
}

func ExampleIter_Names() {
	f, _ := New()
	it := f.Iter()
	for method, route := range it.Names() {
		fmt.Println(method, route.Name())
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

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)

	for method, route := range it.Reverse(slices.Values([]string{"GET", "POST"}), req) {
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

func ExampleIter_PatternPrefix() {
	f, _ := New()
	it := f.Iter()
	for method, route := range it.PatternPrefix(slices.Values([]string{"GET", "POST"}), "ns:default/admin") {
		fmt.Println(method, route.Pattern())
	}
}

func ExampleIter_NamePrefix() {
	f, _ := New()
	it := f.Iter()
	for method, route := range it.NamePrefix(slices.Values([]string{"GET", "POST"}), "ns:default/") {
		fmt.Println(method, route.Name())
	}
}
