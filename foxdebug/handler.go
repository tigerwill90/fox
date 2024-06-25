// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package foxdebug

import (
	"fmt"
	"github.com/tigerwill90/fox"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var Version = "v0.14.0"

// DebugHandler returns a HandlerFunc that responds with detailed system and request information. Additionally, if a
// "sleep" query parameter is provided with a valid duration, the handler will sleep for the specified duration
// before responding. This function may leak sensitive information and is only useful for debugging purposes, providing
// a comprehensive overview of the incoming request and the system it is running on.
func DebugHandler() fox.HandlerFunc {
	return func(c fox.Context) {
		// Sleep if "sleep" query parameter is provided with a valid duration
		if sleep := c.QueryParam("sleep"); sleep != "" {
			if d, err := time.ParseDuration(sleep); err == nil {
				time.Sleep(d)
			}
		}

		// Send the response
		c.SetHeader(fox.HeaderServer, fmt.Sprintf("fox %s", Version))
		_ = c.String(http.StatusOK, dumpSysInfo(c))
	}
}

func dumpSysInfo(c fox.Context) string {
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

	f := c.Fox()
	tree := f.Tree()

	// Use strings.Builder to build the response
	var builder strings.Builder
	builder.WriteString("Fox: awesome and blazing fast Go router!\n")
	builder.WriteString("Repo: https://github.com/tigerwill90/fox\n")
	builder.WriteString("Version: ")
	builder.WriteString(Version)
	builder.WriteString("\n\n")
	builder.WriteString("Router Information:\n")
	builder.WriteString("Redirect Trailing Slash: ")
	builder.WriteString(strconv.FormatBool(f.RedirectTrailingSlashEnabled()))
	builder.WriteByte('\n')
	builder.WriteString("Ignore Trailing Slash: ")
	builder.WriteString(strconv.FormatBool(f.IgnoreTrailingSlashEnabled()))
	builder.WriteByte('\n')
	builder.WriteString("Auto OPTIONS: ")
	builder.WriteString(strconv.FormatBool(f.AutoOptionsEnabled()))
	builder.WriteByte('\n')
	builder.WriteString("Handle 405: ")
	builder.WriteString(strconv.FormatBool(f.MethodNotAllowedEnabled()))
	builder.WriteByte('\n')
	builder.WriteString("Registered route:\n")
	it := fox.NewIterator(tree)
	for it.Rewind(); it.Valid(); it.Next() {
		builder.WriteString("- ")
		builder.WriteString(it.Method())
		builder.WriteString(" ")
		builder.WriteString(it.Path())
		builder.WriteByte('\n')
	}

	builder.WriteString("\n\nHandler Information:\n")
	if ip := c.RemoteIP(); ip != nil {
		builder.WriteString("Remote Address: ")
		builder.WriteString(ip.String())
		builder.WriteByte('\n')
	}
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
	builder.WriteString("Go Version: ")
	builder.WriteString(runtime.Version())
	builder.WriteByte('\n')
	builder.WriteString("Pid: ")
	builder.WriteString(strconv.Itoa(os.Getpid()))
	builder.WriteByte('\n')
	builder.WriteString("CPU Cores: ")
	builder.WriteString(fmt.Sprintf("%d", runtime.NumCPU()))
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
