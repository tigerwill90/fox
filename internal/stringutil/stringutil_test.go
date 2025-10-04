package stringutil

import "testing"

func TestEqualASCIIIgnoreCase(t *testing.T) {
	tests := []struct {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EqualASCIIIgnoreCase(tt.s, tt.t); got != tt.want {
				t.Errorf("equalASCIIIgnoreCase(%c=%d, %c=%d) = %v, want %v",
					tt.s, tt.s, tt.t, tt.t, got, tt.want)
			}
		})
	}
}
