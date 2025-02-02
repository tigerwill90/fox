// The code in this package is derivative of https://gitlab.com/greyxor/slogor.
// Mount of this source code is governed by a MIT license that can be found
// at https://gitlab.com/greyxor/slogor/-/blob/main/LICENSE?ref_type=heads.

package slogpretty

import (
	"context"
	"fmt"
	"github.com/tigerwill90/fox/internal/ansi"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

const (
	maxBufferSize     = 16 << 10 // 16384
	initialBufferSize = 1024
)

var _ slog.Handler = (*Handler)(nil)

var logBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, initialBufferSize)
		return &b
	},
}

var (
	DefaultHandler = &Handler{
		We:  &lockedWriter{w: os.Stderr},
		Wo:  &lockedWriter{w: os.Stdout},
		Lvl: slog.LevelDebug,
		Goa: make([]GroupOrAttrs, 0),
	}
	timeFormat = fmt.Sprintf("%s %s", time.DateOnly, time.TimeOnly)
)

func freeBuf(b *[]byte) {
	if cap(*b) <= maxBufferSize {
		*b = (*b)[:0]
		logBufPool.Put(b)
	}
}

type GroupOrAttrs struct {
	attr  slog.Attr
	group string
}

type Handler struct {
	We  io.Writer
	Wo  io.Writer
	Lvl slog.Leveler
	Goa []GroupOrAttrs
}

func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.Lvl.Level()
}

func (h *Handler) Handle(_ context.Context, record slog.Record) error {
	bufp := logBufPool.Get().(*[]byte)
	buf := *bufp

	defer func() {
		*bufp = buf
		freeBuf(bufp)
	}()

	buf = append(buf, "[FOX] "...)

	if !record.Time.IsZero() {
		buf = append(buf, ansi.Faint...)
		buf = append(buf, record.Time.Format(timeFormat)...)
		buf = append(buf, ansi.NormalIntensity...)
		buf = append(buf, " "...)
	}

	// Write level with appropriate formatting and color.
	// Also append right padding depending on the log level.
	buf = append(buf, "| "...)
	switch record.Level {
	case slog.LevelInfo:
		buf = append(buf, ansi.FgGreen...)
		buf = append(buf, record.Level.String()...)
		buf = append(buf, " "...)
	case slog.LevelError:
		buf = append(buf, ansi.FgRed...)
		buf = append(buf, record.Level.String()...)
	case slog.LevelWarn:
		buf = append(buf, ansi.FgYellow...)
		buf = append(buf, record.Level.String()...)
		buf = append(buf, " "...)
	case slog.LevelDebug:
		buf = append(buf, ansi.FgMagenta...)
		buf = append(buf, record.Level.String()...)
	}

	buf = append(buf, ansi.Reset...)
	buf = append(buf, " | "...)
	// Write the log message.
	if record.Message == "unknown" {
		// special case if the ip cannot be found using the ClientIPResolver
		buf = append(buf, ansi.FgRed...)
		buf = append(buf, record.Message...)
		buf = append(buf, ansi.Reset...)
	} else {
		buf = append(buf, record.Message...)
	}
	buf = append(buf, " | "...)

	lastGroup := ""
	for _, goa := range h.Goa {
		switch {
		case goa.group != "":
			lastGroup += goa.group + "."
		default:
			attr := goa.attr
			if lastGroup != "" {
				attr.Key = lastGroup + attr.Key
			}

			buf = appendAttr(record.Level, buf, attr)
		}
	}

	// If there are additional attributes, append them to the log record.
	if record.NumAttrs() > 0 {
		record.Attrs(func(attr slog.Attr) bool {
			if lastGroup != "" {
				attr.Key = lastGroup + attr.Key
			}
			buf = appendAttr(record.Level, buf, attr)

			return true
		})
	}

	// Replace the latest space by an EOL.
	buf[len(buf)-1] = '\n'

	if record.Level >= slog.LevelError {
		if _, err := h.We.Write(buf); err != nil {
			return fmt.Errorf("failed to write buffer: %w", err)
		}
	} else {
		if _, err := h.Wo.Write(buf); err != nil {
			return fmt.Errorf("failed to write buffer: %w", err)
		}
	}

	return nil
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]GroupOrAttrs, len(attrs))
	for i, attr := range attrs {
		newAttrs[i] = GroupOrAttrs{attr: attr}
	}

	return &Handler{
		We:  h.We,
		Wo:  h.Wo,
		Lvl: h.Lvl,
		Goa: append(h.Goa, newAttrs...),
	}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{
		We:  h.We,
		Wo:  h.Wo,
		Lvl: h.Lvl,
		Goa: append(h.Goa, GroupOrAttrs{group: name}),
	}
}

// appendAttr appends the attribute to the buffer.
func appendAttr(level slog.Level, buf []byte, attr slog.Attr) []byte {
	// Resolve the Attr's value before doing anything else.
	attr.Value = attr.Value.Resolve()

	// Ignore empty Attrs.
	if attr.Equal(slog.Attr{}) {
		return buf
	}

	buf = append(buf, ansi.Faint...)
	buf = append(buf, ansi.Bold...)

	buf = append(buf, attr.Key...)
	buf = append(buf, "="...)
	buf = append(buf, ansi.NormalIntensity...)

	var addWhitespace bool
	switch attr.Key {
	case "method":
		buf = append(buf, ansi.BgBlue...)
		addWhitespace = true
	case "status":
		buf = append(buf, levelColor(level)...)
		addWhitespace = true
	case "location":
		buf = append(buf, ansi.FgYellow...)
	case "latency":
		buf = append(buf, latencyColor(attr.Value.Duration())...)
	case "error":
		buf = append(buf, ansi.FgRed...)
	default:
		buf = append(buf, ansi.FgCyan...)
	}

	if addWhitespace {
		buf = append(buf, " "+attr.Value.String()+" "...)
	} else {
		buf = append(buf, attr.Value.String()...)
	}
	buf = append(buf, ansi.Reset...)
	buf = append(buf, " "...)

	return buf
}

type lockedWriter struct {
	w io.Writer
	sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (n int, err error) {
	w.Lock()
	n, err = w.w.Write(p)
	w.Unlock()
	return
}

func levelColor(level slog.Level) string {
	switch level {
	case slog.LevelInfo:
		return ansi.BgBlue
	case slog.LevelWarn:
		return ansi.BgYellow
	case slog.LevelError:
		return ansi.BgRed
	default:
		return ansi.BgMagenta
	}
}

func latencyColor(d time.Duration) string {
	if d < 100*time.Millisecond {
		return ansi.FgGreen
	}
	if d < 500*time.Millisecond {
		return ansi.FgYellow
	}
	return ansi.FgRed
}
