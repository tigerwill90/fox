// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	ErrRouteNotFound           = errors.New("route not found")
	ErrRouteExist              = errors.New("route already registered")
	ErrRouteConflict           = errors.New("route conflict")
	ErrInvalidRoute            = errors.New("invalid route")
	ErrDiscardedResponseWriter = errors.New("discarded response writer")
	ErrInvalidRedirectCode     = errors.New("invalid redirect code")
)

type RouteConflictError struct {
	err      error
	Method   string
	Path     string
	Matched  []string
	isUpdate bool
}

func newConflictErr(method, path, catchAllKey string, matched []string) *RouteConflictError {
	if catchAllKey != "" {
		path += "*{" + catchAllKey + "}"
	}
	return &RouteConflictError{
		Method:  method,
		Path:    path,
		Matched: matched,
		err:     ErrRouteConflict,
	}
}

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

func (e *RouteConflictError) Unwrap() error {
	return e.err
}

// HTTPError represents an HTTP error with a status code (HTTPErrorCode)
// and an optional error message. If no error message is provided,
// the default error message for the status code will be used.
type HTTPError struct {
	Err  error
	Code int
}

// Error returns the error message associated with the HTTPError,
// or the default error message for the status code if none is provided.
func (e HTTPError) Error() string {
	if e.Err == nil {
		return http.StatusText(e.Code)
	}
	return e.Err.Error()
}

// NewHTTPError creates a new HTTPError with the given status code
// and an optional error message.
func NewHTTPError(code int, err ...error) HTTPError {
	var e error
	if len(err) > 0 {
		e = err[0]
	}
	return HTTPError{
		Code: code,
		Err:  e,
	}
}
