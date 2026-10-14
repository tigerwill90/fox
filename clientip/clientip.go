// The code in this package is derivative of https://github.com/realclientip/realclientip-go (all credit to Adam Pritchard).
// Mount of this source code is governed by a BSD Zero Clause License that can be found
// at https://github.com/realclientip/realclientip-go/blob/main/LICENSE.

package clientip

import (
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/netip"
	"strings"

	"github.com/fox-toolkit/fox"
	"github.com/fox-toolkit/fox/internal/iterutil"
	"github.com/fox-toolkit/fox/internal/netutil"
)

const (
	xForwardedForHdr = "X-Forwarded-For"
	forwardedHdr     = "Forwarded"
)

var (
	ErrInvalidIPAddress      = errors.New("invalid ip address")
	ErrUnspecifiedIPAddress  = errors.New("unspecified ip address")
	ErrTrustedPeer           = errors.New("trusted peer resolver")
	ErrRemoteAddress         = errors.New("remote address resolver")
	ErrSingleIPHeader        = errors.New("single ip header resolver")
	ErrLeftmostNonPrivate    = errors.New("leftmost non private resolver")
	ErrRightmostNonPrivate   = errors.New("rightmost non private resolver")
	ErrRightmostTrustedCount = errors.New("rightmost trusted count resolver")
	ErrRightmostTrustedRange = errors.New("rightmost trusted range resolver")
)

// Avoid allocating those errors each time since it may happen a lot on adversary header or when using multiple single ip
// header resolver in order to find the right match.
var (
	errLeftmostNonPrivate  = fmt.Errorf("%w: unable to find a valid or non-private IP", ErrLeftmostNonPrivate)
	errRightmostNonPrivate = fmt.Errorf("%w: unable to find a valid or non-private IP", ErrRightmostNonPrivate)
	errSingleIPHeader      = fmt.Errorf("%w: header not found", ErrSingleIPHeader)
	errTrustedPeer         = fmt.Errorf("%w: untrusted connecting peer", ErrTrustedPeer)
)

// TrustedIPRange returns a set of trusted IP ranges.
// Implementations of this interface must be thread-safe as it will be invoked
// whenever the client IP needs to be resolved, potentially from multiple goroutines.
type TrustedIPRange interface {
	TrustedIPRange() ([]netip.Prefix, error)
}

// The TrustedIPRangeFunc type is an adapter to allow the use of
// ordinary functions as [TrustedIPRange]. If f is a function
// with the appropriate signature, TrustedIPRangeFunc() is a
// [TrustedIPRange] that calls f.
type TrustedIPRangeFunc func() ([]netip.Prefix, error)

// TrustedIPRange calls f().
func (f TrustedIPRangeFunc) TrustedIPRange() ([]netip.Prefix, error) {
	return f()
}

type HeaderKey uint8

func (h HeaderKey) String() string {
	return http.CanonicalHeaderKey([...]string{xForwardedForHdr, forwardedHdr}[h])
}

const (
	XForwardedForKey HeaderKey = iota
	ForwardedKey
)

// Chain attempts to use the given resolvers in order. If the first one returns an error, the second one is
// tried, and so on, until a good IP is found or the resolvers are exhausted. A common use for this is if a server is
// both directly connected to the internet and expecting a header to check. It might be called like:
//
//	leftmost, _ := NewLeftmostNonPrivate(XForwardedForKey, 16)
//	chain := NewChain(leftmost, NewRemoteAddr())
type Chain struct {
	resolvers []fox.ClientIPResolver
}

// NewChain creates a [Chain] that attempts to use the given resolvers to derive the client IP, stopping when the
// first one succeeds.
func NewChain(resolvers ...fox.ClientIPResolver) Chain {
	return Chain{resolvers: resolvers}
}

// ClientIP try to derive the client IP using this resolver chain.
func (s Chain) ClientIP(c fox.RequestContext) (netip.Addr, error) {
	if len(s.resolvers) == 0 {
		return netip.Addr{}, fox.ErrNoClientIPResolver
	}

	var errs error
	for _, sub := range s.resolvers {
		addr, err := sub.ClientIP(c)
		if err == nil {
			return addr, nil
		}
		errs = errors.Join(errs, err)
	}

	return netip.Addr{}, errs
}

// TrustedPeer wraps another resolver and only consults it when the connecting peer is a trusted
// reverse proxy. It protects header-only resolvers such as [SingleIPHeader] and [RightmostTrustedCount],
// which derive the client IP purely from a header and cannot verify themselves that the request actually
// came through a trusted proxy. On a server that is reachable both through a proxy and directly from the
// internet, an attacker connecting directly could otherwise forge the header and spoof the result.
// [RightmostTrustedRange] does not need this wrapper: it already holds the trusted ranges and verifies
// the peer itself.
type TrustedPeer struct {
	resolver TrustedIPRange
	inner    fox.ClientIPResolver
}

// NewTrustedPeer creates a [TrustedPeer] resolver. resolver must provide the IP ranges of all trusted
// reverse proxies that connect directly to this server. inner is the resolver consulted once the peer
// is found to be trusted.
func NewTrustedPeer(resolver TrustedIPRange, inner fox.ClientIPResolver) (TrustedPeer, error) {
	if resolver == nil {
		return TrustedPeer{}, errors.New("invalid nil resolver")
	}

	if inner == nil {
		return TrustedPeer{}, errors.New("invalid nil inner resolver")
	}

	return TrustedPeer{resolver: resolver, inner: inner}, nil
}

// ClientIP derives the client IP using the [TrustedPeer] resolver. The connecting peer (the request's
// RemoteAddr) is classified first:
//   - a valid IP inside the trusted ranges is a trusted proxy: the inner resolver is consulted and its
//     result returned.
//   - a valid IP outside the trusted ranges did not arrive through a trusted proxy: the header cannot
//     be trusted and an error is returned. To also serve direct clients, chain a [RemoteAddr] resolver
//     after this one with [NewChain].
//   - anything else (empty, a Unix domain socket peer or otherwise unparseable) cannot be classified
//     and the inner resolver is consulted as normal; a Unix socket is unreachable from the network and
//     cannot be a spoofing vector.
func (s TrustedPeer) ClientIP(c fox.RequestContext) (netip.Addr, error) {
	trustedRange, err := s.resolver.TrustedIPRange()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: unable to resolve trusted ip range: %w", ErrTrustedPeer, err)
	}

	// If the connecting peer is a valid IP outside the trusted ranges, the request did not come
	// through a trusted proxy, so the header is untrustworthy and must not be used.
	if peer, err := ParseAddr(c.Request().RemoteAddr); err == nil && !isContainedInRanges(peer, trustedRange) {
		return netip.Addr{}, errTrustedPeer
	}

	return s.inner.ClientIP(c)
}

// RemoteAddr returns the client socket IP, stripped of port.
// This resolver should be used if the server accept direct connections, rather than through a reverse proxy.
type RemoteAddr struct{}

// NewRemoteAddr that uses request remote address to get the client IP.
func NewRemoteAddr() RemoteAddr {
	return RemoteAddr{}
}

// ClientIP derives the client IP using the [RemoteAddr] resolver. The returned [netip.Addr] may contain
// a zone identifier. If no valid IP can be derived, an error is returned.
func (s RemoteAddr) ClientIP(c fox.RequestContext) (netip.Addr, error) {
	addr, err := ParseAddr(c.Request().RemoteAddr)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: %w", ErrRemoteAddress, err)
	}
	return addr, nil
}

// SingleIPHeader derives an IP address from a single-IP header. A non-exhaustive list of such single-IP headers
// is: X-Real-IP, CF-Connecting-IP, True-Client-IP, Fastly-Client-IP, X-Azure-ClientIP, X-Azure-SocketIP. This resolver
// should be used when the given header is added by a trusted reverse proxy. You must ensure that this header is not
// spoofable (as is possible with Akamai's use of True-Client-IP, Fastly's default use of Fastly-Client-IP,
// and Azure's X-Azure-ClientIP). This resolver trusts the header without checking the connecting peer:
// if the server is also reachable directly from the internet, wrap it in a [TrustedPeer].
type SingleIPHeader struct {
	headerName string
}

// NewSingleIPHeader creates a [SingleIPHeader] resolver that uses the headerName request header to get the client IP.
func NewSingleIPHeader(headerName string) (SingleIPHeader, error) {
	if headerName == "" {
		return SingleIPHeader{}, errors.New("empty header name")
	}

	headerName = http.CanonicalHeaderKey(headerName)

	if headerName == xForwardedForHdr || headerName == forwardedHdr {
		return SingleIPHeader{}, fmt.Errorf("header %s not allowed", headerName)
	}

	return SingleIPHeader{headerName: headerName}, nil
}

// ClientIP derives the client IP using the [SingleIPHeader] resolver. The returned [netip.Addr] may contain a zone identifier.
// If no valid IP can be derived, an error is returned.
func (s SingleIPHeader) ClientIP(c fox.RequestContext) (netip.Addr, error) {
	// RFC 2616 does not allow multiple instances of single-IP headers (or any non-list header).
	// It is debatable whether it is better to treat multiple such headers as an error
	// (more correct) or simply pick one of them (more flexible). As we've already
	// told the user to make sure the header is not spoofable, we're going to use the
	// last header instance if there are multiple. (Using the last is arbitrary, but
	// in theory it should be the newest value.)
	ipStr := lastHeader(c.Request().Header, s.headerName)
	if ipStr == "" {
		return netip.Addr{}, errSingleIPHeader
	}

	addr, err := ParseAddr(ipStr)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: %w", ErrSingleIPHeader, err)
	}
	return addr, nil
}

// LeftmostNonPrivate derives the client IP from the leftmost valid and non-private/non-internal IP address in the X-Forwarded-For
// or Forwarded header. This resolver should be used when a valid, non-private IP closest to the client is desired. By default,
// loopback, link local and private net ip range are excluded. Note that this MUST NOT BE USED FOR SECURITY PURPOSES.
// This IP can be TRIVIALLY SPOOFED.
type LeftmostNonPrivate struct {
	headerName     string
	excludedRanges []netip.Prefix
	limit          uint
}

// NewLeftmostNonPrivate creates a [LeftmostNonPrivate] resolver. By default, loopback, link local and private net ip range
// are excluded. A reasonable limit on the number of IPs to parse must be provided to prevent excessive resource usage from
// adversarial headers.
func NewLeftmostNonPrivate(key HeaderKey, limit uint, opts ...ExcludedRangeOption) (LeftmostNonPrivate, error) {
	if key > 1 {
		return LeftmostNonPrivate{}, errors.New("invalid header key")
	}
	if limit == 0 {
		return LeftmostNonPrivate{}, errors.New("invalid limit: expect greater than zero")
	}

	cfg := new(config)
	for _, opt := range opts {
		opt.applyLeft(cfg)
	}

	return LeftmostNonPrivate{
		headerName:     key.String(),
		excludedRanges: orSlice(cfg.ipRanges, privateAndLocalRanges),
		limit:          limit,
	}, nil
}

// ClientIP derives the client IP using the [LeftmostNonPrivate] resolver. The returned [netip.Addr] may contain a
// zone identifier. If no valid IP can be derived, an error returned.
func (s LeftmostNonPrivate) ClientIP(c fox.RequestContext) (netip.Addr, error) {
	if values, ok := c.Request().Header[s.headerName]; ok && len(values) > 0 {
		for ip := range iterutil.Take(addrSeq(values, s.headerName), s.limit) {
			if ip.IsValid() && !isContainedInRanges(ip, s.excludedRanges) {
				// This is the leftmost valid, non-private IP
				return ip, nil
			}
		}
	}
	// We failed to find any valid, non-private IP
	return netip.Addr{}, errLeftmostNonPrivate
}

// RightmostNonPrivate derives the client IP from the rightmost valid, non-private/non-internal IP address in
// the X-Forwarded-For or Forwarded header. This resolver should be used when all reverse proxies between the internet
// and the server have private-space IP addresses. By default, loopback, link local and private net ip range are trusted.
type RightmostNonPrivate struct {
	headerName    string
	trustedRanges []netip.Prefix
}

// NewRightmostNonPrivate creates a [RightmostNonPrivate] resolver. By default, loopback, link local and private net ip range
// are trusted.
func NewRightmostNonPrivate(key HeaderKey, opts ...TrustedRangeOption) (RightmostNonPrivate, error) {
	if key > 1 {
		return RightmostNonPrivate{}, errors.New("invalid header key")
	}

	cfg := new(config)
	for _, opt := range opts {
		opt.applyRight(cfg)
	}

	return RightmostNonPrivate{
		headerName:    key.String(),
		trustedRanges: orSlice(cfg.ipRanges, privateAndLocalRanges),
	}, nil
}

// ClientIP derives the client IP using the [RightmostNonPrivate] resolver. The returned [netip.Addr] may contain a
// zone identifier. If no valid IP can be derived, an error returned.
func (s RightmostNonPrivate) ClientIP(c fox.RequestContext) (netip.Addr, error) {
	if values, ok := c.Request().Header[s.headerName]; ok && len(values) > 0 {
		for ip := range backwardAddrSeq(values, s.headerName) {
			if ip.IsValid() && !isContainedInRanges(ip, s.trustedRanges) {
				return ip, nil
			}
		}
	}
	// We failed to find any valid, non-private IP
	return netip.Addr{}, errRightmostNonPrivate
}

// RightmostTrustedCount derives the client IP from the valid IP address added by the first trusted reverse
// proxy to the X-Forwarded-For or Forwarded header. This resolver should be used when there is a fixed number of
// trusted reverse proxies that are appending IP addresses to the header. It assumes the request arrived
// through those proxies but does not verify it: if the server is also reachable directly from the
// internet, wrap it in a [TrustedPeer].
type RightmostTrustedCount struct {
	headerName   string
	trustedCount uint
}

// NewRightmostTrustedCount creates a [RightmostTrustedCount] resolver. trustedCount is the number of trusted reverse proxies.
// The IP returned will be the (trustedCount-1)th from the right. For example, if there's only one trusted proxy, this
// resolver will return the last (rightmost) IP address.
func NewRightmostTrustedCount(key HeaderKey, trustedCount uint) (RightmostTrustedCount, error) {
	if key > 1 {
		return RightmostTrustedCount{}, errors.New("invalid header key")
	}

	if trustedCount == 0 {
		return RightmostTrustedCount{}, errors.New("invalid trusted count: expect greater than zero")
	}
	return RightmostTrustedCount{headerName: key.String(), trustedCount: trustedCount}, nil
}

// ClientIP derives the client IP using the [RightmostTrustedCount] resolver. The returned [netip.Addr] may contain a
// zone identifier. If no valid IP can be derived, an error returned.
func (s RightmostTrustedCount) ClientIP(c fox.RequestContext) (netip.Addr, error) {
	ip, ok := iterutil.At(backwardAddrSeq(c.Request().Header[s.headerName], s.headerName), s.trustedCount-1)
	if !ok {
		// This is a misconfiguration error. There were fewer IPs than we expected.
		return netip.Addr{}, fmt.Errorf("%w: expected at least %d IP(s)", ErrRightmostTrustedCount, s.trustedCount)
	}

	if !ip.IsValid() {
		// This is a misconfiguration error. Our first trusted proxy didn't add a
		// valid IP address to the header.
		return netip.Addr{}, fmt.Errorf("%w: invalid IP address from the first trusted proxy", ErrRightmostTrustedCount)
	}

	return ip, nil
}

// RightmostTrustedRange derives the client IP from the rightmost valid IP address in the X-Forwarded-For or Forwarded
// header which is not in a set of trusted IP ranges. This resolver should be used when the IP ranges of the reverse
// proxies between the internet and the server are known.
//
// This resolver also verifies the connecting peer (the request's RemoteAddr): if the peer is a valid IP
// that is not in the trusted ranges, the request did not arrive through a trusted proxy, the header is
// ignored and the peer itself is returned. This makes the resolver safe to use on a server that is
// reachable both through a trusted proxy and directly (an attacker connecting directly cannot spoof the
// client IP via a forged header). See ClientIP for the exact peer-handling rules.
//
// If a third-party WAF, CDN, etc., is used, you SHOULD use a
// method of verifying its access to your origin that is stronger than checking its IP address (e.g., using authenticated pulls).
// Failure to do so can result in scenarios like: You use AWS CloudFront in front of a server you host elsewhere. An
// attacker creates a CF distribution that points at your origin server. The attacker uses Lambda@Edge to spoof the Host
// and X-Forwarded-For headers. Now your "trusted" reverse proxy is no longer trustworthy.
type RightmostTrustedRange struct {
	resolver   TrustedIPRange
	headerName string
}

// NewRightmostTrustedRange creates a [RightmostTrustedRange] resolver. key must be [XForwardedForKey] or
// [ForwardedKey]. resolver must provide all trusted reverse proxy ranges on the path to this server, private/internal
// or external (for example, if a third-party reverse proxy is used).
func NewRightmostTrustedRange(key HeaderKey, resolver TrustedIPRange) (RightmostTrustedRange, error) {
	if key > 1 {
		return RightmostTrustedRange{}, errors.New("invalid header key")
	}

	if resolver == nil {
		return RightmostTrustedRange{}, errors.New("invalid nil resolver")
	}

	return RightmostTrustedRange{headerName: key.String(), resolver: resolver}, nil
}

// ClientIP derives the client IP using the [RightmostTrustedRange] resolver. The returned [netip.Addr] may contain a
// zone identifier. If no valid IP can be derived, an error is returned.
//
// The connecting peer (the request's RemoteAddr) is checked first: a valid IP outside the trusted
// ranges is returned as is, since the request did not arrive through a trusted proxy and the peer is
// the rightmost untrusted address. A trusted or unclassifiable peer (empty, a Unix domain socket peer
// or otherwise unparseable) falls through to the header walk.
func (s RightmostTrustedRange) ClientIP(c fox.RequestContext) (netip.Addr, error) {
	trustedRange, err := s.resolver.TrustedIPRange()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: unable to resolve trusted ip range: %w", ErrRightmostTrustedRange, err)
	}

	// If the connecting peer is a valid IP outside the trusted ranges, the request did not come
	// through a trusted proxy: the header is untrustworthy and the peer is the rightmost untrusted
	// address, so return it.
	if peer, err := ParseAddr(c.Request().RemoteAddr); err == nil && !isContainedInRanges(peer, trustedRange) {
		return peer, nil
	}

	for ip := range backwardAddrSeq(c.Request().Header[s.headerName], s.headerName) {
		if ip.IsValid() && isContainedInRanges(ip, trustedRange) {
			// This IP is trusted
			continue
		}

		// At this point we have found the first-from-the-rightmost untrusted IP
		if !ip.IsValid() {
			return netip.Addr{}, fmt.Errorf("%w: unable to find a valid IP address", ErrRightmostTrustedRange)
		}

		return ip, nil
	}

	// Either there are no addresses or they are all in our trusted ranges
	return netip.Addr{}, fmt.Errorf("%w: unable to find a valid IP address", ErrRightmostTrustedRange)
}

// ParseAddr safely parses the given string into a [netip.Addr]. The port and any surrounding square brackets are
// stripped, IPv4-mapped IPv6 addresses are unmapped to their IPv4 form and IPv6 zone identifiers are preserved.
// It also returns an error for unspecified addresses (like "::" or "0.0.0.0"). These are nominally valid IPs,
// but they are never valid "real" client IPs.
//
// The function returns the following errors:
// - [ErrInvalidIPAddress]: if the IP address cannot be parsed.
// - [ErrUnspecifiedIPAddress]: if the IP address is unspecified (e.g., "::" or "0.0.0.0").
func ParseAddr(ip string) (netip.Addr, error) {
	if host, _, ok := netutil.SplitHostPort(ip); ok {
		ip = host
	}

	// A failed split may be an IPv6 address without brackets and port. netip.ParseAddr
	// will be the final arbiter of validity.

	// Square brackets around IPv6 addresses may be used in the Forwarded header.
	// netip.ParseAddr doesn't like them, so we'll trim them off.
	ip = trimMatchedEnds(ip, "[]")

	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Addr{}, ErrInvalidIPAddress
	}

	addr = addr.Unmap()
	if addr.WithZone("").IsUnspecified() {
		return netip.Addr{}, ErrUnspecifiedIPAddress
	}

	return addr, nil
}

// ParsePrefixes converts a slice of strings with IPv4 and IPv6 addresses and CIDR ranges (prefixes) to
// [netip.Prefix] instances. Single addresses are treated as full-length prefixes and IPv4-mapped IPv6 values
// are normalized to their IPv4 form. Zones in addresses or ranges are not allowed and will result in an error.
func ParsePrefixes(ranges ...string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(ranges))
	for _, r := range ranges {
		prefix, err := netutil.ParsePrefix(r)
		if err != nil {
			return nil, fmt.Errorf("invalid address or prefix %q: %w", r, err)
		}
		prefixes = append(prefixes, prefix)
	}

	return prefixes, nil
}

// trimMatchedEnds trims s if and only if the first and last bytes in s are in chars.
// If chars is a single character (like `"`), then the first and last bytes must match
// that single character. If chars is two characters (like `[]`), the first byte in s
// must match the first byte in chars, and the last bytes in s must match the last byte
// in chars.
// This helps us ensure that we only trim _matched_ quotes and brackets,
// which strings.Trim doesn't provide.
func trimMatchedEnds(s string, chars string) string {
	if len(chars) != 1 && len(chars) != 2 {
		panic("chars must be length 1 or 2")
	}

	first, last := chars[0], chars[0]
	if len(chars) > 1 {
		last = chars[1]
	}

	if len(s) < 2 {
		return s
	}

	if s[0] != first {
		return s
	}

	if s[len(s)-1] != last {
		return s
	}

	return s[1 : len(s)-1]
}

// lastHeader returns the last header with the given name. It returns empty string if the
// header is not found or if the header has an empty value. No validation is done on the
// IP string. headerName must already be canonicalized.
// This should be used with single-IP headers, like X-Real-IP. Per RFC 2616, they should
// not have multiple headers, but if they do we can hope we're getting the newest/best by
// taking the last instance.
// This MUST NOT be used with list headers, like X-Forwarded-For and Forwarded.
func lastHeader(headers http.Header, headerName string) string {
	// Note that Go's Header map uses canonicalized keys
	matches, ok := headers[headerName]
	if !ok || len(matches) == 0 {
		// For our uses of this function, returning an empty string in this case is fine
		return ""
	}

	return matches[len(matches)-1]
}

// backwardAddrSeq returns a range iterator over the X-Forwarded-For or Forwarded header
// values, in reverse order. Any invalid IPs will result in zero [netip.Addr] elements. headerName must already
// be in canonical form.
func backwardAddrSeq(values []string, headerName string) iter.Seq[netip.Addr] {
	return func(yield func(netip.Addr) bool) {
		for i := len(values) - 1; i >= 0; i-- {
			for rawListItem := range iterutil.BackwardSplitStringSeq(values[i], ",") {
				// The IPs are often comma-space separated, so we'll need to trim the string
				rawListItem = strings.TrimSpace(rawListItem)

				var addr netip.Addr
				// If this is the XFF header, rawListItem is just an IP;
				// if it's the Forwarded header, then there's more parsing to do.
				if headerName == forwardedHdr {
					addr = parseForwardedListItem(rawListItem)
				} else { // == XFF
					addr, _ = ParseAddr(rawListItem)
				}

				if !yield(addr) {
					return
				}
			}
		}
	}
}

// addrSeq returns a range iterator over the X-Forwarded-For or Forwarded header
// values, in order. Any invalid IPs will result in zero [netip.Addr] elements. headerName must already
// be in canonical form.
func addrSeq(values []string, headerName string) iter.Seq[netip.Addr] {
	return func(yield func(netip.Addr) bool) {
		for _, v := range values {
			// We now have a sequence of comma-separated list items.
			for rawListItem := range iterutil.SplitStringSeq(v, ",") {
				// The IPs are often comma-space separated, so we'll need to trim the string
				rawListItem = strings.TrimSpace(rawListItem)

				var addr netip.Addr
				// If this is the XFF header, rawListItem is just an IP;
				// if it's the Forwarded header, then there's more parsing to do.
				if headerName == forwardedHdr {
					addr = parseForwardedListItem(rawListItem)
				} else { // == XFF
					addr, _ = ParseAddr(rawListItem)
				}

				// addr is the zero Addr if not valid
				if !yield(addr) {
					return
				}
			}
		}
	}
}

// parseForwardedListItem parses a Forwarded header list item, and returns the "for" IP
// address. The zero [netip.Addr] is returned if the "for" IP is absent or invalid.
func parseForwardedListItem(fwd string) netip.Addr {
	// The header list item can look like these kinds of thing:
	//	For="[2001:db8:cafe::17%zone]:4711"
	//	For="[2001:db8:cafe::17%zone]"
	//	for=192.0.2.60;proto=http; by=203.0.113.43
	//	for=192.0.2.43

	// First split up "for=", "by=", "host=", etc.
	// A valid syntax have at most 4 section, e.g. by=<identifier>;for=<identifier>;host=<host>;proto=<http|https>
	// Find the "for=" part, since that has the IP we want (maybe)
	var forPart string
	for fp := range iterutil.Take(iterutil.SplitStringSeq(fwd, ";"), 4) {
		// Whitespace is allowed around the semicolons
		fp = strings.TrimSpace(fp)

		fpSplit := strings.SplitN(fp, "=", 2)
		if len(fpSplit) != 2 {
			// There are too few equal signs in this part
			continue
		}

		if strings.EqualFold(fpSplit[0], "for") {
			// We found the "for=" part
			forPart = fpSplit[1]
			break
		}
	}

	// There shouldn't (per RFC 7239) be spaces around the semicolon or equal sign. It might
	// be more correct to consider spaces an error, but we'll tolerate and trim them.
	forPart = strings.TrimSpace(forPart)

	// Get rid of any quotes, such as surrounding IPv6 addresses.
	// Note that doing this without checking if the quotes are present means that we are
	// effectively accepting IPv6 addresses that don't strictly conform to RFC 7239, which
	// requires quotes. https://www.rfc-editor.org/rfc/rfc7239#section-4
	// This behaviour is debatable.
	// It also means that we will accept IPv4 addresses with quotes, which is correct.
	forPart = trimMatchedEnds(forPart, `"`)

	if forPart == "" {
		// We failed to find a "for=" part
		return netip.Addr{}
	}

	// The zero Addr is returned if the IP extracted from the "for=" part isn't valid
	addr, _ := ParseAddr(forPart)
	return addr
}

// mustParseAddr panics if [ParseAddr] fails.
func mustParseAddr(ipStr string) netip.Addr {
	addr, err := ParseAddr(ipStr)
	if err != nil {
		panic(fmt.Sprintf("ParseAddr failed: %v", err))
	}
	return addr
}

func must[T fox.ClientIPResolver](s T, err error) T {
	if err != nil {
		panic(err)
	}
	return s
}

// privateAndLocalRanges netip.Prefix that are loopback, private, link local, default unicast.
// Based on https://github.com/wader/filtertransport/blob/bdd9e61eee7804e94ceb927c896b59920345c6e4/filter.go#L36-L64
// which is based on https://github.com/letsencrypt/boulder/blob/master/bdns/dns.go
var privateAndLocalRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),         // RFC1918
	netip.MustParsePrefix("172.16.0.0/12"),      // private
	netip.MustParsePrefix("192.168.0.0/16"),     // private
	netip.MustParsePrefix("127.0.0.0/8"),        // RFC5735
	netip.MustParsePrefix("0.0.0.0/8"),          // RFC1122 Section 3.2.1.3
	netip.MustParsePrefix("169.254.0.0/16"),     // RFC3927
	netip.MustParsePrefix("192.0.0.0/24"),       // RFC 5736
	netip.MustParsePrefix("192.0.2.0/24"),       // RFC 5737
	netip.MustParsePrefix("198.51.100.0/24"),    // Assigned as TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),     // Assigned as TEST-NET-3
	netip.MustParsePrefix("192.88.99.0/24"),     // RFC 3068
	netip.MustParsePrefix("198.18.0.0/15"),      // RFC 2544
	netip.MustParsePrefix("224.0.0.0/4"),        // RFC 3171
	netip.MustParsePrefix("240.0.0.0/4"),        // RFC 1112
	netip.MustParsePrefix("255.255.255.255/32"), // RFC 919 Section 7
	netip.MustParsePrefix("100.64.0.0/10"),      // RFC 6598
	netip.MustParsePrefix("::/128"),             // RFC 4291: Unspecified Address
	netip.MustParsePrefix("::1/128"),            // RFC 4291: Loopback Address
	netip.MustParsePrefix("100::/64"),           // RFC 6666: Discard Address Block
	netip.MustParsePrefix("2001::/23"),          // RFC 2928: IETF Protocol Assignments
	netip.MustParsePrefix("2001:2::/48"),        // RFC 5180: Benchmarking
	netip.MustParsePrefix("2001:db8::/32"),      // RFC 3849: Documentation
	netip.MustParsePrefix("2001::/32"),          // RFC 4380: TEREDO
	netip.MustParsePrefix("fc00::/7"),           // RFC 4193: Unique-Local
	netip.MustParsePrefix("fe80::/10"),          // RFC 4291: Section 2.5.6 Link-Scoped Unicast
	netip.MustParsePrefix("ff00::/8"),           // RFC 4291: Section 2.7
	netip.MustParsePrefix("2002::/16"),          // RFC 7526: 6to4 anycast prefix deprecated
}

var privateRange = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),         // RFC1918
	netip.MustParsePrefix("172.16.0.0/12"),      // private
	netip.MustParsePrefix("192.168.0.0/16"),     // private
	netip.MustParsePrefix("0.0.0.0/8"),          // RFC1122 Section 3.2.1.3
	netip.MustParsePrefix("192.0.0.0/24"),       // RFC 5736
	netip.MustParsePrefix("192.0.2.0/24"),       // RFC 5737
	netip.MustParsePrefix("198.51.100.0/24"),    // Assigned as TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),     // Assigned as TEST-NET-3
	netip.MustParsePrefix("192.88.99.0/24"),     // RFC 3068
	netip.MustParsePrefix("198.18.0.0/15"),      // RFC 2544
	netip.MustParsePrefix("224.0.0.0/4"),        // RFC 3171
	netip.MustParsePrefix("240.0.0.0/4"),        // RFC 1112
	netip.MustParsePrefix("255.255.255.255/32"), // RFC 919 Section 7
	netip.MustParsePrefix("100.64.0.0/10"),      // RFC 6598
	netip.MustParsePrefix("::/128"),             // RFC 4291: Unspecified Address
	netip.MustParsePrefix("100::/64"),           // RFC 6666: Discard Address Block
	netip.MustParsePrefix("2001::/23"),          // RFC 2928: IETF Protocol Assignments
	netip.MustParsePrefix("2001:2::/48"),        // RFC 5180: Benchmarking
	netip.MustParsePrefix("2001:db8::/32"),      // RFC 3849: Documentation
	netip.MustParsePrefix("2001::/32"),          // RFC 4380: TEREDO
	netip.MustParsePrefix("fc00::/7"),           // RFC 4193: Unique-Local
	netip.MustParsePrefix("ff00::/8"),           // RFC 4291: Section 2.7
	netip.MustParsePrefix("2002::/16"),          // RFC 7526: 6to4 anycast prefix deprecated
}

// loopbackRanges netip.Prefix that are loopback.
// Based on https://github.com/wader/filtertransport/blob/bdd9e61eee7804e94ceb927c896b59920345c6e4/filter.go#L36-L64
// which is based on https://github.com/letsencrypt/boulder/blob/master/bdns/dns.go
var loopbackRanges = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"), // RFC5735, Loopback
	netip.MustParsePrefix("::1/128"),     // RFC4291, Loopback Address
}

// linkLocalRanges netip.Prefix that are link local.
// Based on https://github.com/wader/filtertransport/blob/bdd9e61eee7804e94ceb927c896b59920345c6e4/filter.go#L36-L64
// which is based on https://github.com/letsencrypt/boulder/blob/master/bdns/dns.go
var linkLocalRanges = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"), // RFC3927, Link Local
	netip.MustParsePrefix("fe80::/10"),      // RFC4291 Section 2.5.6, Link-Scoped Unicast
}

// isContainedInRanges reports whether addr is in at least one of the given ranges.
// The zone is stripped because [netip.Prefix.Contains] returns false when the address has an
// IPv6 zone, and the zone plays no role in matching. IPv4-mapped IPv6 prefixes are rewritten to
// their IPv4 form because Contains never matches an IPv4 address against an IPv6 prefix, and
// addresses are unmapped at parse time.
func isContainedInRanges(addr netip.Addr, ranges []netip.Prefix) bool {
	addr = addr.WithZone("")
	for _, r := range ranges {
		if r.Addr().Is4In6() && r.Bits() >= 96 {
			r = netip.PrefixFrom(r.Addr().Unmap(), r.Bits()-96)
		}
		if r.Contains(addr) {
			return true
		}
	}
	return false
}

// orSlice returns the first of its arguments that has a length greater than zero.
// If no argument is greater than 0, it returns the zero value.
func orSlice[T any, S ~[]T](vals ...S) S {
	var zero S
	for _, val := range vals {
		if len(val) > 0 {
			return val
		}
	}
	return zero
}
