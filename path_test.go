// Copyright 2013 Julien Schmidt. All rights reserved.
// Based on the path package, Copyright 2009 The Go Authors.
// Mount of this source code is governed by a BSD-style license that can be found
// at https://github.com/julienschmidt/httprouter/blob/master/LICENSE.

package fox

import (
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

type cleanPathTest struct {
	path, result string
}

var cleanTests = []cleanPathTest{
	// Already clean
	{"/", "/"},
	{"/abc", "/abc"},
	{"/a/b/c", "/a/b/c"},
	{"/abc/", "/abc/"},
	{"/a/b/c/", "/a/b/c/"},

	// missing root
	{"", "/"},
	{"a/", "/a/"},
	{"abc", "/abc"},
	{"abc/def", "/abc/def"},
	{"a/b/c", "/a/b/c"},

	// Remove doubled slash
	{"//", "/"},
	{"/abc//", "/abc/"},
	{"/abc/def//", "/abc/def/"},
	{"/a/b/c//", "/a/b/c/"},
	{"/abc//def//ghi", "/abc/def/ghi"},
	{"//abc", "/abc"},
	{"///abc", "/abc"},
	{"//abc//", "/abc/"},

	// Remove . elements
	{".", "/"},
	{"./", "/"},
	{"/abc/./def", "/abc/def"},
	{"/./abc/def", "/abc/def"},
	{"/abc/.", "/abc/"},

	// Remove .. elements
	{"..", "/"},
	{"../", "/"},
	{"../../", "/"},
	{"../..", "/"},
	{"../../abc", "/abc"},
	{"/abc/def/ghi/../jkl", "/abc/def/jkl"},
	{"/abc/def/../ghi/../jkl", "/abc/jkl"},
	{"/abc/def/..", "/abc"},
	{"/abc/def/../..", "/"},
	{"/abc/def/../../..", "/"},
	{"/abc/def/../../..", "/"},
	{"/abc/def/../../../ghi/jkl/../../../mno", "/mno"},

	// Combinations
	{"abc/./../def", "/def"},
	{"abc//./../def", "/def"},
	{"abc/../../././../def", "/def"},
}

func TestPathClean(t *testing.T) {
	for _, test := range cleanTests {
		if s := CleanPath(test.path); s != test.result {
			t.Errorf("CleanPath(%q) = %q, want %q", test.path, s, test.result)
		}
		if s := CleanPath(test.result); s != test.result {
			t.Errorf("CleanPath(%q) = %q, want %q", test.result, s, test.result)
		}
	}
}

func TestPathCleanMallocs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping malloc count in short mode")
	}

	for _, test := range cleanTests {
		test := test
		allocs := testing.AllocsPerRun(100, func() { CleanPath(test.result) })
		if allocs > 0 {
			t.Errorf("CleanPath(%q): %v allocs, want zero", test.result, allocs)
		}
	}
}

func BenchmarkPathClean(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, test := range cleanTests {
			CleanPath(test.path)
		}
	}
}

func genLongPaths() (testPaths []cleanPathTest) {
	for i := 1; i <= 1234; i++ {
		ss := strings.Repeat("a", i)

		correctPath := "/" + ss
		testPaths = append(testPaths, cleanPathTest{
			path:   correctPath,
			result: correctPath,
		}, cleanPathTest{
			path:   ss,
			result: correctPath,
		}, cleanPathTest{
			path:   "//" + ss,
			result: correctPath,
		}, cleanPathTest{
			path:   "/" + ss + "/b/..",
			result: correctPath,
		})
	}
	return testPaths
}

func TestPathCleanLong(t *testing.T) {
	cleanTests := genLongPaths()

	for _, test := range cleanTests {
		if s := CleanPath(test.path); s != test.result {
			t.Errorf("CleanPath(%q) = %q, want %q", test.path, s, test.result)
		}
		if s := CleanPath(test.result); s != test.result {
			t.Errorf("CleanPath(%q) = %q, want %q", test.result, s, test.result)
		}
	}
}

func TestFixTrailingSlash(t *testing.T) {
	assert.Equal(t, "/foo/", FixTrailingSlash("/foo"))
	assert.Equal(t, "/foo", FixTrailingSlash("/foo/"))
	assert.Equal(t, "/", FixTrailingSlash(""))
}

func TestSplitHostPath(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		wantHost string
		wantPath string
	}{
		{
			name:     "empty url",
			url:      "",
			wantHost: "",
			wantPath: "/",
		},
		{
			name:     "empty host",
			url:      "/foo",
			wantHost: "",
			wantPath: "/foo",
		},
		{
			name:     "empty host",
			url:      "/",
			wantHost: "",
			wantPath: "/",
		},
		{
			name:     "empty path",
			url:      "a.b.c",
			wantHost: "a.b.c",
			wantPath: "/",
		},
		{
			name:     "host and path",
			url:      "a.b.c/foo",
			wantHost: "a.b.c",
			wantPath: "/foo",
		},
		{
			name:     "host and root path",
			url:      "a.b.c:443/",
			wantHost: "a.b.c",
			wantPath: "/",
		},
		{
			name:     "host, port and path",
			url:      "a.b.c:8080/foo",
			wantHost: "a.b.c",
			wantPath: "/foo",
		},
		{
			name:     "host, port and no path",
			url:      "a.b.c:8080",
			wantHost: "a.b.c",
			wantPath: "/",
		},
		{
			name:     "invalid port",
			url:      "a.b.c:abc/foo",
			wantHost: "a.b.c:abc",
			wantPath: "/foo",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, path := SplitHostPath(tc.url)
			assert.Equal(t, tc.wantHost, host)
			assert.Equal(t, tc.wantPath, path)
		})
	}
}

func BenchmarkPathCleanLong(b *testing.B) {
	cleanTests := genLongPaths()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, test := range cleanTests {
			CleanPath(test.path)
		}
	}
}

func BenchmarkSplitHostPath(b *testing.B) {
	cases := []string{
		"abc.com/foo/bar",
		"exemple.com/a/b/c",
		"a.b.c:8080/",
		"google.com:443/long/long/long/long/path",
	}
	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for _, hostPath := range cases {
			SplitHostPath(hostPath)
		}
	}
}
