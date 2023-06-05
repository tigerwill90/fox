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
	"sync"
)

var (
	_ http.Flusher  = (*h1Writer)(nil)
	_ http.Hijacker = (*h1Writer)(nil)
	_ io.ReaderFrom = (*h1Writer)(nil)
)

var (
	_ ResponseWriter  = (*h1MultiWriter)(nil)
	_ io.StringWriter = (*h1MultiWriter)(nil)
	_ http.Flusher    = (*h1MultiWriter)(nil)
	_ http.Hijacker   = (*h1MultiWriter)(nil)
	_ io.ReaderFrom   = (*h1MultiWriter)(nil)
)

var (
	_ http.Pusher  = (*h2Writer)(nil)
	_ http.Flusher = (*h2Writer)(nil)
)

var (
	_ ResponseWriter  = (*h2MultiWriter)(nil)
	_ io.StringWriter = (*h2MultiWriter)(nil)
	_ http.Flusher    = (*h2MultiWriter)(nil)
	_ http.Pusher     = (*h2MultiWriter)(nil)
)

var copyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

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

func (r *recorder) WriteString(s string) (n int, err error) {
	if !r.Written() {
		r.size = 0
		r.ResponseWriter.WriteHeader(r.status)
	}

	n, err = io.WriteString(r.ResponseWriter, s)
	r.size += n
	return
}

//nolint:unused
type hijackWriter struct {
	*recorder
}

//nolint:unused
func (w hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	return w.recorder.ResponseWriter.(http.Hijacker).Hijack()
}

//nolint:unused
type flushHijackWriter struct {
	*recorder
}

//nolint:unused
func (w flushHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	return w.recorder.ResponseWriter.(http.Hijacker).Hijack()
}

//nolint:unused
func (w flushHijackWriter) Flush() {
	if !w.recorder.Written() {
		w.recorder.size = 0
	}
	w.recorder.ResponseWriter.(http.Flusher).Flush()
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

type noopWriter struct{}

func (n noopWriter) Header() http.Header {
	return make(http.Header)
}

func (n noopWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("%w: writing on a clone", ErrDiscardedResponseWriter)
}

func (n noopWriter) WriteHeader(int) {}

type h1MultiWriter struct {
	writers *[]io.Writer
}

func (w h1MultiWriter) Header() http.Header {
	return (*w.writers)[0].(ResponseWriter).Header()
}

func (w h1MultiWriter) WriteHeader(statusCode int) {
	(*w.writers)[0].(ResponseWriter).WriteHeader(statusCode)
}

func (w h1MultiWriter) Status() int {
	return (*w.writers)[0].(ResponseWriter).Status()
}

func (w h1MultiWriter) Written() bool {
	return (*w.writers)[0].(ResponseWriter).Written()
}

func (w h1MultiWriter) Size() int {
	return (*w.writers)[0].(ResponseWriter).Size()
}

func (w h1MultiWriter) Unwrap() http.ResponseWriter {
	return (*w.writers)[0].(ResponseWriter).Unwrap()
}

func (w h1MultiWriter) Write(p []byte) (n int, err error) {
	for _, writer := range *w.writers {
		n, err = writer.Write(p)
		if err != nil {
			return
		}
		if n != len(p) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(p), nil
}

func (w h1MultiWriter) WriteString(s string) (n int, err error) {
	var p []byte // lazily initialized if/when needed
	for _, writer := range *w.writers {
		if sw, ok := writer.(io.StringWriter); ok {
			n, err = sw.WriteString(s)
		} else {
			if p == nil {
				p = []byte(s)
			}
			n, err = writer.Write(p)
		}
		if err != nil {
			return
		}
		if n != len(s) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(s), nil
}

func (w h1MultiWriter) Flush() {
	for _, writer := range *w.writers {
		if f, ok := writer.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func (w h1MultiWriter) ReadFrom(src io.Reader) (n int64, err error) {
	bufp := copyBufPool.Get().(*[]byte)
	buf := *bufp
	n, err = io.CopyBuffer(w, src, buf)
	copyBufPool.Put(bufp)
	return
}

func (w h1MultiWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return (*w.writers)[0].(http.Hijacker).Hijack()
}

type h2MultiWriter struct {
	writers *[]io.Writer
}

func (w h2MultiWriter) Header() http.Header {
	return (*w.writers)[0].(ResponseWriter).Header()
}

func (w h2MultiWriter) WriteHeader(statusCode int) {
	(*w.writers)[0].(ResponseWriter).WriteHeader(statusCode)
}

func (w h2MultiWriter) Status() int {
	return (*w.writers)[0].(ResponseWriter).Status()
}

func (w h2MultiWriter) Written() bool {
	return (*w.writers)[0].(ResponseWriter).Written()
}

func (w h2MultiWriter) Size() int {
	return (*w.writers)[0].(ResponseWriter).Size()
}

func (w h2MultiWriter) Unwrap() http.ResponseWriter {
	return (*w.writers)[0].(ResponseWriter).Unwrap()
}

func (w h2MultiWriter) Write(p []byte) (n int, err error) {
	for _, writer := range *w.writers {
		n, err = writer.Write(p)
		if err != nil {
			return
		}
		if n != len(p) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(p), nil
}

func (w h2MultiWriter) WriteString(s string) (n int, err error) {
	var p []byte // lazily initialized if/when needed
	for _, writer := range *w.writers {
		if sw, ok := writer.(io.StringWriter); ok {
			n, err = sw.WriteString(s)
		} else {
			if p == nil {
				p = []byte(s)
			}
			n, err = writer.Write(p)
		}
		if err != nil {
			return
		}
		if n != len(s) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(s), nil
}

func (w h2MultiWriter) Flush() {
	for _, writer := range *w.writers {
		if f, ok := writer.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func (w h2MultiWriter) Push(target string, opts *http.PushOptions) error {
	return (*w.writers)[0].(http.Pusher).Push(target, opts)
}

type flushMultiWriter struct {
	writers *[]io.Writer
}

func (w flushMultiWriter) Header() http.Header {
	return (*w.writers)[0].(ResponseWriter).Header()
}

func (w flushMultiWriter) WriteHeader(statusCode int) {
	(*w.writers)[0].(ResponseWriter).WriteHeader(statusCode)
}

func (w flushMultiWriter) Status() int {
	return (*w.writers)[0].(ResponseWriter).Status()
}

func (w flushMultiWriter) Written() bool {
	return (*w.writers)[0].(ResponseWriter).Written()
}

func (w flushMultiWriter) Size() int {
	return (*w.writers)[0].(ResponseWriter).Size()
}

func (w flushMultiWriter) Unwrap() http.ResponseWriter {
	return (*w.writers)[0].(ResponseWriter).Unwrap()
}

func (w flushMultiWriter) Write(p []byte) (n int, err error) {
	for _, writer := range *w.writers {
		n, err = writer.Write(p)
		if err != nil {
			return
		}
		if n != len(p) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(p), nil
}

func (w flushMultiWriter) WriteString(s string) (n int, err error) {
	var p []byte // lazily initialized if/when needed
	for _, writer := range *w.writers {
		if sw, ok := writer.(io.StringWriter); ok {
			n, err = sw.WriteString(s)
		} else {
			if p == nil {
				p = []byte(s)
			}
			n, err = writer.Write(p)
		}
		if err != nil {
			return
		}
		if n != len(s) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(s), nil
}
func (w flushMultiWriter) Flush() {
	for _, writer := range *w.writers {
		if f, ok := writer.(http.Flusher); ok {
			f.Flush()
		}
	}
}
