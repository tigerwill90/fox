package clientip

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tigerwill90/fox"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteAddrStrategy_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("X-Forwarded-For", "1.1.1.1, 2001:db8:cafe::99%eth0, 3.3.3.3, 192.168.1.1")
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(fox.New(), w, req)

	cases := []struct {
		name     string
		remoteIP string
		wantIP   string
		wantZone string
		wantErr  error
	}{
		{
			name:     "should return an ipv4 address",
			remoteIP: "192.0.2.1:56235",
			wantIP:   "192.0.2.1",
		},
		{
			name:     "should return an ipv6 address",
			remoteIP: "[fe80::1ff:fe23:4567:890a]:56235",
			wantIP:   "fe80::1ff:fe23:4567:890a",
		},
		{
			name:     "should return an ipv6 address with zone",
			remoteIP: "[fe80::1ff:fe23:4567:890a%eth0]:56235",
			wantIP:   "fe80::1ff:fe23:4567:890a",
			wantZone: "eth0",
		},
		{
			name:     "should return an an invalid ip address error",
			remoteIP: "@",
			wantErr:  ErrInvalidIpAddress,
		},
		{
			// This is for coverage. It should not be possible for RemoteAddrStrategy.
			name:     "should return an an unspecified ip address error",
			remoteIP: "0.0.0.0",
			wantErr:  ErrUnspecifiedIpAddress,
		},
	}

	s := NewRemoteAddrStrategy()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c.Request().RemoteAddr = tc.remoteIP
			ipAddr, err := s.ClientIP(c)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			assert.Equal(t, tc.wantIP, ipAddr.IP.String())
			assert.Equal(t, tc.wantZone, ipAddr.Zone)

		})
	}

}

func TestSingleIPHeaderStrategy_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("X-Real-IP", "4.4.4.4")
	req.Header.Add("X-Real-IP", "5.5.5.5")
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(fox.New(), w, req)

	s := NewSingleIPHeaderStrategy("X-Real-IP")
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "5.5.5.5", ipAddr.String())

	c.Request().Header.Del("X-Real-IP")
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrSingleIPHeaderStrategy)
}

func TestLeftmostNonPrivateStrategy_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("Forwarded", `For=fe80::abcd;By=fe80::1234, Proto=https;For=::ffff:188.0.2.128, For="[2001:db8:cafe::17]:4848", For=fc00::1`)
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(fox.New(), w, req)

	s := NewLeftmostNonPrivateStrategy("Forwarded")
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "188.0.2.128", ipAddr.String())

	// Only private IP address
	req.Header.Set("Forwarded", `for=192.168.1.1, for=10.0.0.1, for="[fd00::1]", for=172.16.0.1`)
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrLeftmostNonPrivateStrategy)
}

func TestRightmostNonPrivateStrategy_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("X-Forwarded-For", "1.1.1.1, 2001:db8:cafe::99%eth0, 3.3.3.3, 192.168.1.1")
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(fox.New(), w, req)
	s := NewRightmostNonPrivateStrategy("X-Forwarded-For")
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "3.3.3.3", ipAddr.String())

	// With no whitespace
	req.Header.Set("X-Forwarded-For", "1.1.1.1,2001:db8:cafe::99%eth0, 3.3.3.3,192.168.1.1")
	ipAddr, err = s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "3.3.3.3", ipAddr.String())

	// Only private IP
	req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.1, [fd00::1], 172.16.0.1")
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrRightmostNonPrivateStrategy)
}

func TestRightmostTrustedCountStrategy_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("Forwarded", `For=fe80::abcd;By=fe80::1234, Proto=https;For=::ffff:188.0.2.128, For="[2001:db8:cafe::17]:4848", For=fc00::1`)
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(fox.New(), w, req)
	s := NewRightmostTrustedCountStrategy("Forwarded", 2)
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "2001:db8:cafe::17", ipAddr.String())

	req.Header.Set("Forwarded", `For=fc00::1`)
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrRightmostTrustedCountStrategy)
	assert.ErrorContains(t, err, "expected 2 IP(s) but found 1")

	req.Header.Set("Forwarded", `For=fe80::abcd;By=fe80::1234, Proto=https;For=::ffff:188.0.2.128, For="invalid", For=fc00::1`)
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrRightmostTrustedCountStrategy)
	assert.ErrorContains(t, err, "invalid IP address from the first trusted proxy")
}

func TestRightmostTrustedRangeStrategy_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("X-Forwarded-For", "1.1.1.1, 2001:db8:cafe::99%eth0, 3.3.3.3, 192.168.1.1")
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(fox.New(), w, req)
	trustedRanges, _ := AddressesAndRangesToIPNets([]string{"192.168.0.0/16", "3.3.3.3"}...)
	s := NewRightmostTrustedRangeStrategy("X-Forwarded-For", trustedRanges)
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "2001:db8:cafe::99%eth0", ipAddr.String())

	// Invalid IP
	req.Header.Set("X-Forwarded-For", "1.1.1.1, invalid, 3.3.3.3, 192.168.1.1")
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrRightmostTrustedRangeStrategy)
	assert.ErrorContains(t, err, "unable to find a valid IP address")

	req.Header.Set("X-Forwarded-For", "192.168.1.2, 3.3.3.3, 192.168.1.1")
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrRightmostTrustedRangeStrategy)
	assert.ErrorContains(t, err, "unable to find a valid IP address")
}

func TestChainStrategy_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("X-Real-IP", "4.4.4.4")
	req.RemoteAddr = "192.0.2.1:8080"
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(fox.New(), w, req)
	s := NewChainStrategy(
		NewSingleIPHeaderStrategy("Cf-Connecting-IP"),
		NewRemoteAddrStrategy(),
	)
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "192.0.2.1", ipAddr.String())

	// Invalid remote ip
	req.RemoteAddr = " @"
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrSingleIPHeaderStrategy)
	assert.ErrorIs(t, err, ErrRemoteAddressStrategy)
	assert.ErrorIs(t, err, ErrInvalidIpAddress)
	assert.ErrorContains(t, err, "header \"Cf-Connecting-Ip\" not found")
}

func TestAddressesAndRangesToIPNets(t *testing.T) {
	tests := []struct {
		name    string
		ranges  []string
		want    []string
		wantErr bool
	}{
		{
			name:   "Empty input",
			ranges: []string{},
			want:   nil,
		},
		{
			name:   "Single IPv4 address",
			ranges: []string{"1.1.1.1"},
			want:   []string{"1.1.1.1/32"},
		},
		{
			name:   "Single IPv6 address",
			ranges: []string{"2607:f8b0:4004:83f::200e"},
			want:   []string{"2607:f8b0:4004:83f::200e/128"},
		},
		{
			name:   "Single IPv4 range",
			ranges: []string{"1.1.1.1/16"},
			want:   []string{"1.1.0.0/16"},
		},
		{
			name:   "Single IPv6 range",
			ranges: []string{"2607:f8b0:4004:83f::200e/48"},
			want:   []string{"2607:f8b0:4004::/48"},
		},
		{
			name: "Mixed input",
			ranges: []string{
				"1.1.1.1", "2607:f8b0:4004:83f::200e",
				"1.1.1.1/32", "2607:f8b0:4004:83f::200e/128",
				"1.1.1.1/16", "2607:f8b0:4004:83f::200e/56",
				"::ffff:188.0.2.128/112", "::ffff:bc15:0006/104",
				"64:ff9b::188.0.2.128/112",
			},
			want: []string{
				"1.1.1.1/32", "2607:f8b0:4004:83f::200e/128",
				"1.1.1.1/32", "2607:f8b0:4004:83f::200e/128",
				"1.1.0.0/16", "2607:f8b0:4004:800::/56",
				"188.0.0.0/16", "188.0.0.0/8",
				"64:ff9b::bc00:0/112",
			},
		},
		{
			name:   "No input",
			ranges: nil,
			want:   nil,
		},
		{
			name:    "Error: garbage CIDR",
			ranges:  []string{"2607:f8b0:4004:83f::200e/nope"},
			wantErr: true,
		},
		{
			name:    "Error: CIDR with zone",
			ranges:  []string{"fe80::abcd%nope/64"},
			wantErr: true,
		},
		{
			name:    "Error: garbage IP",
			ranges:  []string{"1.1.1.nope"},
			wantErr: true,
		},
		{
			name:    "Error: empty value",
			ranges:  []string{""},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AddressesAndRangesToIPNets(tt.ranges...)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			if err != nil {
				// We can't continue
				return
			}

			require.Equal(t, len(tt.want), len(got))
			for i := 0; i < len(got); i++ {
				if got[i].String() != tt.want[i] {
					assert.Equal(t, tt.want[i], got[i].String())
				}
			}
		})
	}
}

func TestMustParseIPAddr(t *testing.T) {
	// We test the non-panic path elsewhere, but we need to specifically check the panic case
	assert.Panics(t, func() {
		MustParseIPAddr("nope")
	})
}

func TestParseIPAddr(t *testing.T) {
	tests := []struct {
		name    string
		ipStr   string
		want    net.IPAddr
		wantErr bool
	}{
		{
			name:  "Empty zone",
			ipStr: "1.1.1.1%",
			want:  net.IPAddr{IP: net.ParseIP("1.1.1.1"), Zone: ""},
		},
		{
			name:  "No zone",
			ipStr: "1.1.1.1",
			want:  net.IPAddr{IP: net.ParseIP("1.1.1.1"), Zone: ""},
		},
		{
			name:  "With zone",
			ipStr: "fe80::abcd%zone",
			want:  net.IPAddr{IP: net.ParseIP("fe80::abcd"), Zone: "zone"},
		},
		{
			name:  "With zone and port",
			ipStr: "[2607:f8b0:4004:83f::200e%zone]:4484",
			want:  net.IPAddr{IP: net.ParseIP("2607:f8b0:4004:83f::200e"), Zone: "zone"},
		},
		{
			name:  "With port",
			ipStr: "1.1.1.1:48944",
			want:  net.IPAddr{IP: net.ParseIP("1.1.1.1"), Zone: ""},
		},
		{
			name:  "Bad port (is discarded)",
			ipStr: "[fe80::abcd%eth0]:xyz",
			want:  net.IPAddr{IP: net.ParseIP("fe80::abcd"), Zone: "eth0"},
		},
		{
			name:    "Zero address",
			ipStr:   "0.0.0.0",
			wantErr: true,
		},
		{
			name:    "Unspecified address",
			ipStr:   "::",
			wantErr: true,
		},
		{
			name:    "Error: bad IP with zone",
			ipStr:   "nope%zone",
			wantErr: true,
		},
		{
			name:    "Error: bad IP",
			ipStr:   "nope!!",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIPAddr(tt.ipStr)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			if !ipAddrsEqual(*got, tt.want) {
				t.Fatalf("ParseIPAddr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isPrivateOrLocal(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{
			name: "IPv4 loopback",
			ip:   `127.0.0.2`,
			want: true,
		},
		{
			name: "IPv6 loopback",
			ip:   `::1`,
			want: true,
		},
		{
			name: "IPv4 10.*",
			ip:   `10.0.0.1`,
			want: true,
		},
		{
			name: "IPv4 192.168.*",
			ip:   `192.168.1.1`,
			want: true,
		},
		{
			name: "IPv6 unique local address",
			ip:   `fd12:3456:789a:1::1`,
			want: true,
		},
		{
			name: "IPv4 link-local",
			ip:   `169.254.1.1`,
			want: true,
		},
		{
			name: "IPv6 link-local",
			ip:   `fe80::abcd`,
			want: true,
		},
		{
			name: "Non-local IPv4",
			ip:   `1.1.1.1`,
			want: false,
		},
		{
			name: "Non-local IPv4-mapped IPv6",
			ip:   `::ffff:188.0.2.128`,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip)
			got := isPrivateOrLocal(ip)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_mustParseCIDR(t *testing.T) {
	// We test the non-panic path elsewhere, but we need to specifically check the panic case
	assert.Panics(t, func() {
		mustParseCIDR("nope")
	})
}

func Test_trimMatchedEnds(t *testing.T) {
	// We test the non-panic paths elsewhere, but we need to specifically check the panic case
	assert.Panics(t, func() {
		trimMatchedEnds("nope", "abcd")
	})
}

func Test_parseForwardedListItem(t *testing.T) {
	tests := []struct {
		name string
		fwd  string
		want *net.IPAddr
	}{
		{
			// This is the correct form for IPv6 wit port
			name: "IPv6 with port and quotes",
			fwd:  `For="[2607:f8b0:4004:83f::200e]:4711"`,
			want: MustParseIPAddr("2607:f8b0:4004:83f::200e"),
		},
		{
			// This is the correct form for IP with no port
			name: "IPv6 with quotes, brackets and no port",
			fwd:  `fOR="[2607:f8b0:4004:83f::200e]"`,
			want: MustParseIPAddr("2607:f8b0:4004:83f::200e"),
		},
		{
			// RFC deviation: missing brackets
			name: "IPv6 with quotes, no brackets, and no port",
			fwd:  `for="2607:f8b0:4004:83f::200e"`,
			want: MustParseIPAddr("2607:f8b0:4004:83f::200e"),
		},
		{
			// RFC deviation: missing quotes
			name: "IPv6 with brackets, no quotes, and no port",
			fwd:  `FOR=[2607:f8b0:4004:83f::200e]`,
			want: MustParseIPAddr("2607:f8b0:4004:83f::200e"),
		},
		{
			// RFC deviation: missing quotes
			name: "IPv6 with port and no quotes",
			fwd:  `For=[2607:f8b0:4004:83f::200e]:4711`,
			want: MustParseIPAddr("2607:f8b0:4004:83f::200e"),
		},
		{
			name: "IPv6 with port, quotes, and zone",
			fwd:  `For="[fe80::abcd%zone]:4711"`,
			want: MustParseIPAddr("fe80::abcd%zone"),
		},
		{
			// RFC deviation: missing brackets
			name: "IPv6 with zone, no quotes, no port",
			fwd:  `For="fe80::abcd%zone"`,
			want: MustParseIPAddr("fe80::abcd%zone"),
		},
		{
			// RFC deviation: missing quotes
			name: "IPv4 with port",
			fwd:  `FoR=192.0.2.60:4711`,
			want: MustParseIPAddr("192.0.2.60"),
		},
		{
			name: "IPv4 with no port",
			fwd:  `for=192.0.2.60`,
			want: MustParseIPAddr("192.0.2.60"),
		},
		{
			name: "IPv4 with quotes",
			fwd:  `for="192.0.2.60"`,
			want: MustParseIPAddr("192.0.2.60"),
		},
		{
			name: "IPv4 with port and quotes",
			fwd:  `for="192.0.2.60:4823"`,
			want: MustParseIPAddr("192.0.2.60"),
		},
		{
			name: "Error: invalid IPv4",
			fwd:  `for=192.0.2.999`,
			want: nil,
		},
		{
			name: "Error: invalid IPv6",
			fwd:  `for="2607:f8b0:4004:83f::999999"`,
			want: nil,
		},
		{
			name: "Error: non-IP identifier",
			fwd:  `for="_test"`,
			want: nil,
		},
		{
			name: "Error: empty IP value",
			fwd:  `for=`,
			want: nil,
		},
		{
			name: "Multiple IPv4 directives",
			fwd:  `by=1.1.1.1; for=2.2.2.2;host=myhost; proto=https`,
			want: MustParseIPAddr("2.2.2.2"),
		},
		{
			// RFC deviation: missing quotes around IPv6
			name: "Multiple IPv6 directives",
			fwd:  `by=1::1;host=myhost;for=2::2;proto=https`,
			want: MustParseIPAddr("2::2"),
		},
		{
			// RFC deviation: missing quotes around IPv6
			name: "Multiple mixed directives",
			fwd:  `by=1::1;host=myhost;proto=https;for=2.2.2.2`,
			want: MustParseIPAddr("2.2.2.2"),
		},
		{
			name: "IPv4-mapped IPv6",
			fwd:  `for="[::ffff:188.0.2.128]"`,
			want: MustParseIPAddr("188.0.2.128"),
		},
		{
			name: "IPv4-mapped IPv6 with port and quotes",
			fwd:  `for="[::ffff:188.0.2.128]:49428"`,
			want: MustParseIPAddr("188.0.2.128"),
		},
		{
			name: "IPv4-mapped IPv6 in IPv6 form",
			fwd:  `for="[0:0:0:0:0:ffff:bc15:0006]"`,
			want: MustParseIPAddr("188.21.0.6"),
		},
		{
			name: "NAT64 IPv4-mapped IPv6",
			fwd:  `for="[64:ff9b::188.0.2.128]"`,
			want: MustParseIPAddr("64:ff9b::188.0.2.128"),
		},
		{
			name: "IPv4 loopback",
			fwd:  `for=127.0.0.1`,
			want: MustParseIPAddr("127.0.0.1"),
		},
		{
			name: "IPv6 loopback",
			fwd:  `for="[::1]"`,
			want: MustParseIPAddr("::1"),
		},
		{
			// RFC deviation: quotes must be matched
			name: "Error: Unmatched quote",
			fwd:  `for="1.1.1.1`,
			want: nil,
		},
		{
			// RFC deviation: brackets must be matched
			name: "Error: IPv6 loopback",
			fwd:  `for="::1]"`,
			want: nil,
		},
		{
			name: "Error: misplaced quote",
			fwd:  `for="[0:0:0:0:0:ffff:bc15:0006"]`,
			want: nil,
		},
		{
			name: "Error: garbage",
			fwd:  "ads\x00jkl&#*(383fdljk",
			want: nil,
		},
		{
			// Per RFC 7230 section 3.2.6, this should not be an error, but we don't have
			// full syntax support yet.
			name: "RFC deviation: quoted pair",
			fwd:  `for=1.1.1.\1`,
			want: nil,
		},
		{
			// Per RFC 7239, this extraneous whitespace should be an error, but we don't
			// have full syntax support yet.
			name: "RFC deviation: Incorrect whitespace",
			fwd:  `for= 1.1.1.1`,
			want: MustParseIPAddr("1.1.1.1"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseForwardedListItem(tt.fwd)

			if got == nil || tt.want == nil {
				assert.Equal(t, tt.want, got)
				return
			}

			if !ipAddrsEqual(*got, *tt.want) {
				t.Fatalf("parseForwardedListItem() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Demonstrate parsing deviations from Forwarded header syntax RFCs, particularly
// RFC 7239 (Forwarded header) and RFC 7230 (HTTP/1.1 syntax) section 3.2.6.
func Test_forwardedHeaderRFCDeviations(t *testing.T) {
	type args struct {
		headers    http.Header
		headerName string
	}
	tests := []struct {
		name string
		args args
		want []*net.IPAddr
	}{
		{
			// The value in quotes should be a single value but we split by comma, so it's not.
			// The first and third "For=" bits have one double-quote in them, so they are
			// considered invalid by our parser. The second is still in the quoted-string,
			// but doesn't have any quotes in it, so it parses okay.
			name: "Comma in quotes",
			args: args{
				headers:    http.Header{"Forwarded": []string{`For="1.1.1.1, For=2.2.2.2, For=3.3.3.3", For="4.4.4.4"`}},
				headerName: "Forwarded",
			},
			// There are really only two values, so we actually want: {nil, "4.4.4.4"}
			want: []*net.IPAddr{nil, MustParseIPAddr("2.2.2.2"), nil, MustParseIPAddr("4.4.4.4")},
		},
		{
			// Per 7239, the opening unmatched quote makes the whole rest of the header invalid.
			// But that would mean that an attacker can invalidate the whole header with a
			// quote character early on, even the trusted IPs added by our reverse proxies.
			// Our actual behaviour is probably the best approach.
			name: "Unmatched quote",
			args: args{
				headers:    http.Header{"Forwarded": []string{`For="1.1.1.1, For=2.2.2.2`}},
				headerName: "Forwarded",
			},
			// There are really only two values, so the RFC would require: {nil} (or empty slice?)
			want: []*net.IPAddr{nil, MustParseIPAddr("2.2.2.2")},
		},
		{
			// The invalid non-For parameter should invalidate the whole item, but we're
			// not checking anything but the "For=" part.
			name: "Invalid characters",
			args: args{
				headers:    http.Header{"Forwarded": []string{`For=1.1.1.1;@!=😀, For=2.2.2.2`}},
				headerName: "Forwarded",
			},
			// Only the last value is valid, so it should be: {nil, "2.2.2.2"}
			want: []*net.IPAddr{MustParseIPAddr("1.1.1.1"), MustParseIPAddr("2.2.2.2")},
		},
		{
			// The duplicate "For=" parameter should invalidate the whole item but we don't check for it
			name: "Duplicate token",
			args: args{
				headers:    http.Header{"Forwarded": []string{`For=1.1.1.1;For=2.2.2.2, For=3.3.3.3`}},
				headerName: "Forwarded",
			},
			// Only the last value is valid, so it should be: {nil, "3.3.3.3"}
			want: []*net.IPAddr{MustParseIPAddr("1.1.1.1"), MustParseIPAddr("3.3.3.3")},
		},
		{
			// An escaped character in quotes should be unescaped, but we're not doing it.
			// (And if we do end up doing it, make sure that `\\` becomes `\` after escaping.
			// And escaping is only allowed in quoted strings.)
			// There is no good reason for any part of an IP address to be escaped anyway.
			name: "Escaped character",
			args: args{
				headers:    http.Header{"Forwarded": []string{`For="3.3.3.\3"`}},
				headerName: "Forwarded",
			},
			// The value is valid, so it should be: {nil, "3.3.3.3"}
			want: []*net.IPAddr{nil},
		},
		{
			// Spaces are not allowed around the equal signs, but due to the way we parse
			// a space after the equal will pass but one before won't.
			name: "Equal sign spaces",
			args: args{
				headers:    http.Header{"Forwarded": []string{`For =1.1.1.1, For= 3.3.3.3`}},
				headerName: "Forwarded",
			},
			// Neither value is valid, so it should be: {nil, nil}
			want: []*net.IPAddr{nil, MustParseIPAddr("3.3.3.3")},
		},
		{
			// Disallowed characters are only allowed in quoted strings. This means
			// that IPv6 addresses must be quoted.
			name: "Disallowed characters in unquoted value (like colons and square brackets",
			args: args{
				headers:    http.Header{"Forwarded": []string{`For=[2607:f8b0:4004:83f::200e]`}},
				headerName: "Forwarded",
			},
			// Value is invalid without quotes, so should be {nil}
			want: []*net.IPAddr{MustParseIPAddr("2607:f8b0:4004:83f::200e")},
		},
		{
			// IPv6 addresses are required to be contained in square brackets. We don't
			// require this simply to be more flexible in what is accepted.
			name: "IPv6 brackets",
			args: args{
				headers:    http.Header{"Forwarded": []string{`For="2607:f8b0:4004:83f::200e"`}},
				headerName: "Forwarded",
			},
			// IPv6 is invalid without brackets, so should be {nil}
			want: []*net.IPAddr{MustParseIPAddr("2607:f8b0:4004:83f::200e")},
		},
		{
			// IPv4 addresses are _not_ supposed to be in square brackets, but we trim
			// them unconditionally.
			name: "IPv4 brackets",
			args: args{
				headers:    http.Header{"Forwarded": []string{`For="[1.1.1.1]"`}},
				headerName: "Forwarded",
			},
			// IPv4 is invalid with brackets, so should be {nil}
			want: []*net.IPAddr{MustParseIPAddr("1.1.1.1")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getIPAddrList(tt.args.headers, tt.args.headerName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func ipAddrsEqual(a, b net.IPAddr) bool {
	return a.IP.Equal(b.IP) && a.Zone == b.Zone
}
