package fox

import (
	"bytes"
	"net"
)

// Matcher evaluates if an HTTP request satisfies specific conditions.
type Matcher interface {
	// Match evaluates if the [RequestContext] satisfies this matcher.
	Match(c RequestContext) bool
	// Equal checks if this matcher is structurally equivalent to m.
	Equal(m Matcher) bool
	As(target any) bool
}

type QueryMatcher struct {
	Key   string
	Value string
}

func (m QueryMatcher) Match(c RequestContext) bool {
	return c.QueryParam(m.Key) == m.Value
}

func (m QueryMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(QueryMatcher)
	if !ok {
		return false
	}
	return m.Key == om.Key && m.Value == om.Value
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

type HeaderMatcher struct {
	CanonicalKey string
	Value        string
}

func (m HeaderMatcher) Match(c RequestContext) bool {
	values := c.Request().Header[m.CanonicalKey]
	if len(values) == 0 {
		return false
	}
	return values[0] == m.Value
}

func (m HeaderMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(HeaderMatcher)
	if !ok {
		return false
	}
	return m.CanonicalKey == om.CanonicalKey && m.Value == om.Value
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

type ClientIpMatcher struct {
	IPNet *net.IPNet
}

func (m ClientIpMatcher) Match(c RequestContext) bool {
	addr, err := c.ClientIP()
	if err != nil {
		return false
	}
	return m.IPNet.Contains(addr.IP)
}

func (m ClientIpMatcher) Equal(matcher Matcher) bool {
	om, ok := matcher.(ClientIpMatcher)
	if !ok {
		return false
	}
	return m.IPNet.IP.Equal(om.IPNet.IP) && bytes.Equal(m.IPNet.Mask, om.IPNet.Mask)
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
