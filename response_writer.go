// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

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

const copyBufPoolSize = 32 * 1024

var copyBufPool = sync.Pool{New: func() any { return new([copyBufPoolSize]byte) }}

func getCopyBuf() []byte {
	return copyBufPool.Get().(*[copyBufPoolSize]byte)[:]
}

func putCopyBuf(b []byte) {
	if len(b) != copyBufPoolSize {
		panic("trying to put back buffer of the wrong size in the copyBufPool")
	}
	copyBufPool.Put((*[copyBufPoolSize]byte)(b))
}

// ResponseWriter extends [http.ResponseWriter] and provides methods to retrieve the recorded status code,
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
	// matching [http.ErrNotSupported]. See [http.Flusher] for more details.
	FlushError() error
	// Hijack lets the caller take over the connection. If hijacking the connection is not supported, Hijack returns
	// an error matching [http.ErrNotSupported]. See [http.Hijacker] for more details.
	Hijack() (net.Conn, *bufio.ReadWriter, error)
	// Push initiates an HTTP/2 server push. Push returns [http.ErrNotSupported] if the client has disabled push or if push
	// is not supported on the underlying connection. See [http.Pusher] for more details.
	Push(target string, opts *http.PushOptions) error
	// SetReadDeadline sets the deadline for reading the entire request, including the body. Reads from the request
	// body after the deadline has been exceeded will return an error. A zero value means no deadline. Setting the read
	// deadline after it has been exceeded will not extend it. If SetReadDeadline is not supported, it returns
	// an error matching [http.ErrNotSupported].
	SetReadDeadline(deadline time.Time) error
	// SetWriteDeadline sets the deadline for writing the response. Writes to the response body after the deadline has
	// been exceeded will not block, but may succeed if the data has been buffered. A zero value means no deadline.
	// Setting the write deadline after it has been exceeded will not extend it. If SetWriteDeadline is not supported,
	// it returns an error matching [http.ErrNotSupported].
	SetWriteDeadline(deadline time.Time) error
	// EnableFullDuplex indicates that the request handler will interleave reads from [http.Request.Body]
	// with writes to the [ResponseWriter].
	//
	// For HTTP/1 requests, the Go HTTP server by default consumes any unread portion of
	// the request body before beginning to write the response, preventing handlers from
	// concurrently reading from the request and writing the response.
	// Calling EnableFullDuplex disables this behavior and permits handlers to continue to read
	// from the request while concurrently writing the response.
	//
	// For HTTP/2 requests, the Go HTTP server always permits concurrent reads and responses.
	// If EnableFullDuplex is not supported, it returns an error matching [http.ErrNotSupported].
	EnableFullDuplex() error
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
// status code. See [http.ResponseWriter] for more details.
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
// See [http.ResponseWriter] for more details.
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
			log.Printf("http: response.WriteString on hijacked connection from %s (%s:%d)", caller.Function, path.Base(caller.File), caller.Line)
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
	if r.hijacked {
		return 0, http.ErrHijacked
	}

	rf, ok := r.ResponseWriter.(io.ReaderFrom)
	if !ok {
		// Fallback in compatibility mode.
		buf := getCopyBuf()
		n, err = io.CopyBuffer(onlyWrite{r}, src, buf)
		putCopyBuf(buf)
		return
	}

	// Copy the first bytes through Write so the header is not committed before
	// the source produces data (see golang.org/issue/5660).
	if r.size == notWritten {
		buf := getCopyBuf()
		n, err = io.CopyBuffer(onlyWrite{r}, io.LimitReader(src, int64(len(buf))), buf)
		putCopyBuf(buf)
		if err != nil || n < int64(len(buf)) {
			return n, err
		}
	}

	nr, err := rf.ReadFrom(src)
	r.size += int(nr)
	n += nr
	return n, err
}

// FlushError flushes buffered data to the client. If flush is not supported, FlushError returns an error
// matching [http.ErrNotSupported]. See [http.Flusher] for more details.
func (r *recorder) FlushError() error {
	// Same walk as http.ResponseController, but the status is committed only once a flusher is found.
	w := r.ResponseWriter
	for {
		switch flusher := w.(type) {
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
		case interface{ Unwrap() http.ResponseWriter }:
			w = flusher.Unwrap()
		default:
			return ErrNotSupported()
		}
	}
}

// Push initiates an HTTP/2 server push. Push returns [http.ErrNotSupported] if the client has disabled push or if push
// is not supported on the underlying connection. See [http.Pusher] for more details.
func (r *recorder) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := r.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Hijack lets the caller take over the connection. If hijacking the connection is not supported, Hijack returns
// an error matching [http.ErrNotSupported]. See [http.Hijacker] for more details.
func (r *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, err := http.NewResponseController(r.ResponseWriter).Hijack()
	if err == nil {
		r.hijacked = true
	}
	return c, rw, err
}

// SetReadDeadline sets the deadline for reading the entire request, including the body. Reads from the request
// body after the deadline has been exceeded will return an error. A zero value means no deadline. Setting the read
// deadline after it has been exceeded will not extend it. If SetReadDeadline is not supported, it returns
// an error matching [http.ErrNotSupported].
func (r *recorder) SetReadDeadline(deadline time.Time) error {
	return http.NewResponseController(r.ResponseWriter).SetReadDeadline(deadline)
}

// SetWriteDeadline sets the deadline for writing the response. Writes to the response body after the deadline has
// been exceeded will not block, but may succeed if the data has been buffered. A zero value means no deadline.
// Setting the write deadline after it has been exceeded will not extend it. If SetWriteDeadline is not supported,
// it returns an error matching [http.ErrNotSupported].
func (r *recorder) SetWriteDeadline(deadline time.Time) error {
	return http.NewResponseController(r.ResponseWriter).SetWriteDeadline(deadline)
}

// EnableFullDuplex indicates that the request handler will interleave reads from [http.Request.Body]
// with writes to the [ResponseWriter].
//
// For HTTP/1 requests, the Go HTTP server by default consumes any unread portion of
// the request body before beginning to write the response, preventing handlers from
// concurrently reading from the request and writing the response.
// Calling EnableFullDuplex disables this behavior and permits handlers to continue to read
// from the request while concurrently writing the response.
//
// For HTTP/2 requests, the Go HTTP server always permits concurrent reads and responses.
// If EnableFullDuplex is not supported, it returns an error matching [http.ErrNotSupported].
func (r *recorder) EnableFullDuplex() error {
	return http.NewResponseController(r.ResponseWriter).EnableFullDuplex()
}

type noUnwrap struct {
	ResponseWriter
}

type onlyWrite struct {
	io.Writer
}

type flusherWriter struct {
	ResponseWriter
}

func (w flusherWriter) Flush() { _ = w.FlushError() }

func (w flusherWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

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
		var more bool
		frame, more = frames.Next()
		if !strings.HasPrefix(frame.Function, "github.com/fox-toolkit/fox.") {
			return frame
		}
		if !more {
			break
		}
	}
	return frame
}

// ErrNotSupported returns an error that Is http.ErrNotSupported, but is not == to it.
func ErrNotSupported() error {
	return fmt.Errorf("%w", http.ErrNotSupported)
}
