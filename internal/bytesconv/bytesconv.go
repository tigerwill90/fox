//go:build appengine

package bytesconv

// String converts buf to a string value with memory copy.
func String(buf []byte) string {
	return string(buf)
}

// Bytes converts str to a bytes slice value with memory copy.
func Bytes(str string) []byte {
	return []byte(str)
}
