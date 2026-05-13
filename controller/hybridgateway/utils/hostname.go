package utils

import "strings"

// HostnameIntersection returns the most specific hostname that satisfies both constraints
// and whether an intersection exists.
// Empty input hostnames are treated as the Gateway API match-any wildcard ("*").
func HostnameIntersection(listenerHostname, routeHostname string) (string, bool) {
	// Treat empty hostnames as the special match-any "*".
	if listenerHostname == "" && routeHostname == "" {
		return "*", true
	}
	if listenerHostname == "" {
		return routeHostname, true
	}
	if routeHostname == "" {
		return listenerHostname, true
	}

	if listenerHostname == routeHostname {
		return listenerHostname, true
	}

	// Listener wildcard, route precise.
	if isWildcard(listenerHostname) && isPrecise(routeHostname) {
		if wildcardMatches(listenerHostname, routeHostname) {
			return routeHostname, true
		}
	}

	// Route wildcard, listener precise.
	if isWildcard(routeHostname) && isPrecise(listenerHostname) {
		if wildcardMatches(routeHostname, listenerHostname) {
			return listenerHostname, true
		}
	}

	// Wildcard vs wildcard overlap: return the more specific wildcard if they intersect.
	if isWildcard(listenerHostname) && isWildcard(routeHostname) {
		if wildcardOverlaps(listenerHostname, routeHostname) {
			return moreSpecificWildcard(listenerHostname, routeHostname), true
		}
	}

	return "", false
}

func isWildcard(hostname string) bool {
	return hostname == "*" || strings.HasPrefix(hostname, "*.")
}

func isPrecise(hostname string) bool {
	return hostname != "" && !isWildcard(hostname)
}

func wildcardMatches(wildcard, hostname string) bool {
	if wildcard == "*" {
		return hostname != ""
	}
	if !strings.HasPrefix(wildcard, "*.") {
		return false
	}
	suffix := wildcard[1:] // includes leading dot
	// Ensure at least one label exists before the wildcard suffix.
	return strings.HasSuffix(hostname, suffix) && len(hostname) > len(suffix)
}

func wildcardOverlaps(a, b string) bool {
	if a == "*" || b == "*" {
		return true
	}
	suffixA := a[1:]
	suffixB := b[1:]
	return strings.HasSuffix(suffixA, suffixB) || strings.HasSuffix(suffixB, suffixA)
}

func moreSpecificWildcard(a, b string) string {
	if a == "*" {
		return b
	}
	if b == "*" {
		return a
	}
	if len(a) >= len(b) {
		return a
	}
	return b
}
