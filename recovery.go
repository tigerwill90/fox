// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"bytes"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/tigerwill90/fox/internal/iterutil"
	"github.com/tigerwill90/fox/internal/slogpretty"
)

// Keys for "built-in" logger attribute for the recovery middleware.
// Keys for "built-in" logger attributes used by the recovery middleware.
const (
	// LoggerRouteKey is the key used by the built-in recovery middleware for the matched route
	// when the log method is called. The associated [slog.Value] is a string.
	LoggerRouteKey = "route"
	// LoggerParamsKey is the key used by the built-in recovery middleware for route parameters
	// when the log method is called. The associated [slog.Value] is a [slog.GroupValue] containing parameter
	// key-value pairs.
	LoggerParamsKey = "params"
	// LoggerPanicKey is the key used by the built-in recovery middleware for the panic value
	// when the log method is called. The associated [slog.Value] is any.
	LoggerPanicKey = "panic"
)

var reqHeaderSep = []byte("\r\n")

// RecoveryFunc is a function type that defines how to handle panics that occur during the
// handling of an HTTP request.
type RecoveryFunc func(c Context, err any)

// CustomRecoveryWithLogHandler returns a middleware for a given [slog.Handler] that recovers from any panics,
// logs the error, request details, and stack trace, and then calls the provided handle function to handle the recovery.
func CustomRecoveryWithLogHandler(handler slog.Handler, handle RecoveryFunc) MiddlewareFunc {
	slogger := slog.New(handler)
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			defer recovery(slogger, c, handle)
			next(c)
		}
	}
}

// CustomRecovery returns a middleware that recovers from any panics, logs the error, request details, and stack trace
// using the built-in fox's slog handler and then calls the provided handle function to handle the recovery.
func CustomRecovery(handle RecoveryFunc) MiddlewareFunc {
	return CustomRecoveryWithLogHandler(slogpretty.DefaultHandler, handle)
}

// Recovery returns a middleware that recovers from any panics, logs the error, request details, and stack trace
// using the built-in fox's slog handler and writes a 500 status code response if a panic occurs.
func Recovery() MiddlewareFunc {
	return CustomRecovery(DefaultHandleRecovery)
}

// DefaultHandleRecovery is a default implementation of the [RecoveryFunc].
// It responds with a status code 500 and writes a generic error message.
func DefaultHandleRecovery(c Context, _ any) {
	http.Error(c.Writer(), http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func recovery(logger *slog.Logger, c Context, handle RecoveryFunc) {
	if err := recover(); err != nil {
		if e, ok := err.(error); ok && errors.Is(e, http.ErrAbortHandler) {
			panic(e)
		}

		var sb strings.Builder

		sb.WriteString("Recovered from PANIC\n")

		httpRequest, _ := httputil.DumpRequest(c.Request(), false)
		sb.Grow(len(httpRequest))

		if before, after, found := bytes.Cut(httpRequest, reqHeaderSep); found {
			sb.WriteString("Request Dump:\n")
			sb.Write(before)
			for header := range iterutil.SplitBytesSeq(after, reqHeaderSep) {
				sb.Write(reqHeaderSep)
				idx := bytes.IndexByte(header, ':')
				if idx < 0 {
					continue
				}
				if slices.Contains(blacklistedHeader, string(header[:idx])) {
					sb.Write(header[:idx])
					sb.WriteString(": <redacted>")
					continue
				}
				sb.Write(header)
			}
		}

		sb.WriteString("Stack:\n")
		sb.WriteString(stacktrace(3, 6))

		var params []any
		if c.Route() != nil {
			params = make([]any, 0, c.Route().ParamsLen())
			params = slices.AppendSeq(params, mapParamsToAttr(c.Params()))
		}

		pattern := c.Pattern()
		if pattern == "" {
			pattern = scopeToString(c.Scope())
		}

		logger.Error(
			sb.String(),
			slog.String(LoggerRouteKey, pattern),
			slog.Group(LoggerParamsKey, params...),
			slog.Any(LoggerPanicKey, err),
		)

		if !c.Writer().Written() && !connIsBroken(err) {
			handle(c, err)
		}
	}
}

func connIsBroken(err any) bool {
	//goland:noinspection GoTypeAssertionOnErrors
	if ne, ok := err.(*net.OpError); ok {
		var se *os.SyscallError
		if errors.As(ne, &se) {
			seStr := strings.ToLower(se.Error())
			return strings.Contains(seStr, "broken pipe") || strings.Contains(seStr, "connection reset by peer")
		}
	}
	return false
}

func stacktrace(skip, nFrames int) string {
	pcs := make([]uintptr, nFrames+1)
	n := runtime.Callers(skip+1, pcs)
	if n == 0 {
		return "(no stack)"
	}
	frames := runtime.CallersFrames(pcs[:n])
	var b strings.Builder
	i := 0
	for {
		frame, more := frames.Next()
		if i > 0 {
			b.WriteByte('\n')
		}
		_, _ = fmt.Fprintf(&b, "called from %s %s:%d", frame.Function, frame.File, frame.Line)
		if !more {
			break
		}
		i++
		if i >= nFrames {
			_, _ = fmt.Fprintf(&b, "\n(rest of stack elided)")
			break
		}
	}
	return b.String()
}

func mapParamsToAttr(params iter.Seq[Param]) iter.Seq[any] {
	return func(yield func(any) bool) {
		for p := range params {
			if !yield(slog.String(p.Key, p.Value)) {
				break
			}
		}
	}
}

func scopeToString(scope HandlerScope) string {
	var strScope string
	switch scope {
	case OptionsHandler:
		strScope = "OptionsHandler"
	case NoMethodHandler:
		strScope = "NoMethodHandler"
	case RedirectSlashHandler:
		strScope = "RedirectSlashHandler"
	case RedirectPathHandler:
		strScope = "RedirectPathHandler"
	case NoRouteHandler:
		strScope = "NoRouteHandler"
	default:
		strScope = "UnknownHandler"
	}
	return strScope
}
