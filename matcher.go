package fox

// Matcher evaluates if an HTTP request satisfies specific conditions.
type Matcher interface {
	// Match evaluates if the [RequestContext] satisfies this matcher.
	Match(c RequestContext) bool
	// Equal checks if this matcher is structurally equivalent to another.
	Equal(other Matcher) bool
}

type QueryMatcher struct {
	Key   string
	Value string
}

func (m QueryMatcher) Match(c RequestContext) bool {
	// Uses cached query params from ctx.cachedQuery
	return c.QueryParam(m.Key) == m.Value
}

func (m QueryMatcher) Equal(other Matcher) bool {
	om, ok := other.(QueryMatcher)
	if !ok {
		return false
	}
	return m.Key == om.Key && m.Value == om.Value
}

type HeaderMatcher struct {
	Key   string
	Value string
}

func (m HeaderMatcher) Match(c RequestContext) bool {
	return c.Header(m.Key) == m.Value
}

func (m HeaderMatcher) Equal(other Matcher) bool {
	om, ok := other.(HeaderMatcher)
	if !ok {
		return false
	}
	return m.Key == om.Key && m.Value == om.Value
}
