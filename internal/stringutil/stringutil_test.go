package stringutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEqualASCIIIgnoreCase(t *testing.T) {
	cases := []struct {
		name string
		s    uint8
		t    uint8
		want bool
	}{
		// Exact matches
		{"same lowercase letter", 'a', 'a', true},
		{"same uppercase letter", 'A', 'A', true},
		{"same digit", '5', '5', true},
		{"same hyphen", '-', '-', true},

		// Case-insensitive letter matches
		{"A and a", 'A', 'a', true},
		{"a and A", 'a', 'A', true},
		{"Z and z", 'Z', 'z', true},
		{"z and Z", 'z', 'Z', true},
		{"M and m", 'M', 'm', true},
		{"m and M", 'm', 'M', true},

		// Different letters (should not match)
		{"A and B", 'A', 'B', false},
		{"a and b", 'a', 'b', false},
		{"A and b", 'A', 'b', false},
		{"a and B", 'a', 'B', false},

		// Digits (only match exactly)
		{"0 and 0", '0', '0', true},
		{"9 and 9", '9', '9', true},
		{"0 and 1", '0', '1', false},
		{"5 and 6", '5', '6', false},

		// Hyphen (only matches exactly)
		{"hyphen and hyphen", '-', '-', true},
		{"hyphen and A", '-', 'A', false},
		{"hyphen and a", '-', 'a', false},
		{"hyphen and 0", '-', '0', false},

		// Characters just outside letter ranges
		{"@ and A", '@', 'A', false},
		{"Z and [", 'Z', '[', false},
		{"` and a", '`', 'a', false},
		{"z and {", 'z', '{', false},

		// Special characters and control chars
		{"null and A", 0, 'A', false},
		{"A and null", 'A', 0, false},
		{"space and A", ' ', 'A', false},
		{"A and space", 'A', ' ', false},
		{"! and A", '!', 'A', false},
		{"A and !", 'A', '!', false},
		{"/ and A", '/', 'A', false},
		{"A and /", 'A', '/', false},

		// High ASCII values
		{"high byte and A", 0xFF, 'A', false},
		{"A and high byte", 'A', 0xFF, false},
		{"high byte and a", 0xFF, 'a', false},
		{"a and high byte", 'a', 0xFF, false},

		// Case difference edge cases
		{"@ and `", '@', '`', false},
		{"0 and P", '0', 'P', false},

		// Boundary cases for the letter ranges
		{"A-1 and a", 'A' - 1, 'a', false},
		{"Z+1 and z", 'Z' + 1, 'z', false},
		{"a-1 and A", 'a' - 1, 'A', false},
		{"z+1 and Z", 'z' + 1, 'Z', false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, EqualASCIIIgnoreCase(tc.s, tc.t))
		})
	}
}

func TestEqualStringsASCIIIgnoreCase(t *testing.T) {
	cases := []struct {
		name string
		s1   string
		s2   string
		want bool
	}{
		// Empty strings
		{"empty strings", "", "", true},
		{"empty and non-empty", "", "a", false},

		// Same case strings
		{"same lowercase", "hello", "hello", true},
		{"same uppercase", "HELLO", "HELLO", true},
		{"same mixed", "HeLLo", "HeLLo", true},

		// Different case strings
		{"different case simple", "hello", "HELLO", true},
		{"different case mixed", "HeLLo", "hEllO", true},

		// Different lengths
		{"different length 1", "hello", "helloworld", false},
		{"different length 2", "helloworld", "hello", false},

		// Different content
		{"different content", "hello", "world", false},
		{"different content case", "HELLO", "world", false},

		// With digits and hyphens
		{"with digits same", "test123", "TEST123", true},
		{"with digits different", "test123", "test456", false},
		{"with hyphens", "hello-world", "HELLO-WORLD", true},
		{"with underscore", "hello_world", "HELLO_WORLD", true},

		// Mixed content
		{"hostname like", "example.com", "EXAMPLE.COM", true},
		{"subdomain", "api-v2.example.com", "API-V2.EXAMPLE.COM", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, EqualStringsASCIIIgnoreCase(tc.s1, tc.s2))
		})
	}
}

func TestToLowerASCII(t *testing.T) {
	cases := []struct {
		name string
		b    byte
		want byte
	}{
		// Uppercase letters (should convert)
		{"uppercase A", 'A', 'a'},
		{"uppercase Z", 'Z', 'z'},
		{"uppercase M", 'M', 'm'},

		// Lowercase letters (should stay same)
		{"lowercase a", 'a', 'a'},
		{"lowercase z", 'z', 'z'},
		{"lowercase m", 'm', 'm'},

		// Digits (should stay same)
		{"digit 0", '0', '0'},
		{"digit 9", '9', '9'},
		{"digit 5", '5', '5'},

		// Special characters (should stay same)
		{"hyphen", '-', '-'},
		{"underscore", '_', '_'},
		{"dot", '.', '.'},
		{"space", ' ', ' '},

		// Boundary cases
		{"before A", 'A' - 1, 'A' - 1},
		{"after Z", 'Z' + 1, 'Z' + 1},
		{"before a", 'a' - 1, 'a' - 1},
		{"after z", 'z' + 1, 'z' + 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ToLowerASCII(tc.b))
		})
	}
}
