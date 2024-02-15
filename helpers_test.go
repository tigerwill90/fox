// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	w := httptest.NewRecorder()
	_, c := NewTestContext(w, req)

	require.Implements(t, (*interface{ Unwrap() http.ResponseWriter })(nil), c.Writer())
	rw := c.Writer().(interface{ Unwrap() http.ResponseWriter }).Unwrap()
	assert.IsType(t, flushWriter{}, rw)

	flusher, ok := rw.(http.Flusher)
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

	_, ok = rw.(http.Hijacker)
	assert.False(t, ok)

	_, ok = c.Writer().(io.ReaderFrom)
	assert.False(t, ok)

	_, ok = rw.(io.ReaderFrom)
	assert.False(t, ok)
}
