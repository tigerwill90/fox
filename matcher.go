package fox

import (
	"bytes"
	"net"
	"regexp"
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
	// As attempts to convert the matcher to the type pointed to by target.
	As(target any) bool
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

func (m QueryMatcher) As(target any) bool {
	switch x := target.(type) {
	case *QueryMatcher:
		*x = m
	default:
		return false
	}
	return true
}

type QueryRegexpMatcher struct {
	regex *regexp.Regexp
	key   string
}

func (m QueryRegexpMatcher) Key() string {
	return m.key
}

func (m QueryRegexpMatcher) Regex() *regexp.Regexp {
	return m.regex
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

func (m QueryRegexpMatcher) As(target any) bool {
	switch x := target.(type) {
	case *QueryRegexpMatcher:
		*x = m
	default:
		return false
	}
	return true
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

func (m HeaderMatcher) As(target any) bool {
	switch x := target.(type) {
	case *HeaderMatcher:
		*x = m
	default:
		return false
	}
	return true
}

type HeaderRegexpMatcher struct {
	regex        *regexp.Regexp
	canonicalKey string
}

func (m HeaderRegexpMatcher) Key() string {
	return m.canonicalKey
}

func (m HeaderRegexpMatcher) Regex() *regexp.Regexp {
	return m.regex
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

func (m HeaderRegexpMatcher) As(target any) bool {
	switch x := target.(type) {
	case *HeaderRegexpMatcher:
		*x = m
	default:
		return false
	}
	return true
}

type ClientIpMatcher struct {
	ipNet *net.IPNet
}

func (m ClientIpMatcher) IPNet() *net.IPNet {
	return m.ipNet
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

func (m ClientIpMatcher) As(target any) bool {
	switch x := target.(type) {
	case *ClientIpMatcher:
		*x = m
	default:
		return false
	}
	return true
}
