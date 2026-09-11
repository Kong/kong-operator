package helpers

import (
	"net"
	"net/netip"
	"strconv"
)

// URLHost returns host in a form that can be embedded in a URL or used as a
// dial target: an IPv6 address gets bracketed, anything else (an IPv4 address,
// a DNS name) is returned unchanged.
//
// Addresses read off cluster objects - a Gateway's status.addresses, a
// DataPlane's ingress Service LoadBalancer ingress IP, ... - are bare, so
// concatenating them into a URL breaks on IPv6-only clusters.
func URLHost(host string) string {
	if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() && !addr.Is4In6() {
		return "[" + host + "]"
	}
	return host
}

// HostPort joins host and port, bracketing host when it is an IPv6 address, so
// the result works both in a URL and as a [net.Dial] address.
func HostPort(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
