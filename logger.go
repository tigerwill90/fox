// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"log/slog"
	"time"

	"github.com/tigerwill90/fox/internal/slogpretty"
)

// LoggerWithHandler returns a middleware that logs request information using the provided [slog.Handler].
// It logs details such as the remote or client IP, HTTP method, request path, status code and latency.
func LoggerWithHandler(handler slog.Handler) MiddlewareFunc {
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

			if location == "" {
				log.LogAttrs(
					req.Context(),
					lvl,
					ipStr,
					slog.Int("status", c.Writer().Status()),
					slog.String("method", c.Method()),
					slog.String("host", c.Host()),
					slog.String("path", c.Path()),
					slog.Duration("latency", roundLatency(latency)),
				)
			} else {
				log.LogAttrs(
					req.Context(),
					lvl,
					ipStr,
					slog.Int("status", c.Writer().Status()),
					slog.String("method", c.Method()),
					slog.String("host", c.Host()),
					slog.String("path", c.Path()),
					slog.Duration("latency", roundLatency(latency)),
					slog.String("location", location),
				)
			}
		}
	}
}

// Logger returns a middleware that logs request information to [os.Stdout] or [os.Stderr] (for ERROR level).
// It logs details such as the remote or client IP, HTTP method, request path, status code and latency.
func Logger() MiddlewareFunc {
	return LoggerWithHandler(slogpretty.DefaultHandler)
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

func roundLatency(d time.Duration) time.Duration {
	switch {
	case d < 1*time.Microsecond:
		return d.Round(100 * time.Nanosecond)
	case d < 1*time.Millisecond:
		return d.Round(10 * time.Microsecond)
	case d < 10*time.Millisecond:
		return d.Round(100 * time.Microsecond)
	case d < 100*time.Millisecond:
		return d.Round(1 * time.Millisecond)
	case d < 1*time.Second:
		return d.Round(10 * time.Millisecond)
	case d < 10*time.Second:
		return d.Round(100 * time.Millisecond)
	default:
		return d.Round(1 * time.Second)
	}
}
