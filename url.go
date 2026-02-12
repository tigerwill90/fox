package fox

import (
	"net/url"
	"strings"
)

func mustPathUnescape(s string) string {
	p, err := pathUnescape(s)
	if err != nil {
		panic(err)
	}
	return p
}

// pathUnescape decodes percent-encoded sequences in a route pattern, with one exception:
// encoded slashes (%2F, %2f) are preserved and normalized to uppercase %2F.
//
// This distinction matters because "/" is the only character in a URI path that serves as
// a structural delimiter (RFC 3986 §3.3). An encoded slash (%2F) within a segment represents
// a literal slash character in the segment's data and must not be confused with the "/" that
// separates segments. All other percent-encoded characters are semantically equivalent to their
// decoded form (RFC 3986 §2.3) and can be safely decoded.
//
// For example:
//   - /caf%C3%A9  → /café       (%C3%A9 decoded: equivalent resource per RFC 3986 §2.3)
//   - /foo%2Fbar  → /foo%2fbar  (%2F preserved: distinct from /foo/bar per RFC 3986 §2.2)
//   - /foo%25bar  → /foo%bar    (%25 decoded to literal %)
//
// This function is used at route registration time to normalize patterns before insertion
// into the radix tree. Since all patterns are normalized through this function, two patterns
// that differ only in encoding (e.g., /café and /caf%C3%A9) are stored identically and
// cannot coexist, which prevents ambiguous routing.
//
// Returns an error if the pattern contains malformed percent-encoding (e.g., bare % not
// followed by two hex digits).
func pathUnescape(s string) (string, error) {
	n := 0
	hasLowerSlash := false

	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+2 >= len(s) || unhex(s[i+1]) < 0 || unhex(s[i+2]) < 0 {
			if i+2 >= len(s) {
				return "", url.EscapeError(s[i:])
			}
			return "", url.EscapeError(s[i : i+3])
		}
		if unhex(s[i+1])<<4|unhex(s[i+2]) == '/' {
			if s[i+1] != '2' || s[i+2] != 'F' {
				hasLowerSlash = true
			}
		} else {
			n++
		}
		i += 2
	}

	if n == 0 && !hasLowerSlash {
		return s, nil
	}

	var buf strings.Builder
	buf.Grow(len(s) - 2*n)
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			buf.WriteByte(s[i])
			continue
		}
		decoded := byte(unhex(s[i+1])<<4 | unhex(s[i+2]))
		if decoded == '/' {
			buf.WriteString("%2F")
		} else {
			buf.WriteByte(decoded)
		}
		i += 2
	}

	return buf.String(), nil
}

func unhex(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c - 'a' + 10)
	case 'A' <= c && c <= 'F':
		return int(c - 'A' + 10)
	default:
		return -1
	}
}
