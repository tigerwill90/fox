package fox

import (
	"bytes"
	netcontext "context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestContext_QueryParams(t *testing.T) {
	wantValues := url.Values{
		"a": []string{"b"},
		"c": []string{"d", "e"},
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	req.URL.RawQuery = wantValues.Encode()

	c := newTestContextTree(New().Tree())
	c.req = req
	values := c.QueryParams()
	assert.Equal(t, wantValues, values)
	assert.Equal(t, wantValues, c.cachedQuery)
}

func TestContext_QueryParam(t *testing.T) {
	wantValues := url.Values{
		"a": []string{"b"},
		"c": []string{"d", "e"},
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	req.URL.RawQuery = wantValues.Encode()

	c := newTestContextTree(New().Tree())
	c.req = req
	assert.Equal(t, "b", c.QueryParam("a"))
	assert.Equal(t, "d", c.QueryParam("c"))
	assert.Equal(t, wantValues, c.cachedQuery)
}

func TestContext_Clone(t *testing.T) {
	wantValues := url.Values{
		"a": []string{"b"},
		"c": []string{"d", "e"},
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	req.URL.RawQuery = wantValues.Encode()

	c := newTextContextOnly(New(), httptest.NewRecorder(), req)

	buf := []byte("foo bar")
	_, err := c.w.Write(buf)
	require.NoError(t, err)

	cc := c.Clone()
	assert.Equal(t, http.StatusOK, cc.Writer().Status())
	assert.Equal(t, len(buf), cc.Writer().Size())
	assert.Equal(t, wantValues, c.QueryParams())
	_, err = cc.Writer().Write([]byte("invalid"))
	assert.ErrorIs(t, err, ErrDiscardedResponseWriter)
}

func TestContext_Done(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	ctx, cancel := netcontext.WithCancel(netcontext.Background())
	cancel()
	req = req.WithContext(ctx)
	_, c := NewTestContext(httptest.NewRecorder(), req)
	select {
	case <-c.Done():
		require.ErrorIs(t, c.Request().Context().Err(), netcontext.Canceled)
	case <-time.After(1):
		t.FailNow()
	}
}

func TestContext_Redirect(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
	require.NoError(t, c.Redirect(http.StatusTemporaryRedirect, "https://example.com/foo/bar"))
	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.Equal(t, "https://example.com/foo/bar", w.Header().Get(HeaderLocation))
}

func TestContext_Blob(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
	buf := []byte("foobar")
	require.NoError(t, c.Blob(http.StatusCreated, MIMETextPlain, buf))
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, http.StatusCreated, c.Writer().Status())
	assert.Equal(t, MIMETextPlain, w.Header().Get(HeaderContentType))
	assert.Equal(t, buf, w.Body.Bytes())
	assert.Equal(t, len(buf), c.Writer().Size())
	assert.True(t, c.Writer().Written())
}

func TestContext_Stream(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
	buf := []byte("foobar")
	require.NoError(t, c.Stream(http.StatusCreated, MIMETextPlain, bytes.NewBuffer(buf)))
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, http.StatusCreated, c.Writer().Status())
	assert.Equal(t, MIMETextPlain, w.Header().Get(HeaderContentType))
	assert.Equal(t, buf, w.Body.Bytes())
	assert.Equal(t, len(buf), c.Writer().Size())
	assert.True(t, c.Writer().Written())
}

func TestContext_String(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
	s := "foobar"
	require.NoError(t, c.String(http.StatusCreated, s))
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, http.StatusCreated, c.Writer().Status())
	assert.Equal(t, MIMETextPlainCharsetUTF8, w.Header().Get(HeaderContentType))
	assert.Equal(t, s, w.Body.String())
	assert.Equal(t, len(s), c.Writer().Size())
	assert.True(t, c.Writer().Written())
}

func TestContext_Writer(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	_, c := NewTestContext(w, r)
	buf := []byte("foobar")
	c.Writer().WriteHeader(http.StatusCreated)
	assert.Equal(t, 0, c.Writer().Size())
	n, err := c.Writer().Write(buf)
	require.NoError(t, err)
	assert.Equal(t, len(buf), n)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, http.StatusCreated, c.Writer().Status())
	assert.Equal(t, buf, w.Body.Bytes())
	assert.Equal(t, len(buf), c.Writer().Size())
	assert.True(t, c.Writer().Written())
}