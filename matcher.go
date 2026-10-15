package fox

import (
	"errors"
	"net/http"
	"net/netip"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"

	"github.com/fox-toolkit/fox/internal/netutil"
)

const maxRegexpSize = 1000

// compileAnchoredRegexp compiles expr anchored at both ends. expr is parsed alone first, since an
// unbalanced source like "a)|(?:" would otherwise escape the anchors.
func compileAnchoredRegexp(expr string) (*regexp.Regexp, error) {
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return nil, err
	}
	if err = checkRegexpSize(expr, re, maxRegexpSize); err != nil {
		return nil, err
	}
	return regexp.Compile("^(?:" + expr + ")$")
}

// Matcher evaluates if an HTTP request satisfies specific conditions. Matchers are evaluated after hostname and path
// matching succeeds. All matchers associated with a route must match for the route to be selected. Match must return
// the same result for the same request and must not have side effects the application depends on. A lookup may
// evaluate the matchers of routes it does not select, more than once and in any order.
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

// MatchQuery returns a [QueryMatcher] that matches when the request URL contains a query parameter with the given key
// and a value equal to the given one. An empty key returns an error. See [WithQueryMatcher] for the option counterpart.
func MatchQuery(key, value string) (QueryMatcher, error) {
	if key == "" {
		return QueryMatcher{}, errors.New("empty query key")
	}
	return QueryMatcher{
		key:   key,
		value: value,
	}, nil
}

// QueryMatcher matches a request when the URL query contains the configured key with any value equal to the configured
// one. Multiple values for the same key are evaluated independently and the matcher succeeds on the first match.
type QueryMatcher struct {
	key   string
	value string
}

// Key returns the query parameter key tested by this matcher.
func (m QueryMatcher) Key() string {
	return m.key
}

// Value returns the expected query parameter value.
func (m QueryMatcher) Value() string {
	return m.value
}

// Match reports whether the request URL contains the configured key with any value equal to the configured one.
func (m QueryMatcher) Match(c RequestContext) bool {
	values := c.QueryParams()[m.key]
	if len(values) == 0 {
		return false
	}
	return slices.Contains(values, m.value)
}

// Equal reports whether matcher is a [QueryMatcher] with the same key and value.
func (m QueryMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(QueryMatcher)
	if !ok {
		return false
	}
	return m.key == om.key && m.value == om.value
}

// String returns a textual representation of the matcher in the form "q:key=value".
func (m QueryMatcher) String() string {
	return "q:" + m.key + "=" + m.value
}

// MatchQueryRegexp returns a [QueryRegexpMatcher] that matches when the request URL contains a query parameter with
// the given key whose value matches the given regular expression. The expression is auto anchored at both ends, requiring
// a full match of the parameter value. An empty key, or a regular expression that is invalid or compiles to more than
// 1000 instructions, returns an error. See [WithQueryRegexpMatcher] for the option counterpart.
func MatchQueryRegexp(key, expr string) (QueryRegexpMatcher, error) {
	if key == "" {
		return QueryRegexpMatcher{}, errors.New("empty query key")
	}
	regex, err := compileAnchoredRegexp(expr)
	if err != nil {
		return QueryRegexpMatcher{}, err
	}
	return QueryRegexpMatcher{
		key:   key,
		regex: regex,
	}, nil
}

// QueryRegexpMatcher matches a request when the URL query contains the configured key with any value matching the
// configured regular expression. Multiple values for the same key are evaluated independently and the matcher succeeds
// on the first match.
type QueryRegexpMatcher struct {
	regex *regexp.Regexp
	key   string
}

// Key returns the query parameter key tested by this matcher.
func (m QueryRegexpMatcher) Key() string {
	return m.key
}

// Value returns the regular expression matching the query parameter.
func (m QueryRegexpMatcher) Value() string {
	return rawExpr(m.regex)
}

// String returns a textual representation of the matcher in the form "qx:key=expr".
func (m QueryRegexpMatcher) String() string {
	return "qx:" + m.key + "=" + m.Value()
}

// Match reports whether the request URL contains the configured key with any value matching the configured regular
// expression.
func (m QueryRegexpMatcher) Match(c RequestContext) bool {
	values := c.QueryParams()[m.key]
	if len(values) == 0 {
		return false
	}
	return slices.ContainsFunc(values, m.regex.MatchString)
}

// Equal reports whether matcher is a [QueryRegexpMatcher] with the same key and regular expression source.
func (m QueryRegexpMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(QueryRegexpMatcher)
	if !ok {
		return false
	}
	return m.key == om.key && m.regex.String() == om.regex.String()
}

// MatchHeader returns a [HeaderMatcher] that matches when the request contains an HTTP header with the given key and
// a value equal to the given one. The key is canonicalized via [http.CanonicalHeaderKey]. An empty key returns an
// error. See [WithHeaderMatcher] for the option counterpart.
func MatchHeader(key, value string) (HeaderMatcher, error) {
	if key == "" {
		return HeaderMatcher{}, errors.New("empty header key")
	}
	return HeaderMatcher{
		canonicalKey: http.CanonicalHeaderKey(key),
		value:        value,
	}, nil
}

// HeaderMatcher matches a request when the configured header is present with any value equal to the configured one.
// Multiple values for the same header are evaluated independently and the matcher succeeds on the first match.
type HeaderMatcher struct {
	canonicalKey string
	value        string
}

// Key returns the canonicalized header key tested by this matcher.
func (m HeaderMatcher) Key() string {
	return m.canonicalKey
}

// Value returns the expected header value.
func (m HeaderMatcher) Value() string {
	return m.value
}

// Match reports whether the request contains the configured header with any value equal to the configured one.
func (m HeaderMatcher) Match(c RequestContext) bool {
	values := c.Request().Header[m.canonicalKey]
	if len(values) == 0 {
		return false
	}
	return slices.Contains(values, m.value)
}

// Equal reports whether matcher is a [HeaderMatcher] with the same key and value.
func (m HeaderMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(HeaderMatcher)
	if !ok {
		return false
	}
	return m.canonicalKey == om.canonicalKey && m.value == om.value
}

// String returns a textual representation of the matcher in the form "h:key=value".
func (m HeaderMatcher) String() string {
	return "h:" + m.canonicalKey + "=" + m.value
}

// MatchHeaderRegexp returns a [HeaderRegexpMatcher] that matches when the request contains an HTTP header with the
// given key whose value matches the given regular expression. The expression is auto anchored at both ends, requiring a
// full match of the header value. The key is canonicalized via [http.CanonicalHeaderKey]. An empty key, or a regular
// expression that is invalid or compiles to more than 1000 instructions, returns an error. See [WithHeaderRegexpMatcher]
// for the option counterpart.
func MatchHeaderRegexp(key, expr string) (HeaderRegexpMatcher, error) {
	if key == "" {
		return HeaderRegexpMatcher{}, errors.New("empty header key")
	}
	regex, err := compileAnchoredRegexp(expr)
	if err != nil {
		return HeaderRegexpMatcher{}, err
	}
	return HeaderRegexpMatcher{
		canonicalKey: http.CanonicalHeaderKey(key),
		regex:        regex,
	}, nil
}

// HeaderRegexpMatcher matches a request when the configured header is present with any value matching the configured
// regular expression. Multiple values for the same header are evaluated independently and the matcher succeeds on the
// first match.
type HeaderRegexpMatcher struct {
	regex        *regexp.Regexp
	canonicalKey string
}

// Key returns the canonicalized header key tested by this matcher.
func (m HeaderRegexpMatcher) Key() string {
	return m.canonicalKey
}

// Value returns the regular expression matching the header.
func (m HeaderRegexpMatcher) Value() string {
	return rawExpr(m.regex)
}

// Match reports whether the request contains the configured header with any value matching the configured regular
// expression.
func (m HeaderRegexpMatcher) Match(c RequestContext) bool {
	values := c.Request().Header[m.canonicalKey]
	if len(values) == 0 {
		return false
	}
	return slices.ContainsFunc(values, m.regex.MatchString)
}

// Equal reports whether matcher is a [HeaderRegexpMatcher] with the same key and regular expression source.
func (m HeaderRegexpMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(HeaderRegexpMatcher)
	if !ok {
		return false
	}
	return m.canonicalKey == om.canonicalKey && m.regex.String() == om.regex.String()
}

// String returns a textual representation of the matcher in the form "hx:key=expr".
func (m HeaderRegexpMatcher) String() string {
	return "hx:" + m.canonicalKey + "=" + m.Value()
}

// MatchClientIP returns a [ClientIPMatcher] that matches when the resolved client IP belongs to the given CIDR range.
// The ip parameter accepts both single IP addresses (e.g., "192.168.1.1") and CIDR notation (e.g., "192.168.1.0/24").
// An invalid value returns an error. The client IP is resolved via the configured [ClientIPResolver]; without one,
// the matcher always fails. See [WithClientIPMatcher] for the option counterpart and [WithClientIPResolver] to
// configure the resolver.
func MatchClientIP(ip string) (ClientIPMatcher, error) {
	prefix, err := netutil.ParsePrefix(ip)
	if err != nil {
		return ClientIPMatcher{}, err
	}
	return ClientIPMatcher{
		prefix: prefix,
	}, nil
}

// ClientIPMatcher matches a request when the resolved client IP belongs to the configured CIDR range.
type ClientIPMatcher struct {
	prefix netip.Prefix
}

// Prefix returns the network range tested by this matcher.
func (m ClientIPMatcher) Prefix() netip.Prefix {
	return m.prefix
}

// Match reports whether the resolved client IP belongs to the configured CIDR range. It returns false if the resolver
// returns an error or no resolver is configured. The resolved address is unmapped and stripped of any zone before
// the range check.
func (m ClientIPMatcher) Match(c RequestContext) bool {
	addr, err := c.ClientIP()
	if err != nil {
		return false
	}
	return m.prefix.Contains(addr.Unmap().WithZone(""))
}

// Equal reports whether matcher is a [ClientIPMatcher] with the same network range.
func (m ClientIPMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(ClientIPMatcher)
	if !ok {
		return false
	}
	return m.prefix == om.prefix
}

// String returns a textual representation of the matcher in the form "ip:CIDR".
func (m ClientIPMatcher) String() string {
	return "ip:" + m.prefix.String()
}

// MatchScheme returns a [SchemeMatcher] that matches when the request connection scheme equals the given value. Only
// "http" and "https" are accepted (case-insensitive); any other value returns an error. The scheme is derived solely
// from the TLS state of the connection between the client and the server, ignoring r.URL.Scheme to prevent spoofing
// via HTTP/1.1 absolute-form requests. See [WithSchemeMatcher] for the option counterpart.
func MatchScheme(scheme string) (SchemeMatcher, error) {
	s := strings.ToLower(scheme)
	if s != "http" && s != "https" {
		return SchemeMatcher{}, errors.New(`scheme must be "http" or "https"`)
	}
	return SchemeMatcher{scheme: s}, nil
}

// SchemeMatcher matches a request based on the connection scheme ("http" or "https"), derived from the TLS state of
// the connection. Behind a TLS-terminating reverse proxy, the matcher reflects the proxy-to-server hop, not the
// original client connection.
type SchemeMatcher struct {
	scheme string
}

// Scheme returns the scheme tested by this matcher ("http" or "https").
func (m SchemeMatcher) Scheme() string {
	return m.scheme
}

// Match reports whether the request's connection scheme equals the configured one. The scheme is derived from
// [http.Request.TLS]: "https" when set, "http" otherwise.
func (m SchemeMatcher) Match(c RequestContext) bool {
	isHTTPS := c.Request().TLS != nil
	return isHTTPS == (m.scheme == "https")
}

// Equal reports whether matcher is a [SchemeMatcher] with the same scheme.
func (m SchemeMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(SchemeMatcher)
	if !ok {
		return false
	}
	return m.scheme == om.scheme
}

// String returns a textual representation of the matcher in the form "s:scheme".
func (m SchemeMatcher) String() string {
	return "s:" + m.scheme
}
