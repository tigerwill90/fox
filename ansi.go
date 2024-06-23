package fox

import "log/slog"

// ANSI codes for text styling and formatting.
const (
	reset           = "\033[0m"
	bold            = "\033[1m"
	faint           = "\033[2m"
	underline       = "\033[4m"
	normalIntensity = "\033[22m"
	// Foreground colors
	fgRed     = "\033[31m"
	fgGreen   = "\033[32m"
	fgYellow  = "\033[33m"
	fgBlue    = "\033[34m"
	fgMagenta = "\033[35m"
	fgCyan    = "\033[36m"

	// Background colors
	bgRed     = "\033[41m"
	bgGreen   = "\033[42m"
	bgYellow  = "\033[43m"
	bgBlue    = "\033[44m"
	bgMagenta = "\033[45m"
	bgCyan    = "\033[46m"
)

type colorValue struct {
	escapeCode string
	unquoted   bool
}

var (
	fgRedColor     = colorValue{fgRed, false}
	fgGreenColor   = colorValue{fgGreen, false}
	fgYellowColor  = colorValue{fgYellow, false}
	fgBlueColor    = colorValue{fgBlue, false}
	fgMagentaColor = colorValue{fgMagenta, false}
	fgCyanColor    = colorValue{fgCyan, false}
	bgRedColor     = colorValue{bgRed, true}
	bgGreenColor   = colorValue{bgGreen, true}
	bgYellowColor  = colorValue{bgYellow, true}
	bgBlueColor    = colorValue{bgBlue, true}
	bgMagentaColor = colorValue{bgMagenta, true}
	bgCyanColor    = colorValue{bgCyan, true}
)

type colorizedValue struct {
	color colorValue
	value string
}

func (v colorizedValue) String() string {
	return v.value
}

func colorizedAttr(key, value string, color colorValue) slog.Attr {
	return slog.Any(key, colorizedValue{
		color: color,
		value: value,
	})
}
