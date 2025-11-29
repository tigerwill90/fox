// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"cmp"
	"errors"
	"log/slog"
	"time"
)

// Keys for "built-in" logger attribute for the logger middleware.
const (
	// LoggerStatusKey is the key used by the built-in logger middleware for the HTTP response status code
	// when the log method is called. The associated [slog.Value] is a string.
	LoggerStatusKey = "status"
	// LoggerMethodKey is the key used by the built-in logger middleware for the HTTP request method.
	// The associated [slog.Value] is a string.
	LoggerMethodKey = "method"
	// LoggerHostKey is the key used by the built-in logger middleware for the request host.
	// The associated [slog.Value] is a string.
	LoggerHostKey = "host"
	// LoggerPathKey is the key used by the built-in logger middleware for the request path.
	// The associated [slog.Value] is a string.
	LoggerPathKey = "path"
	// LoggerLatencyKey is the key used by the built-in logger middleware for the request processing duration.
	// The associated [slog.Value] is a time.Duration.
	LoggerLatencyKey = "latency"
	// LoggerSizeKey is the key used by the built-in logger middleware for the response body size.
	// The associated [slog.Value] is an int.
	LoggerSizeKey = "size"
	// LoggerLocationKey is the key used by the built-in logger middleware for redirect location header.
	// The associated [slog.Value] is a string.
	LoggerLocationKey = "location"
)

// Logger returns a middleware that logs request information using the provided [slog.Handler].
// It logs details such as the remote or client IP, HTTP method, request path, status code and latency.
// Status codes are logged at different levels: 2xx at INFO, 3xx at DEBUG (with Location header if present),
// 4xx at WARN, and 5xx at ERROR.
func Logger(handler slog.Handler) MiddlewareFunc {
	log := slog.New(handler)
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			start := time.Now()
			next(c)
			latency := time.Since(start)

			req := c.Request()
			lvl := level(c.Writer().Status())
			var location string
			if lvl.Level() == slog.LevelDebug {
				location = c.Writer().Header().Get(HeaderLocation)
			}

			var ipStr string
			ip, err := c.ClientIP()
			if err == nil {
				ipStr = ip.String()
			} else if errors.Is(err, ErrNoClientIPResolver) {
				ipStr = c.RemoteIP().String()
			} else {
				ipStr = "unknown"
			}

			l := log.With(
				slog.Int(LoggerStatusKey, c.Writer().Status()),
				slog.String(LoggerMethodKey, c.Method()),
				slog.String(LoggerHostKey, c.Host()),
				slog.String(LoggerPathKey, cmp.Or(req.URL.RawPath, req.URL.Path)),
				slog.Int(LoggerSizeKey, c.Writer().Size()),
				slog.Duration(LoggerLatencyKey, latency),
			)
			if location == "" {
				l.Log(
					req.Context(),
					lvl,
					ipStr,
				)
				return
			}

			l.LogAttrs(
				req.Context(),
				lvl,
				ipStr,
				slog.String(LoggerLocationKey, location),
			)
		}
	}
}

func level(status int) slog.Level {
	switch {
	case status >= 200 && status < 300:
		return slog.LevelInfo
	case status >= 300 && status < 400:
		return slog.LevelDebug
	case status >= 400 && status < 500:
		return slog.LevelWarn
	case status >= 500:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
