// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.
//
// This implementation is influenced by the work done by goji and chi libraries,
// with additional optimizations to avoid unnecessary memory allocations.
// See their respective licenses for more information:
// https://github.com/zenazn/goji/blob/master/LICENSE
// https://github.com/go-chi/chi/blob/master/LICENSE

package fox

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"
)

var _ ResponseWriter = (*recorder)(nil)

var copyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// ResponseWriter extends http.ResponseWriter and provides methods to retrieve the recorded status code,
// written state, and response size.
type ResponseWriter interface {
	http.ResponseWriter
	io.StringWriter
	io.ReaderFrom
	// Status recorded after Write and WriteHeader.
	Status() int
	// Written returns true if the response has been written.
	Written() bool
	// Size returns the size of the written response.
	Size() int
	// FlushError flushes buffered data to the client. If flush is not supported, FlushError returns an error
	// matching http.ErrNotSupported. See http.Flusher for more details.
	FlushError() error
	// Hijack lets the caller take over the connection. If hijacking the connection is not supported, Hijack returns
	// an error matching http.ErrNotSupported. See http.Hijacker for more details.
	Hijack() (net.Conn, *bufio.ReadWriter, error)
	// Push initiates an HTTP/2 server push. Push returns http.ErrNotSupported if the client has disabled push or if push
	// is not supported on the underlying connection. See http.Pusher for more details.
	Push(target string, opts *http.PushOptions) error
	// SetReadDeadline sets the deadline for reading the entire request, including the body. Reads from the request
	// body after the deadline has been exceeded will return an error. A zero value means no deadline. Setting the read
	// deadline after it has been exceeded will not extend it. If SetReadDeadline is not supported, it returns
	// an error matching http.ErrNotSupported.
	SetReadDeadline(deadline time.Time) error
	// SetWriteDeadline sets the deadline for writing the response. Writes to the response body after the deadline has
	// been exceeded will not block, but may succeed if the data has been buffered. A zero value means no deadline.
	// Setting the write deadline after it has been exceeded will not extend it. If SetWriteDeadline is not supported,
	// it returns an error matching http.ErrNotSupported.
	SetWriteDeadline(deadline time.Time) error
}

const notWritten = -1

type recorder struct {
	http.ResponseWriter
	size     int
	status   int
	hijacked bool
}

func (r *recorder) reset(w http.ResponseWriter) {
	r.ResponseWriter = w
	r.size = notWritten
	r.status = http.StatusOK
	r.hijacked = false
}

// Status recorded after Write or WriteHeader.
func (r *recorder) Status() int {
	return r.status
}

// Written returns true if the response has been written.
func (r *recorder) Written() bool {
	return r.size != notWritten
}

// Size returns the size of the written response.
func (r *recorder) Size() int {
	if r.size < 0 {
		return 0
	}
	return r.size
}

func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// WriteHeader sends an HTTP response header with the provided
// status code. See http.ResponseWriter for more details.
func (r *recorder) WriteHeader(code int) {
	if r.hijacked {
		caller := relevantCaller()
		log.Printf("http: response.WriteHeader on hijacked connection from %s (%s:%d)", caller.Function, path.Base(caller.File), caller.Line)
		return
	}
	if r.size != notWritten {
		caller := relevantCaller()
		log.Printf("http: superfluous response.WriteHeader call from %s (%s:%d)", caller.Function, path.Base(caller.File), caller.Line)
		return
	}

	// Handle informational headers.
	// We shouldn't send any further headers after 101 Switching Protocols,
	// so it takes the non-informational path.
	if code >= 100 && code <= 199 && code != http.StatusSwitchingProtocols {
		r.ResponseWriter.WriteHeader(code)
		return
	}

	r.size = 0
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Write writes the data to the connection as part of an HTTP reply.
// See http.ResponseWriter for more details.
func (r *recorder) Write(buf []byte) (n int, err error) {
	if r.hijacked {
		if len(buf) > 0 {
			caller := relevantCaller()
			log.Printf("http: response.Write on hijacked connection from %s (%s:%d)", caller.Function, path.Base(caller.File), caller.Line)
		}
		return 0, http.ErrHijacked
	}

	if r.size == notWritten {
		r.size = 0
		r.ResponseWriter.WriteHeader(r.status)
	}

	n, err = r.ResponseWriter.Write(buf)
	r.size += n
	return
}

// WriteString writes the provided string to the underlying connection
// as part of an HTTP reply. The method returns the number of bytes written
// and an error, if any.
func (r *recorder) WriteString(s string) (n int, err error) {
	if r.hijacked {
		if len(s) > 0 {
			caller := relevantCaller()
			log.Printf("http: response.Write on hijacked connection from %s (%s:%d)", caller.Function, path.Base(caller.File), caller.Line)
		}
		return 0, http.ErrHijacked
	}

	if r.size == notWritten {
		r.size = 0
		r.ResponseWriter.WriteHeader(r.status)
	}

	n, err = io.WriteString(r.ResponseWriter, s)
	r.size += n
	return
}

// ReadFrom reads data from src until EOF or error. The return value n is the number of bytes read.
// Any error except EOF encountered during the read is also returned.
func (r *recorder) ReadFrom(src io.Reader) (n int64, err error) {
	if rf, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(src)
		if err == nil {
			if r.size == notWritten {
				r.size = 0
			}
			r.size += int(n)
		}
		return n, err
	}

	// Fallback in compatibility mode.
	bufp := copyBufPool.Get().(*[]byte)
	buf := *bufp
	n, err = io.CopyBuffer(onlyWrite{r}, src, buf)
	copyBufPool.Put(bufp)
	return
}

// FlushError flushes buffered data to the client. If flush is not supported, FlushError returns an error
// matching http.ErrNotSupported. See http.Flusher for more details.
func (r *recorder) FlushError() error {
	switch flusher := r.ResponseWriter.(type) {
	case interface{ FlushError() error }:
		if r.size == notWritten {
			r.WriteHeader(r.status)
		}
		return flusher.FlushError()
	case http.Flusher:
		if r.size == notWritten {
			r.WriteHeader(r.status)
		}
		flusher.Flush()
		return nil
	default:
		return ErrNotSupported()
	}
}

// Push initiates an HTTP/2 server push. Push returns http.ErrNotSupported if the client has disabled push or if push
// is not supported on the underlying connection. See http.Pusher for more details.
func (r *recorder) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := r.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Hijack lets the caller take over the connection. If hijacking the connection is not supported, Hijack returns
// an error matching http.ErrNotSupported. See http.Hijacker for more details.
func (r *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := r.ResponseWriter.(http.Hijacker); ok {
		r.hijacked = true
		return hijacker.Hijack()
	}
	return nil, nil, ErrNotSupported()
}

// SetReadDeadline sets the deadline for reading the entire request, including the body. Reads from the request
// body after the deadline has been exceeded will return an error. A zero value means no deadline. Setting the read
// deadline after it has been exceeded will not extend it. If SetReadDeadline is not supported, it returns
// an error matching http.ErrNotSupported.
func (r *recorder) SetReadDeadline(deadline time.Time) error {
	if w, ok := r.ResponseWriter.(interface{ SetReadDeadline(time.Time) error }); ok {
		return w.SetReadDeadline(deadline)
	}
	return ErrNotSupported()
}

// SetWriteDeadline sets the deadline for writing the response. Writes to the response body after the deadline has
// been exceeded will not block, but may succeed if the data has been buffered. A zero value means no deadline.
// Setting the write deadline after it has been exceeded will not extend it. If SetWriteDeadline is not supported,
// it returns an error matching http.ErrNotSupported.
func (r *recorder) SetWriteDeadline(deadline time.Time) error {
	if w, ok := r.ResponseWriter.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return w.SetWriteDeadline(deadline)
	}
	return ErrNotSupported()
}

type noUnwrap struct {
	ResponseWriter
}

type onlyWrite struct {
	io.Writer
}

type noopWriter struct {
	h http.Header
}

func (n noopWriter) Header() http.Header {
	return n.h
}

func (n noopWriter) Write([]byte) (int, error) {
	panic(fmt.Errorf("%w: attempt to write on a clone", ErrDiscardedResponseWriter))
}

func (n noopWriter) WriteHeader(int) {
	panic(fmt.Errorf("%w: attempt to write on a clone", ErrDiscardedResponseWriter))
}

func relevantCaller() runtime.Frame {
	pc := make([]uintptr, 16)
	n := runtime.Callers(1, pc)
	frames := runtime.CallersFrames(pc[:n])
	var frame runtime.Frame
	for {
		f, more := frames.Next()
		if !strings.HasPrefix(f.Function, "github.com/tigerwill90/fox.") {
			return f
		}
		if !more {
			break
		}
	}
	return frame
}

var errHttpNotSupported = fmt.Errorf("%w", http.ErrNotSupported)

// ErrNotSupported returns an error that Is ErrNotSupported, but is not == to it.
func ErrNotSupported() error {
	return errHttpNotSupported
}
