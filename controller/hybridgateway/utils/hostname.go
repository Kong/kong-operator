package utils

import "strings"

// HostnameIntersection returns the intersection of listener and route hostnames.
// Returns the most specific hostname that satisfies both constraints, or an empty string if
// there is no intersection.
func HostnameIntersection(listenerHostname, routeHostname string) string {
	// Exact match - return the common hostname
	if listenerHostname == routeHostname {
		return routeHostname
	}

	// Listener is wildcard (*.example.com), route is specific (api.example.com)
	if strings.HasPrefix(listenerHostname, "*.") {
		wildcardDomain := listenerHostname[1:] // Remove "*"

		// Route hostname must end with the wildcard domain
		if strings.HasSuffix(routeHostname, wildcardDomain) {
			return routeHostname // Return the more specific route hostname
		}
	}

	// Route is wildcard (*.example.com), listener is specific (api.example.com)
	if strings.HasPrefix(routeHostname, "*.") {
		wildcardDomain := routeHostname[1:] // Remove "*"

		// Listener hostname must end with the wildcard domain
		if strings.HasSuffix(listenerHostname, wildcardDomain) {
			return listenerHostname // Return the more specific listener hostname
		}
	}

	return "" // No intersection
}
