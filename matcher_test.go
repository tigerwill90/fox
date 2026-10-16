package fox

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryMatcher_Match(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		url   string
		want  bool
	}{
		{
			name:  "match query param",
			key:   "foo",
			value: "bar",
			url:   "/path?foo=bar",
			want:  true,
		},
		{
			name:  "no match different value",
			key:   "foo",
			value: "bar",
			url:   "/path?foo=baz",
			want:  false,
		},
		{
			name:  "no match missing key",
			key:   "foo",
			value: "bar",
			url:   "/path?other=bar",
			want:  false,
		},
		{
			name:  "no match empty query",
			key:   "foo",
			value: "bar",
			url:   "/path",
			want:  false,
		},
		{
			name:  "match second value of multi-value",
			key:   "foo",
			value: "b",
			url:   "/path?foo=a&foo=b",
			want:  true,
		},
		{
			name:  "no match across multi-value",
			key:   "foo",
			value: "c",
			url:   "/path?foo=a&foo=b",
			want:  false,
		},
		{
			name:  "no match empty value with missing key",
			key:   "foo",
			value: "",
			url:   "/path",
			want:  false,
		},
		{
			name:  "match empty value with present empty value",
			key:   "foo",
			value: "",
			url:   "/path?foo=",
			want:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := QueryMatcher{key: tc.key, value: tc.value}
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()
			c := NewTestContextOnly(w, req)
			assert.Equal(t, tc.want, m.Match(c))
		})
	}
}

func TestQueryMatcher_Equal(t *testing.T) {
	cases := []struct {
		name string
		m1   QueryMatcher
		m2   Matcher
		want bool
	}{
		{
			name: "equal matchers",
			m1:   QueryMatcher{key: "foo", value: "bar"},
			m2:   QueryMatcher{key: "foo", value: "bar"},
			want: true,
		},
		{
			name: "different key",
			m1:   QueryMatcher{key: "foo", value: "bar"},
			m2:   QueryMatcher{key: "baz", value: "bar"},
			want: false,
		},
		{
			name: "different value",
			m1:   QueryMatcher{key: "foo", value: "bar"},
			m2:   QueryMatcher{key: "foo", value: "baz"},
			want: false,
		},
		{
			name: "different type",
			m1:   QueryMatcher{key: "foo", value: "bar"},
			m2:   HeaderMatcher{canonicalKey: "foo", value: "bar"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.m1.Equal(tc.m2))
		})
	}
}

func TestQueryRegexpMatcher_Match(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		pattern string
		url     string
		want    bool
	}{
		{
			name:    "match regex",
			key:     "id",
			pattern: `^\d+$`,
			url:     "/path?id=123",
			want:    true,
		},
		{
			name:    "no match regex",
			key:     "id",
			pattern: `^\d+$`,
			url:     "/path?id=abc",
			want:    false,
		},
		{
			name:    "missing key",
			key:     "id",
			pattern: `^\d+$`,
			url:     "/path?other=123",
			want:    false,
		},
		{
			name:    "match second value of multi-value",
			key:     "id",
			pattern: `^\d+$`,
			url:     "/path?id=abc&id=123",
			want:    true,
		},
		{
			name:    "no match across multi-value",
			key:     "id",
			pattern: `^\d+$`,
			url:     "/path?id=abc&id=def",
			want:    false,
		},
		{
			name:    "no match permissive regex with missing key",
			key:     "id",
			pattern: `^.*$`,
			url:     "/path",
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := QueryRegexpMatcher{key: tc.key, regex: regexp.MustCompile(tc.pattern)}
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()
			c := NewTestContextOnly(w, req)
			assert.Equal(t, tc.want, m.Match(c))
		})
	}
}

func TestQueryRegexpMatcher_Equal(t *testing.T) {
	cases := []struct {
		name string
		m1   QueryRegexpMatcher
		m2   Matcher
		want bool
	}{
		{
			name: "equal matchers",
			m1:   QueryRegexpMatcher{key: "id", regex: regexp.MustCompile(`^\d+$`)},
			m2:   QueryRegexpMatcher{key: "id", regex: regexp.MustCompile(`^\d+$`)},
			want: true,
		},
		{
			name: "different key",
			m1:   QueryRegexpMatcher{key: "id", regex: regexp.MustCompile(`^\d+$`)},
			m2:   QueryRegexpMatcher{key: "other", regex: regexp.MustCompile(`^\d+$`)},
			want: false,
		},
		{
			name: "different regex",
			m1:   QueryRegexpMatcher{key: "id", regex: regexp.MustCompile(`^\d+$`)},
			m2:   QueryRegexpMatcher{key: "id", regex: regexp.MustCompile(`^\w+$`)},
			want: false,
		},
		{
			name: "different type",
			m1:   QueryRegexpMatcher{key: "id", regex: regexp.MustCompile(`^\d+$`)},
			m2:   QueryMatcher{key: "id", value: "123"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.m1.Equal(tc.m2))
		})
	}
}

func TestHeaderMatcher_Match(t *testing.T) {
	cases := []struct {
		name      string
		headerKey string
		value     string
		headers   map[string][]string
		want      bool
	}{
		{
			name:      "match header",
			headerKey: "Content-Type",
			value:     "application/json",
			headers:   map[string][]string{"Content-Type": {"application/json"}},
			want:      true,
		},
		{
			name:      "no match different value",
			headerKey: "Content-Type",
			value:     "application/json",
			headers:   map[string][]string{"Content-Type": {"text/plain"}},
			want:      false,
		},
		{
			name:      "no match missing header",
			headerKey: "Content-Type",
			value:     "application/json",
			headers:   map[string][]string{"Accept": {"application/json"}},
			want:      false,
		},
		{
			name:      "no match empty headers",
			headerKey: "Content-Type",
			value:     "application/json",
			headers:   nil,
			want:      false,
		},
		{
			name:      "match second value of multi-value",
			headerKey: "Content-Type",
			value:     "application/json",
			headers:   map[string][]string{"Content-Type": {"text/plain", "application/json"}},
			want:      true,
		},
		{
			name:      "no match across multi-value",
			headerKey: "Content-Type",
			value:     "application/json",
			headers:   map[string][]string{"Content-Type": {"text/plain", "text/html"}},
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := HeaderMatcher{canonicalKey: http.CanonicalHeaderKey(tc.headerKey), value: tc.value}
			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			for k, vs := range tc.headers {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}
			w := httptest.NewRecorder()
			c := NewTestContextOnly(w, req)
			assert.Equal(t, tc.want, m.Match(c))
		})
	}
}

func TestHeaderMatcher_Equal(t *testing.T) {
	cases := []struct {
		name string
		m1   HeaderMatcher
		m2   Matcher
		want bool
	}{
		{
			name: "equal matchers",
			m1:   HeaderMatcher{canonicalKey: "Content-Type", value: "application/json"},
			m2:   HeaderMatcher{canonicalKey: "Content-Type", value: "application/json"},
			want: true,
		},
		{
			name: "different key",
			m1:   HeaderMatcher{canonicalKey: "Content-Type", value: "application/json"},
			m2:   HeaderMatcher{canonicalKey: "Accept", value: "application/json"},
			want: false,
		},
		{
			name: "different value",
			m1:   HeaderMatcher{canonicalKey: "Content-Type", value: "application/json"},
			m2:   HeaderMatcher{canonicalKey: "Content-Type", value: "text/plain"},
			want: false,
		},
		{
			name: "different type",
			m1:   HeaderMatcher{canonicalKey: "Content-Type", value: "application/json"},
			m2:   QueryMatcher{key: "Content-Type", value: "application/json"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.m1.Equal(tc.m2))
		})
	}
}

func TestHeaderRegexpMatcher_Match(t *testing.T) {
	cases := []struct {
		name      string
		headerKey string
		pattern   string
		headers   map[string][]string
		want      bool
	}{
		{
			name:      "match regex",
			headerKey: "Content-Type",
			pattern:   `^application/.*`,
			headers:   map[string][]string{"Content-Type": {"application/json"}},
			want:      true,
		},
		{
			name:      "no match regex",
			headerKey: "Content-Type",
			pattern:   `^application/.*`,
			headers:   map[string][]string{"Content-Type": {"text/plain"}},
			want:      false,
		},
		{
			name:      "missing header",
			headerKey: "Content-Type",
			pattern:   `^application/.*`,
			headers:   map[string][]string{"Accept": {"application/json"}},
			want:      false,
		},
		{
			name:      "match second value of multi-value",
			headerKey: "Content-Type",
			pattern:   `^application/.*`,
			headers:   map[string][]string{"Content-Type": {"text/plain", "application/json"}},
			want:      true,
		},
		{
			name:      "no match across multi-value",
			headerKey: "Content-Type",
			pattern:   `^application/.*`,
			headers:   map[string][]string{"Content-Type": {"text/plain", "text/html"}},
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := HeaderRegexpMatcher{canonicalKey: http.CanonicalHeaderKey(tc.headerKey), regex: regexp.MustCompile(tc.pattern)}
			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			for k, vs := range tc.headers {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}
			w := httptest.NewRecorder()
			c := NewTestContextOnly(w, req)
			assert.Equal(t, tc.want, m.Match(c))
		})
	}
}

func TestHeaderRegexpMatcher_Equal(t *testing.T) {
	cases := []struct {
		name string
		m1   HeaderRegexpMatcher
		m2   Matcher
		want bool
	}{
		{
			name: "equal matchers",
			m1:   HeaderRegexpMatcher{canonicalKey: "Content-Type", regex: regexp.MustCompile(`^application/.*`)},
			m2:   HeaderRegexpMatcher{canonicalKey: "Content-Type", regex: regexp.MustCompile(`^application/.*`)},
			want: true,
		},
		{
			name: "different key",
			m1:   HeaderRegexpMatcher{canonicalKey: "Content-Type", regex: regexp.MustCompile(`^application/.*`)},
			m2:   HeaderRegexpMatcher{canonicalKey: "Accept", regex: regexp.MustCompile(`^application/.*`)},
			want: false,
		},
		{
			name: "different regex",
			m1:   HeaderRegexpMatcher{canonicalKey: "Content-Type", regex: regexp.MustCompile(`^application/.*`)},
			m2:   HeaderRegexpMatcher{canonicalKey: "Content-Type", regex: regexp.MustCompile(`^text/.*`)},
			want: false,
		},
		{
			name: "different type",
			m1:   HeaderRegexpMatcher{canonicalKey: "Content-Type", regex: regexp.MustCompile(`^application/.*`)},
			m2:   HeaderMatcher{canonicalKey: "Content-Type", value: "application/json"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.m1.Equal(tc.m2))
		})
	}
}

func TestClientIPMatcher_Match(t *testing.T) {
	cases := []struct {
		name     string
		cidr     string
		clientIP string
		want     bool
	}{
		{
			name:     "match single ip",
			cidr:     "192.168.1.1/32",
			clientIP: "192.168.1.1",
			want:     true,
		},
		{
			name:     "match ip in range",
			cidr:     "192.168.1.0/24",
			clientIP: "192.168.1.100",
			want:     true,
		},
		{
			name:     "no match ip outside range",
			cidr:     "192.168.1.0/24",
			clientIP: "192.168.2.1",
			want:     false,
		},
		{
			name:     "match ipv6",
			cidr:     "2001:db8::/32",
			clientIP: "2001:db8::1",
			want:     true,
		},
		{
			name:     "match ipv4 mapped ipv6 client ip",
			cidr:     "192.168.1.0/24",
			clientIP: "::ffff:192.168.1.100",
			want:     true,
		},
		{
			name:     "match zoned link local client ip",
			cidr:     "fe80::/10",
			clientIP: "fe80::1%eth0",
			want:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := ClientIPMatcher{prefix: netip.MustParsePrefix(tc.cidr)}

			resolver := ClientIPResolverFunc(func(c RequestContext) (netip.Addr, error) {
				return netip.MustParseAddr(tc.clientIP), nil
			})

			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			w := httptest.NewRecorder()
			f, c := NewTestContext(w, req, WithClientIPResolver(resolver))
			rte, _ := f.NewRoute(MethodGet, "/path", emptyHandler)
			c.route = rte
			assert.Equal(t, tc.want, m.Match(c))
		})
	}
}

func TestClientIPMatcher_RouteResolver(t *testing.T) {
	routeResolver := ClientIPResolverFunc(func(c RequestContext) (netip.Addr, error) {
		return netip.MustParseAddr("127.0.0.1"), nil
	})
	globalResolver := ClientIPResolverFunc(func(c RequestContext) (netip.Addr, error) {
		return netip.MustParseAddr("10.0.0.1"), nil
	})

	f := MustRouter()
	f.MustAdd(MethodGet, "/foo", emptyHandler, WithClientIPResolver(routeResolver), WithClientIPMatcher("127.0.0.0/8"))

	w := httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/foo", nil))
	assert.Equal(t, http.StatusOK, w.Code)

	f = MustRouter(WithClientIPResolver(globalResolver))
	f.MustAdd(MethodGet, "/foo", emptyHandler, WithClientIPResolver(routeResolver), WithClientIPMatcher("127.0.0.0/8"))

	w = httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/foo", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestClientIPMatcher_Equal(t *testing.T) {
	prefix1 := netip.MustParsePrefix("192.168.1.0/24")
	prefix2 := netip.MustParsePrefix("192.168.1.0/24")
	prefix3 := netip.MustParsePrefix("192.168.2.0/24")
	prefix4 := netip.MustParsePrefix("192.168.1.0/16")

	cases := []struct {
		name string
		m1   ClientIPMatcher
		m2   Matcher
		want bool
	}{
		{
			name: "equal matchers",
			m1:   ClientIPMatcher{prefix: prefix1},
			m2:   ClientIPMatcher{prefix: prefix2},
			want: true,
		},
		{
			name: "different ip",
			m1:   ClientIPMatcher{prefix: prefix1},
			m2:   ClientIPMatcher{prefix: prefix3},
			want: false,
		},
		{
			name: "different mask",
			m1:   ClientIPMatcher{prefix: prefix1},
			m2:   ClientIPMatcher{prefix: prefix4},
			want: false,
		},
		{
			name: "different type",
			m1:   ClientIPMatcher{prefix: prefix1},
			m2:   QueryMatcher{key: "ip", value: "192.168.1.0/24"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.m1.Equal(tc.m2))
		})
	}
}

func TestMatchQuery(t *testing.T) {
	cases := []struct {
		name      string
		key       string
		value     string
		wantErr   bool
		wantKey   string
		wantValue string
	}{
		{
			name:      "valid query matcher",
			key:       "foo",
			value:     "bar",
			wantErr:   false,
			wantKey:   "foo",
			wantValue: "bar",
		},
		{
			name:    "empty key",
			key:     "",
			value:   "bar",
			wantErr: true,
		},
		{
			name:      "empty value is valid",
			key:       "foo",
			value:     "",
			wantErr:   false,
			wantKey:   "foo",
			wantValue: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := MatchQuery(tc.key, tc.value)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantKey, m.Key())
			assert.Equal(t, tc.wantValue, m.Value())
		})
	}
}

func TestMatchQueryRegexp(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		expr    string
		wantErr bool
		wantKey string
	}{
		{
			name:    "valid query regexp matcher",
			key:     "id",
			expr:    `\d+`,
			wantErr: false,
			wantKey: "id",
		},
		{
			name:    "empty key",
			key:     "",
			expr:    `\d+`,
			wantErr: true,
		},
		{
			name:    "invalid regexp",
			key:     "id",
			expr:    `[`,
			wantErr: true,
		},
		{
			name:    "regexp anchor escape rejected",
			key:     "id",
			expr:    `a)|(?:`,
			wantErr: true,
		},
		{
			name:    "regexp too large",
			key:     "id",
			expr:    `[a-z]{1,1000}`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := MatchQueryRegexp(tc.key, tc.expr)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantKey, m.Key())
			assert.Equal(t, tc.expr, m.Value())
		})
	}
}

func TestMatchHeader(t *testing.T) {
	cases := []struct {
		name      string
		key       string
		value     string
		wantErr   bool
		wantKey   string
		wantValue string
	}{
		{
			name:      "valid header matcher",
			key:       "Content-Type",
			value:     "application/json",
			wantErr:   false,
			wantKey:   "Content-Type",
			wantValue: "application/json",
		},
		{
			name:      "lowercase key gets canonicalized",
			key:       "content-type",
			value:     "application/json",
			wantErr:   false,
			wantKey:   "Content-Type",
			wantValue: "application/json",
		},
		{
			name:    "empty key",
			key:     "",
			value:   "application/json",
			wantErr: true,
		},
		{
			name:    "host key",
			key:     "host",
			value:   "example.com",
			wantErr: true,
		},
		{
			name:    "transfer-encoding key",
			key:     "Transfer-Encoding",
			value:   "chunked",
			wantErr: true,
		},
		{
			name:    "key with space",
			key:     "X Foo",
			value:   "bar",
			wantErr: true,
		},
		{
			name:    "key with trailing colon",
			key:     "X-Foo:",
			value:   "bar",
			wantErr: true,
		},
		{
			name:      "empty value is valid",
			key:       "X-Custom",
			value:     "",
			wantErr:   false,
			wantKey:   "X-Custom",
			wantValue: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := MatchHeader(tc.key, tc.value)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantKey, m.Key())
			assert.Equal(t, tc.wantValue, m.Value())
		})
	}
}

func TestMatchHeaderRegexp(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		expr    string
		wantErr bool
		wantKey string
	}{
		{
			name:    "valid header regexp matcher",
			key:     "Content-Type",
			expr:    `application/.*`,
			wantErr: false,
			wantKey: "Content-Type",
		},
		{
			name:    "lowercase key gets canonicalized",
			key:     "content-type",
			expr:    `application/.*`,
			wantErr: false,
			wantKey: "Content-Type",
		},
		{
			name:    "empty key",
			key:     "",
			expr:    `application/.*`,
			wantErr: true,
		},
		{
			name:    "host key",
			key:     "Host",
			expr:    `.*\.example\.com`,
			wantErr: true,
		},
		{
			name:    "transfer-encoding key",
			key:     "transfer-encoding",
			expr:    `chunked`,
			wantErr: true,
		},
		{
			name:    "invalid regexp",
			key:     "Content-Type",
			expr:    `[`,
			wantErr: true,
		},
		{
			name:    "regexp anchor escape rejected",
			key:     "Content-Type",
			expr:    `a)|(?:`,
			wantErr: true,
		},
		{
			name:    "regexp too large",
			key:     "Content-Type",
			expr:    `[a-z]{1,1000}`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := MatchHeaderRegexp(tc.key, tc.expr)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantKey, m.Key())
			assert.Equal(t, tc.expr, m.Value())
		})
	}
}

func TestRegexpMatcherAlternationPrecedence(t *testing.T) {
	mq, err := MatchQueryRegexp("scope", "read|write")
	require.NoError(t, err)
	mh, err := MatchHeaderRegexp("X-Role", "admin|user|guest")
	require.NoError(t, err)

	queryCases := []struct {
		url  string
		want bool
	}{
		{"/?scope=read", true},
		{"/?scope=write", true},
		{"/?scope=readBYPASS", false},
		{"/?scope=EVILwrite", false},
	}
	for _, tc := range queryCases {
		t.Run("query "+tc.url, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			c := NewTestContextOnly(httptest.NewRecorder(), req)
			assert.Equal(t, tc.want, mq.Match(c))
		})
	}

	headerCases := []struct {
		value string
		want  bool
	}{
		{"admin", true},
		{"user", true},
		{"guest", true},
		{"adminBYPASS", false},
		{"EVILguest", false},
	}
	for _, tc := range headerCases {
		t.Run("header "+tc.value, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Role", tc.value)
			c := NewTestContextOnly(httptest.NewRecorder(), req)
			assert.Equal(t, tc.want, mh.Match(c))
		})
	}
}

func TestSchemeMatcher_Match(t *testing.T) {
	cases := []struct {
		name      string
		scheme    string
		urlScheme string
		tls       bool
		want      bool
	}{
		{
			name:   "match https with TLS",
			scheme: "https",
			tls:    true,
			want:   true,
		},
		{
			name:   "match http without TLS",
			scheme: "http",
			tls:    false,
			want:   true,
		},
		{
			name:   "no match https without TLS",
			scheme: "https",
			tls:    false,
			want:   false,
		},
		{
			name:   "no match http with TLS",
			scheme: "http",
			tls:    true,
			want:   false,
		},
		{
			name:      "no spoof via r.URL.Scheme without TLS",
			scheme:    "https",
			urlScheme: "https",
			tls:       false,
			want:      false,
		},
		{
			name:      "no spoof via r.URL.Scheme with TLS",
			scheme:    "http",
			urlScheme: "http",
			tls:       true,
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := SchemeMatcher{scheme: tc.scheme}
			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			if tc.urlScheme != "" {
				req.URL.Scheme = tc.urlScheme
			} else {
				req.URL.Scheme = ""
			}
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			} else {
				req.TLS = nil
			}
			w := httptest.NewRecorder()
			c := NewTestContextOnly(w, req)
			assert.Equal(t, tc.want, m.Match(c))
		})
	}
}

func TestSchemeMatcher_Equal(t *testing.T) {
	cases := []struct {
		name string
		m1   SchemeMatcher
		m2   Matcher
		want bool
	}{
		{
			name: "equal matchers",
			m1:   SchemeMatcher{scheme: "https"},
			m2:   SchemeMatcher{scheme: "https"},
			want: true,
		},
		{
			name: "different scheme",
			m1:   SchemeMatcher{scheme: "https"},
			m2:   SchemeMatcher{scheme: "http"},
			want: false,
		},
		{
			name: "different type",
			m1:   SchemeMatcher{scheme: "https"},
			m2:   QueryMatcher{key: "scheme", value: "https"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.m1.Equal(tc.m2))
		})
	}
}

func TestSchemeMatcher_String(t *testing.T) {
	m := SchemeMatcher{scheme: "https"}
	assert.Equal(t, "s:https", m.String())
}

func TestMatchScheme(t *testing.T) {
	cases := []struct {
		name       string
		scheme     string
		wantErr    bool
		wantScheme string
	}{
		{
			name:       "valid http",
			scheme:     "http",
			wantErr:    false,
			wantScheme: "http",
		},
		{
			name:       "valid https",
			scheme:     "https",
			wantErr:    false,
			wantScheme: "https",
		},
		{
			name:       "uppercase HTTPS gets canonicalized",
			scheme:     "HTTPS",
			wantErr:    false,
			wantScheme: "https",
		},
		{
			name:       "mixed case Https gets canonicalized",
			scheme:     "Https",
			wantErr:    false,
			wantScheme: "https",
		},
		{
			name:    "invalid scheme ws",
			scheme:  "ws",
			wantErr: true,
		},
		{
			name:    "empty scheme",
			scheme:  "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := MatchScheme(tc.scheme)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantScheme, m.Scheme())
		})
	}
}

func TestMatchClientIP(t *testing.T) {
	cases := []struct {
		name    string
		ip      string
		wantErr bool
	}{
		{
			name:    "valid CIDR",
			ip:      "192.168.1.0/24",
			wantErr: false,
		},
		{
			name:    "valid single IP",
			ip:      "192.168.1.1",
			wantErr: false,
		},
		{
			name:    "valid IPv6 CIDR",
			ip:      "2001:db8::/32",
			wantErr: false,
		},
		{
			name:    "valid IPv6 single IP",
			ip:      "2001:db8::1",
			wantErr: false,
		},
		{
			name:    "invalid IP",
			ip:      "invalid",
			wantErr: true,
		},
		{
			name:    "empty IP",
			ip:      "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := MatchClientIP(tc.ip)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.True(t, m.Prefix().IsValid())
		})
	}
}
