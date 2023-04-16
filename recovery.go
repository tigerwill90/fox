// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
)

var stdErr = log.New(os.Stderr, "", log.LstdFlags)

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
	stdErr.Printf("[PANIC] %q panic recovered\n%s", err, debug.Stack())
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
	if ne, ok := err.(*net.OpError); ok {
		var se *os.SyscallError
		if errors.As(ne, &se) {
			seStr := strings.ToLower(se.Error())
			return strings.Contains(seStr, "broken pipe") || strings.Contains(seStr, "connection reset by peer")
		}
	}
	return false
}
