// Package konnectendpoint renders the endpoint values Konnect returns for a
// control plane into the forms Kong consumers accept.
package konnectendpoint

import (
	"fmt"
	"net/url"
	"strings"
)

// defaultPort is the port the gateway assumes when a Konnect endpoint value
// carries none.
const defaultPort = "443"

// Endpoint is a Konnect endpoint value split into the parts the gateway
// environment variables need. Port is empty when the value carried none.
type Endpoint struct {
	Host string
	Port string
	Path string
}

// Parse splits a Konnect endpoint value into its parts. A value that cannot be
// used returns an error so the caller can refuse it rather than render it.
func Parse(raw string) (Endpoint, error) {
	toParse := raw
	if !strings.Contains(raw, "://") {
		// A Konnect endpoint can arrive without a scheme. url.Parse then reads
		// the text before the first colon as the scheme, so "cp.endpoint:443"
		// becomes scheme "cp.endpoint" with no host, and "host/cp/x" is read as
		// a path. "//" makes the text an authority (RFC 3986 §3.2), so host,
		// port and path land where the renderers read them.
		toParse = "//" + raw
	}

	u, err := url.Parse(toParse)
	if err != nil {
		return Endpoint{}, err
	}
	if u.Hostname() == "" {
		return Endpoint{}, fmt.Errorf("no host in %q", raw)
	}

	e := Endpoint{
		Host: u.Hostname(),
		Port: u.Port(),
		Path: u.Path,
	}
	if e.Path == "/" {
		e.Path = ""
	}
	return e, nil
}

// ServerName is the TLS server name to use for the endpoint.
func (e Endpoint) ServerName() string {
	return e.Host
}

// ControlPlaneAddress renders the value of KONG_CLUSTER_CONTROL_PLANE: Kong
// accepts it with or without a port, so a path keeps the port it carried.
func (e Endpoint) ControlPlaneAddress() string {
	if e.Path == "" {
		return e.Host + ":" + e.portOr443()
	}
	return e.Host + e.portSuffix() + e.Path
}

// TelemetryAddress renders the value of KONG_CLUSTER_TELEMETRY_ENDPOINT, where
// the port precedes the path: Kong refuses this address without a port.
func (e Endpoint) TelemetryAddress() string {
	return e.Host + ":" + e.portOr443() + e.Path
}

func (e Endpoint) portOr443() string {
	if e.Port == "" {
		return defaultPort
	}
	return e.Port
}

func (e Endpoint) portSuffix() string {
	if e.Port == "" {
		return ""
	}
	return ":" + e.Port
}
