package netutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitHostZone(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantHost string
		wantZone string
	}{
		{
			name:     "ipv6 with zone",
			input:    "fe80::1%eth0",
			wantHost: "fe80::1",
			wantZone: "eth0",
		},
		{
			name:     "ipv6 without zone",
			input:    "fe80::1",
			wantHost: "fe80::1",
			wantZone: "",
		},
		{
			name:     "ipv4 address",
			input:    "192.168.1.1",
			wantHost: "192.168.1.1",
			wantZone: "",
		},
		{
			name:     "empty string",
			input:    "",
			wantHost: "",
			wantZone: "",
		},
		{
			name:     "zone only percent at start",
			input:    "%eth0",
			wantHost: "%eth0",
			wantZone: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, zone := SplitHostZone(tc.input)
			assert.Equal(t, tc.wantHost, host)
			assert.Equal(t, tc.wantZone, zone)
		})
	}
}

func TestStripHostPort(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "host with port",
			input: "example.com:8080",
			want:  "example.com",
		},
		{
			name:  "host without port",
			input: "example.com",
			want:  "example.com",
		},
		{
			name:  "host with trailing dot",
			input: "example.com.",
			want:  "example.com",
		},
		{
			name:  "host with port and trailing dot",
			input: "example.com.:8080",
			want:  "example.com",
		},
		{
			name:  "ipv4 with port",
			input: "192.168.1.1:80",
			want:  "192.168.1.1",
		},
		{
			name:  "ipv6 with port",
			input: "[::1]:8080",
			want:  "::1",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "invalid host port returns unchanged",
			input: "[invalid",
			want:  "[invalid",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, StripHostPort(tc.input))
		})
	}
}

func TestParseCIDR(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantCIDR  string
		wantError bool
	}{
		{
			name:     "valid ipv4 cidr",
			input:    "192.168.1.0/24",
			wantCIDR: "192.168.1.0/24",
		},
		{
			name:     "valid ipv6 cidr",
			input:    "2001:db8::/32",
			wantCIDR: "2001:db8::/32",
		},
		{
			name:     "plain ipv4 address",
			input:    "192.168.1.1",
			wantCIDR: "192.168.1.1/32",
		},
		{
			name:     "plain ipv6 address",
			input:    "2001:db8::1",
			wantCIDR: "2001:db8::1/128",
		},
		{
			name:      "invalid input",
			input:     "invalid",
			wantError: true,
		},
		{
			name:      "empty string",
			input:     "",
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ipNet, err := ParseCIDR(tc.input)
			if tc.wantError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantCIDR, ipNet.String())
		})
	}
}
