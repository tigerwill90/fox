package stringutil

// EqualStringsASCIIIgnoreCase performs case-insensitive comparison of two strings
// containing ASCII characters. Only supports ASCII letters (A-Z, a-z), digits (0-9), hyphen (-) and underscore (_).
// Used for hostname matching where registered routes follow LDH standard.
func EqualStringsASCIIIgnoreCase(s1, s2 string) bool {
	// Easy case.
	if len(s1) != len(s2) {
		return false
	}
	for i := 0; i < len(s1); i++ {
		if !EqualASCIIIgnoreCase(s1[i], s2[i]) {
			return false
		}
	}
	return true
}

// EqualASCIIIgnoreCase performs case-insensitive comparison of two ASCII bytes.
// Only supports ASCII letters (A-Z, a-z), digits (0-9), hyphen (-) and underscore (_).
// Used for hostname matching where registered routes follow LDH standard.
func EqualASCIIIgnoreCase(s, t uint8) bool {
	// Easy case.
	if t == s {
		return true
	}

	// Make s < t to simplify what follows.
	if t < s {
		t, s = s, t
	}

	// ASCII only, s/t must be upper/lower case
	if 'A' <= s && s <= 'Z' && t == s+'a'-'A' {
		return true
	}

	return false
}

// ToLowerASCII converts an ASCII uppercase letter (A-Z) to lowercase (a-z).
// All other bytes are returned unchanged. Does not validate ASCII range;
func ToLowerASCII(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
