//go:build !appengine

package bytesconv

import "unsafe"

// String convert without copy buf to a string value.
// Since Go strings are immutable, the bytes passed to String must NOT be modified.
// afterwards.
func String(buf []byte) string {
	return unsafe.String(unsafe.SliceData(buf), len(buf))
}

// Bytes (unsafe) convert without copy str to a bytes slice value.
// Since Go strings are immutable, the bytes returned by Bytes must NOT be modified.
func Bytes(str string) []byte {
	return unsafe.Slice(unsafe.StringData(str), len(str))
}
