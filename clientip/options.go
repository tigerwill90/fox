package clientip

import (
	"net"
)

type config struct {
	ipRanges []net.IPNet
}

type TrustedRangeOption interface {
	applyRight(*config)
}

type BlacklistRangeOption interface {
	applyLeft(*config)
}

type rightmostNonPrivateOptionFunc func(*config)

func (o rightmostNonPrivateOptionFunc) applyRight(c *config) {
	o(c)
}

type leftmostNonPrivateOptionFunc func(*config)

func (o leftmostNonPrivateOptionFunc) applyLeft(c *config) {
	o(c)
}

// TrustLoopback enables or disables the inclusion of loopback ip ranges in the trusted ip ranges.
func TrustLoopback(enable bool) TrustedRangeOption {
	return rightmostNonPrivateOptionFunc(func(c *config) {
		if enable {
			c.ipRanges = append(c.ipRanges, loopbackRanges...)
		}
	})
}

// TrustLinkLocal enables or disables the inclusion of link local ip ranges in the trusted ip ranges.
func TrustLinkLocal(enable bool) TrustedRangeOption {
	return rightmostNonPrivateOptionFunc(func(c *config) {
		if enable {
			c.ipRanges = append(c.ipRanges, linkLocalRanges...)
		}
	})
}

// TrustPrivateNet enables or disables the inclusion of private-space ip ranges in the trusted ip ranges.
func TrustPrivateNet(enable bool) TrustedRangeOption {
	return rightmostNonPrivateOptionFunc(func(c *config) {
		if enable {
			c.ipRanges = append(c.ipRanges, privateRange...)
		}
	})
}

// ExcludeLoopback enables or disables the inclusion of loopback ip ranges in the blacklisted ip ranges.
func ExcludeLoopback(enable bool) BlacklistRangeOption {
	return leftmostNonPrivateOptionFunc(func(c *config) {
		if enable {
			c.ipRanges = append(c.ipRanges, loopbackRanges...)
		}
	})
}

// ExcludeLinkLocal enables or disables the inclusion of link local ip ranges in the blacklisted ip ranges.
func ExcludeLinkLocal(enable bool) BlacklistRangeOption {
	return leftmostNonPrivateOptionFunc(func(c *config) {
		if enable {
			c.ipRanges = append(c.ipRanges, linkLocalRanges...)
		}
	})
}

// ExcludePrivateNet enables or disables the inclusion of private-space ip ranges in the blacklisted ip ranges.
func ExcludePrivateNet(enable bool) BlacklistRangeOption {
	return leftmostNonPrivateOptionFunc(func(c *config) {
		if enable {
			c.ipRanges = append(c.ipRanges, privateRange...)
		}
	})
}
