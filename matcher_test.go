package fox

import (
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestQueryMatcher_As(t *testing.T) {
	m := QueryMatcher{key: "foo", value: "bar"}

	var target QueryMatcher
	assert.True(t, m.As(&target))
	assert.Equal(t, m.key, target.Key())
	assert.Equal(t, m.value, target.Value())

	var wrongTarget HeaderMatcher
	assert.False(t, m.As(&wrongTarget))
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

func TestQueryRegexpMatcher_As(t *testing.T) {
	m := QueryRegexpMatcher{key: "id", regex: regexp.MustCompile(`^\d+$`)}

	var target QueryRegexpMatcher
	assert.True(t, m.As(&target))
	assert.Equal(t, m.key, target.Key())
	assert.Equal(t, m.regex.String(), target.Regex().String())

	var wrongTarget QueryMatcher
	assert.False(t, m.As(&wrongTarget))
}

func TestHeaderMatcher_Match(t *testing.T) {
	cases := []struct {
		name      string
		headerKey string
		value     string
		headers   map[string]string
		want      bool
	}{
		{
			name:      "match header",
			headerKey: "Content-Type",
			value:     "application/json",
			headers:   map[string]string{"Content-Type": "application/json"},
			want:      true,
		},
		{
			name:      "no match different value",
			headerKey: "Content-Type",
			value:     "application/json",
			headers:   map[string]string{"Content-Type": "text/plain"},
			want:      false,
		},
		{
			name:      "no match missing header",
			headerKey: "Content-Type",
			value:     "application/json",
			headers:   map[string]string{"Accept": "application/json"},
			want:      false,
		},
		{
			name:      "no match empty headers",
			headerKey: "Content-Type",
			value:     "application/json",
			headers:   nil,
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := HeaderMatcher{canonicalKey: http.CanonicalHeaderKey(tc.headerKey), value: tc.value}
			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
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

func TestHeaderMatcher_As(t *testing.T) {
	m := HeaderMatcher{canonicalKey: "Content-Type", value: "application/json"}

	var target HeaderMatcher
	assert.True(t, m.As(&target))
	assert.Equal(t, m.canonicalKey, target.Key())
	assert.Equal(t, m.value, target.Value())

	var wrongTarget QueryMatcher
	assert.False(t, m.As(&wrongTarget))
}

func TestHeaderRegexpMatcher_Match(t *testing.T) {
	cases := []struct {
		name      string
		headerKey string
		pattern   string
		headers   map[string]string
		want      bool
	}{
		{
			name:      "match regex",
			headerKey: "Content-Type",
			pattern:   `^application/.*`,
			headers:   map[string]string{"Content-Type": "application/json"},
			want:      true,
		},
		{
			name:      "no match regex",
			headerKey: "Content-Type",
			pattern:   `^application/.*`,
			headers:   map[string]string{"Content-Type": "text/plain"},
			want:      false,
		},
		{
			name:      "missing header",
			headerKey: "Content-Type",
			pattern:   `^application/.*`,
			headers:   map[string]string{"Accept": "application/json"},
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := HeaderRegexpMatcher{canonicalKey: http.CanonicalHeaderKey(tc.headerKey), regex: regexp.MustCompile(tc.pattern)}
			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
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

func TestHeaderRegexpMatcher_As(t *testing.T) {
	m := HeaderRegexpMatcher{canonicalKey: "Content-Type", regex: regexp.MustCompile(`^application/.*`)}

	var target HeaderRegexpMatcher
	assert.True(t, m.As(&target))
	assert.Equal(t, m.canonicalKey, target.Key())
	assert.Equal(t, m.regex.String(), target.Regex().String())

	var wrongTarget HeaderMatcher
	assert.False(t, m.As(&wrongTarget))
}

func TestClientIpMatcher_Match(t *testing.T) {
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ipNet, _ := net.ParseCIDR(tc.cidr)
			m := ClientIpMatcher{ipNet: ipNet}

			resolver := ClientIPResolverFunc(func(c Context) (*net.IPAddr, error) {
				return &net.IPAddr{IP: net.ParseIP(tc.clientIP)}, nil
			})

			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			w := httptest.NewRecorder()
			f, c := NewTestContext(w, req, WithClientIPResolver(resolver))
			rte, _ := f.NewRoute("/path", emptyHandler)
			c.SetRoute(rte)
			assert.Equal(t, tc.want, m.Match(c))
		})
	}
}

func TestClientIpMatcher_Equal(t *testing.T) {
	_, ipNet1, _ := net.ParseCIDR("192.168.1.0/24")
	_, ipNet2, _ := net.ParseCIDR("192.168.1.0/24")
	_, ipNet3, _ := net.ParseCIDR("192.168.2.0/24")
	_, ipNet4, _ := net.ParseCIDR("192.168.1.0/16")

	cases := []struct {
		name string
		m1   ClientIpMatcher
		m2   Matcher
		want bool
	}{
		{
			name: "equal matchers",
			m1:   ClientIpMatcher{ipNet: ipNet1},
			m2:   ClientIpMatcher{ipNet: ipNet2},
			want: true,
		},
		{
			name: "different ip",
			m1:   ClientIpMatcher{ipNet: ipNet1},
			m2:   ClientIpMatcher{ipNet: ipNet3},
			want: false,
		},
		{
			name: "different mask",
			m1:   ClientIpMatcher{ipNet: ipNet1},
			m2:   ClientIpMatcher{ipNet: ipNet4},
			want: false,
		},
		{
			name: "different type",
			m1:   ClientIpMatcher{ipNet: ipNet1},
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

func TestClientIpMatcher_As(t *testing.T) {
	_, ipNet, _ := net.ParseCIDR("192.168.1.0/24")
	m := ClientIpMatcher{ipNet: ipNet}

	var target ClientIpMatcher
	assert.True(t, m.As(&target))
	assert.Equal(t, m.ipNet.String(), target.IPNet().String())

	var wrongTarget QueryMatcher
	assert.False(t, m.As(&wrongTarget))
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
			assert.NotNil(t, m.Regex())
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
			name:    "invalid regexp",
			key:     "Content-Type",
			expr:    `[`,
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
			assert.NotNil(t, m.Regex())
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
			assert.NotNil(t, m.IPNet())
		})
	}
}
