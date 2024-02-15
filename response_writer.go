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
)

var (
	_ http.ResponseWriter = (*h1Writer)(nil)
	_ http.Flusher        = (*h1Writer)(nil)
	_ http.Hijacker       = (*h1Writer)(nil)
	_ io.ReaderFrom       = (*h1Writer)(nil)
)

var (
	_ http.ResponseWriter = (*h2Writer)(nil)
	_ http.Pusher         = (*h2Writer)(nil)
	_ http.Flusher        = (*h2Writer)(nil)
)

var (
	_ http.ResponseWriter = (*flushWriter)(nil)
	_ http.Flusher        = (*flushWriter)(nil)
)

var (
	_ http.ResponseWriter = (*pushWriter)(nil)
	_ http.Pusher         = (*pushWriter)(nil)
)

// ResponseWriter extends http.ResponseWriter and provides methods to retrieve the recorded status code,
// written state, and response size. ResponseWriter object implements additional http.Flusher, http.Hijacker,
// io.ReaderFrom interfaces for HTTP/1.x requests and http.Flusher, http.Pusher interfaces for HTTP/2 requests.
type ResponseWriter interface {
	http.ResponseWriter
	// Status recorded after Write and WriteHeader.
	Status() int
	// Written returns true if the response has been written.
	Written() bool
	// Size returns the size of the written response.
	Size() int
	// WriteString writes the provided string to the underlying connection
	// as part of an HTTP reply. The method returns the number of bytes written
	// and an error, if any.
	WriteString(s string) (int, error)
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
// or http.Hijacker. The exact scope of supported interfaces is determined by the capabilities
// of the http.ResponseWriter provided to the ServeHTTP function,
func (r *recorder) Unwrap() http.ResponseWriter {
	switch r.ResponseWriter.(type) {
	case interface {
		http.Flusher
		http.Hijacker
		io.ReaderFrom
	}:
		return h1Writer{r}
	case interface {
		http.Flusher
		http.Pusher
	}:
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

func (w h1Writer) WriteHeader(statusCode int) {
	w.r.WriteHeader(statusCode)
}

func (w h1Writer) WriteString(s string) (int, error) {
	return w.r.WriteString(s)
}

func (w h1Writer) ReadFrom(src io.Reader) (n int64, err error) {
	if !w.r.Written() {
		w.r.size = 0
	}

	rf := w.r.ResponseWriter.(io.ReaderFrom)
	n, err = rf.ReadFrom(src)
	w.r.size += int(n)
	return
}

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

func (w h2Writer) Push(target string, opts *http.PushOptions) error {
	return w.r.ResponseWriter.(http.Pusher).Push(target, opts)
}

func (w h2Writer) Flush() {
	if !w.r.Written() {
		w.r.size = 0
	}
	w.r.ResponseWriter.(http.Flusher).Flush()
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

func (w pushWriter) Push(target string, opts *http.PushOptions) error {
	return w.r.ResponseWriter.(http.Pusher).Push(target, opts)
}

// noUnwrap hide the Unwrap method of the ResponseWriter.
type noUnwrap struct {
	ResponseWriter
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
