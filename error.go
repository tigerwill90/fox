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
// It contains the HTTP method, the route being registered, and the existing route
// that caused the conflict.
type RouteConflictError struct {
	// New is the route that was being registered when the conflict was detected.
	New *Route
	// Method is the HTTP method for which the conflict occurred.
	Method string
	// Conflicts contains the previously registered routes that conflict with New.
	Conflicts []*Route
}

func (e *RouteConflictError) Error() string {
	var sb strings.Builder
	sb.WriteString("route already registered: new route ")
	sb.WriteString(e.Method)
	sb.WriteByte(' ')
	sb.WriteString(e.New.pattern)
	sb.WriteString(" conflicts with ")

	// A RouteConflictError as always at least one conflicting route.
	first := e.Conflicts[0].pattern
	sb.WriteString(first)
	for _, route := range e.Conflicts[1:] {
		sb.WriteString("; ")
		sb.WriteString(route.pattern)
	}

	return sb.String()
}

// Unwrap returns the sentinel value [ErrRouteConflict].
func (e *RouteConflictError) Unwrap() error {
	return ErrRouteExist
}

type RouteNameConflictError struct {
	New      *Route
	Conflict *Route
	Method   string
}

func (e *RouteNameConflictError) Error() string {
	return fmt.Sprintf("%s: new route name '%s' conflict with route at %s %s", ErrRouteNameExist, e.New.name, e.Method, e.Conflict.pattern)
}

func (e *RouteNameConflictError) Unwrap() error {
	return ErrRouteNameExist
}
