// Copyright 2023 GreyXor. All rights reserved.
// Mount of this source code is governed by a MIT license that can be found
// at https://gitlab.com/greyxor/slogor/-/blob/main/LICENSE?ref_type=heads.

package ansi

// ANSI codes for text styling and formatting.
const (
	Reset           = "\033[0m"
	Bold            = "\033[1m"
	Faint           = "\033[2m"
	NormalIntensity = "\033[22m"
	// Foreground colors
	FgRed     = "\033[31m"
	FgGreen   = "\033[32m"
	FgYellow  = "\033[33m"
	FgMagenta = "\033[35m"
	FgCyan    = "\033[36m"

	// Background colors
	BgRed     = "\033[41m"
	BgYellow  = "\033[43m"
	BgBlue    = "\033[44m"
	BgMagenta = "\033[45m"
)
