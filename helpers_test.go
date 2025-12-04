// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	w := httptest.NewRecorder()
	f, c := NewTestContext(w, req)

	assert.NotNil(t, f)
	flusher, ok := c.Writer().(interface{ Unwrap() http.ResponseWriter }).Unwrap().(http.Flusher)
	require.True(t, ok)

	n, err := c.Writer().Write([]byte("foo"))
	assert.NoError(t, err)
	assert.Equal(t, 3, n)
	flusher.Flush()

	n, err = c.Writer().Write([]byte("bar"))
	assert.NoError(t, err)
	assert.Equal(t, 3, n)

	assert.Equal(t, 6, c.Writer().Size())

	_, _, err = c.Writer().Hijack()
	assert.ErrorIs(t, err, http.ErrNotSupported)

	err = c.Writer().Push("foo", nil)
	assert.ErrorIs(t, err, http.ErrNotSupported)

	err = c.Writer().SetReadDeadline(time.Time{})
	assert.ErrorIs(t, err, http.ErrNotSupported)

	err = c.Writer().SetWriteDeadline(time.Time{})
	assert.ErrorIs(t, err, http.ErrNotSupported)

	err = c.Writer().EnableFullDuplex()
	assert.ErrorIs(t, err, http.ErrNotSupported)
}
