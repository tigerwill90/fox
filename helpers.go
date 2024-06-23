// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// NewTestContext returns a new Router and its associated Context, designed only for testing purpose.
func NewTestContext(w http.ResponseWriter, r *http.Request) (*Router, Context) {
	fox := New()
	c := NewTestContextOnly(fox, w, r)
	return fox, c
}

// NewTestContextOnly returns a new Context associated with the provided Router, designed only for testing purpose.
func NewTestContextOnly(fox *Router, w http.ResponseWriter, r *http.Request) Context {
	return newTextContextOnly(fox, w, r)
}

func newTextContextOnly(fox *Router, w http.ResponseWriter, r *http.Request) *context {
	c := fox.Tree().allocateContext()
	c.resetNil()
	c.fox = fox
	c.req = r
	c.rec.reset(w)
	c.w = &c.rec
	return c
}

func newTestContextTree(t *Tree) *context {
	c := t.allocateContext()
	c.resetNil()
	return c
}

func unwrapContext(t *testing.T, c Context) *context {
	t.Helper()
	cc, ok := c.(*context)
	if !ok {
		t.Fatal("unable to unwrap context")
	}
	return cc
}

func remoteAddr(r *http.Request) string {
	ip, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return ""
	}
	return ip
}

// DebugHandlerFrom returns a HandlerFunc that responds with detailed system and request information for the provided router.
//
// The response includes:
// - Remote IP Address
// - Matched route
// - Route parameters, if any
// - Full HTTP request dump
// - Current time
// - Hostname
// - Operating system and architecture
// - Process ID
// - CPU cores
// - Go version
// - Number of goroutines
// - Memory statistics (allocated memory, total allocated memory, system memory)
// - Server description
//
// Additionally, if a "sleep" query parameter is provided with a valid duration,
// the handler will sleep for the specified duration before responding.
//
// This function is useful for debugging purposes, providing a comprehensive
// overview of the incoming request and the system it is running on.
func DebugHandlerFrom(fox *Router) HandlerFunc {
	return func(c Context) {
		// Sleep if "sleep" query parameter is provided with a valid duration
		if sleep := c.QueryParam("sleep"); sleep != "" {
			if d, err := time.ParseDuration(sleep); err == nil {
				time.Sleep(d)
			}
		}

		// Send the response
		_ = c.String(http.StatusOK, dumpSysInfo(fox, c))
	}
}

// DebugHandler returns a HandlerFunc that responds with detailed system and request information from the current router
// instance running the request. See DebugHandlerFrom for more information.
func DebugHandler() HandlerFunc {
	return func(c Context) {
		// Sleep if "sleep" query parameter is provided with a valid duration
		if sleep := c.QueryParam("sleep"); sleep != "" {
			if d, err := time.ParseDuration(sleep); err == nil {
				time.Sleep(d)
			}
		}

		// Send the response
		_ = c.String(http.StatusOK, dumpSysInfo(c.Fox(), c))
	}
}

func dumpSysInfo(fox *Router, c Context) string {
	req := c.Request()
	path := c.Path()
	params := c.Params()

	// Get host information
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	// Get memory statistics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Dump the request
	requestDump, err := httputil.DumpRequest(req, true)
	if err != nil {
		requestDump = []byte("Failed to dump request")
	}

	tree := fox.Tree()

	// Use strings.Builder to build the response
	var builder strings.Builder
	builder.WriteString("Fox: ")
	builder.WriteString("https://github.com/tigerwill90/fox\n\n")
	builder.WriteString("Router Information:\n")
	builder.WriteString("Redirect Trailing Slash: ")
	builder.WriteString(strconv.FormatBool(fox.redirectTrailingSlash))
	builder.WriteByte('\n')
	builder.WriteString("Auto OPTIONS: ")
	builder.WriteString(strconv.FormatBool(fox.handleOptions))
	builder.WriteByte('\n')
	builder.WriteString("Handle 405: ")
	builder.WriteString(strconv.FormatBool(fox.handleMethodNotAllowed))
	builder.WriteByte('\n')
	builder.WriteString("Registered middleware: ")
	builder.WriteString(strconv.Itoa(len(fox.mws)))
	builder.WriteByte('\n')
	builder.WriteString("Registered route:\n")
	it := NewIterator(tree)
	for it.Rewind(); it.Valid(); it.Next() {
		builder.WriteString("- ")
		builder.WriteString(it.Method())
		builder.WriteString(" ")
		builder.WriteString(it.Path())
		builder.WriteByte('\n')
	}

	builder.WriteString("\n\nHandler Information:\n")
	builder.WriteString("Remote Address: ")
	builder.WriteString(remoteAddr(req))
	builder.WriteByte('\n')
	builder.WriteString("Matched Route: ")
	builder.WriteString(path)
	builder.WriteByte('\n')
	builder.WriteString("Route Parameters:\n")
	if len(params) > 0 {
		for _, param := range params {
			builder.WriteString("- ")
			builder.WriteString(param.Key)
			builder.WriteString(": ")
			builder.WriteString(param.Value)
			builder.WriteByte('\n')
		}
	} else {
		builder.WriteString("- None\n")
	}

	builder.WriteString("\n\nFull Request Dump:\n")
	builder.WriteString(string(requestDump))
	builder.WriteString("\nSystem Information:\n")
	builder.WriteString("Time: ")
	builder.WriteString(time.Now().Format(time.RFC3339))
	builder.WriteByte('\n')
	builder.WriteString("Hostname: ")
	builder.WriteString(hostname)
	builder.WriteByte('\n')
	builder.WriteString("OS: ")
	builder.WriteString(runtime.GOOS)
	builder.WriteByte('\n')
	builder.WriteString("Arch: ")
	builder.WriteString(runtime.GOARCH)
	builder.WriteByte('\n')
	builder.WriteString("Pid: ")
	builder.WriteString(strconv.Itoa(os.Getpid()))
	builder.WriteByte('\n')
	builder.WriteString("CPU Cores: ")
	builder.WriteString(fmt.Sprintf("%d", runtime.NumCPU()))
	builder.WriteByte('\n')
	builder.WriteString("Go Version: ")
	builder.WriteString(runtime.Version())
	builder.WriteByte('\n')
	builder.WriteString("Number of Goroutines: ")
	builder.WriteString(fmt.Sprintf("%d", runtime.NumGoroutine()))
	builder.WriteByte('\n')
	builder.WriteString("Allocated Memory: ")
	builder.WriteString(fmt.Sprintf("%d bytes", memStats.Alloc))
	builder.WriteByte('\n')
	builder.WriteString("Total Allocated Memory: ")
	builder.WriteString(fmt.Sprintf("%d bytes", memStats.TotalAlloc))
	builder.WriteByte('\n')
	builder.WriteString("System Memory: ")
	builder.WriteString(fmt.Sprintf("%d bytes", memStats.Sys))
	builder.WriteByte('\n')

	return builder.String()
}
