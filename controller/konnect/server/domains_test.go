package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDomainsAllowed(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		allowed bool
	}{
		{name: "exact domain", host: "konghq.com", allowed: true},
		{name: "public subdomain", host: "us.api.konghq.com", allowed: true},
		{name: "private host", host: "us.svc.konghq.com", allowed: true},
		{name: "private host with path", host: "us.svc.konghq.com/api", allowed: true},
		{name: "host with port", host: "us.api.konghq.tech:443", allowed: true},
		{name: "uppercase", host: "US.API.KONGHQ.COM", allowed: true},
		{name: "dev private host", host: "global.svc.konghq.tech", allowed: true},
		{name: "lookalike suffix", host: "notkonghq.com", allowed: false},
		{name: "allowed domain as prefix", host: "konghq.com.attacker.example", allowed: false},
		{name: "userinfo behind an allowed host and port", host: "us.api.konghq.com:443@evil.example", allowed: false},
		{name: "userinfo behind an allowed host", host: "us.api.konghq.com@evil.example", allowed: false},
		{name: "attacker host", host: "us.attacker.example", allowed: false},
		{name: "internal host", host: "us.10-0-0-1.internal", allowed: false},
		{name: "empty", host: "", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.allowed, DefaultDomains.Allowed(tt.host))
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
		"*.konghq.com",
		"konghq.com.",
		".konghq.com",
	} {
		_, err := NewDomains(invalid)
		require.Error(t, err, invalid)
	}
}

func TestDomainsAllowedEmptyList(t *testing.T) {
	require.False(t, Domains{}.Allowed("us.api.konghq.com"))
	require.False(t, Domains{""}.Allowed(""))
}
