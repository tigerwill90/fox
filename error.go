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
	ErrRouteExist              = errors.New("route already registered")
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
}

func (e *RouteConflictError) Error() string {
	var sb strings.Builder
	sb.WriteString("route already registered: new route ")

	if len(e.New.methods) > 0 {
		first := e.New.methods[0]
		sb.WriteByte('[')
		sb.WriteString(first)
		for _, method := range e.New.methods[1:] {
			sb.WriteString(", ")
			sb.WriteString(method)
		}
		sb.WriteString("] ")
	}

	sb.WriteString(e.New.pattern)
	sb.WriteString(" conflicts with ")

	for _, route := range e.Conflicts {
		sb.WriteByte('\n')
		sb.WriteString(route.pattern)
		if len(route.methods) > 0 {
			first := route.methods[0]
			sb.WriteString(" [")
			sb.WriteString(first)
			for _, method := range route.methods[1:] {
				sb.WriteString(", ")
				sb.WriteString(method)
			}
			sb.WriteByte(']')
		}
	}

	return sb.String()
}

// Unwrap returns the sentinel value [ErrRouteConflict].
func (e *RouteConflictError) Unwrap() error {
	return ErrRouteExist
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
	var sb strings.Builder
	sb.WriteString("route name already registered: new route name ")
	sb.WriteByte('\'')
	sb.WriteString(e.New.name)
	sb.WriteString("' conflicts with route at ")

	if len(e.Conflict.methods) > 0 {
		first := e.Conflict.methods[0]
		sb.WriteByte('[')
		sb.WriteString(first)
		for _, method := range e.Conflict.methods[1:] {
			sb.WriteString(", ")
			sb.WriteString(method)
		}
		sb.WriteString("] ")
	}

	sb.WriteString(e.Conflict.pattern)

	return sb.String()
}

func (e *RouteNameConflictError) Unwrap() error {
	return ErrRouteNameExist
}

func newRouteNotFoundError(route *Route) error {
	if len(route.methods) > 0 {
		return fmt.Errorf("%w: route [%s] %s is not registered", ErrRouteNotFound, strings.Join(route.methods, ", "), route.pattern)
	}
	return fmt.Errorf("%w: route %s is not registered", ErrRouteNotFound, route.pattern)
}
