package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainsAllowed(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		allowed bool
	}{
		{name: "exact domain", url: "https://konghq.com", allowed: true},
		{name: "public subdomain", url: "https://us.api.konghq.com", allowed: true},
		{name: "private host", url: "https://us.svc.konghq.com", allowed: true},
		{name: "private host with path", url: "https://us.svc.konghq.com/api", allowed: true},
		{name: "host with port", url: "https://us.api.konghq.tech:443", allowed: true},
		{name: "uppercase", url: "https://US.API.KONGHQ.COM", allowed: true},
		{name: "dev private host", url: "https://global.svc.konghq.tech", allowed: true},
		{name: "trailing dot", url: "https://us.api.konghq.com./", allowed: true},
		{name: "lookalike suffix", url: "https://notkonghq.com", allowed: false},
		{name: "allowed domain as prefix", url: "https://konghq.com.attacker.example", allowed: false},
		{name: "userinfo hiding the host being called", url: "https://us.api.konghq.com:443@evil.example", allowed: false},
		{name: "userinfo with an allowed host", url: "https://evil.example@us.api.konghq.com", allowed: true},
		{name: "attacker host", url: "https://us.attacker.example", allowed: false},
		{name: "internal host", url: "https://us.10-0-0-1.internal", allowed: false},
		{name: "no host", url: "https://", allowed: false},
		{name: "unparsable", url: "https://us.api.konghq.com /x", allowed: false},
		{name: "empty", url: "", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.allowed, DefaultDomains.Allowed(tt.url))
		})
	}
}

func TestNewDomains(t *testing.T) {
	d, err := NewDomains(" konghq.com , example.com ")
	require.NoError(t, err)
	require.Equal(t, Domains{"konghq.com", "example.com"}, d)
	require.Equal(t, "konghq.com,example.com", d.String())

	for _, invalid := range []string{
		"",
		" ",
		"konghq.com,,example.com",
		"https://konghq.com",
		"konghq.com/api",
		"konghq.com:443",
		"user@konghq.com",
		"kong hq.com",
		"127.0.0.1",
		"::1",
	} {
		_, err := NewDomains(invalid)
		assert.Error(t, err, invalid)
	}
}

func TestDomainsAllowedEmptyList(t *testing.T) {
	require.False(t, Domains{}.Allowed("https://us.api.konghq.com"))
	require.False(t, Domains{""}.Allowed("https://us.api.konghq.com"))
}
