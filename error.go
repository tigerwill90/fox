package fox

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrRouteNotFound = errors.New("route not found")
	ErrRouteExist    = errors.New("route already registered")
	ErrRouteConflict = errors.New("route conflict")
	ErrInvalidRoute  = errors.New("invalid route")
	ErrSkipMethod    = errors.New("skip method")
)

type RouteConflictError struct {
	err     error
	Method  string
	Path    string
	Matched []string
}

func newConflictErr(method, path, catchAllKey string, matched []string) *RouteConflictError {
	if catchAllKey != "" {
		path += "*" + catchAllKey
	}
	return &RouteConflictError{
		Method:  method,
		Path:    path,
		Matched: matched,
		err:     ErrRouteConflict,
	}
}

func (e *RouteConflictError) Error() string {
	path := e.Path
	return fmt.Sprintf("new route [%s] %s conflicts with %s", e.Method, path, strings.Join(e.Matched, ", "))
}

func (e *RouteConflictError) Unwrap() error {
	return e.err
}
