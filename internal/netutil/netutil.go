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

// SplitHostPort separates host and port. If the port is not valid, it returns
// the entire input as host, and it doesn't check the validity of the host.
// Unlike net.SplitHostPort, but per RFC 3986, it requires ports to be numeric.
func SplitHostPort(hostPort string) (host, port string) {
	host = hostPort

	colon := strings.LastIndexByte(host, ':')
	if colon != -1 && validOptionalPort(host[colon:]) {
		host, port = host[:colon], host[colon+1:]
	}

	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
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

// validOptionalPort reports whether port is either an empty string
// or matches /^:\d*$/
func validOptionalPort(port string) bool {
	if port == "" {
		return true
	}
	if port[0] != ':' {
		return false
	}
	for _, b := range port[1:] {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}
