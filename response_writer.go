// ResponseRecorder is influenced by the work done by goji and chi libraries,
// with additional optimizations to avoid unnecessary memory allocations.
// See their respective licenses for more information:
// https://github.com/zenazn/goji/blob/master/LICENSE
// https://github.com/go-chi/chi/blob/master/LICENSE

package fox

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
)

var _ http.Flusher = (*h1Writer)(nil)
var _ http.Hijacker = (*h1Writer)(nil)
var _ io.ReaderFrom = (*h1Writer)(nil)
var _ http.Pusher = (*h2Writer)(nil)
var _ http.Flusher = (*h2Writer)(nil)

// ResponseWriter extends http.ResponseWriter and provides
// methods to retrieve the recorded status code, written state, and response size.
type ResponseWriter interface {
	http.ResponseWriter
	// Status recorded after Write and WriteHeader.
	Status() int
	// Written returns true if the response has been written.
	Written() bool
	// Size returns the size of the written response.
	Size() int
	// Unwrap returns the underlying http.ResponseWriter.
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
	return r.size
}

func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *recorder) WriteHeader(code int) {
	if !r.Written() {
		r.size = 0
		r.status = code
	}
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

type hijackWriter struct {
	*recorder
}

func (w *hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	return w.recorder.ResponseWriter.(http.Hijacker).Hijack()
}

type flushHijackWriter struct {
	*recorder
}

func (w *flushHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	return w.recorder.ResponseWriter.(http.Hijacker).Hijack()
}

func (w *flushHijackWriter) Flush() {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	w.recorder.ResponseWriter.(http.Flusher).Flush()
}

type flushWriter struct {
	*recorder
}

func (w *flushWriter) Flush() {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	w.recorder.ResponseWriter.(http.Flusher).Flush()
}

type h1Writer struct {
	*recorder
}

func (w *h1Writer) ReadFrom(r io.Reader) (n int64, err error) {
	rf := w.recorder.ResponseWriter.(io.ReaderFrom)
	// If not written, status is OK
	w.recorder.WriteHeader(w.recorder.status)
	n, err = rf.ReadFrom(r)
	w.recorder.size += int(n)
	return
}

func (w *h1Writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	return w.recorder.ResponseWriter.(http.Hijacker).Hijack()
}

func (w *h1Writer) Flush() {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	w.recorder.ResponseWriter.(http.Flusher).Flush()
}

type h2Writer struct {
	*recorder
}

func (w *h2Writer) Push(target string, opts *http.PushOptions) error {
	return w.recorder.ResponseWriter.(http.Pusher).Push(target, opts)
}

func (w *h2Writer) Flush() {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	w.recorder.ResponseWriter.(http.Flusher).Flush()
}

type noopWriter struct{}

func (n noopWriter) Header() http.Header {
	return make(http.Header)
}

func (n noopWriter) Write(bytes []byte) (int, error) {
	return 0, fmt.Errorf("%w: writing on a clone", ErrDiscardedResponseWriter)
}

func (n noopWriter) WriteHeader(statusCode int) {}
