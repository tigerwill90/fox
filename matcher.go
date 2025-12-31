package fox

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"regexp"

	"github.com/tigerwill90/fox/internal/netutil"
)

// Matcher evaluates if an HTTP request satisfies specific conditions. Matchers are evaluated after hostname and path
// matching succeeds. All matchers associated with a route must match for the route to be selected.
// Matcher implementations must be safe for concurrent use by multiple goroutines.
type Matcher interface {
	// Match evaluates if the [RequestContext] satisfies this matcher.
	Match(c RequestContext) bool
	// Equal reports whether this matcher is semantically equivalent to another. Implementation must
	// - Handle type checking: matchers of different types are not equal
	// - Be reflexive: m.Equal(m) == true
	// - Be symmetric: m.Equal(n) == n.Equal(m)
	Equal(m Matcher) bool
}

func MatchQuery(key, value string) (QueryMatcher, error) {
	if key == "" {
		return QueryMatcher{}, errors.New("empty query key")
	}
	return QueryMatcher{
		key:   key,
		value: value,
	}, nil
}

type QueryMatcher struct {
	key   string
	value string
}

func (m QueryMatcher) Key() string {
	return m.key
}

func (m QueryMatcher) Value() string {
	return m.value
}

func (m QueryMatcher) Match(c RequestContext) bool {
	return c.QueryParam(m.key) == m.value
}

func (m QueryMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(QueryMatcher)
	if !ok {
		return false
	}
	return m.key == om.key && m.value == om.value
}

func (m QueryMatcher) String() string {
	return "q:" + m.key + "=" + m.value
}

func MatchQueryRegexp(key, expr string) (QueryRegexpMatcher, error) {
	if key == "" {
		return QueryRegexpMatcher{}, errors.New("empty query key")
	}
	regex, err := regexp.Compile("^" + expr + "$")
	if err != nil {
		return QueryRegexpMatcher{}, err
	}
	return QueryRegexpMatcher{
		key:   key,
		regex: regex,
	}, nil
}

type QueryRegexpMatcher struct {
	regex *regexp.Regexp
	key   string
}

func (m QueryRegexpMatcher) Key() string {
	return m.key
}

func (m QueryRegexpMatcher) Regex() *regexp.Regexp {
	re2 := *m.regex
	return &re2
}

func (m QueryRegexpMatcher) String() string {
	return "qx:" + m.key + "=" + m.regex.String()
}

func (m QueryRegexpMatcher) Match(c RequestContext) bool {
	return m.regex.MatchString(c.QueryParam(m.key))
}

func (m QueryRegexpMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(QueryRegexpMatcher)
	if !ok {
		return false
	}
	return m.key == om.key && m.regex.String() == om.regex.String()
}

func MatchHeader(key, value string) (HeaderMatcher, error) {
	if key == "" {
		return HeaderMatcher{}, errors.New("empty header key")
	}
	return HeaderMatcher{
		canonicalKey: http.CanonicalHeaderKey(key),
		value:        value,
	}, nil
}

type HeaderMatcher struct {
	canonicalKey string
	value        string
}

func (m HeaderMatcher) Key() string {
	return m.canonicalKey
}

func (m HeaderMatcher) Value() string {
	return m.value
}

func (m HeaderMatcher) Match(c RequestContext) bool {
	values := c.Request().Header[m.canonicalKey]
	if len(values) == 0 {
		return false
	}
	return values[0] == m.value
}

func (m HeaderMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(HeaderMatcher)
	if !ok {
		return false
	}
	return m.canonicalKey == om.canonicalKey && m.value == om.value
}

func (m HeaderMatcher) String() string {
	return "h:" + m.canonicalKey + "=" + m.value
}

func MatchHeaderRegexp(key, expr string) (HeaderRegexpMatcher, error) {
	if key == "" {
		return HeaderRegexpMatcher{}, errors.New("empty header key")
	}
	regex, err := regexp.Compile("^" + expr + "$")
	if err != nil {
		return HeaderRegexpMatcher{}, err
	}
	return HeaderRegexpMatcher{
		canonicalKey: http.CanonicalHeaderKey(key),
		regex:        regex,
	}, nil
}

type HeaderRegexpMatcher struct {
	regex        *regexp.Regexp
	canonicalKey string
}

func (m HeaderRegexpMatcher) Key() string {
	return m.canonicalKey
}

func (m HeaderRegexpMatcher) Regex() *regexp.Regexp {
	re2 := *m.regex
	return &re2
}

func (m HeaderRegexpMatcher) Match(c RequestContext) bool {
	values := c.Request().Header[m.canonicalKey]
	if len(values) == 0 {
		return false
	}
	return m.regex.MatchString(values[0])
}

func (m HeaderRegexpMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(HeaderRegexpMatcher)
	if !ok {
		return false
	}
	return m.canonicalKey == om.canonicalKey && m.regex.String() == om.regex.String()
}

func (m HeaderRegexpMatcher) String() string {
	return "hx:" + m.canonicalKey + "=" + m.regex.String()
}

func MatchClientIP(ip string) (ClientIpMatcher, error) {
	ipNet, err := netutil.ParseCIDR(ip)
	if err != nil {
		return ClientIpMatcher{}, err
	}
	return ClientIpMatcher{
		ipNet: ipNet,
	}, nil
}

type ClientIpMatcher struct {
	ipNet *net.IPNet
}

func (m ClientIpMatcher) IPNet() *net.IPNet {
	ip := make(net.IP, len(m.ipNet.IP))
	copy(ip, m.ipNet.IP)

	mask := make(net.IPMask, len(m.ipNet.Mask))
	copy(mask, m.ipNet.Mask)

	return &net.IPNet{
		IP:   ip,
		Mask: mask,
	}
}

func (m ClientIpMatcher) Match(c RequestContext) bool {
	addr, err := c.ClientIP()
	if err != nil {
		return false
	}
	return m.ipNet.Contains(addr.IP)
}

func (m ClientIpMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(ClientIpMatcher)
	if !ok {
		return false
	}
	return m.ipNet.IP.Equal(om.ipNet.IP) && bytes.Equal(m.ipNet.Mask, om.ipNet.Mask)
}

func (m ClientIpMatcher) String() string {
	return "ip:" + m.ipNet.String()
}
