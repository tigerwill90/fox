// The code in this package is derivative of https://github.com/realclientip/realclientip-go (all credit to Adam Pritchard).
// Mount of this source code is governed by a BSD Zero Clause License that can be found
// at https://github.com/realclientip/realclientip-go/blob/main/LICENSE.

package clientip

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"testing"

	"github.com/fox-toolkit/fox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteAddr_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("X-Forwarded-For", "1.1.1.1, 2001:db8:cafe::99%eth0, 3.3.3.3, 192.168.1.1")
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(w, req)

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
			wantErr:  ErrInvalidIPAddress,
		},
		{
			// This is for coverage. It should not be possible for RemoteAddr.
			name:     "should return an an unspecified ip address error",
			remoteIP: "0.0.0.0",
			wantErr:  ErrUnspecifiedIPAddress,
		},
	}

	s := NewRemoteAddr()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c.Request().RemoteAddr = tc.remoteIP
			ipAddr, err := s.ClientIP(c)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			assert.Equal(t, tc.wantIP, ipAddr.WithZone("").String())
			assert.Equal(t, tc.wantZone, ipAddr.Zone())

		})
	}

}

func TestSingleIPHeader_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("X-Real-IP", "4.4.4.4")
	req.Header.Add("X-Real-IP", "5.5.5.5")
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(w, req)

	s := must(NewSingleIPHeader("X-Real-IP"))
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "5.5.5.5", ipAddr.String())

	c.Request().Header.Del("X-Real-IP")
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrSingleIPHeader)

	c.Request().Header.Set("X-Real-IP", "not-an-ip")
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrSingleIPHeader)
	assert.ErrorIs(t, err, ErrInvalidIPAddress)
}

func TestLeftmostNonPrivate_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("Forwarded", `For=fe80::abcd;By=fe80::1234, Proto=https;For=::ffff:188.0.2.128, For="[2001:db8:cafe::17]:4848", For=fc00::1`)
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(w, req)

	s := must(NewLeftmostNonPrivate(ForwardedKey, 100, ExcludeLoopback(true), ExcludeLinkLocal(true), ExcludePrivateNet(true)))
	assert.ElementsMatch(t, privateAndLocalRanges, s.excludedRanges)
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "188.0.2.128", ipAddr.String())

	// Only private IP address
	req.Header.Set("Forwarded", `for=192.168.1.1, for=10.0.0.1, for="[fd00::1]", for=172.16.0.1`)
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrLeftmostNonPrivate)

	// IPv4-mapped IPv6 private address is excluded
	req.Header.Set("Forwarded", `for="[::ffff:10.0.0.1]", for=1.1.1.1`)
	ipAddr, err = s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ipAddr.String())

	// Zoned link local address is excluded
	req.Header.Set("Forwarded", `for="[fe80::abcd%eth0]", for=1.1.1.1`)
	ipAddr, err = s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ipAddr.String())
}

func TestRightmostNonPrivate_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("X-Forwarded-For", "1.1.1.1, 2001:db8:cafe::99%eth0, 3.3.3.3, 192.168.1.1")
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(w, req)
	s := must(NewRightmostNonPrivate(XForwardedForKey, TrustLoopback(true), TrustLinkLocal(true), TrustPrivateNet(true)))
	assert.ElementsMatch(t, privateAndLocalRanges, s.trustedRanges)
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
	assert.ErrorIs(t, err, ErrRightmostNonPrivate)

	// IPv4-mapped IPv6 private address is trusted
	req.Header.Set("X-Forwarded-For", "2.2.2.2, ::ffff:192.168.1.1")
	ipAddr, err = s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "2.2.2.2", ipAddr.String())

	// Zoned link local address is trusted
	req.Header.Set("X-Forwarded-For", "2.2.2.2, fe80::abcd%eth0")
	ipAddr, err = s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "2.2.2.2", ipAddr.String())
}

func TestRightmostTrustedCount_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("Forwarded", `For=fe80::abcd;By=fe80::1234, Proto=https;For=::ffff:188.0.2.128, For="[2001:db8:cafe::17]:4848", For=fc00::1`)
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(w, req)
	s := must(NewRightmostTrustedCount(ForwardedKey, 2))
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "2001:db8:cafe::17", ipAddr.String())

	req.Header.Set("Forwarded", `For=fc00::1`)
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrRightmostTrustedCount)
	assert.ErrorContains(t, err, "expected at least 2 IP(s)")

	req.Header.Set("Forwarded", `For=fe80::abcd;By=fe80::1234, Proto=https;For=::ffff:188.0.2.128, For="invalid", For=fc00::1`)
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrRightmostTrustedCount)
	assert.ErrorContains(t, err, "invalid IP address from the first trusted proxy")

	req.Header.Set("Forwarded", `for=8.8.4.4;host="x,for=10.0.0.5;a=b";proto=https, for=fc00::1`)
	ipAddr, err = s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "8.8.4.4", ipAddr.String())
}

func TestRightmostTrustedRange_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("X-Forwarded-For", "1.1.1.1, 2001:db8:cafe::99%eth0, 3.3.3.3, 192.168.1.1")
	// The connecting peer must be a trusted proxy for the header to be walked.
	req.RemoteAddr = "192.168.1.5:4444"
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(w, req)
	trustedRanges, _ := ParsePrefixes([]string{"192.168.0.0/16", "3.3.3.3"}...)
	s := must(NewRightmostTrustedRange(XForwardedForKey, TrustedIPRangeFunc(func() ([]netip.Prefix, error) {
		return trustedRanges, nil
	})))
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "2001:db8:cafe::99%eth0", ipAddr.String())

	// Invalid IP
	req.Header.Set("X-Forwarded-For", "1.1.1.1, invalid, 3.3.3.3, 192.168.1.1")
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrRightmostTrustedRange)
	assert.ErrorContains(t, err, "unable to find a valid IP address")

	req.Header.Set("X-Forwarded-For", "192.168.1.2, 3.3.3.3, 192.168.1.1")
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrRightmostTrustedRange)
	assert.ErrorContains(t, err, "unable to find a valid IP address")

	req.Header.Set("X-Forwarded-For", "203.0.113.99, 10.0.0.5")
	c.Request().RemoteAddr = "10.0.0.9:333"
	s.resolver = TrustedIPRangeFunc(func() ([]netip.Prefix, error) {
		return []netip.Prefix{netip.MustParsePrefix("::ffff:10.0.0.0/104")}, nil
	})
	ipAddr, err = s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.99", ipAddr.String())

	// Untrusted peer: the (spoofed) header is ignored and the peer itself is returned.
	c.Request().RemoteAddr = "9.9.9.9:1234"
	ipAddr, err = s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "9.9.9.9", ipAddr.String())

	var resolverErr = errors.New("resolver error")
	s.resolver = TrustedIPRangeFunc(func() ([]netip.Prefix, error) {
		return nil, resolverErr
	})
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrRightmostTrustedRange)
	assert.ErrorIs(t, err, resolverErr)

	assert.Panics(t, func() {
		s = must(NewRightmostTrustedRange(XForwardedForKey, nil))
	})
}

func TestTrustedPeer_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	// Same forged header for every scenario; only the peer differs.
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1")
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(w, req)
	trustedRanges, _ := ParsePrefixes("10.0.0.0/8")
	s := must(NewTrustedPeer(TrustedIPRangeFunc(func() ([]netip.Prefix, error) {
		return trustedRanges, nil
	}), must(NewRightmostTrustedCount(XForwardedForKey, 2))))

	// Proxied request: the gate delegates to the inner resolver.
	c.Request().RemoteAddr = "10.0.0.9:555"
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3.4", ipAddr.String())

	// Direct connection: the gate refuses without consulting the header.
	c.Request().RemoteAddr = "9.9.9.9:1234"
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrTrustedPeer)
	assert.ErrorContains(t, err, "untrusted connecting peer")

	// Chained with RemoteAddr, a direct client gets its own (unspoofable) IP,
	// NOT the "1.2.3.4" the attacker tried to inject.
	chain := NewChain(s, NewRemoteAddr())
	ipAddr, err = chain.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "9.9.9.9", ipAddr.String())

	var resolverErr = errors.New("resolver error")
	s.resolver = TrustedIPRangeFunc(func() ([]netip.Prefix, error) {
		return nil, resolverErr
	})
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrTrustedPeer)
	assert.ErrorIs(t, err, resolverErr)
}

func TestChain_ClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Add("X-Real-IP", "4.4.4.4")
	req.RemoteAddr = "192.0.2.1:8080"
	w := httptest.NewRecorder()

	c := fox.NewTestContextOnly(w, req)
	s := NewChain(
		must(NewSingleIPHeader("Cf-Connecting-IP")),
		NewRemoteAddr(),
	)
	ipAddr, err := s.ClientIP(c)
	require.NoError(t, err)
	assert.Equal(t, "192.0.2.1", ipAddr.String())

	// Invalid remote ip
	req.RemoteAddr = " @"
	_, err = s.ClientIP(c)
	assert.ErrorIs(t, err, ErrSingleIPHeader)
	assert.ErrorIs(t, err, ErrRemoteAddress)
	assert.ErrorIs(t, err, ErrInvalidIPAddress)
	assert.ErrorContains(t, err, "header not found")

	ipAddr, err = NewChain().ClientIP(c)
	assert.ErrorIs(t, err, fox.ErrNoClientIPResolver)
	assert.Equal(t, netip.Addr{}, ipAddr)
}

func TestParsePrefixes(t *testing.T) {
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
			name:    "Error: address with zone",
			ranges:  []string{"fe80::abcd%nope"},
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
			got, err := ParsePrefixes(tt.ranges...)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			if err != nil {
				// We can't continue
				return
			}

			require.Equal(t, len(tt.want), len(got))
			for i := range got {
				if got[i].String() != tt.want[i] {
					assert.Equal(t, tt.want[i], got[i].String())
				}
			}
		})
	}
}

func Test_mustParseAddr(t *testing.T) {
	// We test the non-panic path elsewhere, but we need to specifically check the panic case
	assert.Panics(t, func() {
		mustParseAddr("nope")
	})
}

func TestParseAddr(t *testing.T) {
	tests := []struct {
		name    string
		ipStr   string
		want    netip.Addr
		wantErr bool
	}{
		{
			name:  "No zone",
			ipStr: "1.1.1.1",
			want:  netip.MustParseAddr("1.1.1.1"),
		},
		{
			name:  "With zone",
			ipStr: "fe80::abcd%zone",
			want:  netip.MustParseAddr("fe80::abcd%zone"),
		},
		{
			name:  "With zone and port",
			ipStr: "[2607:f8b0:4004:83f::200e%zone]:4484",
			want:  netip.MustParseAddr("2607:f8b0:4004:83f::200e%zone"),
		},
		{
			name:  "With port",
			ipStr: "1.1.1.1:48944",
			want:  netip.MustParseAddr("1.1.1.1"),
		},
		{
			name:  "Bad port (is discarded)",
			ipStr: "[fe80::abcd%eth0]:xyz",
			want:  netip.MustParseAddr("fe80::abcd%eth0"),
		},
		{
			name:  "IPv4-mapped IPv6",
			ipStr: "::ffff:188.0.2.128",
			want:  netip.MustParseAddr("188.0.2.128"),
		},
		{
			name:  "IPv4-mapped IPv6 with port",
			ipStr: "[::ffff:188.0.2.128]:48944",
			want:  netip.MustParseAddr("188.0.2.128"),
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
			name:    "IPv4-mapped unspecified address",
			ipStr:   "::ffff:0.0.0.0",
			wantErr: true,
		},
		{
			name:    "Unspecified address with zone",
			ipStr:   "::%eth0",
			wantErr: true,
		},
		{
			name:    "Error: empty zone",
			ipStr:   "1.1.1.1%",
			wantErr: true,
		},
		{
			name:    "Error: zoned IPv4",
			ipStr:   "1.1.1.1%eth0",
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
			got, err := ParseAddr(tt.ipStr)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
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
		want netip.Addr
	}{
		{
			// This is the correct form for IPv6 wit port
			name: "IPv6 with port and quotes",
			fwd:  `For="[2607:f8b0:4004:83f::200e]:4711"`,
			want: mustParseAddr("2607:f8b0:4004:83f::200e"),
		},
		{
			// This is the correct form for IP with no port
			name: "IPv6 with quotes, brackets and no port",
			fwd:  `fOR="[2607:f8b0:4004:83f::200e]"`,
			want: mustParseAddr("2607:f8b0:4004:83f::200e"),
		},
		{
			// RFC deviation: missing brackets
			name: "IPv6 with quotes, no brackets, and no port",
			fwd:  `for="2607:f8b0:4004:83f::200e"`,
			want: mustParseAddr("2607:f8b0:4004:83f::200e"),
		},
		{
			// RFC deviation: missing quotes
			name: "IPv6 with brackets, no quotes, and no port",
			fwd:  `FOR=[2607:f8b0:4004:83f::200e]`,
			want: mustParseAddr("2607:f8b0:4004:83f::200e"),
		},
		{
			// RFC deviation: missing quotes
			name: "IPv6 with port and no quotes",
			fwd:  `For=[2607:f8b0:4004:83f::200e]:4711`,
			want: mustParseAddr("2607:f8b0:4004:83f::200e"),
		},
		{
			name: "IPv6 with port, quotes, and zone",
			fwd:  `For="[fe80::abcd%zone]:4711"`,
			want: mustParseAddr("fe80::abcd%zone"),
		},
		{
			// RFC deviation: missing brackets
			name: "IPv6 with zone, no quotes, no port",
			fwd:  `For="fe80::abcd%zone"`,
			want: mustParseAddr("fe80::abcd%zone"),
		},
		{
			// RFC deviation: missing quotes
			name: "IPv4 with port",
			fwd:  `FoR=192.0.2.60:4711`,
			want: mustParseAddr("192.0.2.60"),
		},
		{
			name: "IPv4 with no port",
			fwd:  `for=192.0.2.60`,
			want: mustParseAddr("192.0.2.60"),
		},
		{
			name: "IPv4 with quotes",
			fwd:  `for="192.0.2.60"`,
			want: mustParseAddr("192.0.2.60"),
		},
		{
			name: "IPv4 with port and quotes",
			fwd:  `for="192.0.2.60:4823"`,
			want: mustParseAddr("192.0.2.60"),
		},
		{
			name: "Error: invalid IPv4",
			fwd:  `for=192.0.2.999`,
			want: netip.Addr{},
		},
		{
			name: "Error: invalid IPv6",
			fwd:  `for="2607:f8b0:4004:83f::999999"`,
			want: netip.Addr{},
		},
		{
			name: "Error: non-IP identifier",
			fwd:  `for="_test"`,
			want: netip.Addr{},
		},
		{
			name: "Error: empty IP value",
			fwd:  `for=`,
			want: netip.Addr{},
		},
		{
			name: "Multiple IPv4 directives",
			fwd:  `by=1.1.1.1; for=2.2.2.2;host=myhost; proto=https`,
			want: mustParseAddr("2.2.2.2"),
		},
		{
			// RFC deviation: missing quotes around IPv6
			name: "Multiple IPv6 directives",
			fwd:  `by=1::1;host=myhost;for=2::2;proto=https`,
			want: mustParseAddr("2::2"),
		},
		{
			// RFC deviation: missing quotes around IPv6
			name: "Multiple mixed directives",
			fwd:  `by=1::1;host=myhost;proto=https;for=2.2.2.2`,
			want: mustParseAddr("2.2.2.2"),
		},
		{
			name: "IPv4-mapped IPv6",
			fwd:  `for="[::ffff:188.0.2.128]"`,
			want: mustParseAddr("188.0.2.128"),
		},
		{
			name: "IPv4-mapped IPv6 with port and quotes",
			fwd:  `for="[::ffff:188.0.2.128]:49428"`,
			want: mustParseAddr("188.0.2.128"),
		},
		{
			name: "IPv4-mapped IPv6 in IPv6 form",
			fwd:  `for="[0:0:0:0:0:ffff:bc15:0006]"`,
			want: mustParseAddr("188.21.0.6"),
		},
		{
			name: "NAT64 IPv4-mapped IPv6",
			fwd:  `for="[64:ff9b::188.0.2.128]"`,
			want: mustParseAddr("64:ff9b::188.0.2.128"),
		},
		{
			name: "IPv4 loopback",
			fwd:  `for=127.0.0.1`,
			want: mustParseAddr("127.0.0.1"),
		},
		{
			name: "IPv6 loopback",
			fwd:  `for="[::1]"`,
			want: mustParseAddr("::1"),
		},
		{
			// RFC deviation: quotes must be matched
			name: "Error: Unmatched quote",
			fwd:  `for="1.1.1.1`,
			want: netip.Addr{},
		},
		{
			// RFC deviation: brackets must be matched
			name: "Error: IPv6 loopback",
			fwd:  `for="::1]"`,
			want: netip.Addr{},
		},
		{
			name: "Error: misplaced quote",
			fwd:  `for="[0:0:0:0:0:ffff:bc15:0006"]`,
			want: netip.Addr{},
		},
		{
			name: "Error: garbage",
			fwd:  "ads\x00jkl&#*(383fdljk",
			want: netip.Addr{},
		},
		{
			// Per RFC 7230 section 3.2.6, this should not be an error, but we don't have
			// full syntax support yet.
			name: "RFC deviation: quoted pair",
			fwd:  `for=1.1.1.\1`,
			want: netip.Addr{},
		},
		{
			// Per RFC 7239, this extraneous whitespace should be an error, but we don't
			// have full syntax support yet.
			name: "RFC deviation: Incorrect whitespace",
			fwd:  `for= 1.1.1.1`,
			want: mustParseAddr("1.1.1.1"),
		},
		{
			name: "Semicolon in quoted value",
			fwd:  `proto=https;host="x;for=10.0.0.5;a=b";for=8.8.4.4`,
			want: mustParseAddr("8.8.4.4"),
		},
		{
			name: "Escaped quote in quoted value",
			fwd:  `host="x\";for=10.0.0.5";for=8.8.4.4`,
			want: mustParseAddr("8.8.4.4"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseForwardedListItem(tt.fwd)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_backwardAddrSeq_Forwarded(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   []netip.Addr
	}{
		{
			name:   "comma in quoted value",
			values: []string{`for=1.1.1.1, for=8.8.4.4;host="x,for=10.0.0.5;a=b";proto=https`},
			want:   []netip.Addr{mustParseAddr("8.8.4.4"), mustParseAddr("1.1.1.1")},
		},
		{
			name:   "escaped quote in quoted value",
			values: []string{`for=1.1.1.1, for=8.8.4.4;host="x\",for=10.0.0.5"`},
			want:   []netip.Addr{mustParseAddr("8.8.4.4"), mustParseAddr("1.1.1.1")},
		},
		{
			name:   "unbalanced quote before trusted elements",
			values: []string{`for="1.1.1.1, for=2.2.2.2, for=8.8.4.4`},
			want:   []netip.Addr{mustParseAddr("8.8.4.4"), mustParseAddr("2.2.2.2"), {}},
		},
		{
			name:   "unbalanced quote at the end of a client element",
			values: []string{`for=1.1.1.1", for=8.8.4.4`},
			want:   []netip.Addr{mustParseAddr("8.8.4.4"), {}},
		},
		{
			name:   "multiple header values",
			values: []string{`for=1.1.1.1, for="[2001:db8::1]:80"`, `for=8.8.4.4;host="a,b"`},
			want:   []netip.Addr{mustParseAddr("8.8.4.4"), mustParseAddr("2001:db8::1"), mustParseAddr("1.1.1.1")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.Collect(backwardAddrSeq(tt.values, forwardedHdr))
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_indexUnquoted(t *testing.T) {
	tests := []struct {
		s    string
		want int
	}{
		{s: `a;b`, want: 1},
		{s: `"a;b";c`, want: 5},
		{s: `"a\";b";c`, want: 7},
		{s: `"a;b"`, want: -1},
		{s: `a"b;c`, want: -1},
	}
	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			assert.Equal(t, tt.want, indexUnquoted(tt.s, ';'))
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
		want []netip.Addr
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
			want: []netip.Addr{{}, mustParseAddr("2.2.2.2"), {}, mustParseAddr("4.4.4.4")},
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
			want: []netip.Addr{{}, mustParseAddr("2.2.2.2")},
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
			want: []netip.Addr{mustParseAddr("1.1.1.1"), mustParseAddr("2.2.2.2")},
		},
		{
			// The duplicate "For=" parameter should invalidate the whole item but we don't check for it
			name: "Duplicate token",
			args: args{
				headers:    http.Header{"Forwarded": []string{`For=1.1.1.1;For=2.2.2.2, For=3.3.3.3`}},
				headerName: "Forwarded",
			},
			// Only the last value is valid, so it should be: {nil, "3.3.3.3"}
			want: []netip.Addr{mustParseAddr("1.1.1.1"), mustParseAddr("3.3.3.3")},
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
			want: []netip.Addr{{}},
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
			want: []netip.Addr{{}, mustParseAddr("3.3.3.3")},
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
			want: []netip.Addr{mustParseAddr("2607:f8b0:4004:83f::200e")},
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
			want: []netip.Addr{mustParseAddr("2607:f8b0:4004:83f::200e")},
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
			want: []netip.Addr{mustParseAddr("1.1.1.1")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.Collect(addrSeq(tt.args.headers[tt.args.headerName], tt.args.headerName))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewRemoteAddr(t *testing.T) {
	var _ fox.ClientIPResolver = RemoteAddr{}

	type args struct {
		headers    http.Header
		remoteAddr string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "IPv4 with port",
			args: args{
				remoteAddr: "2.2.2.2:1234",
			},
			want: "2.2.2.2",
		},
		{
			name: "IPv4 with no port",
			args: args{
				remoteAddr: "2.2.2.2",
			},
			want: "2.2.2.2",
		},
		{
			name: "IPv6 with port",
			args: args{
				remoteAddr: "[2607:f8b0:4004:83f::18]:3838",
			},
			want: "2607:f8b0:4004:83f::18",
		},
		{
			name: "IPv6 with no port",
			args: args{
				remoteAddr: "2607:f8b0:4004:83f::18",
			},
			want: "2607:f8b0:4004:83f::18",
		},
		{
			name: "IPv6 with zone and no port",
			args: args{
				remoteAddr: `fe80::1111%eth0`,
			},
			want: `fe80::1111%eth0`,
		},
		{
			name: "IPv6 with zone and port",
			args: args{
				remoteAddr: `[fe80::2222%eth0]:4848`,
			},
			want: `fe80::2222%eth0`,
		},
		{
			name: "IPv4-mapped IPv6",
			args: args{
				remoteAddr: "[::ffff:172.21.0.6]:4747",
			},
			// It is okay that this converts to the IPv4 format, since it's most important
			// that the respresentation be consistent. It might also be good that it does,
			// so that it will match the same plain IPv4 address.
			// net/netip.ParseAddr gives a different form: "::ffff:172.21.0.6"
			want: "172.21.0.6",
		},
		{
			name: "IPv4-mapped IPv6 in IPv6 form",
			args: args{
				remoteAddr: "0:0:0:0:0:ffff:bc15:0006",
			},
			// net/netip.ParseAddr gives a different form: "::ffff:188.21.0.6"
			want: "188.21.0.6",
		},
		{
			name: "NAT64 IPv4-mapped IPv6",
			args: args{
				remoteAddr: "[64:ff9b::188.0.2.128]:4747",
			},
			// net.ParseIP and net/netip.ParseAddr convert to this. This is fine, as it is
			// done consistently.
			want: "64:ff9b::bc00:280",
		},
		{
			name: "6to4 IPv4-mapped IPv6",
			args: args{
				remoteAddr: "[2002:c000:204::]:4747",
			},
			want: "2002:c000:204::",
		},
		{
			name: "IPv4 loopback",
			args: args{
				remoteAddr: "127.0.0.1",
			},
			want: "127.0.0.1",
		},
		{
			name: "IPv6 loopback",
			args: args{
				remoteAddr: "::1",
			},
			want: "::1",
		},
		{
			name: "Garbage header (unused)",
			args: args{
				headers:    http.Header{"X-Forwarded-For": []string{"!!!"}},
				remoteAddr: "2.2.2.2:1234",
			},
			want: "2.2.2.2",
		},
		{
			name: "Fail: empty RemoteAddr",
			args: args{
				remoteAddr: "",
			},
			want: "",
		},
		{
			name: "Fail: garbage RemoteAddr",
			args: args{
				remoteAddr: "ohno",
			},
			want: "",
		},
		{
			name: "Fail: zero RemoteAddr IP",
			args: args{
				remoteAddr: "0.0.0.0",
			},
			want: "",
		},
		{
			name: "Fail: unspecified RemoteAddr IP",
			args: args{
				remoteAddr: "::",
			},
			want: "",
		},
		{
			name: "Fail: Unix domain socket",
			args: args{
				remoteAddr: "@",
			},
			want: "",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	w := httptest.NewRecorder()
	c := fox.NewTestContextOnly(w, req)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := RemoteAddr{}
			c.Request().Header = tt.args.headers
			c.Request().RemoteAddr = tt.args.remoteAddr
			ipAddr, err := s.ClientIP(c)
			if tt.want == "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, ipAddr.String())
		})
	}
}

func TestNewSingleIPHeader(t *testing.T) {
	var _ fox.ClientIPResolver = SingleIPHeader{}

	type args struct {
		headerName string
		headers    http.Header
		remoteAddr string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "IPv4 with port",
			args: args{
				headerName: "True-Client-IP",
				headers: http.Header{
					"X-Real-Ip":       []string{"1.1.1.1"},
					"True-Client-Ip":  []string{"2.2.2.2:49489"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "2.2.2.2",
		},
		{
			name: "IPv4 with no port",
			args: args{
				headerName: "X-Real-IP",
				headers: http.Header{
					"X-Real-Ip":       []string{"1.1.1.1"},
					"True-Client-Ip":  []string{"2.2.2.2:49489"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "1.1.1.1",
		},
		{
			name: "IPv6 with port",
			args: args{
				headerName: "X-Real-IP",
				headers: http.Header{
					"X-Real-Ip":       []string{"[2607:f8b0:4004:83f::18]:3838"},
					"True-Client-Ip":  []string{"2.2.2.2:49489"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "2607:f8b0:4004:83f::18",
		},
		{
			name: "IPv6 with no port",
			args: args{
				headerName: "X-Real-IP",
				headers: http.Header{
					"X-Real-Ip":       []string{"2607:f8b0:4004:83f::19"},
					"True-Client-Ip":  []string{"2.2.2.2:49489"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "2607:f8b0:4004:83f::19",
		},
		{
			name: "IPv6 with zone and no port",
			args: args{
				headerName: "a-b-c-d",
				headers: http.Header{
					"X-Real-Ip":       []string{"2607:f8b0:4004:83f::19"},
					"A-B-C-D":         []string{"fe80::1111%zone"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "fe80::1111%zone",
		},
		{
			name: "IPv6 with zone and port",
			args: args{
				headerName: "a-b-c-d",
				headers: http.Header{
					"X-Real-Ip":       []string{"2607:f8b0:4004:83f::19"},
					"A-B-C-D":         []string{"[fe80::1111%zone]:4848"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "fe80::1111%zone",
		},
		{
			name: "IPv6 with brackets but no port",
			args: args{
				headerName: "x-real-ip",
				headers: http.Header{
					"X-Real-Ip":       []string{"2607:f8b0:4004:83f::19"},
					"A-B-C-D":         []string{"[fe80::1111%zone]:4848"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "2607:f8b0:4004:83f::19",
		},
		{
			name: "IP-mapped IPv6",
			args: args{
				headerName: "x-real-ip",
				headers: http.Header{
					"X-Real-Ip":       []string{"::ffff:172.21.0.6"},
					"A-B-C-D":         []string{"[fe80::1111%zone]:4848"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "172.21.0.6",
		},
		{
			name: "IPv4-mapped IPv6 in IPv6 form",
			args: args{
				headerName: "x-real-ip",
				headers: http.Header{
					"X-Real-Ip":       []string{"[64:ff9b::188.0.2.128]:4747"},
					"A-B-C-D":         []string{"[fe80::1111%zone]:4848"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "64:ff9b::bc00:280",
		},
		{
			name: "6to4 IPv4-mapped IPv6",
			args: args{
				headerName: "x-real-ip",
				headers: http.Header{
					"X-Real-Ip":       []string{"2002:c000:204::"},
					"A-B-C-D":         []string{"[fe80::1111%zone]:4848"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "2002:c000:204::",
		},
		{
			name: "IPv4 loopback",
			args: args{
				headerName: "x-real-ip",
				headers: http.Header{
					"X-Real-Ip":       []string{"127.0.0.1"},
					"A-B-C-D":         []string{"[fe80::1111%zone]:4848"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "127.0.0.1",
		},
		{
			name: "Fail: missing header",
			args: args{
				headerName: "x-real-ip",
				headers: http.Header{
					"A-B-C-D":         []string{"[fe80::1111%zone]:4848"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "",
		},
		{
			name: "Fail: garbage IP",
			args: args{
				headerName: "True-Client-Ip",
				headers: http.Header{
					"X-Real-Ip":       []string{"::1"},
					"True-Client-Ip":  []string{"nope"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "",
		},
		{
			name: "Fail: zero IP",
			args: args{
				headerName: "True-Client-Ip",
				headers: http.Header{
					"X-Real-Ip":       []string{"::1"},
					"True-Client-Ip":  []string{"0.0.0.0"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			want: "",
		},
		{
			name: "Error: empty header name",
			args: args{
				headerName: "",
				headers: http.Header{
					"X-Real-Ip":       []string{"::1"},
					"True-Client-Ip":  []string{"2.2.2.2"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			wantErr: true,
		},
		{
			name: "Error: X-Forwarded-For header",
			args: args{
				headerName: "X-Forwarded-For",
				headers: http.Header{
					"X-Real-Ip":       []string{"::1"},
					"True-Client-Ip":  []string{"2.2.2.2"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			wantErr: true,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	w := httptest.NewRecorder()
	c := fox.NewTestContextOnly(w, req)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s fox.ClientIPResolver
			if tt.wantErr {
				require.Panics(t, func() {
					s = must(NewSingleIPHeader(tt.args.headerName))
				})
				return
			}

			s = must(NewSingleIPHeader(tt.args.headerName))

			c.Request().Header = tt.args.headers
			c.Request().RemoteAddr = tt.args.remoteAddr
			ipAddr, err := s.ClientIP(c)
			if tt.want == "" {
				require.Error(t, err)
				return
			}
			assert.Equal(t, tt.want, ipAddr.String())
		})
	}
}

func TestNewLeftmostNonPrivate(t *testing.T) {
	var _ fox.ClientIPResolver = LeftmostNonPrivate{}

	type args struct {
		headerType HeaderKey
		headers    http.Header
		remoteAddr string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "IPv4 with port",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
				},
			},
			want: "2.2.2.2",
		},
		{
			name: "IPv4 with no port",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`For=5.5.5.5`, `For=6.6.6.6`},
				},
			},
			want: "5.5.5.5",
		},
		{
			name: "IPv6 with port",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`[2607:f8b0:4004:83f::18]:3838, 3.3.3.3`, `4.4.4.4`},
				},
			},
			want: "2607:f8b0:4004:83f::18",
		},
		{
			name: "IPv6 with no port",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`Host=blah;For="2607:f8b0:4004:83f::18";Proto=https`},
				},
			},
			want: "2607:f8b0:4004:83f::18",
		},
		{
			name: "IPv6 with port and zone",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`For=[fe80::1111%zone], Host=blah;For="[2607:f8b0:4004:83f::18%zone]:9943";Proto=https`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "2607:f8b0:4004:83f::18%zone",
		},
		{
			name: "IPv6 with port and zone, no quotes",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`For=[fe80::1111%zone], Host=blah;For=[2607:f8b0:4004:83f::18%zone]:9943;Proto=https`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "2607:f8b0:4004:83f::18%zone",
		},
		{
			name: "IPv4-mapped IPv6",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`::ffff:188.0.2.128, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`Host=blah;For="7.7.7.7";Proto=https`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "188.0.2.128",
		},
		{
			name: "IPv4-mapped IPv6 with port",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`[::ffff:188.0.2.128]:48483, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`Host=blah;For="7.7.7.7";Proto=https`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "188.0.2.128",
		},
		{
			name: "IPv4-mapped IPv6 in IPv6 (hex) form",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`[::ffff:188.0.2.128]:48483, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "188.21.0.6",
		},
		{
			name: "NAT64 IPv4-mapped IPv6",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`64:ff9b::188.0.2.128, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "64:ff9b::bc00:280",
		},
		{
			name: "XFF: leftmost not desirable",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`::1, nope`, `4.4.4.4, 5.5.5.5`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "4.4.4.4",
		},
		{
			name: "Forwarded: leftmost not desirable",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`::1, nope`, `4.4.4.4, 5.5.5.5`},
					"Forwarded":       []string{`For="", For="::ffff:192.168.1.1"`, `host=what;for=:48485;proto=https,For="2607:f8b0:4004:83f::18"`},
				},
			},
			want: "2607:f8b0:4004:83f::18",
		},
		{
			name: "Fail: XFF: none acceptable",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`::1, nope, ::, 0.0.0.0`, `192.168.1.1, !?!`},
					"Forwarded":       []string{`For="", For="::ffff:192.168.1.1"`, `host=what;for=:48485;proto=https,For="fe80::abcd%zone"`},
				},
			},
			want: "",
		},
		{
			name: "Fail: Forwarded: none acceptable",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`::1, nope`, `192.168.1.1, 2.2.2.2`},
					"Forwarded":       []string{`For="", For="::ffff:192.168.1.1"`, `host=what;for=:48485;proto=https,For="::ffff:ac15:0006%zone",For="::",For=0.0.0.0`},
				},
			},
			want: "",
		},
		{
			name: "Fail: XFF: no header",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip": []string{`1.1.1.1`},
					"Forwarded": []string{`For="", For="::ffff:192.168.1.1"`, `host=what;for=:48485;proto=https,For="::ffff:ac15:0006%zone"`},
				},
			},
			want: "",
		},
		{
			name: "Fail: Forwarded: no header",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`64:ff9b::188.0.2.128, 3.3.3.3`, `4.4.4.4`},
				},
			},
			want: "",
		},
		{
			name: "Error: invalid header key",
			args: args{
				headerType: 3,
				headers: http.Header{
					"X-Real-Ip":       []string{"::1"},
					"True-Client-Ip":  []string{"2.2.2.2"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			wantErr: true,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	w := httptest.NewRecorder()
	c := fox.NewTestContextOnly(w, req)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s fox.ClientIPResolver
			if tt.wantErr {
				require.Panics(t, func() {
					s = must(NewLeftmostNonPrivate(tt.args.headerType, 100))
				})
				return
			}

			s = must(NewLeftmostNonPrivate(tt.args.headerType, 100))

			c.Request().Header = tt.args.headers
			c.Request().RemoteAddr = tt.args.remoteAddr
			ipAddr, err := s.ClientIP(c)
			if tt.want == "" {
				require.Error(t, err)
				return
			}
			assert.Equal(t, tt.want, ipAddr.String())
		})
	}
}

func TestNewLeftmostNonPrivateLimit(t *testing.T) {
	t.Run("limit exactly match the target ip", func(t *testing.T) {
		s := must(NewLeftmostNonPrivate(XForwardedForKey, 3))
		req := httptest.NewRequest(http.MethodGet, "/foo", nil)
		req.Header.Set(fox.HeaderXForwardedFor, "192.168.1.15, 10.8.1.2, 115.45.98.3")
		w := httptest.NewRecorder()
		c := fox.NewTestContextOnly(w, req)
		ip, err := s.ClientIP(c)
		require.NoError(t, err)
		assert.Equal(t, "115.45.98.3", ip.String())
	})
	t.Run("limit under the target ip", func(t *testing.T) {
		s := must(NewLeftmostNonPrivate(XForwardedForKey, 2))
		req := httptest.NewRequest(http.MethodGet, "/foo", nil)
		req.Header.Set(fox.HeaderXForwardedFor, "192.168.1.15, 10.8.1.2, 115.45.98.3")
		w := httptest.NewRecorder()
		c := fox.NewTestContextOnly(w, req)
		_, err := s.ClientIP(c)
		assert.Error(t, err)
	})
}

func TestNewRightmostNonPrivate(t *testing.T) {
	var _ fox.ClientIPResolver = RightmostNonPrivate{}

	type args struct {
		headerType HeaderKey
		headers    http.Header
		remoteAddr string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "IPv4 with port",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4:39333`},
				},
			},
			want: "4.4.4.4",
		},
		{
			name: "IPv4 with no port",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`For=5.5.5.5`, `For=6.6.6.6`},
				},
			},
			want: "6.6.6.6",
		},
		{
			name: "IPv6 with port",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`[2607:f8b0:4004:83f::18]:3838`},
				},
			},
			want: "2607:f8b0:4004:83f::18",
		},
		{
			name: "IPv6 with no port",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`host=what;for=6.6.6.6;proto=https`, `Host=blah;For="2607:f8b0:4004:83f::18";Proto=https`},
				},
			},
			want: "2607:f8b0:4004:83f::18",
		},
		{
			name: "IPv6 with port and zone",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`host=what;for=6.6.6.6;proto=https`, `For="[2607:f8b0:4004:83f::18%eth0]:3393";Proto=https`, `Host=blah;For="[fe80::1111%zone]:9943";Proto=https`},
				},
			},
			want: "2607:f8b0:4004:83f::18%eth0",
		},
		{
			name: "IPv6 with port and zone, no quotes",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`host=what;for=6.6.6.6;proto=https`, `For="[2607:f8b0:4004:83f::18%eth0]:3393";Proto=https`, `Host=blah;For=[fe80::1111%zone]:9943;Proto=https`},
				},
			},
			want: "2607:f8b0:4004:83f::18%eth0",
		},
		{
			name: "IPv4-mapped IPv6",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`3.3.3.3`, `4.4.4.4, ::ffff:188.0.2.128`},
					"Forwarded":       []string{`Host=blah;For="7.7.7.7";Proto=https`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "188.0.2.128",
		},
		{
			name: "IPv4-mapped IPv6 with port",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`3.3.3.3`, `4.4.4.4,[::ffff:188.0.2.128]:48483`},
					"Forwarded":       []string{`Host=blah;For="7.7.7.7";Proto=https`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "188.0.2.128",
		},
		{
			name: "IPv4-mapped IPv6 in IPv6 (hex) form",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`[::ffff:188.0.2.128]:48483, 3.3.3.3`, `4.4.4.4`},
					"Forwarded":       []string{`host=what;for=6.6.6.6;proto=https`, `For="::ffff:bc15:0006"`},
				},
			},
			want: "188.21.0.6",
		},
		{
			name: "NAT64 IPv4-mapped IPv6",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`3.3.3.3`, `4.4.4.4, 64:ff9b::188.0.2.128`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "64:ff9b::bc00:280",
		},
		{
			name: "XFF: rightmost not desirable",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`4.4.4.4, 5.5.5.5`, `::1, nope`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "5.5.5.5",
		},
		{
			name: "Forwarded: rightmost not desirable",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`::1, nope`, `4.4.4.4, 5.5.5.5`},
					"Forwarded":       []string{`host=what;for=:48485;proto=https,For=2.2.2.2`, `For="", For="::ffff:192.168.1.1"`},
				},
			},
			want: "2.2.2.2",
		},
		{
			name: "Fail: XFF: none acceptable",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`::1, nope`, `192.168.1.1, !?!, ::, 0.0.0.0`},
					"Forwarded":       []string{`For="", For="::ffff:192.168.1.1"`, `host=what;for=:48485;proto=https,For="fe80::abcd%zone"`},
				},
			},
			want: "",
		},
		{
			name: "Fail: Forwarded: none acceptable",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`::1, nope`, `192.168.1.1, 2.2.2.2`},
					"Forwarded":       []string{`For="", For="::ffff:192.168.1.1"`, `host=what;for=:48485;proto=https,For="::ffff:ac15:0006%zone", For="::", For=0.0.0.0`},
				},
			},
			want: "",
		},
		{
			name: "Fail: XFF: no header",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip": []string{`1.1.1.1`},
					"Forwarded": []string{`For="", For="::ffff:192.168.1.1"`, `host=what;for=:48485;proto=https,For="::ffff:ac15:0006%zone"`},
				},
				remoteAddr: "9.9.9.9",
			},
			want: "",
		},
		{
			name: "Fail: Forwarded: no header",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`64:ff9b::188.0.2.128, 3.3.3.3`, `4.4.4.4`},
				},
			},
			want: "",
		},
		{
			name: "Error: invalid header key",
			args: args{
				headerType: 3,
				headers: http.Header{
					"X-Real-Ip":       []string{"::1"},
					"True-Client-Ip":  []string{"2.2.2.2"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			wantErr: true,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	w := httptest.NewRecorder()
	c := fox.NewTestContextOnly(w, req)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s fox.ClientIPResolver
			if tt.wantErr {
				require.Panics(t, func() {
					s = must(NewRightmostNonPrivate(tt.args.headerType))
				})
				return
			}

			s = must(NewRightmostNonPrivate(tt.args.headerType))

			c.Request().Header = tt.args.headers
			c.Request().RemoteAddr = tt.args.remoteAddr
			ipAddr, err := s.ClientIP(c)
			if tt.want == "" {
				require.Error(t, err)
				return
			}
			assert.Equal(t, tt.want, ipAddr.String())
		})
	}
}

func TestNewRightmostTrustedCount(t *testing.T) {
	var _ fox.ClientIPResolver = RightmostTrustedCount{}

	type args struct {
		headerType   HeaderKey
		trustedCount uint
		headers      http.Header
		remoteAddr   string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "Count one",
			args: args{
				headerType:   ForwardedKey,
				trustedCount: 1,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`4.4.4.4, 5.5.5.5`, `::1, fe80::382b:141b:fa4a:2a16%28`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "6.6.6.6",
		},
		{
			name: "Count five",
			args: args{
				headerType:   XForwardedForKey,
				trustedCount: 5,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`4.4.4.4, 5.5.5.5`, `::1, fe80::382b:141b:fa4a:2a16%28`, `7.7.7.7.7, 8.8.8.8, 9.9.9.9, 10.10.10.10,11.11.11.11, 12.12.12.12`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "8.8.8.8",
		},
		{
			name: "Fail: header too short/count too large",
			args: args{
				headerType:   XForwardedForKey,
				trustedCount: 50,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`4.4.4.4, 5.5.5.5`, `::1, fe80::382b:141b:fa4a:2a16%28`, `7.7.7.7.7, 8.8.8.8, 9.9.9.9, 10.10.10.10,11.11.11.11, 12.12.12.12`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "",
		},
		{
			name: "Fail: bad value at count index",
			args: args{
				headerType:   ForwardedKey,
				trustedCount: 2,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`4.4.4.4, 5.5.5.5`, `::1, fe80::382b:141b:fa4a:2a16%28`, `7.7.7.7.7, 8.8.8.8, 9.9.9.9, 10.10.10.10,11.11.11.11, 12.12.12.12`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `For=nope`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "",
		},
		{
			name: "Fail: zero value at count index",
			args: args{
				headerType:   ForwardedKey,
				trustedCount: 2,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`4.4.4.4, 5.5.5.5`, `::1, fe80::382b:141b:fa4a:2a16%28`, `7.7.7.7.7, 8.8.8.8, 9.9.9.9, 10.10.10.10,11.11.11.11, 12.12.12.12`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `For=0.0.0.0`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			want: "",
		},
		{
			name: "Fail: header missing",
			args: args{
				headerType:   ForwardedKey,
				trustedCount: 1,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`4.4.4.4, 5.5.5.5`, `::1, fe80::382b:141b:fa4a:2a16%28`, `7.7.7.7.7, 8.8.8.8, 9.9.9.9, 10.10.10.10,11.11.11.11, 12.12.12.12`},
				},
			},
			want: "",
		},
		{
			name: "Error: invalid header key",
			args: args{
				headerType:   3,
				trustedCount: 1,
				headers: http.Header{
					"X-Real-Ip":       []string{"::1"},
					"True-Client-Ip":  []string{"2.2.2.2"},
					"X-Forwarded-For": []string{"3.3.3.3"}},
			},
			wantErr: true,
		},
		{
			name: "Error: zero trustedCount",
			args: args{
				headerType:   XForwardedForKey,
				trustedCount: 0,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`4.4.4.4, 5.5.5.5`, `::1, nope`, `fe80::382b:141b:fa4a:2a16%28`},
					"Forwarded":       []string{`For="::ffff:bc15:0006"`, `host=what;for=6.6.6.6;proto=https`},
				},
			},
			wantErr: true,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	w := httptest.NewRecorder()
	c := fox.NewTestContextOnly(w, req)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s fox.ClientIPResolver
			if tt.wantErr {
				require.Panics(t, func() {
					s = must(NewRightmostTrustedCount(tt.args.headerType, tt.args.trustedCount))
				})
				return
			}

			s = must(NewRightmostTrustedCount(tt.args.headerType, tt.args.trustedCount))

			c.Request().Header = tt.args.headers
			c.Request().RemoteAddr = tt.args.remoteAddr
			ipAddr, err := s.ClientIP(c)
			if tt.want == "" {
				require.Error(t, err)
				return
			}
			assert.Equal(t, tt.want, ipAddr.String())
		})
	}
}

func TestNewRightmostTrustedRange(t *testing.T) {
	var _ fox.ClientIPResolver = RightmostTrustedRange{}

	type args struct {
		headerType    HeaderKey
		headers       http.Header
		remoteAddr    string
		trustedRanges []string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "No ranges",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
				},
				trustedRanges: nil,
			},
			want: "4.4.4.4",
		},
		{
			name: "One range",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
				},
				trustedRanges: []string{`4.4.4.0/24`},
			},
			want: "3.3.3.3",
		},
		{
			name: "One IP",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
				},
				trustedRanges: []string{`4.4.4.4`},
			},
			want: "3.3.3.3",
		},
		{
			name: "Many kinds of ranges",
			args: args{
				headerType: ForwardedKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
					"Forwarded": []string{
						`For=99.99.99.99, For=4.4.4.8, For="[2607:f8b0:4004:83f::200e]:4747"`,
						`For=2.2.2.2:8883, For=64:ff9b::188.0.2.200, For=3.3.5.5, For=2001:db7::abcd`,
					},
				},
				trustedRanges: []string{
					`2.2.2.2/32`, `2607:f8b0:4004:83f::200e/128`,
					`3.3.0.0/16`, `2001:db7::/64`,
					`::ffff:4.4.4.4/124`, `64:ff9b::188.0.2.128/112`,
				},
			},
			want: "99.99.99.99",
		},
		{
			name: "Cloudflare ranges",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`, `2400:cb00::1`},
				},
				trustedRanges: []string{
					"173.245.48.0/20",
					"103.21.244.0/22",
					"103.22.200.0/22",
					"103.31.4.0/22",
					"141.101.64.0/18",
					"108.162.192.0/18",
					"190.93.240.0/20",
					"188.114.96.0/20",
					"197.234.240.0/22",
					"198.41.128.0/17",
					"162.158.0.0/15",
					"104.16.0.0/13",
					"104.24.0.0/14",
					"172.64.0.0/13",
					"131.0.72.0/22",
					"2400:cb00::/32",
					"2606:4700::/32",
					"2803:f800::/32",
					"2405:b500::/32",
					"2405:8100::/32",
					"2a06:98c0::/29",
					"2c0f:f248::/32",
				},
			},
			want: "4.4.4.4",
		},
		{
			// Peer is a valid IP outside the trusted ranges: the request did not
			// arrive through a trusted proxy, so the (spoofed) header is ignored and
			// the peer is returned -- NOT the victim the header tries to inject.
			name: "Peer untrusted: returns peer, ignores spoofed header",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Forwarded-For": []string{`1.2.3.4, 10.0.0.1`},
				},
				remoteAddr:    "9.9.9.9:1234",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "9.9.9.9",
		},
		{
			// The direct-client case: untrusted peer, no header.
			name: "Peer untrusted, no header: returns peer",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Forwarded-For": []string{},
				},
				remoteAddr:    "9.9.9.9",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "9.9.9.9",
		},
		{
			// Untrusted IPv6 peer with a zone is returned canonically, zone retained.
			name: "Peer untrusted IPv6 with zone: returns peer with zone",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Forwarded-For": []string{`1.2.3.4`},
				},
				remoteAddr:    "[fe80::1%eth0]:9999",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "fe80::1%eth0",
		},
		{
			// Trusted peer: behaves exactly as the header-only walk (regression guard
			// that normal proxy deployments are unaffected by peer verification).
			name: "Peer trusted: walks header as normal",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3, 10.0.0.1`},
				},
				remoteAddr:    "10.0.0.5:555",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "3.3.3.3",
		},
		{
			// Unix domain socket peer ("@") is not an IP; fall through to the header.
			name: "Peer Unix socket: walks header as normal",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3, 10.0.0.1`},
				},
				remoteAddr:    "@",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "3.3.3.3",
		},
		{
			name: "Peer garbage: walks header as normal",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3, 10.0.0.1`},
				},
				remoteAddr:    "ohno",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "3.3.3.3",
		},
		{
			// Unspecified peer IP (0.0.0.0) is rejected by ParseAddr, so we cannot
			// classify it and fall through to the header.
			name: "Peer unspecified IP: walks header as normal",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3, 10.0.0.1`},
				},
				remoteAddr:    "0.0.0.0",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "3.3.3.3",
		},
		{
			name: "Fail: no non-trusted IP",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 2.2.2.3`, `2.2.2.4`},
				},
				trustedRanges: []string{`2.2.2.0/24`},
			},
			want: "",
		},
		{
			name: "Fail: rightmost non-trusted IP invalid",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`nope, 2.2.2.2:3384, 2.2.2.3`, `2.2.2.4`},
				},
				trustedRanges: []string{`2.2.2.0/24`},
			},
			want: "",
		},
		{
			name: "Fail: rightmost non-trusted IP unspecified",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`::, 2.2.2.2:3384, 2.2.2.3`, `2.2.2.4`},
				},
				trustedRanges: []string{`2.2.2.0/24`},
			},
			want: "",
		},
		{
			name: "Fail: no values in header",
			args: args{
				headerType: XForwardedForKey,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{}},
				trustedRanges: []string{`2.2.2.0/24`},
			},
			want: "",
		},
		{
			name: "Error: invalid header key",
			args: args{
				headerType: 3,
				headers: http.Header{
					"X-Real-Ip":       []string{`1.1.1.1`},
					"X-Forwarded-For": []string{`2.2.2.2:3384, 3.3.3.3`, `4.4.4.4`},
				},
				trustedRanges: nil,
			},
			wantErr: true,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	w := httptest.NewRecorder()
	c := fox.NewTestContextOnly(w, req)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			ranges, err := ParsePrefixes(tt.args.trustedRanges...)
			if err != nil {
				// We're not testing ParsePrefixes here
				t.Fatalf("ParsePrefixes failed")
			}

			var s fox.ClientIPResolver
			if tt.wantErr {
				require.Panics(t, func() {
					s = must(NewRightmostTrustedRange(tt.args.headerType, TrustedIPRangeFunc(func() ([]netip.Prefix, error) {
						return ranges, nil
					})))
				})
				return
			}

			s = must(NewRightmostTrustedRange(tt.args.headerType, TrustedIPRangeFunc(func() ([]netip.Prefix, error) {
				return ranges, nil
			})))

			c.Request().Header = tt.args.headers
			c.Request().RemoteAddr = tt.args.remoteAddr
			ipAddr, err := s.ClientIP(c)
			if tt.want == "" {
				require.Error(t, err)
				return
			}
			assert.Equal(t, tt.want, ipAddr.String())
		})
	}
}

func TestNewTrustedPeer(t *testing.T) {
	var _ fox.ClientIPResolver = TrustedPeer{}

	type args struct {
		inner         fox.ClientIPResolver
		headers       http.Header
		remoteAddr    string
		trustedRanges []string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			// Peer is a trusted proxy: the inner resolver is consulted as normal.
			name: "Trusted peer: delegates to inner (Count)",
			args: args{
				inner: must(NewRightmostTrustedCount(XForwardedForKey, 2)),
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3, 10.0.0.1`},
				},
				remoteAddr:    "10.0.0.5:555",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "3.3.3.3",
		},
		{
			// The core fix: peer is a valid IP outside the trusted ranges, so the request
			// did not arrive through a proxy. The (spoofed) header is NOT consulted.
			name: "Untrusted peer: refuses, ignores spoofed header (Count)",
			args: args{
				inner: must(NewRightmostTrustedCount(XForwardedForKey, 2)),
				headers: http.Header{
					"X-Forwarded-For": []string{`1.2.3.4, 10.0.0.1`},
				},
				remoteAddr:    "9.9.9.9:1234",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "",
		},
		{
			// The same gap exists for SingleIPHeader; the wrapper protects it too.
			name: "Untrusted peer: refuses, ignores spoofed header (SingleIPHeader)",
			args: args{
				inner: must(NewSingleIPHeader("X-Real-IP")),
				headers: http.Header{
					"X-Real-Ip": []string{`1.2.3.4`},
				},
				remoteAddr:    "9.9.9.9",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "",
		},
		{
			name: "Trusted peer: delegates to inner (SingleIPHeader)",
			args: args{
				inner: must(NewSingleIPHeader("X-Real-IP")),
				headers: http.Header{
					"X-Real-Ip": []string{`1.2.3.4`},
				},
				remoteAddr:    "10.0.0.5:5",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "1.2.3.4",
		},
		{
			name: "Trusted IPv6 peer: delegates to inner",
			args: args{
				inner: must(NewRightmostTrustedCount(XForwardedForKey, 1)),
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3`},
				},
				remoteAddr:    "[fc00::1]:5",
				trustedRanges: []string{`fc00::/7`},
			},
			want: "3.3.3.3",
		},
		{
			// Untrusted IPv6 peer (with a zone) is classified and refused.
			name: "Untrusted IPv6 peer with zone: refuses",
			args: args{
				inner: must(NewRightmostTrustedCount(XForwardedForKey, 1)),
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3`},
				},
				remoteAddr:    "[fe80::1%eth0]:9999",
				trustedRanges: []string{`fc00::/7`},
			},
			want: "",
		},
		{
			// Unix domain socket peer ("@") is not an IP and cannot be a network spoofing
			// vector, so the peer cannot be classified and the inner resolver is consulted.
			name: "Peer Unix socket: delegates to inner",
			args: args{
				inner: must(NewRightmostTrustedCount(XForwardedForKey, 1)),
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3, 4.4.4.4`},
				},
				remoteAddr:    "@",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "4.4.4.4",
		},
		{
			name: "Peer empty: delegates to inner",
			args: args{
				inner: must(NewRightmostTrustedCount(XForwardedForKey, 1)),
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3, 4.4.4.4`},
				},
				remoteAddr:    "",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "4.4.4.4",
		},
		{
			name: "Peer garbage: delegates to inner",
			args: args{
				inner: must(NewRightmostTrustedCount(XForwardedForKey, 1)),
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3, 4.4.4.4`},
				},
				remoteAddr:    "ohno",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "4.4.4.4",
		},
		{
			// Unspecified peer IP (0.0.0.0) is rejected by ParseAddr, so we cannot
			// classify it and delegate to the inner resolver.
			name: "Peer unspecified IP: delegates to inner",
			args: args{
				inner: must(NewRightmostTrustedCount(XForwardedForKey, 1)),
				headers: http.Header{
					"X-Forwarded-For": []string{`3.3.3.3, 4.4.4.4`},
				},
				remoteAddr:    "0.0.0.0",
				trustedRanges: []string{`10.0.0.0/8`},
			},
			want: "4.4.4.4",
		},
		{
			name: "Error: nil inner",
			args: args{
				inner:         nil,
				trustedRanges: []string{`10.0.0.0/8`},
			},
			wantErr: true,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	w := httptest.NewRecorder()
	c := fox.NewTestContextOnly(w, req)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			ranges, err := ParsePrefixes(tt.args.trustedRanges...)
			if err != nil {
				// We're not testing ParsePrefixes here
				t.Fatalf("ParsePrefixes failed")
			}

			var s fox.ClientIPResolver
			if tt.wantErr {
				require.Panics(t, func() {
					s = must(NewTrustedPeer(TrustedIPRangeFunc(func() ([]netip.Prefix, error) {
						return ranges, nil
					}), tt.args.inner))
				})
				return
			}

			s = must(NewTrustedPeer(TrustedIPRangeFunc(func() ([]netip.Prefix, error) {
				return ranges, nil
			}), tt.args.inner))

			c.Request().Header = tt.args.headers
			c.Request().RemoteAddr = tt.args.remoteAddr
			ipAddr, err := s.ClientIP(c)
			if tt.want == "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, ipAddr.String())
		})
	}

	assert.Panics(t, func() {
		must(NewTrustedPeer(nil, must(NewRightmostTrustedCount(XForwardedForKey, 1))))
	})
}

// Test_isContainedInRanges validates the bundled privateAndLocalRanges against the RFCs,
// not just against itself.
func Test_isContainedInRanges(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{
			name: "bad (zero) addr",
			ip:   `nope`,
			want: false,
		},
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
		{
			// RFC 2544 benchmarking range is 198.18.0.0/15.
			name: "IPv4 RFC 2544 benchmarking",
			ip:   `198.18.5.5`,
			want: true,
		},
		{
			// 192.18.0.0/15 is routable public space, not a reserved range.
			// Guards against the 198.18/192.18 typo.
			name: "IPv4 192.18.0.0/15 is public",
			ip:   `192.18.5.5`,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, _ := ParseAddr(tt.ip)
			assert.Equal(t, tt.want, isContainedInRanges(addr, privateAndLocalRanges))
		})
	}
}

func Test_orSlice(t *testing.T) {
	cases := []struct {
		name string
		s1   []string
		s2   []string
		want []string
	}{
		{
			name: "first is empty",
			s1:   []string{},
			s2:   []string{"foo"},
			want: []string{"foo"},
		},
		{
			name: "first is nil",
			s2:   []string{"foo"},
			want: []string{"foo"},
		},
		{
			name: "first non empty",
			s1:   []string{"bar"},
			s2:   []string{"foo"},
			want: []string{"bar"},
		},
		{
			name: "second is empty",
			s1:   []string{"foo"},
			s2:   []string{},
			want: []string{"foo"},
		},
		{
			name: "both are empty",
			s1:   []string{},
			s2:   []string{},
		},
		{
			name: "both are nil",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, orSlice(tc.s1, tc.s2))
		})
	}
}

func BenchmarkBackwardAddrSeq_Forwarded(b *testing.B) {
	cases := []struct {
		name  string
		value string
	}{
		{name: "ipv4", value: `for=192.0.2.60;proto=https, for=198.51.100.17;proto=https, for=203.0.113.43;proto=https`},
		{name: "ipv6 quoted", value: `for="[2001:db8:cafe::17]:4711";proto=https, for="[2001:db8::1]";proto=https, for="[2001:db8::2]"`},
		{name: "host quoted", value: `for=192.0.2.60;host="example.com";proto=https, for=198.51.100.17;host="example.com";proto=https`},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			values := []string{tc.value}
			var sink netip.Addr
			b.ReportAllocs()
			for b.Loop() {
				for addr := range backwardAddrSeq(values, forwardedHdr) {
					sink = addr
				}
			}
			_ = sink
		})
	}
}
