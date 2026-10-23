package konnectendpoint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	testCases := []struct {
		name string
		raw  string
		want Endpoint
	}{
		{name: "private control plane endpoint", raw: "https://us.svc.konghq.com/cp/abc",
			want: Endpoint{Host: "us.svc.konghq.com", Path: "/cp/abc"}},
		{name: "private endpoint without scheme", raw: "us.svc.konghq.com/cp/abc",
			want: Endpoint{Host: "us.svc.konghq.com", Path: "/cp/abc"}},
		{name: "public endpoint without scheme", raw: "7078163243.us.cp.konghq.com",
			want: Endpoint{Host: "7078163243.us.cp.konghq.com"}},
		{name: "endpoint without scheme carrying a port", raw: "cp.endpoint:443",
			want: Endpoint{Host: "cp.endpoint", Port: "443"}},
		{name: "explicit port with a path", raw: "https://host:8443/x",
			want: Endpoint{Host: "host", Port: "8443", Path: "/x"}},
		{name: "lone slash is not a path", raw: "https://us.api.konghq.com/",
			want: Endpoint{Host: "us.api.konghq.com"}},
		{name: "query and fragment are dropped", raw: "https://host/x?a=1#f",
			want: Endpoint{Host: "host", Path: "/x"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEndpointRendering(t *testing.T) {
	testCases := []struct {
		name          string
		raw           string
		wantServer    string
		wantControl   string
		wantTelemetry string
	}{
		{name: "public with scheme", raw: "https://7078163243.us.cp.konghq.com",
			wantServer: "7078163243.us.cp.konghq.com", wantControl: "7078163243.us.cp.konghq.com:443",
			wantTelemetry: "7078163243.us.cp.konghq.com:443"},
		{name: "public without scheme", raw: "cp.endpoint",
			wantServer: "cp.endpoint", wantControl: "cp.endpoint:443", wantTelemetry: "cp.endpoint:443"},
		{name: "public without scheme carrying a port", raw: "cp.endpoint:443",
			wantServer: "cp.endpoint", wantControl: "cp.endpoint:443", wantTelemetry: "cp.endpoint:443"},
		{name: "public with an explicit port", raw: "https://host:8443",
			wantServer: "host", wantControl: "host:8443", wantTelemetry: "host:8443"},
		{name: "trailing slash", raw: "https://us.api.konghq.com/",
			wantServer: "us.api.konghq.com", wantControl: "us.api.konghq.com:443",
			wantTelemetry: "us.api.konghq.com:443"},
		{name: "private control plane endpoint", raw: "https://us.svc.konghq.com/cp/abc",
			wantServer: "us.svc.konghq.com", wantControl: "us.svc.konghq.com/cp/abc",
			wantTelemetry: "us.svc.konghq.com:443/cp/abc"},
		{name: "private telemetry endpoint", raw: "https://us.svc.konghq.com/tp/abc",
			wantServer: "us.svc.konghq.com", wantControl: "us.svc.konghq.com/tp/abc",
			wantTelemetry: "us.svc.konghq.com:443/tp/abc"},
		{name: "private endpoint with an explicit port", raw: "https://us.svc.konghq.com:8443/cp/abc",
			wantServer: "us.svc.konghq.com", wantControl: "us.svc.konghq.com:8443/cp/abc",
			wantTelemetry: "us.svc.konghq.com:8443/cp/abc"},
		{name: "private endpoint without scheme", raw: "us.svc.konghq.com/cp/abc",
			wantServer: "us.svc.konghq.com", wantControl: "us.svc.konghq.com/cp/abc",
			wantTelemetry: "us.svc.konghq.com:443/cp/abc"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := Parse(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.wantServer, e.ServerName())
			assert.Equal(t, tc.wantControl, e.ControlPlaneAddress())
			assert.Equal(t, tc.wantTelemetry, e.TelemetryAddress())
		})
	}
}

func TestParseUnusableValue(t *testing.T) {
	testCases := []struct {
		name string
		raw  string
	}{
		{name: "empty value", raw: ""},
		{name: "scheme without an authority", raw: "https://"},
		{name: "invalid port", raw: "https://host:bad"},
		{name: "invalid port with a path", raw: "https://host:bad/x"},
		{name: "unterminated IPv6 host", raw: "http://[::1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.raw)
			assert.Error(t, err)
		})
	}
}

func TestCarriesPath(t *testing.T) {
	testCases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "private control plane endpoint", raw: "https://us.svc.konghq.tech/cp/2cf352e764", want: true},
		{name: "private endpoint without scheme", raw: "us.svc.konghq.tech/api", want: true},
		{name: "public endpoint with scheme", raw: "https://2cf352e764.us.cp.konghq.tech", want: false},
		{name: "public endpoint without scheme", raw: "us.api.konghq.tech", want: false},
		{name: "lone slash is not a path", raw: "https://us.api.konghq.tech/", want: false},
		{name: "unusable value", raw: "https://host:bad", want: false},
		{name: "empty value", raw: "", want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, CarriesPath(tc.raw))
		})
	}
}

func TestCheckPrivateEndpoints(t *testing.T) {
	testCases := []struct {
		name         string
		serverURL    string
		endpoints    []string
		wantErr      bool
		wantEndpoint string
	}{
		{"both private", "https://us.svc.konghq.tech/api", []string{"https://us.svc.konghq.tech/cp/2cf352e764"}, false, ""},
		{"private server URL with a public endpoint", "https://us.svc.konghq.tech/api", []string{"https://2cf352e764.us.cp.konghq.tech"}, true, "https://2cf352e764.us.cp.konghq.tech"},
		{"public server URL with a private endpoint", "https://us.api.konghq.tech", []string{"https://us.svc.konghq.tech/cp/x"}, false, ""},
		{"both public", "https://us.api.konghq.tech", []string{"https://2cf352e764.us.cp.konghq.tech"}, false, ""},
		{"one of two endpoints public", "https://us.svc.konghq.com/api", []string{"https://us.svc.konghq.com/cp/x", "https://x.us.tp.konghq.com:443"}, true, "https://x.us.tp.konghq.com:443"},
		{"empty server URL", "", []string{"https://2cf352e764.us.cp.konghq.tech"}, false, ""},
		{"no endpoints", "https://us.svc.konghq.tech/api", nil, false, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPrivateEndpoints(tc.serverURL, tc.endpoints...)
			if tc.wantErr {
				require.Error(t, err)
				// The error is read with kubectl describe: it must name the endpoint that fails.
				assert.Contains(t, err.Error(), tc.wantEndpoint)
				return
			}
			assert.NoError(t, err)
		})
	}
}
