package fox

import "fmt"

type ConflictError struct {
	method     string
	path       string
	matching   []string
	isWildcard bool
	err        error
}

func newConflictErr(method, path string, matching []string, isWildcard bool) *ConflictError {
	return &ConflictError{
		method:     method,
		path:       path,
		matching:   matching,
		isWildcard: isWildcard,
		err:        ErrRouteConflict,
	}
}

func (e *ConflictError) Error() string {
	path := e.path
	if e.isWildcard {
		path += "*"
	}
	return fmt.Sprintf("route /%s %s is conflicting with %v", e.method, path, e.matching)
}

func (e *ConflictError) Unwrap() error {
	return e.err
}

func Must(err error) {
	if err != nil {
		panic(err)
	}
}
