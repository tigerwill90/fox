// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/tigerwill90/fox/blob/master/LICENSE.txt.

package fox

import (
	"errors"
	"github.com/tigerwill90/fox/internal/slogpretty"
	"log/slog"
	"time"
)

// LoggerWithHandler returns middleware that logs request information using the provided slog.Handler.
// It logs details such as the remote IP, HTTP method, request path, status code and latency.
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
			} else if errors.Is(err, ErrNoClientIPStrategy) {
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
					slog.String("method", req.Method),
					slog.String("path", c.Request().URL.String()),
					slog.Duration("latency", roundLatency(latency)),
				)
			} else {
				location = c.Writer().Header().Get(HeaderLocation)
				log.LogAttrs(
					req.Context(),
					lvl,
					ipStr,
					slog.Int("status", c.Writer().Status()),
					slog.String("method", req.Method),
					slog.String("path", c.Request().URL.String()),
					slog.Duration("latency", roundLatency(latency)),
					slog.String("location", location),
				)
			}

		}
	}
}

// Logger returns middleware that logs request information to os.Stdout and os.Stderr.
// It logs details such as the remote IP, HTTP method, request path, status code and latency.
func Logger() MiddlewareFunc {
	return LoggerWithHandler(slogpretty.Handler)
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
