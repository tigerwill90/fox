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
	_ http.Flusher  = (*h1Writer)(nil)
	_ http.Hijacker = (*h1Writer)(nil)
	_ io.ReaderFrom = (*h1Writer)(nil)
)

var (
	_ http.Pusher  = (*h2Writer)(nil)
	_ http.Flusher = (*h2Writer)(nil)
)

var (
	_ ResponseWriter = (*flushWriter)(nil)
	_ http.Flusher   = (*flushWriter)(nil)
)

var (
	_ ResponseWriter = (*pushWriter)(nil)
	_ http.Pusher    = (*pushWriter)(nil)
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

type rwUnwrapper interface {
	Unwrap() http.ResponseWriter
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

func (r *recorder) Status() int {
	return r.status
}

func (r *recorder) Written() bool {
	return r.size != notWritten
}

func (r *recorder) Size() int {
	if r.size < 0 {
		return 0
	}
	return r.size
}

func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

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

func (r *recorder) Write(buf []byte) (n int, err error) {
	if !r.Written() {
		r.size = 0
		r.ResponseWriter.WriteHeader(r.status)
	}
	n, err = r.ResponseWriter.Write(buf)
	r.size += n
	return
}

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
	*recorder
}

func (w flushWriter) Flush() {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	w.recorder.ResponseWriter.(http.Flusher).Flush()
}

type h1Writer struct {
	*recorder
}

func (w h1Writer) ReadFrom(src io.Reader) (n int64, err error) {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}

	rf := w.recorder.ResponseWriter.(io.ReaderFrom)
	n, err = rf.ReadFrom(src)
	w.recorder.size += int(n)
	return
}

func (w h1Writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	return w.recorder.ResponseWriter.(http.Hijacker).Hijack()
}

func (w h1Writer) Flush() {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	w.recorder.ResponseWriter.(http.Flusher).Flush()
}

type h2Writer struct {
	*recorder
}

func (w h2Writer) Push(target string, opts *http.PushOptions) error {
	return w.recorder.ResponseWriter.(http.Pusher).Push(target, opts)
}

func (w h2Writer) Flush() {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	w.recorder.ResponseWriter.(http.Flusher).Flush()
}

type pushWriter struct {
	*recorder
}

func (w pushWriter) Push(target string, opts *http.PushOptions) error {
	return w.recorder.ResponseWriter.(http.Pusher).Push(target, opts)
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
