// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrRouteNotFound           = errors.New("route not found")
	ErrRouteConflict           = errors.New("route conflict")
	ErrRouteNameExist          = errors.New("route name already registered")
	ErrInvalidRoute            = errors.New("invalid route")
	ErrDiscardedResponseWriter = errors.New("discarded response writer")
	ErrInvalidRedirectCode     = errors.New("invalid redirect code")
	ErrNoClientIPResolver      = errors.New("no client ip resolver")
	ErrReadOnlyTxn             = errors.New("write on read-only transaction")
	ErrSettledTxn              = errors.New("transaction settled")
	ErrParamKeyTooLarge        = errors.New("parameter key too large")
	ErrTooManyParams           = errors.New("too many params")
	ErrTooManyMatchers         = errors.New("too many matchers")
	ErrRegexpNotAllowed        = errors.New("regexp not allowed")
	ErrInvalidConfig           = errors.New("invalid config")
	ErrInvalidMatcher          = errors.New("invalid matcher")
)

// RouteConflictError represents a conflict that occurred during route registration.
// It contains the route being registered, and the existing routes that caused the conflict.
type RouteConflictError struct {
	// New is the route that was being registered when the conflict was detected.
	New *Route
	// Conflicts contains the previously registered routes that conflict with New.
	Conflicts []*Route
	// IsShadowed indicate that the New route shadow other routes.
	IsShadowed bool
}

func (e *RouteConflictError) Error() string {
	sb := new(strings.Builder)
	sb.WriteString("route conflict: new route\n")
	routef(sb, e.New, 4)

	if e.IsShadowed {
		sb.WriteString("\nis shadowed by")
	} else {
		sb.WriteString("\nconflicts with")
	}

	for _, conflict := range e.Conflicts {
		sb.WriteByte('\n')
		routef(sb, conflict, 4)
		// TODO (with redirect slash or with relaxed slash)
	}

	return sb.String()
}

// Unwrap returns the sentinel value [ErrRouteConflict].
func (e *RouteConflictError) Unwrap() error {
	return ErrRouteConflict
}

// RouteNameConflictError represents a conflict that occurred during route name registration.
// It contains the route being registered, and the existing route that caused the conflict.
type RouteNameConflictError struct {
	// New is the route that was being registered when the conflict was detected.
	New *Route
	// Conflict is the previously registered route that conflict with New.
	Conflict *Route
}

func (e *RouteNameConflictError) Error() string {
	sb := new(strings.Builder)
	sb.WriteString("route name already registered: new route\n")
	routef(sb, e.New, 4)
	sb.WriteString("\nconflicts with\n")
	routef(sb, e.Conflict, 4)
	return sb.String()
}

// Unwrap returns the sentinel value [ErrRouteNameExist].
func (e *RouteNameConflictError) Unwrap() error {
	return ErrRouteNameExist
}

func routef(sb *strings.Builder, route *Route, pad int) {
	sb.WriteString(strings.Repeat(" ", pad))
	sb.WriteString("method:")
	if len(route.methods) > 0 {
		first := route.methods[0]
		sb.WriteString(first)
		for _, method := range route.methods[1:] {
			sb.WriteByte(',')
			sb.WriteString(method)
		}
	} else {
		sb.WriteString("*")
	}

	sb.WriteString(" pattern:")
	sb.WriteString(route.pattern)

	if route.name != "" {
		sb.WriteString(" name:")
		sb.WriteString(route.name)
	}

	size := sb.Len()
	for _, matcher := range route.matchers {
		if m, ok := matcher.(fmt.Stringer); ok {
			if sb.Len() > size {
				sb.WriteByte(',')
			}
			if size == sb.Len() {
				sb.WriteString(" matchers:{")
			}
			sb.WriteString(m.String())
		}
	}
	if sb.Len() > size {
		sb.WriteByte('}')
	}
}

func newRouteNotFoundError(route *Route) error {
	if len(route.methods) > 0 {
		return fmt.Errorf("%w: route [%s] %s is not registered", ErrRouteNotFound, strings.Join(route.methods, ", "), route.pattern)
	}
	return fmt.Errorf("%w: route %s is not registered", ErrRouteNotFound, route.pattern)
}
