package fox

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWrapFlushWriter(t *testing.T) {
	buf := bytes.NewBuffer(nil)

	f := New()
	f.MustHandle(http.MethodGet, "/foo", WrapTestContext(func(c Context) {
		flusher, ok := c.Writer().(http.Flusher)
		require.True(t, ok)

		c.TeeWriter(buf)
		flusher, ok = c.Writer().(http.Flusher)
		require.True(t, ok)

		n, err := c.Writer().Write([]byte("foo"))
		assert.NoError(t, err)
		assert.Equal(t, 3, n)
		flusher.Flush()

		n, err = c.Writer().Write([]byte("bar"))
		assert.NoError(t, err)
		assert.Equal(t, 3, n)

		assert.Equal(t, 6, c.Writer().Size())

		_, ok = c.Writer().(http.Hijacker)
		assert.False(t, ok)

		_, ok = c.Writer().(io.ReaderFrom)
		assert.False(t, ok)
	}))

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	w := httptest.NewRecorder()

	f.ServeHTTP(w, req)

	assert.Equal(t, "foobar", w.Body.String())
	assert.Equal(t, "foobar", buf.String())
	assert.True(t, w.Flushed)
}

func TestNewTestContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	w := httptest.NewRecorder()
	_, c := NewTestContext(w, req)

	buf := bytes.NewBuffer(nil)

	flusher, ok := c.Writer().(http.Flusher)
	require.True(t, ok)

	c.TeeWriter(buf)
	flusher, ok = c.Writer().(http.Flusher)
	require.True(t, ok)

	n, err := c.Writer().Write([]byte("foo"))
	assert.NoError(t, err)
	assert.Equal(t, 3, n)
	flusher.Flush()

	n, err = c.Writer().Write([]byte("bar"))
	assert.NoError(t, err)
	assert.Equal(t, 3, n)

	assert.Equal(t, 6, c.Writer().Size())

	_, ok = c.Writer().(http.Hijacker)
	assert.False(t, ok)

	_, ok = c.Writer().(io.ReaderFrom)
	assert.False(t, ok)
}
