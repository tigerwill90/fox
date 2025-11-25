package netutil

import (
	"net"
	"strings"
)

func SplitHostZone(s string) (host, zone string) {
	// This is copied from an unexported function in the Go stdlib:
	// https://github.com/golang/go/blob/5c9b6e8e63e012513b1cb1a4a08ff23dec4137a1/src/net/ipsock.go#L219-L228

	// The IPv6 scoped addressing zone identifier starts after the last percent sign.
	if i := strings.LastIndexByte(s, '%'); i > 0 {
		host, zone = s[:i], s[i+1:]
	} else {
		host = s
	}
	return
}

// StripHostPort returns h without any trailing ":<port>". It also removes trailing period in the hostname.
// Per RFC 3696, The DNS specification permits a trailing period to be used to denote the root, e.g., "a.b.c" and "a.b.c."
// are equivalent, but the latter is more explicit and is required to be accepted by applications. Note that FQDN does
// not play well with TLS (see https://github.com/traefik/traefik/issues/9157#issuecomment-1180588735)
func StripHostPort(h string) string {
	if h == "" {
		return h
	}
	// If no port on host, return unchanged
	if !strings.Contains(h, ":") {
		return strings.TrimSuffix(h, ".")
	}

	host, _, err := net.SplitHostPort(h)
	if err != nil {
		return h // on error, return unchanged
	}
	return strings.TrimSuffix(host, ".")
}

func ParseCIDR(cidr string) (*net.IPNet, error) {
	// Try parsing as CIDR first
	_, ipNet, err := net.ParseCIDR(cidr)
	if err == nil {
		return ipNet, nil
	}

	// If not a CIDR, try parsing as a plain IP address
	ip := net.ParseIP(cidr)
	if ip == nil {
		return nil, err // return original CIDR parsing error
	}

	// Create a /32 or /128 network for the single IP
	var mask net.IPMask
	if ip.To4() != nil {
		mask = net.CIDRMask(32, 32) // IPv4
	} else {
		mask = net.CIDRMask(128, 128) // IPv6
	}

	return &net.IPNet{
		IP:   ip,
		Mask: mask,
	}, nil
}
