package stringsutil

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
		{"same lowercase letter", 'a', 'a', true},
		{"same uppercase letter", 'A', 'A', true},
		{"same digit", '5', '5', true},
		{"same hyphen", '-', '-', true},
		{"A and a", 'A', 'a', true},
		{"a and A", 'a', 'A', true},
		{"Z and z", 'Z', 'z', true},
		{"z and Z", 'z', 'Z', true},
		{"M and m", 'M', 'm', true},
		{"m and M", 'm', 'M', true},
		{"A and B", 'A', 'B', false},
		{"a and b", 'a', 'b', false},
		{"A and b", 'A', 'b', false},
		{"a and B", 'a', 'B', false},
		{"0 and 0", '0', '0', true},
		{"9 and 9", '9', '9', true},
		{"0 and 1", '0', '1', false},
		{"5 and 6", '5', '6', false},
		{"hyphen and hyphen", '-', '-', true},
		{"hyphen and A", '-', 'A', false},
		{"hyphen and a", '-', 'a', false},
		{"hyphen and 0", '-', '0', false},
		{"@ and A", '@', 'A', false},
		{"Z and [", 'Z', '[', false},
		{"` and a", '`', 'a', false},
		{"z and {", 'z', '{', false},
		{"null and A", 0, 'A', false},
		{"A and null", 'A', 0, false},
		{"space and A", ' ', 'A', false},
		{"A and space", 'A', ' ', false},
		{"! and A", '!', 'A', false},
		{"A and !", 'A', '!', false},
		{"/ and A", '/', 'A', false},
		{"A and /", 'A', '/', false},
		{"high byte and A", 0xFF, 'A', false},
		{"A and high byte", 'A', 0xFF, false},
		{"high byte and a", 0xFF, 'a', false},
		{"a and high byte", 'a', 0xFF, false},
		{"@ and `", '@', '`', false},
		{"0 and P", '0', 'P', false},
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
		{"empty strings", "", "", true},
		{"empty and non-empty", "", "a", false},
		{"same lowercase", "hello", "hello", true},
		{"same uppercase", "HELLO", "HELLO", true},
		{"same mixed", "HeLLo", "HeLLo", true},
		{"different case simple", "hello", "HELLO", true},
		{"different case mixed", "HeLLo", "hEllO", true},
		{"different length 1", "hello", "helloworld", false},
		{"different length 2", "helloworld", "hello", false},
		{"different content", "hello", "world", false},
		{"different content case", "HELLO", "world", false},
		{"with digits same", "test123", "TEST123", true},
		{"with digits different", "test123", "test456", false},
		{"with hyphens", "hello-world", "HELLO-WORLD", true},
		{"with underscore", "hello_world", "HELLO_WORLD", true},
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
		{"uppercase A", 'A', 'a'},
		{"uppercase Z", 'Z', 'z'},
		{"uppercase M", 'M', 'm'},
		{"lowercase a", 'a', 'a'},
		{"lowercase z", 'z', 'z'},
		{"lowercase m", 'm', 'm'},
		{"digit 0", '0', '0'},
		{"digit 9", '9', '9'},
		{"digit 5", '5', '5'},
		{"hyphen", '-', '-'},
		{"underscore", '_', '_'},
		{"dot", '.', '.'},
		{"space", ' ', ' '},
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

func TestNormalizeHexUppercase(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty string", "", ""},
		{"plain ascii", "/foo/bar", "/foo/bar"},
		{"no encoding", "/users/hello/world", "/users/hello/world"},
		{"uppercase hex pair", "/caf%C3%A9", "/caf%C3%A9"},
		{"uppercase encoded slash", "/foo%2Fbar", "/foo%2Fbar"},
		{"multiple uppercase", "/%C3%A9/%E2%82%AC", "/%C3%A9/%E2%82%AC"},
		{"digits only hex", "%20%30%09", "%20%30%09"},
		{"lowercase hex pair", "/caf%c3%a9", "/caf%C3%A9"},
		{"lowercase encoded slash", "/foo%2fbar", "/foo%2Fbar"},
		{"mixed case hi lowercase", "/caf%c3%A9", "/caf%C3%A9"},
		{"mixed case lo lowercase", "/caf%C3%a9", "/caf%C3%A9"},
		{"multiple lowercase", "/%c3%a9/%e2%82%ac", "/%C3%A9/%E2%82%AC"},
		{"mixed sequences", "/foo%C3%a9/bar%2f", "/foo%C3%A9/bar%2F"},
		{"lowercase at start", "%c3%a9/foo", "%C3%A9/foo"},
		{"lowercase at end", "/foo/%c3%a9", "/foo/%C3%A9"},
		{"uppercase then lowercase", "/%C3%A9/%c3%a9", "/%C3%A9/%C3%A9"},
		{"three byte utf8 lowercase", "/%e2%82%ac", "/%E2%82%AC"},
		{"four byte utf8 lowercase", "/%f0%90%8d%88", "/%F0%90%8D%88"},
		{"digit only encoding", "/%25%30%39", "/%25%30%39"},
		{"encoded space", "/hello%20world", "/hello%20world"},
		{"encoded path segment", "/users/caf%c3%a9/profile", "/users/caf%C3%A9/profile"},
		{"encoded slash in path", "/api/v1/foo%2fbar/baz", "/api/v1/foo%2Fbar/baz"},
		{"double encoding preserved", "/foo%252fbar", "/foo%252fbar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeHexUppercase(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNormalizeHexUppercaseNoAlloc(t *testing.T) {
	noAllocCases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"plain path", "/foo/bar/baz"},
		{"already uppercase", "/caf%C3%A9"},
		{"multiple uppercase", "/%C3%A9/%2F/%20"},
		{"no encoding", "/users/hello"},
	}

	for _, tc := range noAllocCases {
		t.Run(tc.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(100, func() {
				_ = NormalizeHexUppercase(tc.in)
			})
			assert.Equal(t, float64(0), allocs)
		})
	}
}
