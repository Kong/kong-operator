// Package konnectendpoint renders the endpoint values Konnect returns for a
// control plane into the forms Kong consumers accept.
package konnectendpoint

import (
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

// Parse splits a Konnect endpoint value into its parts. A value net/url cannot
// parse keeps the host as the pre-helper rendering trimmed it, so it never
// renders worse than the plain concatenation it replaces.
func Parse(raw string) Endpoint {
	toParse := raw
	if !strings.Contains(raw, "://") {
		// Without a scheme, net/url reads "cp.endpoint:443" as the scheme
		// "cp.endpoint" with no authority; "//" starts the authority part.
		toParse = "//" + raw
	}

	u, err := url.Parse(toParse)
	if err != nil {
		return Endpoint{Host: strings.TrimPrefix(raw, "https://")}
	}

	e := Endpoint{
		Host: u.Hostname(),
		Port: u.Port(),
		Path: u.Path,
	}
	if e.Path == "/" {
		e.Path = ""
	}
	return e
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
