package konnectendpoint

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		{name: "empty value", raw: "", want: Endpoint{}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Parse(tc.raw))
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
			e := Parse(tc.raw)
			assert.Equal(t, tc.wantServer, e.ServerName())
			assert.Equal(t, tc.wantControl, e.ControlPlaneAddress())
			assert.Equal(t, tc.wantTelemetry, e.TelemetryAddress())
		})
	}
}

func TestParseUnparseableValueKeepsLegacyRendering(t *testing.T) {
	// "host:bad" has no valid port: net/url rejects it. The rendering must stay
	// byte-identical to the string concatenation used before this helper.
	e := Parse("https://host:bad")
	assert.Equal(t, "host:bad", e.ServerName())
	assert.Equal(t, "host:bad:443", e.ControlPlaneAddress())
	assert.Equal(t, "host:bad:443", e.TelemetryAddress())
}
