// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"fmt"
	"github.com/tigerwill90/fox/internal/slogpretty"
	"iter"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime"
	"slices"
	"strings"
)

// RecoveryFunc is a function type that defines how to handle panics that occur during the
// handling of an HTTP request.
type RecoveryFunc func(c Context, err any)

// CustomRecoveryWithLogHandler returns a middleware for a given slog.Handler that recovers from any panics,
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

// CustomRecovery returns a middleware that recovers from any panics, logs the error, request details, and stack trace,
// and then calls the provided handle function to handle the recovery.
func CustomRecovery(handle RecoveryFunc) MiddlewareFunc {
	return CustomRecoveryWithLogHandler(slogpretty.DefaultHandler, handle)
}

// Recovery returns a middleware that recovers from any panics, logs the error, request details, and stack trace,
// and writes a 500 status code response if a panic occurs.
func Recovery() MiddlewareFunc {
	return CustomRecovery(DefaultHandleRecovery)
}

// DefaultHandleRecovery is a default implementation of the RecoveryFunc.
// It responds with a status code 500 and writes a generic error message.
func DefaultHandleRecovery(c Context, _ any) {
	http.Error(c.Writer(), http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func recovery(logger *slog.Logger, c Context, handle RecoveryFunc) {
	if err := recover(); err != nil {
		if abortErr, ok := err.(error); ok && errors.Is(abortErr, http.ErrAbortHandler) {
			panic(abortErr)
		}

		var sb strings.Builder

		sb.WriteString("Recovered from PANIC\n")
		sb.WriteString("Request Dump:\n")

		httpRequest, _ := httputil.DumpRequest(c.Request(), false)
		headers := strings.Split(string(httpRequest), "\r\n")
		sb.WriteString(headers[0])
		for i := 1; i < len(headers); i++ {
			sb.WriteString("\r\n")
			current := strings.Split(headers[i], ":")
			if slices.Contains(blacklistedHeader, current[0]) {
				sb.WriteString(current[0])
				sb.WriteString(": <redacted>")
				continue
			}
			sb.WriteString(headers[i])
		}

		sb.WriteString("Stack:\n")
		sb.WriteString(stacktrace(3, 6))

		params := slices.Collect(mapParamsToAttr(c.Params()))
		var annotations []any
		if route := c.Route(); route != nil {
			annotations = slices.Collect(mapAnnotationsToAttr(route.Annotations()))
		}

		logger.Error(
			sb.String(),
			slog.String("path", c.Path()),
			slog.Group("params", params...),
			slog.Group("annotations", annotations...),
			slog.Any("error", err),
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

func mapAnnotationsToAttr(annotations iter.Seq[Annotation]) iter.Seq[any] {
	return func(yield func(any) bool) {
		for a := range annotations {
			if !yield(slog.Any(a.Key, a.Value)) {
				break
			}
		}
	}
}
