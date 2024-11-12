package unsafe

import "unsafe"

// String (unsafe) convert without copy buf to a string value. Since Go strings are immutable, the bytes passed to
// String must NOT be modified afterward.
func String(buf []byte) string {
	return unsafe.String(unsafe.SliceData(buf), len(buf))
}
