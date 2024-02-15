// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

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

var (
	_ http.ResponseWriter = (*h1Writer)(nil)
	_ http.Flusher        = (*h1Writer)(nil)
	_ http.Hijacker       = (*h1Writer)(nil)
	_ io.ReaderFrom       = (*h1Writer)(nil)
	_ io.StringWriter     = (*h1Writer)(nil)
)

var (
	_ http.ResponseWriter = (*extendedH1writer)(nil)
	_ http.Flusher        = (*extendedH1writer)(nil)
	_ http.Hijacker       = (*extendedH1writer)(nil)
	_ io.ReaderFrom       = (*extendedH1writer)(nil)
	_ io.StringWriter     = (*extendedH1writer)(nil)
)

var (
	_ http.ResponseWriter = (*h2Writer)(nil)
	_ http.Pusher         = (*h2Writer)(nil)
	_ http.Flusher        = (*h2Writer)(nil)
	_ io.ReaderFrom       = (*h2Writer)(nil)
	_ io.StringWriter     = (*h2Writer)(nil)
)

var (
	_ http.ResponseWriter = (*extendedH2writer)(nil)
	_ http.Pusher         = (*extendedH2writer)(nil)
	_ http.Flusher        = (*extendedH2writer)(nil)
	_ io.ReaderFrom       = (*extendedH2writer)(nil)
	_ io.StringWriter     = (*extendedH2writer)(nil)
)

var (
	_ http.ResponseWriter = (*flushWriter)(nil)
	_ http.Flusher        = (*flushWriter)(nil)
	_ io.ReaderFrom       = (*flushWriter)(nil)
	_ io.StringWriter     = (*flushWriter)(nil)
)

var (
	_ http.ResponseWriter = (*pushWriter)(nil)
	_ http.Pusher         = (*pushWriter)(nil)
	_ io.ReaderFrom       = (*pushWriter)(nil)
	_ io.StringWriter     = (*pushWriter)(nil)
)

// ResponseWriter extends http.ResponseWriter and provides methods to retrieve the recorded status code,
// written state, and response size. Unlike traditional implementations that might directly implement additional
// interfaces such as http.Flusher or http.Hijacker, implementers are encouraged to use the Unwrap() method. Unwrap()
// returns an http.ResponseWriter that potentially supports additional interfaces based on the capabilities of the underlying
// response writer provided to the ServeHTTP function or a custom writer implemented by a middleware in the chain.
// Implementations of ResponseWriter should ensure that Unwrap() methodically identifies and exposes the appropriate
// capabilities (e.g., flushing, hijacking, HTTP/2 pushing) of the underlying writer in a type-safe manner, without
// requiring the ResponseWriter itself to directly implement these interfaces.
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
}

var copyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

const notWritten = -1

type recorder struct {
	http.ResponseWriter
	size   int
	status int
}

func (r *recorder) reset(w http.ResponseWriter) {
	r.ResponseWriter = w
	r.size = notWritten
	r.status = http.StatusOK
}

// Status recorded after Write and WriteHeader.
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

// Unwrap returns a compliant http.ResponseWriter, which safely supports additional interfaces such as http.Flusher
// or http.Hijacker. The exact scope of supported interfaces is determined by the capabilities of the http.ResponseWriter
// provided to the ServeHTTP function or a custom writer implemented by a middleware in the chain.
func (r *recorder) Unwrap() http.ResponseWriter {
	switch r.ResponseWriter.(type) {
	case interface {
		http.Flusher
		http.Hijacker
	}:
		// h1 extended
		if _, ok := r.ResponseWriter.(interface {
			SetReadDeadline(deadline time.Time) error
			SetWriteDeadline(deadline time.Time) error
			EnableFullDuplex() error
		}); ok {
			return extendedH1writer{r}
		}

		return h1Writer{r}
	case interface {
		http.Flusher
		http.Pusher
	}:
		// h2 extended
		if _, ok := r.ResponseWriter.(interface {
			SetReadDeadline(deadline time.Time) error
			SetWriteDeadline(deadline time.Time) error
		}); ok {
			return extendedH2writer{r}
		}

		return h2Writer{r}
	case http.Pusher:
		return pushWriter{r}
	case http.Flusher:
		return flushWriter{r}
	}
	return r
}

// WriteHeader sends an HTTP response header with the provided
// status code. See http.ResponseWriter for more details.
func (r *recorder) WriteHeader(code int) {
	if r.Written() {
		caller := relevantCaller()
		log.Printf("http: superfluous response.WriteHeader call from %s (%s:%d)", caller.Function, path.Base(caller.File), caller.Line)
		return
	}

	r.size = 0
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Write writes the data to the connection as part of an HTTP reply.
// See http.ResponseWriter for more details.
func (r *recorder) Write(buf []byte) (n int, err error) {
	if !r.Written() {
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
	if !r.Written() {
		r.size = 0
		r.ResponseWriter.WriteHeader(r.status)
	}

	n, err = io.WriteString(r.ResponseWriter, s)
	r.size += n
	return
}

func (r *recorder) ReadFrom(src io.Reader) (n int64, err error) {
	if !r.Written() {
		r.size = 0
	}

	if rf, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(src)
		r.size += int(n)
		return
	}

	bufp := copyBufPool.Get().(*[]byte)
	buf := *bufp
	n, err = io.CopyBuffer(onlyWrite{r}, src, buf)
	copyBufPool.Put(bufp)
	return
}

type flushWriter struct {
	r *recorder
}

func (w flushWriter) Header() http.Header {
	return w.r.Header()
}

func (w flushWriter) Write(buf []byte) (int, error) {
	return w.r.Write(buf)
}

func (w flushWriter) WriteHeader(statusCode int) {
	w.r.WriteHeader(statusCode)
}

func (w flushWriter) WriteString(s string) (int, error) {
	return w.r.WriteString(s)
}

func (w flushWriter) ReadFrom(src io.Reader) (n int64, err error) { return w.r.ReadFrom(src) }

func (w flushWriter) Flush() {
	if !w.r.Written() {
		w.r.size = 0
	}
	w.r.ResponseWriter.(http.Flusher).Flush()
}

type h1Writer struct {
	r *recorder
}

func (w h1Writer) Header() http.Header {
	return w.r.Header()
}

func (w h1Writer) Write(buf []byte) (int, error) {
	return w.r.Write(buf)
}

func (w h1Writer) WriteHeader(statusCode int) { w.r.WriteHeader(statusCode) }

func (w h1Writer) WriteString(s string) (int, error) {
	return w.r.WriteString(s)
}

func (w h1Writer) ReadFrom(src io.Reader) (n int64, err error) { return w.r.ReadFrom(src) }

func (w h1Writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.r.Written() {
		w.r.size = 0
	}
	return w.r.ResponseWriter.(http.Hijacker).Hijack()
}

func (w h1Writer) Flush() {
	if !w.r.Written() {
		w.r.size = 0
	}
	w.r.ResponseWriter.(http.Flusher).Flush()
}

type extendedH1writer struct {
	r *recorder
}

func (w extendedH1writer) Header() http.Header {
	return w.r.Header()
}

func (w extendedH1writer) Write(buf []byte) (int, error) {
	return w.r.Write(buf)
}

func (w extendedH1writer) WriteHeader(statusCode int) { w.r.WriteHeader(statusCode) }

func (w extendedH1writer) WriteString(s string) (int, error) {
	return w.r.WriteString(s)
}

func (w extendedH1writer) ReadFrom(src io.Reader) (n int64, err error) { return w.r.ReadFrom(src) }

func (w extendedH1writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.r.Written() {
		w.r.size = 0
	}
	return w.r.ResponseWriter.(http.Hijacker).Hijack()
}

func (w extendedH1writer) Flush() {
	if !w.r.Written() {
		w.r.size = 0
	}
	w.r.ResponseWriter.(http.Flusher).Flush()
}

func (w extendedH1writer) SetReadDeadline(deadline time.Time) error {
	return w.r.ResponseWriter.(interface{ SetReadDeadline(time.Time) error }).SetReadDeadline(deadline)
}

func (w extendedH1writer) SetWriteDeadline(deadline time.Time) error {
	return w.r.ResponseWriter.(interface{ SetWriteDeadline(time.Time) error }).SetWriteDeadline(deadline)
}

func (w extendedH1writer) EnableFullDuplex() error {
	return w.r.ResponseWriter.(interface{ EnableFullDuplex() error }).EnableFullDuplex()
}

type h2Writer struct {
	r *recorder
}

func (w h2Writer) Header() http.Header {
	return w.r.Header()
}

func (w h2Writer) Write(buf []byte) (int, error) {
	return w.r.Write(buf)
}

func (w h2Writer) WriteHeader(statusCode int) {
	w.r.WriteHeader(statusCode)
}

func (w h2Writer) WriteString(s string) (int, error) {
	return w.r.WriteString(s)
}

func (w h2Writer) ReadFrom(src io.Reader) (n int64, err error) { return w.r.ReadFrom(src) }

func (w h2Writer) Push(target string, opts *http.PushOptions) error {
	return w.r.ResponseWriter.(http.Pusher).Push(target, opts)
}

func (w h2Writer) Flush() {
	if !w.r.Written() {
		w.r.size = 0
	}
	w.r.ResponseWriter.(http.Flusher).Flush()
}

type extendedH2writer struct {
	r *recorder
}

func (w extendedH2writer) Header() http.Header {
	return w.r.Header()
}

func (w extendedH2writer) Write(buf []byte) (int, error) {
	return w.r.Write(buf)
}

func (w extendedH2writer) WriteHeader(statusCode int) { w.r.WriteHeader(statusCode) }

func (w extendedH2writer) WriteString(s string) (int, error) {
	return w.r.WriteString(s)
}

func (w extendedH2writer) ReadFrom(src io.Reader) (n int64, err error) { return w.r.ReadFrom(src) }

func (w extendedH2writer) Push(target string, opts *http.PushOptions) error {
	return w.r.ResponseWriter.(http.Pusher).Push(target, opts)
}

func (w extendedH2writer) Flush() {
	if !w.r.Written() {
		w.r.size = 0
	}
	w.r.ResponseWriter.(http.Flusher).Flush()
}

func (w extendedH2writer) SetReadDeadline(deadline time.Time) error {
	return w.r.ResponseWriter.(interface{ SetReadDeadline(time.Time) error }).SetReadDeadline(deadline)
}

func (w extendedH2writer) SetWriteDeadline(deadline time.Time) error {
	return w.r.ResponseWriter.(interface{ SetWriteDeadline(time.Time) error }).SetWriteDeadline(deadline)
}

type pushWriter struct {
	r *recorder
}

func (w pushWriter) Header() http.Header {
	return w.r.Header()
}

func (w pushWriter) Write(buf []byte) (int, error) {
	return w.r.Write(buf)
}

func (w pushWriter) WriteHeader(statusCode int) {
	w.r.WriteHeader(statusCode)
}

func (w pushWriter) WriteString(s string) (int, error) {
	return w.r.WriteString(s)
}

func (w pushWriter) ReadFrom(src io.Reader) (n int64, err error) { return w.r.ReadFrom(src) }

func (w pushWriter) Push(target string, opts *http.PushOptions) error {
	return w.r.ResponseWriter.(http.Pusher).Push(target, opts)
}

// noUnwrap hide the Unwrap method of the ResponseWriter.
type noUnwrap struct {
	ResponseWriter
}

type onlyWrite struct{ io.Writer }

type noopWriter struct {
	h http.Header
}

func (w noopWriter) Header() http.Header {
	return w.h
}

func (w noopWriter) Write([]byte) (int, error) {
	panic(fmt.Errorf("%w: attempt to write on a clone", ErrDiscardedResponseWriter))
}

func (w noopWriter) WriteHeader(int) {
	panic(fmt.Errorf("%w: attempt to write on a clone", ErrDiscardedResponseWriter))
}

func (w noopWriter) ReadFrom(src io.Reader) (n int64, err error) {
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
