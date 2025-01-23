// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"bufio"
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type flushErrorWriterFunc func() error

func (f flushErrorWriterFunc) FlushError() error {
	return f()
}

type flushWriterFunc func()

func (f flushWriterFunc) Flush() {
	f()
}

type hijackWriterFunc func() (net.Conn, *bufio.ReadWriter, error)

func (f hijackWriterFunc) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return f()
}

type pushWriterFunc func(target string, opts *http.PushOptions) error

func (f pushWriterFunc) Push(target string, opts *http.PushOptions) error {
	return f(target, opts)
}

type deadlineWriterFunc func(deadline time.Time) error

func (f deadlineWriterFunc) SetReadDeadline(deadline time.Time) error {
	return f(deadline)
}

func (f deadlineWriterFunc) SetWriteDeadline(deadline time.Time) error {
	return f(deadline)
}

type duplexWriterFunc func() error

func (f duplexWriterFunc) EnableFullDuplex() error { return f() }

func TestRecorder_FlushError(t *testing.T) {
	type flushError interface {
		FlushError() error
	}

	cases := []struct {
		name   string
		rec    *recorder
		assert func(t *testing.T, w ResponseWriter)
	}{
		{
			name: "implement FlushError and flush returns error",
			rec: &recorder{
				ResponseWriter: struct {
					http.ResponseWriter
					flushError
				}{
					ResponseWriter: httptest.NewRecorder(),
					flushError: flushErrorWriterFunc(func() error {
						return errors.New("error")
					}),
				},
			},
			assert: func(t *testing.T, w ResponseWriter) {
				assert.Error(t, w.FlushError())
			},
		},
		{
			name: "implement Flusher and flush return nil",
			rec: &recorder{
				ResponseWriter: struct {
					http.ResponseWriter
					http.Flusher
				}{
					ResponseWriter: httptest.NewRecorder(),
					Flusher:        flushWriterFunc(func() {}),
				},
			},
			assert: func(t *testing.T, w ResponseWriter) {
				assert.Nil(t, w.FlushError())
			},
		},
		{
			name: "does not implement flusher and return http.ErrNotSupported",
			rec: &recorder{
				ResponseWriter: struct {
					http.ResponseWriter
				}{
					ResponseWriter: httptest.NewRecorder(),
				},
			},
			assert: func(t *testing.T, w ResponseWriter) {
				assert.ErrorIs(t, w.FlushError(), http.ErrNotSupported)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.rec)
		})
	}
}

func TestRecorder_Hijack(t *testing.T) {
	cases := []struct {
		name   string
		rec    *recorder
		assert func(t *testing.T, w ResponseWriter)
	}{
		{
			name: "implements Hijacker and hijack returns no error",
			rec: &recorder{
				ResponseWriter: struct {
					http.ResponseWriter
					http.Hijacker
				}{
					ResponseWriter: httptest.NewRecorder(),
					Hijacker: hijackWriterFunc(func() (net.Conn, *bufio.ReadWriter, error) {
						return nil, nil, nil
					}),
				},
			},
			assert: func(t *testing.T, w ResponseWriter) {
				_, _, err := w.Hijack()
				assert.NoError(t, err)
			},
		},
		{
			name: "does not implement Hijacker and return http.ErrNotSupported",
			rec: &recorder{
				ResponseWriter: httptest.NewRecorder(),
			},
			assert: func(t *testing.T, w ResponseWriter) {
				_, _, err := w.Hijack()
				assert.ErrorIs(t, err, http.ErrNotSupported)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.rec)
		})
	}
}

func TestRecorder_Push(t *testing.T) {
	cases := []struct {
		name   string
		rec    *recorder
		assert func(t *testing.T, w ResponseWriter)
	}{
		{
			name: "implements Pusher and push returns no error",
			rec: &recorder{
				ResponseWriter: struct {
					http.ResponseWriter
					http.Pusher
				}{
					ResponseWriter: httptest.NewRecorder(),
					Pusher: pushWriterFunc(func(target string, opts *http.PushOptions) error {
						return nil
					}),
				},
			},
			assert: func(t *testing.T, w ResponseWriter) {
				assert.NoError(t, w.Push("/path", nil))
			},
		},
		{
			name: "does not implement Pusher and return http.ErrNotSupported",
			rec: &recorder{
				ResponseWriter: httptest.NewRecorder(),
			},
			assert: func(t *testing.T, w ResponseWriter) {
				err := w.Push("/path", nil)
				assert.ErrorIs(t, err, http.ErrNotSupported)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.rec)
		})
	}
}

func TestRecorder_SetReadDeadline(t *testing.T) {
	type deadlineWriter interface {
		SetReadDeadline(time.Time) error
	}

	cases := []struct {
		name   string
		rec    *recorder
		assert func(t *testing.T, w ResponseWriter)
	}{
		{
			name: "implements SetReadDeadline and returns no error",
			rec: &recorder{
				ResponseWriter: struct {
					http.ResponseWriter
					deadlineWriter
				}{
					ResponseWriter: httptest.NewRecorder(),
					deadlineWriter: deadlineWriterFunc(func(deadline time.Time) error {
						return nil
					}),
				},
			},
			assert: func(t *testing.T, w ResponseWriter) {
				assert.NoError(t, w.SetReadDeadline(time.Now()))
			},
		},
		{
			name: "does not implement SetReadDeadline and returns http.ErrNotSupported",
			rec: &recorder{
				ResponseWriter: httptest.NewRecorder(),
			},
			assert: func(t *testing.T, w ResponseWriter) {
				err := w.SetReadDeadline(time.Now())
				assert.ErrorIs(t, err, http.ErrNotSupported)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.rec)
		})
	}
}

func TestRecorder_SetWriteDeadline(t *testing.T) {
	type deadlineWriter interface {
		SetWriteDeadline(time.Time) error
	}

	cases := []struct {
		name   string
		rec    *recorder
		assert func(t *testing.T, w ResponseWriter)
	}{
		{
			name: "implements SetWriteDeadline and returns no error",
			rec: &recorder{
				ResponseWriter: struct {
					http.ResponseWriter
					deadlineWriter
				}{
					ResponseWriter: httptest.NewRecorder(),
					deadlineWriter: deadlineWriterFunc(func(deadline time.Time) error {
						return nil
					}),
				},
			},
			assert: func(t *testing.T, w ResponseWriter) {
				assert.NoError(t, w.SetWriteDeadline(time.Now()))
			},
		},
		{
			name: "does not implement SetWriteDeadline and returns http.ErrNotSupported",
			rec: &recorder{
				ResponseWriter: httptest.NewRecorder(),
			},
			assert: func(t *testing.T, w ResponseWriter) {
				err := w.SetWriteDeadline(time.Now())
				assert.ErrorIs(t, err, http.ErrNotSupported)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.rec)
		})
	}
}

func TestRecorder_EnableFullDuplex(t *testing.T) {
	type duplexWriter interface {
		EnableFullDuplex() error
	}

	cases := []struct {
		name   string
		rec    *recorder
		assert func(t *testing.T, w ResponseWriter)
	}{
		{
			name: "implements EnableFullDuplex and returns no error",
			rec: &recorder{
				ResponseWriter: struct {
					http.ResponseWriter
					duplexWriter
				}{
					ResponseWriter: httptest.NewRecorder(),
					duplexWriter: duplexWriterFunc(func() error {
						return nil
					}),
				},
			},
			assert: func(t *testing.T, w ResponseWriter) {
				assert.NoError(t, w.EnableFullDuplex())
			},
		},
		{
			name: "does not implement EnableFullDuplex and returns http.ErrNotSupported",
			rec: &recorder{
				ResponseWriter: httptest.NewRecorder(),
			},
			assert: func(t *testing.T, w ResponseWriter) {
				err := w.EnableFullDuplex()
				assert.ErrorIs(t, err, http.ErrNotSupported)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.rec)
		})
	}
}

func TestRecorderSuperfluousWriteHeader(t *testing.T) {
	rec := new(recorder)
	w := httptest.NewRecorder()
	rec.reset(w)
	rec.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, w.Code)
	rec.WriteHeader(http.StatusAccepted)
	assert.Equal(t, http.StatusCreated, w.Code)

	rec = new(recorder)
	w = httptest.NewRecorder()
	rec.reset(w)
	_, err := rec.Write([]byte("foo"))
	require.NoError(t, err)
	rec.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusOK, w.Code)

	rec = new(recorder)
	w = httptest.NewRecorder()
	rec.reset(w)
	_, err = rec.WriteString("foo")
	require.NoError(t, err)
	rec.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusOK, w.Code)

	rec = new(recorder)
	w = httptest.NewRecorder()
	rec.reset(w)
	err = rec.FlushError()
	require.NoError(t, err)
	rec.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecorderWriteAfterHijack(t *testing.T) {
	f, _ := New()
	f.MustHandle(http.MethodGet, "/foo", func(c Context) {
		conn, _, err := c.Writer().Hijack()
		require.NoError(t, err)
		defer conn.Close()
		c.Writer().WriteHeader(http.StatusAccepted)
		assert.Equal(t, http.StatusOK, c.Writer().Status())
		_, err = c.Writer().Write([]byte("foo"))
		assert.ErrorIs(t, err, http.ErrHijacked)
		_, err = c.Writer().WriteString("foo")
		assert.ErrorIs(t, err, http.ErrHijacked)
		_, err = c.Writer().ReadFrom(strings.NewReader("foo"))
		assert.ErrorIs(t, err, http.ErrHijacked)
	})

	srv := httptest.NewServer(f)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/foo", nil)
	require.NoError(t, err)
	client := srv.Client()
	_, err = client.Do(req)
	require.Error(t, err)
}

func TestInformationalHeader(t *testing.T) {
	f, _ := New()
	f.MustHandle(http.MethodGet, "/foo", func(c Context) {
		c.SetHeader("Link", "</style.css>; rel=preload; as=style")
		c.Writer().WriteHeader(http.StatusEarlyHints)
		_, err := c.Writer().WriteString("final response")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, c.Writer().Status())
	})

	srv := httptest.NewServer(f)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/foo", nil)
	require.NoError(t, err)
	client := srv.Client()
	resp, err := client.Do(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	linkHeader := resp.Header.Get("Link")
	expectedLink := "</style.css>; rel=preload; as=style"
	assert.Equal(t, expectedLink, linkHeader)
}
