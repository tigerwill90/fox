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
	ErrRouteConflict           = errors.New("route conflict")
	ErrInvalidRoute            = errors.New("invalid route")
	ErrDiscardedResponseWriter = errors.New("discarded response writer")
	ErrInvalidRedirectCode     = errors.New("invalid redirect code")
	ErrNoClientIPResolver      = errors.New("no client ip resolver")
	ErrReadOnlyTxn             = errors.New("write on read-only transaction")
	ErrSettledTxn              = errors.New("transaction settled")
)

// RouteConflictError is a custom error type used to represent conflicts when
// registering or updating routes in the router. It holds information about the
// conflicting method, path, and the matched routes that caused the conflict.
type RouteConflictError struct {
	err      error
	Method   string
	Path     string
	Matched  []string
	isUpdate bool
}

func newConflictErr(method, path string, matched []string) *RouteConflictError {
	return &RouteConflictError{
		Method:  method,
		Path:    path,
		Matched: matched,
		err:     ErrRouteConflict,
	}
}

// Error returns a formatted error message for the [RouteConflictError].
func (e *RouteConflictError) Error() string {
	if !e.isUpdate {
		return e.insertError()
	}
	return e.updateError()
}

func (e *RouteConflictError) insertError() string {
	return fmt.Sprintf("%s: new route [%s] %s conflicts with %s", e.err, e.Method, e.Path, strings.Join(e.Matched, ", "))
}

func (e *RouteConflictError) updateError() string {
	return fmt.Sprintf("wildcard conflict: updated route [%s] %s conflicts with %s", e.Method, e.Path, strings.Join(e.Matched, ", "))

}

// Unwrap returns the sentinel value [ErrRouteConflict].
func (e *RouteConflictError) Unwrap() error {
	return e.err
}
