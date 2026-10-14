// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var (
	ErrRouteNotFound           = errors.New("route not found")
	ErrInvalidRoute            = errors.New("invalid route")
	ErrDiscardedResponseWriter = errors.New("discarded response writer")
	ErrNoClientIPResolver      = errors.New("no client ip resolver")
	ErrReadOnlyTxn             = errors.New("write on read-only transaction")
	ErrSettledTxn              = errors.New("transaction settled")
	ErrTooManyMatchers         = errors.New("too many matchers")
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
	// isShadowed indicate that the New route shadow other routes.
	isShadowed bool
}

func (e *RouteConflictError) Error() string {
	sb := new(strings.Builder)
	sb.WriteString("route conflict: new route\n")
	routef(sb, e.New, 4, true)

	if e.isShadowed {
		if e.New.pattern.optionalCatchAll {
			sb.WriteString("\nis shadowed by")
		} else {
			sb.WriteString("\nwould shadow")
		}
	} else {
		sb.WriteString("\nconflicts with")
	}

	for _, conflict := range e.Conflicts {
		sb.WriteByte('\n')
		routef(sb, conflict, 4, true)
	}

	return sb.String()
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
	routef(sb, e.New, 4, true)
	sb.WriteString("\nconflicts with\n")
	routef(sb, e.Conflict, 4, true)
	return sb.String()
}

func newRouteNotFoundError(route *Route) error {
	sb := new(strings.Builder)
	sb.WriteString("route\n")
	routef(sb, route, 4, false)
	sb.WriteString("\nis not registered")
	return fmt.Errorf("%w: %s", ErrRouteNotFound, sb.String())
}

// PatternError represents an error that occurred during route pattern parsing.
// It contains the pattern, and the position of the offending segment.
type PatternError struct {
	err     error  // wrapped error
	Pattern string // provided pattern
	Type    string // hostname | path
	Reason  string // syntax | parameter | regexp | constraint
	Hint    string // hint
	Start   int    // start offset of the offending segment
	End     int    // end offset of the offending segment
}

// Unwrap returns the underlying error, if any.
func (e *PatternError) Unwrap() error {
	return e.err
}

// Error returns a human-readable error message with a visual pointer to the offending segment.
func (e *PatternError) Error() string {
	var sb strings.Builder
	sb.WriteString("pattern: ")
	if e.Type != "" {
		sb.WriteString(e.Type)
		sb.WriteString(": ")
	}
	sb.WriteString(e.Reason)
	sb.WriteString(": ")
	sb.WriteString(e.Hint)
	if e.Pattern != "" {
		sb.WriteByte('\n')
		sb.WriteString("      ")
		sb.WriteString(e.Pattern)
		sb.WriteByte('\n')
		sb.WriteString("      ")

		start := min(max(e.Start, 0), len(e.Pattern))
		end := min(max(e.End, start), len(e.Pattern))
		sb.WriteString(strings.Repeat(" ", utf8.RuneCountInString(e.Pattern[:start])))
		sb.WriteString(strings.Repeat("^", max(utf8.RuneCountInString(e.Pattern[start:end]), 1)))
	}
	return sb.String()
}

func newPatternError(reason string, start, end int, msg string) *PatternError {
	return &PatternError{
		Reason: reason,
		Start:  start,
		End:    end,
		Hint:   msg,
	}
}
