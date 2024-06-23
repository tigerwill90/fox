package fox

import (
	netcontext "context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"
)

const (
	maxBufferSize     = 16 << 10 // 16384
	initialBufferSize = 1024
)

var _ slog.Handler = (*SlogHandler)(nil)

var logBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, initialBufferSize)
		return &b
	},
}

var defaultLogger = NewSlogHandler(os.Stdout, WithLevel(slog.LevelDebug))

func freeBuf(b *[]byte) {
	if cap(*b) <= maxBufferSize {
		*b = (*b)[:0]
		logBufPool.Put(b)
	}
}

type slogConfig struct {
	timeFormat string
	level      slog.Leveler
	source     bool
}

func defaultSlogConfig() *slogConfig {
	return &slogConfig{
		timeFormat: fmt.Sprintf("%s %s", time.DateOnly, time.TimeOnly),
		level:      slog.LevelInfo,
	}
}

type SlogOption interface {
	slogApply(*slogConfig)
}

type slogOptionFunc func(*slogConfig)

func WithLevel(level slog.Leveler) SlogOption {
	return slogOptionFunc(func(c *slogConfig) {
		c.level = level
	})
}

func WithTimeFormat(format string) SlogOption {
	return slogOptionFunc(func(c *slogConfig) {
		if format != "" {
			c.timeFormat = format
		}
	})
}

func WithShowSource(enable bool) SlogOption {
	return slogOptionFunc(func(c *slogConfig) {
		c.source = enable
	})
}

func (o slogOptionFunc) slogApply(c *slogConfig) {
	o(c)
}

type GroupOrAttrs struct {
	attr  slog.Attr
	group string
}

type SlogHandler struct {
	w         io.Writer
	mu        *sync.Mutex
	lvl       slog.Leveler
	cfg       *slogConfig
	groupAttr []GroupOrAttrs
}

func NewSlogHandler(w io.Writer, opts ...SlogOption) *SlogHandler {
	cfg := defaultSlogConfig()
	for _, opt := range opts {
		opt.slogApply(cfg)
	}

	return &SlogHandler{
		w:         w,
		mu:        &sync.Mutex{},
		lvl:       cfg.level,
		cfg:       cfg,
		groupAttr: make([]GroupOrAttrs, 0),
	}
}

func (h *SlogHandler) Enabled(_ netcontext.Context, level slog.Level) bool {
	return level >= h.lvl.Level()
}

func (h *SlogHandler) Handle(_ netcontext.Context, record slog.Record) error {
	bufp := logBufPool.Get().(*[]byte)
	buf := *bufp

	defer func() {
		*bufp = buf
		freeBuf(bufp)
	}()

	buf = append(buf, "[FOX] "...)

	if !record.Time.IsZero() {
		buf = append(buf, faint...)
		buf = append(buf, record.Time.Format(h.cfg.timeFormat)...)
		buf = append(buf, normalIntensity...)
		buf = append(buf, " "...)
	}

	// Write level with appropriate formatting and color.
	// Also append right padding depending on the log level.
	buf = append(buf, "| "...)
	switch record.Level {
	case slog.LevelInfo:
		buf = append(buf, fgGreen...)
		buf = append(buf, record.Level.String()...)
		buf = append(buf, " "...)
	case slog.LevelError:
		buf = append(buf, fgRed...)
		buf = append(buf, record.Level.String()...)
	case slog.LevelWarn:
		buf = append(buf, fgYellow...)
		buf = append(buf, record.Level.String()...)
		buf = append(buf, " "...)
	case slog.LevelDebug:
		buf = append(buf, fgMagenta...)
		buf = append(buf, record.Level.String()...)
	}

	buf = append(buf, reset...)
	buf = append(buf, " | "...)

	var senti error

	// If configured, write the source file and line information.
	for h.cfg.source {
		buf = append(buf, fgBlue...)
		buf = append(buf, underline...)

		frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()

		dir, file := filepath.Split(frame.File)

		rootDir, err := os.Getwd()
		if err != nil {
			senti = fmt.Errorf("failed to get the root directory: %w", err)

			break
		}

		// Trim the root directory prefix to get the relative directory of the source file
		relativeDir, err := filepath.Rel(rootDir, filepath.Dir(dir))
		if err != nil {
			senti = fmt.Errorf("failed to get the relative directory: %w", err)

			buf = append(buf, file...)
			buf = append(buf, ":"...)
			buf = strconv.AppendInt(buf, int64(frame.Line), 10)
			buf = append(buf, reset...)
			buf = append(buf, " "...)

			break
		}

		buf = append(buf, filepath.Join(relativeDir, file)...)
		buf = append(buf, ":"...)
		buf = strconv.AppendInt(buf, int64(frame.Line), 10)
		buf = append(buf, reset...)
		buf = append(buf, " | "...)

		break
	}

	// Write the log message.
	buf = append(buf, record.Message...)
	buf = append(buf, " | "...)

	lastGroup := ""
	for _, goa := range h.groupAttr {
		switch {
		case goa.group != "":
			lastGroup += goa.group + "."
		default:
			attr := goa.attr
			if lastGroup != "" {
				attr.Key = lastGroup + attr.Key
			}

			buf = appendAttr(buf, attr)
		}
	}

	// If there are additional attributes, append them to the log record.
	if record.NumAttrs() > 0 {
		record.Attrs(func(attr slog.Attr) bool {
			if lastGroup != "" {
				attr.Key = lastGroup + attr.Key
			}
			buf = appendAttr(buf, attr)

			return true
		})
	}

	// Replace the latest space by an EOL.
	buf[len(buf)-1] = '\n'

	// Lock the handler for writing and unlock once finished.
	h.mu.Lock()
	defer h.mu.Unlock()

	// Write the buffer to the writer.
	if _, err := h.w.Write(buf); err != nil {
		return fmt.Errorf("failed to write buffer: %w", err)
	}

	return senti
}

func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]GroupOrAttrs, len(attrs))
	for i, attr := range attrs {
		newAttrs[i] = GroupOrAttrs{attr: attr}
	}

	return &SlogHandler{
		w:         h.w,
		mu:        h.mu,
		lvl:       h.lvl,
		cfg:       h.cfg,
		groupAttr: append(h.groupAttr, newAttrs...),
	}
}

func (h *SlogHandler) WithGroup(name string) slog.Handler {
	return &SlogHandler{
		w:         h.w,
		mu:        h.mu,
		lvl:       h.lvl,
		cfg:       h.cfg,
		groupAttr: append(h.groupAttr, GroupOrAttrs{group: name}),
	}
}

// appendAttr appends the attribute to the buffer.
func appendAttr(buf []byte, attr slog.Attr) []byte {
	// Resolve the Attr's value before doing anything else.
	attr.Value = attr.Value.Resolve()

	// Ignore empty Attrs.
	if attr.Equal(slog.Attr{}) {
		return buf
	}

	buf = append(buf, faint...)
	buf = append(buf, bold...)

	buf = append(buf, attr.Key...)
	buf = append(buf, "="...)
	buf = append(buf, normalIntensity...)

	var unquoted bool
	switch v := attr.Value.Any().(type) {
	case colorizedValue:
		buf = append(buf, v.color.escapeCode...)
		unquoted = v.color.unquoted
	default:
		buf = append(buf, fgCyan...)
	}

	if !unquoted {
		buf = strconv.AppendQuote(buf, attr.Value.String())
	} else {
		buf = append(buf, attr.Value.String()...)
	}

	buf = append(buf, reset...)
	buf = append(buf, " "...)

	return buf
}

func LoggerWithHandler(handler slog.Handler) MiddlewareFunc {
	log := slog.New(handler)
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) {
			start := time.Now()
			next(c)
			latency := time.Since(start)
			req := c.Request()
			lvl, color := selectColorAndLevelFromStatus(c.Writer().Status())
			var location string
			if lvl.Level() == slog.LevelDebug {
				location = c.Writer().Header().Get(HeaderLocation)
			}

			if location == "" {
				log.LogAttrs(
					req.Context(),
					lvl,
					remoteAddr(req),
					colorizedAttr("status", " "+strconv.Itoa(c.Writer().Status())+" ", color),
					colorizedAttr("latency", roundLatency(latency).String(), selectColorFromLatency(latency)),
					colorizedAttr("method", " "+req.Method+" ", bgBlueColor),
					slog.String("path", c.Request().URL.String()),
				)
			} else {
				location = c.Writer().Header().Get(HeaderLocation)
				log.LogAttrs(
					req.Context(),
					lvl,
					remoteAddr(req),
					colorizedAttr("status", " "+strconv.Itoa(c.Writer().Status())+" ", color),
					colorizedAttr("latency", roundLatency(latency).String(), selectColorFromLatency(latency)),
					colorizedAttr("method", " "+req.Method+" ", bgBlueColor),
					slog.String("path", req.URL.String()),
					colorizedAttr("location", location, fgYellowColor),
				)
			}

		}
	}
}

func Logger() MiddlewareFunc {
	return LoggerWithHandler(defaultLogger)
}

func selectColorAndLevelFromStatus(status int) (slog.Level, colorValue) {
	switch {
	case status >= 200 && status < 300:
		return slog.LevelInfo, bgBlueColor
	case status >= 300 && status < 400:
		return slog.LevelDebug, bgMagentaColor
	case status >= 400 && status < 500:
		return slog.LevelWarn, bgYellowColor
	case status >= 500:
		return slog.LevelError, bgRedColor
	default:
		return slog.LevelInfo, bgBlueColor
	}
}

func selectColorFromLatency(d time.Duration) colorValue {
	if d < 100*time.Millisecond {
		return fgGreenColor
	} else if d < 500*time.Millisecond {
		return fgYellowColor
	} else {
		return fgRedColor
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
