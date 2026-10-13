// Copyright 2022 Sylvain Müller. All rights reserved.
// Mount of this source code is governed by a Apache-2.0 license that can be found
// at https://github.com/fox-toolkit/fox/blob/master/LICENSE.txt.

package netutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestSplitHostPort(t *testing.T) {
	cases := []struct {
		hostPort string
		host     string
		port     string
		ok       bool
	}{
		{"localhost:http", "localhost", "http", true},
		{"localhost:80", "localhost", "80", true},
		{"localhost%lo0:http", "localhost%lo0", "http", true},
		{"localhost%lo0:80", "localhost%lo0", "80", true},
		{"[localhost%lo0]:http", "localhost%lo0", "http", true},
		{"[localhost%lo0]:80", "localhost%lo0", "80", true},
		{"127.0.0.1:http", "127.0.0.1", "http", true},
		{"127.0.0.1:80", "127.0.0.1", "80", true},
		{"[::1]:http", "::1", "http", true},
		{"[::1]:80", "::1", "80", true},
		{"[::1%lo0]:http", "::1%lo0", "http", true},
		{"[::1%lo0]:80", "::1%lo0", "80", true},
		{":http", "", "http", true},
		{":80", "", "80", true},
		{"golang.org:", "golang.org", "", true},
		{"127.0.0.1:", "127.0.0.1", "", true},
		{"[::1]:", "::1", "", true},
		{"golang.org:https%foo", "golang.org", "https%foo", true},
		{"", "", "", false},
		{"golang.org", "", "", false},
		{"127.0.0.1", "", "", false},
		{"[::1]", "", "", false},
		{"[fe80::1%lo0]", "", "", false},
		{"[localhost%lo0]", "", "", false},
		{"localhost%lo0", "", "", false},
		{"::1", "", "", false},
		{"fe80::1%lo0", "", "", false},
		{"fe80::1%lo0:80", "", "", false},
		{"[foo:bar]", "", "", false},
		{"[foo:bar]baz", "", "", false},
		{"[foo]bar:baz", "", "", false},
		{"[foo]:[bar]:baz", "", "", false},
		{"[foo]:[bar]baz", "", "", false},
		{"foo[bar]:baz", "", "", false},
		{"foo[bar:baz", "", "", false},
		{"foo]bar:baz", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.hostPort, func(t *testing.T) {
			host, port, ok := SplitHostPort(tc.hostPort)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.host, host)
			assert.Equal(t, tc.port, port)
		})
	}
}

func TestParsePrefix(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantPrefix string
		wantError  bool
	}{
		{
			name:       "valid ipv4 cidr",
			input:      "192.168.1.0/24",
			wantPrefix: "192.168.1.0/24",
		},
		{
			name:       "ipv4 cidr with host bits",
			input:      "192.168.1.5/24",
			wantPrefix: "192.168.1.0/24",
		},
		{
			name:       "valid ipv6 cidr",
			input:      "2001:db8::/32",
			wantPrefix: "2001:db8::/32",
		},
		{
			name:       "plain ipv4 address",
			input:      "192.168.1.1",
			wantPrefix: "192.168.1.1/32",
		},
		{
			name:       "plain ipv6 address",
			input:      "2001:db8::1",
			wantPrefix: "2001:db8::1/128",
		},
		{
			name:       "ipv4 mapped ipv6 address",
			input:      "::ffff:192.168.1.1",
			wantPrefix: "192.168.1.1/32",
		},
		{
			name:       "ipv4 mapped ipv6 cidr",
			input:      "::ffff:10.0.0.0/104",
			wantPrefix: "10.0.0.0/8",
		},
		{
			name:       "ipv4 mapped ipv6 cidr below 96 bits",
			input:      "::ffff:0:0/80",
			wantPrefix: "::/80",
		},
		{
			name:      "zoned address",
			input:     "fe80::1%eth0",
			wantError: true,
		},
		{
			name:      "zoned cidr",
			input:     "fe80::1%eth0/10",
			wantError: true,
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
			prefix, err := ParsePrefix(tc.input)
			if tc.wantError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantPrefix, prefix.String())
		})
	}
}
