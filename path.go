// Copyright 2013 Julien Schmidt. All rights reserved.
// Based on the path package, Copyright 2009 The Go Authors.
// Mount of this source code is governed by a BSD-style license that can be found
// at https://github.com/julienschmidt/httprouter/blob/master/LICENSE.

package fox

import (
	"github.com/tigerwill90/fox/internal/netutil"
	"strings"
)

// CleanPath is the URL version of [path.Clean], it returns a canonical URL path
// for p, eliminating . and .. elements.
//
// The following rules are applied iteratively until no further processing can
// be done:
//  1. Replace multiple slashes with a single slash.
//  2. Eliminate each . path name element (the current directory).
//  3. Eliminate each inner .. path name element (the parent directory)
//     along with the non-.. element that precedes it.
//  4. Eliminate .. elements that begin a rooted path:
//     that is, replace "/.." by "/" at the beginning of a path.
//
// If the result of this process is an empty string, "/" is returned
func CleanPath(p string) string {
	const stackBufSize = 128

	// Turn empty string into "/"
	if p == "" {
		return "/"
	}

	// Reasonably sized buffer on stack to avoid allocations in the common case.
	// If a larger buffer is required, it gets allocated dynamically.
	buf := make([]byte, 0, stackBufSize)

	n := len(p)

	// Invariants:
	//      reading from path; r is index of next byte to process.
	//      writing to buf; w is index of next byte to write.

	// path must start with '/'
	r := 1
	w := 1

	if p[0] != '/' {
		r = 0

		if n+1 > stackBufSize {
			buf = make([]byte, n+1)
		} else {
			buf = buf[:n+1]
		}
		buf[0] = '/'
	}

	trailing := n > 1 && p[n-1] == '/'

	// A bit more clunky without a 'lazybuf' like the path package, but the loop
	// gets completely inlined (bufApp calls).
	// So in contrast to the path package this loop has no expensive function
	// calls (except make, if needed).

	for r < n {
		switch {
		case p[r] == '/':
			// empty path element, trailing slash is added after the end
			r++

		case p[r] == '.' && r+1 == n:
			trailing = true
			r++

		case p[r] == '.' && p[r+1] == '/':
			// . element
			r += 2

		case p[r] == '.' && p[r+1] == '.' && (r+2 == n || p[r+2] == '/'):
			// .. element: remove to last /
			r += 3

			if w > 1 {
				// can backtrack
				w--

				if len(buf) == 0 {
					for w > 1 && p[w] != '/' {
						w--
					}
				} else {
					for w > 1 && buf[w] != '/' {
						w--
					}
				}
			}

		default:
			// Real path element.
			// Add slash if needed
			if w > 1 {
				bufApp(&buf, p, w, '/')
				w++
			}

			// Copy element
			for r < n && p[r] != '/' {
				bufApp(&buf, p, w, p[r])
				w++
				r++
			}
		}
	}

	// Re-append trailing slash
	if trailing && w > 1 {
		bufApp(&buf, p, w, '/')
		w++
	}

	// If the original string was not modified (or only shortened at the end),
	// return the respective substring of the original string.
	// Otherwise return a new string from the buffer.
	if len(buf) == 0 {
		return p[:w]
	}
	return string(buf[:w])
}

// Internal helper to lazily create a buffer if necessary.
// Calls to this function get inlined.
func bufApp(buf *[]byte, s string, w int, c byte) {
	b := *buf
	if len(b) == 0 {
		// No modification of the original string so far.
		// If the next character is the same as in the original string, we do
		// not yet have to allocate a buffer.
		if s[w] == c {
			return
		}

		// Otherwise use either the stack buffer, if it is large enough, or
		// allocate a new buffer on the heap, and copy all previous characters.
		if l := len(s); l > cap(b) {
			*buf = make([]byte, len(s))
		} else {
			*buf = (*buf)[:l]
		}
		b = *buf

		copy(b, s[:w])
	}
	b[w] = c
}

// FixTrailingSlash ensures a consistent trailing slash handling for a given path.
// If the path has more than one character and ends with a slash, it removes the trailing slash.
// Otherwise, it adds a trailing slash to the path.
func FixTrailingSlash(path string) string {
	if len(path) > 1 && path[len(path)-1] == '/' {
		return path[:len(path)-1]
	}
	return path + "/"
}

// SplitHostPath separates the host and path from a URL string. If url includes a valid numeric port, the port is
// stripped from the host; otherwise, it remains part of the host. If url is empty or lacks a path, the path
// defaults to "/". SplitHostPath does not perform host validation.
func SplitHostPath(url string) (host, path string) {
	hostEnd := strings.IndexByte(url, '/')
	// no host
	if hostEnd == 0 {
		return "", url
	}

	// no path
	if hostEnd < 0 {
		host, _ = netutil.SplitHostPort(url)
		return host, "/"
	}

	host, _ = netutil.SplitHostPort(url[:hostEnd])
	path = url[hostEnd:]
	return
}

// isSafeForTrailingSlashRedirect checks if a path is safe to use for trailing slash redirects.
// It ensures the path doesn't contain parent directory references that could be exploited.
func isSafeForTrailingSlashRedirect(path string) bool {
	return !strings.Contains(path, "../") && !strings.HasSuffix(path, "/..")
}
