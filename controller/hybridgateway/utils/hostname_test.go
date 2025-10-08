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
	}{
		{
			name:             "exact match",
			listenerHostname: "api.example.com",
			routeHostname:    "api.example.com",
			expected:         "api.example.com",
		},
		{
			name:             "listener wildcard, route specific",
			listenerHostname: "*.example.com",
			routeHostname:    "api.example.com",
			expected:         "api.example.com",
		},
		{
			name:             "route wildcard, listener specific",
			listenerHostname: "api.example.com",
			routeHostname:    "*.example.com",
			expected:         "api.example.com",
		},
		{
			name:             "no intersection - different domains",
			listenerHostname: "*.example.com",
			routeHostname:    "api.other.com",
			expected:         "",
		},
		{
			name:             "multiple subdomains",
			listenerHostname: "*.example.com",
			routeHostname:    "sub.api.example.com",
			expected:         "sub.api.example.com",
		},
		{
			name:             "no intersection - exact mismatch",
			listenerHostname: "api.example.com",
			routeHostname:    "web.example.com",
			expected:         "",
		},
		{
			name:             "wildcard domain match",
			listenerHostname: "*.example.com",
			routeHostname:    "web.example.com",
			expected:         "web.example.com",
		},
		{
			name:             "both wildcards - no intersection",
			listenerHostname: "*.example.com",
			routeHostname:    "*.other.com",
			expected:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HostnameIntersection(tt.listenerHostname, tt.routeHostname)
			assert.Equal(t, tt.expected, result)
		})
	}
}
