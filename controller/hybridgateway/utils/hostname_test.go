package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHostnameIntersection(t *testing.T) {
	tests := []struct {
		name             string
		listenerHostname string
		routeHostname    string
		expected         string
		intersects       bool
	}{
		{
			name:             "exact match",
			listenerHostname: "api.example.com",
			routeHostname:    "api.example.com",
			expected:         "api.example.com",
			intersects:       true,
		},
		{
			name:             "listener wildcard, route specific",
			listenerHostname: "*.example.com",
			routeHostname:    "api.example.com",
			expected:         "api.example.com",
			intersects:       true,
		},
		{
			name:             "route wildcard, listener specific",
			listenerHostname: "api.example.com",
			routeHostname:    "*.example.com",
			expected:         "api.example.com",
			intersects:       true,
		},
		{
			name:             "no intersection - different domains",
			listenerHostname: "*.example.com",
			routeHostname:    "api.other.com",
			expected:         "",
			intersects:       false,
		},
		{
			name:             "multiple subdomains",
			listenerHostname: "*.example.com",
			routeHostname:    "sub.api.example.com",
			expected:         "sub.api.example.com",
			intersects:       true,
		},
		{
			name:             "no intersection - exact mismatch",
			listenerHostname: "api.example.com",
			routeHostname:    "web.example.com",
			expected:         "",
			intersects:       false,
		},
		{
			name:             "wildcard domain match",
			listenerHostname: "*.example.com",
			routeHostname:    "web.example.com",
			expected:         "web.example.com",
			intersects:       true,
		},
		{
			name:             "wildcard does not match apex",
			listenerHostname: "*.example.com",
			routeHostname:    "example.com",
			expected:         "",
			intersects:       false,
		},
		{
			name:             "both wildcards - no intersection",
			listenerHostname: "*.example.com",
			routeHostname:    "*.other.com",
			expected:         "",
			intersects:       false,
		},
		{
			name:             "wildcard overlap returns more specific",
			listenerHostname: "*.com",
			routeHostname:    "*.example.com",
			expected:         "*.example.com",
			intersects:       true,
		},
		{
			name:             "match-any listener",
			listenerHostname: "*",
			routeHostname:    "api.example.com",
			expected:         "api.example.com",
			intersects:       true,
		},
		{
			name:             "match-any route",
			listenerHostname: "api.example.com",
			routeHostname:    "*",
			expected:         "api.example.com",
			intersects:       true,
		},
		{
			name:             "both match-any",
			listenerHostname: "*",
			routeHostname:    "*",
			expected:         "*",
			intersects:       true,
		},
		{
			name:             "empty listener matches route hostname",
			listenerHostname: "",
			routeHostname:    "api.example.com",
			expected:         "api.example.com",
			intersects:       true,
		},
		{
			name:             "empty route matches listener hostname",
			listenerHostname: "api.example.com",
			routeHostname:    "",
			expected:         "api.example.com",
			intersects:       true,
		},
		{
			name:             "both empty hostnames intersect as match-any",
			listenerHostname: "",
			routeHostname:    "",
			expected:         "*",
			intersects:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, intersects := HostnameIntersection(tt.listenerHostname, tt.routeHostname)
			assert.Equal(t, tt.intersects, intersects)
			assert.Equal(t, tt.expected, result)
		})
	}
}
