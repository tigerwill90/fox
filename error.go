// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"fmt"
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
	// Existing is the previously registered route that conflicts with New.
	Existing *Route
	// Method is the HTTP method for which the conflict occurred.
	Method string
	// this is a name conflict
	isNameConflict bool
}

func (e *RouteConflictError) Error() string {
	if !e.isNameConflict {
		return fmt.Sprintf("%s: new route %s %s conflict with %s", ErrRouteExist, e.Method, e.New.pattern, e.Existing.pattern)
	}
	return fmt.Sprintf("%s: new route name %s '%s' conflict with route at %s", ErrRouteNameExist, e.Method, e.New.name, e.Existing.pattern)
}

// Unwrap returns the sentinel value [ErrRouteConflict].
func (e *RouteConflictError) Unwrap() error {
	if !e.isNameConflict {
		return ErrRouteExist
	}
	return ErrRouteNameExist
}
