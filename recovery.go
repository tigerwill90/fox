// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
)

var stdErr = slog.New(defaultHandler)

// RecoveryFunc is a function type that defines how to handle panics that occur during the
// handling of an HTTP request.
type RecoveryFunc func(c Context, err any)

// Recovery is a middleware that captures panics and recovers from them. It takes a custom handle function
// that will be called with the Context and the value recovered from the panic.
// Note that the middleware check if the panic is caused by http.ErrAbortHandler and re-panic if true
// allowing the http server to handle it as an abort.
func Recovery(handle RecoveryFunc) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			defer recovery(c, handle)
			next(c)
		}
	}
}

// DefaultHandleRecovery is a default implementation of the RecoveryFunc.
// It logs the recovered panic error to stderr, including the stack trace.
// If the response has not been written yet and the error is not caused by a broken connection,
// it sets the status code to http.StatusInternalServerError and writes a generic error message.
func DefaultHandleRecovery(c Context, err any) {
	stdErr.Error(fmt.Sprintf("Recovered: %q\n%s", err, stacktrace(4, 6)))
	if !c.Writer().Written() && !connIsBroken(err) {
		http.Error(c.Writer(), http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func recovery(c Context, handle RecoveryFunc) {
	if err := recover(); err != nil {
		if abortErr, ok := err.(error); ok && errors.Is(abortErr, http.ErrAbortHandler) {
			panic(abortErr)
		}
		handle(c, err)
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
		_, _ = fmt.Fprintf(&b, "called from %s %s:%d\n", frame.Function, frame.File, frame.Line)
		if !more {
			break
		}
		i++
		if i >= nFrames {
			_, _ = fmt.Fprintf(&b, "(rest of stack elided)\n")
			break
		}
	}
	return b.String()
}
