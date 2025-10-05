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
	ErrInvalidRoute            = errors.New("invalid route")
	ErrDiscardedResponseWriter = errors.New("discarded response writer")
	ErrInvalidRedirectCode     = errors.New("invalid redirect code")
	ErrNoClientIPResolver      = errors.New("no client ip resolver")
	ErrReadOnlyTxn             = errors.New("write on read-only transaction")
	ErrSettledTxn              = errors.New("transaction settled")
	ErrParamKeyTooLarge        = errors.New("parameter key too large")
	ErrTooManyParams           = errors.New("too many params")
	ErrInvalidConfig           = errors.New("invalid config")
)

// RouteConflict represents a conflict that occurred during route registration.
// It contains the HTTP method, the route being registered, and the existing route
// that caused the conflict.
type RouteConflict struct {
	// Method is the HTTP method for which the conflict occurred.
	Method string
	// New is the route that was being registered when the conflict was detected.
	New *Route
	// Existing is the previously registered route that conflicts with New.
	Existing *Route
}

func (e *RouteConflict) Error() string {
	return fmt.Sprintf("%s: new route %s %s conflict with %s", ErrRouteExist, e.Method, e.New.pattern, e.Existing.pattern)
}

// Unwrap returns the sentinel value [ErrRouteConflict].
func (e *RouteConflict) Unwrap() error {
	return ErrRouteExist
}
